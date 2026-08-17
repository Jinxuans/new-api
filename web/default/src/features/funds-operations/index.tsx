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
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { GrowthRefundCasesSection } from '@/features/system-settings/general/growth-refund-cases-section'
import { GrowthWithdrawalsReviewSection } from '@/features/system-settings/general/growth-withdrawals-review-section'

import { FundRecordsSection } from './fund-records-section'

export function FundsOperations() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Funds Operations')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Review withdrawals, recover refunds, and trace each fund movement from one workspace.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <Tabs defaultValue='withdrawals' className='min-w-0 gap-4'>
          <TabsList
            aria-label={t('Funds Operations')}
            className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'
          >
            <TabsTrigger value='withdrawals'>
              {t('Withdrawal Reviews')}
            </TabsTrigger>
            <TabsTrigger value='refunds'>{t('Refund recovery')}</TabsTrigger>
            <TabsTrigger value='funds'>{t('Fund history')}</TabsTrigger>
          </TabsList>
          <TabsContent value='withdrawals' keepMounted>
            <GrowthWithdrawalsReviewSection />
          </TabsContent>
          <TabsContent value='refunds' keepMounted>
            <GrowthRefundCasesSection />
          </TabsContent>
          <TabsContent value='funds' keepMounted>
            <FundRecordsSection />
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
