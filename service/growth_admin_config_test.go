package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useAdminGrowthConfigTestState(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.DB.Exec("DELETE FROM options").Error)

	growth := operation_setting.GetGrowthSetting()
	previousGrowth := *growth
	payment := operation_setting.GetPaymentSetting()
	previousComplianceConfirmed := payment.ComplianceConfirmed
	previousComplianceVersion := payment.ComplianceTermsVersion
	previousQuotaForInviter := common.QuotaForInviter
	previousQuotaForInvitee := common.QuotaForInvitee
	previousInviteRebatePercentage := common.InviteRebatePercentage
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		require.NoError(t, model.DB.Exec("DELETE FROM options").Error)
		*growth = previousGrowth
		payment.ComplianceConfirmed = previousComplianceConfirmed
		payment.ComplianceTermsVersion = previousComplianceVersion
		common.QuotaForInviter = previousQuotaForInviter
		common.QuotaForInvitee = previousQuotaForInvitee
		common.InviteRebatePercentage = previousInviteRebatePercentage
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func TestUpdateAdminGrowthConfigValidatesRewardRangeAsOneCandidate(t *testing.T) {
	useAdminGrowthConfigTestState(t)
	growth := operation_setting.GetGrowthSetting()
	growth.DailyCheckinMinRewardQuota = 10
	growth.DailyCheckinMaxRewardQuota = 20

	config := GetAdminGrowthConfig().RewardProgram
	config.DailyCheckinMinRewardQuota = 30
	config.DailyCheckinMaxRewardQuota = 40

	require.NoError(t, UpdateAdminGrowthConfig(AdminGrowthConfigUpdate{
		RewardProgram: &config,
	}))
	assert.Equal(t, 30, growth.DailyCheckinMinRewardQuota)
	assert.Equal(t, 40, growth.DailyCheckinMaxRewardQuota)

	var options []model.Option
	require.NoError(t, model.DB.Where("key IN ?", []string{
		"growth_setting.daily_checkin_min_reward_quota",
		"growth_setting.daily_checkin_max_reward_quota",
	}).Find(&options).Error)
	require.Len(t, options, 2)
}

func TestUpdateAdminGrowthConfigRejectsInvalidRewardRangeWithoutWrites(t *testing.T) {
	useAdminGrowthConfigTestState(t)
	config := GetAdminGrowthConfig().RewardProgram
	config.SubmissionMinRewardQuota = 500
	config.SubmissionMaxRewardQuota = 400

	err := UpdateAdminGrowthConfig(AdminGrowthConfigUpdate{
		RewardProgram: &config,
	})

	require.Error(t, err)
	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestValidateReferralProgramConfigRejectsNonFinitePercentage(t *testing.T) {
	for name, percentage := range map[string]float64{
		"nan":               math.NaN(),
		"positive infinity": math.Inf(1),
		"negative infinity": math.Inf(-1),
	} {
		t.Run(name, func(t *testing.T) {
			config := ReferralProgramConfig{InviteRebatePercentage: percentage}
			assert.Error(t, validateReferralProgramConfig(config))
		})
	}
}

func TestUpdateAdminGrowthConfigRequiresComplianceForPositiveReferralRewards(t *testing.T) {
	useAdminGrowthConfigTestState(t)
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = false
	payment.ComplianceTermsVersion = ""
	config := GetAdminGrowthConfig().ReferralProgram
	config.InviterRegistrationRewardQuota = 1

	err := UpdateAdminGrowthConfig(AdminGrowthConfigUpdate{
		ReferralProgram: &config,
	})

	require.Error(t, err)
	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateAdminGrowthConfigAllowsDisabledReferralWithoutCompliance(t *testing.T) {
	useAdminGrowthConfigTestState(t)
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = false
	payment.ComplianceTermsVersion = ""
	config := ReferralProgramConfig{}

	require.NoError(t, UpdateAdminGrowthConfig(AdminGrowthConfigUpdate{
		ReferralProgram: &config,
	}))

	assert.Zero(t, operation_setting.GetInviteRebatePercentage())
	assert.Zero(t, common.InviteRebatePercentage)
	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Count(&count).Error)
	assert.Equal(t, int64(6), count)
}
