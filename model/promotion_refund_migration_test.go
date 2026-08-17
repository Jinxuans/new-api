package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyPromotionRefundAccountingReopensResolvedCaseAndFreezesUser(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "legacy-refund-migration-user", Status: common.UserStatusEnabled, Quota: 400}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 500,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "legacy-refund-migration-order", PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, RefundStatus: TopUpRefundStatusFull,
		RefundedAmountMinor: 1000, RefundedQuota: 0,
	}
	require.NoError(t, DB.Create(topUp).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "legacy-refund-migration-case", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "legacy-refund-migration-refund",
		Kind: PromotionRefundKindFull, PaidAmountMinor: 1000, RefundedAmountMinor: 1000,
		Currency: "CNY", TopUpId: topUp.Id, Status: PromotionRefundCaseStatusResolved,
		ResolvedAt: common.GetTimestamp(), CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(refundCase).Error)

	require.NoError(t, MigrateLegacyPromotionRefundAccounting())

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, storedCase.Status)
	assert.True(t, storedCase.RequiresRootReview)
	assert.Equal(t, promotionRefundAccountingMigrationVersion, storedCase.AccountingMigrationVersion)
	assert.NotZero(t, storedCase.AccountingMigratedAt)
	assert.Equal(t, user.Id, storedCase.UserId)
	assert.Equal(t, 500, storedCase.QuotaAmount)
	assert.Zero(t, storedCase.WalletDebitedQuota)
	assert.Zero(t, storedCase.DebtCreatedQuota)

	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.True(t, storedUser.RefundHold)
	assert.Equal(t, 400, storedUser.Quota)
	assert.Zero(t, storedUser.RefundDebtQuota)
	held, err := IsUserRefundHeld(user.Id)
	require.NoError(t, err)
	assert.True(t, held)

	var obligationCount, fundCount int64
	require.NoError(t, DB.Model(&PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error)
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("source_type = ? AND source_id = ?", "promotion_refund_cases", refundCase.Id).Count(&fundCount).Error)
	assert.Zero(t, obligationCount)
	assert.Zero(t, fundCount)
}

func TestMigrateLegacyPromotionRefundAccountingMissingSnapshotOnlyRequiresReview(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "legacy-refund-no-snapshot-user", Status: common.UserStatusEnabled, Quota: 300}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 500,
		TradeNo: "legacy-refund-no-snapshot-order", PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, RefundStatus: TopUpRefundStatusFull,
	}
	require.NoError(t, DB.Create(topUp).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "legacy-refund-no-snapshot-case", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "legacy-refund-no-snapshot-refund",
		Kind: PromotionRefundKindFull, TopUpId: topUp.Id,
		Status: PromotionRefundCaseStatusResolved, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(refundCase).Error)

	require.NoError(t, MigrateLegacyPromotionRefundAccounting())

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, storedCase.Status)
	assert.True(t, storedCase.RequiresRootReview)
	assert.Zero(t, storedCase.QuotaAmount)
	assert.Contains(t, storedCase.Reason, "principal quota could not be verified")

	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.True(t, storedUser.RefundHold)
	assert.Equal(t, 300, storedUser.Quota)
	assert.Zero(t, storedUser.RefundDebtQuota)
}

func TestMigrateLegacyPromotionRefundAccountingRetainsSoftDeletedUserResponsibility(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "legacy-refund-deleted-user", Status: common.UserStatusEnabled, Quota: 300}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() { _ = ClearUserRefundHoldFence(user.Id) })
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 500,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "legacy-refund-deleted-user-order", PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, RefundStatus: TopUpRefundStatusFull,
		RefundedAmountMinor: 1000,
	}
	require.NoError(t, DB.Create(topUp).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "legacy-refund-deleted-user-case", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "legacy-refund-deleted-user-refund",
		Kind: PromotionRefundKindFull, TopUpId: topUp.Id,
		Status: PromotionRefundCaseStatusResolved, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(refundCase).Error)
	require.NoError(t, DB.Delete(user).Error)

	require.NoError(t, MigrateLegacyPromotionRefundAccounting())

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, user.Id, storedCase.UserId)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, storedCase.Status)
	assert.True(t, storedCase.RequiresRootReview)
	var storedUser User
	require.NoError(t, DB.Unscoped().First(&storedUser, user.Id).Error)
	assert.True(t, storedUser.DeletedAt.Valid)
	assert.True(t, storedUser.RefundHold)
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).UpdateColumn("deleted_at", nil).Error)
	held, err := IsUserRefundHeld(user.Id)
	require.NoError(t, err)
	assert.True(t, held)
}

