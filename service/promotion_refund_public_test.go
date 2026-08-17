package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPromotionRefundRecoveryReturnsOnlyOwnSanitizedProgress(t *testing.T) {
	truncate(t)
	user := &model.User{
		Id: 1951, Username: "refund-progress-user", AffCode: "refund-progress-user-1951",
		Status: common.UserStatusEnabled, RefundHold: true, RefundDebtQuota: 300,
	}
	other := &model.User{
		Id: 1952, Username: "refund-progress-other", AffCode: "refund-progress-other-1952",
		Status: common.UserStatusEnabled, RefundHold: true,
	}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(other).Error)

	caseWithDebt := &model.PromotionRefundCase{
		EventKey: "private-refund-event-1", PayloadHash: "private-payload-hash",
		Provider: "stripe", TradeNo: "private-order-1", RefundTradeNo: "private-provider-ref-1",
		Kind: model.PromotionRefundKindPartial, UserId: user.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
		Reason: "private investigation note", IntakeSource: model.PromotionRefundIntakeProviderRefund,
		InitiatorType: "admin", InitiatorId: 99, ReviewerId: 98, ReviewNote: "private review note",
		CreatedAt: 100,
	}
	require.NoError(t, model.DB.Create(caseWithDebt).Error)
	quotaObligation := &model.PromotionRefundObligation{
		ObligationKey: "public-progress-quota", RefundCaseId: caseWithDebt.Id, UserId: user.Id,
		Account: model.PromotionFundAccountRefundDebt, Asset: model.PromotionFundAssetQuota,
		Amount: 500, RecoveredAmount: 200, Status: model.PromotionRefundObligationStatusOpen,
		SourceType: "top_ups", SourceId: 81,
	}
	cashObligation := &model.PromotionRefundObligation{
		ObligationKey: "public-progress-cash", RefundCaseId: caseWithDebt.Id, UserId: user.Id,
		Account: model.PromotionFundAccountRefundDebt, Asset: model.PromotionFundAssetCash, Currency: "CNY",
		Amount: 1000, RecoveredAmount: 250, Status: model.PromotionRefundObligationStatusOpen,
		SourceType: "promotion_commission_ledgers", SourceId: 82,
	}
	otherObligation := &model.PromotionRefundObligation{
		ObligationKey: "public-progress-other", RefundCaseId: caseWithDebt.Id, UserId: other.Id,
		Account: model.PromotionFundAccountRefundDebt, Asset: model.PromotionFundAssetCash, Currency: "USD",
		Amount: 9999, Status: model.PromotionRefundObligationStatusOpen,
		SourceType: "promotion_commission_ledgers", SourceId: 83,
	}
	for _, obligation := range []*model.PromotionRefundObligation{quotaObligation, cashObligation, otherObligation} {
		require.NoError(t, model.DB.Create(obligation).Error)
	}

	underReview := &model.PromotionRefundCase{
		EventKey: "private-refund-event-2", Provider: "creem", TradeNo: "private-order-2",
		RefundTradeNo: "private-provider-ref-2", Kind: model.PromotionRefundKindFull,
		UserId: user.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		RequiresRootReview: true, Reason: "private root review reason", CreatedAt: 101,
	}
	finalReview := &model.PromotionRefundCase{
		EventKey: "private-refund-event-3", Provider: "waffo", TradeNo: "private-order-3",
		RefundTradeNo: "private-provider-ref-3", Kind: model.PromotionRefundKindDispute,
		UserId: user.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		Reason: "private final review reason", CreatedAt: 102,
	}
	resolved := &model.PromotionRefundCase{
		EventKey: "private-refund-event-4", Provider: "stripe", TradeNo: "private-order-4",
		RefundTradeNo: "private-provider-ref-4", Kind: model.PromotionRefundKindFull,
		UserId: user.Id, Status: model.PromotionRefundCaseStatusResolved,
		Reason: "private resolved reason", CreatedAt: 103, ResolvedAt: 104,
	}
	unrelated := &model.PromotionRefundCase{
		EventKey: "private-refund-event-other", Provider: "stripe", TradeNo: "private-order-other",
		RefundTradeNo: "private-provider-ref-other", Kind: model.PromotionRefundKindFull,
		UserId: other.Id, Status: model.PromotionRefundCaseStatusPendingReview, CreatedAt: 105,
	}
	require.NoError(t, model.DB.Create(underReview).Error)
	require.NoError(t, model.DB.Create(finalReview).Error)
	require.NoError(t, model.DB.Create(resolved).Error)
	require.NoError(t, model.DB.Create(unrelated).Error)

	recovery, err := GetUserPromotionRefundRecovery(user.Id, &common.PageInfo{Page: 1, PageSize: 20}, "all")
	require.NoError(t, err)
	assert.True(t, recovery.Hold)
	assert.Equal(t, int64(300), recovery.OutstandingQuota)
	assert.Equal(t, []UserPromotionRefundCashDebt{{Currency: "CNY", Amount: 750}}, recovery.OutstandingCash)
	assert.Equal(t, int64(4), recovery.Total)
	require.Len(t, recovery.Items, 4)

	stages := make(map[string]string, len(recovery.Items))
	for _, item := range recovery.Items {
		stages[item.Kind+":"+item.Reference] = item.Stage
	}
	assert.Equal(t, UserPromotionRefundStageResolved, recovery.Items[0].Stage)
	assert.Equal(t, UserPromotionRefundStageFinalReview, recovery.Items[1].Stage)
	assert.Equal(t, UserPromotionRefundStageUnderReview, recovery.Items[2].Stage)
	assert.Equal(t, UserPromotionRefundStageRepaymentRequired, recovery.Items[3].Stage)
	assert.Equal(t, int64(300), recovery.Items[3].OutstandingQuota)
	assert.Equal(t, []UserPromotionRefundCashDebt{{Currency: "CNY", Amount: 750}}, recovery.Items[3].OutstandingCash)
	assert.NotEmpty(t, stages)

	encoded, err := common.Marshal(recovery)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &payload))
	items, ok := payload["items"].([]interface{})
	require.True(t, ok)
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]interface{})
		require.True(t, ok)
		for _, forbidden := range []string{
			"id", "event_key", "payload_hash", "provider", "trade_no", "refund_trade_no",
			"top_up_id", "user_id", "reason", "review_note", "reviewer_id", "initiator_id",
			"intake_source", "intake_fingerprint", "obligations", "actions", "responsible_users", "external_ref",
		} {
			assert.NotContains(t, item, forbidden)
		}
	}
	assert.NotContains(t, string(encoded), "private-")
	assert.NotContains(t, string(encoded), "9999")
	assert.NotContains(t, string(encoded), "USD")
}

