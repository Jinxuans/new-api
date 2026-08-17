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
import { useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { api } from '@/lib/api'
import { createIdempotencyKey } from '@/lib/idempotency'

import { GrowthRefundCreateFields } from './growth-refund-create-fields'
import {
  createRefundDefaultValues,
  type CreateRefundFormValues,
  type RefundIntakeSource,
  type RefundKind,
} from './growth-refund-create-types'
import { parseMinorAmount } from './growth-refund-types'

type GrowthRefundCreateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => Promise<void>
}

type CreateRefundRequest = {
  idempotency_key: string
  trade_no: string
  external_ref: string
  intake_source: RefundIntakeSource
  kind: RefundKind
  refunded_amount_minor: number
  currency: string
  amount_is_cumulative: boolean
  remark: string
}

export function GrowthRefundCreateDialog(props: GrowthRefundCreateDialogProps) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)
  const requestRef = useRef<{ signature: string; key: string } | null>(null)
  const formSchema = useMemo(
    () =>
      z.object({
        tradeNo: z
          .string()
          .trim()
          .min(1, t('Local order number is required.'))
          .max(191, t('Enter a valid local order number.')),
        intakeSource: z.enum([
          'offline_refund',
          'provider_refund',
          'chargeback',
          'missed_callback',
        ]),
        kind: z.enum(['full_refund', 'partial_refund', 'dispute']),
        externalReference: z
          .string()
          .trim()
          .min(1, t('External reference is required.'))
          .max(191, t('Enter a valid external reference.')),
        amount: z.string().max(64, t('Enter a valid refund amount.')),
        currency: z
          .string()
          .trim()
          .regex(/^[A-Za-z]{3}$/, t('Use a three-letter currency code.')),
        amountMode: z.enum(['incremental', 'cumulative']),
        remark: z
          .string()
          .trim()
          .min(1, t('A reason is required.'))
          .max(1000, t('Enter a shorter reason.')),
      }),
    [t]
  )
  const form = useForm<CreateRefundFormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: createRefundDefaultValues,
  })
  const intakeSource = useWatch({
    control: form.control,
    name: 'intakeSource',
  })
  const selectedKind = useWatch({ control: form.control, name: 'kind' })
  const amountMode = useWatch({ control: form.control, name: 'amountMode' })
  const effectiveKind = intakeSource === 'chargeback' ? 'dispute' : selectedKind

  const closeDialog = () => {
    if (submitting) return
    requestRef.current = null
    form.reset(createRefundDefaultValues)
    props.onOpenChange(false)
  }

  const submit = form.handleSubmit(async (values) => {
    const currency = values.currency.trim().toUpperCase()
    const amountText = values.amount.trim()
    let refundedAmountMinor = 0
    if (amountText) {
      refundedAmountMinor = parseMinorAmount(amountText, currency) ?? 0
      if (
        !Number.isSafeInteger(refundedAmountMinor) ||
        refundedAmountMinor <= 0
      ) {
        form.setError('amount', {
          message: t('Enter a positive refund amount.'),
        })
        return
      }
    } else if (effectiveKind === 'partial_refund') {
      form.setError('amount', {
        message: t('Enter a positive refund amount.'),
      })
      return
    }

    const economicPayload = {
      trade_no: values.tradeNo.trim(),
      external_ref: values.externalReference.trim(),
      intake_source: intakeSource,
      kind: effectiveKind,
      refunded_amount_minor: refundedAmountMinor,
      currency,
      amount_is_cumulative:
        effectiveKind === 'partial_refund' &&
        values.amountMode === 'cumulative',
      remark: values.remark.trim(),
    }
    const signature = JSON.stringify(economicPayload)
    if (requestRef.current?.signature !== signature) {
      requestRef.current = {
        signature,
        key: createIdempotencyKey('refund-case'),
      }
    }
    const request: CreateRefundRequest = {
      idempotency_key: requestRef.current.key,
      ...economicPayload,
    }

    try {
      setSubmitting(true)
      const response = await api.post('/api/growth/admin/refund-cases', request)
      if (!response.data?.success) {
        throw new Error(
          response.data?.message || t('Failed to create refund case')
        )
      }
      toast.success(t('Refund case created'))
      requestRef.current = null
      form.reset(createRefundDefaultValues)
      props.onOpenChange(false)
      await props.onCreated()
    } catch (error) {
      const responseMessage = (
        error as { response?: { data?: { message?: string } } }
      ).response?.data?.message
      toast.error(
        responseMessage ||
          (error instanceof Error && error.message
            ? error.message
            : t('Failed to create refund case'))
      )
    } finally {
      setSubmitting(false)
    }
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) closeDialog()
      }}
    >
      <DialogContent
        className='max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl'
        showCloseButton={!submitting}
      >
        <DialogHeader>
          <DialogTitle>{t('Create refund case')}</DialogTitle>
          <DialogDescription>
            {t(
              'Record a verified refund, chargeback, or missed callback against an existing local payment order.'
            )}
          </DialogDescription>
        </DialogHeader>

        <Alert>
          <HugeiconsIcon
            icon={ShieldUserIcon}
            strokeWidth={2}
            aria-hidden='true'
          />
          <AlertTitle>{t('Uses the existing recovery chain')}</AlertTitle>
          <AlertDescription>
            {t(
              'The server verifies the local order and payment snapshot, then applies the same balance debit, debt, account hold, idempotency, and immutable fund records as a provider webhook.'
            )}
          </AlertDescription>
        </Alert>

        <form id='create-refund-case-form' onSubmit={submit} noValidate>
          <GrowthRefundCreateFields
            form={form}
            intakeSource={intakeSource}
            effectiveKind={effectiveKind}
            amountMode={amountMode}
            submitting={submitting}
          />
        </form>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={submitting}
            onClick={closeDialog}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='create-refund-case-form'
            disabled={submitting}
          >
            {submitting ? t('Creating...') : t('Create refund case')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
