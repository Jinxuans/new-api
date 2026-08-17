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
import { Clock01Icon, ShieldUserIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Accordion } from '@/components/ui/accordion'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { createIdempotencyKey } from '@/lib/idempotency'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { SettingsSection } from '../components/settings-section'
import {
  GrowthRefundActionDialog,
  type RefundActionRequest,
} from './growth-refund-action-dialog'
import { GrowthRefundCaseCard } from './growth-refund-case-card'
import { GrowthRefundCreateDialog } from './growth-refund-create-dialog'
import { GrowthRefundObligationDialog } from './growth-refund-obligation-dialog'
import type {
  PromotionRefundCase,
  PromotionRefundObligation,
  RefundActionIntent,
  RefundRecoveryAction,
} from './growth-refund-types'

type RefundCaseStatusFilter = 'pending_review' | 'resolved' | 'all'

const PAGE_SIZE = 20

function getRefundCasePage(payload: unknown) {
  const response = payload as {
    data?: {
      total?: number
      items?: PromotionRefundCase[]
    }
  }
  return {
    total: Number(response.data?.total || 0),
    items: (response.data?.items || []).map((refundCase) => ({
      ...refundCase,
      obligations: refundCase.obligations || [],
      actions: refundCase.actions || [],
    })),
  }
}

function createActionKey(
  refundCase: PromotionRefundCase,
  action: RefundRecoveryAction,
  obligation?: PromotionRefundObligation
) {
  const prefix = `refund-${refundCase.id}-${obligation?.id || 0}-${action}`
  return createIdempotencyKey(prefix)
}

