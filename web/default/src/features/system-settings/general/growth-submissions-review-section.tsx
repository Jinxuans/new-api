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
import { CheckCircle2, ExternalLink, RefreshCw, XCircle } from 'lucide-react'
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
  formatTime,
  getItems,
  rewardItemCopy,
  statusVariant,
  type GrowthSubmission,
} from '@/features/promotion/shared'
import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'

import { SettingsSection } from '../components/settings-section'

type AdminGrowthSubmission = GrowthSubmission & {
  user_id: number
  remark?: string
  reviewer_id?: number
  reviewed_at?: number
  item_title?: string
  reward_quota_min: number
  reward_quota_max: number
}

type SubmissionStatusFilter = 'pending' | 'approved' | 'rejected' | 'all'

const PAGE_SIZE = 20

export function GrowthSubmissionsReviewSection() {
  const { t } = useTranslation()
  const [submissions, setSubmissions] = useState<AdminGrowthSubmission[]>([])
  const [loading, setLoading] = useState(true)
  const [reviewingId, setReviewingId] = useState<number | null>(null)
  const [statusFilter, setStatusFilter] =
    useState<SubmissionStatusFilter>('pending')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [approvalConfirmation, setApprovalConfirmation] = useState<{
    submission: AdminGrowthSubmission
    rewardQuota: number
  } | null>(null)
  const [rewardQuotaById, setRewardQuotaById] = useState<
    Record<number, string>
  >({})
  const [noteById, setNoteById] = useState<Record<number, string>>({})
  const loadRequestIdRef = useRef(0)

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(total / PAGE_SIZE)),
    [total]
  )

  const loadSubmissions = useCallback(async () => {
    const requestId = ++loadRequestIdRef.current
    try {
      setLoading(true)
      const res = await api.get('/api/growth/admin/submissions', {
        params: { p: page, page_size: PAGE_SIZE, status: statusFilter },
      })
      if (requestId !== loadRequestIdRef.current) return
      const items = getItems<AdminGrowthSubmission>(res.data)
      setSubmissions(items)
      setTotal(Number(res.data?.data?.total || 0))
      setRewardQuotaById((current) => {
        const next = { ...current }
        for (const item of items) {
          if (next[item.id] === undefined) {
            next[item.id] = String(item.reward_quota_min || '')
          }
        }
        return next
      })
    } finally {
      if (requestId === loadRequestIdRef.current) {
        setLoading(false)
      }
    }
  }, [page, statusFilter])

  useEffect(() => {
    loadSubmissions()
  }, [loadSubmissions])

  const rewardItemTitle = (submission: AdminGrowthSubmission) =>
    t(
      rewardItemCopy[submission.item_code]?.title ||
        submission.item_title ||
        submission.item_code
    )

  const updateNote = (id: number, value: string) => {
    setNoteById((current) => ({ ...current, [id]: value }))
  }

  const updateRewardQuota = (id: number, value: string) => {
    setRewardQuotaById((current) => ({ ...current, [id]: value }))
  }

  const requestApproval = (submission: AdminGrowthSubmission) => {
    const rewardQuota = Number(rewardQuotaById[submission.id] || 0)
    if (!Number.isInteger(rewardQuota) || rewardQuota <= 0) {
      toast.error(t('Reward quota is required'))
      return
    }
    if (
      (submission.reward_quota_min > 0 &&
        rewardQuota < submission.reward_quota_min) ||
      (submission.reward_quota_max > 0 &&
        rewardQuota > submission.reward_quota_max)
    ) {
      toast.error(
        t('Reward must be between {{min}} and {{max}}.', {
          min: formatQuota(submission.reward_quota_min),
          max: formatQuota(submission.reward_quota_max),
        })
      )
      return
    }
    setApprovalConfirmation({ submission, rewardQuota })
  }

  const approveSubmission = async () => {
    if (!approvalConfirmation) return
    const submission = approvalConfirmation.submission
    try {
      setReviewingId(submission.id)
      const res = await api.post(
        `/api/growth/admin/submissions/${submission.id}/approve`,
        {
          reward_quota: approvalConfirmation.rewardQuota,
          review_note: noteById[submission.id] || '',
        }
      )
      if (res.data?.success) {
        toast.success(t('Submission approved'))
        setApprovalConfirmation(null)
        await loadSubmissions()
      }
    } finally {
      setReviewingId(null)
    }
  }

  const rejectSubmission = async (submission: AdminGrowthSubmission) => {
    const note = noteById[submission.id]?.trim()
    if (!note) {
      toast.error(t('Review note is required'))
      return
    }
    try {
      setReviewingId(submission.id)
      const res = await api.post(
        `/api/growth/admin/submissions/${submission.id}/reject`,
        {
          review_note: note,
        }
      )
      if (res.data?.success) {
        toast.success(t('Submission rejected'))
        await loadSubmissions()
      }
    } finally {
      setReviewingId(null)
    }
  }

  return (
    <SettingsSection
      title={t('Content Reward Reviews')}
      description={t(
        'Review promotion proof submissions and settle approved rewards.'
      )}
    >
      <div className='space-y-4 rounded-lg border p-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant={total > 0 ? 'secondary' : 'outline'}>
              {t('{{count}} results', { count: total })}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {t('Latest submissions are shown first.')}
            </span>
          </div>
          <div className='flex items-center gap-2'>
            <Select
              items={[
                { value: 'pending', label: t('Pending') },
                { value: 'approved', label: t('Approved') },
                { value: 'rejected', label: t('Rejected') },
                { value: 'all', label: t('All statuses') },
              ]}
              value={statusFilter}
              onValueChange={(value) => {
                setPage(1)
                setStatusFilter(value as SubmissionStatusFilter)
              }}
            >
              <SelectTrigger
                className='w-40'
                aria-label={t('Submission status')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='pending'>{t('Pending')}</SelectItem>
                  <SelectItem value='approved'>{t('Approved')}</SelectItem>
                  <SelectItem value='rejected'>{t('Rejected')}</SelectItem>
                  <SelectItem value='all'>{t('All statuses')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={loadSubmissions}
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
              <TableHead>{t('Submission')}</TableHead>
              <TableHead>{t('Proof')}</TableHead>
              <TableHead>{t('Review')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {submissions.length > 0 ? (
              submissions.map((submission) => {
                const isPending = submission.status === 'pending'
                const isReviewing = reviewingId === submission.id
                return (
                  <TableRow key={submission.id}>
                    <TableCell className='min-w-64 whitespace-normal'>
                      <div className='space-y-1'>
                        <div className='flex flex-wrap items-center gap-2'>
                          <span className='font-medium'>
                            {rewardItemTitle(submission)}
                          </span>
                          <Badge variant={statusVariant(submission.status)}>
                            {t(submission.status)}
                          </Badge>
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {t('User ID')}: {submission.user_id} ·{' '}
                          {formatTime(submission.created_at)}
                        </div>
                        {submission.review_note ? (
                          <div className='text-muted-foreground text-xs'>
                            {t('Review note')}: {submission.review_note}
                          </div>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className='min-w-72 whitespace-normal'>
                      <div className='space-y-1.5'>
                        <div className='text-xs'>
                          {submission.platform || t('Platform not provided')}
                        </div>
                        {submission.url ? (
                          <a
                            href={submission.url}
                            target='_blank'
                            rel='noreferrer'
                            className='text-primary inline-flex max-w-80 items-center gap-1 truncate text-xs font-medium hover:underline'
                          >
                            <span className='truncate'>{submission.url}</span>
                            <ExternalLink className='size-3 shrink-0' />
                          </a>
                        ) : (
                          <span className='text-muted-foreground text-xs'>
                            -
                          </span>
                        )}
                        {submission.remark ? (
                          <div className='text-muted-foreground text-xs'>
                            {submission.remark}
                          </div>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className='min-w-72 whitespace-normal'>
                      {isPending ? (
                        <div className='grid gap-2'>
                          <Input
                            aria-label={t('Reward quota for user {{id}}', {
                              id: submission.user_id,
                            })}
                            type='number'
                            min={submission.reward_quota_min || 1}
                            max={submission.reward_quota_max || undefined}
                            value={rewardQuotaById[submission.id] || ''}
                            onChange={(event) =>
                              updateRewardQuota(
                                submission.id,
                                event.target.value
                              )
                            }
                            placeholder={t('Reward quota')}
                          />
                          <span className='text-muted-foreground text-xs'>
                            {t('Allowed reward: {{min}} to {{max}}', {
                              min: formatQuota(submission.reward_quota_min),
                              max: formatQuota(submission.reward_quota_max),
                            })}
                          </span>
                          <Textarea
                            aria-label={t('Review note for user {{id}}', {
                              id: submission.user_id,
                            })}
                            value={noteById[submission.id] || ''}
                            onChange={(event) =>
                              updateNote(submission.id, event.target.value)
                            }
                            placeholder={t('Review note')}
                            className='min-h-16'
                          />
                        </div>
                      ) : (
                        <div className='text-muted-foreground text-xs'>
                          {submission.reviewed_at
                            ? formatTime(submission.reviewed_at)
                            : '-'}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className='text-right'>
                      {isPending ? (
                        <div className='flex justify-end gap-2'>
                          <Button
                            type='button'
                            size='sm'
                            onClick={() => requestApproval(submission)}
                            disabled={isReviewing}
                          >
                            <CheckCircle2 className='size-4' />
                            {t('Approve')}
                          </Button>
                          <Button
                            type='button'
                            variant='destructive'
                            size='sm'
                            onClick={() => rejectSubmission(submission)}
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
                  {loading ? t('Loading...') : t('No submissions to review')}
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
            'Approved rewards are settled immediately and added to the user balance.'
          )}
        </div>
      </div>

      <AlertDialog
        open={approvalConfirmation !== null}
        onOpenChange={(open) => {
          if (!open) setApprovalConfirmation(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Approve this reward?')}</AlertDialogTitle>
            <AlertDialogDescription>
              <span className='block space-y-2'>
                <span className='block'>
                  {t('User ID')}: {approvalConfirmation?.submission.user_id}
                </span>
                <span className='block'>
                  {t(
                    '{{amount}} will be added to the user balance immediately. This action cannot be undone here.',
                    {
                      amount: formatQuota(
                        approvalConfirmation?.rewardQuota || 0
                      ),
                    }
                  )}
                </span>
              </span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={reviewingId !== null}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={approveSubmission}
              disabled={reviewingId !== null}
            >
              {t('Approve and add quota')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
