package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePromotionRefundMarksUnknownRecoveryForRootReview(t *testing.T) {
	t.Run("missing local top-up", func(t *testing.T) {
		truncateTables(t)
		ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)
		refundCase, err := HandlePromotionRefund(PromotionRefundInput{
			Provider: PaymentProviderStripe, TradeNo: "root-review-missing-order",
			RefundTradeNo: "root-review-missing-refund", Kind: PromotionRefundKindFull,
		})
		require.NoError(t, err)
		assert.True(t, refundCase.RequiresRootReview)
		assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)
		assert.Zero(t, refundCase.UserId)
		assert.Empty(t, refundCase.Obligations)
		_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
			RefundCaseId: refundCase.Id, IdempotencyKey: "missing-order-admin-release",
			Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
			Remark:                            "ordinary review must not release an unknown case",
			ExpectedResponsibilityFingerprint: "0000000000000000000000000000000000000000",
		})
		require.ErrorContains(t, err, "no verifiable top-up")
	})

	t.Run("missing verified payment snapshot", func(t *testing.T) {
		truncateTables(t)
		ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)
		user := &User{Username: "root-review-snapshot-user", Status: common.UserStatusEnabled, Quota: 1000}
		require.NoError(t, DB.Create(user).Error)
		topUp := &TopUp{
			UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 500,
			TradeNo: "root-review-snapshot-order", PaymentMethod: PaymentMethodStripe,
			PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
		}
		require.NoError(t, DB.Create(topUp).Error)

		refundCase, err := HandlePromotionRefund(PromotionRefundInput{
			Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
			RefundTradeNo: "root-review-snapshot-refund", Kind: PromotionRefundKindFull,
		})
		require.NoError(t, err)
		assert.True(t, refundCase.RequiresRootReview)
		assert.Equal(t, user.Id, refundCase.UserId)
		require.NoError(t, DB.First(user, user.Id).Error)
		assert.True(t, user.RefundHold)
		var obligationCount int64
		require.NoError(t, DB.Model(&PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error)
		assert.Zero(t, obligationCount, "unknown recovery amounts must not become guessed obligations")
		require.Len(t, refundCase.ResponsibilityFingerprint, 40)
		_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
			RefundCaseId: refundCase.Id, IdempotencyKey: "missing-snapshot-admin-release",
			Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
			Remark:                            "ordinary review must not release an unknown amount",
			ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
		})
		require.ErrorContains(t, err, "requires a root review waiver")
	})

	t.Run("subscription payment", func(t *testing.T) {
		truncateTables(t)
		ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)
		user := &User{Username: "root-review-subscription-user", Status: common.UserStatusEnabled, Quota: 1000}
		require.NoError(t, DB.Create(user).Error)
		topUp := &TopUp{
			UserId: user.Id, Purpose: TopUpPurposeSubscription, TradeNo: "root-review-subscription-order",
			PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
			Status: common.TopUpStatusSuccess, PaidAmountVerified: true, PaidAmountMinor: 990,
			PaidCurrency: "CNY",
		}
		require.NoError(t, DB.Create(topUp).Error)

		refundCase, err := HandlePromotionRefund(PromotionRefundInput{
			Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
			RefundTradeNo: "root-review-subscription-refund", Kind: PromotionRefundKindFull,
		})
		require.NoError(t, err)
		assert.True(t, refundCase.RequiresRootReview)
		require.NoError(t, DB.First(user, user.Id).Error)
		assert.True(t, user.RefundHold)
		_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
			RefundCaseId: refundCase.Id, IdempotencyKey: "subscription-admin-release",
			Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
			Remark:                            "subscription recovery is not complete",
			ExpectedResponsibilityFingerprint: "0000000000000000000000000000000000000000",
		})
		require.ErrorContains(t, err, "subscription refund has no unique matching order")
	})
}

func TestHandlePromotionRefundDoesNotClearAnExistingRefundHold(t *testing.T) {
	truncateTables(t)
	user := &User{
		Username: "existing-refund-hold-user", Status: common.UserStatusEnabled,
		Quota: 1000, RefundHold: true,
	}
	require.NoError(t, DB.Create(user).Error)
	existingDispute := &PromotionRefundCase{
		EventKey: "existing-refund-hold-dispute", Provider: PaymentProviderStripe,
		TradeNo: "existing-refund-hold-old-order", RefundTradeNo: "existing-refund-hold-old-refund",
		Kind: PromotionRefundKindDispute, UserId: user.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(existingDispute).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 100,
		TradeNo: "existing-refund-hold-new-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
		PaidAmountVerified: true, PaidAmountMinor: 100, PaidCurrency: "CNY",
	}
	require.NoError(t, DB.Create(topUp).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: "existing-refund-hold-new-refund", Kind: PromotionRefundKindFull,
		PaidAmountMinor: 100, RefundedAmountMinor: 100, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 900, user.Quota)
	assert.True(t, user.RefundHold, "only the structured release action may clear a pre-existing hold")
}

