package model

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func insertInviterAndInviteeForRebateTest(t *testing.T, inviterID int, inviteeID int) {
	t.Helper()

	inviter := &User{
		Id:       inviterID,
		Username: "rebate_inviter",
		AffCode:  "rebate_inviter_" + strconv.Itoa(inviterID),
		Status:   common.UserStatusEnabled,
	}
	invitee := &User{
		Id:        inviteeID,
		Username:  "rebate_invitee",
		AffCode:   "rebate_invitee_" + strconv.Itoa(inviteeID),
		Status:    common.UserStatusEnabled,
		InviterId: inviterID,
	}

	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)
}

func getInvitationRebateCountForTest(t *testing.T, inviterID int) int64 {
	t.Helper()

	var count int64
	require.NoError(t, DB.Model(&InvitationRebate{}).Where("inviter_id = ?", inviterID).Count(&count).Error)
	return count
}

func getInvitationRewardCountForTest(t *testing.T, inviterID int, rewardType string) int64 {
	t.Helper()

	var count int64
	require.NoError(t, DB.Model(&InvitationReward{}).Where("inviter_id = ? AND reward_type = ?", inviterID, rewardType).Count(&count).Error)
	return count
}

func getInvitationQuotaForTest(t *testing.T, userID int) (int, int) {
	t.Helper()

	var user User
	require.NoError(t, DB.Select("aff_quota", "aff_history").Where("id = ?", userID).First(&user).Error)
	return user.AffQuota, user.AffHistoryQuota
}

func getPromotionCommissionLedgerForTest(t *testing.T, userID int) PromotionCommissionLedger {
	t.Helper()

	var ledger PromotionCommissionLedger
	require.NoError(t, DB.Where("user_id = ?", userID).First(&ledger).Error)
	return ledger
}

func setInvitationRebateFreezeDaysForTest(t *testing.T, days int) {
	t.Helper()

	growthSetting := operation_setting.GetGrowthSetting()
	oldDays := growthSetting.RebateFreezeDays
	t.Cleanup(func() {
		growthSetting.RebateFreezeDays = oldDays
	})
	growthSetting.RebateFreezeDays = days
}

func setInvitationRebatePercentageForTest(t *testing.T, percentage float64) {
	t.Helper()

	growthSetting := operation_setting.GetGrowthSetting()
	oldPercentage := growthSetting.InviteRebatePercentage
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		growthSetting.InviteRebatePercentage = oldPercentage
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	growthSetting.InviteRebatePercentage = percentage
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func TestCalculateInvitationRebate_CNYUsesPriceAndRoundedCash(t *testing.T) {
	oldPrice := operation_setting.Price
	oldExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		operation_setting.Price = oldPrice
		operation_setting.USDExchangeRate = oldExchangeRate
	})
	operation_setting.Price = 1
	operation_setting.USDExchangeRate = 7.3

	topUp := &TopUp{
		Money:              10.05,
		PaidAmountMinor:    1005,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
	}
	calculation, err := calculateInvitationRebate(topUp, 10)
	require.NoError(t, err)
	require.NotNil(t, calculation)

	assert.Equal(t, int64(101), calculation.rebateAmountMinor)
	assert.Equal(t, 505000, calculation.rebateQuota)
	assert.Equal(t, 1.01, calculation.rebateAmount.InexactFloat64())
}

func TestCalculateInvitationRebate_USDUsesExchangeRateThenPrice(t *testing.T) {
	oldPrice := operation_setting.Price
	oldExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		operation_setting.Price = oldPrice
		operation_setting.USDExchangeRate = oldExchangeRate
	})
	operation_setting.Price = 1
	operation_setting.USDExchangeRate = 7.3

	topUp := &TopUp{
		Money:              2,
		PaidAmountMinor:    200,
		PaidCurrency:       "USD",
		PaidAmountVerified: true,
	}
	calculation, err := calculateInvitationRebate(topUp, 10)
	require.NoError(t, err)
	require.NotNil(t, calculation)

	assert.Equal(t, "CNY", calculation.rebateCurrency)
	assert.Equal(t, int64(146), calculation.rebateAmountMinor)
	assert.Equal(t, 730000, calculation.rebateQuota)
}

func getInvitationRebateForTest(t *testing.T, inviterID int) InvitationRebate {
	t.Helper()

	var rebate InvitationRebate
	require.NoError(t, DB.Where("inviter_id = ?", inviterID).First(&rebate).Error)
	return rebate
}

func TestRechargeWaffo_SettlesInvitationRebate(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 501, 502)
	insertTopUpForPaymentGuardTest(t, "rebate-waffo-order", 502, PaymentProviderWaffo)

	topUp := GetTopUpByTradeNo("rebate-waffo-order")
	require.NotNil(t, topUp)
	topUp.Amount = 10
	topUp.Money = 20
	require.NoError(t, topUp.Update())

	require.NoError(t, RechargeWaffo("rebate-waffo-order", VerifiedPayment{AmountMinor: 2000, Currency: "CNY"}, "127.0.0.1"))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 501)
	expectedQuota := CalculateInvitationRebateQuota(20)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRebateCountForTest(t, 501))
	ledger := getPromotionCommissionLedgerForTest(t, 501)
	assert.Equal(t, PromotionCommissionStatusSettled, ledger.Status)
	assert.Equal(t, int64(200), ledger.GrossAmountCents)
	assert.Equal(t, expectedQuota, ledger.QuotaEquivalent)
}

func TestSettleInvitationRebate_UsesVerifiedPaymentSnapshot(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 521, 522)
	topUp := &TopUp{
		UserId:             522,
		Amount:             10,
		Money:              10,
		PaidAmountMinor:    8000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "rebate-verified-payment",
		PaymentMethod:      PaymentMethodStripe,
		PaymentProvider:    PaymentProviderStripe,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	assert.Equal(t, 8.0, rebate.RebateAmount)

	ledger := getPromotionCommissionLedgerForTest(t, 521)
	assert.Equal(t, "CNY", ledger.Currency)
	assert.Equal(t, int64(800), ledger.GrossAmountCents)
}

