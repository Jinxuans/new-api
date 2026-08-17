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
import { describe, expect, test } from 'vitest'

import type { PromotionFundTransaction } from '../../shared'
import { PromotionActivityRows } from '../activity-rows'

function renderTransaction(transaction: Partial<PromotionFundTransaction>) {
  render(
    <PromotionActivityRows
      filter='funds'
      items={[
        {
          kind: 'commission_settled',
          source: 'commission',
          legs: [],
          ...transaction,
        },
      ]}
    />
  )
}

describe('promotion activity amount semantics', () => {
  test('shows both account legs when pending commission becomes available', () => {
    renderTransaction({
      legs: [
        {
          account: 'commission_pending',
          asset: 'cash',
          currency: 'CNY',
          amount: -1_000,
          balance_after: 0,
        },
        {
          account: 'commission_available',
          asset: 'cash',
          currency: 'CNY',
          amount: 1_000,
          balance_after: 1_000,
        },
      ],
    })

    expect(
      screen.getByText('Debited from Pending cash commission')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Credited to Available cash commission')
    ).toBeInTheDocument()
    expect(screen.getByText('-CNY 10.00')).toBeInTheDocument()
    expect(screen.getByText('+CNY 10.00')).toBeInTheDocument()
  })

  test('shows cash source and API balance destination for commission conversion', () => {
    renderTransaction({
      kind: 'commission_transferred_to_balance',
      legs: [
        {
          account: 'commission_available',
          asset: 'cash',
          currency: 'CNY',
          amount: -1_000,
        },
        {
          account: 'api_balance',
          asset: 'quota',
          amount: 5_000,
          balance_after: 20_000,
        },
      ],
    })

    expect(
      screen.getByText('Debited from Available cash commission')
    ).toBeInTheDocument()
    expect(screen.getByText('Credited to API balance')).toBeInTheDocument()
    expect(screen.getByText('-CNY 10.00')).toBeInTheDocument()
  })

  test('uses the currency minor-unit scale for cash recovery legs', () => {
    renderTransaction({
      kind: 'refund_debt_assessment',
      source: 'refund',
      legs: [
        {
          account: 'refund_debt',
          asset: 'cash',
          currency: 'JPY',
          amount: 500,
        },
      ],
    })

    expect(screen.getByText('+JPY 500')).toBeInTheDocument()
  })
})
