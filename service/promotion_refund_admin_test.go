package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedPromotionRefundAdminActors(t *testing.T) {
	t.Helper()
	seedFinancialActor(t, 1, common.RoleRootUser)
	seedFinancialActor(t, 91, common.RoleAdminUser)
}

func createRefundRecoveryFixture(t *testing.T, key string, quota int, debt int64) (*model.User, *model.PromotionRefundCase, *model.PromotionRefundObligation) {
	t.Helper()
	seedPromotionRefundAdminActors(t)
	user := &model.User{
		Username: key + "-user", AffCode: key + "-aff", Status: common.UserStatusEnabled, Quota: quota,
		RefundDebtQuota: debt, RefundHold: true,
	}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: key + "-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusFailed,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: key + "-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: key + "-refund",
		Kind: model.PromotionRefundKindFull, UserId: user.Id, TopUpId: topUp.Id,
		Status: model.PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	obligation := &model.PromotionRefundObligation{
		ObligationKey: key + "-obligation", RefundCaseId: refundCase.Id, UserId: user.Id,
		Account: model.PromotionFundAccountRefundDebt, Asset: model.PromotionFundAssetQuota,
		Amount: debt, SourceType: "top_ups", SourceId: topUp.Id,
	}
	if debt > 0 {
		require.NoError(t, model.DB.Create(obligation).Error)
	}
	_, err := model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)
	return user, refundCase, obligation
}

func createManualRefundCaseFixture(t *testing.T, key string) (*model.User, *model.TopUp, *model.PromotionRefundCase) {
	t.Helper()
	seedPromotionRefundAdminActors(t)
	user := &model.User{
		Username: key + "-user", AffCode: key + "-aff", Status: common.UserStatusEnabled, Quota: 100,
	}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: key + "-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: key + "-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: key + "-refund", Kind: model.PromotionRefundKindFull,
		Currency: "CNY", UserId: user.Id, TopUpId: topUp.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	_, err := model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)
	return user, topUp, refundCase
}

func createSubscriptionRefundListFixture(t *testing.T, key string) (*model.User, *model.SubscriptionPlan, *model.TopUp, *model.PromotionRefundCase) {
	t.Helper()
	user := &model.User{
		Username: key + "-user", AffCode: key + "-aff", Status: common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Title: key + " plan", PriceAmount: 10, Currency: "CNY", Enabled: true,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1_000_000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeSubscription, TradeNo: key + "-order",
		PaymentMethod: model.PaymentMethodStripe, PaymentProvider: model.PaymentProviderStripe,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: key + "-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: key + "-refund", Kind: model.PromotionRefundKindFull,
		Currency: "CNY", UserId: user.Id, TopUpId: topUp.Id,
		Status: model.PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	return user, plan, topUp, refundCase
}

func requireListedResponsibilityIntegrityFailure(t *testing.T, refundCase *model.PromotionRefundCase, expected string) {
	t.Helper()
	refundCases, total, err := ListAdminPromotionRefundCases(&common.PageInfo{Page: 1, PageSize: 20}, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, refundCases, 1)
	assert.Equal(t, refundCase.Id, refundCases[0].Id)
	assert.True(t, refundCases[0].RequiresRootReview)
	assert.Contains(t, refundCases[0].ResponsibilityIntegrityError, expected)
	assert.Contains(t, refundCases[0].Reason, expected)

	var stored model.PromotionRefundCase
	require.NoError(t, model.DB.First(&stored, refundCase.Id).Error)
	assert.False(t, stored.RequiresRootReview, "a list request must not silently persist review state")
	assert.Equal(t, refundCase.Reason, stored.Reason, "a list request must not rewrite the stored case reason")
}

func TestListAdminPromotionRefundCasesFailsClosedForBrokenSubscriptionLinks(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
		arrange  func(t *testing.T, user *model.User, plan *model.SubscriptionPlan, topUp *model.TopUp)
	}{
		{
			name:     "missing payment order",
			key:      "refund-subscription-missing-order",
			expected: "no unique matching payment order",
		},
		{
			name:     "payment order belongs to another user",
			key:      "refund-subscription-order-mismatch",
			expected: "payment order does not match the refunded top-up",
			arrange: func(t *testing.T, _ *model.User, plan *model.SubscriptionPlan, topUp *model.TopUp) {
				otherUser := &model.User{
					Username: "refund-subscription-other-user", AffCode: "refundsubscriptionother",
					Status: common.UserStatusEnabled,
				}
				require.NoError(t, model.DB.Create(otherUser).Error)
				require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
					UserId: otherUser.Id, PlanId: plan.Id, Money: plan.PriceAmount,
					TradeNo: topUp.TradeNo, PaymentMethod: model.PaymentMethodStripe,
					PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
					CreateTime: common.GetTimestamp(), CompleteTime: common.GetTimestamp(),
				}).Error)
			},
		},
		{
			name:     "completed order has no entitlement link",
			key:      "refund-subscription-unbound-order",
			expected: "payment order has no linked entitlement",
			arrange: func(t *testing.T, user *model.User, plan *model.SubscriptionPlan, topUp *model.TopUp) {
				require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
					UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount,
					TradeNo: topUp.TradeNo, PaymentMethod: model.PaymentMethodStripe,
					PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
					CreateTime: common.GetTimestamp(), CompleteTime: common.GetTimestamp(),
				}).Error)
			},
		},
		{
			name:     "linked entitlement was deleted",
			key:      "refund-subscription-missing-entitlement",
			expected: "linked subscription entitlement no longer exists",
			arrange: func(t *testing.T, user *model.User, plan *model.SubscriptionPlan, topUp *model.TopUp) {
				missingSubscriptionId := 987654
				require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
					UserId: user.Id, PlanId: plan.Id, UserSubscriptionId: &missingSubscriptionId,
					Money: plan.PriceAmount, TradeNo: topUp.TradeNo, PaymentMethod: model.PaymentMethodStripe,
					PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
					CreateTime: common.GetTimestamp(), CompleteTime: common.GetTimestamp(),
				}).Error)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			user, plan, topUp, refundCase := createSubscriptionRefundListFixture(t, test.key)
			if test.arrange != nil {
				test.arrange(t, user, plan, topUp)
			}

			requireListedResponsibilityIntegrityFailure(t, refundCase, test.expected)
		})
	}
}