func TestSettleInvitationRebate_RequiresCurrentCompliance(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	operation_setting.GetPaymentSetting().ComplianceConfirmed = false

	insertInviterAndInviteeForRebateTest(t, 531, 532)
	topUp := &TopUp{
		UserId:          532,
		Amount:          10,
		Money:           80,
		TradeNo:         "rebate-no-compliance",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	assert.Nil(t, rebate)
	assert.Equal(t, int64(0), getInvitationRebateCountForTest(t, 531))
}

func TestSettleInvitationRebate_UnverifiedMoneyIsNotCashable(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 541, 542)
	topUp := &TopUp{
		UserId:          542,
		Amount:          10,
		Money:           80,
		TradeNo:         "rebate-unverified-payment",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	assert.False(t, rebate.Cashable)
	assert.Equal(t, "unverified_payment", rebate.RiskStatus)

	var ledgerCount int64
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("source_id = ?", rebate.Id).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
}

func TestRechargeWaffo_CreatesPendingInvitationRebateDuringFreeze(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 7)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 551, 552)
	insertTopUpForPaymentGuardTest(t, "rebate-waffo-freeze-order", 552, PaymentProviderWaffo)

	topUp := GetTopUpByTradeNo("rebate-waffo-freeze-order")
	require.NotNil(t, topUp)
	topUp.Amount = 10
	topUp.Money = 20
	require.NoError(t, topUp.Update())

	require.NoError(t, RechargeWaffo("rebate-waffo-freeze-order", VerifiedPayment{AmountMinor: 2000, Currency: "CNY"}, "127.0.0.1"))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 551)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)

	rebate := getInvitationRebateForTest(t, 551)
	assert.Equal(t, InvitationRebateStatusPending, rebate.Status)
	assert.Equal(t, 7, rebate.FreezeDays)
	assert.Greater(t, rebate.SettleAfter, rebate.CreatedAt)
	assert.Equal(t, PaymentProviderWaffo, rebate.PaymentProvider)

	require.NoError(t, DB.Model(&InvitationRebate{}).
		Where("id = ?", rebate.Id).
		Update("settle_after", common.GetTimestamp()-1).Error)
	require.NoError(t, SyncInvitationRebatesForInviter(551))

	affQuota, affHistoryQuota = getInvitationQuotaForTest(t, 551)
	expectedQuota := CalculateInvitationRebateQuota(20)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)

	rebate = getInvitationRebateForTest(t, 551)
	assert.Equal(t, InvitationRebateStatusSettled, rebate.Status)
	assert.NotZero(t, rebate.SettledAt)
	ledger := getPromotionCommissionLedgerForTest(t, 551)
	assert.Equal(t, PromotionCommissionStatusSettled, ledger.Status)
	assert.Equal(t, expectedQuota, ledger.QuotaEquivalent)
}

func TestSyncInvitationRebatesForInviter_BackfillsOnlyOnceWithoutCashLedger(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 601, 602)

	topUp := &TopUp{
		UserId:          602,
		Amount:          10,
		Money:           12.5,
		TradeNo:         "rebate-epay-backfill",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, SyncInvitationRebatesForInviter(601))
	require.NoError(t, SyncInvitationRebatesForInviter(601))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 601)
	expectedQuota := CalculateInvitationRebateQuota(12.5)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRebateCountForTest(t, 601))
	rebate := getInvitationRebateForTest(t, 601)
	assert.Equal(t, expectedQuota, rebate.RebateQuota)
	assert.False(t, rebate.Cashable)
	assert.Equal(t, "unverified_payment", rebate.RiskStatus)
	var ledgerCount int64
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("user_id = ?", 601).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
}

func TestSyncInvitationRebatesForInviter_DoesNotBackfillCommissionAfterPartialRefund(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	const (
		inviterID = 611
		inviteeID = 612
	)
	insertInviterAndInviteeForRebateTest(t, inviterID, inviteeID)
	t.Cleanup(func() {
		for _, userID := range []int{inviterID, inviteeID} {
			_ = DB.Model(&User{}).Where("id = ?", userID).Update("refund_hold", false).Error
			_ = ClearUserRefundHoldFence(userID)
		}
	})

	topUp := &TopUp{
		UserId:             inviteeID,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "rebate-partial-refund-before-backfill",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", inviteeID).Update("quota", topUp.CreditedQuota).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "rebate-partial-refund-before-backfill-1",
		Kind:                PromotionRefundKindPartial,
		PaidAmountMinor:     topUp.PaidAmountMinor,
		RefundedAmountMinor: 100,
		Currency:            topUp.PaidCurrency,
	})
	require.NoError(t, err)
	require.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	require.NotEmpty(t, refundCase.ResponsibilityFingerprint)
	require.NoError(t, DB.Where("id = ?", topUp.Id).First(topUp).Error)
	require.Equal(t, TopUpRefundStatusPartial, topUp.RefundStatus)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.Nil(t, rebate)
	require.Zero(t, getInvitationRebateCountForTest(t, inviterID))

	require.NoError(t, SyncInvitationRebatesForInviter(inviterID))
	require.NoError(t, SyncInvitationRebatesForInviter(inviterID))

	assert.Zero(t, getInvitationRebateCountForTest(t, inviterID))
	var ledgerCount int64
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("user_id = ?", inviterID).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
	require.NoError(t, DB.Where("id = ?", refundCase.Id).First(refundCase).Error)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	assert.False(t, refundCase.RequiresRootReview)
	for _, userID := range []int{inviterID, inviteeID} {
		var user User
		require.NoError(t, DB.Select("id", "refund_hold").Where("id = ?", userID).First(&user).Error)
		assert.False(t, user.RefundHold)
	}
}

func TestReverseInvitationRebate_TransferredCashCommissionDeductsQuota(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 651, 652)
	topUp := &TopUp{
		UserId:             652,
		Amount:             10,
		Money:              20,
		PaidAmountMinor:    2000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "rebate-transfer-reversal",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)

	ledger := getPromotionCommissionLedgerForTest(t, 651)
	require.NoError(t, DB.Model(&User{}).
		Where("id = ?", 651).
		Update("quota", gorm.Expr("quota + ?", ledger.QuotaEquivalent)).Error)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).
		Where("id = ?", ledger.Id).
		Updates(map[string]interface{}{
			"status":         PromotionCommissionStatusTransferred,
			"transferred_at": common.GetTimestamp(),
		}).Error)

	_, err = ReverseInvitationRebateByTopUp(topUp.Id, "refund-transfer-reversal", "refund")
	require.NoError(t, err)

	var user User
	require.NoError(t, DB.Where("id = ?", 651).First(&user).Error)
	assert.Zero(t, user.Quota)

	ledger = getPromotionCommissionLedgerForTest(t, 651)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)
	assert.Equal(t, "refund-transfer-reversal", ledger.RefundTradeNo)
	assert.Equal(t, ledger.NetAmountCents, ledger.ReversalAmountCents)
	assert.Equal(t, ledger.QuotaEquivalent, ledger.ReversalQuota)
	assert.NotZero(t, ledger.ReversedAt)
}

