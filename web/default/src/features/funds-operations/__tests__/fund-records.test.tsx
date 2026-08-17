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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { FundRecordsSection } from '../fund-records-section'

type ApiGet = (
  url: string,
  config?: {
    params?: {
      user_id?: number
      p?: number
      page_size?: number
    }
  }
) => Promise<{ data: unknown }>

const apiClient = api as unknown as { get: ApiGet }
const originalGet = apiClient.get

function renderSection() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <FundRecordsSection />
    </QueryClientProvider>
  )
  return queryClient
}

afterEach(() => {
  apiClient.get = originalGet
})

describe('admin promotion fund records', () => {
  test('validates the user ID and renders the complete audit chain', async () => {
    const requests: Array<{ url: string; userId?: number }> = []
    apiClient.get = async (url, config) => {
      requests.push({ url, userId: config?.params?.user_id })
      return {
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                id: 901,
                transaction_key: 'withdrawal:44:paid',
                kind: 'commission_withdrawal_paid',
                user_id: 42,
                source_type: 'promotion_withdrawals',
                source_id: 44,
                actor_type: 'admin',
                actor_id: 7,
                external_ref: 'bank-7788',
                occurred_at: 1_700_000_000,
                created_at: 1_700_000_000,
                legs: [
                  {
                    id: 902,
                    transaction_id: 901,
                    account: 'commission_reserved',
                    asset: 'cash',
                    currency: 'JPY',
                    amount: -1250,
                    balance_after: 500,
                  },
                ],
              },
            ],
          },
        },
      }
    }
    const queryClient = renderSection()
    const user = userEvent.setup()

    expect(
      screen.getByText(
        'This journal covers account credits, rewards, promotion funds, and recovery adjustments. API usage charges remain in usage logs, so this is not a complete wallet ledger.'
      )
    ).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Search' }))
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Enter a valid user ID.'
    )
    expect(requests).toHaveLength(0)

    await user.type(screen.getByLabelText('User ID'), '42')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('#901')).toBeInTheDocument()
    expect(screen.getByText('bank-7788')).toBeInTheDocument()
    expect(screen.getByText(/promotion_withdrawals/)).toBeInTheDocument()
    expect(screen.getByText(/1,250/)).toBeInTheDocument()
    expect(screen.getByText('Debit')).toBeVisible()
    expect(requests).toEqual([
      {
        url: '/api/growth/admin/fund-records',
        userId: 42,
      },
    ])
    queryClient.clear()
  })

  test('shows a clear administrator source while retaining its audit identifier', async () => {
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 903,
              transaction_key: 'admin_quota:request-1',
              kind: 'api_balance_admin_debited',
              user_id: 42,
              source_type: 'admin_quota_adjustments',
              source_key: 'users:42:subtract:5000',
              actor_type: 'admin',
              actor_id: 7,
              remark: 'Refund recovery',
              occurred_at: 1_700_000_000,
              created_at: 1_700_000_000,
              legs: [],
            },
          ],
        },
      },
    })
    const queryClient = renderSection()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('User ID'), '42')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(
      await screen.findByText('API balance debited by administrator')
    ).toBeVisible()
    expect(screen.getByText('Administrator quota adjustment')).toBeVisible()
    expect(screen.getByText('admin_quota_adjustments')).toBeVisible()
    expect(screen.getByText(/users:42:subtract:5000/)).toBeVisible()
    queryClient.clear()
  })

  test('does not invent a currency for an invalid cash journal leg', async () => {
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 904,
              transaction_key: 'legacy-cash-without-currency',
              kind: 'refund_recovery',
              user_id: 42,
              occurred_at: 1_700_000_000,
              created_at: 1_700_000_000,
              legs: [
                {
                  id: 905,
                  transaction_id: 904,
                  account: 'refund_debt',
                  asset: 'cash',
                  amount: -500,
                  balance_after: 100,
                },
              ],
            },
          ],
        },
      },
    })
    const queryClient = renderSection()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('User ID'), '42')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('#904')).toBeInTheDocument()
    expect(screen.getByText('Payment details unavailable')).toBeVisible()
    expect(screen.getByText(/Balance after:/)).toHaveTextContent(
      'Payment details unavailable'
    )
    expect(screen.queryByText(/CNY/)).not.toBeInTheDocument()
    queryClient.clear()
  })

  test('removes the previous user records while a different user is loading', async () => {
    let resolveUser43:
      | ((response: { data: unknown } | PromiseLike<{ data: unknown }>) => void)
      | undefined
    const user43Response = new Promise<{ data: unknown }>((resolve) => {
      resolveUser43 = resolve
    })
    apiClient.get = async (_url, config) => {
      if (config?.params?.user_id === 43) return user43Response
      return {
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                id: 420,
                transaction_key: 'growth:42',
                kind: 'growth_reward_issued',
                user_id: 42,
                occurred_at: 1_700_000_000,
                created_at: 1_700_000_000,
                legs: [],
              },
            ],
          },
        },
      }
    }
    const queryClient = renderSection()
    const user = userEvent.setup()
    const userIdInput = screen.getByLabelText('User ID')

    await user.type(userIdInput, '42')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('#420')).toBeInTheDocument()
    expect(screen.getByText('User ID: 42')).toBeInTheDocument()

    await user.clear(userIdInput)
    await user.type(userIdInput, '43')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(screen.queryByText('#420')).not.toBeInTheDocument()
    expect(
      screen.getByRole('status', { name: 'Loading...' })
    ).toBeInTheDocument()
    expect(screen.getByText('User ID: 43')).toBeInTheDocument()

    resolveUser43?.({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 430,
              transaction_key: 'growth:43',
              kind: 'growth_reward_issued',
              user_id: 43,
              occurred_at: 1_700_000_000,
              created_at: 1_700_000_000,
              legs: [],
            },
          ],
        },
      },
    })

    expect(await screen.findByText('#430')).toBeInTheDocument()
    expect(screen.queryByText('#420')).not.toBeInTheDocument()
    queryClient.clear()
  })

  test('shows a loading state while the same user next page loads', async () => {
    const pages: number[] = []
    let resolvePageTwo:
      | ((response: { data: unknown } | PromiseLike<{ data: unknown }>) => void)
      | undefined
    const pageTwoResponse = new Promise<{ data: unknown }>((resolve) => {
      resolvePageTwo = resolve
    })
    apiClient.get = async (_url, config) => {
      const page = config?.params?.p || 1
      pages.push(page)
      if (page === 2) return pageTwoResponse
      return {
        data: {
          success: true,
          data: {
            total: 21,
            items: [
              {
                id: page,
                transaction_key: `growth:${page}`,
                kind: 'growth_reward_issued',
                user_id: 42,
                occurred_at: 1_700_000_000,
                created_at: 1_700_000_000,
                legs: [
                  {
                    id: page,
                    transaction_id: page,
                    account: 'api_balance',
                    asset: 'quota',
                    amount: 5000,
                    balance_after: 5000,
                  },
                ],
              },
            ],
          },
        },
      }
    }
    const queryClient = renderSection()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('User ID'), '42')
    await user.click(screen.getByRole('button', { name: 'Search' }))
    expect(
      await screen.findByText('21 records · Page 1 of 2')
    ).toBeInTheDocument()
    expect(screen.getByText('#1')).toBeInTheDocument()
    expect(screen.getByText('Credit')).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Next' }))
    await waitFor(() => expect(pages).toEqual([1, 2]))
    expect(screen.queryByText('#1')).not.toBeInTheDocument()
    expect(
      screen.getByRole('status', { name: 'Loading...' })
    ).toBeInTheDocument()
    expect(
      screen.queryByText('21 records · Page 2 of 2')
    ).not.toBeInTheDocument()

    resolvePageTwo?.({
      data: {
        success: true,
        data: {
          total: 21,
          items: [
            {
              id: 2,
              transaction_key: 'growth:2',
              kind: 'growth_reward_issued',
              user_id: 42,
              occurred_at: 1_700_000_000,
              created_at: 1_700_000_000,
              legs: [
                {
                  id: 2,
                  transaction_id: 2,
                  account: 'api_balance',
                  asset: 'quota',
                  amount: 5000,
                  balance_after: 5000,
                },
              ],
            },
          ],
        },
      },
    })

    expect(await screen.findByText('#2')).toBeInTheDocument()
    expect(screen.queryByText('#1')).not.toBeInTheDocument()
    expect(screen.getByText('21 records · Page 2 of 2')).toBeInTheDocument()
    expect(pages).toEqual([1, 2])
    queryClient.clear()
  })

  test('treats an unsuccessful API payload as a load error', async () => {
    apiClient.get = async () => ({
      data: { success: false, message: 'journal unavailable' },
    })
    const queryClient = renderSection()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('User ID'), '42')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('Unable to load fund history')).toBeVisible()
    expect(screen.getByText('Refresh the list and try again.')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible()
    expect(screen.queryByText('No fund records yet')).not.toBeInTheDocument()
    queryClient.clear()
  })
})
