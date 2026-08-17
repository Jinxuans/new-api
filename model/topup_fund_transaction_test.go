package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRechargeEpayRecordsTopUpFundTransactionExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := &User{
		Username: "topup-fund-user", Status: common.UserStatusEnabled, Quota: 7,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Amount: 2, Money: 1,
		TradeNo: "topup-fund-epay", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())
	payment := VerifiedPayment{AmountMinor: 100, Currency: "CNY", ProviderPaymentId: "epay-payment-1"}

	alreadyDone, err := RechargeEpay(topUp.TradeNo, "alipay", payment, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	alreadyDone, err = RechargeEpay(topUp.TradeNo, "alipay", payment, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)

	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 27, reloadedUser.Quota)

	var transactions []PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").Where("source_type = ? AND source_id = ?", "top_ups", topUp.Id).Find(&transactions).Error)
	require.Len(t, transactions, 1)
	transaction := transactions[0]
	assert.Equal(t, "topup:"+fmt.Sprint(topUp.Id)+":credited", transaction.TransactionKey)
	assert.Equal(t, "api_balance_topup_credited", transaction.Kind)
	assert.Equal(t, user.Id, transaction.UserId)
	assert.Equal(t, topUp.TradeNo, transaction.SourceKey)
	assert.Equal(t, payment.ProviderPaymentId, transaction.ExternalRef)
	require.Len(t, transaction.Legs, 1)
	leg := transaction.Legs[0]
	assert.Equal(t, PromotionFundAccountAPIBalance, leg.Account)
	assert.Equal(t, PromotionFundAssetQuota, leg.Asset)
	assert.Equal(t, int64(20), leg.Amount)
	require.NotNil(t, leg.BalanceAfter)
	assert.Equal(t, int64(27), *leg.BalanceAfter)
}

func TestAllAPIBalanceTopUpCompletionPathsRecordFundTransaction(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	payment := VerifiedPayment{AmountMinor: 100, Currency: "CNY", ProviderPaymentId: "provider-payment"}
	testCases := []struct {
		name           string
		provider       string
		amount         int64
		money          float64
		expectedCredit int
		expectedActor  string
		complete       func(*TopUp) error
	}{
		{
			name: "epay", provider: PaymentProviderEpay, amount: 2, money: 1, expectedCredit: 20, expectedActor: "provider",
			complete: func(topUp *TopUp) error {
				_, err := RechargeEpay(topUp.TradeNo, "alipay", payment, "127.0.0.1")
				return err
			},
		},
		{
			name: "stripe", provider: PaymentProviderStripe, amount: 3, money: 3, expectedCredit: 30, expectedActor: "provider",
			complete: func(topUp *TopUp) error {
				return RechargeStripe(topUp.TradeNo, "cus_topup_fund", payment, "127.0.0.1")
			},
		},
		{
			name: "creem", provider: PaymentProviderCreem, amount: 4, money: 4, expectedCredit: 4, expectedActor: "provider",
			complete: func(topUp *TopUp) error {
				return RechargeCreem(topUp.TradeNo, "fund@example.invalid", "Fund User", payment, "127.0.0.1")
			},
		},
		{
			name: "waffo", provider: PaymentProviderWaffo, amount: 5, money: 5, expectedCredit: 50, expectedActor: "provider",
			complete: func(topUp *TopUp) error {
				return RechargeWaffo(topUp.TradeNo, payment, "127.0.0.1")
			},
		},
		{
			name: "waffo pancake", provider: PaymentProviderWaffoPancake, amount: 6, money: 6, expectedCredit: 60, expectedActor: "provider",
			complete: func(topUp *TopUp) error {
				return RechargeWaffoPancake(topUp.TradeNo, payment)
			},
		},
		{
			name: "administrator completion", provider: PaymentProviderEpay, amount: 7, money: 7, expectedCredit: 70, expectedActor: "admin",
			complete: func(topUp *TopUp) error {
				return ManualCompleteTopUp(ManualTopUpCompletionInput{
					TradeNo: topUp.TradeNo, CallerIp: "127.0.0.1", ActorId: 9001,
					ActorRef: "root-auditor", Reason: "provider callback was verified manually",
				})
			},
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			if testCase.expectedActor == "admin" {
				require.NoError(t, DB.Create(&User{
					Id: 9001, Username: "root-auditor", AffCode: "root-auditor-aff",
					Role: common.RoleRootUser, Status: common.UserStatusEnabled,
				}).Error)
			}
			user := &User{
				Username: fmt.Sprintf("all-topup-fund-user-%d", index),
				Status:   common.UserStatusEnabled,
				Quota:    11,
			}
			require.NoError(t, DB.Create(user).Error)
			topUp := &TopUp{
				UserId: user.Id, Amount: testCase.amount, Money: testCase.money,
				TradeNo: fmt.Sprintf("all-topup-fund-%d", index), PaymentMethod: testCase.provider,
				PaymentProvider: testCase.provider, CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
			}
			require.NoError(t, topUp.Insert())

			require.NoError(t, testCase.complete(topUp))
			require.NoError(t, testCase.complete(topUp))

			var reloadedUser User
			require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
			assert.Equal(t, 11+testCase.expectedCredit, reloadedUser.Quota)
			var transactions []PromotionFundTransaction
			require.NoError(t, DB.Preload("Legs").
				Where("source_type = ? AND source_id = ?", PromotionFundSourceTopUps, topUp.Id).
				Find(&transactions).Error)
			require.Len(t, transactions, 1)
			assert.Equal(t, PromotionFundKindTopUpCredited, transactions[0].Kind)
			assert.Equal(t, testCase.expectedActor, transactions[0].ActorType)
			if testCase.expectedActor == "admin" {
				assert.Equal(t, 9001, transactions[0].ActorId)
				assert.Equal(t, "root-auditor", transactions[0].ActorRef)
				assert.Equal(t, "provider callback was verified manually", transactions[0].Remark)
			} else {
				assert.Zero(t, transactions[0].ActorId)
				assert.Equal(t, testCase.provider, transactions[0].ActorRef)
			}
			require.Len(t, transactions[0].Legs, 1)
			assert.Equal(t, int64(testCase.expectedCredit), transactions[0].Legs[0].Amount)
			require.NotNil(t, transactions[0].Legs[0].BalanceAfter)
			assert.Equal(t, int64(11+testCase.expectedCredit), *transactions[0].Legs[0].BalanceAfter)
		})
	}
}