func TestListAdminPromotionRefundCasesIncludesRecoveryDetails(t *testing.T) {
	truncate(t)
	_, pending, obligation := createRefundRecoveryFixture(t, "refund-admin-list", 100, 80)
	resolved := &model.PromotionRefundCase{
		EventKey: "refund-admin-list-resolved", Provider: model.PaymentProviderStripe,
		TradeNo: "refund-admin-list-resolved-order", RefundTradeNo: "refund-admin-list-resolved-refund",
		Kind: model.PromotionRefundKindFull, Status: model.PromotionRefundCaseStatusResolved,
	}
	require.NoError(t, model.DB.Create(resolved).Error)
	action := &model.PromotionRefundAction{
		ActionKey: "refund-admin-list-action", RefundCaseId: pending.Id, ObligationId: obligation.Id,
		UserId: obligation.UserId, Action: model.PromotionRefundActionRecordExternalRepayment,
		Asset: model.PromotionFundAssetQuota, Amount: 20, ActorId: 91,
	}
	require.NoError(t, model.DB.Create(action).Error)

	pageInfo := &common.PageInfo{Page: 1, PageSize: 20}
	refundCases, total, err := ListAdminPromotionRefundCases(pageInfo, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, refundCases, 1)
	assert.Equal(t, pending.Id, refundCases[0].Id)
	require.Len(t, refundCases[0].Obligations, 1)
	assert.Equal(t, obligation.Id, refundCases[0].Obligations[0].Id)
	require.Len(t, refundCases[0].Actions, 1)
	assert.Equal(t, action.Id, refundCases[0].Actions[0].Id)
}

func TestListAdminPromotionRefundCasesIncludesVerifiedResponsibleUsers(t *testing.T) {
	truncate(t)
	principal := &model.User{Username: "refund-principal", AffCode: "refundprincipal", Status: common.UserStatusEnabled}
	commissionRecipient := &model.User{Username: "refund-commission", AffCode: "refundcommission", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(principal).Error)
	require.NoError(t, model.DB.Create(commissionRecipient).Error)
	topUp := &model.TopUp{
		UserId: principal.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "responsible-users-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	rebate := &model.InvitationRebate{
		InviterId: commissionRecipient.Id, InviteeId: principal.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo,
	}
	require.NoError(t, model.DB.Create(rebate).Error)
	ledger := &model.PromotionCommissionLedger{
		UserId: commissionRecipient.Id, InviteeId: principal.Id,
		SourceType: model.PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo,
		Currency:      "CNY", GrossAmountCents: 120, NetAmountCents: 120, QuotaEquivalent: 600,
		Status: model.PromotionCommissionStatusReversed,
	}
	require.NoError(t, model.DB.Create(ledger).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "responsible-users-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "responsible-users-refund", Kind: model.PromotionRefundKindFull,
		Currency: "CNY", UserId: principal.Id, TopUpId: topUp.Id,
		InvitationRebateId: rebate.Id, CommissionLedgerId: ledger.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	refundCases, total, err := ListAdminPromotionRefundCases(&common.PageInfo{Page: 1, PageSize: 20}, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, refundCases, 1)
	require.Len(t, refundCases[0].ResponsibleUsers, 2)
	assert.Equal(t, principal.Id, refundCases[0].ResponsibleUsers[0].UserId)
	assert.Equal(t, principal.Username, refundCases[0].ResponsibleUsers[0].Username)
	assert.True(t, refundCases[0].ResponsibleUsers[0].IsTopUpUser)
	assert.Equal(t, commissionRecipient.Id, refundCases[0].ResponsibleUsers[1].UserId)
	assert.True(t, refundCases[0].ResponsibleUsers[1].IsCommissionRecipient)
	assert.Equal(t, int64(120), refundCases[0].ResponsibleUsers[1].CommissionAmountMinor)
	assert.Equal(t, 600, refundCases[0].ResponsibleUsers[1].CommissionQuota)
}

func TestListAdminPromotionRefundCasesRediscoversLateUniqueTopUp(t *testing.T) {
	truncate(t)
	seedPromotionRefundAdminActors(t)
	user := &model.User{
		Username: "late-refund-topup-user", AffCode: "laterefundtopupuser",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "late-refund-topup-case", Provider: model.PaymentProviderStripe,
		TradeNo: "late-refund-topup-order", RefundTradeNo: "late-refund-topup-event",
		Kind: model.PromotionRefundKindFull, Currency: "CNY",
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance,
		TradeNo: refundCase.TradeNo, PaymentProvider: refundCase.Provider,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)

	refundCases, total, err := ListAdminPromotionRefundCases(&common.PageInfo{Page: 1, PageSize: 20}, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, refundCases, 1)
	assert.Equal(t, topUp.Id, refundCases[0].TopUpId)
	assert.Equal(t, user.Id, refundCases[0].UserId)
	require.Len(t, refundCases[0].ResponsibleUsers, 1)
	assert.Equal(t, user.Id, refundCases[0].ResponsibleUsers[0].UserId)
	assert.True(t, refundCases[0].ResponsibleUsers[0].IsTopUpUser)

	var storedCase model.PromotionRefundCase
	require.NoError(t, model.DB.First(&storedCase, refundCase.Id).Error)
	assert.Zero(t, storedCase.TopUpId, "read-time discovery must not silently rewrite the case")
	assert.Zero(t, storedCase.UserId)
	changed, err := model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, model.DB.First(&storedCase, refundCase.Id).Error)
	assessed, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "late-refund-topup-obligation", Action: model.PromotionRefundActionDefineManualObligation,
		UserId: user.Id, TopUpId: topUp.Id, Asset: model.PromotionFundAssetQuota, Amount: 100,
		ExternalRef: "late-refund-topup-evidence", Remark: "late order uniquely matches provider and trade number",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, topUp.Id, assessed.TopUpId)
	assert.Equal(t, user.Id, assessed.UserId)
	require.Len(t, assessed.Obligations, 1)
}

func TestListAdminPromotionRefundCasesDoesNotRediscoverProviderMismatch(t *testing.T) {
	truncate(t)
	user := &model.User{
		Username: "late-refund-provider-user", AffCode: "laterefundprovideruser",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "late-refund-provider-case", Provider: model.PaymentProviderStripe,
		TradeNo: "late-refund-provider-order", RefundTradeNo: "late-refund-provider-event",
		Kind: model.PromotionRefundKindFull, Status: model.PromotionRefundCaseStatusPendingReview,
		RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance,
		TradeNo: refundCase.TradeNo, PaymentProvider: model.PaymentProviderCreem,
		Status: common.TopUpStatusSuccess,
	}).Error)

	refundCases, _, err := ListAdminPromotionRefundCases(&common.PageInfo{Page: 1, PageSize: 20}, "")
	require.NoError(t, err)
	require.Len(t, refundCases, 1)
	assert.Zero(t, refundCases[0].TopUpId)
	assert.Zero(t, refundCases[0].UserId)
	assert.Empty(t, refundCases[0].ResponsibleUsers)
}

