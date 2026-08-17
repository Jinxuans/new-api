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
import dayjs from '@/lib/dayjs'

export type GrowthSummary = {
  task_reward_earned_quota: number
  task_reward_pending_quota: number
  referral_credit_available_quota: number
  referral_credit_total_quota: number
  available_reward_quota: number
  pending_reward_quota: number
  total_reward_quota: number
  invite_count: number
  monthly_rebate_quota: number
  total_rebate_quota: number
  aff_code: string
  invite_rebate_percent: number
  invitation_chain_reward_quota: number
  cash_commission?: PromotionCommissionSummary
}

export type PromotionCommissionSummary = {
  currency: string
  available_amount_cents: number
  pending_amount_cents: number
  withdrawing_amount_cents: number
  withdrawn_amount_cents: number
  transferred_amount_cents: number
  available_quota_equivalent: number
}

export type PromotionCommissionLedger = {
  currency: string
  gross_amount_cents: number
  fee_amount_cents: number
  tax_amount_cents: number
  net_amount_cents: number
  quota_equivalent: number
  status: string
  available_at?: number | string
  settled_at?: number | string
  reversal_amount_cents?: number
  reversal_quota?: number
  reversed_at?: number | string
  created_at?: number | string
}

export type PromotionWithdrawal = {
  id: number
  currency: string
  gross_amount_cents: number
  fee_amount_cents: number
  tax_amount_cents: number
  net_amount_cents: number
  status: string
  payout_method?: string
  trade_no?: string
  applied_at?: number | string
  reviewed_at?: number | string
  payout_initiated_at?: number | string
  paid_at?: number | string
  created_at?: number | string
}

export type PromotionFundTransactionLeg = {
  account: string
  asset: 'quota' | 'cash'
  currency?: string
  amount: number
  balance_after?: number | null
}

export type PromotionFundTransaction = {
  kind: string
  source?:
    | 'registration_reward'
    | 'growth_reward'
    | 'invitation_reward'
    | 'commission'
    | 'withdrawal'
    | 'refund'
    | 'topup'
    | 'subscription'
    | 'redemption'
    | 'admin_adjustment'
    | 'opening_balance'
    | 'legacy'
  external_ref?: string
  occurred_at?: number | string
  created_at?: number | string
  legs: PromotionFundTransactionLeg[]
}

export type GrowthRewardItem = {
  code: string
  title: string
  description: string
  introduction?: string
  reward_quota: number
  reward_quota_min?: number
  reward_quota_max?: number
  progress_current_quota?: number
  progress_target_quota?: number
  item_type: string
  action_url?: string
  enabled?: boolean
  once_per_user?: boolean
  daily_limit?: number
  status: string
  claimable: boolean
  reason?: string
}

export type GrowthReward = {
  item_code: string
  reward_quota: number
  status: string
  available_at?: number
  created_at: number
  settled_at?: number
}

export type GrowthSubmission = {
  item_code: string
  platform?: string
  url?: string
  remark?: string
  status: string
  created_at: number
  review_note?: string
  reviewed_at?: number
}

export type InvitationReward = {
  invitee_name?: string
  reward_type: string
  reward_quota: number
  status: string
  created_at: number
  settled_at?: number
  trigger_at?: number
}

export const rewardItemCopy: Record<
  string,
  { title: string; description: string }
> = {
  daily_checkin: {
    title: 'Daily check-in',
    description: 'Check in once per day to keep your account active.',
  },
  create_first_api_key: {
    title: 'Create your first API key',
    description: 'Create an API key and prepare your first integration.',
  },
  first_api_request: {
    title: 'Complete your first API request',
    description: 'Send one successful API request through the gateway.',
  },
  first_topup: {
    title: 'Complete your first top-up',
    description: 'Add funds for the first time.',
  },
  three_day_usage: {
    title: 'Use the API for 3 consecutive days',
    description: 'Send requests on 3 consecutive days.',
  },
  monthly_spend_target: {
    title: "Reach this month's spend target",
    description: 'Reach the configured monthly consumption target.',
  },
  join_community: {
    title: 'Join the community',
    description:
      'Join the community and enter the task password to claim the reward.',
  },
  content_publish: {
    title: 'Publish an article, video, or tutorial',
    description: 'Share content that helps others use the API service.',
  },
  backlink_submission: {
    title: 'Submit a website backlink or directory listing',
    description: 'Submit an approved backlink or directory listing.',
  },
}

export function getItems<T>(payload: unknown): T[] {
  const data = payload as { data?: { items?: T[] } }
  return data?.data?.items || []
}

export function formatTime(value?: number | string) {
  const timestamp = Number(value || 0)
  if (!Number.isFinite(timestamp) || timestamp <= 0) return '-'
  return dayjs(timestamp * 1000).format('YYYY-MM-DD HH:mm')
}

export function statusVariant(status: string) {
  if (
    status === 'completed' ||
    status === 'settled' ||
    status === 'approved' ||
    status === 'paid' ||
    status === 'withdrawn' ||
    status === 'transferred'
  ) {
    return 'default'
  }
  if (
    status === 'pending' ||
    status === 'pending_review' ||
    status === 'withdrawing' ||
    status === 'processing'
  ) {
    return 'secondary'
  }
  if (
    status === 'rejected' ||
    status === 'frozen' ||
    status === 'reversed' ||
    status === 'failed'
  ) {
    return 'destructive'
  }
  return 'outline'
}
