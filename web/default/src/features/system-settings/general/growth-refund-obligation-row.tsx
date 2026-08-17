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
import { Wallet01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemMedia,
  ItemTitle,
} from '@/components/ui/item'
import { statusVariant } from '@/features/promotion/shared'

import {
  formatRecoveryAmount,
  getOutstandingAmount,
  type PromotionRefundObligation,
  type RefundRecoveryAction,
} from './growth-refund-types'

type GrowthRefundObligationRowProps = {
  obligation: PromotionRefundObligation
  busy: boolean
  canWaive: boolean
  onAction: (
    action: RefundRecoveryAction,
    obligation: PromotionRefundObligation
  ) => void
}

export function GrowthRefundObligationRow(
  props: GrowthRefundObligationRowProps
) {
  const { t } = useTranslation()
  const obligation = props.obligation
  const outstanding = getOutstandingAmount(obligation)
  const isOpen = obligation.status === 'open' && outstanding > 0
  const isPaidCommission =
    obligation.asset === 'cash' &&
    obligation.source_type === 'promotion_commission_ledgers'
  const amount = formatRecoveryAmount(
    obligation.amount,
    obligation.asset,
    obligation.currency
  )
  const recovered = formatRecoveryAmount(
    obligation.recovered_amount,
    obligation.asset,
    obligation.currency
  )
  const waived = formatRecoveryAmount(
    obligation.waived_amount,
    obligation.asset,
    obligation.currency
  )
  const remaining = formatRecoveryAmount(
    outstanding,
    obligation.asset,
    obligation.currency
  )
  let title = 'API balance recovery'
  if (obligation.asset === 'cash') {
    title = isPaidCommission ? 'Paid commission recovery' : 'Cash recovery'
  }

  let recoveryActions
  if (obligation.asset === 'quota') {
    recoveryActions = (
      <>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={props.busy}
          onClick={() => props.onAction('retry_wallet_debit', obligation)}
        >
          {t('Debit API balance')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={props.busy}
          onClick={() =>
            props.onAction('record_external_repayment', obligation)
          }
        >
          {t('Record repayment')}
        </Button>
      </>
    )
  } else if (isPaidCommission) {
    recoveryActions = (
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={props.busy}
        onClick={() => props.onAction('recover_paid_commission', obligation)}
      >
        {t('Record commission recovery')}
      </Button>
    )
  } else {
    recoveryActions = (
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={props.busy}
        onClick={() => props.onAction('record_external_repayment', obligation)}
      >
        {t('Record cash repayment')}
      </Button>
    )
  }

  return (
    <Item variant='outline'>
      <ItemMedia variant='icon'>
        <HugeiconsIcon icon={Wallet01Icon} strokeWidth={2} aria-hidden='true' />
      </ItemMedia>
      <ItemContent className='min-w-56'>
        <ItemTitle className='flex-wrap'>
          {t(title)}
          <Badge variant={statusVariant(obligation.status)}>
            {t(obligation.status)}
          </Badge>
        </ItemTitle>
        <ItemDescription className='line-clamp-none'>
          {t('Original debt')}: {amount} · {t('Recovered')}: {recovered} ·{' '}
          {t('Waived')}: {waived} · {t('Outstanding')}: {remaining}
        </ItemDescription>
      </ItemContent>
      {isOpen ? (
        <ItemActions className='ms-auto flex-wrap justify-end'>
          {recoveryActions}
          {props.canWaive ? (
            <Button
              type='button'
              size='sm'
              variant='destructive'
              disabled={props.busy}
              onClick={() => props.onAction('waive', obligation)}
            >
              {t('Waive debt')}
            </Button>
          ) : null}
        </ItemActions>
      ) : null}
    </Item>
  )
}
