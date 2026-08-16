package service

import (
	"errors"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const maxReferralFreezeDays = 3650

type RewardProgramConfig struct {
	Enabled                    bool `json:"enabled"`
	DailyCheckinEnabled        bool `json:"daily_checkin_enabled"`
	DailyCheckinMinRewardQuota int  `json:"daily_checkin_min_reward_quota"`
	DailyCheckinMaxRewardQuota int  `json:"daily_checkin_max_reward_quota"`
	FirstAPIKeyRewardQuota     int  `json:"first_api_key_reward_quota"`
	FirstAPIRequestRewardQuota int  `json:"first_api_request_reward_quota"`
	FirstTopUpRewardQuota      int  `json:"first_topup_reward_quota"`
	ThreeDayUsageRewardQuota   int  `json:"three_day_usage_reward_quota"`
	MonthlySpendRewardQuota    int  `json:"monthly_spend_reward_quota"`
	MonthlySpendTargetQuota    int  `json:"monthly_spend_target_quota"`
	UserDailyRewardLimitQuota  int  `json:"user_daily_reward_limit_quota"`
	SiteDailyBudgetQuota       int  `json:"site_daily_budget_quota"`
	SubmissionEnabled          bool `json:"submission_enabled"`
	SubmissionMinRewardQuota   int  `json:"submission_min_reward_quota"`
	SubmissionMaxRewardQuota   int  `json:"submission_max_reward_quota"`
}

type ReferralProgramConfig struct {
	InviterRegistrationRewardQuota int     `json:"inviter_registration_reward_quota"`
	InviteeRegistrationRewardQuota int     `json:"invitee_registration_reward_quota"`
	InviteRebatePercentage         float64 `json:"invite_rebate_percentage"`
	InviteFirstRequestRewardQuota  int     `json:"invite_first_request_reward_quota"`
	InviteFirstTopUpRewardQuota    int     `json:"invite_first_topup_reward_quota"`
	RebateFreezeDays               int     `json:"rebate_freeze_days"`
}

type AdminGrowthConfig struct {
	RewardProgram     RewardProgramConfig   `json:"reward_program"`
	ReferralProgram   ReferralProgramConfig `json:"referral_program"`
	ComplianceEnabled bool                  `json:"compliance_enabled"`
}

type AdminGrowthConfigUpdate struct {
	RewardProgram   *RewardProgramConfig   `json:"reward_program"`
	ReferralProgram *ReferralProgramConfig `json:"referral_program"`
}

func GetAdminGrowthConfig() AdminGrowthConfig {
	growth := operation_setting.GetGrowthSetting()
	return AdminGrowthConfig{
		RewardProgram: RewardProgramConfig{
			Enabled:                    growth.Enabled,
			DailyCheckinEnabled:        growth.DailyCheckinEnabled,
			DailyCheckinMinRewardQuota: growth.DailyCheckinMinRewardQuota,
			DailyCheckinMaxRewardQuota: growth.DailyCheckinMaxRewardQuota,
			FirstAPIKeyRewardQuota:     growth.FirstAPIKeyRewardQuota,
			FirstAPIRequestRewardQuota: growth.FirstAPIRequestRewardQuota,
			FirstTopUpRewardQuota:      growth.FirstTopUpRewardQuota,
			ThreeDayUsageRewardQuota:   growth.ThreeDayUsageRewardQuota,
			MonthlySpendRewardQuota:    growth.MonthlySpendRewardQuota,
			MonthlySpendTargetQuota:    growth.MonthlySpendTargetQuota,
			UserDailyRewardLimitQuota:  growth.UserDailyRewardLimitQuota,
			SiteDailyBudgetQuota:       growth.SiteDailyBudgetQuota,
			SubmissionEnabled:          growth.SubmissionEnabled,
			SubmissionMinRewardQuota:   growth.SubmissionMinRewardQuota,
			SubmissionMaxRewardQuota:   growth.SubmissionMaxRewardQuota,
		},
		ReferralProgram: ReferralProgramConfig{
			InviterRegistrationRewardQuota: common.QuotaForInviter,
			InviteeRegistrationRewardQuota: common.QuotaForInvitee,
			InviteRebatePercentage:         operation_setting.GetInviteRebatePercentage(),
			InviteFirstRequestRewardQuota:  growth.InviteFirstRequestRewardQuota,
			InviteFirstTopUpRewardQuota:    growth.InviteFirstTopUpRewardQuota,
			RebateFreezeDays:               growth.RebateFreezeDays,
		},
		ComplianceEnabled: operation_setting.IsPaymentComplianceConfirmed(),
	}
}

func UpdateAdminGrowthConfig(update AdminGrowthConfigUpdate) error {
	if update.RewardProgram == nil && update.ReferralProgram == nil {
		return errors.New("reward_program or referral_program is required")
	}

	values := make(map[string]string)
	if update.RewardProgram != nil {
		if err := validateRewardProgramConfig(*update.RewardProgram); err != nil {
			return err
		}
		appendRewardProgramOptions(values, *update.RewardProgram)
	}
	if update.ReferralProgram != nil {
		if err := validateReferralProgramConfig(*update.ReferralProgram); err != nil {
			return err
		}
		appendReferralProgramOptions(values, *update.ReferralProgram)
	}
	return model.UpdateOptionsBulk(values)
}

func validateRewardProgramConfig(config RewardProgramConfig) error {
	quotas := []int{
		config.DailyCheckinMinRewardQuota,
		config.DailyCheckinMaxRewardQuota,
		config.FirstAPIKeyRewardQuota,
		config.FirstAPIRequestRewardQuota,
		config.FirstTopUpRewardQuota,
		config.ThreeDayUsageRewardQuota,
		config.MonthlySpendRewardQuota,
		config.MonthlySpendTargetQuota,
		config.UserDailyRewardLimitQuota,
		config.SiteDailyBudgetQuota,
		config.SubmissionMinRewardQuota,
		config.SubmissionMaxRewardQuota,
	}
	for _, quota := range quotas {
		if quota < 0 || int64(quota) > int64(math.MaxInt32) {
			return errors.New("reward quota must be between 0 and 2147483647")
		}
	}
	if config.DailyCheckinMinRewardQuota > config.DailyCheckinMaxRewardQuota {
		return errors.New("minimum check-in quota cannot exceed maximum check-in quota")
	}
	if config.SubmissionMinRewardQuota > config.SubmissionMaxRewardQuota {
		return errors.New("minimum submission reward cannot exceed maximum submission reward")
	}
	return nil
}

func validateReferralProgramConfig(config ReferralProgramConfig) error {
	quotas := []int{
		config.InviterRegistrationRewardQuota,
		config.InviteeRegistrationRewardQuota,
		config.InviteFirstRequestRewardQuota,
		config.InviteFirstTopUpRewardQuota,
	}
	for _, quota := range quotas {
		if quota < 0 || int64(quota) > int64(math.MaxInt32) {
			return errors.New("referral reward quota must be between 0 and 2147483647")
		}
	}
	if math.IsNaN(config.InviteRebatePercentage) || math.IsInf(config.InviteRebatePercentage, 0) ||
		config.InviteRebatePercentage < 0 || config.InviteRebatePercentage > 100 {
		return errors.New("referral commission percentage must be between 0 and 100")
	}
	if config.RebateFreezeDays < 0 || config.RebateFreezeDays > maxReferralFreezeDays {
		return errors.New("referral commission freeze days must be between 0 and 3650")
	}
	if !operation_setting.IsPaymentComplianceConfirmed() && (config.InviterRegistrationRewardQuota > 0 ||
		config.InviteeRegistrationRewardQuota > 0 ||
		config.InviteRebatePercentage > 0 ||
		config.InviteFirstRequestRewardQuota > 0 ||
		config.InviteFirstTopUpRewardQuota > 0) {
		return errors.New("payment compliance confirmation is required before enabling referral rewards")
	}
	return nil
}

func appendRewardProgramOptions(values map[string]string, config RewardProgramConfig) {
	values["growth_setting.enabled"] = strconv.FormatBool(config.Enabled)
	values["growth_setting.daily_checkin_enabled"] = strconv.FormatBool(config.DailyCheckinEnabled)
	values["growth_setting.daily_checkin_min_reward_quota"] = strconv.Itoa(config.DailyCheckinMinRewardQuota)
	values["growth_setting.daily_checkin_max_reward_quota"] = strconv.Itoa(config.DailyCheckinMaxRewardQuota)
	values["growth_setting.first_api_key_reward_quota"] = strconv.Itoa(config.FirstAPIKeyRewardQuota)
	values["growth_setting.first_api_request_reward_quota"] = strconv.Itoa(config.FirstAPIRequestRewardQuota)
	values["growth_setting.first_topup_reward_quota"] = strconv.Itoa(config.FirstTopUpRewardQuota)
	values["growth_setting.three_day_usage_reward_quota"] = strconv.Itoa(config.ThreeDayUsageRewardQuota)
	values["growth_setting.monthly_spend_reward_quota"] = strconv.Itoa(config.MonthlySpendRewardQuota)
	values["growth_setting.monthly_spend_target_quota"] = strconv.Itoa(config.MonthlySpendTargetQuota)
	values["growth_setting.user_daily_reward_limit_quota"] = strconv.Itoa(config.UserDailyRewardLimitQuota)
	values["growth_setting.site_daily_budget_quota"] = strconv.Itoa(config.SiteDailyBudgetQuota)
	values["growth_setting.submission_enabled"] = strconv.FormatBool(config.SubmissionEnabled)
	values["growth_setting.submission_min_reward_quota"] = strconv.Itoa(config.SubmissionMinRewardQuota)
	values["growth_setting.submission_max_reward_quota"] = strconv.Itoa(config.SubmissionMaxRewardQuota)
}

func appendReferralProgramOptions(values map[string]string, config ReferralProgramConfig) {
	values["QuotaForInviter"] = strconv.Itoa(config.InviterRegistrationRewardQuota)
	values["QuotaForInvitee"] = strconv.Itoa(config.InviteeRegistrationRewardQuota)
	values["growth_setting.invite_rebate_percentage"] = strconv.FormatFloat(config.InviteRebatePercentage, 'f', -1, 64)
	values["growth_setting.invite_first_request_reward_quota"] = strconv.Itoa(config.InviteFirstRequestRewardQuota)
	values["growth_setting.invite_first_topup_reward_quota"] = strconv.Itoa(config.InviteFirstTopUpRewardQuota)
	values["growth_setting.rebate_freeze_days"] = strconv.Itoa(config.RebateFreezeDays)
}