func TestReverseInvitationRebateByTradeNo_ReversesRebateAndLedger(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 661, 662)
	topUp := &TopUp{
		UserId:             662,
		Amount:             10,
		Money:              20,
		PaidAmountMinor:    2000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "rebate-tradeno-reversal",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)

	reversed, err := ReverseInvitationRebateByTradeNo("rebate-tradeno-reversal", "refund-tradeno-reversal", "refund")
	require.NoError(t, err)
	require.NotNil(t, reversed)
	assert.Equal(t, InvitationRebateStatusReversed, reversed.Status)
	assert.Equal(t, "refund-tradeno-reversal", reversed.RefundTradeNo)
	assert.Equal(t, reversed.RebateQuota, reversed.ReversalQuota)
	assert.NotZero(t, reversed.ReversedAt)

	ledger := getPromotionCommissionLedgerForTest(t, 661)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)
	assert.Equal(t, "refund-tradeno-reversal", ledger.RefundTradeNo)
	assert.Equal(t, ledger.NetAmountCents, ledger.ReversalAmountCents)
	assert.Equal(t, ledger.QuotaEquivalent, ledger.ReversalQuota)
}

func TestReverseInvitationRebateByTradeNo_IsIdempotent(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 671, 672)
	topUp := &TopUp{
		UserId:             672,
		Amount:             10,
		Money:              20,
		PaidAmountMinor:    2000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "rebate-idempotent-reversal",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	_, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)

	first, err := ReverseInvitationRebateByTradeNo("rebate-idempotent-reversal", "refund-idempotent-first", "refund")
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := ReverseInvitationRebateByTradeNo("rebate-idempotent-reversal", "refund-idempotent-second", "refund")
	require.NoError(t, err)
	require.NotNil(t, second)

	assert.Equal(t, InvitationRebateStatusReversed, second.Status)
	assert.Equal(t, "refund-idempotent-first", second.RefundTradeNo)

	var count int64
	require.NoError(t, DB.Model(&PromotionEvent{}).
		Where("source_table = ? AND source_id = ? AND event_type = ?", PromotionEventSourceCommissionLedger, getPromotionCommissionLedgerForTest(t, 671).Id, PromotionEventTypeCommissionReversed).
		Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestHandlePromotionRefund_FullRefundReversesPromotionRewardsOnce(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	growthSetting := operation_setting.GetGrowthSetting()
	oldInvitationReward := growthSetting.InviteFirstTopUpRewardQuota
	oldGrowthReward := growthSetting.FirstTopUpRewardQuota
	oldQuotaPerUnit := common.QuotaPerUnit
	oldExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		growthSetting.InviteFirstTopUpRewardQuota = oldInvitationReward
		growthSetting.FirstTopUpRewardQuota = oldGrowthReward
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.USDExchangeRate = oldExchangeRate
	})
	growthSetting.InviteFirstTopUpRewardQuota = 100
	growthSetting.FirstTopUpRewardQuota = 200
	common.QuotaPerUnit = 730
	operation_setting.USDExchangeRate = 7.3

	insertInviterAndInviteeForRebateTest(t, 681, 682)
	const creditedQuota = 1000
	topUp := &TopUp{
		UserId:             682,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      creditedQuota,
		PaidAmountMinor:    7300,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "refund-full-rewards",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 682).Update("quota", creditedQuota).Error)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	invitationReward, err := SettleInvitationMilestoneReward(682, InvitationRewardTypeFirstTopUp)
	require.NoError(t, err)
	require.NotNil(t, invitationReward)

	claimKey := "682:first_topup:once"
	growthReward := NewSettledGrowthReward(682, GrowthRewardItemFirstTopUp, 200, 0, "")
	growthReward.ClaimKey = &claimKey
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return CreateSettledGrowthRewardTx(tx, growthReward)
	}))

	ledger := getPromotionCommissionLedgerForTest(t, 681)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 681).Updates(map[string]interface{}{
		"aff_quota": gorm.Expr("aff_quota - ?", 60),
		"quota":     gorm.Expr("quota + ?", ledger.QuotaEquivalent+60),
	}).Error)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("status", PromotionCommissionStatusTransferred).Error)

	input := PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-full-rewards-1",
		Kind:                PromotionRefundKindFull,
		PaidAmountMinor:     7300,
		RefundedAmountMinor: 7300,
		Currency:            "CNY",
		Remark:              "full refund",
	}
	refundCase, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	require.NotNil(t, refundCase)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)

	var inviter User
	require.NoError(t, DB.Where("id = ?", 681).First(&inviter).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Zero(t, inviter.AffHistoryQuota)
	assert.Zero(t, inviter.Quota)
	var invitee User
	require.NoError(t, DB.Where("id = ?", 682).First(&invitee).Error)
	assert.Zero(t, invitee.Quota)

	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)
	require.NoError(t, DB.Where("id = ?", invitationReward.Id).First(invitationReward).Error)
	assert.Equal(t, InvitationRewardStatusReversed, invitationReward.Status)
	require.NoError(t, DB.Where("id = ?", growthReward.Id).First(growthReward).Error)
	assert.Equal(t, GrowthRewardStatusReversed, growthReward.Status)

	duplicate, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, refundCase.Id, duplicate.Id)
	require.NoError(t, DB.Where("id = ?", 681).First(&inviter).Error)
	assert.Zero(t, inviter.Quota)
}

