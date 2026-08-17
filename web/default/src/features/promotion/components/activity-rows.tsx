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
  getPromotionFundAccountLabelKey,
  getPromotionFundKindTitleKey,
  getPromotionFundSourceLabelKey,
} from '@/features/promotion/fund-record-copy'
import {
  formatTime,
  rewardItemCopy,
  statusVariant,
  type GrowthReward,
  type GrowthSubmission,
  type InvitationReward,
  type PromotionCommissionLedger,
  type PromotionFundTransaction,
  type PromotionFundTransactionLeg,
  type PromotionWithdrawal,
} from '@/features/promotion/shared'
import { formatMinorAmount } from '@/lib/currency'
import { formatQuota } from '@/lib/format'

import type { PromotionActivityFilter, PromotionActivityItem } from '../api'

function withStableKeys<T>(
  records: T[],
  getBaseKey: (record: T) => string
): { key: string; record: T }[] {
  const occurrences = new Map<string, number>()
  return records.map((record) => {
    const baseKey = getBaseKey(record)
    const occurrence = (occurrences.get(baseKey) || 0) + 1
    occurrences.set(baseKey, occurrence)
    return { key: `${baseKey}:${occurrence}`, record }
  })
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

function formatFundLegAmount(leg: PromotionFundTransactionLeg) {
  const absoluteAmount = Math.abs(Number(leg.amount || 0))
  const amount =
    leg.asset === 'cash'
      ? formatMinorAmount(absoluteAmount, leg.currency || 'CNY')
      : formatQuota(absoluteAmount)
  return `${leg.amount > 0 ? '+' : '-'}${amount}`
}

function FundTransactionRow(props: { transaction: PromotionFundTransaction }) {
  const { t } = useTranslation()
  const transaction = props.transaction
  const sourceLabel = getPromotionFundSourceLabelKey(transaction.source || '')

  return (
    <article className='flex flex-col gap-3 p-3'>
      <header className='flex flex-col gap-1 sm:flex-row sm:items-start sm:justify-between sm:gap-4'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-2'>
            <h3 className='text-sm font-medium'>
              {t(getPromotionFundKindTitleKey(transaction.kind))}
            </h3>
            <Badge variant='outline'>
              {t('{{count}} account changes', {
                count: transaction.legs?.length || 0,
              })}
            </Badge>
          </div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {formatTime(transaction.occurred_at)} · {t('Source')}:{' '}
            {t(sourceLabel)}
          </p>
        </div>
      </header>

      <ul className='divide-y rounded-md border'>
        {withStableKeys(transaction.legs || [], (leg) =>
          [
            leg.account,
            leg.asset,
            leg.currency || '',
            leg.amount,
            leg.balance_after ?? '',
          ].join(':')
        ).map(({ key, record: leg }) => {
          const account = t(getPromotionFundAccountLabelKey(leg.account))
          let direction = t('Debited from {{account}}', { account })
          if (leg.account === 'refund_debt') {
            direction =
              leg.amount > 0
                ? t('Debt increased in {{account}}', { account })
                : t('Debt reduced in {{account}}', { account })
          } else if (leg.amount > 0) {
            direction = t('Credited to {{account}}', { account })
          }
          return (
            <li
              key={key}
              className='flex flex-col gap-1 px-3 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4'
            >
              <div className='min-w-0 text-xs font-medium'>{direction}</div>
              <div className='shrink-0 text-xs tabular-nums sm:text-right'>
                <div className='font-semibold'>{formatFundLegAmount(leg)}</div>
                {leg.balance_after !== null &&
                leg.balance_after !== undefined ? (
                  <div className='text-muted-foreground mt-0.5'>
                    {t('Balance after')}:{' '}
                    {leg.asset === 'cash'
                      ? formatMinorAmount(
                          leg.balance_after,
                          leg.currency || 'CNY'
                        )
                      : formatQuota(leg.balance_after)}
                  </div>
                ) : null}
              </div>
            </li>
          )
        })}
      </ul>

      {transaction.external_ref ? (
        <details className='text-xs'>
          <summary className='focus-visible:ring-ring cursor-pointer rounded-sm font-medium outline-none focus-visible:ring-2'>
            {t('Audit references')}
          </summary>
          <dl className='text-muted-foreground mt-2 grid gap-1 break-words'>
            <div>
              <dt className='inline font-medium'>{t('External reference')}:</dt>{' '}
              <dd className='inline'>{transaction.external_ref}</dd>
            </div>
          </dl>
        </details>
      ) : null}
    </article>
  )
}

function renderRows(
  filter: PromotionActivityFilter,
  items: PromotionActivityItem[],
  t: TFunction
) {
  if (filter === 'funds') {
    return withStableKeys(items as PromotionFundTransaction[], (transaction) =>
      [
        transaction.kind,
        transaction.occurred_at || transaction.created_at || '',
        transaction.external_ref || '',
        JSON.stringify(transaction.legs || []),
      ].join(':')
    ).map(({ key, record: transaction }) => (
      <FundTransactionRow key={key} transaction={transaction} />
    ))
  }
  if (filter === 'tasks') {
    return withStableKeys(items as GrowthReward[], (reward) =>
      [
        reward.item_code,
        reward.created_at,
        reward.settled_at || '',
        reward.reward_quota,
        reward.status,
      ].join(':')
    ).map(({ key, record: reward }) => (
      <ActivityRow
        key={key}
        title={t(rewardItemCopy[reward.item_code]?.title || reward.item_code)}
        detail={formatTime(reward.created_at)}
        status={reward.status}
        amount={formatQuota(reward.reward_quota)}
      />
    ))
  }
  if (filter === 'submissions') {
    return withStableKeys(items as GrowthSubmission[], (submission) =>
      [
        submission.item_code,
        submission.created_at,
        submission.url || '',
        submission.status,
      ].join(':')
    ).map(({ key, record: submission }) => (
      <ActivityRow
        key={key}
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
    return withStableKeys(items as InvitationReward[], (reward) =>
      [
        reward.reward_type,
        reward.created_at,
        reward.settled_at || '',
        reward.invitee_name || '',
        reward.reward_quota,
        reward.status,
      ].join(':')
    ).map(({ key, record: reward }) => (
      <ActivityRow
        key={key}
        title={t(titleByType[reward.reward_type] || 'Referral reward')}
        detail={`${reward.invitee_name || t('User')} · ${formatTime(reward.created_at)}`}
        status={reward.status}
        amount={formatQuota(reward.reward_quota)}
      />
    ))
  }
  if (filter === 'commissions') {
    return withStableKeys(items as PromotionCommissionLedger[], (commission) =>
      [
        commission.created_at,
        commission.currency,
        commission.net_amount_cents,
        commission.status,
        commission.available_at || '',
        commission.settled_at || '',
        commission.reversed_at || '',
      ].join(':')
    ).map(({ key, record: commission }) => (
      <ActivityRow
        key={key}
        title={t('Top-up cash commission')}
        detail={t('{{time}} · {{quota}} API balance equivalent', {
          time: formatTime(commission.created_at),
          quota: formatQuota(commission.quota_equivalent || 0),
        })}
        status={commission.status}
        amount={formatMinorAmount(
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
      amount={formatMinorAmount(
        withdrawal.net_amount_cents,
        withdrawal.currency
      )}
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
