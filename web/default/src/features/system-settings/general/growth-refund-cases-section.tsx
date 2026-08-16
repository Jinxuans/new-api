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
import { CheckCircle2, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
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
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'

import { SettingsSection } from '../components/settings-section'

type RefundCaseStatusFilter = 'pending_review' | 'resolved' | 'all'

type PromotionRefundCase = {
  id: number
  provider: string
  trade_no: string
  refund_trade_no: string
  kind: 'full_refund' | 'partial_refund' | 'dispute'
  paid_amount_minor: number
  refunded_amount_minor: number
  currency: string
  status: 'pending_review' | 'resolved'
  reason: string
  review_note?: string
  reviewer_id?: number
  created_at: number
  resolved_at?: number
}

const PAGE_SIZE = 20
const MAX_REVIEW_NOTE_LENGTH = 1000

function formatMinorAmount(amountMinor: number, currency: string) {
  const normalizedCurrency = (currency || '').trim().toUpperCase() || 'CNY'
  const safeAmountMinor = Number.isFinite(amountMinor) ? amountMinor : 0
  try {
    const formatter = new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: normalizedCurrency,
    })
    const fractionDigits =
      formatter.resolvedOptions().maximumFractionDigits ?? 2
    return formatter.format(safeAmountMinor / 10 ** fractionDigits)
  } catch {
    return `${normalizedCurrency} ${(safeAmountMinor / 100).toFixed(2)}`
  }
}

function getRefundCaseItems(payload: unknown): PromotionRefundCase[] {
  const response = payload as {
    data?: { items?: PromotionRefundCase[] }
  }
  return response.data?.items || []
}