func TestManualTopUpRequiresAuditableReasonBeforeMutation(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "manual-topup-reason-user", Status: common.UserStatusEnabled, Quota: 19}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Amount: 1, Money: 1, TradeNo: "manual-topup-reason",
		PaymentMethod: PaymentMethodCreem, PaymentProvider: PaymentProviderCreem,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	err := ManualCompleteTopUp(ManualTopUpCompletionInput{
		TradeNo: topUp.TradeNo, CallerIp: "127.0.0.1", ActorId: 9002, ActorRef: "root-auditor",
	})
	require.ErrorIs(t, err, ErrManualTopUpReasonNeeded)

	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 19, reloadedUser.Quota)
	var reloadedTopUp TopUp
	require.NoError(t, DB.First(&reloadedTopUp, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, reloadedTopUp.Status)
	var count int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", PromotionFundSourceTopUps, topUp.Id).
		Count(&count).Error)
	assert.Zero(t, count)
}

func TestRechargeEpayRollsBackCreditWhenFundTransactionCannotBeWritten(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := &User{
		Username: "topup-fund-rollback-user", Status: common.UserStatusEnabled, Quota: 7,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Amount: 2, Money: 1,
		TradeNo: "topup-fund-rollback", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, DB.Migrator().DropTable(&PromotionFundTransactionLeg{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&PromotionFundTransactionLeg{}))
	})
	_, err := RechargeEpay(topUp.TradeNo, "alipay", VerifiedPayment{AmountMinor: 100, Currency: "CNY"}, "127.0.0.1")
	require.Error(t, err)

	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 7, reloadedUser.Quota)
	var reloadedTopUp TopUp
	require.NoError(t, DB.First(&reloadedTopUp, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, reloadedTopUp.Status)
	assert.Zero(t, reloadedTopUp.CreditedQuota)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "top_ups", topUp.Id).
		Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestCompleteSubscriptionOrderDoesNotRecordAPIBalanceTopUp(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "subscription-fund-user", Status: common.UserStatusEnabled, Quota: 50}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Fund Evidence Plan", PriceAmount: 9.99, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, TotalAmount: 1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	order := &SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: 9.99, TradeNo: "subscription-fund-order",
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(),
	}
	require.NoError(t, order.Insert())

	require.NoError(t, CompleteSubscriptionOrder(order.TradeNo, "", PaymentProviderStripe, PaymentMethodStripe))

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&topUp).Error)
	assert.Equal(t, TopUpPurposeSubscription, topUp.Purpose)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "top_ups", topUp.Id).
		Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 50, reloadedUser.Quota)
}

