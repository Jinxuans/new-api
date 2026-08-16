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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import type {
  ReferralProgramFormValues,
  RewardProgramFormValues,
} from '../general/growth-admin-config'
import { GrowthRefundCasesSection } from '../general/growth-refund-cases-section'
import { GrowthRewardItemsSection } from '../general/growth-reward-items-section'
import { RewardProgramSettingsSection } from '../general/growth-settings-section'
import { GrowthSubmissionsReviewSection } from '../general/growth-submissions-review-section'
import { GrowthWithdrawalsReviewSection } from '../general/growth-withdrawals-review-section'
import { ReferralProgramSettingsSection } from '../general/referral-program-settings-section'

export function RewardProgramAdminSection(props: {
  defaultValues: RewardProgramFormValues
}) {
  const { t } = useTranslation()

  return (
    <Tabs defaultValue='rules' className='min-w-0 gap-5'>
      <TabsList
        aria-label={t('Reward Program')}
        className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'
      >
        <TabsTrigger value='rules'>{t('Reward rules')}</TabsTrigger>
        <TabsTrigger value='items'>{t('Reward Task Items')}</TabsTrigger>
        <TabsTrigger value='reviews'>{t('Content Reward Reviews')}</TabsTrigger>
      </TabsList>
      <TabsContent value='rules' keepMounted>
        <ProgramPanel
          title={t('Reward rules')}
          description={t(
            'Configure check-in, activation, retention, content rewards, and budget limits.'
          )}
        >
          <RewardProgramSettingsSection defaultValues={props.defaultValues} />
        </ProgramPanel>
      </TabsContent>
      <TabsContent value='items' keepMounted>
        <ProgramPanel
          title={t('Reward Task Items')}
          description={t(
            'Control which tasks users see and override task-specific behavior.'
          )}
        >
          <GrowthRewardItemsSection />
        </ProgramPanel>
      </TabsContent>
      <TabsContent value='reviews' keepMounted>
        <ProgramPanel
          title={t('Content Reward Reviews')}
          description={t(
            'Review submitted proof before adding quota to a user balance.'
          )}
        >
          <GrowthSubmissionsReviewSection />
        </ProgramPanel>
      </TabsContent>
    </Tabs>
  )
}

export function ReferralProgramAdminSection(props: {
  defaultValues: ReferralProgramFormValues
  complianceConfirmed: boolean
}) {
  const { t } = useTranslation()

  return (
    <Tabs defaultValue='rules' className='min-w-0 gap-5'>
      <TabsList
        aria-label={t('Referral Program')}
        className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'
      >
        <TabsTrigger value='rules'>{t('Referral rules')}</TabsTrigger>
        <TabsTrigger value='withdrawals'>{t('Withdrawal Reviews')}</TabsTrigger>
        <TabsTrigger value='refunds'>{t('Refund cases')}</TabsTrigger>
      </TabsList>
      <TabsContent value='rules' keepMounted>
        <ProgramPanel
          title={t('Referral rules')}
          description={t(
            'Configure rewards and commission for invitation relationships.'
          )}
        >
          <ReferralProgramSettingsSection
            defaultValues={props.defaultValues}
            complianceConfirmed={props.complianceConfirmed}
          />
        </ProgramPanel>
      </TabsContent>
      <TabsContent value='withdrawals' keepMounted>
        <ProgramPanel
          title={t('Withdrawal Reviews')}
          description={t(
            'Review cash commission withdrawals and record offline payment.'
          )}
        >
          <GrowthWithdrawalsReviewSection />
        </ProgramPanel>
      </TabsContent>
      <TabsContent value='refunds' keepMounted>
        <ProgramPanel
          title={t('Refund cases')}
          description={t(
            'Review refunds that need manual commission recovery or quota adjustment.'
          )}
        >
          <GrowthRefundCasesSection />
        </ProgramPanel>
      </TabsContent>
    </Tabs>
  )
}

function ProgramPanel(props: {
  title: string
  description: string
  children: ReactNode
}) {
  return (
    <section className='flex min-w-0 flex-col gap-4'>
      <div className='space-y-1'>
        <h2 className='text-sm font-semibold'>{props.title}</h2>
        <p className='text-muted-foreground max-w-3xl text-sm'>
          {props.description}
        </p>
      </div>
      {props.children}
    </section>
  )
}