func TestListAdminPromotionRefundCasesFlagsMissingStoredTopUp(t *testing.T) {
	truncate(t)
	user := &model.User{Username: "missing-topup-user", AffCode: "missingtopupuser", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "missing-topup-case", Provider: model.PaymentProviderStripe,
		TradeNo: "missing-topup-order", RefundTradeNo: "missing-topup-refund", Kind: model.PromotionRefundKindFull,
		TopUpId: 987654, UserId: user.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		Reason: "Provider refund received.",
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	requireListedResponsibilityIntegrityFailure(t, refundCase, "stored top-up link no longer exists")
}

func TestListAdminPromotionRefundCasesFlagsInconsistentStoredTopUp(t *testing.T) {
	truncate(t)
	user := &model.User{Username: "mismatched-topup-user", AffCode: "mismatchedtopupuser", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "mismatched-topup-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "mismatched-topup-case", Provider: model.PaymentProviderStripe,
		TradeNo: "different-order", RefundTradeNo: "mismatched-topup-refund", Kind: model.PromotionRefundKindFull,
		TopUpId: topUp.Id, UserId: user.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		Reason: "Provider refund received.",
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	requireListedResponsibilityIntegrityFailure(t, refundCase, "stored top-up link does not match")
}

func TestListAdminPromotionRefundCasesFlagsMissingStoredRebate(t *testing.T) {
	truncate(t)
	user := &model.User{Username: "missing-rebate-user", AffCode: "missingrebateuser", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "missing-rebate-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "missing-rebate-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "missing-rebate-refund", Kind: model.PromotionRefundKindFull,
		TopUpId: topUp.Id, UserId: user.Id, InvitationRebateId: 987654,
		Status: model.PromotionRefundCaseStatusPendingReview, Reason: "Provider refund received.",
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	requireListedResponsibilityIntegrityFailure(t, refundCase, "stored invitation rebate link no longer exists")
}

func TestListAdminPromotionRefundCasesFlagsMissingStoredCommissionLedger(t *testing.T) {
	truncate(t)
	user := &model.User{Username: "missing-ledger-user", AffCode: "missingledgeruser", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "missing-ledger-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "missing-ledger-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "missing-ledger-refund", Kind: model.PromotionRefundKindFull,
		TopUpId: topUp.Id, UserId: user.Id, CommissionLedgerId: 987654,
		Status: model.PromotionRefundCaseStatusPendingReview, Reason: "Provider refund received.",
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	requireListedResponsibilityIntegrityFailure(t, refundCase, "stored commission ledger link no longer exists")
}

func TestListAdminPromotionRefundCasesFlagsInconsistentInvitationReward(t *testing.T) {
	truncate(t)
	principal := &model.User{Username: "reward-principal", AffCode: "rewardprincipal", Status: common.UserStatusEnabled}
	inviter := &model.User{Username: "reward-inviter", AffCode: "rewardinviter", Status: common.UserStatusEnabled}
	otherInvitee := &model.User{Username: "reward-other", AffCode: "rewardother", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(principal).Error)
	require.NoError(t, model.DB.Create(inviter).Error)
	require.NoError(t, model.DB.Create(otherInvitee).Error)
	topUp := &model.TopUp{
		UserId: principal.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "inconsistent-reward-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	require.NoError(t, model.DB.Create(&model.InvitationReward{
		InviterId: inviter.Id, InviteeId: otherInvitee.Id, RewardType: model.InvitationRewardTypeFirstTopUp,
		RewardQuota: 100, TriggerTopUpId: topUp.Id, TriggerTradeNo: topUp.TradeNo,
		Status: model.InvitationRewardStatusSettled,
	}).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "inconsistent-reward-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "inconsistent-reward-refund", Kind: model.PromotionRefundKindFull,
		TopUpId: topUp.Id, UserId: principal.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		Reason: "Provider refund received.",
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	requireListedResponsibilityIntegrityFailure(t, refundCase, "linked invitation reward does not match")
}

func TestListAdminPromotionRefundCasesFlagsHardDeletedResponsibleUser(t *testing.T) {
	truncate(t)
	user := &model.User{Username: "deleted-responsible-user", AffCode: "deletedresponsibleuser", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "deleted-responsible-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "deleted-responsible-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "deleted-responsible-refund", Kind: model.PromotionRefundKindFull,
		TopUpId: topUp.Id, UserId: user.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		Reason: "Provider refund received.",
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	require.NoError(t, model.DB.Unscoped().Delete(user).Error)

	requireListedResponsibilityIntegrityFailure(t, refundCase, "principal responsible user no longer exists")
}

func TestListAdminPromotionRefundCasesOmitsInconsistentCommissionRecipient(t *testing.T) {
	truncate(t)
	principal := &model.User{Username: "refund-link-principal", AffCode: "refundlinkprincipal", Status: common.UserStatusEnabled}
	unrelated := &model.User{Username: "refund-link-unrelated", AffCode: "refundlinkunrelated", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(principal).Error)
	require.NoError(t, model.DB.Create(unrelated).Error)
	topUp := &model.TopUp{
		UserId: principal.Id, Purpose: model.TopUpPurposeAPIBalance, TradeNo: "inconsistent-responsible-order",
		PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	rebate := &model.InvitationRebate{
		InviterId: unrelated.Id, InviteeId: principal.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo,
	}
	require.NoError(t, model.DB.Create(rebate).Error)
	ledger := &model.PromotionCommissionLedger{
		UserId: unrelated.Id, InviteeId: unrelated.Id,
		SourceType: model.PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo, Currency: "CNY", NetAmountCents: 120, QuotaEquivalent: 600,
		Status: model.PromotionCommissionStatusReversed,
	}
	require.NoError(t, model.DB.Create(ledger).Error)
	require.NoError(t, model.DB.Create(&model.PromotionRefundCase{
		EventKey: "inconsistent-responsible-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "inconsistent-responsible-refund", Kind: model.PromotionRefundKindFull,
		Currency: "CNY", UserId: principal.Id, TopUpId: topUp.Id,
		InvitationRebateId: rebate.Id, CommissionLedgerId: ledger.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}).Error)

	refundCases, total, err := ListAdminPromotionRefundCases(&common.PageInfo{Page: 1, PageSize: 20}, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, refundCases, 1)
	require.Len(t, refundCases[0].ResponsibleUsers, 2)
	assert.Equal(t, principal.Id, refundCases[0].ResponsibleUsers[0].UserId)
	assert.True(t, refundCases[0].ResponsibleUsers[0].IsTopUpUser)
	assert.False(t, refundCases[0].ResponsibleUsers[0].IsCommissionRecipient)
	assert.Equal(t, unrelated.Id, refundCases[0].ResponsibleUsers[1].UserId)
	assert.True(t, refundCases[0].ResponsibleUsers[1].IsRebateRecipient)
	assert.False(t, refundCases[0].ResponsibleUsers[1].IsCommissionRecipient)
	assert.Contains(t, refundCases[0].ResponsibilityIntegrityError, "commission ledger link does not match")
}

func TestListAdminPromotionRefundCasesRejectsInvalidPagination(t *testing.T) {
	tests := []struct {
		name     string
		pageInfo *common.PageInfo
	}{
		{name: "zero page", pageInfo: &common.PageInfo{Page: 0, PageSize: 20}},
		{name: "negative page", pageInfo: &common.PageInfo{Page: -1, PageSize: 20}},
		{name: "zero page size", pageInfo: &common.PageInfo{Page: 1, PageSize: 0}},
		{name: "negative page size", pageInfo: &common.PageInfo{Page: 1, PageSize: -1}},
		{name: "oversized page", pageInfo: &common.PageInfo{Page: 1, PageSize: 101}},
		{name: "overflowing offset", pageInfo: &common.PageInfo{Page: int(^uint(0) >> 1), PageSize: 100}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ListAdminPromotionRefundCases(test.pageInfo, "")
			require.Error(t, err)
		})
	}
}

func TestApplyAdminPromotionRefundActionRetriesWalletIdempotentlyAndReleasesHold(t *testing.T) {
	truncate(t)
	user, refundCase, obligation := createRefundRecoveryFixture(t, "refund-admin-wallet", 600, 500)

	firstRequest := AdminPromotionRefundActionRequest{
		IdempotencyKey: "wallet-debit-1", Action: model.PromotionRefundActionRetryWalletDebit,
		ObligationId: obligation.Id, Amount: 300, Remark: "retry after the user recharged",
	}
	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, firstRequest)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, int64(300), result.Actions[0].Amount)
	assert.NotZero(t, result.Actions[0].FundTransactionId)
	var transaction model.PromotionFundTransaction
	require.NoError(t, model.DB.Preload("Legs").First(&transaction, result.Actions[0].FundTransactionId).Error)
	require.Len(t, transaction.Legs, 2)
	legAmounts := map[string]int64{}
	for _, leg := range transaction.Legs {
		legAmounts[leg.Account] = leg.Amount
	}
	assert.Equal(t, int64(-300), legAmounts[model.PromotionFundAccountAPIBalance])
	assert.Equal(t, int64(-300), legAmounts[model.PromotionFundAccountRefundDebt])

	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, 300, user.Quota)
	assert.Equal(t, int64(200), user.RefundDebtQuota)
	assert.True(t, user.RefundHold)
	require.NoError(t, model.DB.First(obligation, obligation.Id).Error)
	assert.Equal(t, int64(300), obligation.RecoveredAmount)
	assert.Equal(t, model.PromotionRefundObligationStatusOpen, obligation.Status)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, firstRequest)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, 300, user.Quota)
	assert.Equal(t, int64(200), user.RefundDebtQuota)
	var actionCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundAction{}).Count(&actionCount).Error)
	assert.Equal(t, int64(1), actionCount)
	var fundCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).Count(&fundCount).Error)
	assert.Equal(t, int64(1), fundCount)

	conflictingRequest := firstRequest
	conflictingRequest.Amount = 200
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, conflictingRequest)
	require.ErrorContains(t, err, "idempotency key")

	result, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "wallet-debit-2", Action: model.PromotionRefundActionRetryWalletDebit,
		ObligationId: obligation.Id, Amount: 200, Remark: "recover the remaining quota debt",
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.First(obligation, obligation.Id).Error)
	assert.Equal(t, model.PromotionRefundObligationStatusRecovered, obligation.Status)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Zero(t, user.RefundDebtQuota)
	assert.True(t, user.RefundHold, "the hold requires an explicit release action")
	assert.Equal(t, model.PromotionRefundCaseStatusPendingReview, result.Status)

	result, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-hold", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "all recovery obligations are closed",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionRefundCaseStatusResolved, result.Status)
	assert.Equal(t, 91, result.ReviewerId)
	require.Len(t, result.Actions, 3)
	assert.Zero(t, result.Actions[2].FundTransactionId, "releasing a hold does not fabricate a money movement")
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.False(t, user.RefundHold)
}

