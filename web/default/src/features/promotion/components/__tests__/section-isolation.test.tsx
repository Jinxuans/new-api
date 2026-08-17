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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { PromotionCenter } from '../../index'

type ApiGet = (url: string) => Promise<{ data: unknown }>
type ApiPost = (url: string) => Promise<{ data: unknown }>
const apiClient = api as unknown as { get: ApiGet; post: ApiPost }
const originalGet = apiClient.get
const originalPost = apiClient.post

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  window.localStorage.clear()
})

describe('promotion section isolation', () => {
  test('keeps referral, activity, and guide usable when earnings fail', async () => {
    const user = userEvent.setup()
    apiClient.get = async (url) => {
      if (url === '/api/status') {
        return {
          data: {
            data: {
              growth_center_enabled: false,
              growth_submission_enabled: false,
            },
          },
        }
      }
      if (url === '/api/user/aff') {
        return { data: { success: true, data: 'CODE' } }
      }
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
      throw new Error(`Unexpected GET ${url}`)
    }
    apiClient.post = async (url) => {
      if (url === '/api/growth/commissions/settle') {
        throw new Error('Earnings unavailable')
      }
      throw new Error(`Unexpected POST ${url}`)
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <PromotionCenter />
      </QueryClientProvider>
    )

    expect(
      await screen.findByText('This section could not be loaded')
    ).toBeInTheDocument()
    expect(await screen.findByLabelText('Referral code')).toHaveValue('CODE')
    expect(await screen.findByText('Task reward added')).toBeInTheDocument()
    const guideTrigger = screen.getByRole('button', {
      name: 'Show promotion guide',
    })
    expect(guideTrigger).toHaveAttribute('aria-expanded', 'false')
    expect(guideTrigger).toHaveClass('min-h-11')
    expect(
      screen.getByRole('button', { name: 'Copy referral link' })
    ).toHaveClass('size-11')
    expect(
      screen.getByRole('button', { name: 'Copy referral code' })
    ).toHaveClass('size-11')
    guideTrigger.focus()
    await user.keyboard('{Enter}')
    expect(guideTrigger).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByText('How it works')).toBeInTheDocument()
    const sectionNavigation = screen.getByRole('navigation', {
      name: 'Rewards page sections',
    })
    expect(within(sectionNavigation).getAllByRole('link')).toHaveLength(4)
    expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
    queryClient.clear()
  })
})
