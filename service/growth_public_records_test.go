package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserGrowthRewardItemJSONExcludesAdministrativeConfiguration(t *testing.T) {
	item := &GrowthRewardItemStatus{
		GrowthRewardItem: &model.GrowthRewardItem{
			Id: 41, Code: "content_publish", Title: "Publish content", Description: "Share a guide",
			Introduction: "Public instructions", RewardQuota: 100, ItemType: model.GrowthRewardItemTypeManual,
			ActionURL: "https://example.com/guide", ClaimPassword: "private-password", Enabled: true,
			OncePerUser: true, DailyLimit: 3, CreatedAt: 10, UpdatedAt: 20,
		},
		RewardQuota: 100, RewardQuotaMin: 50, RewardQuotaMax: 200,
		Status: "available", Claimable: true,
	}

	encoded, err := common.Marshal(ToUserGrowthRewardItem(item))
	require.NoError(t, err)
	var value map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &value))
	for _, forbidden := range []string{
		"id", "claim_password", "enabled", "once_per_user", "daily_limit", "created_at", "updated_at",
	} {
		assert.NotContains(t, value, forbidden)
	}
	assert.Equal(t, "content_publish", value["code"])
	assert.Equal(t, "https://example.com/guide", value["action_url"])
}

func TestUserGrowthRewardJSONExcludesSourceAndInternalRemark(t *testing.T) {
	reward := &model.GrowthReward{
		Id: 51, UserId: 61, ItemCode: "daily_checkin", RewardQuota: 500,
		Status: model.GrowthRewardStatusSettled, SourceId: 71, AvailableAt: 80,
		CreatedAt: 81, SettledAt: 82, Remark: "internal settlement note",
	}

	encoded, err := common.Marshal(ToUserGrowthReward(reward))
	require.NoError(t, err)
	var value map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &value))
	for _, forbidden := range []string{"id", "user_id", "source_id", "claim_key", "remark"} {
		assert.NotContains(t, value, forbidden)
	}
	assert.Equal(t, "daily_checkin", value["item_code"])
	assert.Equal(t, float64(500), value["reward_quota"])
}

func TestUserGrowthSubmissionJSONKeepsCustomerProofWithoutReviewerIdentity(t *testing.T) {
	submission := &model.GrowthSubmission{
		Id: 91, UserId: 92, ItemCode: "content_publish", Platform: "Blog",
		Url: "https://example.com/proof", Remark: "customer context",
		Status: model.GrowthSubmissionStatusApproved, ReviewerId: 93,
		ReviewNote: "Accepted", CreatedAt: 94, ReviewedAt: 95,
	}

	encoded, err := common.Marshal(ToUserGrowthSubmission(submission))
	require.NoError(t, err)
	var value map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &value))
	for _, forbidden := range []string{"id", "user_id", "reviewer_id"} {
		assert.NotContains(t, value, forbidden)
	}
	assert.Equal(t, "https://example.com/proof", value["url"])
	assert.Equal(t, "customer context", value["remark"])
	assert.Equal(t, "Accepted", value["review_note"])
}

func TestUserGrowthSummaryJSONHasNoInternalIdentityOrSnapshots(t *testing.T) {
	summary := &GrowthSummary{
		TaskRewardEarnedQuota: 100,
		AffCode:               "public-code",
		CashCommission: PromotionCommissionSummary{
			Currency: "CNY", AvailableAmountCents: 200,
		},
	}

	encoded, err := common.Marshal(summary)
	require.NoError(t, err)
	var value map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &value))
	for _, forbidden := range []string{
		"id", "user_id", "invitee_id", "reviewer_id", "actor_id", "rule_snapshot", "payment_snapshot", "remark",
	} {
		assert.NotContains(t, value, forbidden)
	}
	assert.Equal(t, "public-code", value["aff_code"])
}
