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
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'

import type {
  CreateRefundFormValues,
  PartialAmountMode,
  RefundIntakeSource,
  RefundKind,
} from './growth-refund-create-types'

type RefundSelectFieldProps = {
  id: string
  label: string
  description: string
  value: string
  options: Array<{ value: string; label: string }>
  disabled: boolean
  onValueChange: (value: string) => void
}

function RefundSelectField(props: RefundSelectFieldProps) {
  return (
    <Field>
      <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
      <Select
        items={props.options}
        value={props.value}
        onValueChange={(value) => {
          if (value) props.onValueChange(value)
        }}
        disabled={props.disabled}
      >
        <SelectTrigger id={props.id} className='w-full'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {props.options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <FieldDescription>{props.description}</FieldDescription>
    </Field>
  )
}

type GrowthRefundCreateFieldsProps = {
  form: UseFormReturn<CreateRefundFormValues>
  intakeSource: RefundIntakeSource
  effectiveKind: RefundKind
  amountMode: PartialAmountMode
  submitting: boolean
}

export function GrowthRefundCreateFields(props: GrowthRefundCreateFieldsProps) {
  const { t } = useTranslation()
  return (
    <FieldGroup>
      <div className='grid gap-5 sm:grid-cols-2'>
        <Field
          data-invalid={Boolean(props.form.formState.errors.tradeNo)}
          aria-invalid={Boolean(props.form.formState.errors.tradeNo)}
        >
          <FieldLabel htmlFor='refund-case-trade-no'>
            {t('Local order number')}
          </FieldLabel>
          <Input
            id='refund-case-trade-no'
            maxLength={191}
            autoComplete='off'
            disabled={props.submitting}
            aria-invalid={Boolean(props.form.formState.errors.tradeNo)}
            {...props.form.register('tradeNo')}
          />
          <FieldDescription>
            {t('The payment order already stored in this service.')}
          </FieldDescription>
          <FieldError errors={[props.form.formState.errors.tradeNo]} />
        </Field>

        <RefundSelectField
          id='refund-case-source'
          label={t('Intake source')}
          description={t('How this verified refund reached operations.')}
          value={props.intakeSource}
          options={[
            {
              value: 'provider_refund',
              label: t('Payment provider refund'),
            },
            { value: 'offline_refund', label: t('Offline refund') },
            { value: 'chargeback', label: t('Chargeback') },
            {
              value: 'missed_callback',
              label: t('Missed provider callback'),
            },
          ]}
          disabled={props.submitting}
          onValueChange={(value) => {
            const source = value as RefundIntakeSource
            props.form.setValue('intakeSource', source, {
              shouldValidate: true,
            })
            if (source === 'chargeback') {
              props.form.setValue('kind', 'dispute', { shouldValidate: true })
            }
          }}
        />
      </div>

      <div className='grid gap-5 sm:grid-cols-2'>
        <RefundSelectField
          id='refund-case-kind'
          label={t('Refund kind')}
          description={
            props.intakeSource === 'chargeback'
              ? t('Chargebacks are always recorded as disputes.')
              : t('Choose how much of the original payment was reversed.')
          }
          value={props.effectiveKind}
          options={[
            { value: 'full_refund', label: t('Full refund') },
            { value: 'partial_refund', label: t('Partial refund') },
            { value: 'dispute', label: t('Dispute') },
          ]}
          disabled={props.submitting || props.intakeSource === 'chargeback'}
          onValueChange={(value) =>
            props.form.setValue('kind', value as RefundKind, {
              shouldValidate: true,
            })
          }
        />

        <Field
          data-invalid={Boolean(props.form.formState.errors.externalReference)}
          aria-invalid={Boolean(props.form.formState.errors.externalReference)}
        >
          <FieldLabel htmlFor='refund-case-external-reference'>
            {t('External reference')}
          </FieldLabel>
          <Input
            id='refund-case-external-reference'
            maxLength={191}
            autoComplete='off'
            disabled={props.submitting}
            aria-invalid={Boolean(
              props.form.formState.errors.externalReference
            )}
            {...props.form.register('externalReference')}
          />
          <FieldDescription>
            {t('Use the provider, bank, or support case reference.')}
          </FieldDescription>
          <FieldError
            errors={[props.form.formState.errors.externalReference]}
          />
        </Field>
      </div>

      <div className='grid gap-5 sm:grid-cols-2'>
        <Field
          data-invalid={Boolean(props.form.formState.errors.amount)}
          aria-invalid={Boolean(props.form.formState.errors.amount)}
        >
          <FieldLabel htmlFor='refund-case-amount'>
            {t('Refund amount')}
          </FieldLabel>
          <Input
            id='refund-case-amount'
            type='number'
            min='0'
            step='any'
            inputMode='decimal'
            disabled={props.submitting}
            aria-invalid={Boolean(props.form.formState.errors.amount)}
            {...props.form.register('amount')}
          />
          <FieldDescription>
            {props.effectiveKind === 'partial_refund'
              ? t('Enter the amount in normal currency units, not minor units.')
              : t(
                  'Leave blank to use the verified payment snapshot. Legacy orders without a snapshot require an amount.'
                )}
          </FieldDescription>
          <FieldError errors={[props.form.formState.errors.amount]} />
        </Field>

        <Field
          data-invalid={Boolean(props.form.formState.errors.currency)}
          aria-invalid={Boolean(props.form.formState.errors.currency)}
        >
          <FieldLabel htmlFor='refund-case-currency'>
            {t('Currency')}
          </FieldLabel>
          <Input
            id='refund-case-currency'
            maxLength={3}
            autoCapitalize='characters'
            autoComplete='off'
            placeholder='CNY'
            disabled={props.submitting}
            aria-invalid={Boolean(props.form.formState.errors.currency)}
            {...props.form.register('currency')}
          />
          <FieldDescription>
            {t('Must match the original payment currency.')}
          </FieldDescription>
          <FieldError errors={[props.form.formState.errors.currency]} />
        </Field>
      </div>

      {props.effectiveKind === 'partial_refund' ? (
        <RefundSelectField
          id='refund-case-amount-mode'
          label={t('Partial amount mode')}
          description={t(
            'Use cumulative only when the external reference reports the total refunded amount across multiple partial refunds.'
          )}
          value={props.amountMode}
          options={[
            { value: 'incremental', label: t('This refund only') },
            {
              value: 'cumulative',
              label: t('Cumulative refunded total'),
            },
          ]}
          disabled={props.submitting}
          onValueChange={(value) =>
            props.form.setValue('amountMode', value as PartialAmountMode, {
              shouldValidate: true,
            })
          }
        />
      ) : null}

      <Field
        data-invalid={Boolean(props.form.formState.errors.remark)}
        aria-invalid={Boolean(props.form.formState.errors.remark)}
      >
        <FieldLabel htmlFor='refund-case-reason'>
          {t('Evidence and reason')}
        </FieldLabel>
        <Textarea
          id='refund-case-reason'
          className='min-h-24'
          maxLength={1000}
          disabled={props.submitting}
          aria-invalid={Boolean(props.form.formState.errors.remark)}
          {...props.form.register('remark')}
        />
        <FieldDescription>
          {t(
            'Summarize the verified evidence. Do not paste secrets or full payment credentials.'
          )}
        </FieldDescription>
        <FieldError errors={[props.form.formState.errors.remark]} />
      </Field>
    </FieldGroup>
  )
}
