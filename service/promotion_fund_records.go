package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// UserPromotionFundRecord is the public view of a promotion-fund journal
// entry. Internal idempotency keys, actor metadata, and free-form remarks are
// intentionally excluded from the user API.
type UserPromotionFundRecord struct {
	Kind        string                       `json:"kind"`
	Source      string                       `json:"source,omitempty"`
	ExternalRef string                       `json:"external_ref,omitempty"`
	OccurredAt  int64                        `json:"occurred_at"`
	CreatedAt   int64                        `json:"created_at"`
	Legs        []UserPromotionFundRecordLeg `json:"legs"`
}

type UserPromotionFundRecordLeg struct {
	Account      string `json:"account"`
	Asset        string `json:"asset"`
	Currency     string `json:"currency"`
	Amount       int64  `json:"amount"`
	BalanceAfter *int64 `json:"balance_after"`
}

// ListPromotionFundRecords exposes a user-safe projection of tracked account
// funding, reward, promotion, and recovery changes. API usage charges remain
// in usage logs, so this journal is intentionally not a complete wallet ledger.
func ListPromotionFundRecords(userId int, pageInfo *common.PageInfo) ([]*UserPromotionFundRecord, int64, error) {
	transactions, total, err := model.ListPromotionFundTransactions(userId, pageInfo)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*UserPromotionFundRecord, 0, len(transactions))
	for _, transaction := range transactions {
		record := &UserPromotionFundRecord{
			Kind:       transaction.Kind,
			Source:     userPromotionFundSource(transaction.Kind),
			OccurredAt: transaction.OccurredAt,
			CreatedAt:  transaction.CreatedAt,
			Legs:       make([]UserPromotionFundRecordLeg, 0, len(transaction.Legs)),
		}
		// Only withdrawal references belong to the current user. Commission
		// source/refund references may belong to an invitee or a provider and
		// must remain admin-only.
		switch transaction.Kind {
		case model.PromotionFundKindCommissionWithdrawalReserved,
			model.PromotionFundKindCommissionWithdrawalReleased,
			model.PromotionFundKindCommissionWithdrawalPaid:
			record.ExternalRef = transaction.ExternalRef
		}
		for _, leg := range transaction.Legs {
			record.Legs = append(record.Legs, UserPromotionFundRecordLeg{
				Account:      leg.Account,
				Asset:        leg.Asset,
				Currency:     leg.Currency,
				Amount:       leg.Amount,
				BalanceAfter: leg.BalanceAfter,
			})
		}
		records = append(records, record)
	}
	return records, total, nil
}

func userPromotionFundSource(kind string) string {
	switch kind {
	case "new_user_registration_reward_issued", "invitee_registration_reward_issued":
		return "registration_reward"
	case model.PromotionFundKindGrowthRewardIssued, model.PromotionFundKindGrowthRewardReversed:
		return "growth_reward"
	case model.PromotionFundKindInvitationRewardIssued, model.PromotionFundKindInvitationRewardTransferred:
		return "invitation_reward"
	case model.PromotionFundKindCommissionPendingAccrued,
		model.PromotionFundKindCommissionAvailableAccrued,
		model.PromotionFundKindCommissionSettled,
		model.PromotionFundKindCommissionTransferredToBalance,
		model.PromotionFundKindCommissionReversed:
		return "commission"
	case model.PromotionFundKindCommissionWithdrawalReserved,
		model.PromotionFundKindCommissionWithdrawalReleased,
		model.PromotionFundKindCommissionWithdrawalPaid:
		return "withdrawal"
	case "refund_debt_assessment", "refund_recovery", "refund_waiver", model.PromotionFundKindReversal:
		return "refund"
	case model.PromotionFundKindRedemptionCredited:
		return "redemption"
	case model.PromotionFundKindTopUpCredited:
		return "topup"
	case model.PromotionFundKindSubscriptionBalanceDebited:
		return "subscription"
	case model.PromotionFundKindAdminQuotaCredited,
		model.PromotionFundKindAdminQuotaDebited,
		model.PromotionFundKindAdminQuotaOverridden:
		return "admin_adjustment"
	case model.PromotionFundKindRootInitialQuotaGranted:
		return "opening_balance"
	case model.PromotionFundKindLegacyAggregate:
		return "legacy"
	default:
		return ""
	}
}

// ListAdminPromotionFundRecords retains the complete immutable journal header
// for authorized investigation and reconciliation.
func ListAdminPromotionFundRecords(userId int, pageInfo *common.PageInfo) ([]*model.PromotionFundTransaction, int64, error) {
	return model.ListPromotionFundTransactions(userId, pageInfo)
}
