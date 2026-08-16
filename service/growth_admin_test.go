package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetGrowthAdminTestData(t *testing.T) {
	t.Helper()
	tables := []string{
		"growth_rewards",
		"growth_reward_budgets",
		"promotion_events",
		"growth_submissions",
		"growth_reward_items",
		"promotion_withdrawals",
		"users",
	}
	for _, table := range tables {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range tables {
			require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
		}
	})
}

func TestListAdminGrowthRewardItemsReturnsEffectiveRulesAndCapabilities(t *testing.T) {
	resetGrowthAdminTestData(t)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.Enabled = true
		setting.DailyCheckinEnabled = true
		setting.DailyCheckinMinRewardQuota = 100
		setting.DailyCheckinMaxRewardQuota = 300
		setting.SubmissionEnabled = true
		setting.SubmissionMinRewardQuota = 500
		setting.SubmissionMaxRewardQuota = 900
	})
	require.NoError(t, model.DB.Create([]*model.GrowthRewardItem{
		{
			Code: model.GrowthRewardItemDailyCheckin, Title: "Daily check-in",
			ItemType: model.GrowthRewardItemTypeAuto, Enabled: true,
		},
		{
			Code: model.GrowthRewardItemContentPublish, Title: "Publish content",
			ItemType: model.GrowthRewardItemTypeManual, Enabled: true,
		},
	}).Error)

	items, err := ListAdminGrowthRewardItems()
	require.NoError(t, err)
	require.Len(t, items, 2)

	daily := items[0]
	assert.Equal(t, 100, daily.EffectiveRewardQuotaMin)
	assert.Equal(t, 300, daily.EffectiveRewardQuotaMax)
	assert.False(t, daily.Capabilities.RewardOverride)
	assert.True(t, daily.EffectiveEnabled)

	content := items[1]
	assert.Equal(t, 500, content.EffectiveRewardQuotaMin)
	assert.Equal(t, 900, content.EffectiveRewardQuotaMax)
	assert.True(t, content.Capabilities.Introduction)
	assert.True(t, content.Capabilities.DailyLimit)
	assert.False(t, content.Capabilities.ActionURL)
}

func TestUpdateAdminGrowthRewardItemKeepsBuiltInIdentityAndValidatesFields(t *testing.T) {
	resetGrowthAdminTestData(t)
	item := &model.GrowthRewardItem{
		Code: model.GrowthRewardItemJoinCommunity, Title: "Join",
		ItemType: model.GrowthRewardItemTypeAuto, Enabled: true,
	}
	require.NoError(t, model.DB.Create(item).Error)

	_, err := UpdateAdminGrowthRewardItem(item.Id, AdminGrowthRewardItemUpdateRequest{
		Title:       "Join us",
		Description: "Community task",
		RewardQuota: 100,
		ActionURL:   "javascript:alert(1)",
		Enabled:     true,
	})
	require.Error(t, err)

	updated, err := UpdateAdminGrowthRewardItem(item.Id, AdminGrowthRewardItemUpdateRequest{
		Title:       "Join us",
		Description: "Community task",
		RewardQuota: 100,
		ActionURL:   "https://example.com/community",
		Enabled:     true,
	})
	require.NoError(t, err)
	assert.Equal(t, model.GrowthRewardItemJoinCommunity, updated.Code)
	assert.Equal(t, model.GrowthRewardItemTypeAuto, updated.ItemType)

	var stored model.GrowthRewardItem
	require.NoError(t, model.DB.Where("id = ?", item.Id).First(&stored).Error)
	assert.Equal(t, model.GrowthRewardItemJoinCommunity, stored.Code)
	assert.Equal(t, model.GrowthRewardItemTypeAuto, stored.ItemType)
}