func TestApplyAdminPromotionRefundActionWalletFailureIsAtomic(t *testing.T) {
	truncate(t)
	user, refundCase, obligation := createRefundRecoveryFixture(t, "refund-admin-wallet-fail", 100, 300)

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "wallet-too-small", Action: model.PromotionRefundActionRetryWalletDebit,
		ObligationId: obligation.Id, Amount: 200,
	})
	require.ErrorContains(t, err, "insufficient")
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Equal(t, int64(300), user.RefundDebtQuota)
	require.NoError(t, model.DB.First(obligation, obligation.Id).Error)
	assert.Zero(t, obligation.RecoveredAmount)
	var actionCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundAction{}).Count(&actionCount).Error)
	assert.Zero(t, actionCount)
	var fundCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).Count(&fundCount).Error)
	assert.Zero(t, fundCount)
}

func TestApplyAdminPromotionRefundActionRecordsExternalQuotaRepayment(t *testing.T) {
	truncate(t)
	user, refundCase, obligation := createRefundRecoveryFixture(t, "refund-admin-external", 50, 300)

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "external-missing-ref", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: obligation.Id, Amount: 100,
	})
	require.ErrorContains(t, err, "external reference")

	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "external-repayment", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: obligation.Id, Amount: 100, ExternalRef: "bank-receipt-2026-001",
		Remark: "payment confirmed by finance",
	})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, "bank-receipt-2026-001", result.Actions[0].ExternalRef)
	var receipt model.PromotionRefundRecoveryReceipt
	require.NoError(t, model.DB.Where("action_key = ?", result.Actions[0].ActionKey).First(&receipt).Error)
	assert.Equal(t, "bank-receipt-2026-001", receipt.ExternalRef)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, 50, user.Quota, "external repayment must not debit the API wallet")
	assert.Equal(t, int64(200), user.RefundDebtQuota)
	require.NoError(t, model.DB.First(obligation, obligation.Id).Error)
	assert.Equal(t, int64(100), obligation.RecoveredAmount)
	retried, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "external-repayment", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: obligation.Id, Amount: 100, ExternalRef: "BANK-RECEIPT-2026-001",
		Remark: "payment confirmed by finance",
	})
	require.NoError(t, err)
	require.Len(t, retried.Actions, 1)
}

