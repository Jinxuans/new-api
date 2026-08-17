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

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { formatMinorAmount } from '@/lib/currency'

import type { WithdrawalConfirmation } from './growth-withdrawal-review-types'
import {
  getWithdrawalExternalReference,
  getWithdrawalPayoutAccount,
} from './growth-withdrawal-review-utils'

type GrowthWithdrawalConfirmationDialogProps = {
  confirmation: WithdrawalConfirmation | null
  tradeNo: string
  note: string
  busy: boolean
  onCancel: () => void
  onConfirm: () => void
}

export function GrowthWithdrawalConfirmationDialog(
  props: GrowthWithdrawalConfirmationDialogProps
) {
  const { t } = useTranslation()
  const withdrawal = props.confirmation?.withdrawal
  const amount = formatMinorAmount(
    withdrawal?.net_amount_cents || 0,
    withdrawal?.currency || 'CNY'
  )
  const payoutAccount = withdrawal
    ? getWithdrawalPayoutAccount(withdrawal)
    : '-'
  const existingReference = withdrawal
    ? getWithdrawalExternalReference(withdrawal)
    : '-'
  const externalReference =
    props.confirmation?.action === 'initiate'
      ? props.tradeNo || existingReference
      : existingReference
  let title = t('Approve this withdrawal request?')
  let actionLabel = t('Approve request')
  let impact = t(
    'This approves a withdrawal of {{amount}} for offline payment. Verify the payout account before continuing.',
    { amount }
  )

  if (props.confirmation?.action === 'initiate') {
    title = t('Start this payout?')
    actionLabel = t('Start payout')
    impact = t(
      'This records payout reference {{reference}} and moves {{amount}} into processing. Processing is not treated as paid; finish by confirming completion or recording failure.',
      { reference: props.tradeNo, amount }
    )
  } else if (props.confirmation?.action === 'paid') {
    title = t('Confirm this payout completed?')
    actionLabel = t('Confirm payout completed')
    impact = t(
      'This confirms the offline payment of {{amount}} and closes the linked commission entries. Use this only after the payment provider confirms completion.',
      { amount }
    )
  } else if (props.confirmation?.action === 'failed') {
    title = t('Mark this payout failed?')
    actionLabel = t('Mark payout failed')
    impact = t(
      'This records that payout reference {{reference}} failed and returns {{amount}} from reserved commission to available commission. The withdrawal cannot later be marked paid.',
      { reference: existingReference, amount }
    )
  } else if (props.confirmation?.action === 'reject') {
    title = t('Reject request')
    actionLabel = t('Reject request')
    impact = t(
      'This rejects the withdrawal and returns {{amount}} from reserved commission to available commission. The withdrawal cannot later be paid.',
      { amount }
    )
  }

  return (
    <AlertDialog
      open={props.confirmation !== null}
      onOpenChange={(open) => !open && props.onCancel()}
    >
      <AlertDialogContent className='sm:max-w-md'>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>
            <span className='flex w-full flex-col gap-2 text-left'>
              <span className='grid gap-1 rounded-lg border p-3'>
                <span>
                  {t('Withdrawal')}: #{withdrawal?.id || '-'}
                </span>
                <span>
                  {t('User ID')}: {withdrawal?.user_id || '-'}
                </span>
                <span>
                  {t('Amount')}: {amount}
                </span>
                <span>
                  {t('Payout method')}: {withdrawal?.payout_method || '-'}
                </span>
                <span className='break-all'>
                  {t('Payout account')}: {payoutAccount}
                </span>
                <span className='break-all'>
                  {t('Trade no')}: {externalReference}
                </span>
                {props.confirmation?.action === 'reject' ||
                props.confirmation?.action === 'failed' ? (
                  <span className='break-words'>
                    {props.confirmation?.action === 'failed'
                      ? t('Failure reason')
                      : t('Review note')}
                    : {props.note}
                  </span>
                ) : null}
              </span>
              <span>{impact}</span>
            </span>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.busy}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            variant={
              props.confirmation?.action === 'reject' ||
              props.confirmation?.action === 'failed'
                ? 'destructive'
                : 'default'
            }
            aria-label={`${actionLabel} #${withdrawal?.id || '-'}`}
            onClick={props.onConfirm}
            disabled={
              props.busy ||
              (props.confirmation?.action === 'failed' && !props.note)
            }
          >
            {actionLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