func TestCreateAdminGrowthRewardItemOnlyAllowsSubmissionTypes(t *testing.T) {
	resetGrowthAdminTestData(t)

	for _, itemType := range []string{
		model.GrowthRewardItemTypeAuto,
		model.GrowthRewardItemTypeInvitation,
	} {
		_, err := CreateAdminGrowthRewardItem(AdminGrowthRewardItemCreateRequest{
			Code:        "unsupported_" + itemType,
			Title:       "Unsupported item",
			RewardQuota: 100,
			ItemType:    itemType,
			Enabled:     true,
		})
		require.Error(t, err)
	}

	for _, itemType := range []string{
		model.GrowthRewardItemTypeManual,
		model.GrowthRewardItemTypeSemiAuto,
	} {
		created, err := CreateAdminGrowthRewardItem(AdminGrowthRewardItemCreateRequest{
			Code:        "supported_" + itemType,
			Title:       "Supported item",
			RewardQuota: 100,
			ItemType:    itemType,
			Enabled:     true,
		})
		require.NoError(t, err)
		assert.Equal(t, itemType, created.ItemType)
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.GrowthRewardItem{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestListAdminGrowthSubmissionsFiltersPaginatesAndAddsRewardBounds(t *testing.T) {
	resetGrowthAdminTestData(t)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.SubmissionMinRewardQuota = 100
		setting.SubmissionMaxRewardQuota = 500
	})
	require.NoError(t, model.DB.Create(&model.GrowthRewardItem{
		Code: model.GrowthRewardItemContentPublish, Title: "Publish content",
		ItemType: model.GrowthRewardItemTypeManual, Enabled: true,
	}).Error)
	for index := 0; index < 21; index++ {
		require.NoError(t, model.DB.Create(&model.GrowthSubmission{
			UserId: index + 1, ItemCode: model.GrowthRewardItemContentPublish,
			Url: "https://example.com/proof", Status: model.GrowthSubmissionStatusPending,
		}).Error)
	}
	require.NoError(t, model.DB.Create(&model.GrowthSubmission{
		UserId: 99, ItemCode: model.GrowthRewardItemContentPublish,
		Url: "https://example.com/approved", Status: model.GrowthSubmissionStatusApproved,
	}).Error)

	rows, total, err := ListAdminGrowthSubmissions(
		&common.PageInfo{Page: 2, PageSize: 20},
		model.GrowthSubmissionStatusPending,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(21), total)
	require.Len(t, rows, 1)
	assert.Equal(t, 100, rows[0].RewardQuotaMin)
	assert.Equal(t, 500, rows[0].RewardQuotaMax)
	assert.Equal(t, "Publish content", rows[0].ItemTitle)

	_, _, err = ListAdminGrowthSubmissions(&common.PageInfo{Page: 1, PageSize: 20}, "unknown")
	assert.Error(t, err)
}

func TestListAdminPromotionWithdrawalsFiltersStatus(t *testing.T) {
	resetGrowthAdminTestData(t)
	require.NoError(t, model.DB.Create([]*model.PromotionWithdrawal{
		{UserId: 1, NetAmountCents: 1000, Status: model.PromotionWithdrawalStatusPendingReview},
		{UserId: 2, NetAmountCents: 2000, Status: model.PromotionWithdrawalStatusApproved},
	}).Error)

	rows, total, err := ListAdminPromotionWithdrawals(
		&common.PageInfo{Page: 1, PageSize: 20},
		model.PromotionWithdrawalStatusApproved,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].UserId)
}

func TestApproveGrowthSubmissionRejectsRewardWhenConfiguredMaximumIsZero(t *testing.T) {
	resetGrowthAdminTestData(t)
	seedUser(t, 7001, 100)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.SubmissionMinRewardQuota = 0
		setting.SubmissionMaxRewardQuota = 0
		setting.UserDailyRewardLimitQuota = 0
		setting.SiteDailyBudgetQuota = 0
	})
	item := &model.GrowthRewardItem{
		Code: model.GrowthRewardItemContentPublish, Title: "Publish content",
		ItemType: model.GrowthRewardItemTypeManual, Enabled: true,
	}
	require.NoError(t, model.DB.Create(item).Error)
	submission := &model.GrowthSubmission{
		UserId: 7001, ItemCode: item.Code, Url: "https://example.com/proof",
		Status: model.GrowthSubmissionStatusPending,
	}
	require.NoError(t, model.DB.Create(submission).Error)

	_, err := ApproveGrowthSubmission(submission.Id, 1, GrowthReviewRequest{RewardQuota: 1})
	require.Error(t, err)

	require.NoError(t, model.DB.Where("id = ?", submission.Id).First(submission).Error)
	assert.Equal(t, model.GrowthSubmissionStatusPending, submission.Status)
	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 7001).First(&user).Error)
	assert.Equal(t, 100, user.Quota)
}
