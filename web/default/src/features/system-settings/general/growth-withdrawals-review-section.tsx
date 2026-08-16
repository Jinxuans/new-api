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
import { Banknote, CheckCircle2, RefreshCw, XCircle } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import {
  formatCashCents,
  formatTime,
  getItems,
  statusVariant,
  type PromotionWithdrawal,
} from '@/features/promotion/shared'
import { api } from '@/lib/api'

import { SettingsSection } from '../components/settings-section'

type AdminPromotionWithdrawal = PromotionWithdrawal & {
  user_id: number
  reviewer_id?: number
  payout_account_snapshot?: string
}

type WithdrawalStatusFilter =
  | 'pending_review'
  | 'approved'
  | 'paid'
  | 'rejected'
  | 'failed'
  | 'all'

const PAGE_SIZE = 20

function getPayoutAccount(withdrawal: AdminPromotionWithdrawal) {
  if (!withdrawal.payout_account_snapshot) return '-'
  try {
    const snapshot = JSON.parse(withdrawal.payout_account_snapshot) as {
      payout_account?: string
    }
    return snapshot.payout_account || '-'
  } catch {
    return '-'
  }
}

export function GrowthWithdrawalsReviewSection() {
  const { t } = useTranslation()
  const [withdrawals, setWithdrawals] = useState<AdminPromotionWithdrawal[]>([])
  const [loading, setLoading] = useState(true)
  const [reviewingId, setReviewingId] = useState<number | null>(null)
  const [statusFilter, setStatusFilter] =
    useState<WithdrawalStatusFilter>('pending_review')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [confirmation, setConfirmation] = useState<{
    action: 'approve' | 'paid'
    withdrawal: AdminPromotionWithdrawal
  } | null>(null)
  const [tradeNoById, setTradeNoById] = useState<Record<number, string>>({})
  const [noteById, setNoteById] = useState<Record<number, string>>({})
  const loadRequestIdRef = useRef(0)

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(total / PAGE_SIZE)),
    [total]
  )

  const loadWithdrawals = useCallback(async () => {
    const requestId = ++loadRequestIdRef.current
    try {
      setLoading(true)
      const res = await api.get('/api/growth/admin/withdrawals', {
        params: { p: page, page_size: PAGE_SIZE, status: statusFilter },
      })
      if (requestId !== loadRequestIdRef.current) return
      setWithdrawals(getItems<AdminPromotionWithdrawal>(res.data))
      setTotal(Number(res.data?.data?.total || 0))
    } finally {
      if (requestId === loadRequestIdRef.current) {
        setLoading(false)
      }
    }
  }, [page, statusFilter])

  useEffect(() => {
    loadWithdrawals()
  }, [loadWithdrawals])

  const updateTradeNo = (id: number, value: string) => {
    setTradeNoById((current) => ({ ...current, [id]: value }))
  }

  const updateNote = (id: number, value: string) => {
    setNoteById((current) => ({ ...current, [id]: value }))
  }

  const approveWithdrawal = async () => {
    if (!confirmation || confirmation.action !== 'approve') return
    const withdrawal = confirmation.withdrawal
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/approve`,
        {
          review_note: noteById[withdrawal.id] || '',
        }
      )
      if (res.data?.success) {
        toast.success(t('Withdrawal request approved'))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const rejectWithdrawal = async (withdrawal: AdminPromotionWithdrawal) => {
    const note = noteById[withdrawal.id]?.trim()
    if (!note) {
      toast.error(t('Review note is required'))
      return
    }
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/reject`,
        {
          review_note: note,
        }
      )
      if (res.data?.success) {
        toast.success(t('Withdrawal request rejected'))
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const requestMarkPaid = (withdrawal: AdminPromotionWithdrawal) => {
    const tradeNo = tradeNoById[withdrawal.id]?.trim()
    if (!tradeNo) {
      toast.error(t('Trade no is required'))
      return
    }
    setConfirmation({ action: 'paid', withdrawal })
  }

  const markWithdrawalPaid = async () => {
    if (!confirmation || confirmation.action !== 'paid') return
    const withdrawal = confirmation.withdrawal
    const tradeNo = tradeNoById[withdrawal.id]?.trim()
    if (!tradeNo) return
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/paid`,
        {
          trade_no: tradeNo,
          review_note: noteById[withdrawal.id] || '',
        }
      )
      if (res.data?.success) {
        toast.success(t('Withdrawal marked paid'))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const confirmationWithdrawal = confirmation?.withdrawal
  const confirmsPayment = confirmation?.action === 'paid'
  const confirmationTitle = confirmsPayment
    ? t('Mark this withdrawal paid?')
    : t('Approve this withdrawal request?')
  const confirmationAction = confirmsPayment
    ? t('Confirm payment')
    : t('Approve request')
  const confirmationImpact = confirmsPayment
    ? t(
        'This records an offline payment of {{amount}} and closes the linked commission entries. This action cannot be undone here.',
        {
          amount: formatCashCents(
            confirmationWithdrawal?.net_amount_cents,
            confirmationWithdrawal?.currency
          ),
        }
      )
    : t(
        'This approves a withdrawal of {{amount}} for offline payment. Verify the payout account before continuing.',
        {
          amount: formatCashCents(
            confirmationWithdrawal?.net_amount_cents,
            confirmationWithdrawal?.currency
          ),
        }
      )

  return (
    <SettingsSection
      title={t('Withdrawal Reviews')}
      description={t(
        'Review submitted cash withdrawal requests and mark paid after offline settlement.'
      )}
    >
      <div className='space-y-4 rounded-lg border p-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant={total > 0 ? 'secondary' : 'outline'}>
              {t('{{count}} results', { count: total })}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {t('Latest withdrawal requests are shown first.')}
            </span>
          </div>
          <div className='flex items-center gap-2'>
            <Select
              items={[
                { value: 'pending_review', label: t('Pending review') },
                { value: 'approved', label: t('Approved awaiting payout') },
                { value: 'paid', label: t('Paid') },
                { value: 'rejected', label: t('Rejected') },
                { value: 'failed', label: t('Failed') },
                { value: 'all', label: t('All statuses') },
              ]}
              value={statusFilter}
              onValueChange={(value) => {
                setPage(1)
                setStatusFilter(value as WithdrawalStatusFilter)
              }}
            >
              <SelectTrigger
                className='w-48'
                aria-label={t('Withdrawal status')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='pending_review'>
                    {t('Pending review')}
                  </SelectItem>
                  <SelectItem value='approved'>
                    {t('Approved awaiting payout')}
                  </SelectItem>
                  <SelectItem value='paid'>{t('Paid')}</SelectItem>
                  <SelectItem value='rejected'>{t('Rejected')}</SelectItem>
                  <SelectItem value='failed'>{t('Failed')}</SelectItem>
                  <SelectItem value='all'>{t('All statuses')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={loadWithdrawals}
              disabled={loading}
            >
              <RefreshCw className='size-4' />
              {t('Refresh')}
            </Button>
          </div>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Withdrawal')}</TableHead>
              <TableHead>{t('Payout')}</TableHead>
              <TableHead>{t('Review')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {withdrawals.length > 0 ? (
              withdrawals.map((withdrawal) => {
                const isPendingReview = withdrawal.status === 'pending_review'
                const isApproved = withdrawal.status === 'approved'
                const canReview = isPendingReview || isApproved
                const isReviewing = reviewingId === withdrawal.id
                let reviewTime = '-'
                if (withdrawal.paid_at) {
                  reviewTime = formatTime(withdrawal.paid_at)
                } else if (withdrawal.reviewed_at) {
                  reviewTime = formatTime(withdrawal.reviewed_at)
                }
                return (
                  <TableRow key={withdrawal.id}>
                    <TableCell className='min-w-64 whitespace-normal'>
                      <div className='space-y-1'>
                        <div className='flex flex-wrap items-center gap-2'>
                          <span className='font-medium'>
                            {formatCashCents(
                              withdrawal.net_amount_cents,
                              withdrawal.currency
                            )}
                          </span>
                          <Badge variant={statusVariant(withdrawal.status)}>
                            {t(withdrawal.status)}
                          </Badge>
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {t('User ID')}: {withdrawal.user_id} ·{' '}
                          {t('Applied at')}: {formatTime(withdrawal.applied_at)}
                        </div>
                        {withdrawal.trade_no ? (
                          <div className='text-muted-foreground text-xs'>
                            {t('Trade no')}: {withdrawal.trade_no}
                          </div>
                        ) : null}
                        {withdrawal.review_note ? (
                          <div className='text-muted-foreground text-xs'>
                            {t('Review note')}: {withdrawal.review_note}
                          </div>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className='min-w-72 whitespace-normal'>
                      <div className='space-y-1.5 text-xs'>
                        <div>
                          {t('Payout method')}:{' '}
                          {withdrawal.payout_method || '-'}
                        </div>
                        <div className='text-muted-foreground break-all'>
                          {t('Payout account')}: {getPayoutAccount(withdrawal)}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className='min-w-72 whitespace-normal'>
                      {canReview ? (
                        <div className='grid gap-2'>
                          {isApproved ? (
                            <Input
                              aria-label={t('Trade no for user {{id}}', {
                                id: withdrawal.user_id,
                              })}
                              value={tradeNoById[withdrawal.id] || ''}
                              onChange={(event) =>
                                updateTradeNo(withdrawal.id, event.target.value)
                              }
                              placeholder={t('Trade no')}
                            />
                          ) : null}
                          <Textarea
                            aria-label={t('Review note for user {{id}}', {
                              id: withdrawal.user_id,
                            })}
                            value={noteById[withdrawal.id] || ''}
                            onChange={(event) =>
                              updateNote(withdrawal.id, event.target.value)
                            }
                            placeholder={t('Review note')}
                            className='min-h-16'
                          />
                        </div>
                      ) : (
                        <div className='text-muted-foreground text-xs'>
                          {reviewTime}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className='text-right'>
                      {canReview ? (
                        <div className='flex flex-wrap justify-end gap-2'>
                          {isPendingReview ? (
                            <Button
                              type='button'
                              size='sm'
                              onClick={() =>
                                setConfirmation({
                                  action: 'approve',
                                  withdrawal,
                                })
                              }
                              disabled={isReviewing}
                            >
                              <CheckCircle2 className='size-4' />
                              {t('Approve request')}
                            </Button>
                          ) : null}
                          {isApproved ? (
                            <Button
                              type='button'
                              size='sm'
                              onClick={() => requestMarkPaid(withdrawal)}
                              disabled={isReviewing}
                            >
                              <Banknote className='size-4' />
                              {t('Mark paid')}
                            </Button>
                          ) : null}
                          <Button
                            type='button'
                            variant='destructive'
                            size='sm'
                            onClick={() => rejectWithdrawal(withdrawal)}
                            disabled={isReviewing}
                          >
                            <XCircle className='size-4' />
                            {t('Reject')}
                          </Button>
                        </div>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          {t('Reviewed')}
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })
            ) : (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className='text-muted-foreground py-10 text-center'
                >
                  {loading
                    ? t('Loading...')
                    : t('No withdrawal requests to review')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>

        <div className='flex flex-wrap items-center justify-between gap-3'>
          <span className='text-muted-foreground text-xs'>
            {t('Page {{page}} of {{total}}', {
              page,
              total: totalPages,
            })}
          </span>
          <div className='flex gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage((current) => Math.max(1, current - 1))}
              disabled={loading || page <= 1}
            >
              {t('Previous')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                setPage((current) => Math.min(totalPages, current + 1))
              }
              disabled={loading || page >= totalPages}
            >
              {t('Next')}
            </Button>
          </div>
        </div>

        <div className='text-muted-foreground text-xs'>
          {t(
            'Paid withdrawals close the linked cash commission ledgers and rejected requests return them to withdrawable status.'
          )}
        </div>
      </div>

      <AlertDialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmationTitle}</AlertDialogTitle>
            <AlertDialogDescription>
              <span className='block space-y-2'>
                <span className='block'>
                  {t('User ID')}: {confirmationWithdrawal?.user_id}
                </span>
                <span className='block'>{confirmationImpact}</span>
              </span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={reviewingId !== null}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirmsPayment) {
                  void markWithdrawalPaid()
                } else {
                  void approveWithdrawal()
                }
              }}
              disabled={reviewingId !== null}
            >
              {confirmationAction}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
