package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAdminPromotionRefundCaseReusesAccountingAndFundEvidence(t *testing.T) {
	truncateTables(t)

	root := &User{Username: "manual-refund-root", AffCode: "manual-refund-root-aff", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	user := &User{Username: "manual-refund-user", AffCode: "manual-refund-user-aff", Status: common.UserStatusEnabled, Quota: 200}
	require.NoError(t, DB.Create(root).Error)
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "manual-refund-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)

	input := AdminPromotionRefundCaseInput{
		IdempotencyKey: "manual-refund-request-1", TradeNo: topUp.TradeNo,
		ExternalReference: "refund-provider-AbC-1", IntakeSource: PromotionRefundIntakeProviderRefund,
		Kind: PromotionRefundKindPartial, RefundedAmountMinor: 500, Currency: "cny",
		Remark: "Provider dashboard confirms a partial refund.", ActorId: root.Id, ActorRole: root.Role,
	}
	refundCase, err := CreateAdminPromotionRefundCase(input)
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundIntakeProviderRefund, refundCase.IntakeSource)
	assert.Equal(t, "admin", refundCase.InitiatorType)
	assert.Equal(t, root.Id, refundCase.InitiatorId)
	assert.Equal(t, input.ExternalReference, refundCase.RefundTradeNo)
	assert.NotContains(t, refundCase.EventKey, input.IdempotencyKey)

	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.Zero(t, storedUser.Quota)
	assert.Equal(t, int64(300), storedUser.RefundDebtQuota)
	assert.True(t, storedUser.RefundHold)

	var transactions []PromotionFundTransaction
	require.NoError(t, DB.Where("source_type = ? AND source_id = ?", "promotion_refund_cases", refundCase.Id).
		Order("id ASC").Find(&transactions).Error)
	require.NotEmpty(t, transactions)
	for _, transaction := range transactions {
		assert.Equal(t, "admin", transaction.ActorType)
		assert.Equal(t, root.Id, transaction.ActorId)
		assert.Empty(t, transaction.ActorRef)
		assert.Equal(t, input.ExternalReference, transaction.ExternalRef)
	}

	replayed, err := CreateAdminPromotionRefundCase(input)
	require.NoError(t, err)
	assert.Equal(t, refundCase.Id, replayed.Id)
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.Zero(t, storedUser.Quota)
	assert.Equal(t, int64(300), storedUser.RefundDebtQuota)

	// A delayed webhook with the same provider identity must converge on the
	// Root-created case instead of applying the refund a second time.
	webhookReplay, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: input.ExternalReference, Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 500, Currency: "CNY",
		Remark: "stripe refund",
	})
	require.NoError(t, err)
	assert.Equal(t, refundCase.Id, webhookReplay.Id)
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount)
}

func TestCreateAdminPromotionRefundCaseRejectsIdempotencyPayloadChange(t *testing.T) {
	truncateTables(t)
	root := &User{Username: "manual-refund-conflict-root", AffCode: "manual-refund-conflict-root-aff", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	user := &User{Username: "manual-refund-conflict-user", AffCode: "manual-refund-conflict-user-aff", Status: common.UserStatusEnabled, Quota: 1000}
	require.NoError(t, DB.Create(root).Error)
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "manual-refund-conflict-order", PaymentMethod: PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	input := AdminPromotionRefundCaseInput{
		IdempotencyKey: "manual-refund-conflict-key", TradeNo: topUp.TradeNo,
		ExternalReference: "manual-refund-conflict-ref", IntakeSource: PromotionRefundIntakeOfflineRefund,
		Kind: PromotionRefundKindPartial, RefundedAmountMinor: 100, Currency: "CNY",
		Remark: "Confirmed offline refund receipt.", ActorId: root.Id, ActorRole: root.Role,
	}
	_, err := CreateAdminPromotionRefundCase(input)
	require.NoError(t, err)

	input.RefundedAmountMinor = 200
	_, err = CreateAdminPromotionRefundCase(input)
	require.ErrorIs(t, err, ErrPromotionRefundEventConflict)
	input.RefundedAmountMinor = 100
	input.Remark = "Changed evidence interpretation."
	_, err = CreateAdminPromotionRefundCase(input)
	require.ErrorIs(t, err, ErrPromotionRefundEventConflict)

	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount)
}

func TestCreateAdminPromotionRefundCaseRequiresRootAndKnownOrder(t *testing.T) {
	truncateTables(t)
	input := AdminPromotionRefundCaseInput{
		IdempotencyKey: "manual-refund-permission", TradeNo: "missing-order",
		ExternalReference: "manual-refund-permission-ref", IntakeSource: PromotionRefundIntakeMissedCallback,
		Kind: PromotionRefundKindFull, RefundedAmountMinor: 100, Currency: "CNY",
		Remark: strings.Repeat("e", 10), ActorId: 9, ActorRole: common.RoleAdminUser,
	}
	_, err := CreateAdminPromotionRefundCase(input)
	require.EqualError(t, err, "only root can create a refund recovery case")

	input.ActorRole = common.RoleRootUser
	_, err = CreateAdminPromotionRefundCase(input)
	require.EqualError(t, err, "local top-up order not found")
}
