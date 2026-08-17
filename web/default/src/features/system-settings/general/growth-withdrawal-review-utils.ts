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
import type { AdminPromotionWithdrawal } from './growth-withdrawal-review-types'

export function getWithdrawalPayoutAccount(
  withdrawal: AdminPromotionWithdrawal
) {
  if (!withdrawal.payout_account_snapshot) return '-'
  try {
    const snapshot = JSON.parse(withdrawal.payout_account_snapshot) as {
      payout_account?: string
    }
    return snapshot.payout_account || '-'
  } catch {
    return '-'
  }
}

export function getWithdrawalExternalReference(
  withdrawal: AdminPromotionWithdrawal
) {
  if (withdrawal.trade_no) return withdrawal.trade_no
  const operation = [...(withdrawal.operations || [])]
    .reverse()
    .find((item) => item.external_reference)
  return operation?.external_reference || '-'
}
