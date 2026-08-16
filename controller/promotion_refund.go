package controller

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func handlePromotionRefundFromWebhook(ctx context.Context, input model.PromotionRefundInput) error {
	refundCase, err := model.HandlePromotionRefund(input)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("%s 推广退款处理失败 trade_no=%s refund_trade_no=%s error=%q", input.Provider, input.TradeNo, input.RefundTradeNo, err.Error()))
		return err
	}
	if refundCase.Status == model.PromotionRefundCaseStatusPendingReview {
		logger.LogWarn(ctx, fmt.Sprintf("%s 推广退款已进入人工复核 trade_no=%s refund_trade_no=%s case_id=%d reason=%q", input.Provider, input.TradeNo, input.RefundTradeNo, refundCase.Id, refundCase.Reason))
	} else {
		logger.LogInfo(ctx, fmt.Sprintf("%s 推广退款已自动冲正 trade_no=%s refund_trade_no=%s case_id=%d", input.Provider, input.TradeNo, input.RefundTradeNo, refundCase.Id))
	}
	return nil
}
