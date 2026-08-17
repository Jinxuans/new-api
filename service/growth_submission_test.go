package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateGrowthSubmissionValidatesExternalInput(t *testing.T) {
	truncate(t)
	seedUser(t, 3110, 0)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.SubmissionEnabled = true
	})
	item := &model.GrowthRewardItem{
		Code:       "test_content_publish_validation",
		Title:      "Content publish",
		ItemType:   model.GrowthRewardItemTypeManual,
		Enabled:    true,
		DailyLimit: 10,
	}
	require.NoError(t, model.DB.Create(item).Error)

	testCases := []struct {
		name string
		req  GrowthSubmissionRequest
	}{
		{name: "non HTTP URL", req: GrowthSubmissionRequest{ItemCode: item.Code, Platform: "blog", Url: "ftp://example.com/post"}},
		{name: "platform too long", req: GrowthSubmissionRequest{ItemCode: item.Code, Platform: strings.Repeat("平", 65), Url: "https://example.com/post"}},
		{name: "URL too long", req: GrowthSubmissionRequest{ItemCode: item.Code, Platform: "blog", Url: "https://example.com/" + strings.Repeat("a", 2048)}},
		{name: "remark too long", req: GrowthSubmissionRequest{ItemCode: item.Code, Platform: "blog", Url: "https://example.com/post", Remark: strings.Repeat("注", 501)}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreateGrowthSubmission(3110, tc.req)
			require.Error(t, err)
		})
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.GrowthSubmission{}).Where("user_id = ?", 3110).Count(&count).Error)
	assert.Zero(t, count)
}

func TestCreateGrowthSubmissionEnforcesDailyLimitInsideTransaction(t *testing.T) {
	truncate(t)
	seedUser(t, 3111, 0)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.SubmissionEnabled = true
	})
	item := &model.GrowthRewardItem{
		Code:       "test_content_publish_daily_limit",
		Title:      "Content publish",
		ItemType:   model.GrowthRewardItemTypeManual,
		Enabled:    true,
		DailyLimit: 1,
	}
	require.NoError(t, model.DB.Create(item).Error)
	req := GrowthSubmissionRequest{ItemCode: item.Code, Platform: "blog", Url: "https://example.com/post"}
	_, err := CreateGrowthSubmission(3111, req)
	require.NoError(t, err)
	_, err = CreateGrowthSubmission(3111, req)
	require.Error(t, err)
	var count int64
	require.NoError(t, model.DB.Model(&model.GrowthSubmission{}).Where("user_id = ?", 3111).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestGrowthSubmissionLifecycleIsRecordedInPromotionEvents(t *testing.T) {
	truncate(t)
	seedFinancialActor(t, 91, common.RoleAdminUser)
	seedUser(t, 3112, 0)
	withGrowthSetting(t, func(setting *operation_setting.GrowthSetting) {
		setting.SubmissionEnabled = true
		setting.UserDailyRewardLimitQuota = 0
		setting.SiteDailyBudgetQuota = 0
	})
	item := &model.GrowthRewardItem{
		Code:        "test_content_publish_events",
		Title:       "Content publish",
		ItemType:    model.GrowthRewardItemTypeManual,
		Enabled:     true,
		RewardQuota: 100,
		DailyLimit:  10,
	}
	require.NoError(t, model.DB.Create(item).Error)

	approved, err := CreateGrowthSubmission(3112, GrowthSubmissionRequest{
		ItemCode: item.Code,
		Platform: "blog",
		Url:      "https://example.com/approved",
	})
	require.NoError(t, err)
	_, err = ApproveGrowthSubmission(approved.Id, 91, GrowthReviewRequest{RewardQuota: 100, ReviewNote: "approved"})
	require.NoError(t, err)

	rejected, err := CreateGrowthSubmission(3112, GrowthSubmissionRequest{
		ItemCode: item.Code,
		Platform: "blog",
		Url:      "https://example.com/rejected",
	})
	require.NoError(t, err)
	_, err = RejectGrowthSubmission(rejected.Id, 91, "rejected")
	require.NoError(t, err)

	var events []model.PromotionEvent
	require.NoError(t, model.DB.
		Where("user_id = ? AND source_table = ?", 3112, model.PromotionEventSourceGrowthSubmission).
		Order("id ASC").
		Find(&events).Error)
	require.Len(t, events, 4)
	assert.Equal(t, []string{
		model.PromotionEventTypeGrowthSubmissionCreated,
		model.PromotionEventTypeGrowthSubmissionApproved,
		model.PromotionEventTypeGrowthSubmissionCreated,
		model.PromotionEventTypeGrowthSubmissionRejected,
	}, []string{events[0].EventType, events[1].EventType, events[2].EventType, events[3].EventType})
	assert.Equal(t, model.GrowthSubmissionStatusPending, events[0].Status)
	assert.Equal(t, model.GrowthSubmissionStatusApproved, events[1].Status)
	assert.Equal(t, model.GrowthSubmissionStatusPending, events[2].Status)
	assert.Equal(t, model.GrowthSubmissionStatusRejected, events[3].Status)
}
