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
  Activity01Icon,
  AlertCircleIcon,
  CheckmarkCircle02Icon,
  ShieldUserIcon,
  Wallet01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ItemGroup } from '@/components/ui/item'
import { Separator } from '@/components/ui/separator'
import { formatTime } from '@/features/promotion/shared'
import { formatQuota } from '@/lib/format'

import { GrowthRefundObligationRow } from './growth-refund-obligation-row'
import {
  formatMinorAmount,
  formatRecoveryAmount,
  getOutstandingAmount,
  refundActionLabel,
  refundKindLabel,
  type PromotionRefundCase,
  type PromotionRefundObligation,
  type RefundRecoveryAction,
} from './growth-refund-types'

type GrowthRefundCaseCardProps = {
  refundCase: PromotionRefundCase
  busy: boolean
  canUseRootActions: boolean
  onAction: (
    refundCase: PromotionRefundCase,
    action: RefundRecoveryAction,
    obligation?: PromotionRefundObligation
  ) => void
}

function FinancialValue(props: { label: string; value: string }) {
  return (
    <div className='flex min-w-0 flex-col gap-1'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd className='text-sm font-medium tabular-nums'>{props.value}</dd>
    </div>
  )
}

function subscriptionStatusLabel(
  status: PromotionRefundCase['subscription_status']
) {
  switch (status) {
    case 'active':
      return 'Active'
    case 'cancelled':
      return 'Cancelled'
    case 'expired':
      return 'Expired'
    default:
      return 'Entitlement not linked'
  }
}

