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
import { t } from 'i18next'

import type {
  GrowthReward,
  GrowthRewardItem,
  GrowthSubmission,
  GrowthSummary,
  InvitationReward,
  PromotionCommissionLedger,
  PromotionFundTransaction,
  PromotionWithdrawal,
} from '@/features/promotion/shared'
import { getAffiliateCode, transferAffiliateQuota } from '@/features/wallet/api'
import { generateAffiliateLink } from '@/features/wallet/lib'
import { api } from '@/lib/api'

type ApiEnvelope<T> = {
  success?: boolean
  message?: string
  data?: T
}

export type PromotionEarnings = {
  taskRewardsEarned: number
  taskRewardsPending: number
  transferableReferralCredit: number
  totalReferralCredit: number
  invitedUsers: number
  withdrawableCashCents: number
  pendingCashCents: number
  withdrawingCashCents: number
  cashQuotaEquivalent: number
  cashCurrency: string
}

export type PromotionReferralTools = {
  referralCode: string
  referralLink: string
}

export type PromotionOverview = GrowthSummary

export type PromotionActivityFilter =
  | 'funds'
  | 'tasks'
  | 'submissions'
  | 'referrals'
  | 'commissions'
  | 'withdrawals'

export type PromotionActivityItem =
  | PromotionFundTransaction
  | GrowthReward
  | GrowthSubmission
  | InvitationReward
  | PromotionCommissionLedger
  | PromotionWithdrawal

export type PromotionActivityPage = {
  page: number
  pageSize: number
  total: number
  items: PromotionActivityItem[]
}

export type PromotionSubmissionInput = {
  item_code: string
  platform: string
  url: string
  remark: string
}

export type PromotionWithdrawalInput = {
  payout_method: string
  payout_account: string
  remark: string
  expected_amount_cents: number
  expected_quota_equivalent: number
}

export type PromotionCommissionBalanceExpectation = {
  expected_amount_cents: number
  expected_quota_equivalent: number
}

export const promotionQueryKeys = {
  all: ['promotion'] as const,
  overview: ['promotion', 'overview'] as const,
  referralTools: ['promotion', 'referral-tools'] as const,
  rewardItems: ['promotion', 'reward-items'] as const,
  activityRoot: ['promotion', 'activity'] as const,
  activity: (filter: PromotionActivityFilter, page: number, pageSize: number) =>
    ['promotion', 'activity', filter, page, pageSize] as const,
}

function unwrap<T>(response: ApiEnvelope<T>, fallbackMessage: string): T {
  if (response.success === false) {
    throw new Error(response.message || fallbackMessage)
  }
  if (response.data === undefined || response.data === null) {
    throw new Error(response.message || fallbackMessage)
  }
  return response.data
}

function ensureSuccess(
  response: ApiEnvelope<unknown>,
  fallbackMessage: string
): void {
  if (response.success !== true) {
    throw new Error(response.message || fallbackMessage)
  }
}

async function getPage<T extends PromotionActivityItem>(
  endpoint: string,
  page: number,
  pageSize: number
): Promise<PromotionActivityPage> {
  const response = await api.get(endpoint, {
    params: { p: page, page_size: pageSize },
  })
  const payload = unwrap<{
    page?: number
    page_size?: number
    total?: number
    items?: T[]
  }>(response.data, t('Unable to load activity'))

  return {
    page: payload.page || page,
    pageSize: payload.page_size || pageSize,
    total: payload.total || 0,
    items: payload.items || [],
  }
}

export async function getPromotionOverview(): Promise<PromotionOverview> {
  const response = await api.post('/api/growth/commissions/settle')
  return unwrap<GrowthSummary>(response.data, t('Unable to load earnings'))
}

export function toPromotionEarnings(
  overview: PromotionOverview
): PromotionEarnings {
  const cash = overview.cash_commission
  return {
    taskRewardsEarned: overview.task_reward_earned_quota || 0,
    taskRewardsPending: overview.task_reward_pending_quota || 0,
    transferableReferralCredit: overview.referral_credit_available_quota || 0,
    totalReferralCredit: overview.referral_credit_total_quota || 0,
    invitedUsers: overview.invite_count || 0,
    withdrawableCashCents: cash?.available_amount_cents || 0,
    pendingCashCents: cash?.pending_amount_cents || 0,
    withdrawingCashCents: cash?.withdrawing_amount_cents || 0,
    cashQuotaEquivalent: cash?.available_quota_equivalent || 0,
    cashCurrency: cash?.currency || 'CNY',
  }
}

export async function getPromotionReferralTools(): Promise<PromotionReferralTools> {
  const codeResponse = await getAffiliateCode()
  const referralCode = unwrap<string>(
    codeResponse,
    t('Unable to load referral link')
  )
  return {
    referralCode,
    referralLink: generateAffiliateLink(referralCode),
  }
}

export async function getPromotionRewardItems(): Promise<GrowthRewardItem[]> {
  const response = await api.get('/api/growth/items')
  return unwrap<GrowthRewardItem[]>(
    response.data,
    t('Unable to load reward tasks')
  )
}

export async function getPromotionActivity(
  filter: PromotionActivityFilter,
  page: number,
  pageSize: number
): Promise<PromotionActivityPage> {
  const endpointByFilter: Record<PromotionActivityFilter, string> = {
    funds: '/api/growth/fund-records',
    tasks: '/api/growth/rewards',
    submissions: '/api/growth/submissions',
    referrals: '/api/user/aff/rewards',
    commissions: '/api/growth/commissions',
    withdrawals: '/api/growth/withdrawals',
  }
  return getPage<PromotionActivityItem>(
    endpointByFilter[filter],
    page,
    pageSize
  )
}

export async function claimPromotionReward(
  code: string,
  options?: { password?: string; turnstileToken?: string }
): Promise<void> {
  const suffix = options?.turnstileToken
    ? `?turnstile=${encodeURIComponent(options.turnstileToken)}`
    : ''
  const response = await api.post(
    `/api/growth/items/${code}/claim${suffix}`,
    options?.password ? { password: options.password } : undefined
  )
  ensureSuccess(response.data, t('Unable to claim reward'))
}

export async function createPromotionSubmission(
  input: PromotionSubmissionInput
): Promise<void> {
  const response = await api.post('/api/growth/submissions', input)
  ensureSuccess(response.data, t('Unable to submit proof'))
}

export async function transferAllReferralCredit(quota: number): Promise<void> {
  const response = await transferAffiliateQuota({ quota })
  ensureSuccess(response, t('Unable to transfer referral credit'))
}

export async function convertAllCashToBalance(
  expected: PromotionCommissionBalanceExpectation
): Promise<number> {
  const response = await api.post('/api/growth/commissions/transfer', expected)
  const data = unwrap<{ quota?: number }>(
    response.data,
    t('Unable to convert cash commission')
  )
  return data.quota || 0
}

export async function withdrawAllCash(
  input: PromotionWithdrawalInput
): Promise<void> {
  const response = await api.post('/api/growth/withdrawals', input)
  ensureSuccess(response.data, t('Unable to submit withdrawal'))
}
