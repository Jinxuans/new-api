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
import { AffiliateIcon, ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useIsSidebarModuleVisible } from '@/hooks/use-sidebar-config'

interface AffiliateRewardsCardProps {
  loading?: boolean
}

export function AffiliateRewardsCard(props: AffiliateRewardsCardProps) {
  const { t } = useTranslation()
  const isPromotionVisible = useIsSidebarModuleVisible('/promotion')
  if (!isPromotionVisible) return null

  if (props.loading) {
    return (
      <Card data-card-hover='false' className='bg-muted/20 py-0'>
        <CardContent className='grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
          <div>
            <Skeleton className='h-5 w-40' />
            <Skeleton className='mt-2 h-4 w-64 max-w-full' />
          </div>
          <Skeleton className='h-8 w-44' />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
        <div className='flex min-w-0 items-center gap-3'>
          <IconBadge tone='chart-3'>
            <HugeiconsIcon
              icon={AffiliateIcon}
              strokeWidth={2}
              aria-hidden='true'
            />
          </IconBadge>
          <div className='min-w-0'>
            <h3 className='text-sm font-semibold'>
              {t('Rewards & referrals')}
            </h3>
            <p className='text-muted-foreground text-xs leading-5'>
              {t(
                'View reward tasks, share your referral link, and manage every type of earnings.'
              )}
            </p>
          </div>
        </div>

        <Link
          to='/promotion'
          className={buttonVariants({
            variant: 'outline',
            className: 'min-h-11 w-full sm:w-auto',
          })}
        >
          {t('View rewards & referrals')}
          <HugeiconsIcon
            icon={ArrowRight01Icon}
            strokeWidth={2}
            aria-hidden='true'
          />
        </Link>
      </CardContent>
    </Card>
  )
}
