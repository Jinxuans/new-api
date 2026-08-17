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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { BillingHistoryDialog } from '../billing-history-dialog'

type ApiMethod = (
  url: string,
  payload?: unknown
) => Promise<{ data: Record<string, unknown> }>

const apiClient = api as unknown as { get: ApiMethod; post: ApiMethod }
const originalGet = apiClient.get
const originalPost = apiClient.post

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  useAuthStore.getState().auth.setUser(null)
})

describe('administrator manual top-up completion', () => {
  test('requires an audit reason and submits its trimmed value', async () => {
    const requests: Array<{ url: string; payload: unknown }> = []
    apiClient.get = async (url) => {
      expect(url).toBe('/api/user/topup?p=1&page_size=10')
      return {
        data: {
          success: true,
          data: {
            items: [
              {
                id: 1,
                user_id: 42,
                amount: 10,
                money: 10,
                trade_no: 'topup-pending-1',
                payment_method: 'stripe',
                create_time: 1_700_000_000,
                status: 'pending',
              },
            ],
            total: 1,
          },
        },
      }
    }
    apiClient.post = async (url, payload) => {
      requests.push({ url, payload })
      return { data: { success: true } }
    }
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'administrator',
      role: ROLE.ADMIN,
    })

    render(<BillingHistoryDialog open onOpenChange={() => undefined} />)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'Complete Order' })
    )

    const reason = screen.getByLabelText('Reason')
    const confirm = screen.getByRole('button', { name: 'Confirm' })
    expect(confirm).toBeDisabled()

    await user.click(reason)
    await user.tab()
    expect(screen.getByRole('alert')).toHaveTextContent('Reason is required.')

    await user.type(reason, '  Verified payment receipt  ')
    expect(confirm).toBeEnabled()
    await user.click(confirm)

    await waitFor(() => {
      expect(requests).toEqual([
        {
          url: '/api/user/topup/complete',
          payload: {
            trade_no: 'topup-pending-1',
            reason: 'Verified payment receipt',
          },
        },
      ])
    })
    await waitFor(() => {
      expect(screen.queryByLabelText('Reason')).not.toBeInTheDocument()
    })
  })
})

describe('user top-up history', () => {
  test('shows the authoritative paid currency, credited balance, purpose, and refund', async () => {
    apiClient.get = async (url) => {
      expect(url).toBe('/api/user/topup/self?p=1&page_size=10')
      return {
        data: {
          success: true,
          data: {
            items: [
              {
                id: 2,
                user_id: 7,
                purpose: 'api_balance',
                amount: 2,
                money: 2,
                credited_quota: 750_000,
                paid_amount_minor: 1_250,
                paid_currency: 'JPY',
                paid_amount_verified: true,
                refunded_amount_minor: 250,
                refunded_quota: 100_000,
                refund_status: 'partial',
                trade_no: 'topup-refunded-1',
                payment_method: 'stripe',
                create_time: 1_700_000_000,
                status: 'success',
              },
            ],
            total: 1,
          },
        },
      }
    }
    useAuthStore.getState().auth.setUser({
      id: 7,
      username: 'customer',
      role: ROLE.USER,
    })

    render(<BillingHistoryDialog open onOpenChange={() => undefined} />)

    expect(
      await screen.findByText('Showing your top-ups from the last 30 days.')
    ).toBeVisible()
    expect(screen.getByText('API balance top-up')).toBeVisible()
    expect(screen.getByText(/JPY\s*1,250/)).toBeVisible()
    expect(screen.getByText(formatQuota(750_000))).toBeVisible()
    expect(screen.getByText('Partially refunded')).toBeVisible()
    expect(screen.getByText(/JPY\s*250/)).toBeVisible()
    expect(screen.getByText(formatQuota(100_000))).toBeVisible()
  })
})