func TestMigrateLegacyPromotionRefundAccountingIsIdempotentAndPreservesHandledCase(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "legacy-refund-handled-user", Status: common.UserStatusEnabled, Quota: 200}
	require.NoError(t, DB.Create(user).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "legacy-refund-handled-case", Provider: PaymentProviderStripe,
		TradeNo: "legacy-refund-handled-order", RefundTradeNo: "legacy-refund-handled-refund",
		Kind: PromotionRefundKindFull, UserId: user.Id,
		Status: PromotionRefundCaseStatusResolved, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(refundCase).Error)
	action := &PromotionRefundAction{
		ActionKey: "legacy-refund-handled-action", RefundCaseId: refundCase.Id,
		UserId: user.Id, Action: PromotionRefundActionWaive, ActorId: 1,
		Remark: "Root already reviewed the legacy recovery evidence",
	}
	require.NoError(t, DB.Create(action).Error)

	require.NoError(t, MigrateLegacyPromotionRefundAccounting())
	require.NoError(t, MigrateLegacyPromotionRefundAccounting())

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, PromotionRefundCaseStatusResolved, storedCase.Status)
	assert.False(t, storedCase.RequiresRootReview)
	assert.Equal(t, promotionRefundAccountingMigrationVersion, storedCase.AccountingMigrationVersion)
	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.False(t, storedUser.RefundHold)
	assert.Equal(t, 200, storedUser.Quota)

	var actionCount, obligationCount, fundCount int64
	require.NoError(t, DB.Model(&PromotionRefundAction{}).Where("refund_case_id = ?", refundCase.Id).Count(&actionCount).Error)
	require.NoError(t, DB.Model(&PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error)
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("source_type = ? AND source_id = ?", "promotion_refund_cases", refundCase.Id).Count(&fundCount).Error)
	assert.Equal(t, int64(1), actionCount)
	assert.Zero(t, obligationCount)
	assert.Zero(t, fundCount)
}

