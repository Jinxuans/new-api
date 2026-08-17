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
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Copy01Icon,
  SearchIcon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { formatCurrencyFromUSD, formatMinorAmount } from '@/lib/currency'
import { formatQuota } from '@/lib/format'

import { useBillingHistory } from '../../hooks/use-billing-history'
import {
  getStatusConfig,
  getPaymentMethodName,
  formatTimestamp,
} from '../../lib/billing'

interface BillingHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function BillingHistoryDialog({
  open,
  onOpenChange,
}: BillingHistoryDialogProps) {
  const { t } = useTranslation()
  const {
    records,
    total,
    page,
    pageSize,
    keyword,
    loading,
    completing,
    isAdmin,
    handlePageChange,
    handlePageSizeChange,
    handleSearch,
    handleCompleteOrder,
  } = useBillingHistory()

  const [confirmTradeNo, setConfirmTradeNo] = useState<string | null>(null)
  const [completionReason, setCompletionReason] = useState('')
  const [completionReasonTouched, setCompletionReasonTouched] = useState(false)
  const { copyToClipboard, copiedText } = useCopyToClipboard({ notify: false })

  const totalPages = Math.ceil(total / pageSize)

  const handleConfirmComplete = async () => {
    const reason = completionReason.trim()
    if (confirmTradeNo && reason) {
      const success = await handleCompleteOrder(confirmTradeNo, reason)
      if (success) {
        setConfirmTradeNo(null)
        setCompletionReason('')
        setCompletionReasonTouched(false)
      }
    }
  }

  const closeCompletionDialog = () => {
    if (completing) return
    setConfirmTradeNo(null)
    setCompletionReason('')
    setCompletionReasonTouched(false)
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title={t('Billing History')}
        description={
          isAdmin
            ? t('View your topup transaction records and payment history')
            : t('Showing your top-ups from the last 30 days.')
        }
        contentClassName='flex max-h-[calc(100dvh-2rem)] flex-col max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-4xl'
        contentHeight='auto'
        bodyClassName='space-y-3'
      >
        <div className='min-h-0 space-y-3'>
          {/* Search and Filter Bar */}
          <div className='flex items-center gap-2'>
            <div className='relative flex-1'>
              <HugeiconsIcon
                icon={SearchIcon}
                strokeWidth={2}
                className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2'
                aria-hidden='true'
              />
              <Input
                placeholder={t('Search by order number...')}
                aria-label={t('Search by order number...')}
                value={keyword}
                onChange={(e) => handleSearch(e.target.value)}
                className='h-9 pl-10'
              />
            </div>
            <Select
              items={[
                { value: '10', label: t('10 / page') },
                { value: '20', label: t('20 / page') },
                { value: '50', label: t('50 / page') },
                { value: '100', label: t('100 / page') },
              ]}
              value={pageSize.toString()}
              onValueChange={(value) =>
                value !== null && handlePageSizeChange(Number.parseInt(value))
              }
            >
              <SelectTrigger className='h-9 w-[92px] sm:w-32'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='10'>{t('10 / page')}</SelectItem>
                  <SelectItem value='20'>{t('20 / page')}</SelectItem>
                  <SelectItem value='50'>{t('50 / page')}</SelectItem>
                  <SelectItem value='100'>{t('100 / page')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          {/* Records List */}
          <div className='max-h-[min(54vh,520px)] overflow-y-auto pr-1'>
            {loading && (
              <div className='space-y-3'>
                {['first', 'second', 'third', 'fourth', 'fifth'].map(
                  (skeletonKey) => (
                    <div
                      key={skeletonKey}
                      className='rounded-lg border p-3 sm:p-4'
                    >
                      <div className='flex items-start justify-between'>
                        <div className='flex-1 space-y-2'>
                          <Skeleton className='h-4 w-48' />
                          <Skeleton className='h-3 w-32' />
                        </div>
                        <Skeleton className='h-5 w-16' />
                      </div>
                      <div className='mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4'>
                        <Skeleton className='h-3 w-full' />
                        <Skeleton className='h-3 w-full' />
                        <Skeleton className='h-3 w-full' />
                      </div>
                    </div>
                  )
                )}
              </div>
            )}
            {!loading && records.length === 0 && (
              <div className='text-muted-foreground flex min-h-40 flex-col items-center justify-center py-10 text-center'>
                <p className='text-sm font-medium'>
                  {t('No billing records found')}
                </p>
                <p className='mt-1 text-xs'>
                  {keyword
                    ? t('Try adjusting your search')
                    : t('Your transaction history will appear here')}
                </p>
              </div>
            )}
            {!loading && records.length > 0 && (
              <div className='space-y-3'>
                {records.map((record) => {
                  const statusConfig = getStatusConfig(record.status)
                  const isSubscription = record.purpose === 'subscription'
                  let refundStatusLabel = ''
                  if (record.refund_status === 'partial') {
                    refundStatusLabel = 'Partially refunded'
                  } else if (record.refund_status === 'full') {
                    refundStatusLabel = 'Fully refunded'
                  } else if (record.refund_status === 'disputed') {
                    refundStatusLabel = 'Payment disputed'
                  }
                  let creditedValue = t('Subscription entitlement')
                  if (!isSubscription && record.credited_quota != null) {
                    creditedValue = formatQuota(record.credited_quota)
                  } else if (!isSubscription) {
                    creditedValue = formatCurrencyFromUSD(record.amount, {
                      digitsLarge: 2,
                      digitsSmall: 2,
                      abbreviate: false,
                    })
                  }
                  return (
                    <div
                      key={record.id}
                      className='rounded-lg border p-3 sm:p-4'
                    >
                      {/* Header Row */}
                      <div className='flex items-start justify-between gap-2'>
                        <div className='flex-1 space-y-1'>
                          <div className='flex min-w-0 items-center gap-2'>
                            <code className='text-foreground truncate font-mono text-sm'>
                              {record.trade_no}
                            </code>
                            <Button
                              variant='ghost'
                              size='sm'
                              className='size-7 p-0'
                              onClick={() => copyToClipboard(record.trade_no)}
                              aria-label={t('Copy order number')}
                            >
                              {copiedText === record.trade_no ? (
                                <HugeiconsIcon
                                  icon={Tick02Icon}
                                  strokeWidth={2}
                                />
                              ) : (
                                <HugeiconsIcon
                                  icon={Copy01Icon}
                                  strokeWidth={2}
                                />
                              )}
                            </Button>
                            {isAdmin && record.user_id != null && (
                              <StatusBadge
                                label={`${t('User ID')}: ${record.user_id}`}
                                variant='neutral'
                                size='sm'
                                copyText={String(record.user_id)}
                              />
                            )}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            {formatTimestamp(record.create_time)}
                          </div>
                        </div>
                        <div className='flex flex-wrap justify-end gap-1.5'>
                          <StatusBadge
                            label={t(statusConfig.label)}
                            variant={statusConfig.variant}
                            showDot
                            copyable={false}
                          />
                          {refundStatusLabel ? (
                            <StatusBadge
                              label={t(refundStatusLabel)}
                              variant={
                                record.refund_status === 'partial'
                                  ? 'warning'
                                  : 'danger'
                              }
                              copyable={false}
                            />
                          ) : null}
                        </div>
                      </div>

                      {/* Details Grid */}
                      <div className='mt-3 grid grid-cols-2 gap-3 sm:mt-4 sm:grid-cols-4 sm:gap-4'>
                        <div className='space-y-1'>
                          <Label className='text-muted-foreground text-xs'>
                            {t('Payment Method')}
                          </Label>
                          <div className='text-sm font-medium'>
                            {getPaymentMethodName(record.payment_method, t)}
                          </div>
                        </div>
                        <div className='space-y-1'>
                          <Label className='text-muted-foreground text-xs'>
                            {t('Purpose')}
                          </Label>
                          <div className='text-sm font-medium'>
                            {t(
                              isSubscription
                                ? 'Subscription purchase'
                                : 'API balance top-up'
                            )}
                          </div>
                        </div>
                        <div className='space-y-1'>
                          <Label className='text-muted-foreground text-xs'>
                            {t('Paid amount')}
                          </Label>
                          <div className='text-sm font-semibold tabular-nums'>
                            {record.paid_amount_verified &&
                            record.paid_currency &&
                            record.paid_amount_minor != null
                              ? formatMinorAmount(
                                  record.paid_amount_minor,
                                  record.paid_currency
                                )
                              : t('Payment details unavailable')}
                          </div>
                        </div>
                        <div className='space-y-1'>
                          <Label className='text-muted-foreground text-xs'>
                            {isSubscription
                              ? t('Purchased item')
                              : t('Credited API balance')}
                          </Label>
                          <div className='text-sm font-semibold tabular-nums'>
                            {creditedValue}
                          </div>
                        </div>
                      </div>

                      {record.refund_status ? (
                        <div className='bg-muted/40 mt-3 grid grid-cols-1 gap-2 rounded-md p-3 sm:grid-cols-2'>
                          <div>
                            <div className='text-muted-foreground text-xs'>
                              {t('Refunded amount')}
                            </div>
                            <div className='mt-0.5 text-sm font-medium tabular-nums'>
                              {record.paid_currency &&
                              record.refunded_amount_minor != null
                                ? formatMinorAmount(
                                    record.refunded_amount_minor,
                                    record.paid_currency
                                  )
                                : t('Payment details unavailable')}
                            </div>
                          </div>
                          {record.refunded_quota != null &&
                          record.refunded_quota > 0 ? (
                            <div>
                              <div className='text-muted-foreground text-xs'>
                                {t('Refunded API balance')}
                              </div>
                              <div className='mt-0.5 text-sm font-medium tabular-nums'>
                                {formatQuota(record.refunded_quota)}
                              </div>
                            </div>
                          ) : null}
                        </div>
                      ) : null}

                      {/* Admin Actions */}
                      {isAdmin && record.status === 'pending' && (
                        <div className='mt-4 flex justify-end'>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setConfirmTradeNo(record.trade_no)}
                            disabled={completing}
                          >
                            {t('Complete Order')}
                          </Button>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            )}
          </div>

          {/* Pagination */}
          {!loading && records.length > 0 && (
            <div className='flex flex-col items-center gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between'>
              <div className='text-muted-foreground text-xs sm:text-sm'>
                {t('Showing')} {(page - 1) * pageSize + 1}-
                {Math.min(page * pageSize, total)} {t('of')} {total}
              </div>
              <div className='flex items-center gap-2'>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => handlePageChange(page - 1)}
                  disabled={page <= 1}
                  className='h-8 w-8 p-0'
                  aria-label={t('Previous page')}
                >
                  <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
                </Button>
                <div className='text-muted-foreground flex items-center gap-1 text-sm'>
                  <span className='font-medium'>{page}</span>
                  <span>/</span>
                  <span>{totalPages}</span>
                </div>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => handlePageChange(page + 1)}
                  disabled={page >= totalPages}
                  className='h-8 w-8 p-0'
                  aria-label={t('Next page')}
                >
                  <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} />
                </Button>
              </div>
            </div>
          )}
        </div>
      </Dialog>

      <Dialog
        open={!!confirmTradeNo}
        onOpenChange={(open) => !open && closeCompletionDialog()}
        title={t('Complete Order')}
        description={t(
          'Are you sure you want to manually complete this order? The user will be credited with the corresponding quota.'
        )}
        contentClassName='sm:max-w-lg'
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              disabled={completing}
              onClick={closeCompletionDialog}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={completing || completionReason.trim() === ''}
              onClick={handleConfirmComplete}
            >
              {completing ? t('Processing...') : t('Confirm')}
            </Button>
          </>
        }
      >
        <FieldGroup>
          <Field
            data-invalid={
              (completionReasonTouched && completionReason.trim() === '') ||
              undefined
            }
          >
            <FieldLabel htmlFor='manual-topup-reason'>{t('Reason')}</FieldLabel>
            <Textarea
              id='manual-topup-reason'
              value={completionReason}
              maxLength={1000}
              className='min-h-24'
              aria-invalid={
                (completionReasonTouched && completionReason.trim() === '') ||
                undefined
              }
              disabled={completing}
              required
              onBlur={() => setCompletionReasonTouched(true)}
              onChange={(event) => setCompletionReason(event.target.value)}
            />
            <FieldDescription>
              {t('Explain the evidence and decision for the audit record.')}
            </FieldDescription>
            {completionReasonTouched && completionReason.trim() === '' ? (
              <FieldError>{t('Reason is required.')}</FieldError>
            ) : null}
          </Field>
        </FieldGroup>
      </Dialog>
    </>
  )
}
