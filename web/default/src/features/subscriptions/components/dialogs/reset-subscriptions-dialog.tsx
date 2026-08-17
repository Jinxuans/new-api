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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { createIdempotencyKey } from '@/lib/idempotency'

import { resetPlanSubscriptions } from '../../api'
import { useSubscriptions } from '../subscriptions-provider'

export function ResetSubscriptionsDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useSubscriptions()
  const [advanceResetTime, setAdvanceResetTime] = useState(true)
  const [reason, setReason] = useState('')
  const [resetting, setResetting] = useState(false)
  const idempotencyRequestRef = useRef<{
    signature: string
    key: string
  } | null>(null)
  const isOpen = open === 'reset-subscriptions'
  const plan = currentRow?.plan
  const planLabel = plan?.title || (plan?.id ? `#${plan.id}` : '-')

  useEffect(() => {
    if (isOpen) {
      setAdvanceResetTime(true)
      setReason('')
      idempotencyRequestRef.current = null
    }
  }, [isOpen])

  const handleConfirm = async () => {
    const normalizedReason = reason.trim()
    if (!plan?.id || !normalizedReason) return
    const signature = JSON.stringify([
      plan.id,
      advanceResetTime,
      normalizedReason,
    ])
    if (idempotencyRequestRef.current?.signature !== signature) {
      idempotencyRequestRef.current = {
        signature,
        key: createIdempotencyKey('subscription-plan-reset'),
      }
    }
    setResetting(true)
    try {
      const res = await resetPlanSubscriptions(plan.id, {
        advance_reset_time: advanceResetTime,
        reason: normalizedReason,
        idempotency_key: idempotencyRequestRef.current.key,
      })
      if (res.success) {
        toast.success(
          t('Reset {{count}} active subscriptions', {
            count: res.data?.reset_count || 0,
          })
        )
        triggerRefresh()
        setOpen(null)
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setResetting(false)
    }
  }

  return (
    <ConfirmDialog
      open={isOpen}
      onOpenChange={(nextOpen) => !nextOpen && setOpen(null)}
      title={t('Reset subscription quota')}
      desc={t('Reset all active subscriptions under {{plan}}?', {
        plan: planLabel,
      })}
      confirmText={t('Reset quota')}
      handleConfirm={handleConfirm}
      disabled={!plan?.id || !reason.trim()}
      isLoading={resetting}
    >
      <div className='grid gap-3'>
        <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'>
          <span>{t('Advance next reset time')}</span>
          <Switch
            checked={advanceResetTime}
            onCheckedChange={(checked) => setAdvanceResetTime(!!checked)}
            aria-label={t('Advance next reset time')}
          />
        </label>
        <label className='grid gap-2 text-sm'>
          <span className='font-medium'>{t('Reason')}</span>
          <Textarea
            value={reason}
            maxLength={1000}
            required
            disabled={resetting}
            placeholder={t('Explain why this quota reset is necessary.')}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
      </div>
    </ConfirmDialog>
  )
}
