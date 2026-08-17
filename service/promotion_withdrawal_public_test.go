package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPromotionWithdrawalJSONKeepsResourceIDButExcludesAdminAuditData(t *testing.T) {
	withdrawal := &model.PromotionWithdrawal{
		Id: 101, UserId: 102, Currency: "CNY", GrossAmountCents: 1_000,
		NetAmountCents: 1_000, Status: model.PromotionWithdrawalStatusPaid,
		PayoutMethod: "bank", PayoutAccountSnapshot: `{"account":"private"}`,
		TradeNo: "PAYOUT-REFERENCE", ReviewerId: 103, ReviewNote: "internal operator note",
		AppliedAt: 104, ReviewedAt: 105, PayoutInitiatedAt: 106, PaidAt: 107, CreatedAt: 104,
		Operations: []*model.PromotionWithdrawalOperation{{
			Id: 108, ActorType: model.PromotionWithdrawalActorAdmin, ActorId: 103, Note: "private audit note",
		}},
	}

	encoded, err := common.Marshal(ToUserPromotionWithdrawal(withdrawal))
	require.NoError(t, err)
	var value map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &value))
	for _, forbidden := range []string{
		"user_id", "reviewer_id", "payout_account_snapshot", "review_note", "operations", "actor_type", "actor_id", "note",
	} {
		assert.NotContains(t, value, forbidden)
	}
	assert.Equal(t, float64(101), value["id"])
	assert.Equal(t, "PAYOUT-REFERENCE", value["trade_no"])
}
