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
export type AdminPromotionFundRecordLeg = {
  id: number
  transaction_id: number
  account: string
  asset: 'quota' | 'cash'
  currency?: string
  amount: number
  source_type?: string
  source_id?: number
  balance_after: number | null
}

export type AdminPromotionFundRecord = {
  id: number
  transaction_key: string
  kind: string
  user_id: number
  source_type?: string
  source_id?: number
  source_key?: string
  reverses_transaction_id?: number
  actor_type?: string
  actor_id?: number
  actor_ref?: string
  external_ref?: string
  remark?: string
  occurred_at: number
  created_at: number
  legs: AdminPromotionFundRecordLeg[]
}
