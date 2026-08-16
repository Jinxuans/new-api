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
import { AlertCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'

export function PromotionSectionSkeleton() {
  const { t } = useTranslation()
  return (
    <div aria-label={t('Loading')} className='grid gap-3 sm:grid-cols-3'>
      <Skeleton className='h-28 w-full' />
      <Skeleton className='h-28 w-full' />
      <Skeleton className='h-28 w-full' />
    </div>
  )
}

type PromotionSectionErrorProps = {
  onRetry: () => void
}

export function PromotionSectionError(props: PromotionSectionErrorProps) {
  const { t } = useTranslation()
  return (
    <Empty className='border'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
        </EmptyMedia>
        <EmptyTitle>{t('This section could not be loaded')}</EmptyTitle>
        <EmptyDescription>
          {t('Retry without losing the rest of the page.')}
        </EmptyDescription>
      </EmptyHeader>
      <Button type='button' variant='outline' onClick={props.onRetry}>
        {t('Retry')}
      </Button>
    </Empty>
  )
}
