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
import { GiftIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { useStatus } from '@/hooks/use-status'

import { ContentSubmissionPanel } from './content-submission-panel'
import { ReferralSharingPanel } from './referral-sharing-panel'
import { PromotionSectionSkeleton } from './section-state'
import { TaskRewardsPanel } from './task-rewards-panel'

export function EarnRewardsSection() {
  const { t } = useTranslation()
  const { status, loading } = useStatus()
  let taskAndContent
  if (loading) {
    taskAndContent = <PromotionSectionSkeleton />
  } else if (status?.growth_center_enabled !== true) {
    taskAndContent = (
      <Empty className='border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={GiftIcon} strokeWidth={2} />
          </EmptyMedia>
          <EmptyTitle>{t('Reward tasks are not available')}</EmptyTitle>
          <EmptyDescription>
            {t('Referral rewards remain available below.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    taskAndContent = (
      <div className='grid gap-4 lg:grid-cols-2'>
        <TaskRewardsPanel />
        <ContentSubmissionPanel />
      </div>
    )
  }

  return (
    <section
      id='earn-rewards'
      aria-labelledby='earn-rewards-title'
      className='scroll-mt-20 space-y-4'
    >
      <div>
        <h2 id='earn-rewards-title' className='text-xl font-semibold'>
          {t('Earn rewards')}
        </h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t(
            'Choose a task, publish useful content, or invite a real customer.'
          )}
        </p>
      </div>
      {taskAndContent}
      <ReferralSharingPanel />
    </section>
  )
}