func TestTopUpRefundReversalReferencesOriginalCredit(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := &User{
		Username: "topup-refund-link-user", AffCode: "topup-refund-link-code",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Amount: 10, Money: 10,
		TradeNo: "topup-refund-link", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())
	payment := VerifiedPayment{AmountMinor: 1000, Currency: "CNY", ProviderPaymentId: "epay-payment-refund-link"}
	_, err := RechargeEpay(topUp.TradeNo, "alipay", payment, "127.0.0.1")
	require.NoError(t, err)

	var original PromotionFundTransaction
	require.NoError(t, DB.Where("transaction_key = ?", fmt.Sprintf("topup:%d:credited", topUp.Id)).First(&original).Error)
	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderEpay, TradeNo: topUp.TradeNo, RefundTradeNo: "epay-refund-link",
		Kind: PromotionRefundKindFull, PaidAmountMinor: payment.AmountMinor,
		RefundedAmountMinor: payment.AmountMinor, Currency: payment.Currency,
	})
	require.NoError(t, err)

	var reversal PromotionFundTransaction
	require.NoError(t, DB.Where("transaction_key = ?", fmt.Sprintf("refund:%d:principal", refundCase.Id)).First(&reversal).Error)
	assert.Equal(t, original.Id, reversal.ReversesTransactionId)
	assert.Equal(t, PromotionFundKindReversal, reversal.Kind)
}

func TestBackfillTopUpFundTransactionLeavesHistoricalBalanceUnknown(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)

	rows := []*TopUp{
		{
			UserId: 2001, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 250,
			TradeNo: "historical-topup-snapshot", PaymentProvider: PaymentProviderEpay,
			Status: common.TopUpStatusSuccess, CreateTime: 100, CompleteTime: 200,
		},
		{
			UserId: 2002, Purpose: TopUpPurposeAPIBalance, Amount: 2, Money: 2,
			TradeNo: "historical-topup-derived", PaymentProvider: PaymentProviderStripe,
			Status: common.TopUpStatusSuccess, CreateTime: 300, CompleteTime: 400,
		},
		{
			UserId: 2003, Purpose: TopUpPurposeSubscription, CreditedQuota: 999,
			TradeNo: "historical-subscription", PaymentProvider: PaymentProviderStripe,
			Status: common.TopUpStatusSuccess, CreateTime: 500, CompleteTime: 600,
		},
		{
			UserId: 2004, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 100,
			TradeNo: "historical-pending-topup", PaymentProvider: PaymentProviderEpay,
			Status: common.TopUpStatusPending, CreateTime: 700,
		},
	}
	for _, row := range rows {
		require.NoError(t, db.Create(row).Error)
	}

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = oldQuotaPerUnit * 7
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	require.NoError(t, BackfillPromotionFundTransactions(db))
	require.NoError(t, ReconcilePromotionFundTransactions(db))

	var transactions []PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("source_type = ?", "top_ups").Order("source_id ASC").Find(&transactions).Error)
	require.Len(t, transactions, 1)
	assert.Equal(t, rows[0].Id, transactions[0].SourceId)
	assert.Equal(t, "api_balance_topup_credited", transactions[0].Kind)
	require.Len(t, transactions[0].Legs, 1)
	assert.Equal(t, int64(250), transactions[0].Legs[0].Amount)
	assert.Nil(t, transactions[0].Legs[0].BalanceAfter)

	var unsupportedCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", PromotionFundSourceTopUps, rows[1].Id).
		Count(&unsupportedCount).Error)
	assert.Zero(t, unsupportedCount, "a changed quota ratio must not invent a historical wallet credit")

	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("source_type = ?", "top_ups").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestBackfillTopUpFundTransactionReusesRealtimeRecord(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	topUp := &TopUp{
		UserId: 2010, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 300,
		TradeNo: "realtime-before-backfill", PaymentProvider: PaymentProviderEpay,
		Status: common.TopUpStatusSuccess, CreateTime: 100, CompleteTime: 200,
	}
	require.NoError(t, db.Create(topUp).Error)
	balanceAfter := int64(450)
	realtime := &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("topup:%d:credited", topUp.Id),
		Kind:           "api_balance_topup_credited", UserId: topUp.UserId,
		SourceType: "top_ups", SourceId: topUp.Id, SourceKey: topUp.TradeNo,
		ActorType: "provider", ActorRef: topUp.PaymentProvider, OccurredAt: topUp.CompleteTime,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, realtime, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
			Amount: int64(topUp.CreditedQuota), SourceType: "top_ups", SourceId: topUp.Id,
			BalanceAfter: &balanceAfter,
		}})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))

	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "top_ups", topUp.Id).
		Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var stored PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").First(&stored, realtime.Id).Error)
	require.Len(t, stored.Legs, 1)
	require.NotNil(t, stored.Legs[0].BalanceAfter)
	assert.Equal(t, balanceAfter, *stored.Legs[0].BalanceAfter)
}