export function GrowthRefundCasesSection() {
  const { t } = useTranslation()
  const [refundCases, setRefundCases] = useState<PromotionRefundCase[]>([])
  const [statusFilter, setStatusFilter] =
    useState<RefundCaseStatusFilter>('pending_review')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [resolvingId, setResolvingId] = useState<number | null>(null)
  const [reviewNoteById, setReviewNoteById] = useState<Record<number, string>>(
    {}
  )
  const [confirmation, setConfirmation] = useState<PromotionRefundCase | null>(
    null
  )
  const loadRequestIdRef = useRef(0)

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(total / PAGE_SIZE)),
    [total]
  )

  const loadRefundCases = useCallback(async () => {
    const requestId = ++loadRequestIdRef.current
    setLoading(true)
    setLoadError('')
    try {
      const response = await api.get('/api/growth/admin/refund-cases', {
        params: { p: page, page_size: PAGE_SIZE, status: statusFilter },
      })
      if (requestId !== loadRequestIdRef.current) return
      if (!response.data?.success) {
        throw new Error(response.data?.message || 'Failed to load refund cases')
      }
      setRefundCases(getRefundCaseItems(response.data))
      setTotal(Number(response.data?.data?.total || 0))
    } catch {
      if (requestId !== loadRequestIdRef.current) return
      setRefundCases([])
      setTotal(0)
      setLoadError(t('Failed to load refund cases.'))
    } finally {
      if (requestId === loadRequestIdRef.current) {
        setLoading(false)
      }
    }
  }, [page, statusFilter, t])

  useEffect(() => {
    void loadRefundCases()
  }, [loadRefundCases])

  const requestResolve = (refundCase: PromotionRefundCase) => {
    const reviewNote = reviewNoteById[refundCase.id]?.trim() || ''
    if (!reviewNote) {
      toast.error(t('Review note is required'))
      return
    }
    if ([...reviewNote].length > MAX_REVIEW_NOTE_LENGTH) {
      toast.error(t('Review note cannot exceed 1000 characters'))
      return
    }
    setConfirmation(refundCase)
  }

  const resolveRefundCase = async () => {
    if (!confirmation) return
    const reviewNote = reviewNoteById[confirmation.id]?.trim()
    if (!reviewNote) return

    try {
      setResolvingId(confirmation.id)
      const response = await api.post(
        `/api/growth/admin/refund-cases/${confirmation.id}/resolve`,
        { review_note: reviewNote }
      )
      if (!response.data?.success) {
        throw new Error(
          response.data?.message || t('Failed to resolve refund case')
        )
      }
      toast.success(t('Refund case marked resolved'))
      setConfirmation(null)
      await loadRefundCases()
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t('Failed to resolve refund case')
      )
    } finally {
      setResolvingId(null)
    }
  }

  const refundKindLabel = (kind: PromotionRefundCase['kind']) => {
    switch (kind) {
      case 'full_refund':
        return t('Full refund')
      case 'partial_refund':
        return t('Partial refund')
      case 'dispute':
        return t('Dispute')
    }
  }

  return (
    <SettingsSection
      title={t('Refund cases')}
      description={t(
        'Review refunds that require manual commission recovery or quota adjustment.'
      )}
    >
      <div className='space-y-4 rounded-lg border p-4'>
        <Alert>
          <AlertDescription>
            {t(
              'Marking a case resolved only records the completed manual review. It does not change balances or commissions.'
            )}
          </AlertDescription>
        </Alert>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <Badge variant={total > 0 ? 'secondary' : 'outline'}>
            {t('{{count}} results', { count: total })}
          </Badge>
          <div className='flex items-center gap-2'>
            <Select
              items={[
                { value: 'pending_review', label: t('Pending review') },
                { value: 'resolved', label: t('Resolved') },
                { value: 'all', label: t('All statuses') },
              ]}
              value={statusFilter}
              onValueChange={(value) => {
                setPage(1)
                setStatusFilter(value as RefundCaseStatusFilter)
              }}
            >
              <SelectTrigger
                className='w-44'
                aria-label={t('Refund case status')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='pending_review'>
                    {t('Pending review')}
                  </SelectItem>
                  <SelectItem value='resolved'>{t('Resolved')}</SelectItem>
                  <SelectItem value='all'>{t('All statuses')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void loadRefundCases()}
              disabled={loading}
            >
              <RefreshCw className='size-4' />
              {t('Refresh')}
            </Button>
          </div>
        </div>

        {loadError ? (
          <Alert variant='destructive'>
            <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
              <span>{loadError}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void loadRefundCases()}
              >
                {t('Try again')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}

        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Order')}</TableHead>
                <TableHead>{t('Refund')}</TableHead>
                <TableHead>{t('Amount')}</TableHead>
                <TableHead>{t('Reason')}</TableHead>
                <TableHead>{t('Review')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {refundCases.length > 0 ? (
                refundCases.map((refundCase) => {
                  const pending = refundCase.status === 'pending_review'
                  return (
                    <TableRow key={refundCase.id}>
                      <TableCell className='min-w-56 whitespace-normal'>
                        <div className='space-y-1 text-xs'>
                          <div className='font-medium'>
                            {refundCase.provider}
                          </div>
                          <div className='text-muted-foreground break-all'>
                            {t('Order')}: {refundCase.trade_no}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className='min-w-56 whitespace-normal'>
                        <div className='space-y-1 text-xs'>
                          <div className='flex flex-wrap items-center gap-2'>
                            <Badge variant='outline'>
                              {refundKindLabel(refundCase.kind)}
                            </Badge>
                            <Badge variant={pending ? 'secondary' : 'default'}>
                              {pending ? t('Pending review') : t('Resolved')}
                            </Badge>
                          </div>
                          <div className='text-muted-foreground break-all'>
                            {t('Refund order')}: {refundCase.refund_trade_no}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className='min-w-44 whitespace-normal'>
                        <div className='space-y-1 text-xs'>
                          <div className='font-medium'>
                            {formatMinorAmount(
                              refundCase.refunded_amount_minor,
                              refundCase.currency
                            )}
                          </div>
                          <div className='text-muted-foreground'>
                            {t('Paid amount')}:{' '}
                            {formatMinorAmount(
                              refundCase.paid_amount_minor,
                              refundCase.currency
                            )}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className='min-w-64 text-xs whitespace-normal'>
                        {refundCase.reason || '-'}
                      </TableCell>
                      <TableCell className='min-w-72 whitespace-normal'>
                        {pending ? (
                          <Textarea
                            aria-label={t('Review note for refund {{id}}', {
                              id: refundCase.id,
                            })}
                            value={reviewNoteById[refundCase.id] || ''}
                            maxLength={MAX_REVIEW_NOTE_LENGTH}
                            onChange={(event) =>
                              setReviewNoteById((current) => ({
                                ...current,
                                [refundCase.id]: event.target.value,
                              }))
                            }
                            placeholder={t(
                              'Describe the manual recovery or quota adjustment completed.'
                            )}
                            className='min-h-16'
                          />
                        ) : (
                          <div className='space-y-1 text-xs'>
                            <div>{refundCase.review_note || '-'}</div>
                            {refundCase.reviewer_id ? (
                              <div className='text-muted-foreground'>
                                {t('Reviewer ID')}: {refundCase.reviewer_id}
                              </div>
                            ) : null}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className='text-right'>
                        {pending ? (
                          <Button
                            type='button'
                            size='sm'
                            onClick={() => requestResolve(refundCase)}
                            disabled={resolvingId === refundCase.id}
                          >
                            <CheckCircle2 className='size-4' />
                            {t('Mark resolved')}
                          </Button>
                        ) : (
                          <span className='text-muted-foreground text-xs'>
                            {t('Resolved')}
                          </span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })
              ) : (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground py-10 text-center'
                  >
                    {loading ? t('Loading...') : t('No refund cases')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>

        <div className='flex flex-wrap items-center justify-between gap-3'>
          <span className='text-muted-foreground text-xs'>
            {t('Page {{page}} of {{total}}', { page, total: totalPages })}
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
      </div>

      <AlertDialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Mark this refund case resolved?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Confirm only after the manual commission recovery or quota adjustment is complete. This action only closes the operational case.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className='rounded-lg border p-3 text-sm'>
            <div>
              {t('Order')}: {confirmation?.trade_no}
            </div>
            <div>
              {t('Refunded amount')}:{' '}
              {formatMinorAmount(
                confirmation?.refunded_amount_minor || 0,
                confirmation?.currency || 'CNY'
              )}
            </div>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resolvingId !== null}>
              {t('Cancel')}
            </AlertDialogCancel>
            <Button
              type='button'
              onClick={() => void resolveRefundCase()}
              disabled={resolvingId !== null}
            >
              {t('Confirm manual resolution')}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