func TestObligationWaiverRejectsStaleResponsibilityAndReplaysBeforeReconciliation(t *testing.T) {
	truncateTables(t)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	user := &User{
		Username: "waiver-fingerprint-user", AffCode: "waiver-fingerprint-user",
		Status: common.UserStatusEnabled, RefundHold: true, RefundDebtQuota: 100,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, TradeNo: "waiver-fingerprint-order",
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusFailed,
	}
	require.NoError(t, DB.Create(topUp).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "waiver-fingerprint-case", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "waiver-fingerprint-refund", Kind: PromotionRefundKindFull,
		TopUpId: topUp.Id, UserId: user.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(refundCase).Error)
	obligation := &PromotionRefundObligation{
		ObligationKey: "waiver-fingerprint-obligation", RefundCaseId: refundCase.Id, UserId: user.Id,
		Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 100,
		SourceType: "top_ups", SourceId: topUp.Id,
	}
	require.NoError(t, DB.Create(obligation).Error)
	_, err := ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(refundCase, refundCase.Id).Error)
	require.Len(t, refundCase.ResponsibilityFingerprint, 40)
	staleFingerprint := refundCase.ResponsibilityFingerprint

	require.NoError(t, DB.Model(topUp).Update("status", common.TopUpStatusExpired).Error)
	request := PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "waiver-fingerprint-action",
		Action: PromotionRefundActionWaive, ObligationId: obligation.Id, Amount: 100,
		ActorId: 1, ActorRole: common.RoleRootUser, Remark: "approved recovery write-off",
		ExpectedResponsibilityFingerprint: staleFingerprint,
	}
	_, err = ApplyPromotionRefundRecoveryAction(request)
	require.ErrorIs(t, err, ErrPromotionRefundResponsibilityChanged)
	require.NoError(t, DB.First(obligation, obligation.Id).Error)
	assert.Zero(t, obligation.WaivedAmount)

	require.NoError(t, DB.First(refundCase, refundCase.Id).Error)
	assert.NotEqual(t, staleFingerprint, refundCase.ResponsibilityFingerprint)
	request.ExpectedResponsibilityFingerprint = refundCase.ResponsibilityFingerprint
	result, err := ApplyPromotionRefundRecoveryAction(request)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	require.NoError(t, DB.First(obligation, obligation.Id).Error)
	assert.Equal(t, int64(100), obligation.WaivedAmount)
	assert.Equal(t, PromotionRefundObligationStatusWaived, obligation.Status)

	require.NoError(t, DB.Model(topUp).Update("status", common.TopUpStatusFailed).Error)
	replayed, err := ApplyPromotionRefundRecoveryAction(request)
	require.NoError(t, err)
	require.Len(t, replayed.Actions, 1)
	var actionCount int64
	require.NoError(t, DB.Model(&PromotionRefundAction{}).
		Where("refund_case_id = ? AND action = ?", refundCase.Id, PromotionRefundActionWaive).
		Count(&actionCount).Error)
	assert.Equal(t, int64(1), actionCount)
	var fundCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ? AND kind = ?", "promotion_refund_cases", refundCase.Id, "refund_waiver").
		Count(&fundCount).Error)
	assert.Equal(t, int64(1), fundCount)
}

