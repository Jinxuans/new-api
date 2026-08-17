package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// UserPromotionWithdrawal is the customer-safe withdrawal view. Reviewer
// identities, internal notes, payout-account snapshots, and the administrator
// operation timeline remain available only through the admin endpoints.
type UserPromotionWithdrawal struct {
	Id                int    `json:"id"`
	Currency          string `json:"currency"`
	GrossAmountCents  int64  `json:"gross_amount_cents"`
	FeeAmountCents    int64  `json:"fee_amount_cents"`
	TaxAmountCents    int64  `json:"tax_amount_cents"`
	NetAmountCents    int64  `json:"net_amount_cents"`
	Status            string `json:"status"`
	PayoutMethod      string `json:"payout_method"`
	ExternalReference string `json:"trade_no,omitempty"`
	AppliedAt         int64  `json:"applied_at"`
	ReviewedAt        int64  `json:"reviewed_at,omitempty"`
	PayoutInitiatedAt int64  `json:"payout_initiated_at,omitempty"`
	PaidAt            int64  `json:"paid_at,omitempty"`
	CreatedAt         int64  `json:"created_at"`
}

func ToUserPromotionWithdrawal(withdrawal *model.PromotionWithdrawal) *UserPromotionWithdrawal {
	if withdrawal == nil {
		return nil
	}
	return &UserPromotionWithdrawal{
		Id:                withdrawal.Id,
		Currency:          withdrawal.Currency,
		GrossAmountCents:  withdrawal.GrossAmountCents,
		FeeAmountCents:    withdrawal.FeeAmountCents,
		TaxAmountCents:    withdrawal.TaxAmountCents,
		NetAmountCents:    withdrawal.NetAmountCents,
		Status:            withdrawal.Status,
		PayoutMethod:      withdrawal.PayoutMethod,
		ExternalReference: withdrawal.TradeNo,
		AppliedAt:         withdrawal.AppliedAt,
		ReviewedAt:        withdrawal.ReviewedAt,
		PayoutInitiatedAt: withdrawal.PayoutInitiatedAt,
		PaidAt:            withdrawal.PaidAt,
		CreatedAt:         withdrawal.CreatedAt,
	}
}

func listUserPromotionWithdrawals(userId int, pageInfo *common.PageInfo) ([]*UserPromotionWithdrawal, int64, error) {
	withdrawals, total, err := model.ListPromotionWithdrawals(userId, pageInfo)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*UserPromotionWithdrawal, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		records = append(records, ToUserPromotionWithdrawal(withdrawal))
	}
	return records, total, nil
}

func getUserPromotionWithdrawal(userId int, id int) (*UserPromotionWithdrawal, error) {
	withdrawal, err := model.GetPromotionWithdrawal(userId, id)
	if err != nil {
		return nil, err
	}
	return ToUserPromotionWithdrawal(withdrawal), nil
}