func TestApplyAdminPromotionRefundActionRecoversPaidCommission(t *testing.T) {
	truncate(t)
	user, refundCase, _ := createRefundRecoveryFixture(t, "refund-admin-commission", 100, 0)
	obligation := &model.PromotionRefundObligation{
		ObligationKey: "refund-admin-commission-obligation", RefundCaseId: refundCase.Id, UserId: user.Id,
		Account: model.PromotionFundAccountRefundDebt, Asset: model.PromotionFundAssetCash,
		Currency: "CNY", Amount: 880, SourceType: "promotion_commission_ledgers", SourceId: 501,
	}
	require.NoError(t, model.DB.Create(obligation).Error)

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "wrong-cash-action", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: obligation.Id, Amount: 80, ExternalRef: "cash-receipt-wrong",
	})
	require.ErrorContains(t, err, "quota obligation")

	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "commission-recovered", Action: model.PromotionRefundActionRecoverPaidCommission,
		ObligationId: obligation.Id, Amount: 880, ExternalRef: "cash-receipt-001",
		Remark: "referrer returned the paid commission",
	})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, model.PromotionFundAssetCash, result.Actions[0].Asset)
	assert.Equal(t, "CNY", result.Actions[0].Currency)
	require.NoError(t, model.DB.First(obligation, obligation.Id).Error)
	assert.Equal(t, model.PromotionRefundObligationStatusRecovered, obligation.Status)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.RefundDebtQuota)
	assert.True(t, user.RefundHold)
}

func TestApplyAdminPromotionRefundActionWaiveRequiresRootAndClosesDebt(t *testing.T) {
	truncate(t)
	user, refundCase, obligation := createRefundRecoveryFixture(t, "refund-admin-waive", 10, 250)
	request := AdminPromotionRefundActionRequest{
		IdempotencyKey: "waive-debt", Action: model.PromotionRefundActionWaive,
		ObligationId: obligation.Id, Amount: 250, Remark: "approved loss write-off",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, request)
	require.ErrorContains(t, err, "only root")
	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, request)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, model.PromotionRefundActionWaive, result.Actions[0].Action)
	require.NoError(t, model.DB.First(obligation, obligation.Id).Error)
	assert.Equal(t, int64(250), obligation.WaivedAmount)
	assert.Equal(t, model.PromotionRefundObligationStatusWaived, obligation.Status)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, user.RefundDebtQuota)
	assert.True(t, user.RefundHold)
}

func TestApplyAdminPromotionRefundActionRootReviewBarrierRequiresCaseWaiver(t *testing.T) {
	truncate(t)
	user, refundCase, _ := createRefundRecoveryFixture(t, "refund-admin-root-review", 100, 0)
	refundCase.RequiresRootReview = true
	require.NoError(t, model.DB.Model(refundCase).Update("requires_root_review", true).Error)

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "blocked-release", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "ordinary review cannot clear an unknown recovery amount",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "requires a root review waiver")

	caseWaiver := AdminPromotionRefundActionRequest{
		IdempotencyKey: "root-review-waiver", Action: model.PromotionRefundActionWaive,
		Remark:                            "root accepted the unquantifiable legacy recovery risk",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, caseWaiver)
	require.ErrorContains(t, err, "only root")
	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, caseWaiver)
	require.NoError(t, err)
	assert.False(t, result.RequiresRootReview)
	assert.Equal(t, model.PromotionRefundCaseStatusPendingReview, result.Status)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, model.PromotionRefundActionWaive, result.Actions[0].Action)
	assert.Zero(t, result.Actions[0].ObligationId)
	assert.Zero(t, result.Actions[0].Amount)
	assert.Zero(t, result.Actions[0].FundTransactionId)
	var fundCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).Count(&fundCount).Error)
	assert.Zero(t, fundCount, "case-level review decisions must not fabricate zero-value fund transactions")
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.True(t, user.RefundHold, "root review waiver must not silently release the user")

	result, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-after-root-review", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "root review barrier cleared and no debt remains",
		ExpectedResponsibilityFingerprint: result.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionRefundCaseStatusResolved, result.Status)
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).Count(&fundCount).Error)
	assert.Zero(t, fundCount)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.False(t, user.RefundHold)
}

