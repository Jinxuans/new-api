package model

import (
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
	topUp := &TopUp{
		UserId:             682,
		Amount:             10,
		Money:              10,
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

func TestHandlePromotionRefund_PartialRefundCreatesOnePendingReviewCase(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 691, 0)
	topUp := &TopUp{
		UserId:             691,
		Amount:             10,
		Money:              10,
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
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, first.Status)
	var count int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("trade_no = ?", topUp.TradeNo).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, DB.Where("id = ?", topUp.Id).First(topUp).Error)
	assert.Equal(t, TopUpRefundStatusPartial, topUp.RefundStatus)
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
		Amount:             10,
		Money:              10,
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
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusReversed, rebate.Status)
	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusReversed, ledger.Status)

	ledgers, err := LockSettledPromotionCommissionLedgersTx(DB, 7011)
	require.NoError(t, err)
	assert.Empty(t, ledgers)
}

func TestHandlePromotionRefund_WithdrawingCommissionStaysPendingReview(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 711, 712)
	topUp := &TopUp{
		UserId:             712,
		Amount:             10,
		Money:              10,
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
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 711)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("status", PromotionCommissionStatusWithdrawing).Error)

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
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)
	assert.Contains(t, refundCase.Reason, PromotionCommissionStatusWithdrawing)
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusSettled, rebate.Status)
	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusWithdrawing, ledger.Status)
	assert.False(t, ledger.Cashable)
}

func TestHandlePromotionRefund_TransferredCommissionWithInsufficientWalletStaysPendingReview(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	insertInviterAndInviteeForRebateTest(t, 721, 722)
	topUp := &TopUp{
		UserId:             722,
		Amount:             10,
		Money:              10,
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
	assert.Contains(t, refundCase.Reason, "wallet balance is insufficient")
	require.NoError(t, DB.Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, InvitationRebateStatusSettled, rebate.Status)
	require.NoError(t, DB.Where("id = ?", ledger.Id).First(&ledger).Error)
	assert.Equal(t, PromotionCommissionStatusTransferred, ledger.Status)
	assert.False(t, ledger.Cashable)
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

func TestInvitationMilestoneRewardSkipsWhenInvitationQuotaIsFull(t *testing.T) {
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
		"aff_quota":   common.MaxQuota - 100,
		"aff_history": common.MaxQuota - 100,
	}).Error)

	reward, err := SettleInvitationMilestoneReward(952, InvitationRewardTypeFirstRequest)
	require.NoError(t, err)
	assert.Nil(t, reward)
	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 951)
	assert.Equal(t, common.MaxQuota-100, affQuota)
	assert.Equal(t, common.MaxQuota-100, affHistoryQuota)
	assert.Zero(t, getInvitationRewardCountForTest(t, 951, InvitationRewardTypeFirstRequest))
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
		"aff_quota":   common.MaxQuota - 100,
		"aff_history": common.MaxQuota - 100,
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
	assert.Equal(t, common.MaxQuota-100, affQuota)
	assert.Equal(t, common.MaxQuota-100, affHistoryQuota)
	assert.Zero(t, getInvitationRewardCountForTest(t, 971, InvitationRewardTypeFirstTopUp))
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
	oldCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	oldComplianceVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		common.QuotaForInviter = oldQuotaForInviter
		operation_setting.GetPaymentSetting().ComplianceConfirmed = oldCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = oldComplianceVersion
	})
	common.QuotaForInviter = 2468
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
