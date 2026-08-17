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
import {
  AffiliateIcon,
  BankIcon,
  Cash01Icon,
  Task01Icon,
  UserMultipleIcon,
  Wallet01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { formatMinorAmount } from '@/lib/currency'
import { formatQuota } from '@/lib/format'

import type { PromotionEarnings } from '../api'

type EarningsBalancesProps = {
  earnings: PromotionEarnings
  onTransferReferral: () => void
  onConvertCash: () => void
  onWithdrawCash: () => void
}

export function EarningsBalances(props: EarningsBalancesProps) {
  const { t } = useTranslation()
  const earnings = props.earnings
  return (
    <div className='grid gap-4 lg:grid-cols-3'>
      <Card data-card-hover='false'>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <HugeiconsIcon
              icon={Task01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            {t('Task rewards')}
          </CardTitle>
          <CardDescription>
            {t('Approved task rewards go directly to your API balance.')}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Total earned')}
            </div>
            <div className='mt-1 text-2xl font-semibold tabular-nums'>
              {formatQuota(earnings.taskRewardsEarned)}
            </div>
          </div>
          <Badge variant='secondary'>
            {t('{{quota}} pending review', {
              quota: formatQuota(earnings.taskRewardsPending),
            })}
          </Badge>
        </CardContent>
      </Card>

      <Card data-card-hover='false'>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <HugeiconsIcon
              icon={AffiliateIcon}
              strokeWidth={2}
              aria-hidden='true'
            />
            {t('Referral credit')}
          </CardTitle>
          <CardDescription>
            {t(
              'Settled referral credit is available to move into your API balance.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Available now')}
            </div>
            <div className='mt-1 text-2xl font-semibold tabular-nums'>
              {formatQuota(earnings.transferableReferralCredit)}
            </div>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Badge variant='outline'>
              {t('{{quota}} earned in total', {
                quota: formatQuota(earnings.totalReferralCredit),
              })}
            </Badge>
            <Badge variant='outline' className='gap-1'>
              <HugeiconsIcon
                icon={UserMultipleIcon}
                strokeWidth={2}
                aria-hidden='true'
              />
              {t('{{count}} invited users', { count: earnings.invitedUsers })}
            </Badge>
          </div>
          <Button
            type='button'
            variant='outline'
            className='w-full'
            disabled={earnings.transferableReferralCredit <= 0}
            onClick={props.onTransferReferral}
          >
            <HugeiconsIcon
              icon={Wallet01Icon}
              strokeWidth={2}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Transfer all referral credit')}
          </Button>
        </CardContent>
      </Card>

      <Card data-card-hover='false'>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <HugeiconsIcon
              icon={Cash01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            {t('Cash commission')}
          </CardTitle>
          <CardDescription>
            {t('Settled cash can be converted to API balance or withdrawn.')}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Available now')}
            </div>
            <div className='mt-1 text-2xl font-semibold tabular-nums'>
              {formatMinorAmount(
                earnings.withdrawableCashCents,
                earnings.cashCurrency
              )}
            </div>
            <div className='text-muted-foreground mt-1 text-xs'>
              {t('Equivalent to {{quota}} of API balance', {
                quota: formatQuota(earnings.cashQuotaEquivalent),
              })}
            </div>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Badge variant='secondary'>
              {t('{{amount}} pending settlement', {
                amount: formatMinorAmount(
                  earnings.pendingCashCents,
                  earnings.cashCurrency
                ),
              })}
            </Badge>
            {earnings.withdrawingCashCents > 0 ? (
              <Badge variant='outline'>
                {t('{{amount}} under withdrawal review', {
                  amount: formatMinorAmount(
                    earnings.withdrawingCashCents,
                    earnings.cashCurrency
                  ),
                })}
              </Badge>
            ) : null}
          </div>
          <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2'>
            <Button
              type='button'
              variant='outline'
              disabled={earnings.withdrawableCashCents <= 0}
              onClick={props.onConvertCash}
            >
              <HugeiconsIcon
                icon={Wallet01Icon}
                strokeWidth={2}
                data-icon='inline-start'
                aria-hidden='true'
              />
              {t('Convert all to balance')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={earnings.withdrawableCashCents <= 0}
              onClick={props.onWithdrawCash}
            >
              <HugeiconsIcon
                icon={BankIcon}
                strokeWidth={2}
                data-icon='inline-start'
                aria-hidden='true'
              />
              {t('Withdraw all cash')}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