func TestApplyAdminPromotionRefundActionQuarantinesUnknownCommissionBeforeRelease(t *testing.T) {
	truncate(t)
	seedPromotionRefundAdminActors(t)
	principal := &model.User{
		Username: "refund-quarantine-principal", AffCode: "refundquarantineprincipal",
		Status: common.UserStatusEnabled, RefundHold: true,
	}
	commissionRecipient := &model.User{
		Username: "refund-quarantine-commission", AffCode: "refundquarantinecommission",
		Status: common.UserStatusEnabled, RefundHold: true,
	}
	require.NoError(t, model.DB.Create(principal).Error)
	require.NoError(t, model.DB.Create(commissionRecipient).Error)
	topUp := &model.TopUp{
		UserId: principal.Id, Purpose: model.TopUpPurposeAPIBalance,
		TradeNo: "refund-quarantine-order", PaymentProvider: model.PaymentProviderStripe,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	rebate := &model.InvitationRebate{
		InviterId: commissionRecipient.Id, InviteeId: principal.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo,
	}
	require.NoError(t, model.DB.Create(rebate).Error)
	unknown := &model.PromotionCommissionLedger{
		UserId: commissionRecipient.Id, InviteeId: principal.Id,
		SourceType: model.PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo, Cashable: true, Currency: "CNY",
		GrossAmountCents: 120, NetAmountCents: 120, Status: "legacy_unknown",
	}
	require.NoError(t, model.DB.Create(unknown).Error)
	recoverable := &model.PromotionCommissionLedger{
		UserId: commissionRecipient.Id, InviteeId: principal.Id,
		SourceType: "test", SourceId: 900002, Cashable: true, Currency: "CNY",
		GrossAmountCents: 75, NetAmountCents: 75, Status: model.PromotionCommissionStatusSettled,
	}
	require.NoError(t, model.DB.Create(recoverable).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "refund-quarantine-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "refund-quarantine-refund",
		Kind: model.PromotionRefundKindFull, UserId: principal.Id, TopUpId: topUp.Id,
		InvitationRebateId: rebate.Id, CommissionLedgerId: unknown.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	_, err := model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)

	require.NoError(t, model.DB.Model(refundCase).Update("requires_root_review", false).Error)
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-before-refund-quarantine", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "attempt release before the unknown ledger is explained",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "requires root reconciliation quarantine")
	require.NoError(t, model.DB.Model(refundCase).Update("requires_root_review", true).Error)
	require.ErrorContains(t, model.ReconcilePromotionFundTransactions(model.DB), "unknown promotion commission status")
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "complete-before-refund-quarantine", Action: model.PromotionRefundActionWaive,
		Remark:                            "attempt to complete review before the unknown ledger is explained",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "must be quarantined before completing root review")
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)
	assert.True(t, refundCase.RequiresRootReview)

	unknownStatus := unknown.Status
	quarantineRequest := AdminPromotionRefundActionRequest{
		IdempotencyKey: "quarantine-unknown-commission", Action: model.PromotionRefundActionQuarantineUnknownCommission,
		ExternalRef:        "provider-audit-legacy-commission-001",
		Remark:             "provider records confirm the legacy status cannot be reconstructed safely",
		CommissionLedgerId: unknown.Id, CommissionLedgerStatus: &unknownStatus,
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, quarantineRequest)
	require.ErrorContains(t, err, "only root")
	missingSnapshot := quarantineRequest
	missingSnapshot.IdempotencyKey = "quarantine-without-snapshot"
	missingSnapshot.CommissionLedgerId = 0
	missingSnapshot.CommissionLedgerStatus = nil
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, missingSnapshot)
	require.ErrorContains(t, err, "requires the reviewed ledger snapshot")
	missingEvidence := quarantineRequest
	missingEvidence.IdempotencyKey = "quarantine-without-evidence"
	missingEvidence.ExternalRef = ""
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, missingEvidence)
	require.ErrorContains(t, err, "evidence reference and remark are required")
	staleStatus := "older_unknown_status"
	staleSnapshot := quarantineRequest
	staleSnapshot.IdempotencyKey = "quarantine-stale-snapshot"
	staleSnapshot.CommissionLedgerStatus = &staleStatus
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, staleSnapshot)
	require.ErrorContains(t, err, "changed since it was reviewed")

	quarantined, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, quarantineRequest)
	require.NoError(t, err)
	require.Len(t, quarantined.Actions, 1)
	quarantineAction := quarantined.Actions[0]
	assert.Equal(t, model.PromotionRefundActionQuarantineUnknownCommission, quarantineAction.Action)
	assert.Equal(t, unknown.Id, quarantineAction.CommissionLedgerId)
	assert.Equal(t, "legacy_unknown", quarantineAction.CommissionLedgerStatus)
	assert.Equal(t, "provider-audit-legacy-commission-001", quarantineAction.ExternalRef)
	assert.Zero(t, quarantineAction.FundTransactionId)
	assert.False(t, quarantined.CommissionReconciliationRequired)

	retried, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, quarantineRequest)
	require.NoError(t, err)
	require.Len(t, retried.Actions, 1)
	assert.Equal(t, quarantineAction.Id, retried.Actions[0].Id)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "complete-refund-quarantine-assessment", Action: model.PromotionRefundActionWaive,
		Remark:                            "all recovery obligations and reconciliation exceptions have been assessed",
		ExpectedResponsibilityFingerprint: quarantined.ResponsibilityFingerprint,
	})
	require.NoError(t, err)

	require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
		Where("id = ?", unknown.Id).Update("status", "").Error)
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-after-commission-status-changed", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "attempt release after the ledger changed to another unknown state",
		ExpectedResponsibilityFingerprint: quarantined.ResponsibilityFingerprint,
	})
	require.ErrorIs(t, err, model.ErrPromotionRefundResponsibilityChanged)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)

	emptyStatus := ""
	changedStatusRequest := AdminPromotionRefundActionRequest{
		IdempotencyKey: "quarantine-changed-empty-commission", Action: model.PromotionRefundActionQuarantineUnknownCommission,
		ExternalRef:        "incident-empty-commission-status",
		Remark:             "Root verified the newly observed empty legacy status",
		CommissionLedgerId: unknown.Id, CommissionLedgerStatus: &emptyStatus,
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}
	reopened, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, changedStatusRequest)
	require.NoError(t, err)
	assert.True(t, reopened.RequiresRootReview)
	assert.False(t, reopened.CommissionReconciliationRequired)
	retriedReopened, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, changedStatusRequest)
	require.NoError(t, err)
	assert.True(t, retriedReopened.RequiresRootReview)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "complete-changed-commission-assessment", Action: model.PromotionRefundActionWaive,
		Remark:                            "the changed commission status has also been assessed",
		ExpectedResponsibilityFingerprint: reopened.ResponsibilityFingerprint,
	})
	require.NoError(t, err)

	require.NoError(t, model.ReconcilePromotionFundTransactions(model.DB))
	var storedUnknown model.PromotionCommissionLedger
	require.NoError(t, model.DB.First(&storedUnknown, unknown.Id).Error)
	assert.Empty(t, storedUnknown.Status)
	var unknownFundCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", unknown.Id).
		Count(&unknownFundCount).Error)
	assert.Zero(t, unknownFundCount)
	var recoverableFundCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", recoverable.Id).
		Count(&recoverableFundCount).Error)
	assert.Equal(t, int64(1), recoverableFundCount)

	released, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-after-refund-quarantine", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "all obligations and the reconciliation exception are closed",
		ExpectedResponsibilityFingerprint: reopened.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionRefundCaseStatusResolved, released.Status)
	for _, userId := range []int{principal.Id, commissionRecipient.Id} {
		var user model.User
		require.NoError(t, model.DB.First(&user, userId).Error)
		assert.False(t, user.RefundHold)
	}
}

func TestApplyAdminPromotionRefundActionDefinesManualObligation(t *testing.T) {
	truncate(t)
	seedPromotionRefundAdminActors(t)
	user := &model.User{
		Username: "refund-admin-manual-user", Status: common.UserStatusEnabled, Quota: 100,
	}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeAPIBalance,
		TradeNo: "refund-admin-manual-order", PaymentProvider: model.PaymentProviderStripe,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase := &model.PromotionRefundCase{
		EventKey: "refund-admin-manual-case", Provider: model.PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "refund-admin-manual-refund",
		Kind: model.PromotionRefundKindFull, UserId: user.Id, TopUpId: topUp.Id,
		Status: model.PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)
	_, err := model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)
	request := AdminPromotionRefundActionRequest{
		IdempotencyKey: "define-manual-quota-debt", Action: model.PromotionRefundActionDefineManualObligation,
		UserId: user.Id, TopUpId: topUp.Id, Asset: model.PromotionFundAssetQuota, Amount: 300,
		ExternalRef: "provider-evidence-001", Remark: "verified against the provider dashboard",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, request)
	require.ErrorContains(t, err, "only root")
	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, request)
	require.NoError(t, err)
	assert.True(t, result.RequiresRootReview, "manual assessments stay open until Root confirms responsibility discovery is complete")
	assert.Equal(t, user.Id, result.UserId)
	assert.Equal(t, topUp.Id, result.TopUpId)
	assert.Equal(t, 300, result.QuotaAmount)
	assert.Equal(t, int64(300), result.DebtCreatedQuota)
	require.Len(t, result.Obligations, 1)
	obligation := result.Obligations[0]
	assert.Equal(t, model.PromotionFundAssetQuota, obligation.Asset)
	assert.Equal(t, int64(300), obligation.Amount)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, obligation.Id, result.Actions[0].ObligationId)
	assert.NotZero(t, result.Actions[0].FundTransactionId)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, int64(300), user.RefundDebtQuota)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)
	assert.Equal(t, 300, refundCase.QuotaAmount)
	assert.Equal(t, int64(300), refundCase.DebtCreatedQuota)
	assert.True(t, user.RefundHold)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, request)
	require.NoError(t, err)
	var obligationCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error)
	assert.Equal(t, int64(1), obligationCount)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Equal(t, int64(300), user.RefundDebtQuota)
}