func TestHandlePromotionRefund_PartialRefundRecoversProportionalQuotaOnce(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 691, 0)
	topUp := &TopUp{
		UserId:             691,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "USD",
		PaidAmountVerified: true,
		TradeNo:            "refund-partial-review",
		PaymentMethod:      PaymentMethodStripe,
		PaymentProvider:    PaymentProviderStripe,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 691).Update("quota", 1000).Error)
	input := PromotionRefundInput{
		Provider:            PaymentProviderStripe,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-partial-review-1",
		Kind:                PromotionRefundKindPartial,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 250,
		Currency:            "USD",
	}
	first, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	second, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, first.Id, second.Id)
	assert.Equal(t, PromotionRefundCaseStatusResolved, first.Status)
	var count int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("trade_no = ?", topUp.TradeNo).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, DB.Where("id = ?", topUp.Id).First(topUp).Error)
	assert.Equal(t, TopUpRefundStatusPartial, topUp.RefundStatus)
}

func TestHandlePromotionRefund_CumulativePartialsReverseFixedRewardAtFullAmount(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 693, 1000)
	topUp := &TopUp{
		UserId:             693,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "USD",
		PaidAmountVerified: true,
		TradeNo:            "refund-cumulative-partials",
		PaymentMethod:      PaymentMethodStripe,
		PaymentProvider:    PaymentProviderStripe,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	claimKey := "693:first_topup:cumulative-refund"
	reward := NewSettledGrowthReward(693, GrowthRewardItemFirstTopUp, 200, 0, "")
	reward.ClaimKey = &claimKey
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return CreateSettledGrowthRewardTx(tx, reward)
	}))

	first, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderStripe,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-cumulative-partials-1",
		Kind:                PromotionRefundKindPartial,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 400,
		Currency:            "USD",
		AmountIsCumulative:  true,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, first.Status)
	require.NoError(t, DB.Where("id = ?", reward.Id).First(reward).Error)
	assert.Equal(t, GrowthRewardStatusSettled, reward.Status)

	second, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderStripe,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-cumulative-partials-2",
		Kind:                PromotionRefundKindPartial,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 1000,
		Currency:            "USD",
		AmountIsCumulative:  true,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, second.Status)
	require.NoError(t, DB.Where("id = ?", reward.Id).First(reward).Error)
	assert.Equal(t, GrowthRewardStatusReversed, reward.Status)
	require.NoError(t, DB.Where("id = ?", topUp.Id).First(topUp).Error)
	assert.Equal(t, TopUpRefundStatusFull, topUp.RefundStatus)
	assert.Equal(t, 1000, topUp.RefundedQuota)
	var user User
	require.NoError(t, DB.Where("id = ?", 693).First(&user).Error)
	assert.Zero(t, user.Quota)
}

func TestPromotionCommissionSettledEventRecordsCashWithoutQuotaIncome(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 695, 696)
	topUp := &TopUp{
		UserId:             696,
		Amount:             10,
		Money:              10,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "commission-event-cash-only",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)

	var event PromotionEvent
	require.NoError(t, DB.Where("user_id = ? AND event_type = ?", 695, PromotionEventTypeCommissionSettled).First(&event).Error)
	assert.Equal(t, PromotionEventDirectionIncome, event.Direction)
	assert.Zero(t, event.QuotaDelta)
	assert.Positive(t, event.CashAmountCents)
}

func TestHandlePromotionRefund_PartialRefundImmediatelyInvalidatesSettledCommission(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 7011, 7012)
	topUp := &TopUp{
		UserId:             7012,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "refund-partial-settled-commission",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 7012).Update("quota", 1000).Error)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 7011)
	assert.Equal(t, PromotionCommissionStatusSettled, ledger.Status)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-partial-settled-commission-1",
		Kind:                PromotionRefundKindPartial,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 100,
		Currency:            "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusReversed, rebate.Status)
	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)

	ledgers, err := LockSettledPromotionCommissionLedgersTx(DB, 7011)
	require.NoError(t, err)
	assert.Empty(t, ledgers)
}

func TestHandlePromotionRefund_WithdrawingCommissionCancelsUnpaidWithdrawal(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 711, 712)
	topUp := &TopUp{
		UserId:             712,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "refund-withdrawing-review",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 712).Update("quota", 1000).Error)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 711)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("status", PromotionCommissionStatusWithdrawing).Error)
	otherLedger := &PromotionCommissionLedger{
		UserId: 711, SourceType: "test_commission", SourceId: 1, Cashable: true,
		Currency: "CNY", GrossAmountCents: 50, NetAmountCents: 50,
		Status: PromotionCommissionStatusWithdrawing,
	}
	require.NoError(t, DB.Create(otherLedger).Error)
	withdrawal := &PromotionWithdrawal{
		UserId:           711,
		Currency:         ledger.Currency,
		GrossAmountCents: ledger.NetAmountCents + otherLedger.NetAmountCents,
		NetAmountCents:   ledger.NetAmountCents + otherLedger.NetAmountCents,
		Status:           PromotionWithdrawalStatusPendingReview,
		PayoutMethod:     "alipay",
	}
	require.NoError(t, DB.Create(withdrawal).Error)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)
	require.NoError(t, DB.Create([]*PromotionWithdrawalItem{
		{WithdrawalId: withdrawal.Id, LedgerId: ledger.Id, AmountCents: ledger.NetAmountCents},
		{WithdrawalId: withdrawal.Id, LedgerId: otherLedger.Id, AmountCents: otherLedger.NetAmountCents},
	}).Error)
	reserve := &PromotionFundTransaction{
		TransactionKey: "withdrawal:" + strconv.Itoa(withdrawal.Id) + ":reserved",
		Kind:           PromotionFundKindCommissionWithdrawalReserved,
		UserId:         withdrawal.UserId,
		SourceType:     "promotion_withdrawals",
		SourceId:       withdrawal.Id,
		SourceKey:      "promotion_withdrawals:" + strconv.Itoa(withdrawal.Id),
		ActorType:      "user",
		ActorId:        withdrawal.UserId,
	}
	require.NoError(t, CreatePromotionFundTransactionTx(DB, reserve, []PromotionFundTransactionLeg{
		{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: -ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
		{Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
		{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: -otherLedger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: otherLedger.Id},
		{Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: otherLedger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: otherLedger.Id},
	}))

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-withdrawing-review-1",
		Kind:                PromotionRefundKindFull,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 1000,
		Currency:            "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusReversed, rebate.Status)
	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)
	assert.False(t, ledger.Cashable)
	require.NoError(t, DB.Where("id = ?", withdrawal.Id).First(withdrawal).Error)
	assert.Equal(t, PromotionWithdrawalStatusFailed, withdrawal.Status)
	require.NoError(t, DB.Where("id = ?", otherLedger.Id).First(otherLedger).Error)
	assert.Equal(t, PromotionCommissionStatusSettled, otherLedger.Status)

	var operations []PromotionWithdrawalOperation
	require.NoError(t, DB.Where("withdrawal_id = ?", withdrawal.Id).Order("id ASC").Find(&operations).Error)
	require.Len(t, operations, 1)
	assert.Equal(t, PromotionWithdrawalActionCancelledByRefund, operations[0].Action)
	assert.Equal(t, PromotionWithdrawalActorSystem, operations[0].ActorType)
	assert.Equal(t, "refund-withdrawing-review-1", operations[0].ExternalReference)

	var release PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("transaction_key = ?", "withdrawal:"+strconv.Itoa(withdrawal.Id)+":released").
		First(&release).Error)
	assert.Equal(t, reserve.Id, release.ReversesTransactionId)
	require.Len(t, release.Legs, 4)
	assert.Equal(t, []int{ledger.Id, ledger.Id, otherLedger.Id, otherLedger.Id}, []int{
		release.Legs[0].SourceId, release.Legs[1].SourceId, release.Legs[2].SourceId, release.Legs[3].SourceId,
	})
}

