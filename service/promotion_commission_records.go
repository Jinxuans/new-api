package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// UserPromotionCommissionLedger exposes the customer's commission amounts and
// lifecycle without leaking invitee payment references, pricing snapshots, or
// operator-only notes.
type UserPromotionCommissionLedger struct {
	Currency            string `json:"currency"`
	GrossAmountCents    int64  `json:"gross_amount_cents"`
	FeeAmountCents      int64  `json:"fee_amount_cents"`
	TaxAmountCents      int64  `json:"tax_amount_cents"`
	NetAmountCents      int64  `json:"net_amount_cents"`
	QuotaEquivalent     int    `json:"quota_equivalent"`
	Status              string `json:"status"`
	AvailableAt         int64  `json:"available_at,omitempty"`
	SettledAt           int64  `json:"settled_at,omitempty"`
	WithdrawnAt         int64  `json:"withdrawn_at,omitempty"`
	TransferredAt       int64  `json:"transferred_at,omitempty"`
	ReversalAmountCents int64  `json:"reversal_amount_cents,omitempty"`
	ReversalQuota       int    `json:"reversal_quota,omitempty"`
	ReversedAt          int64  `json:"reversed_at,omitempty"`
	CreatedAt           int64  `json:"created_at"`
}

func listUserPromotionCommissionLedgers(userId int, pageInfo *common.PageInfo) ([]*UserPromotionCommissionLedger, int64, error) {
	ledgers, total, err := model.ListPromotionCommissionLedgers(userId, pageInfo)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*UserPromotionCommissionLedger, 0, len(ledgers))
	for _, ledger := range ledgers {
		records = append(records, &UserPromotionCommissionLedger{
			Currency:         ledger.Currency,
			GrossAmountCents: ledger.GrossAmountCents, FeeAmountCents: ledger.FeeAmountCents,
			TaxAmountCents: ledger.TaxAmountCents, NetAmountCents: ledger.NetAmountCents,
			QuotaEquivalent: ledger.QuotaEquivalent, Status: ledger.Status,
			AvailableAt: ledger.AvailableAt, SettledAt: ledger.SettledAt,
			WithdrawnAt: ledger.WithdrawnAt, TransferredAt: ledger.TransferredAt,
			ReversalAmountCents: ledger.ReversalAmountCents, ReversalQuota: ledger.ReversalQuota,
			ReversedAt: ledger.ReversedAt, CreatedAt: ledger.CreatedAt,
		})
	}
	return records, total, nil
}