export function GrowthRefundCasesSection() {
  const { t } = useTranslation()
  const canUseRootActions = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const [refundCases, setRefundCases] = useState<PromotionRefundCase[]>([])
  const [statusFilter, setStatusFilter] =
    useState<RefundCaseStatusFilter>('pending_review')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [actionIntent, setActionIntent] = useState<RefundActionIntent | null>(
    null
  )
  const [submitting, setSubmitting] = useState(false)
  const loadRequestIdRef = useRef(0)
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const loadRefundCases = useCallback(async () => {
    const requestId = ++loadRequestIdRef.current
    setActionIntent(null)
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
      const refundCasePage = getRefundCasePage(response.data)
      setRefundCases(refundCasePage.items)
      setTotal(refundCasePage.total)
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

  const openAction = (
    refundCase: PromotionRefundCase,
    action: RefundRecoveryAction,
    obligation?: PromotionRefundObligation
  ) => {
    if (loading || submitting) return
    if (
      !canUseRootActions &&
      (action === 'define_manual_obligation' ||
        action === 'quarantine_unknown_commission' ||
        action === 'revoke_subscription_entitlement' ||
        action === 'waive')
    ) {
      return
    }
    setActionIntent({
      refundCase,
      action,
      obligation,
      idempotencyKey: createActionKey(refundCase, action, obligation),
    })
  }

  const submitAction = async (request: RefundActionRequest) => {
    if (!actionIntent) return
    try {
      setSubmitting(true)
      const response = await api.post(
        `/api/growth/admin/refund-cases/${actionIntent.refundCase.id}/actions`,
        request
      )
      if (!response.data?.success) {
        throw new Error(
          response.data?.message || t('Failed to record recovery action')
        )
      }
      toast.success(t('Recovery action recorded'))
      setActionIntent(null)
      await loadRefundCases()
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t('Failed to record recovery action')
      )
    } finally {
      setSubmitting(false)
    }
  }

  let content
  if (loadError) {
    content = null
  } else if (loading && refundCases.length === 0) {
    content = (
      <div
        className='flex flex-col gap-3'
        role='status'
        aria-label={t('Loading refund cases')}
      >
        {[0, 1, 2].map((skeletonId) => (
          <div
            key={skeletonId}
            className='flex flex-col gap-3 rounded-lg border p-4'
          >
            <Skeleton className='h-5 w-56 max-w-full' />
            <Skeleton className='h-4 w-80 max-w-full' />
          </div>
        ))}
      </div>
    )
  } else if (refundCases.length === 0) {
    content = (
      <Empty className='border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon
              icon={Clock01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
          </EmptyMedia>
          <EmptyTitle>{t('No refund recovery cases')}</EmptyTitle>
          <EmptyDescription>
            {statusFilter === 'pending_review'
              ? t('No refunds currently require recovery action.')
              : t('No refund cases match this status.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <Accordion className='rounded-lg border' aria-busy={loading}>
        {refundCases.map((refundCase) => (
          <GrowthRefundCaseCard
            key={refundCase.id}
            refundCase={refundCase}
            busy={loading || submitting}
            canUseRootActions={canUseRootActions}
            onAction={openAction}
          />
        ))}
      </Accordion>
    )
  }

  let actionDialog = null
  if (actionIntent?.action === 'define_manual_obligation') {
    actionDialog = (
      <GrowthRefundObligationDialog
        key={actionIntent.idempotencyKey}
        intent={actionIntent}
        submitting={submitting}
        onCancel={() => !submitting && setActionIntent(null)}
        onSubmit={submitAction}
      />
    )
  } else if (actionIntent) {
    actionDialog = (
      <GrowthRefundActionDialog
        key={actionIntent.idempotencyKey}
        intent={actionIntent}
        submitting={submitting}
        onCancel={() => !submitting && setActionIntent(null)}
        onSubmit={submitAction}
      />
    )
  }

  return (
    <SettingsSection
      title={t('Refund recovery')}
      description={t(
        'Recover refunded API balance and paid commission through explicit, auditable actions.'
      )}
    >
      <div className='flex flex-col gap-4 rounded-lg border p-4'>
        <Alert>
          <HugeiconsIcon
            icon={ShieldUserIcon}
            strokeWidth={2}
            aria-hidden='true'
          />
          <AlertTitle>{t('Recovery follows the money')}</AlertTitle>
          <AlertDescription>
            {t(
              'Each case shows the refunded principal, immediate wallet debit, remaining obligations, immutable action history, and the final hold release. A case cannot be closed by adding a note.'
            )}
          </AlertDescription>
        </Alert>

        <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <Badge variant={total > 0 ? 'secondary' : 'outline'}>
            {t('{{count}} results', { count: total })}
          </Badge>
          <div className='flex flex-col gap-2 sm:flex-row'>
            {canUseRootActions ? (
              <Button type='button' onClick={() => setCreateDialogOpen(true)}>
                {t('Create refund case')}
              </Button>
            ) : null}
            <Select
              items={[
                { value: 'pending_review', label: t('Recovery in progress') },
                { value: 'resolved', label: t('Resolved') },
                { value: 'all', label: t('All statuses') },
              ]}
              value={statusFilter}
              onValueChange={(value) => {
                setActionIntent(null)
                setLoading(true)
                setPage(1)
                setStatusFilter(value as RefundCaseStatusFilter)
              }}
            >
              <SelectTrigger
                className='w-full sm:w-48'
                aria-label={t('Refund case status')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='pending_review'>
                    {t('Recovery in progress')}
                  </SelectItem>
                  <SelectItem value='resolved'>{t('Resolved')}</SelectItem>
                  <SelectItem value='all'>{t('All statuses')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type='button'
              variant='outline'
              onClick={() => void loadRefundCases()}
              disabled={loading}
            >
              {t('Refresh cases')}
            </Button>
          </div>
        </div>

        {loadError ? (
          <Alert variant='destructive'>
            <AlertTitle>{t('Refund cases could not be loaded')}</AlertTitle>
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

        {content}

        <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <span className='text-muted-foreground text-center text-xs sm:text-left'>
            {t('Page {{page}} of {{total}}', { page, total: totalPages })}
          </span>
          <div className='flex justify-center gap-2 sm:justify-end'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => {
                setActionIntent(null)
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
                setActionIntent(null)
                setLoading(true)
                setPage((current) => Math.min(totalPages, current + 1))
              }}
              disabled={loading || page >= totalPages}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </div>

      {actionDialog}
      {canUseRootActions ? (
        <GrowthRefundCreateDialog
          open={createDialogOpen}
          onOpenChange={setCreateDialogOpen}
          onCreated={loadRefundCases}
        />
      ) : null}
    </SettingsSection>
  )
}