func TestHandlePromotionRefund_ProcessingWithdrawalWithoutPaidJournalCreatesWholePayoutDebt(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 713, 714)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)
	topUp := &TopUp{
		UserId:             714,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "refund-withdrawal-processing",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 714).Update("quota", 1000).Error)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 713)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("status", PromotionCommissionStatusWithdrawing).Error)
	otherLedger := &PromotionCommissionLedger{
		UserId: 713, SourceType: "test_commission", SourceId: 71301, Cashable: true,
		Currency: ledger.Currency, GrossAmountCents: 50, NetAmountCents: 50,
		Status: PromotionCommissionStatusWithdrawing,
	}
	require.NoError(t, DB.Create(otherLedger).Error)
	now := common.GetTimestamp()
	withdrawal := &PromotionWithdrawal{
		UserId: 713, Currency: ledger.Currency,
		GrossAmountCents: ledger.NetAmountCents + otherLedger.NetAmountCents,
		NetAmountCents:   ledger.NetAmountCents + otherLedger.NetAmountCents,
		Status:           PromotionWithdrawalStatusProcessing, PayoutMethod: "bank", TradeNo: "payout-processing-713",
		ReviewerId: 91, ReviewedAt: now - 1, PayoutInitiatedAt: now,
	}
	require.NoError(t, DB.Create(withdrawal).Error)
	require.NoError(t, DB.Create([]*PromotionWithdrawalItem{
		{WithdrawalId: withdrawal.Id, LedgerId: ledger.Id, AmountCents: ledger.NetAmountCents},
		{WithdrawalId: withdrawal.Id, LedgerId: otherLedger.Id, AmountCents: otherLedger.NetAmountCents},
	}).Error)
	require.NoError(t, CreatePromotionWithdrawalOperationTx(DB, &PromotionWithdrawalOperation{
		WithdrawalId: withdrawal.Id, Action: PromotionWithdrawalActionPayoutInitiated,
		ActorType: PromotionWithdrawalActorAdmin, ActorId: 91,
		ExternalReference: withdrawal.TradeNo, CreatedAt: withdrawal.PayoutInitiatedAt,
	}))
	require.NoError(t, CreatePromotionFundTransactionTx(DB, &PromotionFundTransaction{
		TransactionKey: "withdrawal:" + strconv.Itoa(withdrawal.Id) + ":reserved",
		Kind:           PromotionFundKindCommissionWithdrawalReserved,
		UserId:         withdrawal.UserId,
		SourceType:     "promotion_withdrawals",
		SourceId:       withdrawal.Id,
		SourceKey:      "promotion_withdrawals:" + strconv.Itoa(withdrawal.Id),
		ActorType:      "user",
		ActorId:        withdrawal.UserId,
	}, []PromotionFundTransactionLeg{
		{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: -ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
		{Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
		{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: otherLedger.Currency, Amount: -otherLedger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: otherLedger.Id},
		{Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash, Currency: otherLedger.Currency, Amount: otherLedger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: otherLedger.Id},
	}))

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-withdrawal-processing-1",
		Kind:                PromotionRefundKindFull,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 1000,
		Currency:            "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)
	assert.Equal(t, withdrawal.NetAmountCents, refundCase.CashDebtCreatedMinor)
	require.Len(t, refundCase.Obligations, 1)
	obligation := refundCase.Obligations[0]
	assert.Equal(t, withdrawal.UserId, obligation.UserId)
	assert.Equal(t, PromotionFundAssetCash, obligation.Asset)
	assert.Equal(t, withdrawal.Currency, obligation.Currency)
	assert.Equal(t, withdrawal.NetAmountCents, obligation.Amount)
	assert.Equal(t, "promotion_withdrawals", obligation.SourceType)
	assert.Equal(t, withdrawal.Id, obligation.SourceId)
	assert.Equal(t, PromotionRefundObligationStatusOpen, obligation.Status)

	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)
	require.NoError(t, DB.Where("id = ?", otherLedger.Id).First(otherLedger).Error)
	assert.Equal(t, PromotionCommissionStatusSettled, otherLedger.Status)
	require.NoError(t, DB.Where("id = ?", withdrawal.Id).First(withdrawal).Error)
	assert.Equal(t, PromotionWithdrawalStatusFailed, withdrawal.Status)
	assert.Contains(t, withdrawal.ReviewNote, "payout result is unknown")
	var inviter User
	require.NoError(t, DB.Where("id = ?", 713).First(&inviter).Error)
	assert.True(t, inviter.RefundHold)

	var payoutCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", "withdrawal:"+strconv.Itoa(withdrawal.Id)+":paid").Count(&payoutCount).Error)
	assert.Zero(t, payoutCount)
	var release PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("transaction_key = ?", "withdrawal:"+strconv.Itoa(withdrawal.Id)+":released").First(&release).Error)
	require.Len(t, release.Legs, 4)
	assert.Equal(t, PromotionFundAccountCommissionReserved, release.Legs[0].Account)
	assert.Equal(t, -ledger.NetAmountCents, release.Legs[0].Amount)
	assert.Equal(t, PromotionFundAccountCommissionAvailable, release.Legs[1].Account)
	assert.Equal(t, ledger.NetAmountCents, release.Legs[1].Amount)
	assert.Equal(t, PromotionFundAccountCommissionReserved, release.Legs[2].Account)
	assert.Equal(t, -otherLedger.NetAmountCents, release.Legs[2].Amount)
	assert.Equal(t, PromotionFundAccountCommissionAvailable, release.Legs[3].Account)
	assert.Equal(t, otherLedger.NetAmountCents, release.Legs[3].Amount)

	var payoutDebt PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").
		Where("transaction_key = ?", fmt.Sprintf("refund:%d:promotion_withdrawals:%d:cash_debt", refundCase.Id, withdrawal.Id)).
		First(&payoutDebt).Error)
	require.Len(t, payoutDebt.Legs, 1)
	assert.Equal(t, PromotionFundAccountRefundDebt, payoutDebt.Legs[0].Account)
	assert.Equal(t, PromotionFundAssetCash, payoutDebt.Legs[0].Asset)
	assert.Equal(t, withdrawal.NetAmountCents, payoutDebt.Legs[0].Amount)

	var cancelledOperations int64
	require.NoError(t, DB.Model(&PromotionWithdrawalOperation{}).
		Where("withdrawal_id = ? AND action = ?", withdrawal.Id, PromotionWithdrawalActionCancelledByRefund).
		Count(&cancelledOperations).Error)
	assert.Equal(t, int64(1), cancelledOperations)
}