func TestApplyAdminPromotionRefundActionRejectsReusedExternalReceipt(t *testing.T) {
	truncate(t)
	firstUser, firstCase, firstObligation := createRefundRecoveryFixture(t, "refund-admin-receipt-first", 0, 100)
	secondUser, secondCase, secondObligation := createRefundRecoveryFixture(t, "refund-admin-receipt-second", 0, 100)
	firstRequest := AdminPromotionRefundActionRequest{
		IdempotencyKey: "receipt-first", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: firstObligation.Id, Amount: 100, ExternalRef: "bank-receipt-shared",
		Remark: "first verified allocation",
	}
	_, err := ApplyAdminPromotionRefundAction(firstCase.Id, 91, common.RoleAdminUser, firstRequest)
	require.NoError(t, err)

	_, err = ApplyAdminPromotionRefundAction(secondCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "receipt-second", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: secondObligation.Id, Amount: 100, ExternalRef: "BANK-RECEIPT-SHARED",
		Remark: "must not allocate the same receipt twice",
	})
	require.ErrorContains(t, err, "already used")
	require.NoError(t, model.DB.First(firstUser, firstUser.Id).Error)
	assert.Zero(t, firstUser.RefundDebtQuota)
	require.NoError(t, model.DB.First(secondUser, secondUser.Id).Error)
	assert.Equal(t, int64(100), secondUser.RefundDebtQuota)
	require.NoError(t, model.DB.First(secondObligation, secondObligation.Id).Error)
	assert.Zero(t, secondObligation.RecoveredAmount)
	var receiptCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundRecoveryReceipt{}).Count(&receiptCount).Error)
	assert.Equal(t, int64(1), receiptCount)
}

func TestApplyAdminPromotionRefundActionRecoversManualCashObligationAndReleasesHold(t *testing.T) {
	truncate(t)
	user, topUp, refundCase := createManualRefundCaseFixture(t, "refund-admin-manual-cash")

	defined, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "define-manual-cash-debt", Action: model.PromotionRefundActionDefineManualObligation,
		UserId: user.Id, TopUpId: topUp.Id, Asset: model.PromotionFundAssetCash,
		Currency: "CNY", Amount: 500, ExternalRef: "cash-assessment-evidence-001",
		Remark:                            "provider dashboard confirms a cash recovery balance",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	require.Len(t, defined.Obligations, 1)
	obligation := defined.Obligations[0]
	assert.Equal(t, model.PromotionFundAssetCash, obligation.Asset)
	assert.Equal(t, "CNY", obligation.Currency)
	assert.Equal(t, int64(500), defined.CashDebtCreatedMinor)

	assessmentComplete, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "complete-manual-cash-assessment", Action: model.PromotionRefundActionWaive,
		Remark:                            "all responsible parties have been assessed",
		ExpectedResponsibilityFingerprint: defined.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.False(t, assessmentComplete.RequiresRootReview)

	recovered, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "record-manual-cash-repayment", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: obligation.Id, Amount: 500, ExternalRef: "cash-repayment-receipt-001",
		Remark: "cash repayment verified in the bank statement",
	})
	require.NoError(t, err)
	require.Len(t, recovered.Obligations, 1)
	assert.Equal(t, model.PromotionRefundObligationStatusRecovered, recovered.Obligations[0].Status)
	assert.Equal(t, int64(500), recovered.Obligations[0].RecoveredAmount)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.True(t, user.RefundHold)
	assert.Zero(t, user.RefundDebtQuota)

	released, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-after-manual-cash-repayment", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "cash recovery is complete",
		ExpectedResponsibilityFingerprint: assessmentComplete.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionRefundCaseStatusResolved, released.Status)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.False(t, user.RefundHold)

	var evidenceCount, repaymentCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundRecoveryReceipt{}).
		Where("purpose = ?", model.PromotionRefundReceiptPurposeManualEvidence).Count(&evidenceCount).Error)
	require.NoError(t, model.DB.Model(&model.PromotionRefundRecoveryReceipt{}).
		Where("purpose = ?", model.PromotionRefundReceiptPurposeRepayment).Count(&repaymentCount).Error)
	assert.Equal(t, int64(1), evidenceCount)
	assert.Equal(t, int64(1), repaymentCount)
}

func TestApplyAdminPromotionRefundActionRejectsReusedManualEvidence(t *testing.T) {
	truncate(t)
	firstUser, firstTopUp, firstCase := createManualRefundCaseFixture(t, "refund-admin-evidence-first")
	secondUser, secondTopUp, secondCase := createManualRefundCaseFixture(t, "refund-admin-evidence-second")

	_, err := ApplyAdminPromotionRefundAction(firstCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "manual-evidence-first", Action: model.PromotionRefundActionDefineManualObligation,
		UserId: firstUser.Id, TopUpId: firstTopUp.Id, Asset: model.PromotionFundAssetQuota, Amount: 50,
		ExternalRef: "provider-evidence-shared", Remark: "first evidence allocation",
		ExpectedResponsibilityFingerprint: firstCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	_, err = ApplyAdminPromotionRefundAction(secondCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "manual-evidence-second", Action: model.PromotionRefundActionDefineManualObligation,
		UserId: secondUser.Id, TopUpId: secondTopUp.Id, Asset: model.PromotionFundAssetQuota, Amount: 50,
		ExternalRef: "provider-evidence-shared", Remark: "must not reuse the same evidence",
		ExpectedResponsibilityFingerprint: secondCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "already used")

	var secondObligations int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundObligation{}).
		Where("refund_case_id = ?", secondCase.Id).Count(&secondObligations).Error)
	assert.Zero(t, secondObligations)
	require.NoError(t, model.DB.First(secondUser, secondUser.Id).Error)
	assert.True(t, secondUser.RefundHold, "the pending refund responsibility remains held even though no obligation was created")
	assert.Zero(t, secondUser.RefundDebtQuota)
}

