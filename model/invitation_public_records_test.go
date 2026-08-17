package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvitationPublicRecordTypesExcludeInternalReferences(t *testing.T) {
	testCases := []struct {
		name      string
		record    interface{}
		forbidden []string
	}{
		{
			name: "milestone reward",
			record: UserInvitationRewardRecord{
				InviteeName: "invitee", RewardType: InvitationRewardTypeFirstTopUp, RewardQuota: 100,
			},
			forbidden: []string{"id", "invitee_id", "trigger_top_up_id", "trigger_trade_no", "remark"},
		},
		{
			name: "rebate",
			record: UserInvitationRebateRecord{
				InviteeName: "invitee", RebateAmountMinor: 100, RebateCurrency: "CNY",
			},
			forbidden: []string{
				"id", "invitee_id", "trade_no", "payment_method", "payment_provider", "paid_amount_minor",
				"paid_currency", "paid_amount_verified", "quota_per_unit_snapshot", "freeze_days", "risk_status",
				"refund_trade_no", "remark",
			},
		},
		{
			name:      "invited user",
			record:    UserInvitationRecord{Username: "invitee"},
			forbidden: []string{"user_id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := common.Marshal(testCase.record)
			require.NoError(t, err)
			var value map[string]interface{}
			require.NoError(t, common.Unmarshal(encoded, &value))
			for _, field := range testCase.forbidden {
				assert.NotContains(t, value, field)
			}
		})
	}
}
