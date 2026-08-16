package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListAdminPromotionRefundCasesDefaultsToPending(t *testing.T) {
	truncate(t)
	pending := &model.PromotionRefundCase{
		EventKey:      "refund-admin-pending",
		Provider:      model.PaymentProviderStripe,
		TradeNo:       "refund-admin-order-pending",
		RefundTradeNo: "refund-admin-refund-pending",
		Kind:          model.PromotionRefundKindPartial,
		Status:        model.PromotionRefundCaseStatusPendingReview,
	}
	resolved := &model.PromotionRefundCase{
		EventKey:      "refund-admin-resolved",
		Provider:      model.PaymentProviderStripe,
		TradeNo:       "refund-admin-order-resolved",
		RefundTradeNo: "refund-admin-refund-resolved",
		Kind:          model.PromotionRefundKindFull,
		Status:        model.PromotionRefundCaseStatusResolved,
	}
	require.NoError(t, model.DB.Create(pending).Error)
	require.NoError(t, model.DB.Create(resolved).Error)

	pageInfo := &common.PageInfo{Page: 1, PageSize: 20}
	refundCases, total, err := ListAdminPromotionRefundCases(pageInfo, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, refundCases, 1)
	assert.Equal(t, pending.Id, refundCases[0].Id)
}

func TestResolvePromotionRefundCaseUsesPendingCASAndRequiresNote(t *testing.T) {
	truncate(t)
	refundCase := &model.PromotionRefundCase{
		EventKey:      "refund-admin-resolve",
		Provider:      model.PaymentProviderWaffo,
		TradeNo:       "refund-admin-order",
		RefundTradeNo: "refund-admin-refund",
		Kind:          model.PromotionRefundKindPartial,
		Status:        model.PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	_, err := ResolvePromotionRefundCase(refundCase.Id, 91, "   ")
	require.Error(t, err)
	resolved, err := ResolvePromotionRefundCase(refundCase.Id, 91, "provider refund verified; ledger adjusted externally")
	require.NoError(t, err)
	assert.Equal(t, model.PromotionRefundCaseStatusResolved, resolved.Status)
	assert.Equal(t, 91, resolved.ReviewerId)
	assert.Equal(t, "provider refund verified; ledger adjusted externally", resolved.ReviewNote)
	assert.NotZero(t, resolved.ResolvedAt)

	_, err = ResolvePromotionRefundCase(refundCase.Id, 92, "overwrite")
	require.Error(t, err)
	require.NoError(t, model.DB.Where("id = ?", refundCase.Id).First(refundCase).Error)
	assert.Equal(t, 91, refundCase.ReviewerId)
	assert.Equal(t, "provider refund verified; ledger adjusted externally", refundCase.ReviewNote)
}

func TestResolvePromotionRefundCaseRejectsReviewNoteOver1000Characters(t *testing.T) {
	truncate(t)
	refundCase := &model.PromotionRefundCase{
		EventKey:      "refund-admin-note-limit",
		Provider:      model.PaymentProviderStripe,
		TradeNo:       "refund-admin-note-limit-order",
		RefundTradeNo: "refund-admin-note-limit-refund",
		Kind:          model.PromotionRefundKindPartial,
		Status:        model.PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, model.DB.Create(refundCase).Error)

	_, err := ResolvePromotionRefundCase(refundCase.Id, 91, strings.Repeat("界", 1001))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1000")

	require.NoError(t, model.DB.Where("id = ?", refundCase.Id).First(refundCase).Error)
	assert.Equal(t, model.PromotionRefundCaseStatusPendingReview, refundCase.Status)
	assert.Empty(t, refundCase.ReviewNote)
}
