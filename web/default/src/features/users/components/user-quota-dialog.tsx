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
import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { createIdempotencyKey } from '@/lib/idempotency'

import { adjustUserQuota } from '../api'
import type { QuotaAdjustMode } from '../types'

const MIN_QUOTA = -2_147_483_648
const MAX_QUOTA_EXCLUSIVE = 2_147_483_647
const MAX_REMARK_LENGTH = 1000

interface UserQuotaDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  currentQuota: number
  onSuccess: () => void
}

type IdempotencyRequest = {
  signature: string
  key: string
}

export function UserQuotaDialog(props: UserQuotaDialogProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<QuotaAdjustMode>('add')
  const [amount, setAmount] = useState('')
  const [remark, setRemark] = useState('')
  const [loading, setLoading] = useState(false)
  const idempotencyRequestRef = useRef<IdempotencyRequest | null>(null)

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const normalizedAmount = amount.trim()
  const amountValue = Number(normalizedAmount)
  const hasFiniteAmount =
    normalizedAmount !== '' && Number.isFinite(amountValue)
  const quotaValue = hasFiniteAmount
    ? parseQuotaFromDollars(
        mode === 'override' ? amountValue : Math.abs(amountValue)
      )
    : 0
  let resultingQuota = props.currentQuota
  if (mode === 'add') resultingQuota += quotaValue
  if (mode === 'subtract') resultingQuota -= quotaValue
  if (mode === 'override') resultingQuota = quotaValue

  const amountHasValidSign =
    mode === 'override' ? amountValue >= 0 : amountValue > 0
  const quotaIsSupported =
    Number.isSafeInteger(quotaValue) &&
    quotaValue >= (mode === 'override' ? 0 : 1) &&
    quotaValue < MAX_QUOTA_EXCLUSIVE
  const resultingQuotaIsSupported =
    Number.isSafeInteger(resultingQuota) &&
    resultingQuota >= MIN_QUOTA &&
    resultingQuota < MAX_QUOTA_EXCLUSIVE &&
    resultingQuota !== props.currentQuota
  const amountIsValid =
    hasFiniteAmount &&
    amountHasValidSign &&
    (!tokensOnly || Number.isInteger(amountValue)) &&
    quotaIsSupported &&
    resultingQuotaIsSupported
  const normalizedRemark = remark.trim()
  const remarkIsValid =
    normalizedRemark !== '' && remark.length <= MAX_REMARK_LENGTH
  const formIsValid = amountIsValid && remarkIsValid

  let amountErrorKey = ''
  if (normalizedAmount !== '' && !amountIsValid) {
    if (tokensOnly && hasFiniteAmount && !Number.isInteger(amountValue)) {
      amountErrorKey = 'Amount must be a whole number'
    } else if (
      hasFiniteAmount &&
      amountHasValidSign &&
      resultingQuota === props.currentQuota
    ) {
      amountErrorKey = 'Enter an amount that changes the current quota.'
    } else if (hasFiniteAmount && amountHasValidSign) {
      amountErrorKey = 'The resulting quota is outside the supported range.'
    } else {
      amountErrorKey = 'Enter a valid amount.'
    }
  }

  let previewText = `${t('Current quota')}: ${formatQuota(props.currentQuota)}`
  if (hasFiniteAmount && Number.isSafeInteger(quotaValue)) {
    if (mode === 'add') {
      previewText += `  +${formatQuota(quotaValue)} = ${formatQuota(resultingQuota)}`
    } else if (mode === 'subtract') {
      previewText += `  -${formatQuota(quotaValue)} = ${formatQuota(resultingQuota)}`
    } else {
      previewText += ` → ${formatQuota(resultingQuota)}`
    }
  }

  const resetForm = () => {
    setAmount('')
    setRemark('')
    setMode('add')
    idempotencyRequestRef.current = null
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) resetForm()
    props.onOpenChange(open)
  }

  const handleConfirm = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!formIsValid || loading) return

    const signature = JSON.stringify([
      props.userId,
      mode,
      quotaValue,
      normalizedRemark,
    ])
    if (idempotencyRequestRef.current?.signature !== signature) {
      idempotencyRequestRef.current = {
        signature,
        key: createIdempotencyKey(`admin-quota-${props.userId}`),
      }
    }

    setLoading(true)
    try {
      const result = await adjustUserQuota({
        id: props.userId,
        action: 'add_quota',
        mode,
        value: quotaValue,
        remark: normalizedRemark,
        idempotency_key: idempotencyRequestRef.current.key,
      })
      if (result.success) {
        toast.success(t('Quota adjusted successfully'))
        handleOpenChange(false)
        props.onSuccess()
      } else {
        toast.error(result.message || t('Failed to adjust quota'))
      }
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to adjust quota')
      )
    } finally {
      setLoading(false)
    }
  }

  const placeholder = tokensOnly
    ? t('Enter amount in tokens')
    : t('Enter amount in {{currency}}', { currency: currencyLabel })

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Adjust Quota')}
      description={t('Select an operation mode and enter the amount')}
      contentHeight='auto'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
            disabled={loading}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='user-quota-adjustment-form'
            disabled={loading || !formIsValid}
          >
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <form id='user-quota-adjustment-form' onSubmit={handleConfirm} noValidate>
        <FieldGroup>
          <div className='text-muted-foreground text-sm'>{previewText}</div>

          <Field>
            <FieldTitle id='quota-adjustment-mode-label'>
              {t('Mode')}
            </FieldTitle>
            <ToggleGroup
              aria-labelledby='quota-adjustment-mode-label'
              value={[mode]}
              variant='outline'
              size='sm'
              spacing={1}
              onValueChange={(values) => {
                const nextMode = values.find((value) => value !== mode) as
                  | QuotaAdjustMode
                  | undefined
                if (!nextMode) return
                setMode(nextMode)
                setAmount('')
                idempotencyRequestRef.current = null
              }}
            >
              <ToggleGroupItem value='add' disabled={loading}>
                {t('Add')}
              </ToggleGroupItem>
              <ToggleGroupItem value='subtract' disabled={loading}>
                {t('Subtract')}
              </ToggleGroupItem>
              <ToggleGroupItem value='override' disabled={loading}>
                {t('Override')}
              </ToggleGroupItem>
            </ToggleGroup>
          </Field>

          <Field data-invalid={Boolean(amountErrorKey) || undefined}>
            <FieldLabel htmlFor='quota-adjustment-amount'>
              {t('Amount')} ({currencyLabel})
            </FieldLabel>
            <Input
              id='quota-adjustment-amount'
              type='number'
              step={tokensOnly ? 1 : 0.000001}
              min={0}
              placeholder={placeholder}
              value={amount}
              aria-invalid={Boolean(amountErrorKey) || undefined}
              disabled={loading}
              required
              onChange={(event) => {
                setAmount(event.target.value)
                idempotencyRequestRef.current = null
              }}
            />
            {amountErrorKey ? (
              <FieldError>{t(amountErrorKey)}</FieldError>
            ) : null}
          </Field>

          <Field data-invalid={(remark !== '' && !remarkIsValid) || undefined}>
            <div className='flex items-center gap-2'>
              <FieldLabel htmlFor='quota-adjustment-reason'>
                {t('Reason')}
              </FieldLabel>
              <span
                className='text-muted-foreground text-sm'
                aria-hidden='true'
              >
                {t('Required')}
              </span>
            </div>
            <Textarea
              id='quota-adjustment-reason'
              value={remark}
              maxLength={MAX_REMARK_LENGTH}
              className='min-h-24'
              aria-invalid={(remark !== '' && !remarkIsValid) || undefined}
              disabled={loading}
              required
              onChange={(event) => {
                setRemark(event.target.value)
                idempotencyRequestRef.current = null
              }}
            />
            <FieldDescription>
              {t('Explain the evidence and decision for the audit record.')}
            </FieldDescription>
            {remark !== '' && !remarkIsValid ? (
              <FieldError>{t('Reason is required.')}</FieldError>
            ) : null}
          </Field>
        </FieldGroup>
      </form>
    </Dialog>
  )
}
