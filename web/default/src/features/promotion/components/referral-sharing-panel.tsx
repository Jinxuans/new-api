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
import { AffiliateIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { formatQuota } from '@/lib/format'

import {
  getPromotionOverview,
  getPromotionReferralTools,
  promotionQueryKeys,
} from '../api'
import {
  PromotionSectionError,
  PromotionSectionSkeleton,
} from './section-state'

export function ReferralSharingPanel() {
  const { t } = useTranslation()
  const referralQuery = useQuery({
    queryKey: promotionQueryKeys.referralTools,
    queryFn: getPromotionReferralTools,
  })
  const overviewQuery = useQuery({
    queryKey: promotionQueryKeys.overview,
    queryFn: getPromotionOverview,
    retry: false,
  })

  let content
  if (referralQuery.isLoading) {
    content = <PromotionSectionSkeleton />
  } else if (referralQuery.isError || !referralQuery.data) {
    content = <PromotionSectionError onRetry={() => referralQuery.refetch()} />
  } else {
    content = (
      <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end'>
        <div className='grid gap-3 sm:grid-cols-2'>
          <Field>
            <FieldLabel htmlFor='promotion-referral-link'>
              {t('Referral link')}
            </FieldLabel>
            <div className='flex gap-2'>
              <Input
                id='promotion-referral-link'
                value={referralQuery.data.referralLink}
                readOnly
                className='min-w-0 font-mono text-xs'
              />
              <CopyButton
                value={referralQuery.data.referralLink}
                variant='outline'
                className='size-11 shrink-0 sm:size-8'
                tooltip={t('Copy referral link')}
                aria-label={t('Copy referral link')}
              />
            </div>
          </Field>
          <Field>
            <FieldLabel htmlFor='promotion-referral-code'>
              {t('Referral code')}
            </FieldLabel>
            <div className='flex gap-2'>
              <Input
                id='promotion-referral-code'
                value={referralQuery.data.referralCode}
                readOnly
                className='font-mono'
              />
              <CopyButton
                value={referralQuery.data.referralCode}
                variant='outline'
                className='size-11 shrink-0 sm:size-8'
                tooltip={t('Copy referral code')}
                aria-label={t('Copy referral code')}
              />
            </div>
          </Field>
        </div>
        <div className='flex flex-wrap gap-2 lg:justify-end'>
          {overviewQuery.data?.invitation_chain_reward_quota ? (
            <Badge variant='secondary'>
              {t('Milestone rewards up to {{quota}}', {
                quota: formatQuota(
                  overviewQuery.data.invitation_chain_reward_quota
                ),
              })}
            </Badge>
          ) : null}
          {overviewQuery.data?.invite_rebate_percent ? (
            <Badge variant='outline'>
              {t('{{rate}}% eligible top-up rebate', {
                rate: overviewQuery.data.invite_rebate_percent,
              })}
            </Badge>
          ) : null}
        </div>
      </div>
    )
  }

  return (
    <Card data-card-hover='false'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <HugeiconsIcon icon={AffiliateIcon} strokeWidth={2} />
          {t('Referral rewards')}
        </CardTitle>
        <CardDescription>
          {t(
            'Share one link. Valid registrations, first actions, and eligible top-ups can earn rewards.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>{content}</CardContent>
    </Card>
  )
}
