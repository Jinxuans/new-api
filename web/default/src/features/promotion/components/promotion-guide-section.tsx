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
import {
  ArrowDown01Icon,
  BookOpen01Icon,
  CheckListIcon,
  ShieldUserIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Separator } from '@/components/ui/separator'
import { statusVariant } from '@/features/promotion/shared'

const GUIDE_STEPS = [
  'Share your referral link with people who can genuinely benefit from the API service.',
  'Help new users register, create an API key, and complete their first successful request.',
  'Track settlement here, then move settled referral credit or cash commission using the matching action.',
] as const

const PROMOTION_EXAMPLES = [
  'Publish a practical tutorial that walks through API key creation and a first request.',
  'Create a model comparison or integration video and include your referral link in the description.',
  'Add the service to relevant developer resources, project documentation, or tool directories.',
] as const

const PROGRAM_RULES = [
  'A customer must register through your referral link or code before eligible actions can be attributed to you.',
  'Self-referrals, abnormal registrations, refunded orders, and risk-control orders do not earn rewards.',
  'Referral credit is for API balance. Cash commission is tracked separately and may support withdrawal.',
] as const

const STATUS_GUIDE = [
  ['pending', 'Waiting for settlement or review; it cannot be moved yet.'],
  ['settled', 'Available for the action shown in My earnings.'],
  ['transferred', 'Already moved into API balance.'],
  ['withdrawing', 'Locked while a cash withdrawal is reviewed.'],
  ['reversed', 'Removed because the related order was refunded or reversed.'],
] as const

export function PromotionGuideSection() {
  const { t } = useTranslation()
  return (
    <section
      id='promotion-guide'
      aria-labelledby='promotion-guide-title'
      className='scroll-mt-20 space-y-4'
    >
      <Collapsible>
        <div className='flex items-start justify-between gap-4'>
          <div>
            <h2 id='promotion-guide-title' className='text-xl font-semibold'>
              {t('Promotion guide')}
            </h2>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t(
                'A short guide to attribution, settlement, and compliant promotion.'
              )}
            </p>
          </div>
          <CollapsibleTrigger className='group hover:bg-muted focus-visible:border-ring focus-visible:ring-ring/50 flex min-h-11 shrink-0 items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium outline-none focus-visible:ring-3'>
            {t('Show promotion guide')}
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              strokeWidth={2}
              className='size-4 transition-transform group-data-[panel-open]:rotate-180'
            />
          </CollapsibleTrigger>
        </div>

        <CollapsibleContent className='pt-4'>
          <Card data-card-hover='false'>
            <CardContent className='space-y-6'>
              <div className='grid gap-6 lg:grid-cols-3'>
                <section aria-labelledby='promotion-how-it-works'>
                  <h3
                    id='promotion-how-it-works'
                    className='flex items-center gap-2 font-semibold'
                  >
                    <HugeiconsIcon icon={BookOpen01Icon} strokeWidth={2} />
                    {t('How it works')}
                  </h3>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    {t('Follow the same three steps for every referral.')}
                  </p>
                  <ol className='mt-4 space-y-3'>
                    {GUIDE_STEPS.map((step, index) => (
                      <li key={step} className='flex gap-3 text-sm leading-6'>
                        <span className='bg-primary text-primary-foreground mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold'>
                          {index + 1}
                        </span>
                        <span>{t(step)}</span>
                      </li>
                    ))}
                  </ol>
                </section>

                <section aria-labelledby='promotion-useful-ideas'>
                  <h3
                    id='promotion-useful-ideas'
                    className='flex items-center gap-2 font-semibold'
                  >
                    <HugeiconsIcon icon={CheckListIcon} strokeWidth={2} />
                    {t('Useful promotion ideas')}
                  </h3>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    {t(
                      'Lead with useful content instead of a bare referral link.'
                    )}
                  </p>
                  <ul className='mt-4 list-disc space-y-2 ps-5 text-sm leading-6'>
                    {PROMOTION_EXAMPLES.map((example) => (
                      <li key={example}>{t(example)}</li>
                    ))}
                  </ul>
                </section>

                <section aria-labelledby='promotion-program-rules'>
                  <h3
                    id='promotion-program-rules'
                    className='flex items-center gap-2 font-semibold'
                  >
                    <HugeiconsIcon icon={ShieldUserIcon} strokeWidth={2} />
                    {t('Program rules')}
                  </h3>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    {t(
                      'Only genuine, attributable customer activity is eligible.'
                    )}
                  </p>
                  <ul className='mt-4 list-disc space-y-2 ps-5 text-sm leading-6'>
                    {PROGRAM_RULES.map((rule) => (
                      <li key={rule}>{t(rule)}</li>
                    ))}
                  </ul>
                </section>
              </div>

              <Separator />

              <section aria-labelledby='promotion-status-guide'>
                <h3 id='promotion-status-guide' className='font-semibold'>
                  {t('What each status means')}
                </h3>
                <div className='mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
                  {STATUS_GUIDE.map(([status, description]) => (
                    <div key={status} className='rounded-lg border p-3'>
                      <Badge variant={statusVariant(status)}>{t(status)}</Badge>
                      <p className='text-muted-foreground mt-2 text-xs leading-5'>
                        {t(description)}
                      </p>
                    </div>
                  ))}
                </div>
              </section>
            </CardContent>
          </Card>
        </CollapsibleContent>
      </Collapsible>
    </section>
  )
}
