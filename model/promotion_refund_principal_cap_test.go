package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualPrincipalQuotaObligationCannotExceedTopUpAfterAutomaticRecoveryAcrossCases(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "aggregate-principal-quota-user", AffCode: "aggregateprincipalquota",
		Status: common.UserStatusEnabled, Quota: 400,
	}
	require.NoError(t, DB.Create(user).Error)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	t.Cleanup(func() {
		_ = DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
			"refund_hold": false, "refund_debt_quota": 0,
		}).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "aggregate-principal-quota-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)

	first, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: "aggregate-principal-quota-refund-400", Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 400, Currency: "CNY", AmountIsCumulative: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 400, first.WalletDebitedQuota)
	assert.Zero(t, first.DebtCreatedQuota)

	second, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: "aggregate-principal-quota-refund-1000", Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY", AmountIsCumulative: true,
	})
	require.NoError(t, err)
	assert.Zero(t, second.WalletDebitedQuota)
	assert.Equal(t, int64(600), second.DebtCreatedQuota)
	require.Len(t, second.Obligations, 1)
	assert.Equal(t, int64(600), second.Obligations[0].Amount)
	assert.Equal(t, "top_ups", second.Obligations[0].SourceType)
	assert.Equal(t, topUp.Id, second.Obligations[0].SourceId)

	manualCase := &PromotionRefundCase{
		EventKey: "aggregate-principal-quota-manual-case", Provider: PaymentProviderStripe,
		TradeNo: topUp.TradeNo, RefundTradeNo: "aggregate-principal-quota-manual-refund",
		Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: 1,
		Currency: "CNY", TopUpId: topUp.Id, UserId: user.Id,
		Status: PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, DB.Create(manualCase).Error)
	_, err = ReconcilePromotionRefundCaseResponsibility(manualCase.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(manualCase, manualCase.Id).Error)
	require.Len(t, manualCase.ResponsibilityFingerprint, 40)

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: manualCase.Id, IdempotencyKey: "aggregate-principal-quota-over-cap",
		Action: PromotionRefundActionDefineManualObligation, UserId: user.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetQuota, Amount: 1, ActorId: 1, ActorRole: common.RoleRootUser,
		ExternalRef: "aggregate-principal-quota-evidence", Remark: "verified principal quota evidence",
		ExpectedResponsibilityFingerprint: manualCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "aggregate top-up refund quota exceeds the credited quota")

	var manualObligationCount int64
	require.NoError(t, DB.Model(&PromotionRefundObligation{}).
		Where("refund_case_id = ?", manualCase.Id).Count(&manualObligationCount).Error)
	assert.Zero(t, manualObligationCount)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Equal(t, int64(600), user.RefundDebtQuota)
}

func TestManualPrincipalCashObligationsCannotExceedPaidAmountAcrossCases(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "aggregate-principal-cash-user", AffCode: "aggregateprincipalcash",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
	ensureFinancialActorTestUser(t, 1, common.RoleRootUser)
	t.Cleanup(func() {
		_ = DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "aggregate-principal-cash-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)

	refundAmounts := []int64{600, 400, 1}
	refundCases := make([]PromotionRefundCase, 0, len(refundAmounts))
	for i, amount := range refundAmounts {
		refundCase := PromotionRefundCase{
			EventKey: "aggregate-principal-cash-case-" + string(rune('a'+i)), Provider: PaymentProviderStripe,
			TradeNo: topUp.TradeNo, RefundTradeNo: "aggregate-principal-cash-refund-" + string(rune('a'+i)),
			Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: amount,
			Currency: "CNY", TopUpId: topUp.Id, UserId: user.Id,
			Status: PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
		}
		require.NoError(t, DB.Create(&refundCase).Error)
		_, err := ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
		require.NoError(t, err)
		require.NoError(t, DB.First(&refundCase, refundCase.Id).Error)
		require.Len(t, refundCase.ResponsibilityFingerprint, 40)
		refundCases = append(refundCases, refundCase)
	}

	for i, amount := range []int64{600, 400} {
		_, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
			RefundCaseId:   refundCases[i].Id,
			IdempotencyKey: "aggregate-principal-cash-assessment-" + string(rune('a'+i)),
			Action:         PromotionRefundActionDefineManualObligation, UserId: user.Id, TopUpId: topUp.Id,
			Asset: PromotionFundAssetCash, Currency: "CNY", Amount: amount,
			ActorId: 1, ActorRole: common.RoleRootUser,
			ExternalRef:                       "aggregate-principal-cash-evidence-" + string(rune('a'+i)),
			Remark:                            "verified principal cash evidence",
			ExpectedResponsibilityFingerprint: refundCases[i].ResponsibilityFingerprint,
		})
		require.NoError(t, err)
	}

	_, err := ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCases[2].Id, IdempotencyKey: "aggregate-principal-cash-over-cap",
		Action: PromotionRefundActionDefineManualObligation, UserId: user.Id, TopUpId: topUp.Id,
		Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 1,
		ActorId: 1, ActorRole: common.RoleRootUser,
		ExternalRef: "aggregate-principal-cash-evidence-over-cap", Remark: "verified principal cash evidence",
		ExpectedResponsibilityFingerprint: refundCases[2].ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "aggregate top-up refund cash exceeds the verified paid amount")

	var obligations []PromotionRefundObligation
	require.NoError(t, DB.Where("source_type = ? AND source_id = ? AND asset = ?",
		"top_ups", topUp.Id, PromotionFundAssetCash).Order("amount DESC").Find(&obligations).Error)
	require.Len(t, obligations, 2)
	assert.Equal(t, int64(600), obligations[0].Amount)
	assert.Equal(t, int64(400), obligations[1].Amount)
	assert.Equal(t, topUp.PaidAmountMinor, obligations[0].Amount+obligations[1].Amount)
}
