/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'vitest'

import {
  createRewardProgramSchema,
  referralProgramSchema,
  rewardProgramSchema,
  toReferralProgramConfig,
  toRewardProgramConfig,
  type ReferralProgramFormValues,
  type RewardProgramFormValues,
} from '../growth-admin-config'

const rewardValues: RewardProgramFormValues = {
  enabled: true,
  dailyCheckinEnabled: true,
  dailyCheckinMinRewardQuota: 100,
  dailyCheckinMaxRewardQuota: 200,
  firstAPIKeyRewardQuota: 300,
  firstAPIRequestRewardQuota: 400,
  firstTopUpRewardQuota: 500,
  threeDayUsageRewardQuota: 600,
  monthlySpendRewardQuota: 700,
  monthlySpendTargetQuota: 800,
  userDailyRewardLimitQuota: 900,
  siteDailyBudgetQuota: 1000,
  submissionEnabled: true,
  submissionMinRewardQuota: 1100,
  submissionMaxRewardQuota: 1200,
}

const referralValues: ReferralProgramFormValues = {
  inviterRegistrationRewardQuota: 100,
  inviteeRegistrationRewardQuota: 200,
  inviteRebatePercentage: 12.5,
  inviteFirstRequestRewardQuota: 300,
  inviteFirstTopUpRewardQuota: 400,
  rebateFreezeDays: 7,
}

describe('growth admin configuration', () => {
  it('validates reward ranges as one configuration', () => {
    expect(
      rewardProgramSchema.safeParse({
        ...rewardValues,
        dailyCheckinMinRewardQuota: 300,
        dailyCheckinMaxRewardQuota: 400,
      }).success
    ).toBe(true)
    expect(
      rewardProgramSchema.safeParse({
        ...rewardValues,
        submissionMinRewardQuota: 500,
        submissionMaxRewardQuota: 400,
      }).success
    ).toBe(false)
  })

  it('translates cross-field validation messages for display', () => {
    const schema = createRewardProgramSchema((key) => `translated:${key}`)
    const result = schema.safeParse({
      ...rewardValues,
      dailyCheckinMinRewardQuota: 300,
      dailyCheckinMaxRewardQuota: 200,
    })

    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues[0]?.message).toBe(
        'translated:Maximum check-in quota must be greater than or equal to the minimum.'
      )
    }
  })

  it('maps the complete reward form to one server block', () => {
    expect(toRewardProgramConfig(rewardValues)).toEqual({
      enabled: true,
      daily_checkin_enabled: true,
      daily_checkin_min_reward_quota: 100,
      daily_checkin_max_reward_quota: 200,
      first_api_key_reward_quota: 300,
      first_api_request_reward_quota: 400,
      first_topup_reward_quota: 500,
      three_day_usage_reward_quota: 600,
      monthly_spend_reward_quota: 700,
      monthly_spend_target_quota: 800,
      user_daily_reward_limit_quota: 900,
      site_daily_budget_quota: 1000,
      submission_enabled: true,
      submission_min_reward_quota: 1100,
      submission_max_reward_quota: 1200,
    })
  })

  it('bounds referral commission and maps one server block', () => {
    expect(
      referralProgramSchema.safeParse({
        ...referralValues,
        inviteRebatePercentage: 100.01,
      }).success
    ).toBe(false)
    expect(toReferralProgramConfig(referralValues)).toEqual({
      inviter_registration_reward_quota: 100,
      invitee_registration_reward_quota: 200,
      invite_rebate_percentage: 12.5,
      invite_first_request_reward_quota: 300,
      invite_first_topup_reward_quota: 400,
      rebate_freeze_days: 7,
    })
  })
})
