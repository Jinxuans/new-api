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

import { ActivitySection } from './components/activity-section'
import { EarnRewardsSection } from './components/earn-rewards-section'
import { EarningsSection } from './components/earnings-section'
import { PromotionGuideSection } from './components/promotion-guide-section'

const SECTION_LINKS = [
  ['#earn-rewards', 'Earn rewards'],
  ['#my-earnings', 'My earnings'],
  ['#earnings-history', 'Fund history'],
  ['#promotion-guide', 'Promotion guide'],
] as const

export function PromotionCenter() {
  const { t } = useTranslation()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Rewards & referrals')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Earn API balance, track referral credit, and manage cash commission in one place.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-8'>
          <nav
            aria-label={t('Rewards page sections')}
            className='border-border bg-background/95 sticky top-0 z-10 -mx-3 overflow-x-auto border-y px-3 py-2 backdrop-blur sm:mx-0 sm:rounded-lg sm:border'
          >
            <div className='flex w-max min-w-full gap-1 sm:w-auto'>
              {SECTION_LINKS.map(([href, label]) => (
                <a
                  key={href}
                  href={href}
                  className='hover:bg-muted focus-visible:border-ring focus-visible:ring-ring/50 shrink-0 rounded-md border border-transparent px-3 py-2 text-sm font-medium outline-none focus-visible:ring-3'
                >
                  {t(label)}
                </a>
              ))}
            </div>
          </nav>

          <EarnRewardsSection />
          <EarningsSection />
          <ActivitySection />
          <PromotionGuideSection />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
