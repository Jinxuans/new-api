package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// AdminPromotionRefundCaseInput describes a verified refund event that did
// not arrive through the normal provider webhook path. It still enters the
// same refund case, obligation, hold, recovery, and fund-journal workflow.
type AdminPromotionRefundCaseInput struct {
	IdempotencyKey      string
	TradeNo             string
	ExternalReference   string
	IntakeSource        string
	Kind                string
	RefundedAmountMinor int64
	Currency            string
	AmountIsCumulative  bool
	Remark              string
	ActorId             int
	ActorRole           int
}

// CreateAdminPromotionRefundCase lets Root report an externally verified
// refund, dispute, or missed callback without introducing a second recovery
// system. Reusing an idempotency key returns the same PromotionRefundCase;
// changing its economic payload returns ErrPromotionRefundEventConflict.
func CreateAdminPromotionRefundCase(input AdminPromotionRefundCaseInput) (*PromotionRefundCase, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.TradeNo = strings.TrimSpace(input.TradeNo)
	input.ExternalReference = strings.TrimSpace(input.ExternalReference)
	input.IntakeSource = strings.TrimSpace(input.IntakeSource)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Remark = strings.TrimSpace(input.Remark)

	if input.ActorId <= 0 || input.ActorRole != common.RoleRootUser {
		return nil, errors.New("only root can create a refund recovery case")
	}
	if input.IdempotencyKey == "" || utf8.RuneCountInString(input.IdempotencyKey) > maxPromotionRefundIdempotencyKeyLength {
		return nil, fmt.Errorf("idempotency key must contain 1 to %d characters", maxPromotionRefundIdempotencyKeyLength)
	}
	if input.TradeNo == "" || utf8.RuneCountInString(input.TradeNo) > promotionFundReferenceMaxRunes {
		return nil, errors.New("a valid local order number is required")
	}
	if input.ExternalReference == "" || utf8.RuneCountInString(input.ExternalReference) > maxPromotionRefundExternalRefLength {
		return nil, fmt.Errorf("external reference must contain 1 to %d characters", maxPromotionRefundExternalRefLength)
	}
	if input.Remark == "" || utf8.RuneCountInString(input.Remark) > maxPromotionRefundActionRemarkLength {
		return nil, fmt.Errorf("reason must contain 1 to %d characters", maxPromotionRefundActionRemarkLength)
	}
	switch input.IntakeSource {
	case PromotionRefundIntakeOfflineRefund, PromotionRefundIntakeProviderRefund,
		PromotionRefundIntakeChargeback, PromotionRefundIntakeMissedCallback:
	default:
		return nil, errors.New("invalid refund case source")
	}
	if input.IntakeSource == PromotionRefundIntakeChargeback {
		if input.Kind != "" && input.Kind != PromotionRefundKindDispute {
			return nil, errors.New("a chargeback must be recorded as a dispute")
		}
		input.Kind = PromotionRefundKindDispute
	}
	if input.Kind != PromotionRefundKindFull && input.Kind != PromotionRefundKindPartial && input.Kind != PromotionRefundKindDispute {
		return nil, errors.New("invalid promotion refund kind")
	}
	if input.Kind == PromotionRefundKindPartial && input.RefundedAmountMinor <= 0 {
		return nil, errors.New("partial refund amount must be positive")
	}
	if input.Kind != PromotionRefundKindPartial && input.AmountIsCumulative {
		return nil, errors.New("only a partial refund can use a cumulative amount")
	}
	if input.RefundedAmountMinor < 0 {
		return nil, errors.New("refunded amount cannot be negative")
	}
	if input.Currency != "" && !isISOCurrencyCode(input.Currency) {
		return nil, errors.New("invalid refund currency")
	}

	var topUp TopUp
	if err := DB.Where("trade_no = ?", input.TradeNo).First(&topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("local top-up order not found")
		}
		return nil, err
	}
	if strings.TrimSpace(topUp.PaymentProvider) == "" {
		return nil, errors.New("top-up payment provider is missing")
	}

	paidAmountMinor := int64(0)
	currency := input.Currency
	if topUp.PaidAmountVerified {
		if topUp.PaidAmountMinor <= 0 || !isISOCurrencyCode(topUp.PaidCurrency) {
			return nil, errors.New("verified top-up payment snapshot is invalid")
		}
		paidAmountMinor = topUp.PaidAmountMinor
		if currency != "" && currency != topUp.PaidCurrency {
			return nil, errors.New("refund currency does not match the payment")
		}
		currency = topUp.PaidCurrency
	}
	if input.Kind == PromotionRefundKindFull || input.Kind == PromotionRefundKindDispute {
		if paidAmountMinor > 0 {
			if input.RefundedAmountMinor > 0 && input.RefundedAmountMinor != paidAmountMinor {
				return nil, errors.New("full refund amount does not match the payment")
			}
			input.RefundedAmountMinor = paidAmountMinor
		} else if input.RefundedAmountMinor <= 0 {
			return nil, errors.New("refund amount is required when the payment snapshot is missing")
		}
	}
	if currency == "" {
		return nil, errors.New("refund currency is required")
	}

	return HandlePromotionRefund(PromotionRefundInput{
		Provider:            topUp.PaymentProvider,
		TradeNo:             topUp.TradeNo,
		RefundTradeNo:       input.ExternalReference,
		Kind:                input.Kind,
		PaidAmountMinor:     paidAmountMinor,
		RefundedAmountMinor: input.RefundedAmountMinor,
		Currency:            currency,
		Remark:              input.Remark,
		AmountIsCumulative:  input.AmountIsCumulative,
		adminIdempotencyKey: input.IdempotencyKey,
		intakeSource:        input.IntakeSource,
		initiatorId:         input.ActorId,
		initiatorRole:       input.ActorRole,
	})
}
