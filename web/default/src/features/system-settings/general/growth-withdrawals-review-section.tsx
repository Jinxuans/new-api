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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { getItems } from '@/features/promotion/shared'
import { api } from '@/lib/api'

import { SettingsSection } from '../components/settings-section'
import { GrowthWithdrawalConfirmationDialog } from './growth-withdrawal-confirmation-dialog'
import { GrowthWithdrawalReviewRow } from './growth-withdrawal-review-row'
import type {
  AdminPromotionWithdrawal,
  WithdrawalConfirmation,
  WithdrawalStatusFilter,
} from './growth-withdrawal-review-types'

const PAGE_SIZE = 20

export function GrowthWithdrawalsReviewSection() {
  const { t } = useTranslation()
  const [withdrawals, setWithdrawals] = useState<AdminPromotionWithdrawal[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [reviewingId, setReviewingId] = useState<number | null>(null)
  const [statusFilter, setStatusFilter] =
    useState<WithdrawalStatusFilter>('pending_review')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [confirmation, setConfirmation] =
    useState<WithdrawalConfirmation | null>(null)
  const [tradeNoById, setTradeNoById] = useState<Record<number, string>>({})
  const [reviewNoteById, setReviewNoteById] = useState<Record<number, string>>(
    {}
  )
  const [failureNoteById, setFailureNoteById] = useState<
    Record<number, string>
  >({})
  const loadRequestIdRef = useRef(0)

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const loadWithdrawals = useCallback(async () => {
    const requestId = ++loadRequestIdRef.current
    setConfirmation(null)
    try {
      setLoading(true)
      setLoadError(false)
      const res = await api.get('/api/growth/admin/withdrawals', {
        params: { p: page, page_size: PAGE_SIZE, status: statusFilter },
      })
      if (requestId !== loadRequestIdRef.current) return
      if (!res.data?.success) {
        throw new Error(
          res.data?.message || 'promotion withdrawals unavailable'
        )
      }
      setWithdrawals(getItems<AdminPromotionWithdrawal>(res.data))
      setTotal(Number(res.data?.data?.total || 0))
    } catch {
      if (requestId !== loadRequestIdRef.current) return
      setWithdrawals([])
      setTotal(0)
      setLoadError(true)
    } finally {
      if (requestId === loadRequestIdRef.current) {
        setLoading(false)
      }
    }
  }, [page, statusFilter])

  useEffect(() => {
    void loadWithdrawals()
  }, [loadWithdrawals])

  const updateTradeNo = (id: number, value: string) => {
    setTradeNoById((current) => ({ ...current, [id]: value }))
  }

  const updateReviewNote = (id: number, value: string) => {
    setReviewNoteById((current) => ({ ...current, [id]: value }))
  }

  const updateFailureNote = (id: number, value: string) => {
    setFailureNoteById((current) => ({ ...current, [id]: value }))
  }

  const approveWithdrawal = async () => {
    if (!confirmation || confirmation.action !== 'approve') return
    const withdrawal = confirmation.withdrawal
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/approve`,
        {
          review_note: reviewNoteById[withdrawal.id] || '',
        }
      )
      if (res.data?.success) {
        toast.success(t('Withdrawal request approved'))
        setReviewNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const requestRejectWithdrawal = (withdrawal: AdminPromotionWithdrawal) => {
    const note = reviewNoteById[withdrawal.id]?.trim()
    if (!note) {
      toast.error(t('Review note is required'))
      return
    }
    setConfirmation({ action: 'reject', withdrawal })
  }

  const rejectWithdrawal = async () => {
    if (!confirmation || confirmation.action !== 'reject') return
    const withdrawal = confirmation.withdrawal
    const note = reviewNoteById[withdrawal.id]?.trim()
    if (!note) return
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
        setReviewNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setTradeNoById((current) => ({ ...current, [withdrawal.id]: '' }))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const requestInitiatePayout = (withdrawal: AdminPromotionWithdrawal) => {
    const tradeNo = tradeNoById[withdrawal.id]?.trim()
    if (!tradeNo) {
      toast.error(t('Trade no is required'))
      return
    }
    setConfirmation({ action: 'initiate', withdrawal })
  }

  const initiatePayout = async () => {
    if (!confirmation || confirmation.action !== 'initiate') return
    const withdrawal = confirmation.withdrawal
    const tradeNo = tradeNoById[withdrawal.id]?.trim()
    if (!tradeNo) return
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/initiate`,
        {
          trade_no: tradeNo,
          review_note: reviewNoteById[withdrawal.id] || '',
        }
      )
      if (res.data?.success) {
        toast.success(t('Payout marked in progress'))
        setReviewNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setFailureNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setTradeNoById((current) => ({ ...current, [withdrawal.id]: '' }))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const markWithdrawalPaid = async () => {
    if (!confirmation || confirmation.action !== 'paid') return
    const withdrawal = confirmation.withdrawal
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/paid`,
        {
          trade_no: withdrawal.trade_no || '',
          review_note: reviewNoteById[withdrawal.id] || '',
        }
      )
      if (res.data?.success) {
        toast.success(t('Withdrawal marked paid'))
        setReviewNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setFailureNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setTradeNoById((current) => ({ ...current, [withdrawal.id]: '' }))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const requestFailWithdrawal = (withdrawal: AdminPromotionWithdrawal) => {
    const failureNote = failureNoteById[withdrawal.id]?.trim()
    if (!failureNote) {
      toast.error(t('Failure reason is required'))
      return
    }
    setConfirmation({ action: 'failed', withdrawal })
  }

  const markWithdrawalFailed = async () => {
    if (!confirmation || confirmation.action !== 'failed') return
    const withdrawal = confirmation.withdrawal
    const failureNote = failureNoteById[withdrawal.id]?.trim()
    if (!failureNote || !withdrawal.trade_no) return
    try {
      setReviewingId(withdrawal.id)
      const res = await api.post(
        `/api/growth/admin/withdrawals/${withdrawal.id}/failed`,
        {
          trade_no: withdrawal.trade_no,
          failure_note: failureNote,
        }
      )
      if (res.data?.success) {
        toast.success(t('Payout marked failed'))
        setReviewNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setFailureNoteById((current) => ({
          ...current,
          [withdrawal.id]: '',
        }))
        setTradeNoById((current) => ({ ...current, [withdrawal.id]: '' }))
        setConfirmation(null)
        await loadWithdrawals()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const confirmSelectedAction = () => {
    if (loading || reviewingId !== null) return

    let actionPromise: Promise<void> | null = null
    switch (confirmation?.action) {
      case 'failed':
        actionPromise = markWithdrawalFailed()
        break
      case 'paid':
        actionPromise = markWithdrawalPaid()
        break
      case 'initiate':
        actionPromise = initiatePayout()
        break
      case 'reject':
        actionPromise = rejectWithdrawal()
        break
      case 'approve':
        actionPromise = approveWithdrawal()
        break
    }

    if (!actionPromise) return
    void actionPromise.catch(() => {
      // The shared HTTP client already displays request failures.
    })
  }

  let confirmationNote = ''
  if (confirmation) {
    confirmationNote =
      confirmation.action === 'failed'
        ? failureNoteById[confirmation.withdrawal.id]?.trim() || ''
        : reviewNoteById[confirmation.withdrawal.id]?.trim() || ''
  }

  return (
    <SettingsSection
      title={t('Withdrawal Reviews')}
      description={t(
        'Review submitted cash withdrawal requests and mark paid after offline settlement.'
      )}
    >
      <div className='flex flex-col gap-4 rounded-lg border p-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant={total > 0 ? 'secondary' : 'outline'}>
              {t('{{count}} results', { count: total })}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {t('Latest withdrawal requests are shown first.')}
            </span>
          </div>
          <div className='flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center'>
            <Select
              items={[
                { value: 'pending_review', label: t('Pending review') },
                { value: 'approved', label: t('Approved awaiting payout') },
                { value: 'processing', label: t('Payout in progress') },
                { value: 'paid', label: t('Paid') },
                { value: 'rejected', label: t('Rejected') },
                { value: 'failed', label: t('Failed') },
                { value: 'all', label: t('All statuses') },
              ]}
              value={statusFilter}
              onValueChange={(value) => {
                setConfirmation(null)
                setLoading(true)
                setPage(1)
                setStatusFilter(value as WithdrawalStatusFilter)
              }}
            >
              <SelectTrigger
                className='w-full sm:w-48'
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
                  <SelectItem value='processing'>
                    {t('Payout in progress')}
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
              {t('Refresh')}
            </Button>
          </div>
        </div>

        {loadError ? (
          <Alert variant='destructive'>
            <AlertTitle>{t('No data available')}</AlertTitle>
            <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
              <span>{t('Refresh the list and try again.')}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void loadWithdrawals()}
              >
                {t('Try again')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <Table aria-busy={loading}>
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
                  withdrawals.map((withdrawal) => (
                    <GrowthWithdrawalReviewRow
                      key={withdrawal.id}
                      withdrawal={withdrawal}
                      busy={loading || reviewingId === withdrawal.id}
                      tradeNo={tradeNoById[withdrawal.id] || ''}
                      note={
                        withdrawal.status === 'processing'
                          ? failureNoteById[withdrawal.id] || ''
                          : reviewNoteById[withdrawal.id] || ''
                      }
                      onTradeNoChange={(value) =>
                        updateTradeNo(withdrawal.id, value)
                      }
                      onNoteChange={(value) => {
                        if (withdrawal.status === 'processing') {
                          updateFailureNote(withdrawal.id, value)
                        } else {
                          updateReviewNote(withdrawal.id, value)
                        }
                      }}
                      onApprove={() =>
                        setConfirmation({ action: 'approve', withdrawal })
                      }
                      onReject={() => requestRejectWithdrawal(withdrawal)}
                      onInitiate={() => requestInitiatePayout(withdrawal)}
                      onConfirmPaid={() =>
                        setConfirmation({ action: 'paid', withdrawal })
                      }
                      onFail={() => requestFailWithdrawal(withdrawal)}
                    />
                  ))
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
                  onClick={() => {
                    setConfirmation(null)
                    setLoading(true)
                    setPage((current) => Math.max(1, current - 1))
                  }}
                  disabled={loading || page <= 1}
                >
                  {t('Previous')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => {
                    setConfirmation(null)
                    setLoading(true)
                    setPage((current) => Math.min(totalPages, current + 1))
                  }}
                  disabled={loading || page >= totalPages}
                >
                  {t('Next')}
                </Button>
              </div>
            </div>
          </>
        )}

        <div className='text-muted-foreground text-xs'>
          {t(
            'Approved requests may still be rejected. Processing payouts must be resolved as paid or failed; a failed payout returns reserved commission to the available balance.'
          )}
        </div>
      </div>

      <GrowthWithdrawalConfirmationDialog
        confirmation={confirmation}
        tradeNo={
          confirmation
            ? tradeNoById[confirmation.withdrawal.id]?.trim() || ''
            : ''
        }
        note={confirmationNote}
        busy={loading || reviewingId !== null}
        onCancel={() => setConfirmation(null)}
        onConfirm={confirmSelectedAction}
      />
    </SettingsSection>
  )
}
