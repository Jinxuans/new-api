package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionRefundRevocationClosesEntitlementBeforeRelease(t *testing.T) {
	truncateTables(t)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)

	user := &User{
		Username: "subscription-refund-revoke-user", AffCode: "subrefundrevoke",
		Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Refunded Pro", PriceAmount: 10, Currency: "CNY", Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 1_000_000, UpgradeGroup: "pro",
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	order := &SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo: "subscription-refund-revoke-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusPending,
		CreateTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(order).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: order.TradeNo,
		RefundTradeNo: "subscription-refund-revoke-event", Kind: PromotionRefundKindFull,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.Zero(t, refundCase.UserId)

	require.NoError(t, CompleteSubscriptionOrder(order.TradeNo, "verified callback", PaymentProviderStripe, PaymentMethodStripe))
	require.NoError(t, DB.First(order, order.Id).Error)
	require.NotNil(t, order.UserSubscriptionId)

	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, *order.UserSubscriptionId).Error)
	assert.Equal(t, "active", subscription.Status)
	assert.Equal(t, "pro", func() string {
		var stored User
		require.NoError(t, DB.First(&stored, user.Id).Error)
		return stored.Group
	}())

	require.NoError(t, DB.First(refundCase, refundCase.Id).Error)
	assert.Equal(t, user.Id, refundCase.UserId)
	assert.NotEmpty(t, refundCase.ResponsibilityFingerprint)
	assert.True(t, refundCase.RequiresRootReview)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-review-before-revoke",
		Action: PromotionRefundActionWaive, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "attempt to complete review while entitlement is active",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "subscription")

	revokeInput := PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-entitlement-revoke",
		Action: PromotionRefundActionRevokeSubscription, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "provider refund confirmed; terminate the linked entitlement",
		UserSubscriptionId:                *order.UserSubscriptionId,
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	}
	revokedCase, err := ApplyPromotionRefundRecoveryAction(revokeInput)
	require.NoError(t, err)
	assert.NotEqual(t, refundCase.ResponsibilityFingerprint, revokedCase.ResponsibilityFingerprint)
	require.NoError(t, DB.First(&subscription, subscription.Id).Error)
	assert.Equal(t, "cancelled", subscription.Status)
	assert.Less(t, subscription.EndTime, common.GetTimestamp()+5)

	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.Equal(t, "default", storedUser.Group)
	assert.True(t, storedUser.RefundHold)

	var revokeAction PromotionRefundAction
	require.NoError(t, DB.Where("refund_case_id = ? AND action = ?", refundCase.Id, PromotionRefundActionRevokeSubscription).
		First(&revokeAction).Error)
	require.NotNil(t, revokeAction.UserSubscriptionId)
	assert.Equal(t, subscription.Id, *revokeAction.UserSubscriptionId)
	assert.Greater(t, revokeAction.SubscriptionEndTimeBefore, subscription.EndTime)

	retriedCase, err := ApplyPromotionRefundRecoveryAction(revokeInput)
	require.NoError(t, err)
	assert.Equal(t, revokedCase.ResponsibilityFingerprint, retriedCase.ResponsibilityFingerprint)

	secondKeyInput := revokeInput
	secondKeyInput.IdempotencyKey = "subscription-entitlement-revoke-second-key"
	secondKeyInput.ExpectedResponsibilityFingerprint = revokedCase.ResponsibilityFingerprint
	_, err = ApplyPromotionRefundRecoveryAction(secondKeyInput)
	require.ErrorContains(t, err, "already")

	var revokeActionCount int64
	require.NoError(t, DB.Model(&PromotionRefundAction{}).
		Where("refund_case_id = ? AND action = ?", refundCase.Id, PromotionRefundActionRevokeSubscription).
		Count(&revokeActionCount).Error)
	assert.Equal(t, int64(1), revokeActionCount)

	reviewedCase, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-review-complete",
		Action: PromotionRefundActionWaive, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "subscription entitlement recovery is fully assessed",
		ExpectedResponsibilityFingerprint: revokedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.False(t, reviewedCase.RequiresRootReview)

	resolvedCase, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-refund-release",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "subscription entitlement is terminated and recovery checks passed",
		ExpectedResponsibilityFingerprint: reviewedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, resolvedCase.Status)
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.False(t, storedUser.RefundHold)

	_, err = AdminDeleteUserSubscription(subscription.Id)
	require.NoError(t, err)
	var retainedSubscription UserSubscription
	require.NoError(t, DB.First(&retainedSubscription, subscription.Id).Error)
	assert.Equal(t, "cancelled", retainedSubscription.Status)
	assert.Equal(t, subscription.EndTime, retainedSubscription.EndTime,
		"the legacy DELETE route must retain the earlier refund revocation time")
}

