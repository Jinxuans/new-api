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
import { describe, expect, test } from 'vitest'

import type { GrowthSummary } from '@/features/promotion/shared'

import { toPromotionEarnings } from '../api'

describe('promotion earnings semantics', () => {
  test('uses dedicated task and referral fields instead of mixed legacy totals', () => {
    const summary = {
      task_reward_earned_quota: 8_000,
      task_reward_pending_quota: 2_000,
      referral_credit_available_quota: 12_000,
      referral_credit_total_quota: 20_000,
      total_reward_quota: 999_999,
      pending_reward_quota: 888_888,
      invite_count: 3,
      cash_commission: {
        currency: 'CNY',
        available_amount_cents: 1_234,
        pending_amount_cents: 200,
        withdrawing_amount_cents: 0,
        withdrawn_amount_cents: 0,
        transferred_amount_cents: 0,
        available_quota_equivalent: 500_000,
      },
    } as GrowthSummary

    expect(toPromotionEarnings(summary)).toMatchObject({
      taskRewardsEarned: 8_000,
      taskRewardsPending: 2_000,
      transferableReferralCredit: 12_000,
      totalReferralCredit: 20_000,
      invitedUsers: 3,
    })
  })
})
