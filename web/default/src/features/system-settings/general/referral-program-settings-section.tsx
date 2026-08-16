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

import { Alert, AlertDescription } from '@/components/ui/alert'
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

import { SettingsSection } from '../components/settings-section'
import { useUpdateGrowthConfig } from '../hooks/use-update-growth-config'
import {
  referralProgramSchema,
  toReferralProgramConfig,
  type ReferralProgramFormValues,
} from './growth-admin-config'

type ReferralProgramSettingsSectionProps = {
  defaultValues: ReferralProgramFormValues
  complianceConfirmed: boolean
}

export function ReferralProgramSettingsSection(
  props: ReferralProgramSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateGrowthConfig = useUpdateGrowthConfig()
  const form = useForm<ReferralProgramFormValues>({
    resolver: zodResolver(
      referralProgramSchema
    ) as unknown as Resolver<ReferralProgramFormValues>,
    defaultValues: props.defaultValues,
  })

  const values = form.watch()
  const hasPositiveReward = Object.entries(values).some(
    ([key, value]) => key !== 'rebateFreezeDays' && Number(value) > 0
  )
  const saving = updateGrowthConfig.isPending || form.formState.isSubmitting
  const saveBlocked = !props.complianceConfirmed && hasPositiveReward

  function onSubmit(formValues: ReferralProgramFormValues) {
    updateGrowthConfig.mutate(
      { referral_program: toReferralProgramConfig(formValues) },
      { onSuccess: () => form.reset(formValues) }
    )
  }

  return (
    <SettingsSection
      title={t('Referral Program')}
      description={t(
        'Configure registration rewards, inviter milestones, and top-up commission.'
      )}
    >
      {!props.complianceConfirmed ? (
        <Alert variant='destructive'>
          <AlertDescription>
            {t(
              'Non-zero invitation rewards require compliance confirmation in Payment Gateway settings.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          autoComplete='off'
          className='flex flex-col gap-6'
        >
          <ReferralSettingsGroup
            title={t('Registration rewards')}
            description={t(
              'Quota granted once when an invitation creates a new account.'
            )}
          >
            <div className='grid gap-5 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='inviterRegistrationRewardQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Inviter Reward')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('Quota awarded to the inviter after registration.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='inviteeRegistrationRewardQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Invitee Reward')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Quota awarded to the invited user after registration.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </ReferralSettingsGroup>

          <ReferralSettingsGroup
            title={t('Inviter milestone rewards')}
            description={t(
              'One-time quota rewards triggered by the invited user.'
            )}
          >
            <div className='grid gap-5 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='inviteFirstRequestRewardQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Inviter first request reward')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Quota awarded to the inviter when an invited user completes the first API request.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='inviteFirstTopUpRewardQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Inviter first top-up reward')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Quota awarded to the inviter when an invited user completes the first top-up.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </ReferralSettingsGroup>

          <ReferralSettingsGroup
            title={t('Top-up commission')}
            description={t(
              'Commission credited from invited-user top-ups after the freeze period.'
            )}
          >
            <div className='grid gap-5 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='inviteRebatePercentage'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Referral Rebate Percentage')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={100}
                        step={0.1}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Percentage awarded to inviters from invited user top-ups.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='rebateFreezeDays'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Rebate freeze days')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} max={3650} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('Days before future rebates become settleable.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </ReferralSettingsGroup>

          <Button
            type='submit'
            disabled={!form.formState.isDirty || saving || saveBlocked}
            className='w-fit'
          >
            {saving ? t('Saving...') : t('Save referral program')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}

function ReferralSettingsGroup(props: {
  title: string
  description: string
  children: ReactNode
}) {
  return (
    <section className='rounded-lg border p-4'>
      <div className='space-y-1'>
        <h3 className='text-sm font-semibold'>{props.title}</h3>
        <p className='text-muted-foreground text-xs leading-5'>
          {props.description}
        </p>
      </div>
      <Separator className='my-4' />
      {props.children}
    </section>
  )
}