func TestAdminDeleteUserSubscriptionInvalidatesWithoutDeletingOrMovingHistory(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "subscription-delete-evidence-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Evidence Retention Plan", PriceAmount: 10, Currency: "CNY", Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1_000_000,
	}
	require.NoError(t, DB.Create(plan).Error)
	subscription, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "admin")
	require.NoError(t, err)
	originalEndTime := subscription.EndTime

	_, err = AdminDeleteUserSubscription(subscription.Id)
	require.NoError(t, err)
	var invalidated UserSubscription
	require.NoError(t, DB.First(&invalidated, subscription.Id).Error)
	assert.Equal(t, "cancelled", invalidated.Status)
	assert.Positive(t, invalidated.EndTime)
	assert.LessOrEqual(t, invalidated.EndTime, originalEndTime)

	// A retry must not reinterpret or move the historical cancellation time.
	historicalEndTime := invalidated.EndTime - 1
	historicalUpdatedAt := invalidated.UpdatedAt - 1
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", invalidated.Id).
		UpdateColumns(map[string]interface{}{
			"end_time": historicalEndTime, "updated_at": historicalUpdatedAt,
		}).Error)
	_, err = AdminDeleteUserSubscription(subscription.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(&invalidated, subscription.Id).Error)
	assert.Equal(t, "cancelled", invalidated.Status)
	assert.Equal(t, historicalEndTime, invalidated.EndTime)
	assert.Equal(t, historicalUpdatedAt, invalidated.UpdatedAt)

	var retainedCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subscription.Id).Count(&retainedCount).Error)
	assert.Equal(t, int64(1), retainedCount)
}

func TestSubscriptionRefundMayKeepActiveEntitlementAfterCashRecovery(t *testing.T) {
	truncateTables(t)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)

	user := &User{Username: "subscription-refund-cash-user", AffCode: "subrefundcash", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Cash Recovery Plan", PriceAmount: 10, Currency: "CNY", Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1_000_000,
	}
	require.NoError(t, DB.Create(plan).Error)
	subscription, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "order")
	require.NoError(t, err)
	order := &SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, UserSubscriptionId: &subscription.Id,
		Money: plan.PriceAmount, TradeNo: "subscription-refund-cash-order",
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, CreateTime: subscription.CreatedAt, CompleteTime: subscription.CreatedAt,
	}
	require.NoError(t, DB.Create(order).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeSubscription, TradeNo: order.TradeNo,
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, PaidAmountVerified: true,
		PaidAmountMinor: 1000, PaidCurrency: "CNY",
	}
	require.NoError(t, DB.Create(topUp).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: order.TradeNo,
		RefundTradeNo: "subscription-refund-cash-event", Kind: PromotionRefundKindFull,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	_, err = ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(refundCase, refundCase.Id).Error)
	require.NotEmpty(t, refundCase.ResponsibilityFingerprint)

	assessedCase, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-cash-assessment",
		Action: PromotionRefundActionDefineManualObligation, UserId: user.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 1000,
		ActorId: 1, ActorRole: common.RoleRootUser,
		ExternalRef: "subscription-cash-assessment-evidence", Remark: "verified full subscription refund",
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	require.Len(t, assessedCase.Obligations, 1)

	recoveredCase, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-cash-repayment",
		Action:       PromotionRefundActionRecordExternalRepayment,
		ObligationId: assessedCase.Obligations[0].Id, Amount: 1000,
		ActorId: 91, ActorRole: common.RoleAdminUser,
		ExternalRef: "subscription-cash-repayment-receipt", Remark: "cash recovered externally",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundObligationStatusRecovered, recoveredCase.Obligations[0].Status)

	reviewedCase, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-cash-review-complete",
		Action: PromotionRefundActionWaive, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "cash recovery permits the paid subscription to remain active",
		ExpectedResponsibilityFingerprint: recoveredCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)

	resolvedCase, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-cash-release",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "cash responsibility recovered in full",
		ExpectedResponsibilityFingerprint: reviewedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, resolvedCase.Status)
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, "active", subscription.Status)
}

