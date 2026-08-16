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
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { GrowthWithdrawalsReviewSection } from '../growth-withdrawals-review-section'

type ApiGet = (
  url: string,
  config?: unknown
) => Promise<{ data: Record<string, unknown> }>

const apiClient = api as unknown as { get: ApiGet }
const originalGet = apiClient.get

afterEach(() => {
  apiClient.get = originalGet
})

describe('withdrawal review actions', () => {
  test('allows an approved withdrawal to be rejected', async () => {
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 9,
              user_id: 42,
              currency: 'CNY',
              gross_amount_cents: 1200,
              fee_amount_cents: 0,
              tax_amount_cents: 0,
              net_amount_cents: 1200,
              status: 'approved',
              payout_method: 'bank',
              payout_account_snapshot: JSON.stringify({
                payout_account: 'ending 1234',
              }),
              applied_at: 1,
            },
          ],
        },
      },
    })

    render(<GrowthWithdrawalsReviewSection />)

    expect(
      await screen.findByRole('button', { name: 'Mark paid' })
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeVisible()
  })
})
