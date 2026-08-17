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
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { ActivitySection } from '../activity-section'

type ApiGet = (
  url: string,
  config?: { params?: { p?: number; page_size?: number } }
) => Promise<{ data: unknown }>

const apiClient = api as unknown as { get: ApiGet }
const originalGet = apiClient.get

afterEach(() => {
  apiClient.get = originalGet
})

describe('promotion activity pagination', () => {
  test('loads the selected server page when the user moves forward', async () => {
    const requestedPages: number[] = []
    apiClient.get = async (url, config) => {
      expect(url).toBe('/api/growth/fund-records')
      const page = config?.params?.p || 1
      requestedPages.push(page)
      return {
        data: {
          success: true,
          data: {
            page,
            page_size: 10,
            total: 11,
            items: [
              {
                kind: 'growth_reward_issued',
                source: 'growth_reward',
                occurred_at: 1_700_000_000,
                legs: [
                  {
                    account: 'referral_credit',
                    asset: 'quota',
                    amount: 5_000,
                    balance_after: page * 5_000,
                  },
                ],
              },
            ],
          },
        },
      }
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ActivitySection />
      </QueryClientProvider>
    )

    expect(await screen.findByText(/Source: Task reward/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    expect(
      await screen.findByText('11 records · Page 2 of 2')
    ).toBeInTheDocument()
    expect(requestedPages).toEqual([1, 2])
    queryClient.clear()
  })

  test('loads and renders fixed referral credit from its paginated endpoint', async () => {
    const requestedUrls: string[] = []
    apiClient.get = async (url) => {
      requestedUrls.push(url)
      if (url === '/api/growth/fund-records') {
        return {
          data: {
            success: true,
            data: {
              page: 1,
              page_size: 10,
              total: 1,
              items: [
                {
                  kind: 'growth_reward_issued',
                  source: 'growth_reward',
                  legs: [
                    {
                      account: 'referral_credit',
                      asset: 'quota',
                      amount: 5_000,
                    },
                  ],
                },
              ],
            },
          },
        }
      }
      expect(url).toBe('/api/user/aff/rewards')
      return {
        data: {
          success: true,
          data: {
            page: 1,
            page_size: 10,
            total: 1,
            items: [
              {
                invitee_name: 'Alice',
                reward_type: 'first_request',
                reward_quota: 12_000,
                status: 'settled',
                created_at: 1_700_000_000,
              },
            ],
          },
        },
      }
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={queryClient}>
        <ActivitySection />
      </QueryClientProvider>
    )

    expect(await screen.findByText('Task reward added')).toBeInTheDocument()
    await user.click(screen.getByLabelText('Activity type'))
    await user.click(
      await screen.findByRole('option', { name: 'Referral credit' })
    )

    expect(
      await screen.findByText('Invitation first request reward')
    ).toBeInTheDocument()
    expect(screen.getByText(/Alice/)).toBeInTheDocument()
    expect(requestedUrls).toEqual([
      '/api/growth/fund-records',
      '/api/user/aff/rewards',
    ])
    queryClient.clear()
  })
})