func TestApplyAdminPromotionRefundActionRejectsReceiptReusedAcrossPurposes(t *testing.T) {
	truncate(t)
	user, topUp, refundCase := createManualRefundCaseFixture(t, "refund-admin-cross-purpose-receipt")
	const externalRef = "provider-reference-used-once"

	defined, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "cross-purpose-evidence", Action: model.PromotionRefundActionDefineManualObligation,
		UserId: user.Id, TopUpId: topUp.Id, Asset: model.PromotionFundAssetCash,
		Currency: "CNY", Amount: 500, ExternalRef: externalRef,
		Remark:                            "provider evidence establishes the recovery obligation",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	require.Len(t, defined.Obligations, 1)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "cross-purpose-assessment-complete", Action: model.PromotionRefundActionWaive,
		Remark:                            "all responsible parties have been assessed",
		ExpectedResponsibilityFingerprint: defined.ResponsibilityFingerprint,
	})
	require.NoError(t, err)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "cross-purpose-repayment", Action: model.PromotionRefundActionRecordExternalRepayment,
		ObligationId: defined.Obligations[0].Id, Amount: 500, ExternalRef: externalRef,
		Remark: "the evidence reference must not also prove repayment",
	})
	require.ErrorContains(t, err, "already used")

	var obligation model.PromotionRefundObligation
	require.NoError(t, model.DB.First(&obligation, defined.Obligations[0].Id).Error)
	assert.Zero(t, obligation.RecoveredAmount)
	assert.Equal(t, model.PromotionRefundObligationStatusOpen, obligation.Status)
	var receiptCount int64
	require.NoError(t, model.DB.Model(&model.PromotionRefundRecoveryReceipt{}).Count(&receiptCount).Error)
	assert.Equal(t, int64(1), receiptCount)
}

func TestApplyAdminPromotionRefundActionReleaseHoldChecksAllRecoveryBarriers(t *testing.T) {
	truncate(t)
	user, refundCase, _ := createRefundRecoveryFixture(t, "refund-admin-release", 100, 0)
	otherCase := &model.PromotionRefundCase{
		EventKey: "refund-admin-release-other", Provider: model.PaymentProviderStripe,
		TradeNo: "refund-admin-release-other-order", RefundTradeNo: "refund-admin-release-other-refund",
		Kind: model.PromotionRefundKindFull, UserId: user.Id, Status: model.PromotionRefundCaseStatusPendingReview,
		RequiresRootReview: true,
	}
	require.NoError(t, model.DB.Create(otherCase).Error)
	otherObligation := &model.PromotionRefundObligation{
		ObligationKey: "refund-admin-release-other-obligation", RefundCaseId: otherCase.Id, UserId: user.Id,
		Account: model.PromotionFundAccountRefundDebt, Asset: model.PromotionFundAssetCash,
		Currency: "CNY", Amount: 100, SourceType: "promotion_commission_ledgers", SourceId: 701,
	}
	require.NoError(t, model.DB.Create(otherObligation).Error)
	releaseRequest := AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-after-checks", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "all recovery checks passed",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, releaseRequest)
	require.ErrorContains(t, err, "open refund obligations")
	require.NoError(t, model.DB.Model(otherObligation).Updates(map[string]interface{}{
		"waived_amount": otherObligation.Amount, "status": model.PromotionRefundObligationStatusWaived,
	}).Error)
	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, releaseRequest)
	require.ErrorContains(t, err, "another pending refund case")
	require.NoError(t, model.DB.Model(otherCase).Update("status", model.PromotionRefundCaseStatusResolved).Error)

	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, releaseRequest)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionRefundCaseStatusResolved, result.Status)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.False(t, user.RefundHold)
}

func TestApplyAdminPromotionRefundActionReleaseHoldRequiresZeroQuotaDebt(t *testing.T) {
	truncate(t)
	user, refundCase, obligation := createRefundRecoveryFixture(t, "refund-admin-release-debt", 100, 100)
	require.NoError(t, model.DB.Model(obligation).Updates(map[string]interface{}{
		"recovered_amount": obligation.Amount, "status": model.PromotionRefundObligationStatusRecovered,
	}).Error)

	_, err := ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey: "release-with-debt", Action: model.PromotionRefundActionReleaseHold,
		Remark:                            "attempted release",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "still has refund quota debt")
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.True(t, user.RefundHold)
	assert.Equal(t, int64(100), user.RefundDebtQuota)
}

func TestApplyAdminPromotionRefundActionRevokesLinkedSubscription(t *testing.T) {
	truncate(t)
	seedPromotionRefundAdminActors(t)
	user := &model.User{
		Username: "refund-admin-subscription-user", AffCode: "refundadminsub",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Title: "Refund admin subscription", PriceAmount: 10, Currency: "CNY", Enabled: true,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1_000_000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription, err := model.CreateUserSubscriptionFromPlanTx(model.DB, user.Id, plan, "order")
	require.NoError(t, err)
	order := &model.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, UserSubscriptionId: &subscription.Id,
		Money: plan.PriceAmount, TradeNo: "refund-admin-subscription-order",
		PaymentMethod: model.PaymentMethodStripe, PaymentProvider: model.PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, CreateTime: subscription.CreatedAt, CompleteTime: subscription.CreatedAt,
	}
	require.NoError(t, model.DB.Create(order).Error)
	topUp := &model.TopUp{
		UserId: user.Id, Purpose: model.TopUpPurposeSubscription, TradeNo: order.TradeNo,
		PaymentMethod: model.PaymentMethodStripe, PaymentProvider: model.PaymentProviderStripe,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	refundCase, err := model.HandlePromotionRefund(model.PromotionRefundInput{
		Provider: model.PaymentProviderStripe, TradeNo: order.TradeNo,
		RefundTradeNo: "refund-admin-subscription-event", Kind: model.PromotionRefundKindFull,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	_, err = model.ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(refundCase, refundCase.Id).Error)

	_, err = ApplyAdminPromotionRefundAction(refundCase.Id, 91, common.RoleAdminUser, AdminPromotionRefundActionRequest{
		IdempotencyKey:     "refund-admin-subscription-role-check",
		Action:             model.PromotionRefundActionRevokeSubscription,
		UserSubscriptionId: subscription.Id, Remark: "ordinary admin must not terminate the entitlement",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "only root")

	result, err := ApplyAdminPromotionRefundAction(refundCase.Id, 1, common.RoleRootUser, AdminPromotionRefundActionRequest{
		IdempotencyKey:     "refund-admin-subscription-revoke",
		Action:             model.PromotionRefundActionRevokeSubscription,
		UserSubscriptionId: subscription.Id, Remark: "terminate entitlement linked to provider refund",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, subscription.Id, result.UserSubscriptionId)
	assert.Equal(t, "cancelled", result.SubscriptionStatus)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, model.PromotionRefundActionRevokeSubscription, result.Actions[0].Action)
}
