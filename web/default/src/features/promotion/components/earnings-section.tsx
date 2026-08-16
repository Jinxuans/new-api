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
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  getPromotionOverview,
  promotionQueryKeys,
  toPromotionEarnings,
} from '../api'
import { CashActionDialogs } from './cash-action-dialogs'
import { EarningsBalances } from './earnings-balances'
import { ReferralTransferDialog } from './referral-transfer-dialog'
import {
  PromotionSectionError,
  PromotionSectionSkeleton,
} from './section-state'

export function EarningsSection() {
  const { t } = useTranslation()
  const [referralTransferOpen, setReferralTransferOpen] = useState(false)
  const [cashConversionOpen, setCashConversionOpen] = useState(false)
  const [withdrawalOpen, setWithdrawalOpen] = useState(false)
  const overviewQuery = useQuery({
    queryKey: promotionQueryKeys.overview,
    queryFn: getPromotionOverview,
    retry: false,
  })
  const earnings = overviewQuery.data
    ? toPromotionEarnings(overviewQuery.data)
    : null

  let content: ReactNode
  if (overviewQuery.isLoading) {
    content = <PromotionSectionSkeleton />
  } else if (overviewQuery.isError || !earnings) {
    content = <PromotionSectionError onRetry={() => overviewQuery.refetch()} />
  } else {
    content = (
      <EarningsBalances
        earnings={earnings}
        onTransferReferral={() => setReferralTransferOpen(true)}
        onConvertCash={() => setCashConversionOpen(true)}
        onWithdrawCash={() => setWithdrawalOpen(true)}
      />
    )
  }

  return (
    <section
      id='my-earnings'
      aria-labelledby='my-earnings-title'
      className='scroll-mt-20 space-y-4'
    >
      <div>
        <h2 id='my-earnings-title' className='text-xl font-semibold'>
          {t('My earnings')}
        </h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t(
            'Task rewards, referral credit, and cash commission stay separate so every balance is clear.'
          )}
        </p>
      </div>
      {content}

      {earnings ? (
        <>
          <ReferralTransferDialog
            open={referralTransferOpen}
            onOpenChange={setReferralTransferOpen}
            quota={earnings.transferableReferralCredit}
          />
          <CashActionDialogs
            earnings={earnings}
            conversionOpen={cashConversionOpen}
            onConversionOpenChange={setCashConversionOpen}
            withdrawalOpen={withdrawalOpen}
            onWithdrawalOpenChange={setWithdrawalOpen}
          />
        </>
      ) : null}
    </section>
  )
}