func TestMigrateLegacyPromotionRefundAccountingProtectsTopUpUserAndPaidCommissionOwner(t *testing.T) {
	truncateTables(t)

	topupUser := &User{Username: "legacy-refund-topup-user", AffCode: "legacy-refund-topup-aff", Status: common.UserStatusEnabled, Quota: 400}
	commissionOwner := &User{Username: "legacy-refund-commission-owner", AffCode: "legacy-refund-commission-aff", Status: common.UserStatusEnabled, Quota: 200}
	unrelatedUser := &User{Username: "legacy-refund-unrelated-user", AffCode: "legacy-refund-unrelated-aff", Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(topupUser).Error)
	require.NoError(t, DB.Create(commissionOwner).Error)
	require.NoError(t, DB.Create(unrelatedUser).Error)
	for _, userId := range []int{topupUser.Id, commissionOwner.Id, unrelatedUser.Id} {
		userId := userId
		t.Cleanup(func() {
			_ = DB.Model(&User{}).Where("id = ?", userId).Update("refund_hold", false).Error
			_ = ClearUserRefundHoldFence(userId)
		})
	}

	topUp := &TopUp{
		UserId: topupUser.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 500,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "legacy-multi-party-refund-order", PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, RefundStatus: TopUpRefundStatusFull,
		RefundedAmountMinor: 1000,
	}
	require.NoError(t, DB.Create(topUp).Error)
	rebate := &InvitationRebate{
		InviterId: commissionOwner.Id, InviteeId: topupUser.Id, TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo, PaidAmountMinor: topUp.PaidAmountMinor, PaidCurrency: "CNY",
		PaidAmountVerified: true, RebateAmountMinor: 120, RebateCurrency: "CNY",
		Cashable: true, Status: InvitationRebateStatusReversed,
	}
	require.NoError(t, DB.Create(rebate).Error)
	ledger := &PromotionCommissionLedger{
		UserId: commissionOwner.Id, InviteeId: topupUser.Id,
		SourceType: PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		SourceTradeNo: topUp.TradeNo, Cashable: false, Currency: "CNY",
		GrossAmountCents: 120, NetAmountCents: 120, Status: PromotionCommissionStatusReversed,
		WithdrawnAt: common.GetTimestamp(), ReversalAmountCents: 120,
	}
	require.NoError(t, DB.Create(ledger).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "legacy-multi-party-refund-case", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "legacy-multi-party-refund",
		Kind: PromotionRefundKindFull, PaidAmountMinor: 1000, RefundedAmountMinor: 1000,
		Currency: "CNY", TopUpId: topUp.Id, Status: PromotionRefundCaseStatusResolved,
		CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(refundCase).Error)

	require.NoError(t, MigrateLegacyPromotionRefundAccounting())
	require.NoError(t, MigrateLegacyPromotionRefundAccounting())

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, topupUser.Id, storedCase.UserId)
	assert.Equal(t, rebate.Id, storedCase.InvitationRebateId)
	assert.Equal(t, ledger.Id, storedCase.CommissionLedgerId)
	assert.True(t, storedCase.RequiresRootReview)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, storedCase.Status)
	for _, user := range []*User{topupUser, commissionOwner} {
		require.NoError(t, DB.First(user, user.Id).Error)
		assert.True(t, user.RefundHold)
		assert.Zero(t, user.RefundDebtQuota)
		held, err := IsUserRefundHeld(user.Id)
		require.NoError(t, err)
		assert.True(t, held)
	}
	require.NoError(t, DB.First(unrelatedUser, unrelatedUser.Id).Error)
	assert.False(t, unrelatedUser.RefundHold)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	ensureFinancialActorTestUser(t, 91, common.RoleAdminUser)

	var obligationCount, fundCount int64
	require.NoError(t, DB.Model(&PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error)
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("source_type = ? AND source_id = ?", "promotion_refund_cases", refundCase.Id).Count(&fundCount).Error)
	assert.Zero(t, obligationCount, "migration must not guess or assess a debt")
	assert.Zero(t, fundCount, "migration must not write a debt journal entry")
	changed, err := ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	require.Len(t, storedCase.ResponsibilityFingerprint, 40)

	principalAssessment := PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-principal-assessment",
		Action: PromotionRefundActionDefineManualObligation, UserId: topupUser.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetQuota, Amount: 500, ActorId: 1, ActorRole: common.RoleRootUser,
		ExternalRef: "legacy-principal-evidence", Remark: "verified principal recovery requirement",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	}
	result, err := ApplyPromotionRefundRecoveryAction(principalAssessment)
	require.NoError(t, err)
	assert.True(t, result.RequiresRootReview, "one assessment must not close review for the paid commission owner")
	require.Len(t, result.Obligations, 1)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-unrelated-assessment",
		Action: PromotionRefundActionDefineManualObligation, UserId: unrelatedUser.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 120,
		ActorId: 1, ActorRole: common.RoleRootUser, ExternalRef: "legacy-unrelated-evidence",
		Remark: "must not attach an unrelated user", ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "not responsible")
	require.NoError(t, DB.First(unrelatedUser, unrelatedUser.Id).Error)
	assert.False(t, unrelatedUser.RefundHold)
	assert.Zero(t, unrelatedUser.RefundDebtQuota)

	commissionAssessment := PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-paid-commission-assessment",
		Action: PromotionRefundActionDefineManualObligation, UserId: commissionOwner.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 120,
		ActorId: 1, ActorRole: common.RoleRootUser, ExternalRef: "legacy-paid-commission-evidence",
		Remark:                            "verified paid commission recovery requirement",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	}
	result, err = ApplyPromotionRefundRecoveryAction(commissionAssessment)
	require.NoError(t, err)
	assert.True(t, result.RequiresRootReview)
	require.Len(t, result.Obligations, 2)
	assert.Equal(t, topupUser.Id, result.UserId, "the case owner remains the refunded top-up user")

	result, err = ApplyPromotionRefundRecoveryAction(commissionAssessment)
	require.NoError(t, err)
	require.Len(t, result.Obligations, 2, "retrying one assessment must not duplicate its obligation")

	var principalObligation, commissionObligation *PromotionRefundObligation
	for _, obligation := range result.Obligations {
		switch obligation.UserId {
		case topupUser.Id:
			principalObligation = obligation
		case commissionOwner.Id:
			commissionObligation = obligation
		}
	}
	require.NotNil(t, principalObligation)
	require.NotNil(t, commissionObligation)
	assert.Equal(t, "top_ups", principalObligation.SourceType)
	assert.Equal(t, topUp.Id, principalObligation.SourceId)
	assert.Equal(t, "promotion_commission_ledgers", commissionObligation.SourceType)
	assert.Equal(t, ledger.Id, commissionObligation.SourceId)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-review-complete",
		Action: PromotionRefundActionWaive, ActorId: 1, ActorRole: common.RoleRootUser,
		Remark:                            "all legacy responsibilities have now been assessed",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-principal-waiver",
		Action: PromotionRefundActionWaive, ObligationId: principalObligation.Id, Amount: principalObligation.Amount,
		ActorId: 1, ActorRole: common.RoleRootUser, Remark: "principal recovery waived after review",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-release-too-early",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "commission recovery is not complete",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "open obligations")

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-paid-commission-recovery",
		Action: PromotionRefundActionRecoverPaidCommission, ObligationId: commissionObligation.Id,
		Amount: commissionObligation.Amount, ActorId: 91, ActorRole: common.RoleAdminUser,
		ExternalRef: "legacy-paid-commission-receipt", Remark: "paid commission returned",
	})
	require.NoError(t, err)
	result, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "legacy-release-complete",
		Action: PromotionRefundActionReleaseHold, ActorId: 91, ActorRole: common.RoleAdminUser,
		Remark:                            "all related recovery obligations are closed",
		ExpectedResponsibilityFingerprint: storedCase.ResponsibilityFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, result.Status)
	for _, user := range []*User{topupUser, commissionOwner} {
		require.NoError(t, DB.First(user, user.Id).Error)
		assert.False(t, user.RefundHold)
	}
}
