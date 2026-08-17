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
import { useQuery } from '@tanstack/react-query'
import { useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getItems } from '@/features/promotion/shared'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { api } from '@/lib/api'

import { FundRecordRow } from './fund-record-row'
import type { AdminPromotionFundRecord } from './fund-record-types'

const PAGE_SIZE = 20

export function FundRecordsSection() {
  const { t } = useTranslation()
  const [draftUserId, setDraftUserId] = useState('')
  const [userId, setUserId] = useState<number | null>(null)
  const [page, setPage] = useState(1)
  const [hasValidationError, setHasValidationError] = useState(false)

  const recordsQuery = useQuery({
    queryKey: ['admin', 'promotion-fund-records', userId, page],
    enabled: userId !== null,
    queryFn: async () => {
      const response = await api.get('/api/growth/admin/fund-records', {
        params: { user_id: userId, p: page, page_size: PAGE_SIZE },
      })
      if (!response.data?.success) {
        throw new Error(
          response.data?.message || 'promotion fund records unavailable'
        )
      }
      return {
        items: getItems<AdminPromotionFundRecord>(response.data),
        total: Number(response.data?.data?.total || 0),
      }
    },
  })

  const records = recordsQuery.data?.items || []
  const total = recordsQuery.data?.total || 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const submitSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const normalized = draftUserId.trim()
    const parsed = Number(normalized)
    const valid =
      /^\d+$/.test(normalized) && Number.isSafeInteger(parsed) && parsed > 0
    setHasValidationError(!valid)
    if (!valid) return
    setPage(1)
    setUserId(parsed)
  }

  let recordsContent: ReactNode = null
  if (recordsQuery.isLoading) {
    recordsContent = (
      <div className='grid gap-2' role='status' aria-label={t('Loading...')}>
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
      </div>
    )
  } else if (userId !== null && recordsQuery.isError) {
    recordsContent = (
      <Alert variant='destructive'>
        <AlertTitle>{t('Unable to load fund history')}</AlertTitle>
        <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
          <span>{t('Refresh the list and try again.')}</span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => recordsQuery.refetch()}
            disabled={recordsQuery.isFetching}
          >
            {t('Try again')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else if (userId !== null && records.length === 0) {
    recordsContent = (
      <Empty className='border'>
        <EmptyHeader>
          <EmptyTitle>{t('No fund records yet')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  } else if (userId !== null) {
    recordsContent = (
      <>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Journal reference')}</TableHead>
              <TableHead>{t('Kind')}</TableHead>
              <TableHead>{t('Amount')}</TableHead>
              <TableHead>{t('Audit references')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => (
              <FundRecordRow key={record.id} record={record} />
            ))}
          </TableBody>
        </Table>

        <div className='flex flex-wrap items-center justify-between gap-3'>
          <span className='text-muted-foreground text-xs'>
            {t('{{total}} records · Page {{page}} of {{pages}}', {
              total,
              page,
              pages: totalPages,
            })}
          </span>
          <div className='flex gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage((current) => Math.max(1, current - 1))}
              disabled={recordsQuery.isFetching || page <= 1}
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
              disabled={recordsQuery.isFetching || page >= totalPages}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </>
    )
  }

  return (
    <SettingsSection
      title={t('Fund history')}
      description={t(
        'This journal covers account credits, rewards, promotion funds, and recovery adjustments. API usage charges remain in usage logs, so this is not a complete wallet ledger.'
      )}
    >
      <div className='flex flex-col gap-4 rounded-lg border p-4'>
        <form onSubmit={submitSearch} noValidate>
          <div className='flex max-w-xl flex-col items-stretch gap-2 sm:flex-row sm:items-end'>
            <FieldGroup className='min-w-0 flex-1'>
              <Field data-invalid={hasValidationError || undefined}>
                <FieldLabel htmlFor='fund-records-user-id'>
                  {t('User ID')}
                </FieldLabel>
                <Input
                  id='fund-records-user-id'
                  value={draftUserId}
                  inputMode='numeric'
                  autoComplete='off'
                  aria-invalid={hasValidationError || undefined}
                  onChange={(event) => {
                    setDraftUserId(event.target.value)
                    if (hasValidationError) setHasValidationError(false)
                  }}
                />
                {hasValidationError ? (
                  <FieldError>{t('Enter a valid user ID.')}</FieldError>
                ) : null}
              </Field>
            </FieldGroup>
            <Button type='submit' disabled={recordsQuery.isFetching}>
              {t('Search')}
            </Button>
          </div>
        </form>

        {userId !== null ? (
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <div className='flex flex-wrap items-center gap-2'>
              <Badge variant={total > 0 ? 'secondary' : 'outline'}>
                {t('{{count}} results', { count: total })}
              </Badge>
              <span className='text-muted-foreground text-xs'>
                {t('User ID')}: {userId}
              </span>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => recordsQuery.refetch()}
              disabled={recordsQuery.isFetching}
            >
              {t('Refresh')}
            </Button>
          </div>
        ) : null}

        {recordsContent}
      </div>
    </SettingsSection>
  )
}
