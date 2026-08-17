package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHandlePromotionRefundRecognizesLegacyEventKeyAfterReferenceUpgrade(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "legacy-refund-reference-user", Status: common.UserStatusEnabled, Quota: 900}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "legacy-refund-reference-order", PaymentMethod: PaymentMethodCreem,
		PaymentProvider: PaymentProviderCreem, Status: common.TopUpStatusSuccess,
		RefundStatus: TopUpRefundStatusPartial, RefundedAmountMinor: 100, RefundedQuota: 100,
	}
	require.NoError(t, DB.Create(topUp).Error)

	legacyRefundReference := "creem-provider-refund-object"
	legacyCase := &PromotionRefundCase{
		EventKey: fmt.Sprintf("%s:%s", PaymentProviderCreem, common.Sha1([]byte(topUp.TradeNo+":"+legacyRefundReference+":"+PromotionRefundKindPartial))),
		Provider: PaymentProviderCreem, TradeNo: topUp.TradeNo, RefundTradeNo: legacyRefundReference,
		Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: 100,
		Currency: "CNY", TopUpId: topUp.Id, UserId: user.Id, QuotaAmount: 100,
		WalletDebitedQuota: 100, Status: PromotionRefundCaseStatusResolved, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(legacyCase).Error)

	result, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderCreem, TradeNo: topUp.TradeNo,
		RefundTradeNo: "creem-webhook-event", EquivalentRefundTradeNos: []string{legacyRefundReference},
		Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: 100,
		Currency: "CNY", Remark: "creem refund", AmountIsCumulative: true,
	})
	require.NoError(t, err)
	assert.Equal(t, legacyCase.Id, result.Id)

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 900, user.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	assert.Equal(t, int64(100), topUp.RefundedAmountMinor)
	var stored PromotionRefundCase
	require.NoError(t, DB.First(&stored, legacyCase.Id).Error)
	assert.Empty(t, stored.PayloadHash)
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount)
}

func TestHandlePromotionRefundCumulativeAmountAdvancesPastLegacyEventKey(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "legacy-refund-advance-user", Status: common.UserStatusEnabled, Quota: 900}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "legacy-refund-advance-order", PaymentMethod: PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess,
		RefundStatus: TopUpRefundStatusPartial, RefundedAmountMinor: 100, RefundedQuota: 100,
	}
	require.NoError(t, DB.Create(topUp).Error)

	legacyRefundReference := "stripe-refund-object"
	legacyCase := &PromotionRefundCase{
		EventKey: fmt.Sprintf("%s:%s", PaymentProviderStripe, common.Sha1([]byte(topUp.TradeNo+":"+legacyRefundReference+":"+PromotionRefundKindPartial))),
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo, RefundTradeNo: legacyRefundReference,
		Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: 100,
		Currency: "CNY", TopUpId: topUp.Id, UserId: user.Id, QuotaAmount: 100,
		WalletDebitedQuota: 100, Status: PromotionRefundCaseStatusResolved, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(legacyCase).Error)

	result, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: topUp.TradeNo,
		RefundTradeNo: "stripe-event-id", EquivalentRefundTradeNos: []string{legacyRefundReference},
		Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: 250,
		Currency: "CNY", Remark: "stripe refund", AmountIsCumulative: true,
	})
	require.NoError(t, err)
	assert.NotEqual(t, legacyCase.Id, result.Id)
	assert.Equal(t, 150, result.QuotaAmount)

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 750, user.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	assert.Equal(t, int64(250), topUp.RefundedAmountMinor)
	assert.Equal(t, 250, topUp.RefundedQuota)
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Count(&caseCount).Error)
	assert.Equal(t, int64(2), caseCount)
}