export function GrowthRefundCaseCard(props: GrowthRefundCaseCardProps) {
  const { t } = useTranslation()
  const refundCase = props.refundCase
  const pending = refundCase.status === 'pending_review'
  const obligations = refundCase.obligations || []
  const actions = refundCase.actions || []
  let intakeSource = t('Provider webhook')
  if (refundCase.intake_source === 'offline_refund') {
    intakeSource = t('Offline refund')
  } else if (refundCase.intake_source === 'provider_refund') {
    intakeSource = t('Payment provider refund')
  } else if (refundCase.intake_source === 'chargeback') {
    intakeSource = t('Chargeback')
  } else if (refundCase.intake_source === 'missed_callback') {
    intakeSource = t('Missed provider callback')
  }
  const allObligationsClosed = obligations.every(
    (obligation) =>
      obligation.status !== 'open' && getOutstandingAmount(obligation) === 0
  )
  const needsCommissionQuarantine =
    refundCase.commission_reconciliation_required
  const isSubscriptionRefund = Boolean(refundCase.subscription_order_id)
  const subscriptionDispositionRecorded = actions.some(
    (action) =>
      action.action === 'revoke_subscription_entitlement' &&
      action.user_subscription_id === refundCase.user_subscription_id
  )
  const principalCashObligations = obligations.filter(
    (obligation) =>
      obligation.asset === 'cash' &&
      obligation.source_type === 'top_ups' &&
      obligation.source_id === refundCase.top_up_id
  )
  const activeSubscriptionCashRecovered =
    principalCashObligations.length > 0 &&
    principalCashObligations.every(
      (obligation) =>
        obligation.status === 'recovered' &&
        obligation.recovered_amount === obligation.amount &&
        obligation.waived_amount === 0
    )
  const subscriptionDispositionReady =
    !isSubscriptionRefund ||
    (refundCase.subscription_status === 'active'
      ? activeSubscriptionCashRecovered
      : Boolean(refundCase.subscription_status) &&
        subscriptionDispositionRecorded)
  let subscriptionBadgeVariant: 'default' | 'destructive' | 'secondary' =
    'secondary'
  if (refundCase.subscription_status === 'active') {
    subscriptionBadgeVariant = 'destructive'
  } else if (subscriptionDispositionReady) {
    subscriptionBadgeVariant = 'default'
  }
  let subscriptionDescription =
    'Record the entitlement disposition before completing Root review.'
  if (refundCase.subscription_status === 'active') {
    subscriptionDescription = activeSubscriptionCashRecovered
      ? 'The subscription stays active because the assessed cash obligation was fully recovered.'
      : 'An active refunded subscription must be terminated or backed by a fully recovered cash obligation.'
  } else if (subscriptionDispositionRecorded) {
    subscriptionDescription =
      'The entitlement disposition is linked to this refund in the immutable action history.'
  }
  const subscriptionActionLabel =
    refundCase.subscription_status === 'active'
      ? 'Terminate subscription'
      : 'Record subscription disposition'
  const canRelease =
    pending &&
    !refundCase.requires_root_review &&
    !needsCommissionQuarantine &&
    subscriptionDispositionReady &&
    allObligationsClosed
  let nextStepTitle = 'Recover every outstanding obligation first'
  let nextStepDescription =
    'The account hold remains active while any obligation has an outstanding amount.'
  if (needsCommissionQuarantine) {
    nextStepTitle = 'Quarantine unknown commission first'
    nextStepDescription =
      'Record external evidence for the linked ledger status. This does not change the ledger or create a fund transaction.'
  } else if (!subscriptionDispositionReady) {
    nextStepTitle = 'Resolve the subscription entitlement first'
    nextStepDescription =
      'Terminate the linked entitlement, or fully recover the assessed cash obligation before keeping an active subscription.'
  } else if (refundCase.requires_root_review) {
    nextStepTitle = 'Complete Root review next'
    nextStepDescription =
      'Create all recovery obligations supported by verified evidence, then complete Root review. The account hold remains active.'
  } else if (allObligationsClosed) {
    nextStepTitle = 'Recovery is ready to close'
    nextStepDescription =
      'Releasing the hold performs the final server-side checks and resolves this case.'
  }

  let nextStepAction: ReactNode = null
  if (needsCommissionQuarantine) {
    if (props.canUseRootActions) {
      nextStepAction = (
        <Button
          type='button'
          disabled={props.busy}
          onClick={() =>
            props.onAction(refundCase, 'quarantine_unknown_commission')
          }
        >
          <HugeiconsIcon
            icon={AlertCircleIcon}
            strokeWidth={2}
            data-icon='inline-start'
            aria-hidden='true'
          />
          {t('Quarantine unknown commission')}
        </Button>
      )
    }
  } else if (!subscriptionDispositionReady) {
    if (props.canUseRootActions) {
      nextStepAction = (
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='destructive'
            disabled={props.busy}
            onClick={() =>
              props.onAction(refundCase, 'revoke_subscription_entitlement')
            }
          >
            <HugeiconsIcon
              icon={ShieldUserIcon}
              strokeWidth={2}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t(subscriptionActionLabel)}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={props.busy}
            onClick={() =>
              props.onAction(refundCase, 'define_manual_obligation')
            }
          >
            <HugeiconsIcon
              icon={Wallet01Icon}
              strokeWidth={2}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Create cash recovery obligation')}
          </Button>
        </div>
      )
    }
  } else if (refundCase.requires_root_review) {
    if (props.canUseRootActions) {
      nextStepAction = (
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            disabled={props.busy}
            onClick={() =>
              props.onAction(refundCase, 'define_manual_obligation')
            }
          >
            <HugeiconsIcon
              icon={Wallet01Icon}
              strokeWidth={2}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Create recovery obligation')}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={props.busy}
            onClick={() => props.onAction(refundCase, 'waive')}
          >
            <HugeiconsIcon
              icon={ShieldUserIcon}
              strokeWidth={2}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Complete Root review')}
          </Button>
        </div>
      )
    }
  } else {
    nextStepAction = (
      <Button
        type='button'
        disabled={!canRelease || props.busy}
        onClick={() => props.onAction(refundCase, 'release_hold')}
      >
        <HugeiconsIcon
          icon={CheckmarkCircle02Icon}
          strokeWidth={2}
          data-icon='inline-start'
          aria-hidden='true'
        />
        {t('Release account hold')}
      </Button>
    )
  }

  return (
    <AccordionItem value={String(refundCase.id)}>
      <AccordionTrigger className='px-4 py-3 hover:no-underline'>
        <span className='flex min-w-0 flex-1 flex-col gap-2 pe-4 sm:flex-row sm:items-center sm:justify-between'>
          <span className='min-w-0'>
            <span className='flex flex-wrap items-center gap-2'>
              <span className='font-semibold'>
                {formatMinorAmount(
                  refundCase.refunded_amount_minor,
                  refundCase.currency
                )}
              </span>
              <Badge variant={pending ? 'secondary' : 'default'}>
                {pending ? t('Recovery in progress') : t('Resolved')}
              </Badge>
              <Badge variant='outline'>
                {t(refundKindLabel(refundCase.kind))}
              </Badge>
              <Badge variant='outline'>{intakeSource}</Badge>
              {refundCase.requires_root_review ? (
                <Badge variant='destructive'>{t('Root review required')}</Badge>
              ) : null}
            </span>
            <span className='text-muted-foreground mt-1 block truncate text-xs font-normal'>
              {refundCase.provider} · {refundCase.trade_no}
            </span>
          </span>
          <span className='text-muted-foreground shrink-0 text-xs font-normal'>
            {t('{{count}} open obligations', {
              count: obligations.filter(
                (obligation) =>
                  obligation.status === 'open' &&
                  getOutstandingAmount(obligation) > 0
              ).length,
            })}
          </span>
        </span>
      </AccordionTrigger>

      <AccordionContent className='flex flex-col gap-5 px-4 pb-4'>
        {refundCase.requires_root_review ? (
          <Alert variant='destructive'>
            <HugeiconsIcon
              icon={ShieldUserIcon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <AlertTitle>{t('Root review is required')}</AlertTitle>
            <AlertDescription>
              {needsCommissionQuarantine
                ? t(
                    'Quarantine the unknown commission before completing Root review.'
                  )
                : t(
                    'The refund could not be fully assessed from automated payment data. Root must create every obligation supported by verified evidence, then complete the review.'
                  )}
            </AlertDescription>
          </Alert>
        ) : null}

        <section aria-labelledby={`refund-financial-${refundCase.id}`}>
          <h3
            id={`refund-financial-${refundCase.id}`}
            className='mb-3 text-sm font-semibold'
          >
            {t('Recovery summary')}
          </h3>
          <dl className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6'>
            <FinancialValue
              label={t('Paid amount')}
              value={formatMinorAmount(
                refundCase.paid_amount_minor,
                refundCase.currency
              )}
            />
            <FinancialValue
              label={t('Refunded principal')}
              value={formatMinorAmount(
                refundCase.refunded_amount_minor,
                refundCase.currency
              )}
            />
            <FinancialValue
              label={t('API balance affected')}
              value={formatQuota(refundCase.quota_amount || 0)}
            />
            <FinancialValue
              label={t('Debited immediately')}
              value={formatQuota(refundCase.wallet_debited_quota || 0)}
            />
            <FinancialValue
              label={t('Quota debt created')}
              value={formatQuota(refundCase.debt_created_quota || 0)}
            />
            <FinancialValue
              label={t('Cash recovery obligation')}
              value={formatMinorAmount(
                refundCase.cash_debt_created_minor || 0,
                refundCase.currency
              )}
            />
          </dl>
          <div className='text-muted-foreground mt-4 grid gap-1 text-xs'>
            <p>
              {t('User ID')}: {refundCase.user_id || '-'} · {t('Top-up ID')}:{' '}
              {refundCase.top_up_id || '-'}
            </p>
            <p className='break-all'>
              {t('Refund order')}: {refundCase.refund_trade_no || '-'}
            </p>
            <p>{refundCase.reason || t('No recovery note provided.')}</p>
          </div>
        </section>

        {isSubscriptionRefund ? (
          <>
            <Separator />
            <section aria-labelledby={`refund-subscription-${refundCase.id}`}>
              <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
                <h3
                  id={`refund-subscription-${refundCase.id}`}
                  className='text-sm font-semibold'
                >
                  {t('Subscription entitlement')}
                </h3>
                <Badge variant={subscriptionBadgeVariant}>
                  {t(subscriptionStatusLabel(refundCase.subscription_status))}
                </Badge>
              </div>
              <dl className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
                <FinancialValue
                  label={t('Subscription ID')}
                  value={String(refundCase.user_subscription_id || '-')}
                />
                <FinancialValue
                  label={t('Plan ID')}
                  value={String(refundCase.subscription_plan_id || '-')}
                />
                <FinancialValue
                  label={t('Subscription quota used')}
                  value={`${formatQuota(refundCase.subscription_amount_used || 0)} / ${formatQuota(refundCase.subscription_amount_total || 0)}`}
                />
                <FinancialValue
                  label={t('Original access end')}
                  value={
                    refundCase.subscription_end_time
                      ? formatTime(refundCase.subscription_end_time)
                      : '-'
                  }
                />
              </dl>
              <Alert className='mt-4'>
                <HugeiconsIcon
                  icon={ShieldUserIcon}
                  strokeWidth={2}
                  aria-hidden='true'
                />
                <AlertDescription>
                  {t(subscriptionDescription)}
                </AlertDescription>
              </Alert>
            </section>
          </>
        ) : null}

        <Separator />

        <section aria-labelledby={`refund-obligations-${refundCase.id}`}>
          <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
            <h3
              id={`refund-obligations-${refundCase.id}`}
              className='text-sm font-semibold'
            >
              {t('Recovery obligations')}
            </h3>
            {allObligationsClosed ? (
              <Badge variant='default'>{t('All obligations closed')}</Badge>
            ) : (
              <Badge variant='secondary'>{t('Recovery required')}</Badge>
            )}
          </div>

          {obligations.length > 0 ? (
            <ItemGroup>
              {obligations.map((obligation) => (
                <GrowthRefundObligationRow
                  key={obligation.id}
                  obligation={obligation}
                  busy={props.busy}
                  canWaive={props.canUseRootActions}
                  onAction={(action, selectedObligation) =>
                    props.onAction(refundCase, action, selectedObligation)
                  }
                />
              ))}
            </ItemGroup>
          ) : (
            <Alert>
              <HugeiconsIcon
                icon={AlertCircleIcon}
                strokeWidth={2}
                aria-hidden='true'
              />
              <AlertTitle>{t('No quantified obligation')}</AlertTitle>
              <AlertDescription>
                {refundCase.requires_root_review
                  ? t(
                      'Create any recovery obligation supported by verified evidence, then complete Root review.'
                    )
                  : t('This case did not create a recoverable balance.')}
              </AlertDescription>
            </Alert>
          )}
        </section>

        <Separator />

        <section aria-labelledby={`refund-actions-${refundCase.id}`}>
          <h3
            id={`refund-actions-${refundCase.id}`}
            className='mb-3 flex items-center gap-2 text-sm font-semibold'
          >
            <HugeiconsIcon
              icon={Activity01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            {t('Action history')}
          </h3>
          {actions.length > 0 ? (
            <ol className='flex flex-col gap-3'>
              {[...actions].reverse().map((action) => {
                const details = [action.remark || t('No remark')]
                if (action.external_ref) {
                  details.push(
                    `${t('External reference')}: ${action.external_ref}`
                  )
                }
                if (action.action === 'define_manual_obligation') {
                  details.push(`${t('User ID')}: ${action.user_id}`)
                  if (action.top_up_id) {
                    details.push(`${t('Top-up ID')}: ${action.top_up_id}`)
                  }
                }
                if (action.action === 'quarantine_unknown_commission') {
                  details.push(
                    `${t('Commission ledger ID')}: ${action.commission_ledger_id || '-'}`,
                    `${t('Stored commission status')}: ${action.commission_ledger_status || '-'}`
                  )
                }
                if (action.action === 'revoke_subscription_entitlement') {
                  details.push(
                    `${t('Subscription ID')}: ${action.user_subscription_id || '-'}`
                  )
                }

                return (
                  <li
                    key={action.id}
                    className='bg-muted/50 grid gap-1 rounded-lg p-3 text-xs sm:grid-cols-[minmax(0,1fr)_auto]'
                  >
                    <div className='min-w-0'>
                      <div className='font-medium'>
                        {t(
                          action.action === 'waive' &&
                            action.obligation_id === 0
                            ? 'Root review completed'
                            : refundActionLabel(action.action)
                        )}
                      </div>
                      <div className='text-muted-foreground mt-1 break-words'>
                        {details.join(' · ')}
                      </div>
                    </div>
                    <div className='text-muted-foreground tabular-nums sm:text-right'>
                      <div>
                        {action.amount > 0
                          ? formatRecoveryAmount(
                              action.amount,
                              action.asset,
                              action.currency
                            )
                          : t('Case-level action')}
                      </div>
                      <div>
                        {formatTime(action.created_at)} · {t('Admin')} #
                        {action.actor_id}
                      </div>
                    </div>
                  </li>
                )
              })}
            </ol>
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t('No recovery actions recorded yet.')}
            </p>
          )}
        </section>

        {pending ? (
          <div className='bg-muted/50 flex flex-col gap-2 rounded-lg p-3 sm:flex-row sm:items-center sm:justify-between'>
            <div className='min-w-0'>
              <p className='text-sm font-medium'>{t(nextStepTitle)}</p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t(nextStepDescription)}
              </p>
            </div>
            {nextStepAction}
          </div>
        ) : (
          <Alert>
            <HugeiconsIcon
              icon={CheckmarkCircle02Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <AlertTitle>{t('Recovery case resolved')}</AlertTitle>
            <AlertDescription>
              {refundCase.review_note ||
                t('All required recovery steps were completed.')}
            </AlertDescription>
          </Alert>
        )}
      </AccordionContent>
    </AccordionItem>
  )
}
