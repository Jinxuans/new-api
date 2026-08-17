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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'

import { EarningsSection } from '../earnings-section'

type ApiGet = (url: string) => Promise<{ data: unknown }>
type ApiPost = (url: string, data?: unknown) => Promise<{ data: unknown }>
const apiClient = api as unknown as { get: ApiGet; post: ApiPost }
const originalGet = apiClient.get
const originalPost = apiClient.post

function installOverviewFixture() {
  apiClient.post = async (url) => {
    if (url === '/api/growth/commissions/settle') {
      return {
        data: {
          success: true,
          data: {
            task_reward_earned_quota: 8_000,
            task_reward_pending_quota: 2_000,
            referral_credit_available_quota: 12_000,
            referral_credit_total_quota: 20_000,
            available_reward_quota: 0,
            pending_reward_quota: 2_000,
            total_reward_quota: 8_000,
            invite_count: 3,
            monthly_rebate_quota: 0,
            total_rebate_quota: 20_000,
            aff_code: 'CODE',
            invite_rebate_percent: 10,
            invitation_chain_reward_quota: 0,
            cash_commission: {
              currency: 'CNY',
              available_amount_cents: 1_234,
              pending_amount_cents: 200,
              withdrawing_amount_cents: 0,
              withdrawn_amount_cents: 0,
              transferred_amount_cents: 0,
              available_quota_equivalent: 500_000,
            },
          },
        },
      }
    }
    throw new Error(`Unexpected POST ${url}`)
  }
  apiClient.get = async (url) => {
    if (url === '/api/user/self') {
      return {
        data: {
          success: true,
          data: {
            id: 1,
            username: 'customer',
            quota: 0,
            used_quota: 0,
            request_count: 0,
            aff_quota: 12_000,
            aff_history_quota: 20_000,
            aff_count: 3,
            group: 'default',
          },
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  }
}

function renderEarnings() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <EarningsSection />
    </QueryClientProvider>
  )
  return queryClient
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
})

describe('promotion earnings actions', () => {
  test('confirms converting all cash with both cash and quota amounts', async () => {
    installOverviewFixture()
    const queryClient = renderEarnings()
    expect(await screen.findByText('Total earned')).toBeInTheDocument()
    fireEvent.click(
      screen.getByRole('button', { name: 'Convert all to balance' })
    )

    expect(
      await screen.findByText('Convert all cash commission to API balance?')
    ).toBeInTheDocument()
    expect(
      screen.getByText((content) =>
        content.includes(
          `CNY 12.34 will be converted into ${formatQuota(500_000)}`
        )
      )
    ).toBeInTheDocument()
    queryClient.clear()
  })

  test('converts only the cash balance shown in the confirmation', async () => {
    installOverviewFixture()
    const overviewPost = apiClient.post
    const actionRequests: unknown[] = []
    apiClient.post = async (url, data) => {
      if (url === '/api/growth/commissions/transfer') {
        actionRequests.push(data)
        return { data: { success: true, data: { quota: 500_000 } } }
      }
      return overviewPost(url, data)
    }
    const queryClient = renderEarnings()

    fireEvent.click(
      await screen.findByRole('button', { name: 'Convert all to balance' })
    )
    fireEvent.click(
      await screen.findByRole('button', { name: 'Convert all cash' })
    )

    await waitFor(() => {
      expect(actionRequests).toEqual([
        {
          expected_amount_cents: 1_234,
          expected_quota_equivalent: 500_000,
        },
      ])
    })
    queryClient.clear()
  })

  test('labels withdrawal fields and blocks empty payout details', async () => {
    installOverviewFixture()
    const queryClient = renderEarnings()
    fireEvent.click(
      await screen.findByRole('button', { name: 'Withdraw all cash' })
    )

    expect(
      await screen.findByText('Withdraw all cash commission?')
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Payout method')).toBeInTheDocument()
    expect(screen.getByLabelText('Payout account')).toBeInTheDocument()
    expect(
      screen.getByText((content) =>
        content.includes(
          `CNY 12.34 (equivalent to ${formatQuota(500_000)} of API balance)`
        )
      )
    ).toBeInTheDocument()

    fireEvent.click(
      screen.getByRole('button', { name: 'Confirm withdrawal of all cash' })
    )
    expect(await screen.findByText('Payout method is required')).toBeVisible()
    expect(screen.getByText('Payout account is required')).toBeVisible()
    expect(screen.getByLabelText('Payout method')).toHaveAttribute(
      'aria-invalid',
      'true'
    )
    queryClient.clear()
  })

  test('withdraws only the cash balance shown in the confirmation', async () => {
    installOverviewFixture()
    const overviewPost = apiClient.post
    const actionRequests: unknown[] = []
    apiClient.post = async (url, data) => {
      if (url === '/api/growth/withdrawals') {
        actionRequests.push(data)
        return { data: { success: true, data: { id: 1 } } }
      }
      return overviewPost(url, data)
    }
    const queryClient = renderEarnings()

    fireEvent.click(
      await screen.findByRole('button', { name: 'Withdraw all cash' })
    )
    fireEvent.change(screen.getByLabelText('Payout method'), {
      target: { value: 'bank' },
    })
    fireEvent.change(screen.getByLabelText('Payout account'), {
      target: { value: 'account-1' },
    })
    fireEvent.click(
      screen.getByRole('button', { name: 'Confirm withdrawal of all cash' })
    )

    await waitFor(() => {
      expect(actionRequests).toEqual([
        {
          payout_method: 'bank',
          payout_account: 'account-1',
          remark: '',
          expected_amount_cents: 1_234,
          expected_quota_equivalent: 500_000,
        },
      ])
    })
    queryClient.clear()
  })
})
