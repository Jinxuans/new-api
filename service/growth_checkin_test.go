package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withGrowthSetting(t *testing.T, fn func(setting *operation_setting.GrowthSetting)) {
	t.Helper()
	setting := operation_setting.GetGrowthSetting()
	old := *setting
	t.Cleanup(func() {
		*setting = old
	})
	fn(setting)
}

func TestClaimDailyCheckinCreatesRewardAndEvent(t *testing.T) {
	truncate(t)
	seedUser(t, 3101, 100)
	require.NoError(t, model.EnsureDefaultGrowthRewardItems())
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.DailyCheckinEnabled = true
		setting.DailyCheckinMinRewardQuota = 200
		setting.DailyCheckinMaxRewardQuota = 200
		setting.UserDailyRewardLimitQuota = 0
		setting.SiteDailyBudgetQuota = 0
	})

	reward, err := ClaimGrowthRewardItem(3101, model.GrowthRewardItemDailyCheckin, "")
	require.NoError(t, err)
	require.NotNil(t, reward)
	assert.Equal(t, 200, reward.RewardQuota)

	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 3101).First(&user).Error)
	assert.Equal(t, 300, user.Quota)

	var checkin model.Checkin
	require.NoError(t, model.DB.Where("user_id = ?", 3101).First(&checkin).Error)
	assert.Equal(t, checkin.Id, reward.SourceId)

	var event model.PromotionEvent
	require.NoError(t, model.DB.Where("user_id = ? AND event_type = ?", 3101, model.PromotionEventTypeGrowthRewardSettled).First(&event).Error)
	assert.Equal(t, 200, event.QuotaDelta)
}

func TestClaimDailyCheckinRespectsDailyBudget(t *testing.T) {
	truncate(t)
	seedUser(t, 3102, 100)
	require.NoError(t, model.EnsureDefaultGrowthRewardItems())
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.DailyCheckinEnabled = true
		setting.DailyCheckinMinRewardQuota = 200
		setting.DailyCheckinMaxRewardQuota = 200
		setting.UserDailyRewardLimitQuota = 100
		setting.SiteDailyBudgetQuota = 0
	})

	_, err := ClaimGrowthRewardItem(3102, model.GrowthRewardItemDailyCheckin, "")
	require.Error(t, err)

	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 3102).First(&user).Error)
	assert.Equal(t, 100, user.Quota)

	var checkinCount int64
	require.NoError(t, model.DB.Model(&model.Checkin{}).Where("user_id = ?", 3102).Count(&checkinCount).Error)
	assert.Zero(t, checkinCount)
}

func TestDailyCheckinListUsesConfiguredQuotaRange(t *testing.T) {
	truncate(t)
	seedUser(t, 3103, 100)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.Enabled = true
		setting.DailyCheckinEnabled = true
		setting.DailyCheckinMinRewardQuota = 1000000
		setting.DailyCheckinMaxRewardQuota = 50000000
	})
	require.NoError(t, model.DB.Create(&model.GrowthRewardItem{
		Code:        model.GrowthRewardItemDailyCheckin,
		Title:       "Daily check-in",
		Description: "Daily check-in",
		RewardQuota: 500000,
		ItemType:    model.GrowthRewardItemTypeAuto,
		Enabled:     true,
		OncePerUser: false,
		DailyLimit:  1,
	}).Error)

	items, err := ListGrowthRewardItemsForUser(3103)
	require.NoError(t, err)

	var checkinItem *GrowthRewardItemStatus
	for _, item := range items {
		if item.Code == model.GrowthRewardItemDailyCheckin {
			checkinItem = item
			break
		}
	}
	require.NotNil(t, checkinItem)
	assert.Equal(t, 1000000, checkinItem.RewardQuota)
	assert.Equal(t, 1000000, checkinItem.RewardQuotaMin)
	assert.Equal(t, 50000000, checkinItem.RewardQuotaMax)
}

func TestGrowthSummaryIncludesInvitationGuideValue(t *testing.T) {
	truncate(t)
	seedUser(t, 3104, 100)
	oldQuotaForInviter := common.QuotaForInviter
	oldInviteRebatePercentage := common.InviteRebatePercentage
	t.Cleanup(func() {
		common.QuotaForInviter = oldQuotaForInviter
		common.InviteRebatePercentage = oldInviteRebatePercentage
	})
	common.QuotaForInviter = 100
	common.InviteRebatePercentage = 10
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.InviteRebatePercentage = 10
		setting.InviteFirstRequestRewardQuota = 200
		setting.InviteFirstTopUpRewardQuota = 300
	})

	summary, err := GetGrowthSummary(3104)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 600, summary.InvitationChainRewardQuota)
	assert.Equal(t, 10.0, summary.InviteRebatePercent)
}