func TestHandlePromotionRefundAcceptsHashedCaseThroughVerifiedReferenceAlias(t *testing.T) {
	truncateTables(t)

	input := PromotionRefundInput{
		Provider: PaymentProviderStripe, TradeNo: "hashed-refund-alias-order",
		RefundTradeNo: "stripe-event-id", EquivalentRefundTradeNos: []string{"stripe-refund-object"},
		Kind: PromotionRefundKindPartial, PaidAmountMinor: 1000, RefundedAmountMinor: 100,
		Currency: "CNY", Remark: "stripe refund", AmountIsCumulative: true,
	}
	legacyPayloadHash, err := promotionRefundPayloadHash(input, "stripe-refund-object")
	require.NoError(t, err)
	legacyCase := &PromotionRefundCase{
		EventKey:    fmt.Sprintf("%s:%s", input.Provider, common.Sha1([]byte(input.TradeNo+":"+"stripe-refund-object"+":100"))),
		PayloadHash: legacyPayloadHash, Provider: input.Provider, TradeNo: input.TradeNo,
		RefundTradeNo: "stripe-refund-object", Kind: input.Kind, PaidAmountMinor: input.PaidAmountMinor,
		RefundedAmountMinor: input.RefundedAmountMinor, Currency: input.Currency,
		Status: PromotionRefundCaseStatusResolved, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(legacyCase).Error)

	result, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, legacyCase.Id, result.Id)
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount)
}

func TestHandlePromotionRefundCumulativeAmountAdvancesStableRefundObject(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "cumulative-refund-user",
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId:             user.Id,
		Purpose:            TopUpPurposeAPIBalance,
		CreditedQuota:      1000,
		PaidAmountMinor:    1000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		TradeNo:            "cumulative-refund-order",
		PaymentMethod:      PaymentMethodCreem,
		PaymentProvider:    PaymentProviderCreem,
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)

	input := PromotionRefundInput{
		Provider:            PaymentProviderCreem,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       "stable-refund-object",
		Kind:                PromotionRefundKindPartial,
		PaidAmountMinor:     1000,
		RefundedAmountMinor: 100,
		Currency:            "CNY",
		AmountIsCumulative:  true,
	}
	first, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, 100, first.QuotaAmount)

	input.RefundedAmountMinor = 250
	second, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.NotEqual(t, first.Id, second.Id)
	assert.Equal(t, 150, second.QuotaAmount)

	duplicate, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, second.Id, duplicate.Id)

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 750, user.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	assert.Equal(t, int64(250), topUp.RefundedAmountMinor)
	assert.Equal(t, 250, topUp.RefundedQuota)

	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).
		Where("provider = ? AND trade_no = ?", input.Provider, input.TradeNo).
		Count(&caseCount).Error)
	assert.Equal(t, int64(2), caseCount)
}

func TestHandlePromotionRefundRejectsChangedPayloadForSameEvent(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "refund-payload-conflict-user", AffCode: "refund-payload-conflict-aff",
		Status: common.UserStatusEnabled, Quota: 1000,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "refund-payload-conflict-order", PaymentMethod: PaymentMethodCreem,
		PaymentProvider: PaymentProviderCreem, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	input := PromotionRefundInput{
		Provider: PaymentProviderCreem, TradeNo: topUp.TradeNo,
		RefundTradeNo: "refund-payload-conflict-event", Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 100, Currency: "CNY",
	}

	first, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	duplicate, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, first.Id, duplicate.Id)

	input.RefundedAmountMinor = 900
	_, err = HandlePromotionRefund(input)
	require.ErrorIs(t, err, ErrPromotionRefundEventConflict)

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 900, user.Quota)
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("event_key = ?", first.EventKey).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount)
}

func TestHandlePromotionRefundIgnoresRemarkChangesForSameEvent(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "refund-remark-idempotency-user", Status: common.UserStatusEnabled, Quota: 1000}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, CreditedQuota: 1000,
		PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "refund-remark-idempotency-order", PaymentMethod: PaymentMethodCreem,
		PaymentProvider: PaymentProviderCreem, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	input := PromotionRefundInput{
		Provider: PaymentProviderCreem, TradeNo: topUp.TradeNo,
		RefundTradeNo: "refund-remark-idempotency-event", Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 100, Currency: "CNY", Remark: "first delivery",
	}

	first, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	input.Remark = "provider changed this descriptive text"
	duplicate, err := HandlePromotionRefund(input)
	require.NoError(t, err)
	assert.Equal(t, first.Id, duplicate.Id)

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 900, user.Quota)
	var caseCount int64
	require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("event_key = ?", first.EventKey).Count(&caseCount).Error)
	assert.Equal(t, int64(1), caseCount)
}

