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
import { Activity01Icon, Clock01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldLabel } from '@/components/ui/field'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { PromotionActivityFilter } from '../api'
import { usePromotionActivity } from '../hooks/use-promotion-activity'
import { PromotionActivityRows } from './activity-rows'
import {
  PromotionSectionError,
  PromotionSectionSkeleton,
} from './section-state'

export function ActivitySection() {
  const { t } = useTranslation()
  const activity = usePromotionActivity()
  const filters: Array<{ value: PromotionActivityFilter; label: string }> = [
    { value: 'funds', label: t('Fund flow') },
    { value: 'tasks', label: t('Task rewards') },
    { value: 'submissions', label: t('Content submissions') },
    { value: 'referrals', label: t('Referral credit') },
    { value: 'commissions', label: t('Cash commission') },
    { value: 'withdrawals', label: t('Cash withdrawals') },
  ]

  let content: ReactNode
  if (activity.query.isLoading) {
    content = <PromotionSectionSkeleton />
  } else if (activity.query.isError) {
    content = <PromotionSectionError onRetry={() => activity.query.refetch()} />
  } else if (!activity.query.data?.items.length) {
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
          <EmptyTitle>
            {activity.filter === 'funds'
              ? t('No fund records yet')
              : t('No activity yet')}
          </EmptyTitle>
          <EmptyDescription>
            {activity.filter === 'funds'
              ? t(
                  'This journal covers account credits, rewards, promotion funds, and recovery adjustments. API usage charges remain in usage logs, so this is not a complete wallet ledger.'
                )
              : t('New reward and referral activity will appear here.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <>
        <div
          className='divide-y rounded-lg border'
          aria-busy={activity.query.isFetching}
        >
          <PromotionActivityRows
            filter={activity.filter}
            items={activity.query.data.items}
          />
        </div>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <p className='text-muted-foreground text-center text-xs sm:text-left'>
            {t('{{total}} records · Page {{page}} of {{pages}}', {
              total: activity.query.data.total,
              page: activity.page,
              pages: activity.totalPages,
            })}
          </p>
          <div className='grid grid-cols-2 gap-2'>
            <Button
              type='button'
              variant='outline'
              disabled={activity.page <= 1 || activity.query.isFetching}
              onClick={activity.previousPage}
            >
              {t('Previous')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={
                activity.page >= activity.totalPages ||
                activity.query.isFetching
              }
              onClick={activity.nextPage}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </>
    )
  }

  return (
    <section
      id='earnings-history'
      aria-labelledby='earnings-history-title'
      className='flex scroll-mt-20 flex-col gap-4'
    >
      <div className='flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between'>
        <div>
          <h2
            id='earnings-history-title'
            className='flex items-center gap-2 text-xl font-semibold'
          >
            <HugeiconsIcon
              icon={Activity01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            {t('Fund history')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'This journal covers account credits, rewards, promotion funds, and recovery adjustments. API usage charges remain in usage logs, so this is not a complete wallet ledger.'
            )}
          </p>
        </div>
        <Field className='sm:w-56'>
          <FieldLabel htmlFor='promotion-activity-filter'>
            {t('Activity type')}
          </FieldLabel>
          <Select
            items={filters}
            value={activity.filter}
            onValueChange={(value) => {
              if (value) {
                activity.setFilter(value as PromotionActivityFilter)
              }
            }}
          >
            <SelectTrigger id='promotion-activity-filter' className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {filters.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Card data-card-hover='false'>
        <CardContent className='flex flex-col gap-4'>{content}</CardContent>
      </Card>
    </section>
  )
}
