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

import { formatQuota } from '@/lib/format'

import type { PromotionEvent } from '../../shared'
import { PromotionActivityRows } from '../activity-rows'

function renderEvent(event: Partial<PromotionEvent>) {
  render(
    <PromotionActivityRows
      filter='all'
      items={[
        {
          id: 1,
          event_type: 'commission_settled',
          direction: 'income',
          ...event,
        },
      ]}
    />
  )
}

describe('promotion activity amount semantics', () => {
  test('shows settled commission as cash without a second API balance gain', () => {
    renderEvent({
      quota_delta: 5_000,
      cash_amount_cents: 1_000,
      currency: 'CNY',
    })

    expect(screen.getByText('+CNY 10.00')).toBeInTheDocument()
    expect(screen.queryByText(`+${formatQuota(5_000)}`)).not.toBeInTheDocument()
  })

  test('shows converted commission as API balance and keeps cash as context', () => {
    renderEvent({
      event_type: 'commission_transferred',
      quota_delta: 5_000,
      cash_amount_cents: 1_000,
      currency: 'CNY',
    })

    expect(screen.getByText(`+${formatQuota(5_000)}`)).toBeInTheDocument()
    expect(screen.getByText(/Cash commission: CNY 10\.00/)).toBeInTheDocument()
    expect(screen.queryByText(/\//)).not.toBeInTheDocument()
  })
})
