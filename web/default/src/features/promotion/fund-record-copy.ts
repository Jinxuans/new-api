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
const FUND_KIND_TITLE_KEYS: Record<string, string> = {
  new_user_registration_reward_issued: 'Welcome reward added',
  invitee_registration_reward_issued: 'Referral sign-up reward added',
  growth_reward_issued: 'Task reward added',
  growth_reward_reversed: 'Task reward reversed',
  invitation_reward_issued: 'Referral reward added',
  invitation_reward_transferred: 'Referral credit moved to API balance',
  commission_pending_accrued: 'Cash commission pending',
  commission_available_accrued: 'Cash commission added',
  commission_settled: 'Cash commission became available',
  commission_transferred_to_balance: 'Cash commission moved to API balance',
  commission_reversed: 'Cash commission reversed',
  commission_withdrawal_reserved: 'Cash reserved for withdrawal',
  commission_withdrawal_released: 'Cash returned from withdrawal',
  commission_withdrawal_paid: 'Cash withdrawal paid',
  refund_debt_assessment: 'Recovery obligation created',
  refund_recovery: 'Recovery payment recorded',
  refund_waiver: 'Recovery debt waived',
  reversal: 'Refund reversal recorded',
  api_balance_topup_credited: 'Top-up',
  api_balance_subscription_debited: 'Subscription purchased with API balance',
  redemption_credited: 'Redemption credited to API balance',
  api_balance_admin_credited: 'API balance credited by administrator',
  api_balance_admin_debited: 'API balance debited by administrator',
  api_balance_admin_overridden: 'API balance set by administrator',
  root_initial_quota_granted: 'Initial administrator balance',
  legacy_aggregate: 'Opening reward balance recorded',
}

const FUND_ACCOUNT_LABEL_KEYS: Record<string, string> = {
  api_balance: 'API balance',
  referral_credit: 'Referral credit',
  commission_pending: 'Pending cash commission',
  commission_available: 'Available cash commission',
  commission_reserved: 'Cash reserved for withdrawal',
  refund_debt: 'Refund recovery debt',
}

const FUND_SOURCE_LABEL_KEYS: Record<string, string> = {
  registration_reward: 'Account registration',
  growth_reward: 'Task reward',
  growth_rewards: 'Task reward',
  invitation_reward: 'Referral reward',
  invitation_rewards: 'Referral reward',
  commission: 'Cash commission',
  promotion_commission_ledgers: 'Cash commission',
  withdrawal: 'Cash withdrawal',
  promotion_withdrawals: 'Cash withdrawal',
  refund: 'Refund recovery case',
  promotion_refund_cases: 'Refund recovery case',
  topup: 'Top-up account',
  top_ups: 'Top-up account',
  subscription: 'Subscription order',
  subscription_orders: 'Subscription order',
  redemption: 'Redemption code',
  redemptions: 'Redemption code',
  admin_adjustment: 'Administrator quota adjustment',
  admin_quota_adjustments: 'Administrator quota adjustment',
  opening_balance: 'Opening balance',
  system_setup: 'Opening balance',
  legacy: 'Legacy balance snapshot',
  legacy_aggregate: 'Legacy balance snapshot',
}

export function getPromotionFundKindTitleKey(kind: string): string {
  return FUND_KIND_TITLE_KEYS[kind] || 'Fund adjustment'
}

export function getPromotionFundAccountLabelKey(account: string): string {
  return FUND_ACCOUNT_LABEL_KEYS[account] || 'Promotion account'
}

export function getPromotionFundSourceLabelKey(source: string): string {
  return FUND_SOURCE_LABEL_KEYS[source] || 'System record'
}
