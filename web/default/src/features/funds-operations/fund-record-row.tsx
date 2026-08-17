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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { TableCell, TableRow } from '@/components/ui/table'
import {
  getPromotionFundAccountLabelKey,
  getPromotionFundKindTitleKey,
  getPromotionFundSourceLabelKey,
} from '@/features/promotion/fund-record-copy'
import { formatTime } from '@/features/promotion/shared'
import { formatMinorAmount } from '@/lib/currency'
import { formatQuota } from '@/lib/format'

import type {
  AdminPromotionFundRecord,
  AdminPromotionFundRecordLeg,
} from './fund-record-types'

function formatFinancialAmount(
  leg: AdminPromotionFundRecordLeg,
  amount: number
): string | null {
  if (leg.asset === 'cash') {
    return leg.currency ? formatMinorAmount(amount, leg.currency) : null
  }
  return formatQuota(amount)
}

function formatLegAmount(leg: AdminPromotionFundRecordLeg): string | null {
  const amount = Math.abs(Number(leg.amount || 0))
  const formatted = formatFinancialAmount(leg, amount)
  if (formatted === null) return null
  return `${leg.amount > 0 ? '+' : '-'}${formatted}`
}

function formatBalanceAfter(leg: AdminPromotionFundRecordLeg): string | null {
  if (leg.balance_after === null || leg.balance_after === undefined) return null
  return formatFinancialAmount(leg, leg.balance_after)
}

export function FundRecordRow(props: { record: AdminPromotionFundRecord }) {
  const { t } = useTranslation()
  const record = props.record
  const sourceReference = [
    record.source_id ? `#${record.source_id}` : '',
    record.source_key,
  ]
    .filter(Boolean)
    .join(' · ')
  const actor = [
    record.actor_type,
    record.actor_id ? `#${record.actor_id}` : '',
    record.actor_ref,
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <TableRow>
      <TableCell className='min-w-36 align-top'>
        <div className='font-medium tabular-nums'>#{record.id}</div>
        <div className='text-muted-foreground mt-1 text-xs'>
          {formatTime(record.occurred_at)}
        </div>
      </TableCell>
      <TableCell className='min-w-56 align-top'>
        <div className='font-medium'>
          {t(getPromotionFundKindTitleKey(record.kind))}
        </div>
        <code className='text-muted-foreground mt-1 block text-xs break-all'>
          {record.kind}
        </code>
        <div className='mt-2 flex flex-wrap items-center gap-1.5 text-xs'>
          <Badge variant='outline'>{t('Source')}</Badge>
          <span className='text-muted-foreground'>
            {t(getPromotionFundSourceLabelKey(record.source_type || ''))}
          </span>
        </div>
        {record.source_type || sourceReference ? (
          <div className='text-muted-foreground mt-1 flex flex-wrap gap-1 text-xs'>
            {record.source_type ? (
              <code className='break-all'>{record.source_type}</code>
            ) : null}
            {record.source_type && sourceReference ? <span>·</span> : null}
            {sourceReference ? (
              <span className='break-all'>{sourceReference}</span>
            ) : null}
          </div>
        ) : null}
      </TableCell>
      <TableCell className='min-w-72 align-top'>
        <ul className='divide-y'>
          {(record.legs || []).map((leg) => {
            const legAmount = formatLegAmount(leg)
            const hasBalanceAfter =
              leg.balance_after !== null && leg.balance_after !== undefined
            const balanceAfter = formatBalanceAfter(leg)
            return (
              <li
                key={leg.id}
                className='flex items-start justify-between gap-4 py-1.5 first:pt-0 last:pb-0'
              >
                <div className='min-w-0 text-xs'>
                  <div className='flex flex-wrap items-center gap-1.5'>
                    <Badge variant={leg.amount > 0 ? 'secondary' : 'outline'}>
                      {t(leg.amount > 0 ? 'Credit' : 'Debit')}
                    </Badge>
                    <span className='font-medium'>
                      {t(getPromotionFundAccountLabelKey(leg.account))}
                    </span>
                  </div>
                  <code className='text-muted-foreground break-all'>
                    {leg.account}
                  </code>
                </div>
                <div className='shrink-0 text-right text-xs tabular-nums'>
                  <div className='font-semibold'>
                    {legAmount ?? t('Payment details unavailable')}
                  </div>
                  {hasBalanceAfter ? (
                    <div className='text-muted-foreground mt-0.5'>
                      {t('Balance after')}:{' '}
                      {balanceAfter ?? t('Payment details unavailable')}
                    </div>
                  ) : null}
                </div>
              </li>
            )
          })}
        </ul>
      </TableCell>
      <TableCell className='min-w-64 align-top text-xs'>
        <dl className='grid gap-1.5 break-words'>
          <div>
            <dt className='text-muted-foreground inline'>{t('Key')}:</dt>{' '}
            <dd className='inline font-mono'>{record.transaction_key}</dd>
          </div>
          {record.external_ref ? (
            <div>
              <dt className='text-muted-foreground inline'>
                {t('External reference')}:
              </dt>{' '}
              <dd className='inline'>{record.external_ref}</dd>
            </div>
          ) : null}
          {record.reverses_transaction_id ? (
            <div>
              <dt className='text-muted-foreground inline'>
                {t('Reverses journal record')}:
              </dt>{' '}
              <dd className='inline'>#{record.reverses_transaction_id}</dd>
            </div>
          ) : null}
          {actor ? (
            <div>
              <dt className='text-muted-foreground inline'>{t('Details')}:</dt>{' '}
              <dd className='inline'>{actor}</dd>
            </div>
          ) : null}
          {record.remark ? (
            <div>
              <dt className='text-muted-foreground inline'>{t('Remark')}:</dt>{' '}
              <dd className='inline'>{record.remark}</dd>
            </div>
          ) : null}
        </dl>
      </TableCell>
    </TableRow>
  )
}