func TestSubscriptionRefundDispositionDatabaseKeyIsUniqueWithoutCollidingWithOtherActions(t *testing.T) {
	truncateTables(t)

	refundCase := &PromotionRefundCase{
		EventKey: "subscription-disposition-unique-case", Provider: PaymentProviderStripe,
		TradeNo: "subscription-disposition-unique-order", RefundTradeNo: "subscription-disposition-unique-refund",
		Kind: PromotionRefundKindFull, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(refundCase).Error)

	require.NoError(t, DB.Create(&PromotionRefundAction{
		ActionKey: "subscription-disposition-unrelated-one", RefundCaseId: refundCase.Id,
		Action: PromotionRefundActionWaive, ActorId: 1,
	}).Error)
	require.NoError(t, DB.Create(&PromotionRefundAction{
		ActionKey: "subscription-disposition-unrelated-two", RefundCaseId: refundCase.Id,
		Action: PromotionRefundActionReleaseHold, ActorId: 1,
	}).Error)

	subscriptionId := 71
	require.NoError(t, DB.Create(&PromotionRefundAction{
		ActionKey: "subscription-disposition-first", RefundCaseId: refundCase.Id,
		Action: PromotionRefundActionRevokeSubscription, ActorId: 1,
		UserSubscriptionId: &subscriptionId,
	}).Error)
	err := DB.Create(&PromotionRefundAction{
		ActionKey: "subscription-disposition-duplicate", RefundCaseId: refundCase.Id,
		Action: PromotionRefundActionRevokeSubscription, ActorId: 1,
		UserSubscriptionId: &subscriptionId,
	}).Error
	require.Error(t, err)

	var actionCount int64
	require.NoError(t, DB.Model(&PromotionRefundAction{}).Where("refund_case_id = ?", refundCase.Id).Count(&actionCount).Error)
	assert.Equal(t, int64(3), actionCount)
}

func TestSubscriptionRefundDispositionRollsBackEntitlementWhenActionInsertFails(t *testing.T) {
	truncateTables(t)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)

	user := &User{
		Username: "subscription-refund-rollback-user", AffCode: "subrefundrollback",
		Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Refund rollback plan", PriceAmount: 10, Currency: "CNY", Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 1_000_000, UpgradeGroup: "pro",
	}
	require.NoError(t, DB.Create(plan).Error)
	subscription, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "order")
	require.NoError(t, err)
	originalEndTime := subscription.EndTime
	order := &SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, UserSubscriptionId: &subscription.Id,
		Money: plan.PriceAmount, TradeNo: "subscription-refund-rollback-order",
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, CreateTime: subscription.CreatedAt, CompleteTime: subscription.CreatedAt,
	}
	require.NoError(t, DB.Create(order).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeSubscription, TradeNo: order.TradeNo,
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, PaidAmountVerified: true,
		PaidAmountMinor: 1000, PaidCurrency: "CNY",
	}
	require.NoError(t, DB.Create(topUp).Error)
	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: order.TradeNo,
		RefundTradeNo: "subscription-refund-rollback-event", Kind: PromotionRefundKindFull,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	_, err = ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(refundCase, refundCase.Id).Error)
	originalFingerprint := refundCase.ResponsibilityFingerprint

	const triggerName = "test_fail_subscription_refund_action"
	require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS "+triggerName).Error)
	require.NoError(t, DB.Exec("CREATE TRIGGER "+triggerName+" BEFORE INSERT ON promotion_refund_actions "+
		"WHEN NEW.action = 'revoke_subscription_entitlement' BEGIN "+
		"SELECT RAISE(ABORT, 'forced subscription action failure'); END").Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS "+triggerName).Error)
	})

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-refund-rollback-action",
		Action: PromotionRefundActionRevokeSubscription, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "the action insert failure must roll back the entitlement change",
		UserSubscriptionId:                subscription.Id,
		ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "forced subscription action failure")

	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, "active", subscription.Status)
	assert.Equal(t, originalEndTime, subscription.EndTime)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, "pro", user.Group)
	require.NoError(t, DB.First(refundCase, refundCase.Id).Error)
	assert.Equal(t, originalFingerprint, refundCase.ResponsibilityFingerprint)
	var dispositionCount int64
	require.NoError(t, DB.Model(&PromotionRefundAction{}).
		Where("refund_case_id = ? AND action = ?", refundCase.Id, PromotionRefundActionRevokeSubscription).
		Count(&dispositionCount).Error)
	assert.Zero(t, dispositionCount)
}