func TestDuplicateSuccessfulPaymentCallbackDoesNotReopenRecoveredRefund(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "recovered-refund-replay-user", Status: common.UserStatusEnabled, Quota: 1000}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1000, PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "recovered-refund-replay-order", PaymentMethod: "alipay",
		PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderEpay, TradeNo: topUp.TradeNo,
		RefundTradeNo: "recovered-refund-replay-event", Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 100, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	assert.NotEmpty(t, refundCase.ResponsibilityFingerprint)

	alreadyDone, err := RechargeEpay(topUp.TradeNo, topUp.PaymentMethod, VerifiedPayment{
		AmountMinor: topUp.PaidAmountMinor,
		Currency:    topUp.PaidCurrency,
	}, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, PromotionRefundCaseStatusResolved, storedCase.Status)
	assert.NotEmpty(t, storedCase.ResponsibilityFingerprint)
	assert.False(t, storedCase.RequiresRootReview)

	var storedUser User
	require.NoError(t, DB.Select("refund_hold", "refund_debt_quota").First(&storedUser, user.Id).Error)
	assert.False(t, storedUser.RefundHold)
	assert.Zero(t, storedUser.RefundDebtQuota)
}

func TestPaymentReplayDoesNotReopenResolvedStaleCumulativeRefund(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "stale-refund-replay-user", Status: common.UserStatusEnabled, Quota: 750}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(user.Id)
	})
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1000, PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		RefundStatus: TopUpRefundStatusPartial, RefundedAmountMinor: 250, RefundedQuota: 250,
		TradeNo: "stale-refund-replay-order", PaymentMethod: "alipay",
		PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderEpay, TradeNo: topUp.TradeNo,
		RefundTradeNo: "stale-refund-replay-event", Kind: PromotionRefundKindPartial,
		PaidAmountMinor: 1000, RefundedAmountMinor: 100, Currency: "CNY", AmountIsCumulative: true,
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	assert.Equal(t, "stale cumulative refund notification", refundCase.Reason)
	assert.NotEmpty(t, refundCase.ResponsibilityFingerprint)

	alreadyDone, err := RechargeEpay(topUp.TradeNo, topUp.PaymentMethod, VerifiedPayment{
		AmountMinor: topUp.PaidAmountMinor,
		Currency:    topUp.PaidCurrency,
	}, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)

	var storedCase PromotionRefundCase
	require.NoError(t, DB.First(&storedCase, refundCase.Id).Error)
	assert.Equal(t, PromotionRefundCaseStatusResolved, storedCase.Status)
	assert.NotEmpty(t, storedCase.ResponsibilityFingerprint)
	assert.False(t, storedCase.RequiresRootReview)

	var storedUser User
	require.NoError(t, DB.Select("quota", "refund_hold", "refund_debt_quota").First(&storedUser, user.Id).Error)
	assert.Equal(t, 750, storedUser.Quota)
	assert.False(t, storedUser.RefundHold)
	assert.Zero(t, storedUser.RefundDebtQuota)
}

func TestCreatePromotionRefundObligationRejectsChangedPayloadForSameKey(t *testing.T) {
	truncateTables(t)
	user := &User{
		Username: "refund-obligation-conflict-user", AffCode: "refund-obligation-conflict-aff",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "refund-obligation-conflict-case", Provider: PaymentProviderCreem,
		TradeNo: "refund-obligation-conflict-order", RefundTradeNo: "refund-obligation-conflict-event",
		Kind: PromotionRefundKindFull, UserId: user.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(refundCase).Error)

	first := &PromotionRefundObligation{
		ObligationKey: "refund-obligation-conflict-key", RefundCaseId: refundCase.Id, UserId: user.Id,
		Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota,
		Amount: 100, SourceType: "top_ups", SourceId: 10,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionRefundObligationTx(tx, first)
	}))
	conflicting := *first
	conflicting.Id = 0
	conflicting.Amount = 900

	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionRefundObligationTx(tx, &conflicting)
	})
	require.ErrorIs(t, err, ErrPromotionRefundObligationConflict)

	var stored PromotionRefundObligation
	require.NoError(t, DB.Where("obligation_key = ?", first.ObligationKey).First(&stored).Error)
	assert.Equal(t, int64(100), stored.Amount)
	var count int64
	require.NoError(t, DB.Model(&PromotionRefundObligation{}).Where("obligation_key = ?", first.ObligationKey).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
