package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserPromotionFundRecordsSerializeOnlySafeFields(t *testing.T) {
	truncate(t)
	user := &model.User{Id: 1901, Username: "fund-record-user", AffCode: "fund-record-user-1901", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)

	withdrawal := &model.PromotionFundTransaction{
		TransactionKey: "withdrawal:92:paid",
		Kind:           model.PromotionFundKindCommissionWithdrawalPaid,
		UserId:         user.Id,
		SourceType:     "promotion_withdrawals",
		SourceId:       92,
		SourceKey:      "promotion_withdrawals:92",
		ActorType:      "admin",
		ActorId:        77,
		ActorRef:       "admin@example.invalid",
		ExternalRef:    "USER-PAYOUT-REFERENCE",
		Remark:         "private payout note",
		OccurredAt:     200,
	}
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.CreatePromotionFundTransactionTx(tx, withdrawal, []model.PromotionFundTransactionLeg{{
			Account: model.PromotionFundAccountCommissionReserved, Asset: model.PromotionFundAssetCash,
			Currency: "CNY", Amount: -100, SourceType: "promotion_commission_ledgers", SourceId: 91,
		}})
	}))

	unsafe := &model.PromotionFundTransaction{
		TransactionKey:        "pfb:promotion_commission_ledgers:91:reversed",
		Kind:                  model.PromotionFundKindCommissionReversed,
		UserId:                user.Id,
		SourceType:            "promotion_commission_ledgers",
		SourceId:              91,
		SourceKey:             "promotion_commission_ledgers:91",
		ReversesTransactionId: withdrawal.Id,
		ActorType:             "provider",
		ActorId:               4321,
		ActorRef:              "backfill:v99:provider-internal",
		ExternalRef:           "invitee-provider-refund-reference",
		Remark:                "arbitrary internal investigation note",
		OccurredAt:            100,
	}
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.CreatePromotionFundTransactionTx(tx, unsafe, []model.PromotionFundTransactionLeg{{
			Account: model.PromotionFundAccountCommissionAvailable, Asset: model.PromotionFundAssetCash,
			Currency: "CNY", Amount: -100, SourceType: "promotion_commission_ledgers", SourceId: 91,
		}})
	}))

	records, total, err := ListPromotionFundRecords(user.Id, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, records, 2)

	byKind := make(map[string]map[string]interface{}, len(records))
	for _, record := range records {
		encoded, marshalErr := common.Marshal(record)
		require.NoError(t, marshalErr)
		var value map[string]interface{}
		require.NoError(t, common.Unmarshal(encoded, &value))
		byKind[record.Kind] = value
		for _, forbidden := range []string{
			"id", "transaction_key", "user_id", "source_type", "source_id", "source_key", "reverses_transaction_id",
			"actor_type", "actor_id", "actor_ref", "remark",
		} {
			assert.NotContains(t, value, forbidden)
		}
		legs, ok := value["legs"].([]interface{})
		require.True(t, ok)
		require.Len(t, legs, 1)
		leg, ok := legs[0].(map[string]interface{})
		require.True(t, ok)
		assert.NotContains(t, leg, "id")
		assert.NotContains(t, leg, "transaction_id")
		assert.NotContains(t, leg, "source_type")
		assert.NotContains(t, leg, "source_id")
	}
	assert.NotContains(t, byKind[model.PromotionFundKindCommissionReversed], "external_ref")
	assert.Equal(t, "USER-PAYOUT-REFERENCE", byKind[model.PromotionFundKindCommissionWithdrawalPaid]["external_ref"])

	adminRecords, adminTotal, err := ListAdminPromotionFundRecords(user.Id, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(2), adminTotal)
	require.Len(t, adminRecords, 2)
	var adminUnsafe *model.PromotionFundTransaction
	for _, record := range adminRecords {
		if record.Kind == model.PromotionFundKindCommissionReversed {
			adminUnsafe = record
			break
		}
	}
	require.NotNil(t, adminUnsafe)
	assert.Equal(t, unsafe.TransactionKey, adminUnsafe.TransactionKey)
	assert.Equal(t, unsafe.ActorId, adminUnsafe.ActorId)
	assert.Equal(t, unsafe.ActorRef, adminUnsafe.ActorRef)
	assert.Equal(t, unsafe.ExternalRef, adminUnsafe.ExternalRef)
	assert.Equal(t, unsafe.Remark, adminUnsafe.Remark)
}

func TestUserPromotionFundRecordsDerivePublicSourceOnlyFromKind(t *testing.T) {
	truncate(t)
	user := &model.User{Id: 1902, Username: "fund-record-source-user", AffCode: "fund-record-source-user-1902", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)

	testCases := []struct {
		kind   string
		source string
	}{
		{kind: "new_user_registration_reward_issued", source: "registration_reward"},
		{kind: model.PromotionFundKindGrowthRewardReversed, source: "growth_reward"},
		{kind: model.PromotionFundKindInvitationRewardTransferred, source: "invitation_reward"},
		{kind: model.PromotionFundKindCommissionReversed, source: "commission"},
		{kind: model.PromotionFundKindCommissionWithdrawalPaid, source: "withdrawal"},
		{kind: "refund_recovery", source: "refund"},
		{kind: model.PromotionFundKindRedemptionCredited, source: "redemption"},
		{kind: model.PromotionFundKindTopUpCredited, source: "topup"},
		{kind: model.PromotionFundKindSubscriptionBalanceDebited, source: "subscription"},
		{kind: model.PromotionFundKindAdminQuotaCredited, source: "admin_adjustment"},
		{kind: model.PromotionFundKindAdminQuotaDebited, source: "admin_adjustment"},
		{kind: model.PromotionFundKindAdminQuotaOverridden, source: "admin_adjustment"},
		{kind: model.PromotionFundKindRootInitialQuotaGranted, source: "opening_balance"},
		{kind: model.PromotionFundKindLegacyAggregate, source: "legacy"},
	}
	for i, testCase := range testCases {
		transaction := &model.PromotionFundTransaction{
			TransactionKey: "public-source:" + testCase.kind,
			Kind:           testCase.kind,
			UserId:         user.Id,
			SourceType:     "private_internal_source",
			SourceId:       9000 + i,
			OccurredAt:     int64(i + 1),
		}
		require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
			return model.CreatePromotionFundTransactionTx(tx, transaction, []model.PromotionFundTransactionLeg{{
				Account: model.PromotionFundAccountAPIBalance, Asset: model.PromotionFundAssetQuota,
				Amount: 1, SourceType: "private_internal_source", SourceId: 9000 + i,
			}})
		}))
	}

	records, total, err := ListPromotionFundRecords(user.Id, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(len(testCases)), total)
	require.Len(t, records, len(testCases))

	expectedByKind := make(map[string]string, len(testCases))
	for _, testCase := range testCases {
		expectedByKind[testCase.kind] = testCase.source
	}
	for _, record := range records {
		encoded, marshalErr := common.Marshal(record)
		require.NoError(t, marshalErr)
		var value map[string]interface{}
		require.NoError(t, common.Unmarshal(encoded, &value))
		assert.Equal(t, expectedByKind[record.Kind], value["source"])
		assert.NotContains(t, value, "source_type")
		assert.NotContains(t, value, "source_id")
	}
}
