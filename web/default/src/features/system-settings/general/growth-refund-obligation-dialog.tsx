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
import { ShieldUserIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars } from '@/lib/format'

import type { RefundActionRequest } from './growth-refund-action-dialog'
import {
  getMinorAmountFactor,
  parseMinorAmount,
  type PromotionRefundResponsibleUser,
  type RefundActionIntent,
} from './growth-refund-types'

const manualObligationSchema = z.object({
  userId: z.string().max(32),
  topUpId: z.string().max(32),
  asset: z.enum(['quota', 'cash']),
  currency: z.string().max(3),
  amount: z.string().max(64),
  externalRef: z.string().max(191),
  remark: z.string().max(1000),
})

type ManualObligationValues = z.infer<typeof manualObligationSchema>

type GrowthRefundObligationDialogProps = {
  intent: RefundActionIntent
  submitting: boolean
  onCancel: () => void
  onSubmit: (request: RefundActionRequest) => Promise<void>
}

function responsibleUserRoleLabels(user: PromotionRefundResponsibleUser) {
  const roles: string[] = []
  if (user.is_top_up_user) {
    roles.push('Top-up account')
  }
  if (user.is_commission_recipient) {
    roles.push('Commission recipient')
  }
  if (user.is_rebate_recipient) {
    roles.push('Rebate recipient')
  }
  if (user.is_invitation_reward_recipient) {
    roles.push('Invitation reward recipient')
  }
  return roles.length > 0 ? roles : ['Refund account']
}

