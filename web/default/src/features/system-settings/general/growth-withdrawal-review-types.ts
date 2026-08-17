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
import type { PromotionWithdrawal } from '@/features/promotion/shared'

export type PromotionWithdrawalOperation = {
  id: number
  action: string
  actor_type: string
  actor_id: number
  reconstructed: boolean
  note?: string
  external_reference?: string
  created_at?: number | string
}

export type AdminPromotionWithdrawal = PromotionWithdrawal & {
  user_id: number
  reviewer_id?: number
  review_note?: string
  payout_account_snapshot?: string
  payout_initiated_at?: number | string
  operations?: PromotionWithdrawalOperation[]
}

export type WithdrawalStatusFilter =
  | 'pending_review'
  | 'approved'
  | 'processing'
  | 'paid'
  | 'rejected'
  | 'failed'
  | 'all'

export type WithdrawalConfirmation = {
  action: 'approve' | 'initiate' | 'paid' | 'failed' | 'reject'
  withdrawal: AdminPromotionWithdrawal
}