func TestHandlePromotionRefund_UnknownCommissionStateRequiresRootReviewAndHoldsBothUsers(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 715, 716)
	topUp := &TopUp{
		UserId:             716,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "refund-unknown-commission-state",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", topUp.CreditedQuota).Error)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 715)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("status", "legacy_unknown").Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-unknown-commission-state-1",
		Kind:                PromotionRefundKindFull,
		PaidAmountMinor:     topUp.PaidAmountMinor,
		RefundedAmountMinor: topUp.PaidAmountMinor,
		Currency:            topUp.PaidCurrency,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)
	assert.True(t, refundCase.RequiresRootReview)
	assert.Equal(t, topUp.Id, refundCase.TopUpId)
	assert.Equal(t, rebate.Id, refundCase.InvitationRebateId)
	assert.Equal(t, ledger.Id, refundCase.CommissionLedgerId)
	assert.Contains(t, refundCase.Reason, `commission ledger state "legacy_unknown" requires Root assessment`)
	assert.Empty(t, refundCase.Obligations)

	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, "legacy_unknown", ledger.Status)
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusSettled, rebate.Status)
	for _, userId := range []int{715, 716} {
		var user User
		require.NoError(t, DB.Where("id = ?", userId).First(&user).Error)
		assert.True(t, user.RefundHold)
	}
	ensureFinancialActorTestUser(t, 91, common.RoleRootUser)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "release-unknown-commission-hold",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleRootUser,
		Remark:                            "attempt release before Root assessment",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.EqualError(t, err, "refund case requires a root review waiver before hold release")
}

func TestHandlePromotionRefund_TransferredCommissionWithInsufficientWalletCreatesDebt(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 721, 722)
	topUp := &TopUp{
		UserId:             722,
		Purpose:            TopUpPurposeAPIBalance,
		Amount:             10,
		Money:              10,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "refund-transferred-insufficient-wallet",
		PaymentMethod:      "alipay",
		PaymentProvider:    PaymentProviderEpay,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 722).Update("quota", 1000).Error)
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 721)
	require.Positive(t, ledger.QuotaEquivalent)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Updates(map[string]interface{}{
		"status":         PromotionCommissionStatusTransferred,
		"transferred_at": common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 721).
		Update("quota", common.MinQuota+ledger.QuotaEquivalent-1).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderEpay,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-transferred-insufficient-wallet-1",
		Kind:                PromotionRefundKindFull,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 1000,
		Currency:            "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)
	assert.Equal(t, int64(ledger.QuotaEquivalent), refundCase.DebtCreatedQuota)
	require.Len(t, refundCase.Obligations, 1)
	assert.Equal(t, int64(ledger.QuotaEquivalent), refundCase.Obligations[0].OutstandingAmount())
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusReversed, rebate.Status)
	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)
	assert.False(t, ledger.Cashable)
	var inviter User
	require.NoError(t, DB.Where("id = ?", 721).First(&inviter).Error)
	assert.True(t, inviter.RefundHold)
	assert.Equal(t, int64(ledger.QuotaEquivalent), inviter.RefundDebtQuota)
}

func TestSyncInvitationRebatesForInviter_ExcludesSubscriptionTopUps(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)

	insertInviterAndInviteeForRebateTest(t, 701, 702)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 801)
	insertSubscriptionOrderForPaymentGuardTest(t, "rebate-subscription-order", 702, plan.Id, PaymentProviderStripe)
	topUp := &TopUp{
		UserId:          702,
		Purpose:         TopUpPurposeSubscription,
		Amount:          0,
		Money:           9.99,
		TradeNo:         "rebate-subscription-order",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, SyncInvitationRebatesForInviter(701))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 701)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)
	assert.Equal(t, int64(0), getInvitationRebateCountForTest(t, 701))
}

func TestInvitationFirstTopUpRewardIgnoresSubscriptionCompatibilityRows(t *testing.T) {
	truncateTables(t)
	growthSetting := operation_setting.GetGrowthSetting()
	oldQuota := growthSetting.InviteFirstTopUpRewardQuota
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		growthSetting.InviteFirstTopUpRewardQuota = oldQuota
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	growthSetting.InviteFirstTopUpRewardQuota = 500
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	insertInviterAndInviteeForRebateTest(t, 781, 782)
	subscriptionTopUp := &TopUp{
		UserId: 782, Purpose: TopUpPurposeSubscription, Money: 9.99,
		TradeNo: "reward-subscription-first", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, CreateTime: time.Now().Unix(),
		CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, subscriptionTopUp.Insert())

	reward, err := SettleInvitationMilestoneReward(782, InvitationRewardTypeFirstTopUp)
	require.NoError(t, err)
	assert.Nil(t, reward)

	apiTopUp := &TopUp{
		UserId: 782, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		TradeNo: "reward-api-first", PaymentMethod: "alipay",
		PaymentProvider: PaymentProviderEpay, CreateTime: time.Now().Unix(),
		CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, apiTopUp.Insert())
	reward, err = SettleInvitationMilestoneReward(782, InvitationRewardTypeFirstTopUp)
	require.NoError(t, err)
	require.NotNil(t, reward)
	assert.Equal(t, apiTopUp.Id, reward.TriggerTopUpId)
}

