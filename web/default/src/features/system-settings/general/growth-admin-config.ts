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
import { z } from 'zod'

import type { ReferralProgramConfig, RewardProgramConfig } from '../types'

const maxQuota = 2_147_483_647

const quota = z.coerce.number().int().min(0).max(maxQuota)

type Translate = (key: string) => string

export function createRewardProgramSchema(t: Translate) {
  return z
    .object({
      enabled: z.boolean(),
      dailyCheckinEnabled: z.boolean(),
      dailyCheckinMinRewardQuota: quota,
      dailyCheckinMaxRewardQuota: quota,
      firstAPIKeyRewardQuota: quota,
      firstAPIRequestRewardQuota: quota,
      firstTopUpRewardQuota: quota,
      threeDayUsageRewardQuota: quota,
      monthlySpendRewardQuota: quota,
      monthlySpendTargetQuota: quota,
      userDailyRewardLimitQuota: quota,
      siteDailyBudgetQuota: quota,
      submissionEnabled: z.boolean(),
      submissionMinRewardQuota: quota,
      submissionMaxRewardQuota: quota,
    })
    .refine(
      (values) =>
        values.dailyCheckinMaxRewardQuota >= values.dailyCheckinMinRewardQuota,
      {
        path: ['dailyCheckinMaxRewardQuota'],
        message: t(
          'Maximum check-in quota must be greater than or equal to the minimum.'
        ),
      }
    )
    .refine(
      (values) =>
        values.submissionMaxRewardQuota >= values.submissionMinRewardQuota,
      {
        path: ['submissionMaxRewardQuota'],
        message: t(
          'Submission maximum reward must be greater than or equal to the minimum.'
        ),
      }
    )
}

export const rewardProgramSchema = createRewardProgramSchema((key) => key)

export type RewardProgramFormValues = z.infer<typeof rewardProgramSchema>

export const referralProgramSchema = z.object({
  inviterRegistrationRewardQuota: quota,
  inviteeRegistrationRewardQuota: quota,
  inviteRebatePercentage: z.coerce.number().min(0).max(100),
  inviteFirstRequestRewardQuota: quota,
  inviteFirstTopUpRewardQuota: quota,
  rebateFreezeDays: z.coerce.number().int().min(0).max(3650),
})

export type ReferralProgramFormValues = z.infer<typeof referralProgramSchema>

export function toRewardProgramConfig(
  values: RewardProgramFormValues
): RewardProgramConfig {
  return {
    enabled: values.enabled,
    daily_checkin_enabled: values.dailyCheckinEnabled,
    daily_checkin_min_reward_quota: values.dailyCheckinMinRewardQuota,
    daily_checkin_max_reward_quota: values.dailyCheckinMaxRewardQuota,
    first_api_key_reward_quota: values.firstAPIKeyRewardQuota,
    first_api_request_reward_quota: values.firstAPIRequestRewardQuota,
    first_topup_reward_quota: values.firstTopUpRewardQuota,
    three_day_usage_reward_quota: values.threeDayUsageRewardQuota,
    monthly_spend_reward_quota: values.monthlySpendRewardQuota,
    monthly_spend_target_quota: values.monthlySpendTargetQuota,
    user_daily_reward_limit_quota: values.userDailyRewardLimitQuota,
    site_daily_budget_quota: values.siteDailyBudgetQuota,
    submission_enabled: values.submissionEnabled,
    submission_min_reward_quota: values.submissionMinRewardQuota,
    submission_max_reward_quota: values.submissionMaxRewardQuota,
  }
}

export function toReferralProgramConfig(
  values: ReferralProgramFormValues
): ReferralProgramConfig {
  return {
    inviter_registration_reward_quota: values.inviterRegistrationRewardQuota,
    invitee_registration_reward_quota: values.inviteeRegistrationRewardQuota,
    invite_rebate_percentage: values.inviteRebatePercentage,
    invite_first_request_reward_quota: values.inviteFirstRequestRewardQuota,
    invite_first_topup_reward_quota: values.inviteFirstTopUpRewardQuota,
    rebate_freeze_days: values.rebateFreezeDays,
  }
}