func TestManualRefundReviewProtectsAllResponsibleUsersUntilRootCompletesAssessment(t *testing.T) {
	truncateTables(t)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)

	commissionOwner := &User{
		Username: "fresh-manual-refund-commission-owner", AffCode: "freshmanualowner",
		Status: common.UserStatusEnabled, Quota: 200,
	}
	require.NoError(t, DB.Create(commissionOwner).Error)
	topUpUser := &User{
		Username: "fresh-manual-refund-topup-user", AffCode: "freshmanualtopup",
		Status: common.UserStatusEnabled,
		Quota:  1000, InviterId: commissionOwner.Id,
	}
	require.NoError(t, DB.Create(topUpUser).Error)
	for _, userId := range []int{topUpUser.Id, commissionOwner.Id} {
		userId := userId
		t.Cleanup(func() {
			_ = DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
				"refund_hold": false, "refund_debt_quota": 0,
			}).Error
			_ = ClearUserRefundHoldFence(userId)
		})
	}

	// The payment snapshot is verified, but the old top-up has no credited
	// quota snapshot, so recovery amounts must be assessed manually.
	topUp := &TopUp{
		UserId: topUpUser.Id, Purpose: TopUpPurposeAPIBalance,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "fresh-manual-refund-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	rebate := &InvitationRebate{
		InviterId: commissionOwner.Id, InviteeId: topUpUser.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo, PaidAmountMinor: topUp.PaidAmountMinor,
		PaidCurrency: "CNY", PaidAmountVerified: true, RebateAmountMinor: 120,
		RebateCurrency: "CNY", Cashable: true,
	}
	require.NoError(t, DB.Create(rebate).Error)
	ledger := &PromotionCommissionLedger{
		UserId: commissionOwner.Id, InviteeId: topUpUser.Id,
		SourceType: PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo, Cashable: true, Currency: "CNY",
		GrossAmountCents: 120, NetAmountCents: 120, QuotaEquivalent: 60,
		Status: PromotionCommissionStatusSettled,
	}
	require.NoError(t, DB.Create(ledger).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: "fresh-manual-refund-event", Kind: PromotionRefundKindFull,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, refundCase.PayloadHash)
	assert.Zero(t, refundCase.AccountingMigrationVersion)
	assert.True(t, refundCase.RequiresRootReview)
	assert.Equal(t, topUpUser.Id, refundCase.UserId)
	assert.Equal(t, topUp.Id, refundCase.TopUpId)
	assert.Equal(t, rebate.Id, refundCase.InvitationRebateId)
	assert.Equal(t, ledger.Id, refundCase.CommissionLedgerId)
	assert.Empty(t, refundCase.Obligations, "manual review must not guess a recovery amount")

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, rebate.Id, storedCase.InvitationRebateId)
	assert.Equal(t, ledger.Id, storedCase.CommissionLedgerId)
	require.Len(t, storedCase.ResponsibilityFingerprint, 40)
	for _, user := range []*User{topUpUser, commissionOwner} {
		require.NoError(t, DB.First(user, user.Id).Error)
		assert.True(t, user.RefundHold)
	}

	result, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "fresh-manual-principal-assessment",
		Action: PromotionRefundActionDefineManualObligation, UserId: topUpUser.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 400, ActorId: 1, ActorRole: common.RoleRootUser,
		ExternalRef: "fresh-manual-principal-evidence", Remark: "verified principal responsibility",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.True(t, result.RequiresRootReview, "one assessment must not close responsibility discovery")
	require.Len(t, result.Obligations, 1)

	result, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "fresh-manual-commission-assessment",
		Action: PromotionRefundActionDefineManualObligation, UserId: commissionOwner.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 120,
		ActorId: 1, ActorRole: common.RoleRootUser, ExternalRef: "fresh-manual-commission-evidence",
		Remark:                            "verified commission responsibility",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.True(t, result.RequiresRootReview, "manual assessments stay open until Root explicitly completes them")
	require.Len(t, result.Obligations, 2)

	result, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "fresh-manual-assessment-complete",
		Action: PromotionRefundActionWaive, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "all responsible parties have been assessed",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.False(t, result.RequiresRootReview)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "fresh-manual-release-with-open-debt",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "attempt release before obligations are closed",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "open obligations")
	for _, user := range []*User{topUpUser, commissionOwner} {
		require.NoError(t, DB.First(user, user.Id).Error)
		assert.True(t, user.RefundHold)
	}
}

func TestManualRefundReviewKeepsTopUpUserHeldWhenCommissionLinkageIsInconsistent(t *testing.T) {
	truncateTables(t)

	topUpUser := &User{
		Username: "manual-refund-linkage-topup-user", AffCode: "manuallinktopup",
		Status: common.UserStatusEnabled, Quota: 1000,
	}
	otherUser := &User{
		Username: "manual-refund-linkage-other-user", AffCode: "manuallinkother",
		Status: common.UserStatusEnabled, Quota: 200,
	}
	require.NoError(t, DB.Create(topUpUser).Error)
	require.NoError(t, DB.Create(otherUser).Error)
	for _, userId := range []int{topUpUser.Id, otherUser.Id} {
		userId := userId
		t.Cleanup(func() {
			_ = DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
				"refund_hold": false, "refund_debt_quota": 0,
			}).Error
			_ = ClearUserRefundHoldFence(userId)
		})
	}
	topUp := &TopUp{
		UserId: topUpUser.Id, Purpose: TopUpPurposeAPIBalance,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "manual-refund-linkage-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	// This row claims the right top-up but the wrong invitee. It must not be
	// trusted as proof that otherUser is responsible for the refund.
	require.NoError(t, DB.Create(&InvitationRebate{
		InviterId: otherUser.Id, InviteeId: otherUser.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo, PaidAmountMinor: topUp.PaidAmountMinor,
		PaidCurrency: "CNY", PaidAmountVerified: true,
	}).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: "manual-refund-linkage-event", Kind: PromotionRefundKindFull,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.True(t, refundCase.RequiresRootReview)
	assert.Equal(t, topUpUser.Id, refundCase.UserId)
	assert.Zero(t, refundCase.InvitationRebateId)
	assert.Zero(t, refundCase.CommissionLedgerId)
	assert.Contains(t, refundCase.Reason, "linked promotion responsibility is inconsistent")

	require.NoError(t, DB.First(topUpUser, topUpUser.Id).Error)
	assert.True(t, topUpUser.RefundHold)
	require.NoError(t, DB.First(otherUser, otherUser.Id).Error)
	assert.False(t, otherUser.RefundHold, "an inconsistent linkage cannot assign responsibility to another user")
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("event_key = ?", refundCase.EventKey).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount, "linkage anomalies must remain visible to Root")
}

