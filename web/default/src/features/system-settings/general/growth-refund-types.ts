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
import { formatMinorAmount } from '@/lib/currency'
import { formatQuota } from '@/lib/format'

export {
  formatMinorAmount,
  getMinorAmountFactor,
  parseMinorAmount,
} from '@/lib/currency'

export type PromotionRefundObligation = {
  id: number
  refund_case_id: number
  user_id: number
  account: string
  asset: 'quota' | 'cash'
  currency?: string
  amount: number
  recovered_amount: number
  waived_amount: number
  source_type?: string
  source_id?: number
  status: 'open' | 'recovered' | 'waived'
  created_at: number
  updated_at: number
}

export type PromotionRefundAction = {
  id: number
  obligation_id: number
  user_id: number
  action:
    | 'retry_wallet_debit'
    | 'record_external_repayment'
    | 'recover_paid_commission'
    | 'define_manual_obligation'
    | 'quarantine_unknown_commission'
    | 'revoke_subscription_entitlement'
    | 'waive'
    | 'release_hold'
  asset?: 'quota' | 'cash'
  currency?: string
  amount: number
  actor_id: number
  top_up_id?: number
  commission_ledger_id?: number
  commission_ledger_status?: string
  user_subscription_id?: number
  subscription_end_time_before?: number
  fund_transaction_id?: number
  external_ref?: string
  remark?: string
  created_at: number
}

export type PromotionRefundResponsibleUser = {
  user_id: number
  username?: string
  is_top_up_user: boolean
  is_rebate_recipient?: boolean
  invitation_rebate_id?: number
  rebate_amount_minor?: number
  rebate_quota?: number
  rebate_currency?: string
  is_commission_recipient: boolean
  commission_ledger_id?: number
  commission_amount_minor?: number
  commission_quota?: number
  commission_currency?: string
  is_invitation_reward_recipient?: boolean
  invitation_reward_id?: number
  invitation_reward_quota?: number
  invitation_transferred_quota?: number
}

export type PromotionRefundCase = {
  id: number
  intake_source?:
    | 'provider_webhook'
    | 'offline_refund'
    | 'provider_refund'
    | 'chargeback'
    | 'missed_callback'
  initiator_type?: 'provider' | 'admin'
  initiator_id?: number
  provider: string
  trade_no: string
  refund_trade_no: string
  kind: 'full_refund' | 'partial_refund' | 'dispute'
  paid_amount_minor: number
  refunded_amount_minor: number
  currency: string
  top_up_id: number
  user_id: number
  quota_amount: number
  wallet_debited_quota: number
  debt_created_quota: number
  cash_debt_created_minor: number
  status: 'pending_review' | 'resolved'
  requires_root_review: boolean
  responsibility_fingerprint?: string
  commission_ledger_id?: number
  commission_ledger_status?: string
  commission_reconciliation_required: boolean
  subscription_order_id?: number
  user_subscription_id?: number
  subscription_plan_id?: number
  subscription_status?: 'active' | 'expired' | 'cancelled'
  subscription_start_time?: number
  subscription_end_time?: number
  subscription_amount_total?: number
  subscription_amount_used?: number
  reason: string
  review_note?: string
  reviewer_id?: number
  created_at: number
  resolved_at?: number
  obligations: PromotionRefundObligation[]
  actions: PromotionRefundAction[]
  responsible_users?: PromotionRefundResponsibleUser[]
}

export type RefundRecoveryAction = PromotionRefundAction['action']

export type RefundActionIntent = {
  refundCase: PromotionRefundCase
  action: RefundRecoveryAction
  obligation?: PromotionRefundObligation
  idempotencyKey: string
}

export function getOutstandingAmount(obligation: PromotionRefundObligation) {
  return Math.max(
    0,
    Number(obligation.amount || 0) -
      Number(obligation.recovered_amount || 0) -
      Number(obligation.waived_amount || 0)
  )
}

export function formatRecoveryAmount(
  amount: number,
  asset?: PromotionRefundObligation['asset'],
  currency?: string
) {
  if (asset === 'cash') {
    return formatMinorAmount(amount, currency || 'CNY')
  }
  if (asset === 'quota') {
    return formatQuota(amount)
  }
  return '-'
}

export function refundKindLabel(kind: PromotionRefundCase['kind']) {
  switch (kind) {
    case 'full_refund':
      return 'Full refund'
    case 'partial_refund':
      return 'Partial refund'
    case 'dispute':
      return 'Dispute'
  }
}

export function refundActionLabel(action: PromotionRefundAction['action']) {
  switch (action) {
    case 'retry_wallet_debit':
      return 'API balance debited'
    case 'record_external_repayment':
      return 'External repayment recorded'
    case 'recover_paid_commission':
      return 'Paid commission recovered'
    case 'define_manual_obligation':
      return 'Manual recovery obligation created'
    case 'quarantine_unknown_commission':
      return 'Unknown commission quarantined'
    case 'revoke_subscription_entitlement':
      return 'Subscription entitlement terminated'
    case 'waive':
      return 'Recovery waived'
    case 'release_hold':
      return 'Account hold released'
  }
}