func TestInvitationFirstTopUpReward_SettlesOnlyOnce(t *testing.T) {
	truncateTables(t)
	growthSetting := operation_setting.GetGrowthSetting()
	oldQuota := growthSetting.InviteFirstTopUpRewardQuota
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		growthSetting.InviteFirstTopUpRewardQuota = oldQuota
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	growthSetting.InviteFirstTopUpRewardQuota = 1234
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	insertInviterAndInviteeForRebateTest(t, 801, 802)

	topUp := &TopUp{
		UserId:          802,
		Amount:          10,
		Money:           10,
		TradeNo:         "reward-first-topup-1",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	var reward *InvitationReward
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		reward, err = SettleInvitationMilestoneRewardTx(tx, 802, InvitationRewardTypeFirstTopUp)
		return err
	}))
	require.NotNil(t, reward)

	secondTopUp := &TopUp{
		UserId:          802,
		Amount:          10,
		Money:           10,
		TradeNo:         "reward-first-topup-2",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, secondTopUp.Insert())
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		reward, err := SettleInvitationMilestoneRewardTx(tx, 802, InvitationRewardTypeFirstTopUp)
		require.NoError(t, err)
		assert.Nil(t, reward)
		return nil
	}))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 801)
	assert.Equal(t, 1234, affQuota)
	assert.Equal(t, 1234, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, 801, InvitationRewardTypeFirstTopUp))
}

func TestInvitationFirstRequestReward_SettlesOnlyOnce(t *testing.T) {
	truncateTables(t)
	growthSetting := operation_setting.GetGrowthSetting()
	oldQuota := growthSetting.InviteFirstRequestRewardQuota
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		growthSetting.InviteFirstRequestRewardQuota = oldQuota
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	growthSetting.InviteFirstRequestRewardQuota = 4321
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	insertInviterAndInviteeForRebateTest(t, 901, 902)

	reward, err := SettleInvitationMilestoneReward(902, InvitationRewardTypeFirstRequest)
	require.NoError(t, err)
	require.NotNil(t, reward)
	reward, err = SettleInvitationMilestoneReward(902, InvitationRewardTypeFirstRequest)
	require.NoError(t, err)
	assert.Nil(t, reward)

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 901)
	assert.Equal(t, 4321, affQuota)
	assert.Equal(t, 4321, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, 901, InvitationRewardTypeFirstRequest))
}

func TestInvitationFirstRequestRewardDoesNotBackfillExistingUsers(t *testing.T) {
	truncateTables(t)
	growthSetting := operation_setting.GetGrowthSetting()
	oldQuota := growthSetting.InviteFirstRequestRewardQuota
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		growthSetting.InviteFirstRequestRewardQuota = oldQuota
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	growthSetting.InviteFirstRequestRewardQuota = 4321
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	insertInviterAndInviteeForRebateTest(t, 905, 906)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 906).Update("request_count", 7).Error)

	var reward *InvitationReward
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		reward, err = QueueInvitationFirstRequestRewardTx(tx, 906)
		return err
	}))
	assert.Nil(t, reward)
	assert.Zero(t, getInvitationRewardCountForTest(t, 905, InvitationRewardTypeFirstRequest))
}

func TestInvitationFirstRequestRewardRetriesDurableSnapshotAfterJournalFailure(t *testing.T) {
	truncateTables(t)
	growthSetting := operation_setting.GetGrowthSetting()
	oldQuota := growthSetting.InviteFirstRequestRewardQuota
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	oldBatchUpdate := common.BatchUpdateEnabled
	t.Cleanup(func() {
		growthSetting.InviteFirstRequestRewardQuota = oldQuota
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
		common.BatchUpdateEnabled = oldBatchUpdate
		require.NoError(t, DB.AutoMigrate(&PromotionFundTransactionLeg{}))
	})
	growthSetting.InviteFirstRequestRewardQuota = 4321
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	common.BatchUpdateEnabled = false

	insertInviterAndInviteeForRebateTest(t, 911, 912)
	require.NoError(t, DB.Migrator().DropTable(&PromotionFundTransactionLeg{}))
	UpdateUserUsedQuotaAndRequestCount(912, 25)

	var invitee User
	require.NoError(t, DB.Where("id = ?", 912).First(&invitee).Error)
	assert.Equal(t, 1, invitee.RequestCount)
	assert.Equal(t, 25, invitee.UsedQuota)
	var pending InvitationReward
	require.NoError(t, DB.Where("invitee_id = ? AND reward_type = ?", 912, InvitationRewardTypeFirstRequest).First(&pending).Error)
	assert.Equal(t, InvitationRewardStatusPending, pending.Status)
	assert.Equal(t, 4321, pending.RewardQuota)
	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 911)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)

	growthSetting.InviteFirstRequestRewardQuota = 9999
	require.NoError(t, DB.AutoMigrate(&PromotionFundTransactionLeg{}))
	reward, err := SettleInvitationMilestoneReward(912, InvitationRewardTypeFirstRequest)
	require.NoError(t, err)
	require.NotNil(t, reward)
	assert.Equal(t, 4321, reward.RewardQuota)
	assert.Equal(t, InvitationRewardStatusSettled, reward.Status)
	reward, err = SettleInvitationMilestoneReward(912, InvitationRewardTypeFirstRequest)
	require.NoError(t, err)
	assert.Nil(t, reward)
	affQuota, affHistoryQuota = getInvitationQuotaForTest(t, 911)
	assert.Equal(t, 4321, affQuota)
	assert.Equal(t, 4321, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, 911, InvitationRewardTypeFirstRequest))

	var issuedCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", "invitation_reward:"+strconv.Itoa(pending.Id)+":issued").
		Count(&issuedCount).Error)
	assert.Equal(t, int64(1), issuedCount)
}