func TestReleasePromotionRefundHoldBlocksLinkedCommissionOwnerPendingCase(t *testing.T) {
	truncateTables(t)

	topUpUser := &User{
		Username: "refund-linked-case-topup-user", AffCode: "linkedcasetopup",
		Status: common.UserStatusEnabled, RefundHold: true,
	}
	commissionOwner := &User{
		Username: "refund-linked-case-commission-owner", AffCode: "linkedcaseowner",
		Status: common.UserStatusEnabled, RefundHold: true,
	}
	require.NoError(t, DB.Create(topUpUser).Error)
	require.NoError(t, DB.Create(commissionOwner).Error)
	for _, userId := range []int{topUpUser.Id, commissionOwner.Id} {
		userId := userId
		t.Cleanup(func() {
			_ = DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
				"refund_hold": false, "refund_debt_quota": 0,
			}).Error
			_ = ClearUserRefundHoldFence(userId)
		})
	}
	topUp := &TopUp{
		UserId: topUpUser.Id, Purpose: TopUpPurposeAPIBalance, TradeNo: "refund-linked-case-order",
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	rebate := &InvitationRebate{
		InviterId: commissionOwner.Id, InviteeId: topUpUser.Id, TopUpId: topUp.Id, TradeNo: topUp.TradeNo,
	}
	require.NoError(t, DB.Create(rebate).Error)
	ledger := &PromotionCommissionLedger{
		UserId: commissionOwner.Id, InviteeId: topUpUser.Id,
		SourceType: PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo, Currency: "CNY", NetAmountCents: 120,
		Status: PromotionCommissionStatusReversed,
	}
	require.NoError(t, DB.Create(ledger).Error)
	linkedCase := &PromotionRefundCase{
		EventKey: "refund-linked-case-pending", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "refund-linked-case-pending-event",
		Kind: PromotionRefundKindFull, TopUpId: topUp.Id, UserId: topUpUser.Id,
		InvitationRebateId: rebate.Id, CommissionLedgerId: ledger.Id,
		Status: PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, DB.Create(linkedCase).Error)
	releasableTopUp := &TopUp{
		UserId: commissionOwner.Id, Purpose: TopUpPurposeAPIBalance,
		TradeNo: "refund-linked-owner-other-order", PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(releasableTopUp).Error)
	releasableCase := &PromotionRefundCase{
		EventKey: "refund-linked-owner-releasable", Provider: PaymentProviderStripe,
		TradeNo: releasableTopUp.TradeNo, RefundTradeNo: "refund-linked-owner-other-event",
		Kind: PromotionRefundKindFull, UserId: commissionOwner.Id, TopUpId: releasableTopUp.Id,
		Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(releasableCase).Error)
	_, err := ReconcilePromotionRefundCaseResponsibility(releasableCase.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(releasableCase, releasableCase.Id).Error)
	require.Len(t, releasableCase.ResponsibilityFingerprint, 40)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: releasableCase.Id, IdempotencyKey: "refund-linked-owner-blocked-release",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "must retain a hold owned by another pending case",
		ExpectedResponsibilityFingerprint: releasableCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "another pending refund case")
	require.NoError(t, DB.First(commissionOwner, commissionOwner.Id).Error)
	assert.True(t, commissionOwner.RefundHold)
	require.NoError(t, DB.First(releasableCase, releasableCase.Id).Error)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, releasableCase.Status)

	require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("id = ?", linkedCase.Id).
		Update("status", PromotionRefundCaseStatusResolved).Error)
	result, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: releasableCase.Id, IdempotencyKey: "refund-linked-owner-successful-release",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "the linked pending case is now closed",
		ExpectedResponsibilityFingerprint: releasableCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, result.Status)
	require.NoError(t, DB.First(commissionOwner, commissionOwner.Id).Error)
	assert.False(t, commissionOwner.RefundHold)
}
