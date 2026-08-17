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
import { AlertCircleIcon, ShieldUserIcon } from '@hugeicons/core-free-icons'
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
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToEditableAmount,
} from '@/lib/format'

import {
  formatMinorAmount,
  getMinorAmountFactor,
  getOutstandingAmount,
  parseMinorAmount,
  type RefundActionIntent,
} from './growth-refund-types'

const actionFormSchema = z.object({
  amount: z.string().max(64),
  externalRef: z.string().max(191),
  remark: z.string().max(1000),
  userSubscriptionId: z.string().max(20),
})

type ActionFormValues = z.infer<typeof actionFormSchema>

export type RefundActionRequest = {
  idempotency_key: string
  expected_responsibility_fingerprint?: string
  action: RefundActionIntent['action']
  obligation_id: number
  user_id?: number
  top_up_id?: number
  asset?: 'quota' | 'cash'
  currency?: string
  amount: number
  external_ref: string
  remark: string
  commission_ledger_id?: number
  commission_ledger_status?: string
  user_subscription_id?: number
}

type GrowthRefundActionDialogProps = {
  intent: RefundActionIntent
  submitting: boolean
  onCancel: () => void
  onSubmit: (request: RefundActionRequest) => Promise<void>
}

function getDialogCopy(intent: RefundActionIntent) {
  const caseLevelRootReview = intent.action === 'waive' && !intent.obligation
  if (caseLevelRootReview) {
    return {
      title: 'Complete Root review',
      description:
        'Confirm that every responsible party has been assessed. This closes Root review but keeps every obligation and account hold in place.',
      submit: 'Complete Root review',
    }
  }
  switch (intent.action) {
    case 'retry_wallet_debit':
      return {
        title: 'Debit API balance',
        description:
          "Deduct the selected amount from the user's available API balance and reduce the matching refund debt in one transaction.",
        submit: 'Debit API balance',
      }
    case 'record_external_repayment':
      return {
        title: 'Record external repayment',
        description:
          'Record funds collected outside this system and reduce the matching refund debt. Keep the external reference for reconciliation.',
        submit: 'Record repayment',
      }
    case 'recover_paid_commission':
      return {
        title: 'Record commission recovery',
        description:
          'Record the return of cash commission that had already been paid. Keep the external reference for reconciliation.',
        submit: 'Record commission recovery',
      }
    case 'define_manual_obligation':
      return {
        title: 'Create recovery obligation',
        description:
          'Create a quantified debt from verified evidence and keep the account on hold until it is recovered or waived.',
        submit: 'Create recovery obligation',
      }
    case 'quarantine_unknown_commission':
      return {
        title: 'Quarantine unknown commission',
        description:
          'Record the external evidence that explains why this exact ledger status cannot be reconstructed. The ledger stays unchanged and reconciliation may continue past it.',
        submit: 'Quarantine unknown commission',
      }
    case 'revoke_subscription_entitlement':
      if (intent.refundCase.subscription_status !== 'active') {
        return {
          title: 'Record subscription disposition',
          description:
            "Record the already-ended subscription entitlement in this refund case's immutable history.",
          submit: 'Record subscription disposition',
        }
      }
      return {
        title: 'Terminate subscription entitlement',
        description:
          'End the subscription linked to this refunded payment and record the disposition in the immutable case history.',
        submit: 'Terminate subscription',
      }
    case 'waive':
      return {
        title: 'Waive outstanding debt',
        description:
          'Close the selected amount without collecting it. This Root-only decision is permanent and remains in the action history.',
        submit: 'Waive debt',
      }
    case 'release_hold':
      return {
        title: 'Release account hold',
        description:
          'Run the final recovery checks, release the refund hold, and resolve this case. The server will reject the action if this user still has debt, open obligations, or another pending dispute.',
        submit: 'Release account hold',
      }
  }
}

function initialAmount(intent: RefundActionIntent) {
  if (!intent.obligation) return ''
  const outstanding = getOutstandingAmount(intent.obligation)
  if (intent.obligation.asset === 'cash') {
    const factor = getMinorAmountFactor(intent.obligation.currency || 'CNY')
    return String(outstanding / factor)
  }
  return String(quotaUnitsToEditableAmount(outstanding))
}

