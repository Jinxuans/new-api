package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type GrowthSetting struct {
	Enabled                       bool    `json:"enabled"`
	DailyCheckinEnabled           bool    `json:"daily_checkin_enabled"`
	DailyCheckinMinRewardQuota    int     `json:"daily_checkin_min_reward_quota"`
	DailyCheckinMaxRewardQuota    int     `json:"daily_checkin_max_reward_quota"`
	FirstAPIKeyRewardQuota        int     `json:"first_api_key_reward_quota"`
	FirstAPIRequestRewardQuota    int     `json:"first_api_request_reward_quota"`
	FirstTopUpRewardQuota         int     `json:"first_topup_reward_quota"`
	ThreeDayUsageRewardQuota      int     `json:"three_day_usage_reward_quota"`
	MonthlySpendRewardQuota       int     `json:"monthly_spend_reward_quota"`
	MonthlySpendTargetQuota       int     `json:"monthly_spend_target_quota"`
	InviteRebatePercentage        float64 `json:"invite_rebate_percentage"`
	InviteFirstRequestRewardQuota int     `json:"invite_first_request_reward_quota"`
	InviteFirstTopUpRewardQuota   int     `json:"invite_first_topup_reward_quota"`
	RebateFreezeDays              int     `json:"rebate_freeze_days"`
	UserDailyRewardLimitQuota     int     `json:"user_daily_reward_limit_quota"`
	SiteDailyBudgetQuota          int     `json:"site_daily_budget_quota"`
	SubmissionEnabled             bool    `json:"submission_enabled"`
	SubmissionMinRewardQuota      int     `json:"submission_min_reward_quota"`
	SubmissionMaxRewardQuota      int     `json:"submission_max_reward_quota"`
}

var growthSetting = GrowthSetting{
	Enabled:                       false,
	DailyCheckinEnabled:           false,
	DailyCheckinMinRewardQuota:    1000,
	DailyCheckinMaxRewardQuota:    10000,
	FirstAPIKeyRewardQuota:        1000,
	FirstAPIRequestRewardQuota:    5000,
	FirstTopUpRewardQuota:         5000,
	ThreeDayUsageRewardQuota:      5000,
	MonthlySpendRewardQuota:       10000,
	MonthlySpendTargetQuota:       500000,
	InviteRebatePercentage:        0,
	InviteFirstRequestRewardQuota: 0,
	InviteFirstTopUpRewardQuota:   0,
	RebateFreezeDays:              7,
	UserDailyRewardLimitQuota:     0,
	SiteDailyBudgetQuota:          0,
	SubmissionEnabled:             false,
	SubmissionMinRewardQuota:      5000,
	SubmissionMaxRewardQuota:      50000,
}

func init() {
	config.GlobalConfig.Register("growth_setting", &growthSetting)
}

func GetGrowthSetting() *GrowthSetting {
	return &growthSetting
}

func GetInviteRebatePercentage() float64 {
	return growthSetting.InviteRebatePercentage
}

func IsDailyCheckinEnabled() bool {
	return growthSetting.DailyCheckinEnabled
}

func GetDailyCheckinQuotaRange() (min, max int) {
	return growthSetting.DailyCheckinMinRewardQuota, growthSetting.DailyCheckinMaxRewardQuota
}
