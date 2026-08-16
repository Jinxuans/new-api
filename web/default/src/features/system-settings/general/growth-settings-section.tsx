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
import { zodResolver } from '@hookform/resolvers/zod'
import type { ReactNode } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import { SettingsSection } from '../components/settings-section'
import { useUpdateGrowthConfig } from '../hooks/use-update-growth-config'
import {
  createRewardProgramSchema,
  toRewardProgramConfig,
  type RewardProgramFormValues,
} from './growth-admin-config'

type NumberFieldName = Exclude<
  keyof RewardProgramFormValues,
  'enabled' | 'dailyCheckinEnabled' | 'submissionEnabled'
>

const automationFields: Array<{
  name: NumberFieldName
  label: string
  description: string
}> = [
  {
    name: 'firstAPIKeyRewardQuota',
    label: 'First API key reward',
    description: 'Quota awarded after the user creates an API key.',
  },
  {
    name: 'firstAPIRequestRewardQuota',
    label: 'First API request reward',
    description: 'Quota awarded after the user completes the first request.',
  },
  {
    name: 'firstTopUpRewardQuota',
    label: 'First top-up reward',
    description: 'Quota awarded after the user completes the first top-up.',
  },
  {
    name: 'threeDayUsageRewardQuota',
    label: 'Three-day usage reward',
    description: 'Quota reserved for the consecutive usage reward item.',
  },
  {
    name: 'monthlySpendRewardQuota',
    label: 'Monthly spend reward',
    description: 'Quota awarded after the monthly spend target is reached.',
  },
  {
    name: 'monthlySpendTargetQuota',
    label: 'Monthly spend target',
    description: 'Monthly consumed quota required for the spend reward item.',
  },
]

const dailyCheckinFields: Array<{
  name: NumberFieldName
  label: string
  description: string
}> = [
  {
    name: 'dailyCheckinMinRewardQuota',
    label: 'Minimum check-in quota',
    description: 'Minimum quota amount awarded for check-in',
  },
  {
    name: 'dailyCheckinMaxRewardQuota',
    label: 'Maximum check-in quota',
    description: 'Maximum quota amount awarded for check-in',
  },
]

const budgetFields: Array<{
  name: NumberFieldName
  label: string
  description: string
}> = [
  {
    name: 'userDailyRewardLimitQuota',
    label: 'User daily reward limit',
    description:
      'Maximum growth reward quota per user per day. Zero means unlimited.',
  },
  {
    name: 'siteDailyBudgetQuota',
    label: 'Site daily reward budget',
    description:
      'Maximum growth reward quota for the whole site per day. Zero means unlimited.',
  },
]

const submissionFields: Array<{
  name: NumberFieldName
  label: string
  description: string
}> = [
  {
    name: 'submissionMinRewardQuota',
    label: 'Submission minimum reward',
    description: 'Default minimum quota for approved content submissions.',
  },
  {
    name: 'submissionMaxRewardQuota',
    label: 'Submission maximum reward',
    description: 'Suggested maximum quota for approved content submissions.',
  },
]

