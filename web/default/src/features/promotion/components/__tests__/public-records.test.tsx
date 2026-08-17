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

import type {
  GrowthReward,
  GrowthSubmission,
  PromotionCommissionLedger,
  PromotionFundTransaction,
} from '../../shared'
import { PromotionActivityRows } from '../activity-rows'

describe('promotion public records', () => {
  test('renders task rewards without a database id or internal remark', () => {
    const reward: GrowthReward = {
      item_code: 'daily_checkin',
      reward_quota: 5_000,
      status: 'settled',
      created_at: 1_700_000_000,
    }

    render(<PromotionActivityRows filter='tasks' items={[reward, reward]} />)

    expect(screen.getAllByText('Daily check-in')).toHaveLength(2)
    expect(screen.getAllByText('settled')).toHaveLength(2)
  })

  test('renders a user submission without reviewer identity or a database id', () => {
    const submission: GrowthSubmission = {
      item_code: 'content_publish',
      platform: 'Blog',
      url: 'https://example.com/post',
      remark: 'My proof',
      status: 'approved',
      review_note: 'Accepted',
      created_at: 1_700_000_000,
    }

    render(<PromotionActivityRows filter='submissions' items={[submission]} />)

    expect(
      screen.getByText('Publish an article, video, or tutorial')
    ).toBeInTheDocument()
    expect(screen.getByText(/Blog.*Accepted/)).toBeInTheDocument()
  })

  test('renders commission history without a source payment id', () => {
    const commission: PromotionCommissionLedger = {
      currency: 'CNY',
      gross_amount_cents: 1_000,
      fee_amount_cents: 0,
      tax_amount_cents: 0,
      net_amount_cents: 1_000,
      quota_equivalent: 5_000,
      status: 'settled',
      created_at: 1_700_000_000,
    }

    render(<PromotionActivityRows filter='commissions' items={[commission]} />)

    expect(screen.getByText('Top-up cash commission')).toBeInTheDocument()
    expect(screen.getByText('CNY 10.00')).toBeInTheDocument()
  })

  test('labels wallet funding, subscription, and administrator records clearly', () => {
    const records: PromotionFundTransaction[] = [
      {
        kind: 'api_balance_topup_credited',
        source: 'topup',
        occurred_at: 1_699_999_999,
        legs: [],
      },
      {
        kind: 'redemption_credited',
        source: 'redemption',
        occurred_at: 1_700_000_000,
        legs: [],
      },
      {
        kind: 'api_balance_admin_credited',
        source: 'admin_adjustment',
        occurred_at: 1_700_000_001,
        legs: [],
      },
      {
        kind: 'api_balance_admin_debited',
        source: 'admin_adjustment',
        occurred_at: 1_700_000_002,
        legs: [],
      },
      {
        kind: 'api_balance_admin_overridden',
        source: 'admin_adjustment',
        occurred_at: 1_700_000_003,
        legs: [],
      },
      {
        kind: 'api_balance_subscription_debited',
        source: 'subscription',
        occurred_at: 1_700_000_004,
        legs: [],
      },
      {
        kind: 'root_initial_quota_granted',
        source: 'opening_balance',
        occurred_at: 1_700_000_005,
        legs: [],
      },
    ]

    render(<PromotionActivityRows filter='funds' items={records} />)

    expect(screen.getByText('Top-up')).toBeVisible()
    expect(screen.getByText('Redemption credited to API balance')).toBeVisible()
    expect(
      screen.getByText('API balance credited by administrator')
    ).toBeVisible()
    expect(
      screen.getByText('API balance debited by administrator')
    ).toBeVisible()
    expect(screen.getByText('API balance set by administrator')).toBeVisible()
    expect(
      screen.getByText('Subscription purchased with API balance')
    ).toBeVisible()
    expect(screen.getByText('Initial administrator balance')).toBeVisible()
    expect(screen.getByText(/Source: Redemption code/)).toBeVisible()
    expect(screen.getByText(/Source: Top-up account/)).toBeVisible()
    expect(screen.getByText(/Source: Subscription order/)).toBeVisible()
    expect(screen.getByText(/Source: Opening balance/)).toBeVisible()
    expect(
      screen.getAllByText(/Source: Administrator quota adjustment/)
    ).toHaveLength(3)
  })
})