func TestInvitationMilestoneRewardRemainsPendingWhenInvitationQuotaIsFull(t *testing.T) {
	truncateTables(t)
	growthSetting := operation_setting.GetGrowthSetting()
	oldQuota := growthSetting.InviteFirstRequestRewardQuota
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		growthSetting.InviteFirstRequestRewardQuota = oldQuota
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	growthSetting.InviteFirstRequestRewardQuota = 200
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	insertInviterAndInviteeForRebateTest(t, 951, 952)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 951).Updates(map[string]interface{}{
		"aff_quota":   common.MaxWalletQuota - 100,
		"aff_history": common.MaxWalletQuota - 100,
	}).Error)

	reward, err := SettleInvitationMilestoneReward(952, InvitationRewardTypeFirstRequest)
	require.NoError(t, err)
	assert.Nil(t, reward)
	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 951)
	assert.Equal(t, common.MaxWalletQuota-100, affQuota)
	assert.Equal(t, common.MaxWalletQuota-100, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, 951, InvitationRewardTypeFirstRequest))
	var pending InvitationReward
	require.NoError(t, DB.Where("invitee_id = ? AND reward_type = ?", 952, InvitationRewardTypeFirstRequest).First(&pending).Error)
	assert.Equal(t, InvitationRewardStatusPending, pending.Status)
	assert.Equal(t, 200, pending.RewardQuota)
}

func TestRechargeEpayKeepsTopUpWhenFirstTopUpRewardCapacityIsFull(t *testing.T) {
	truncateTables(t)
	setInvitationRebatePercentageForTest(t, 0)
	growthSetting := operation_setting.GetGrowthSetting()
	oldRewardQuota := growthSetting.InviteFirstTopUpRewardQuota
	oldQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		growthSetting.InviteFirstTopUpRewardQuota = oldRewardQuota
		common.QuotaPerUnit = oldQuotaPerUnit
	})
	growthSetting.InviteFirstTopUpRewardQuota = 200
	common.QuotaPerUnit = 100

	insertInviterAndInviteeForRebateTest(t, 971, 972)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 971).Updates(map[string]interface{}{
		"aff_quota":   common.MaxWalletQuota - 100,
		"aff_history": common.MaxWalletQuota - 100,
	}).Error)
	topUp := &TopUp{
		UserId:          972,
		Amount:          2,
		Money:           10,
		TradeNo:         "epay-milestone-capacity",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)

	alreadyDone, err := RechargeEpay(topUp.TradeNo, "alipay", VerifiedPayment{AmountMinor: 1000, Currency: "CNY"}, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	require.NoError(t, DB.Where("id = ?", topUp.Id).First(topUp).Error)
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	var invitee User
	require.NoError(t, DB.Select("quota").Where("id = ?", 972).First(&invitee).Error)
	assert.Equal(t, 200, invitee.Quota)
	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 971)
	assert.Equal(t, common.MaxWalletQuota-100, affQuota)
	assert.Equal(t, common.MaxWalletQuota-100, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, 971, InvitationRewardTypeFirstTopUp))
	var pending InvitationReward
	require.NoError(t, DB.Where("invitee_id = ? AND reward_type = ?", 972, InvitationRewardTypeFirstTopUp).First(&pending).Error)
	assert.Equal(t, InvitationRewardStatusPending, pending.Status)
	assert.Equal(t, 200, pending.RewardQuota)
	assert.Equal(t, topUp.Id, pending.TriggerTopUpId)
}

func TestInvitationRegisterRewardWithZeroQuotaCountsOnceAndStaysOutOfRewardList(t *testing.T) {
	truncateTables(t)
	oldQuotaForInviter := common.QuotaForInviter
	t.Cleanup(func() { common.QuotaForInviter = oldQuotaForInviter })
	common.QuotaForInviter = 0

	inviter := &User{
		Id:       961,
		Username: "register_zero_reward_inviter",
		AffCode:  "register_zero_reward_inviter",
		Status:   common.UserStatusEnabled,
	}
	invitee := &User{Id: 962, Username: "register_zero_reward_invitee", AffCode: "register_zero_reward_invitee", Status: common.UserStatusEnabled, InviterId: inviter.Id}
	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)

	require.NoError(t, inviteUser(inviter.Id, invitee.Id))
	require.NoError(t, inviteUser(inviter.Id, invitee.Id))
	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, inviter.Id)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)
	var reloaded User
	require.NoError(t, DB.Select("aff_count").Where("id = ?", inviter.Id).First(&reloaded).Error)
	assert.Equal(t, 1, reloaded.AffCount)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, inviter.Id, InvitationRewardTypeRegister))

	records, total, err := GetUserInvitationRewardRecords(inviter.Id, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, records)
}

func TestInvitationRegisterReward_IsRecorded(t *testing.T) {
	truncateTables(t)
	oldQuotaForInviter := common.QuotaForInviter
	oldQuotaForInvitee := common.QuotaForInvitee
	oldQuotaForNewUser := common.QuotaForNewUser
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		common.QuotaForInviter = oldQuotaForInviter
		common.QuotaForInvitee = oldQuotaForInvitee
		common.QuotaForNewUser = oldQuotaForNewUser
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	common.QuotaForInviter = 2468
	common.QuotaForInvitee = 1357
	common.QuotaForNewUser = 100
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	inviter := &User{
		Id:       1001,
		Username: "register_reward_inviter",
		AffCode:  "register_reward_inviter",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(inviter).Error)

	invitee := &User{
		Username:  "register_reward_invitee",
		Password:  "password123",
		InviterId: 1001,
		Status:    common.UserStatusEnabled,
	}
	require.NoError(t, invitee.Insert(1001))
	assert.Equal(t, 1457, invitee.Quota)

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 1001)
	assert.Equal(t, 2468, affQuota)
	assert.Equal(t, 2468, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRewardCountForTest(t, 1001, InvitationRewardTypeRegister))

	var reward InvitationReward
	require.NoError(t, DB.Where("inviter_id = ? AND reward_type = ?", 1001, InvitationRewardTypeRegister).First(&reward).Error)
	assert.Equal(t, invitee.Id, reward.InviteeId)
	assert.Equal(t, 2468, reward.RewardQuota)
	assert.Equal(t, InvitationRewardStatusSettled, reward.Status)
}
