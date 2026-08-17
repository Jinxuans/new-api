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
import {
  AlertCircleIcon,
  CheckmarkCircle02Icon,
  Wallet01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { TableCell, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { formatTime, statusVariant } from '@/features/promotion/shared'
import { formatMinorAmount } from '@/lib/currency'

import type {
  AdminPromotionWithdrawal,
  PromotionWithdrawalOperation,
} from './growth-withdrawal-review-types'
import { getWithdrawalPayoutAccount } from './growth-withdrawal-review-utils'

type GrowthWithdrawalReviewRowProps = {
  withdrawal: AdminPromotionWithdrawal
  busy: boolean
  tradeNo: string
  note: string
  onTradeNoChange: (value: string) => void
  onNoteChange: (value: string) => void
  onApprove: () => void
  onReject: () => void
  onInitiate: () => void
  onConfirmPaid: () => void
  onFail: () => void
}

function operationLabel(action: string) {
  switch (action) {
    case 'submitted':
      return 'Withdrawal submitted'
    case 'approved':
      return 'Withdrawal approved'
    case 'payout_initiated':
      return 'Payout started'
    case 'payout_failed':
      return 'Payout failed'
    case 'paid':
      return 'Payout completed'
    case 'rejected':
      return 'Withdrawal rejected'
    case 'cancelled_by_refund':
      return 'Withdrawal cancelled by refund'
    default:
      return action
  }
}

function actorLabel(operation: PromotionWithdrawalOperation) {
  if (operation.actor_type === 'legacy') return 'Historical import'
  if (operation.reconstructed) return 'Reconstructed record'

  switch (operation.actor_type) {
    case 'user':
      return 'User'
    case 'admin':
      return 'Admin'
    default:
      return 'System'
  }
}

export function GrowthWithdrawalReviewRow(
  props: GrowthWithdrawalReviewRowProps
) {
  const { t } = useTranslation()
  const withdrawal = props.withdrawal
  const isPendingReview = withdrawal.status === 'pending_review'
  const isApproved = withdrawal.status === 'approved'
  const isProcessing = withdrawal.status === 'processing'
  const isActionable = isPendingReview || isApproved || isProcessing
  const reviewTime = withdrawal.paid_at
    ? formatTime(withdrawal.paid_at)
    : formatTime(withdrawal.reviewed_at)

  return (
    <TableRow>
      <TableCell className='min-w-64 whitespace-normal'>
        <div className='flex flex-col gap-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='font-medium'>
              {formatMinorAmount(
                withdrawal.net_amount_cents,
                withdrawal.currency
              )}
            </span>
            <Badge variant={statusVariant(withdrawal.status)}>
              {t(withdrawal.status)}
            </Badge>
          </div>
          <div className='text-muted-foreground text-xs'>
            {t('User ID')}: {withdrawal.user_id} · {t('Applied at')}:{' '}
            {formatTime(withdrawal.applied_at)}
          </div>
          {withdrawal.trade_no ? (
            <div className='text-muted-foreground text-xs'>
              {t('Trade no')}: {withdrawal.trade_no}
            </div>
          ) : null}
          {withdrawal.review_note ? (
            <div className='text-muted-foreground text-xs'>
              {withdrawal.status === 'failed'
                ? t('Failure reason')
                : t('Review note')}
              : {withdrawal.review_note}
            </div>
          ) : null}
          {withdrawal.operations?.length ? (
            <details className='pt-1 text-xs'>
              <summary className='focus-visible:ring-ring cursor-pointer rounded-sm font-medium outline-none focus-visible:ring-2'>
                {t('{{count}} recorded steps', {
                  count: withdrawal.operations.length,
                })}
              </summary>
              <ol className='mt-2 flex flex-col gap-2'>
                {withdrawal.operations.map((operation) => (
                  <li key={operation.id} className='bg-muted/50 rounded-md p-2'>
                    <div className='font-medium'>
                      {t(operationLabel(operation.action))}
                    </div>
                    <div className='text-muted-foreground mt-1 break-words'>
                      {formatTime(operation.created_at)} ·{' '}
                      {t(actorLabel(operation))}
                      {!operation.reconstructed &&
                      operation.actor_type !== 'legacy'
                        ? ` #${operation.actor_id}`
                        : ''}
                      {operation.external_reference
                        ? ` · ${t('External reference')}: ${operation.external_reference}`
                        : ''}
                      {operation.note ? ` · ${operation.note}` : ''}
                    </div>
                  </li>
                ))}
              </ol>
            </details>
          ) : null}
        </div>
      </TableCell>

      <TableCell className='min-w-72 whitespace-normal'>
        <div className='flex flex-col gap-1.5 text-xs'>
          <div>
            {t('Payout method')}: {withdrawal.payout_method || '-'}
          </div>
          <div className='text-muted-foreground break-all'>
            {t('Payout account')}: {getWithdrawalPayoutAccount(withdrawal)}
          </div>
        </div>
      </TableCell>

      <TableCell className='min-w-72 whitespace-normal'>
        {isActionable ? (
          <FieldGroup className='gap-2'>
            {isApproved ? (
              <Field>
                <FieldLabel
                  htmlFor={`withdrawal-trade-no-${withdrawal.id}`}
                  className='sr-only'
                >
                  {t('Trade no')}
                </FieldLabel>
                <Input
                  id={`withdrawal-trade-no-${withdrawal.id}`}
                  aria-label={`${t('Trade no')} #${withdrawal.id}`}
                  value={props.tradeNo}
                  onChange={(event) =>
                    props.onTradeNoChange(event.target.value)
                  }
                  placeholder={t('Trade no')}
                  disabled={props.busy}
                />
              </Field>
            ) : null}
            <Field>
              <FieldLabel
                htmlFor={`withdrawal-note-${withdrawal.id}`}
                className='sr-only'
              >
                {isProcessing ? t('Failure reason') : t('Review note')}
              </FieldLabel>
              <Textarea
                id={`withdrawal-note-${withdrawal.id}`}
                aria-label={`${isProcessing ? t('Failure reason') : t('Review note')} #${withdrawal.id}`}
                value={props.note}
                onChange={(event) => props.onNoteChange(event.target.value)}
                placeholder={
                  isProcessing ? t('Failure reason') : t('Review note')
                }
                className='min-h-16'
                disabled={props.busy}
              />
            </Field>
          </FieldGroup>
        ) : (
          <div className='text-muted-foreground text-xs'>{reviewTime}</div>
        )}
      </TableCell>

      <TableCell className='text-right'>
        {isActionable ? (
          <div className='flex flex-wrap justify-end gap-2'>
            {isPendingReview ? (
              <Button
                type='button'
                size='sm'
                aria-label={`${t('Approve request')} #${withdrawal.id}`}
                onClick={props.onApprove}
                disabled={props.busy}
              >
                <HugeiconsIcon
                  icon={CheckmarkCircle02Icon}
                  strokeWidth={2}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
                {t('Approve request')}
              </Button>
            ) : null}
            {isApproved ? (
              <Button
                type='button'
                size='sm'
                aria-label={`${t('Start payout')} #${withdrawal.id}`}
                onClick={props.onInitiate}
                disabled={props.busy}
              >
                <HugeiconsIcon
                  icon={Wallet01Icon}
                  strokeWidth={2}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
                {t('Start payout')}
              </Button>
            ) : null}
            {isProcessing ? (
              <>
                <Button
                  type='button'
                  size='sm'
                  aria-label={`${t('Confirm payout completed')} #${withdrawal.id}`}
                  onClick={props.onConfirmPaid}
                  disabled={props.busy}
                >
                  <HugeiconsIcon
                    icon={CheckmarkCircle02Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                  {t('Confirm payout completed')}
                </Button>
                <Button
                  type='button'
                  variant='destructive'
                  size='sm'
                  aria-label={`${t('Mark payout failed')} #${withdrawal.id}`}
                  onClick={props.onFail}
                  disabled={props.busy}
                >
                  <HugeiconsIcon
                    icon={AlertCircleIcon}
                    strokeWidth={2}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                  {t('Mark payout failed')}
                </Button>
              </>
            ) : null}
            {isPendingReview || isApproved ? (
              <Button
                type='button'
                variant='destructive'
                size='sm'
                aria-label={`${t('Reject request')} #${withdrawal.id}`}
                onClick={props.onReject}
                disabled={props.busy}
              >
                <HugeiconsIcon
                  icon={AlertCircleIcon}
                  strokeWidth={2}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
                {t('Reject request')}
              </Button>
            ) : null}
          </div>
        ) : (
          <span className='text-muted-foreground text-xs'>{t('Reviewed')}</span>
        )}
      </TableCell>
    </TableRow>
  )
}