export function RewardProgramSettingsSection(props: {
  defaultValues: RewardProgramFormValues
}) {
  const { t } = useTranslation()
  const updateGrowthConfig = useUpdateGrowthConfig()

  const form = useForm<RewardProgramFormValues>({
    resolver: zodResolver(
      createRewardProgramSchema((key) => t(key))
    ) as unknown as Resolver<RewardProgramFormValues>,
    defaultValues: props.defaultValues,
  })

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')
  const dailyCheckinEnabled = form.watch('dailyCheckinEnabled')
  const submissionEnabled = form.watch('submissionEnabled')
  const saving = updateGrowthConfig.isPending || isSubmitting

  function onSubmit(values: RewardProgramFormValues) {
    updateGrowthConfig.mutate(
      { reward_program: toRewardProgramConfig(values) },
      { onSuccess: () => form.reset(values) }
    )
  }

  return (
    <SettingsSection
      title={t('Reward Program')}
      description={t(
        'Configure check-in, activation, retention, and content rewards.'
      )}
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          autoComplete='off'
          className='flex flex-col gap-6'
        >
          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel>{t('Enable growth rewards')}</FormLabel>
                    <FormDescription>
                      {t('Allow users to claim automatic growth rewards.')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={saving}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='submissionEnabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel>{t('Enable promotion submissions')}</FormLabel>
                    <FormDescription>
                      {t('Allow users to submit community and content proofs.')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={saving}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
          </div>

          <SettingsGroup
            title={t('Daily check-in rules')}
            description={t(
              'Allow users to claim a random quota reward once per day.'
            )}
            badge={
              dailyCheckinEnabled
                ? t('Daily check-in enabled')
                : t('Daily check-in disabled')
            }
          >
            <div className='flex flex-col gap-5'>
              <FormField
                control={form.control}
                name='dailyCheckinEnabled'
                render={({ field }) => (
                  <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                    <div className='space-y-0.5'>
                      <FormLabel>{t('Enable daily check-in')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Allow users to check in daily for random quota rewards'
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={saving}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
              <FieldGrid>
                {dailyCheckinFields.map((item) => (
                  <FormField
                    key={item.name}
                    control={form.control}
                    name={item.name}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t(item.label)}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            disabled={!dailyCheckinEnabled}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>{t(item.description)}</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                ))}
              </FieldGrid>
            </div>
          </SettingsGroup>

          <SettingsGroup
            title={t('Automatic reward rules')}
            description={t(
              'Global quota values used by one-time activation and retention tasks.'
            )}
            badge={
              enabled
                ? t('Growth rewards enabled')
                : t('Growth rewards disabled')
            }
          >
            <FieldGrid>
              {automationFields.map((item) => (
                <FormField
                  key={item.name}
                  control={form.control}
                  name={item.name}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t(item.label)}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          disabled={!enabled}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>{t(item.description)}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ))}
            </FieldGrid>
          </SettingsGroup>

          <SettingsGroup
            title={t('Content submission rules')}
            description={t(
              'Default reward range for reviewed promotion proof submissions.'
            )}
            badge={
              submissionEnabled
                ? t('Submissions enabled')
                : t('Submissions disabled')
            }
          >
            <FieldGrid>
              {submissionFields.map((item) => (
                <FormField
                  key={item.name}
                  control={form.control}
                  name={item.name}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t(item.label)}</FormLabel>
                      <FormControl>
                        <Input type='number' min={0} {...field} />
                      </FormControl>
                      <FormDescription>{t(item.description)}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ))}
            </FieldGrid>
          </SettingsGroup>

          <SettingsGroup
            title={t('Budget limits')}
            description={t(
              'Optional guardrails that cap rewards per user or across the site each day.'
            )}
          >
            <FieldGrid>
              {budgetFields.map((item) => (
                <FormField
                  key={item.name}
                  control={form.control}
                  name={item.name}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t(item.label)}</FormLabel>
                      <FormControl>
                        <Input type='number' min={0} {...field} />
                      </FormControl>
                      <FormDescription>{t(item.description)}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ))}
            </FieldGrid>
          </SettingsGroup>

          <Button type='submit' disabled={!isDirty || saving} className='w-fit'>
            {saving ? t('Saving...') : t('Save reward program')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}

function SettingsGroup({
  title,
  description,
  badge,
  children,
}: {
  title: string
  description: string
  badge?: string
  children: ReactNode
}) {
  return (
    <div className='rounded-lg border p-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex min-w-0 flex-col gap-1'>
          <h4 className='text-sm font-semibold'>{title}</h4>
          <p className='text-muted-foreground text-xs leading-5'>
            {description}
          </p>
        </div>
        {badge ? <Badge variant='outline'>{badge}</Badge> : null}
      </div>
      <Separator className='my-4' />
      {children}
    </div>
  )
}

function FieldGrid({ children }: { children: ReactNode }) {
  return (
    <div className='grid gap-5 md:grid-cols-2 xl:grid-cols-3'>{children}</div>
  )
}