func TestUserPromotionRefundRecoveryDefaultsToPendingCases(t *testing.T) {
	truncate(t)
	user := &model.User{Id: 1953, Username: "refund-pending-user", AffCode: "refund-pending-user-1953", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	for index, status := range []string{model.PromotionRefundCaseStatusPendingReview, model.PromotionRefundCaseStatusResolved} {
		require.NoError(t, model.DB.Create(&model.PromotionRefundCase{
			EventKey: "refund-status-case-" + status, Provider: "stripe", TradeNo: "refund-status-order-" + status,
			RefundTradeNo: "refund-status-ref-" + status, Kind: model.PromotionRefundKindFull,
			UserId: user.Id, Status: status, CreatedAt: int64(index + 1),
		}).Error)
	}

	recovery, err := GetUserPromotionRefundRecovery(user.Id, &common.PageInfo{Page: 1, PageSize: 20}, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), recovery.Total)
	require.Len(t, recovery.Items, 1)
	assert.Equal(t, model.PromotionRefundCaseStatusPendingReview, recovery.Items[0].Status)
}

func TestUserPromotionRefundRecoveryIncludesHeldCommissionRecipientWithoutObligation(t *testing.T) {
	truncate(t)
	principal := &model.User{
		Id: 1954, Username: "refund-principal-user", AffCode: "refund-principal-user-1954",
		Status: common.UserStatusEnabled,
	}
	commissionRecipient := &model.User{
		Id: 1955, Username: "refund-commission-user", AffCode: "refund-commission-user-1955",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(principal).Error)
	require.NoError(t, model.DB.Create(commissionRecipient).Error)
	t.Cleanup(func() {
		_ = model.DB.Model(&model.User{}).Where("id IN ?", []int{principal.Id, commissionRecipient.Id}).
			Update("refund_hold", false).Error
		_ = model.ClearUserRefundHoldFence(principal.Id)
		_ = model.ClearUserRefundHoldFence(commissionRecipient.Id)
	})

	topUp := &model.TopUp{
		UserId: principal.Id, Purpose: model.TopUpPurposeAPIBalance,
		TradeNo: "refund-secondary-visibility", PaymentMethod: "card", PaymentProvider: model.PaymentProviderStripe,
		PaidAmountMinor: 1_000, PaidCurrency: "CNY", PaidAmountVerified: true,
		CreditedQuota: 5_000, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	rebate := &model.InvitationRebate{
		InviterId: commissionRecipient.Id, InviteeId: principal.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo, PaymentProvider: topUp.PaymentProvider,
		PaidAmountMinor: topUp.PaidAmountMinor, PaidCurrency: topUp.PaidCurrency, PaidAmountVerified: true,
		RebateAmountMinor: 100, RebateCurrency: "CNY", Cashable: true, RebateQuota: 500,
		Status: model.InvitationRebateStatusSettled,
	}
	require.NoError(t, model.DB.Create(rebate).Error)
	ledger := &model.PromotionCommissionLedger{
		UserId: commissionRecipient.Id, InviteeId: principal.Id,
		SourceType: model.PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo, Cashable: true, Currency: "CNY",
		GrossAmountCents: 100, NetAmountCents: 100, QuotaEquivalent: 500,
		Status: model.PromotionCommissionStatusSettled,
	}
	require.NoError(t, model.DB.Create(ledger).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "refund-secondary-visibility-case", Provider: topUp.PaymentProvider,
		TradeNo: topUp.TradeNo, RefundTradeNo: "refund-secondary-visibility-ref",
		Kind: model.PromotionRefundKindFull, TopUpId: topUp.Id, UserId: principal.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	changed, err := model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.True(t, changed)

	var held model.User
	require.NoError(t, model.DB.Select("refund_hold").First(&held, commissionRecipient.Id).Error)
	require.True(t, held.RefundHold)
	var obligationCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundObligation{}).
		Where("refund_case_id = ? AND user_id = ?", refundCase.Id, commissionRecipient.Id).
		Count(&obligationCount).Error)
	require.Zero(t, obligationCount)

	recovery, err := GetUserPromotionRefundRecovery(
		commissionRecipient.Id,
		&common.PageInfo{Page: 1, PageSize: 20},
		model.PromotionRefundCaseStatusPendingReview,
	)
	require.NoError(t, err)
	assert.True(t, recovery.Hold)
	assert.Equal(t, int64(1), recovery.Total)
	require.Len(t, recovery.Items, 1)
	assert.Equal(t, fmt.Sprintf("RC-%06d", refundCase.Id), recovery.Items[0].Reference)
	assert.Equal(t, UserPromotionRefundStageUnderReview, recovery.Items[0].Stage)
}
