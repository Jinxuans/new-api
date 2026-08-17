package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPromotionEventsSerializeOnlySafeFields(t *testing.T) {
	truncate(t)
	user := &model.User{Id: 1911, Username: "promotion-event-user", AffCode: "promotion-event-user-1911", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(&model.PromotionEvent{
		EventKey: "private:event:key", UserId: user.Id, EventType: model.PromotionEventTypeCommissionReversed,
		SourceTable: "promotion_commission_ledgers", SourceId: 88, Direction: model.PromotionEventDirectionOutcome,
		CashAmountCents: -500, Currency: "CNY", Status: model.PromotionCommissionStatusReversed,
		Title: "Cash commission reversed", Remark: "private provider refund reference",
	}).Error)

	records, total, err := ListPromotionEvents(user.Id, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	encoded, err := common.Marshal(records[0])
	require.NoError(t, err)
	var value map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &value))
	for _, forbidden := range []string{"id", "event_key", "user_id", "source_table", "source_id", "remark"} {
		assert.NotContains(t, value, forbidden)
	}
	assert.Equal(t, model.PromotionEventTypeCommissionReversed, value["event_type"])
}
