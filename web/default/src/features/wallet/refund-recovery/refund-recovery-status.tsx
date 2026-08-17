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
import { AlertCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { formatMinorAmount } from '@/lib/currency'
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import { getRefundRecovery } from './api'
import type { RefundRecoveryStage } from './types'

const RECOVERY_PAGE_SIZE = 20
const SUPPORT_URL = 'https://docs.newapi.pro/support/community-interaction/'

function getStagePresentation(
  stage: RefundRecoveryStage,
  t: TFunction
): {
  label: string
  guidance: string
  variant: 'default' | 'secondary' | 'warning' | 'outline'
} {
  switch (stage) {
    case 'under_review':
      return {
        label: t('Under review'),
        guidance: t('No action is required while we review this refund.'),
        variant: 'secondary',
      }
    case 'repayment_required':
      return {
        label: t('Repayment required'),
        guidance: t(
          'Contact support to arrange repayment and provide the case reference.'
        ),
        variant: 'warning',
      }
    case 'final_review':
      return {
        label: t('Final review'),
        guidance: t(
          'Your repayment is recorded and final review is in progress.'
        ),
        variant: 'outline',
      }
    case 'resolved':
      return {
        label: t('Resolved'),
        guidance: t('Resolved'),
        variant: 'default',
      }
  }
}

export function RefundRecoveryStatus() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const recoveryQuery = useQuery({
    queryKey: ['wallet', 'refund-recovery', page],
    queryFn: ({ signal }) =>
      getRefundRecovery(page, RECOVERY_PAGE_SIZE, signal),
    placeholderData: (previousData) => previousData,
  })

  if (recoveryQuery.isLoading) {
    return (
      <div
        className='rounded-lg border p-4'
        role='status'
        aria-label={t('Loading refund recovery status')}
      >
        <div className='flex items-center gap-2'>
          <Skeleton className='size-4 shrink-0 rounded-full' />
          <Skeleton className='h-5 w-52 max-w-full' />
        </div>
        <Skeleton className='mt-3 h-4 w-full max-w-2xl' />
        <div className='mt-4 flex flex-wrap gap-6'>
          <Skeleton className='h-10 w-40' />
          <Skeleton className='h-10 w-40' />
        </div>
      </div>
    )
  }

  const recovery = recoveryQuery.data
  if (recoveryQuery.isError || !recovery) {
    return (
      <Alert variant='destructive'>
        <HugeiconsIcon
          icon={AlertCircleIcon}
          strokeWidth={2}
          aria-hidden='true'
        />
        <AlertTitle>{t('Unable to load refund recovery status')}</AlertTitle>
        <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
          <span>{t('Refresh the status and try again.')}</span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => recoveryQuery.refetch()}
            disabled={recoveryQuery.isFetching}
          >
            {t('Try again')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }

  const outstandingCash = recovery.outstanding_cash.filter(
    (debt) => debt.amount > 0
  )
  const pendingCases = recovery.items.filter(
    (refundCase) => refundCase.status === 'pending_review'
  )
  const hasOutstandingDebt =
    recovery.outstanding_quota > 0 || outstandingCash.length > 0
  const totalPages = Math.max(1, Math.ceil(recovery.total / recovery.page_size))

  if (!recovery.hold && !hasOutstandingDebt && pendingCases.length === 0) {
    return null
  }

  return (
    <Alert className='border-warning/40 bg-warning/5 py-3 sm:px-4 sm:py-4'>
      <HugeiconsIcon
        icon={AlertCircleIcon}
        strokeWidth={2}
        className='text-warning'
        aria-hidden='true'
      />
      <AlertTitle>
        {recovery.hold
          ? t('API access temporarily paused')
          : t('Refund review in progress')}
      </AlertTitle>
      <AlertDescription className='mt-1 flex flex-col gap-4 text-left text-pretty'>
        <p>
          {recovery.hold
            ? t(
                'New API requests are paused until the refund recovery is resolved.'
              )
            : t('A refund-related balance adjustment is being reviewed.')}
        </p>

        {recovery.hold ? (
          <div className='flex flex-col items-start justify-between gap-3 sm:flex-row sm:items-center'>
            <p className='max-w-2xl'>
              {t(
                'Contact support to resolve this refund recovery. Provide the case reference shown below.'
              )}
            </p>
            <Button
              variant='outline'
              render={<a href={SUPPORT_URL} target='_blank' rel='noreferrer' />}
              nativeButton={false}
              className='min-h-9 shrink-0'
            >
              {t('Contact support')}
            </Button>
          </div>
        ) : null}

        {hasOutstandingDebt ? (
          <dl className='flex flex-wrap gap-x-8 gap-y-3'>
            {recovery.outstanding_quota > 0 ? (
              <div className='min-w-40'>
                <dt className='text-muted-foreground text-xs'>
                  {t('Outstanding API balance')}
                </dt>
                <dd className='text-foreground mt-0.5 font-medium tabular-nums'>
                  {formatQuota(recovery.outstanding_quota)}
                </dd>
              </div>
            ) : null}
            {outstandingCash.map((debt) => (
              <div key={debt.currency} className='min-w-40'>
                <dt className='text-muted-foreground text-xs'>
                  {t('Outstanding cash')} · {debt.currency.toUpperCase()}
                </dt>
                <dd className='text-foreground mt-0.5 font-medium tabular-nums'>
                  {formatMinorAmount(debt.amount, debt.currency)}
                </dd>
              </div>
            ))}
          </dl>
        ) : null}

        {pendingCases.length > 0 ? (
          <>
            <Separator />
            <div>
              <div className='text-foreground mb-1 text-xs font-medium'>
                {t('Refund cases')}
              </div>
              <ul className='divide-border divide-y'>
                {pendingCases.map((refundCase) => {
                  const stage = getStagePresentation(refundCase.stage, t)
                  return (
                    <li
                      key={refundCase.reference}
                      className='flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-1 last:pb-0'
                    >
                      <div className='min-w-0'>
                        <div className='flex min-w-0 items-center gap-1.5'>
                          <span className='text-foreground font-mono text-xs font-medium'>
                            {refundCase.reference}
                          </span>
                          <CopyButton
                            value={refundCase.reference}
                            className='size-7'
                            aria-label={t('Copy case reference')}
                            tooltip={t('Copy case reference')}
                          />
                          <Badge variant={stage.variant}>{stage.label}</Badge>
                        </div>
                        <p className='text-muted-foreground mt-1 text-xs'>
                          {stage.guidance}
                        </p>
                      </div>
                      <time
                        className='text-muted-foreground text-xs tabular-nums'
                        dateTime={new Date(
                          refundCase.created_at * 1000
                        ).toISOString()}
                      >
                        {t('Opened {{time}}', {
                          time: formatTimestampToDate(refundCase.created_at),
                        })}
                      </time>
                    </li>
                  )
                })}
              </ul>
              {recovery.total > recovery.page_size ? (
                <div className='mt-3 flex flex-wrap items-center justify-between gap-2'>
                  <span className='text-muted-foreground text-xs tabular-nums'>
                    {t('Showing')}{' '}
                    {(recovery.page - 1) * recovery.page_size + 1}-
                    {Math.min(
                      recovery.page * recovery.page_size,
                      recovery.total
                    )}{' '}
                    {t('of')} {recovery.total}
                  </span>
                  <div className='flex gap-2'>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={page <= 1 || recoveryQuery.isFetching}
                      onClick={() => setPage((currentPage) => currentPage - 1)}
                    >
                      {t('Previous page')}
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={page >= totalPages || recoveryQuery.isFetching}
                      onClick={() => setPage((currentPage) => currentPage + 1)}
                    >
                      {t('Next page')}
                    </Button>
                  </div>
                </div>
              ) : null}
            </div>
          </>
        ) : null}
      </AlertDescription>
    </Alert>
  )
}