export function GrowthRefundActionDialog(props: GrowthRefundActionDialogProps) {
  const { t } = useTranslation()
  const copy = getDialogCopy(props.intent)
  const obligation = props.intent.obligation
  const outstanding = obligation ? getOutstandingAmount(obligation) : 0
  const requiresAmount = Boolean(obligation)
  const requiresExternalReference =
    props.intent.action === 'record_external_repayment' ||
    props.intent.action === 'recover_paid_commission' ||
    props.intent.action === 'quarantine_unknown_commission'
  const requiresRemark =
    props.intent.action === 'waive' ||
    props.intent.action === 'release_hold' ||
    props.intent.action === 'quarantine_unknown_commission' ||
    props.intent.action === 'revoke_subscription_entitlement'
  const terminatingSubscription =
    props.intent.action === 'revoke_subscription_entitlement'
  const endingActiveSubscription =
    terminatingSubscription &&
    props.intent.refundCase.subscription_status === 'active'
  const destructive =
    endingActiveSubscription ||
    (props.intent.action === 'waive' && Boolean(obligation))
  const form = useForm<ActionFormValues>({
    resolver: zodResolver(actionFormSchema),
    defaultValues: {
      amount: initialAmount(props.intent),
      externalRef: '',
      remark: '',
      userSubscriptionId: props.intent.refundCase.user_subscription_id
        ? String(props.intent.refundCase.user_subscription_id)
        : '',
    },
  })

  const submit = form.handleSubmit(async (values) => {
    let amount = 0
    if (requiresAmount) {
      if (obligation?.asset === 'cash') {
        amount =
          parseMinorAmount(values.amount, obligation.currency || 'CNY') ?? 0
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
      if (amount > outstanding) {
        form.setError('amount', {
          message: t('Recovery amount cannot exceed the outstanding debt.'),
        })
        return
      }
    }

    const externalRef = values.externalRef.trim()
    if (requiresExternalReference && !externalRef) {
      form.setError('externalRef', {
        message: t('External reference is required.'),
      })
      return
    }
    const remark = values.remark.trim()
    if (requiresRemark && !remark) {
      form.setError('remark', { message: t('A reason is required.') })
      return
    }

    let userSubscriptionId = 0
    if (terminatingSubscription) {
      userSubscriptionId = Number(values.userSubscriptionId)
      if (
        !Number.isSafeInteger(userSubscriptionId) ||
        userSubscriptionId <= 0
      ) {
        form.setError('userSubscriptionId', {
          message: t('Enter a valid subscription ID.'),
        })
        return
      }
    }

    const request: RefundActionRequest = {
      idempotency_key: props.intent.idempotencyKey,
      expected_responsibility_fingerprint:
        props.intent.refundCase.responsibility_fingerprint,
      action: props.intent.action,
      obligation_id: obligation?.id || 0,
      amount,
      external_ref: externalRef,
      remark,
    }
    if (terminatingSubscription) {
      request.user_subscription_id = userSubscriptionId
    }
    if (props.intent.action === 'quarantine_unknown_commission') {
      request.commission_ledger_id =
        props.intent.refundCase.commission_ledger_id || 0
      request.commission_ledger_status =
        props.intent.refundCase.commission_ledger_status ?? ''
    }
    await props.onSubmit(request)
  })

  let outstandingLabel = ''
  if (obligation?.asset === 'cash') {
    outstandingLabel = formatMinorAmount(
      outstanding,
      obligation.currency || 'CNY'
    )
  } else if (obligation?.asset === 'quota') {
    outstandingLabel = formatQuota(outstanding)
  }
  const amountUnit =
    obligation?.asset === 'cash'
      ? obligation.currency || 'CNY'
      : getCurrencyLabel()
  const amountStep =
    obligation?.asset === 'cash'
      ? String(1 / getMinorAmountFactor(obligation.currency || 'CNY'))
      : 'any'
  let consistencyNotice = (
    <AlertDescription>
      {t(
        'Balance changes, debt changes, the immutable fund journal, and this action record are committed together.'
      )}
    </AlertDescription>
  )
  if (terminatingSubscription) {
    consistencyNotice = (
      <>
        <AlertTitle>{t('Entitlement recovery')}</AlertTitle>
        <AlertDescription>
          {t(
            'The entitlement change and immutable recovery action are committed together. A failed action leaves the subscription unchanged.'
          )}
        </AlertDescription>
      </>
    )
  }
  if (props.intent.action === 'quarantine_unknown_commission') {
    consistencyNotice = (
      <>
        <AlertTitle>{t('Reconciliation exception')}</AlertTitle>
        <AlertDescription>
          {t(
            'The ledger status stays unchanged. This immutable exception lets reconciliation skip only this exact ledger and status.'
          )}
        </AlertDescription>
      </>
    )
  }

  return (
    <Dialog open onOpenChange={(open) => !open && props.onCancel()}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t(copy.title)}</DialogTitle>
          <DialogDescription>{t(copy.description)}</DialogDescription>
        </DialogHeader>

        {destructive ? (
          <Alert variant='destructive'>
            <HugeiconsIcon
              icon={ShieldUserIcon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <AlertTitle>
              {endingActiveSubscription
                ? t('Subscription ends immediately')
                : t('Permanent Root decision')}
            </AlertTitle>
            <AlertDescription>
              {endingActiveSubscription
                ? t(
                    'Unused subscription access is removed. If this subscription upgraded the account group, it is reverted when no other active upgraded subscription remains.'
                  )
                : t(
                    'The waived amount will no longer block account recovery. The action cannot be edited or deleted.'
                  )}
            </AlertDescription>
          </Alert>
        ) : null}

        <form id='refund-recovery-action-form' onSubmit={submit} noValidate>
          <FieldGroup>
            {requiresAmount ? (
              <Field
                data-invalid={Boolean(form.formState.errors.amount)}
                aria-invalid={Boolean(form.formState.errors.amount)}
              >
                <FieldLabel htmlFor='refund-recovery-amount'>
                  {t('Recovery amount ({{unit}})', { unit: amountUnit })}
                </FieldLabel>
                <Input
                  id='refund-recovery-amount'
                  type='number'
                  min='0'
                  step={amountStep}
                  inputMode='decimal'
                  aria-invalid={Boolean(form.formState.errors.amount)}
                  disabled={props.submitting}
                  {...form.register('amount')}
                />
                <FieldDescription>
                  {t('Outstanding debt')}: {outstandingLabel}
                </FieldDescription>
                <FieldError errors={[form.formState.errors.amount]} />
              </Field>
            ) : null}

            {requiresExternalReference ? (
              <Field
                data-invalid={Boolean(form.formState.errors.externalRef)}
                aria-invalid={Boolean(form.formState.errors.externalRef)}
              >
                <FieldLabel htmlFor='refund-recovery-reference'>
                  {t('External reference')}
                </FieldLabel>
                <Input
                  id='refund-recovery-reference'
                  maxLength={191}
                  autoComplete='off'
                  aria-invalid={Boolean(form.formState.errors.externalRef)}
                  disabled={props.submitting}
                  {...form.register('externalRef')}
                />
                <FieldDescription>
                  {props.intent.action === 'quarantine_unknown_commission'
                    ? t(
                        'Use the provider audit, support ticket, or internal incident reference that supports this exception.'
                      )
                    : t(
                        'Use the bank transfer, payment, or collection reference that proves the recovery.'
                      )}
                </FieldDescription>
                <FieldError errors={[form.formState.errors.externalRef]} />
              </Field>
            ) : null}

            {terminatingSubscription ? (
              <Field
                data-invalid={Boolean(form.formState.errors.userSubscriptionId)}
                aria-invalid={Boolean(form.formState.errors.userSubscriptionId)}
              >
                <FieldLabel htmlFor='refund-subscription-id'>
                  {t('Subscription ID')}
                </FieldLabel>
                <Input
                  id='refund-subscription-id'
                  type='number'
                  min='1'
                  step='1'
                  inputMode='numeric'
                  aria-invalid={Boolean(
                    form.formState.errors.userSubscriptionId
                  )}
                  disabled={
                    props.submitting ||
                    Boolean(props.intent.refundCase.user_subscription_id)
                  }
                  {...form.register('userSubscriptionId')}
                />
                <FieldDescription>
                  {props.intent.refundCase.user_subscription_id
                    ? t(
                        'This entitlement is durably linked to the payment order.'
                      )
                    : t(
                        'For a legacy order, enter the entitlement verified against the same user and plan.'
                      )}
                </FieldDescription>
                <FieldError
                  errors={[form.formState.errors.userSubscriptionId]}
                />
              </Field>
            ) : null}

            <Field
              data-invalid={Boolean(form.formState.errors.remark)}
              aria-invalid={Boolean(form.formState.errors.remark)}
            >
              <FieldLabel htmlFor='refund-recovery-remark'>
                {requiresRemark ? t('Reason') : t('Remark')}
              </FieldLabel>
              <Textarea
                id='refund-recovery-remark'
                maxLength={1000}
                className='min-h-24'
                aria-invalid={Boolean(form.formState.errors.remark)}
                disabled={props.submitting}
                {...form.register('remark')}
              />
              <FieldDescription>
                {requiresRemark
                  ? t('Explain the evidence and decision for the audit record.')
                  : t('Optional context for the audit record.')}
              </FieldDescription>
              <FieldError errors={[form.formState.errors.remark]} />
            </Field>
          </FieldGroup>
        </form>

        <Alert>
          <HugeiconsIcon
            icon={AlertCircleIcon}
            strokeWidth={2}
            aria-hidden='true'
          />
          {consistencyNotice}
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
            form='refund-recovery-action-form'
            variant={destructive ? 'destructive' : 'default'}
            disabled={props.submitting}
          >
            {props.submitting ? t('Processing...') : t(copy.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