export function GrowthRefundObligationDialog(
  props: GrowthRefundObligationDialogProps
) {
  const { t } = useTranslation()
  const refundCase = props.intent.refundCase
  const responsibleUsers: PromotionRefundResponsibleUser[] =
    refundCase.responsible_users ?? []
  const defaultResponsibleUser =
    responsibleUsers.find((user) => user.user_id === refundCase.user_id) ??
    responsibleUsers[0]
  const defaultAsset =
    refundCase.subscription_status === 'active' ? 'cash' : 'quota'
  const form = useForm<ManualObligationValues>({
    resolver: zodResolver(manualObligationSchema),
    defaultValues: {
      userId: defaultResponsibleUser
        ? String(defaultResponsibleUser.user_id)
        : '',
      topUpId: refundCase.top_up_id > 0 ? String(refundCase.top_up_id) : '',
      asset: defaultAsset,
      currency: refundCase.currency || 'CNY',
      amount: '',
      externalRef: '',
      remark: '',
    },
  })
  const asset = form.watch('asset')
  const currency = form.watch('currency').trim().toUpperCase()
  const amountUnit = asset === 'cash' ? currency || 'CNY' : getCurrencyLabel()
  let amountStep = 'any'
  if (asset === 'cash') {
    const amountCurrency = /^[A-Z]{3}$/.test(currency) ? currency : 'CNY'
    amountStep = String(1 / getMinorAmountFactor(amountCurrency))
  }

  const submit = form.handleSubmit(async (values) => {
    const userId = Number(values.userId)
    if (!Number.isSafeInteger(userId) || userId <= 0) {
      form.setError('userId', { message: t('Enter a valid user ID.') })
      return
    }

    const topUpId = values.topUpId.trim() ? Number(values.topUpId) : 0
    if (!Number.isSafeInteger(topUpId) || topUpId < 0) {
      form.setError('topUpId', { message: t('Enter a valid top-up ID.') })
      return
    }

    const normalizedCurrency = values.currency.trim().toUpperCase()
    if (values.asset === 'cash' && !/^[A-Z]{3}$/.test(normalizedCurrency)) {
      form.setError('currency', {
        message: t('Enter a three-letter currency code.'),
      })
      return
    }
    let amount = 0
    if (values.asset === 'cash') {
      amount = parseMinorAmount(values.amount, normalizedCurrency || 'CNY') ?? 0
    } else {
      const displayAmount = Number(values.amount)
      if (!Number.isFinite(displayAmount) || displayAmount <= 0) {
        form.setError('amount', {
          message: t('Enter a positive recovery amount.'),
        })
        return
      }
      amount = parseQuotaFromDollars(displayAmount)
    }
    if (!Number.isSafeInteger(amount) || amount <= 0) {
      form.setError('amount', {
        message: t('Enter a valid recovery amount.'),
      })
      return
    }

    const externalRef = values.externalRef.trim()
    if (!externalRef) {
      form.setError('externalRef', {
        message: t('Evidence reference is required.'),
      })
      return
    }
    const remark = values.remark.trim()
    if (!remark) {
      form.setError('remark', {
        message: t('Explain how the recovery amount was determined.'),
      })
      return
    }

    await props.onSubmit({
      idempotency_key: props.intent.idempotencyKey,
      expected_responsibility_fingerprint:
        props.intent.refundCase.responsibility_fingerprint,
      action: 'define_manual_obligation',
      obligation_id: 0,
      user_id: userId,
      top_up_id: topUpId,
      asset: values.asset,
      currency: values.asset === 'cash' ? normalizedCurrency : '',
      amount,
      external_ref: externalRef,
      remark,
    })
  })

  return (
    <Dialog open onOpenChange={(open) => !open && props.onCancel()}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>{t('Create recovery obligation')}</DialogTitle>
          <DialogDescription>
            {t(
              'Use verified payment or support evidence to identify the liable user and exact recoverable amount.'
            )}
          </DialogDescription>
        </DialogHeader>

        <form id='refund-manual-obligation-form' onSubmit={submit} noValidate>
          <FieldGroup>
            <FieldSet data-invalid={Boolean(form.formState.errors.userId)}>
              <FieldLegend variant='label'>{t('Liable user')}</FieldLegend>
              <FieldDescription>
                {t(
                  'Select a user whose responsibility is supported by this refund case.'
                )}
              </FieldDescription>
              <input type='hidden' {...form.register('userId')} />
              {responsibleUsers.length > 0 ? (
                <RadioGroup
                  aria-label={t('Liable user')}
                  value={form.watch('userId')}
                  onValueChange={(value) =>
                    form.setValue('userId', value, { shouldValidate: true })
                  }
                  disabled={props.submitting}
                  className='grid gap-2 sm:grid-cols-2'
                >
                  {responsibleUsers.map((user) => {
                    const optionId = `refund-obligation-user-${user.user_id}`
                    return (
                      <FieldLabel
                        key={user.user_id}
                        htmlFor={optionId}
                        className='border-input has-[[data-state=checked]]:border-primary flex cursor-pointer items-start gap-3 rounded-lg border p-3 font-normal'
                      >
                        <RadioGroupItem
                          id={optionId}
                          value={String(user.user_id)}
                          aria-invalid={Boolean(form.formState.errors.userId)}
                        />
                        <span className='flex min-w-0 flex-col gap-0.5'>
                          <span className='font-medium'>
                            {responsibleUserRoleLabels(user)
                              .map((role) => t(role))
                              .join(' · ')}
                          </span>
                          <span className='text-muted-foreground text-xs break-all'>
                            {t('User ID')}: {user.user_id}
                            {user.username ? ` · ${user.username}` : ''}
                          </span>
                        </span>
                      </FieldLabel>
                    )
                  })}
                </RadioGroup>
              ) : (
                <Alert variant='destructive'>
                  <AlertTitle>
                    {t('No verified responsible account was found.')}
                  </AlertTitle>
                </Alert>
              )}
              <FieldError errors={[form.formState.errors.userId]} />
            </FieldSet>

            <div className='grid gap-4 sm:grid-cols-2'>
              <Field data-invalid={Boolean(form.formState.errors.topUpId)}>
                <FieldLabel htmlFor='refund-obligation-top-up-id'>
                  {t('Top-up ID')}
                </FieldLabel>
                <Input
                  id='refund-obligation-top-up-id'
                  type='number'
                  min='1'
                  step='1'
                  inputMode='numeric'
                  readOnly={refundCase.top_up_id > 0}
                  aria-invalid={Boolean(form.formState.errors.topUpId)}
                  disabled={props.submitting}
                  {...form.register('topUpId')}
                />
                <FieldDescription>
                  {t('Optional when the matching top-up cannot be identified.')}
                </FieldDescription>
                <FieldError errors={[form.formState.errors.topUpId]} />
              </Field>
            </div>

            <Field data-invalid={Boolean(form.formState.errors.asset)}>
              <FieldLabel id='refund-obligation-asset-label'>
                {t('Recovery asset')}
              </FieldLabel>
              <RadioGroup
                aria-labelledby='refund-obligation-asset-label'
                value={asset}
                onValueChange={(value) =>
                  form.setValue('asset', value as 'quota' | 'cash', {
                    shouldValidate: true,
                  })
                }
                className='flex flex-wrap gap-4'
              >
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='quota' id='refund-obligation-quota' />
                  <FieldLabel
                    htmlFor='refund-obligation-quota'
                    className='font-normal'
                  >
                    {t('API balance debt')}
                  </FieldLabel>
                </div>
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='cash' id='refund-obligation-cash' />
                  <FieldLabel
                    htmlFor='refund-obligation-cash'
                    className='font-normal'
                  >
                    {t('Cash debt')}
                  </FieldLabel>
                </div>
              </RadioGroup>
              <FieldError errors={[form.formState.errors.asset]} />
            </Field>

            <div className='grid gap-4 sm:grid-cols-2'>
              <Field data-invalid={Boolean(form.formState.errors.amount)}>
                <FieldLabel htmlFor='refund-obligation-amount'>
                  {t('Recovery amount ({{unit}})', { unit: amountUnit })}
                </FieldLabel>
                <Input
                  id='refund-obligation-amount'
                  type='number'
                  min='0'
                  step={amountStep}
                  inputMode='decimal'
                  aria-invalid={Boolean(form.formState.errors.amount)}
                  disabled={props.submitting}
                  {...form.register('amount')}
                />
                <FieldError errors={[form.formState.errors.amount]} />
              </Field>

              {asset === 'cash' ? (
                <Field data-invalid={Boolean(form.formState.errors.currency)}>
                  <FieldLabel htmlFor='refund-obligation-currency'>
                    {t('Currency')}
                  </FieldLabel>
                  <Input
                    id='refund-obligation-currency'
                    maxLength={3}
                    autoCapitalize='characters'
                    aria-invalid={Boolean(form.formState.errors.currency)}
                    disabled={props.submitting}
                    {...form.register('currency')}
                  />
                  <FieldDescription>
                    {t('Use the three-letter payment currency code.')}
                  </FieldDescription>
                  <FieldError errors={[form.formState.errors.currency]} />
                </Field>
              ) : null}
            </div>

            <Field data-invalid={Boolean(form.formState.errors.externalRef)}>
              <FieldLabel htmlFor='refund-obligation-evidence'>
                {t('Evidence reference')}
              </FieldLabel>
              <Input
                id='refund-obligation-evidence'
                maxLength={191}
                autoComplete='off'
                aria-invalid={Boolean(form.formState.errors.externalRef)}
                disabled={props.submitting}
                {...form.register('externalRef')}
              />
              <FieldDescription>
                {t(
                  'Use a provider event, payment record, support ticket, or other unique evidence reference.'
                )}
              </FieldDescription>
              <FieldError errors={[form.formState.errors.externalRef]} />
            </Field>

            <Field data-invalid={Boolean(form.formState.errors.remark)}>
              <FieldLabel htmlFor='refund-obligation-reason'>
                {t('Assessment reason')}
              </FieldLabel>
              <Textarea
                id='refund-obligation-reason'
                maxLength={1000}
                className='min-h-24'
                aria-invalid={Boolean(form.formState.errors.remark)}
                disabled={props.submitting}
                {...form.register('remark')}
              />
              <FieldDescription>
                {t('Explain how the user, asset, and amount were verified.')}
              </FieldDescription>
              <FieldError errors={[form.formState.errors.remark]} />
            </Field>
          </FieldGroup>
        </form>

        <Alert variant='destructive'>
          <HugeiconsIcon
            icon={ShieldUserIcon}
            strokeWidth={2}
            aria-hidden='true'
          />
          <AlertTitle>{t('Permanent Root assessment')}</AlertTitle>
          <AlertDescription>
            {t(
              'Creating this obligation increases refund debt, keeps the account frozen, and records the evidence, action, and fund journal entry together.'
            )}
          </AlertDescription>
        </Alert>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={props.submitting}
            onClick={props.onCancel}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='refund-manual-obligation-form'
            disabled={props.submitting || responsibleUsers.length === 0}
          >
            {props.submitting
              ? t('Processing...')
              : t('Create recovery obligation')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
