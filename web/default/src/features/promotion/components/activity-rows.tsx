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
import type { TFunction } from 'i18next'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  formatCashCents,
  formatTime,
  rewardItemCopy,
  statusVariant,
  type GrowthReward,
  type GrowthSubmission,
  type InvitationReward,
  type PromotionCommissionLedger,
  type PromotionEvent,
  type PromotionWithdrawal,
} from '@/features/promotion/shared'
import { formatQuota } from '@/lib/format'

import type { PromotionActivityFilter, PromotionActivityItem } from '../api'

const EVENT_TITLES: Record<string, string> = {
  invitation_register_reward: 'Invitation registration reward',
  invitation_first_request_reward: 'Invitation first request reward',
  invitation_first_topup_reward: 'Invitation first top-up reward',
  commission_pending: 'Cash commission pending settlement',
  commission_settled: 'Cash commission settled',
  commission_transferred: 'Cash commission converted to API balance',
  promotion_reward_transferred: 'Referral credit transferred to API balance',
  commission_withdraw_submitted: 'Cash withdrawal request submitted',
  commission_withdraw_approved: 'Cash withdrawal request approved',
  commission_withdraw_rejected: 'Cash withdrawal request rejected',
  commission_withdraw_paid: 'Cash withdrawal paid',
  commission_reversed: 'Cash commission reversed',
  growth_reward_settled: 'Task reward settled',
}

type ActivityRowProps = {
  title: string
  detail: ReactNode
  status?: string
  amount?: string
}

function ActivityRow(props: ActivityRowProps) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-2 p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
      <div className='min-w-0'>
        <div className='flex flex-wrap items-center gap-2'>
          <h3 className='text-sm font-medium'>{props.title}</h3>
          {props.status ? (
            <Badge variant={statusVariant(props.status)}>
              {t(props.status)}
            </Badge>
          ) : null}
        </div>
        <div className='text-muted-foreground mt-1 text-xs leading-5'>
          {props.detail}
        </div>
      </div>
      {props.amount ? (
        <div className='text-sm font-semibold tabular-nums sm:text-right'>
          {props.amount}
        </div>
      ) : null}
    </div>
  )
}

function EventRow(props: { event: PromotionEvent }) {
  const { t } = useTranslation()
  const quota = Number(props.event.quota_delta || 0)
  const cash = Number(props.event.cash_amount_cents || 0)
  const currency = props.event.currency || 'CNY'
  const quotaAmount = quota
    ? `${quota > 0 ? '+' : '-'}${formatQuota(Math.abs(quota))}`
    : ''
  const cashAmount = cash
    ? `${cash > 0 ? '+' : '-'}${formatCashCents(Math.abs(cash), currency)}`
    : ''
  let amount = quotaAmount || cashAmount || '-'
  let cashContext = ''

  switch (props.event.event_type) {
    case 'commission_pending':
    case 'commission_settled':
    case 'commission_reversed':
    case 'commission_withdraw_paid':
      amount = cashAmount || '-'
      break
    case 'commission_withdraw_submitted':
    case 'commission_withdraw_approved':
    case 'commission_withdraw_rejected':
      amount = cash ? formatCashCents(Math.abs(cash), currency) : '-'
      break
    case 'commission_transferred':
      amount = quotaAmount || '-'
      if (cash) {
        cashContext = `${t('Cash commission')}: ${formatCashCents(
          Math.abs(cash),
          currency
        )}`
      }
      break
  }

  const detail = [
    formatTime(props.event.created_at),
    cashContext,
    props.event.remark,
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <ActivityRow
      title={t(
        EVENT_TITLES[props.event.event_type] ||
          props.event.title ||
          props.event.event_type
      )}
      detail={detail}
      status={props.event.status}
      amount={amount}
    />
  )
}

function renderRows(
  filter: PromotionActivityFilter,
  items: PromotionActivityItem[],
  t: TFunction
) {
  if (filter === 'all') {
    return (items as PromotionEvent[]).map((event) => (
      <EventRow key={event.id} event={event} />
    ))
  }
  if (filter === 'tasks') {
    return (items as GrowthReward[]).map((reward) => (
      <ActivityRow
        key={reward.id}
        title={t(rewardItemCopy[reward.item_code]?.title || reward.item_code)}
        detail={`${formatTime(reward.created_at)}${reward.remark ? ` · ${t(reward.remark)}` : ''}`}
        status={reward.status}
        amount={formatQuota(reward.reward_quota)}
      />
    ))
  }
  if (filter === 'submissions') {
    return (items as GrowthSubmission[]).map((submission) => (
      <ActivityRow
        key={submission.id}
        title={t(
          rewardItemCopy[submission.item_code]?.title || submission.item_code
        )}
        detail={`${submission.platform || t('Unknown platform')} · ${formatTime(submission.created_at)}${submission.review_note ? ` · ${submission.review_note}` : ''}`}
        status={submission.status}
      />
    ))
  }
  if (filter === 'referrals') {
    const titleByType: Record<string, string> = {
      register: 'Invitation registration reward',
      first_request: 'Invitation first request reward',
      first_topup: 'Invitation first top-up reward',
    }
    return (items as InvitationReward[]).map((reward) => (
      <ActivityRow
        key={reward.id}
        title={t(titleByType[reward.reward_type] || 'Referral reward')}
        detail={`${reward.invitee_name || `#${reward.invitee_id}`} · ${formatTime(reward.created_at)}${reward.remark ? ` · ${reward.remark}` : ''}`}
        status={reward.status}
        amount={formatQuota(reward.reward_quota)}
      />
    ))
  }
  if (filter === 'commissions') {
    return (items as PromotionCommissionLedger[]).map((commission) => (
      <ActivityRow
        key={commission.id}
        title={t('Top-up cash commission')}
        detail={t('{{time}} · {{quota}} API balance equivalent', {
          time: formatTime(commission.created_at),
          quota: formatQuota(commission.quota_equivalent || 0),
        })}
        status={commission.status}
        amount={formatCashCents(
          commission.net_amount_cents,
          commission.currency
        )}
      />
    ))
  }
  return (items as PromotionWithdrawal[]).map((withdrawal) => (
    <ActivityRow
      key={withdrawal.id}
      title={withdrawal.payout_method || t('Cash withdrawal')}
      detail={`${formatTime(withdrawal.applied_at)}${withdrawal.trade_no ? ` · ${withdrawal.trade_no}` : ''}`}
      status={withdrawal.status}
      amount={formatCashCents(withdrawal.net_amount_cents, withdrawal.currency)}
    />
  ))
}

type PromotionActivityRowsProps = {
  filter: PromotionActivityFilter
  items: PromotionActivityItem[]
}

export function PromotionActivityRows(props: PromotionActivityRowsProps) {
  const { t } = useTranslation()
  return <>{renderRows(props.filter, props.items, t)}</>
}
