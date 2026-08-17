package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useRefundBatchTestMode(t *testing.T) {
	t.Helper()
	resetBatchUpdateTestState(t)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
	})
}

func TestPromotionRefundIncludesSynchronousWalletDebitWhileUsageRemainsBatchable(t *testing.T) {
	truncateTables(t)
	useRefundBatchTestMode(t)

	user := createReserveTestUser(t, 1000)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId:             user.Id,
		Purpose:            TopUpPurposeAPIBalance,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "USD",
		PaidAmountVerified: true,
		TradeNo:            "refund-batch-drain-" + common.GetRandomString(8),
		PaymentMethod:      PaymentMethodStripe,
		PaymentProvider:    PaymentProviderStripe,
		CreateTime:         time.Now().Unix(),
		CompleteTime:       time.Now().Unix(),
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, persistUserQuotaDelta(user.Id, -300))
	addNewRecord(BatchUpdateTypeUsedQuota, user.Id, 300)
	addNewRecord(BatchUpdateTypeRequestCount, user.Id, 1)
	assert.Equal(t, 700, getUserQuotaFromDB(t, user.Id), "a successful wallet debit must be durable before refund recovery starts")

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider:            PaymentProviderStripe,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "refund-batch-drain-event",
		Kind:                PromotionRefundKindFull,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 1000,
		Currency:            "USD",
		AmountIsCumulative:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, refundCase)
	assert.Equal(t, 700, refundCase.WalletDebitedQuota)
	assert.Equal(t, int64(300), refundCase.DebtCreatedQuota)
	var stored User
	require.NoError(t, DB.Where("id = ?", user.Id).First(&stored).Error)
	assert.Zero(t, stored.Quota)
	assert.Zero(t, stored.UsedQuota)
	assert.Zero(t, stored.RequestCount)
	assert.Equal(t, int64(300), stored.RefundDebtQuota)
	assert.True(t, stored.RefundHold)

	batchUpdate()
	require.NoError(t, DB.Where("id = ?", user.Id).First(&stored).Error)
	assert.Zero(t, stored.Quota, "flushing usage counters must not mutate the wallet")
	assert.Equal(t, 300, stored.UsedQuota)
	assert.Equal(t, 1, stored.RequestCount)
}

func TestWalletDebitCannotBeDeferredUntilAfterRefundHoldRelease(t *testing.T) {
	truncateTables(t)
	useRefundBatchTestMode(t)

	user := createReserveTestUser(t, 100)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	require.NoError(t, DecreaseUserQuota(user.Id, 40, false))
	addNewRecord(BatchUpdateTypeUsedQuota, user.Id, 40)
	addNewRecord(BatchUpdateTypeRequestCount, user.Id, 1)
	assert.Equal(t, 60, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, SetUserRefundHoldFence(user.Id))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", true).Error)

	batchUpdate()
	var held User
	require.NoError(t, DB.Where("id = ?", user.Id).First(&held).Error)
	assert.Equal(t, 60, held.Quota)
	assert.Equal(t, 40, held.UsedQuota)
	assert.Equal(t, 1, held.RequestCount)
	require.ErrorIs(t, persistUserQuotaDelta(user.Id, -1), ErrUserRefundHeld)

	batchUpdate()
	require.NoError(t, DB.Where("id = ?", user.Id).First(&held).Error)
	assert.Equal(t, 60, held.Quota)
	assert.Equal(t, 40, held.UsedQuota)
	assert.Equal(t, 1, held.RequestCount)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error)
	require.NoError(t, ClearUserRefundHoldFence(user.Id))
	batchUpdate()
	require.NoError(t, DB.Where("id = ?", user.Id).First(&held).Error)
	assert.Equal(t, 60, held.Quota)
	assert.Equal(t, 40, held.UsedQuota)
	assert.Equal(t, 1, held.RequestCount)
}

func TestDurableRefundHoldRejectsDirectLocalBatchDebit(t *testing.T) {
	truncateTables(t)
	useRefundBatchTestMode(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", true).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})

	err := DecreaseUserQuota(user.Id, 10, false)
	require.ErrorIs(t, err, ErrUserRefundHeld)
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
}

func TestRefundWalletRetrySeesSynchronousCreditBeforeDebit(t *testing.T) {
	truncateTables(t)
	useRefundBatchTestMode(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"refund_hold": true, "refund_debt_quota": int64(150),
	}).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	refundCase := &PromotionRefundCase{
		EventKey: "refund-wallet-retry-batch-case", Provider: PaymentProviderStripe,
		TradeNo: "refund-wallet-retry-batch-order", RefundTradeNo: "refund-wallet-retry-batch-event",
		Kind: PromotionRefundKindFull, UserId: user.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(refundCase).Error)
	obligation := &PromotionRefundObligation{
		ObligationKey: "refund-wallet-retry-batch-obligation", RefundCaseId: refundCase.Id, UserId: user.Id,
		Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota,
		Amount: 150, SourceType: "top_ups", SourceId: 99,
	}
	require.NoError(t, DB.Create(obligation).Error)
	require.NoError(t, IncreaseUserQuota(user.Id, 100, false))
	assert.Equal(t, 200, getUserQuotaFromDB(t, user.Id))
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)

	_, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "refund-wallet-retry-batch-action",
		Action: PromotionRefundActionRetryWalletDebit, ObligationId: obligation.Id, Amount: 150,
		ActorId: 91, ActorRole: common.RoleAdminUser, Remark: "recover after synchronous credit",
	})
	require.NoError(t, err)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, 50, stored.Quota)
	assert.Zero(t, stored.RefundDebtQuota)
}
