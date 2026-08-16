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
import { CheckmarkCircle02Icon, Task01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { statusVariant } from '@/features/promotion/shared'
import { formatQuota } from '@/lib/format'

import { getPromotionRewardItems, promotionQueryKeys } from '../api'
import { useTaskRewardClaim } from '../hooks/use-task-reward-claim'
import {
  getRewardAmount,
  getRewardDescription,
  getRewardTitle,
} from '../reward-display'
import {
  PromotionSectionError,
  PromotionSectionSkeleton,
} from './section-state'
import { TaskRewardClaimDialogs } from './task-reward-claim-dialogs'

const JOIN_COMMUNITY_CODE = 'join_community'
const MONTHLY_SPEND_TARGET_CODE = 'monthly_spend_target'

export function TaskRewardsPanel() {
  const { t } = useTranslation()
  const claim = useTaskRewardClaim()
  const itemsQuery = useQuery({
    queryKey: promotionQueryKeys.rewardItems,
    queryFn: getPromotionRewardItems,
  })
  const items = useMemo(
    () =>
      (itemsQuery.data || []).filter(
        (item) => item.item_type === 'auto' || item.code === JOIN_COMMUNITY_CODE
      ),
    [itemsQuery.data]
  )

  let content
  if (itemsQuery.isLoading) {
    content = <PromotionSectionSkeleton />
  } else if (itemsQuery.isError) {
    content = <PromotionSectionError onRetry={() => itemsQuery.refetch()} />
  } else if (items.length === 0) {
    content = (
      <p className='text-muted-foreground text-sm'>
        {t('No reward tasks are available right now.')}
      </p>
    )
  } else {
    content = (
      <div className='divide-y rounded-lg border'>
        {items.map((item) => {
          const progressCurrent = Number(item.progress_current_quota || 0)
          const progressTarget = Number(item.progress_target_quota || 0)
          return (
            <div
              key={item.code}
              className='grid gap-3 p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'
            >
              <div className='min-w-0'>
                <div className='flex flex-wrap items-center gap-2'>
                  <h3 className='text-sm font-medium'>
                    {getRewardTitle(item, t)}
                  </h3>
                  <Badge variant={statusVariant(item.status)}>
                    {t(item.status)}
                  </Badge>
                </div>
                <p className='text-muted-foreground mt-1 text-xs leading-5'>
                  {getRewardDescription(item, t)}
                </p>
                {item.code === MONTHLY_SPEND_TARGET_CODE &&
                progressTarget > 0 ? (
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t('Monthly consumed {{current}} / target {{target}}', {
                      current: formatQuota(progressCurrent),
                      target: formatQuota(progressTarget),
                    })}
                  </p>
                ) : null}
              </div>
              <div className='flex items-center justify-between gap-3 sm:justify-end'>
                <span className='text-sm font-semibold tabular-nums'>
                  {getRewardAmount(item)}
                </span>
                <Button
                  type='button'
                  size='sm'
                  className='min-h-11 sm:min-h-8'
                  disabled={
                    !item.claimable ||
                    item.status === 'completed' ||
                    claim.isPending
                  }
                  onClick={() => claim.claimItem(item)}
                >
                  <HugeiconsIcon icon={CheckmarkCircle02Icon} strokeWidth={2} />
                  {t('Claim')}
                </Button>
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <>
      <Card data-card-hover='false'>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <HugeiconsIcon icon={Task01Icon} strokeWidth={2} />
            {t('Reward tasks')}
          </CardTitle>
          <CardDescription>
            {t(
              'Complete account milestones and claim rewards directly to your API balance.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>{content}</CardContent>
      </Card>
      <TaskRewardClaimDialogs claim={claim} />
    </>
  )
}
