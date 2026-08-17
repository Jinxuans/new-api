package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPromotionCommissionListExcludesInternalPaymentData(t *testing.T) {
	truncate(t)
	seedUser(t, 3090, 0)
	ledger := seedPromotionCommissionLedger(t, 3090, 1200, 6000)
	require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
		Where("id = ?", ledger.Id).
		Updates(map[string]interface{}{
			"source_trade_no":  "invitee-provider-order",
			"rule_snapshot":    `{"private":"rule"}`,
			"payment_snapshot": `{"private":"payment"}`,
			"remark":           "operator-only note",
		}).Error)

	records, total, err := ListPromotionCommissionLedgers(3090, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, records, 1)

	payload, err := common.Marshal(records[0])
	require.NoError(t, err)
	for _, internalField := range []string{
		`"id"`, `"user_id"`, `"source_type"`, `"source_id"`, `"source_trade_no"`,
		`"rule_snapshot"`, `"payment_snapshot"`, `"refund_trade_no"`, `"remark"`,
	} {
		assert.False(t, strings.Contains(string(payload), internalField), internalField)
	}
}
