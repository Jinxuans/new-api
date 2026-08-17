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
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import { RefundRecoveryStatus } from '../refund-recovery-status'

type ApiGet = (
  url: string,
  config?: { signal?: AbortSignal }
) => Promise<{ data: unknown }>

const apiClient = api as unknown as { get: ApiGet }
const originalGet = apiClient.get
const queryClients: QueryClient[] = []

function renderStatus() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  return render(
    <QueryClientProvider client={queryClient}>
      <RefundRecoveryStatus />
    </QueryClientProvider>
  )
}

afterEach(() => {
  apiClient.get = originalGet
  for (const queryClient of queryClients) queryClient.clear()
  queryClients.length = 0
})

describe('wallet refund recovery status', () => {
  test('shows loading, account hold, outstanding amounts, and safe case progress', async () => {
    apiClient.get = async (url) => {
      expect(url).toBe('/api/growth/refund-recovery?p=1&page_size=20')
      return {
        data: {
          success: true,
          data: {
            hold: true,
            outstanding_quota: 500_000,
            outstanding_cash: [{ currency: 'JPY', amount: 1_250 }],
            page: 1,
            page_size: 20,
            total: 1,
            items: [
              {
                reference: 'RC-000123',
                kind: 'partial_refund',
                status: 'pending_review',
                stage: 'repayment_required',
                outstanding_quota: 500_000,
                outstanding_cash: [{ currency: 'JPY', amount: 1_250 }],
                created_at: 1_700_000_000,
              },
            ],
          },
        },
      }
    }

    renderStatus()

    expect(
      screen.getByRole('status', { name: 'Loading refund recovery status' })
    ).toBeInTheDocument()
    expect(
      await screen.findByText('API access temporarily paused')
    ).toBeVisible()
    expect(screen.getByText(formatQuota(500_000))).toBeVisible()
    expect(screen.getByText(/1,250/)).toBeVisible()
    expect(screen.getByText('RC-000123')).toBeVisible()
    expect(screen.getByText('Repayment required')).toBeVisible()
    expect(
      screen.getByText(
        'Contact support to arrange repayment and provide the case reference.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Contact support' })
    ).toHaveAttribute(
      'href',
      'https://docs.newapi.pro/support/community-interaction/'
    )
    expect(
      screen.getByRole('button', { name: 'Copy case reference' })
    ).toBeEnabled()
    expect(
      screen.getByText(`Opened ${formatTimestampToDate(1_700_000_000)}`)
    ).toBeVisible()
  })

  test('renders nothing without a hold, outstanding debt, or pending case', async () => {
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          hold: false,
          outstanding_quota: 0,
          outstanding_cash: [],
          page: 1,
          page_size: 20,
          total: 1,
          items: [
            {
              reference: 'RC-000001',
              kind: 'full_refund',
              status: 'resolved',
              stage: 'resolved',
              outstanding_quota: 0,
              outstanding_cash: [],
              created_at: 1_700_000_000,
              resolved_at: 1_700_000_100,
            },
          ],
        },
      },
    })

    const view = renderStatus()

    await waitFor(() => expect(view.container).toBeEmptyDOMElement())
  })

  test('shows an inline error and retries the recovery request', async () => {
    let requestCount = 0
    apiClient.get = async () => {
      requestCount += 1
      if (requestCount === 1) throw new Error('network unavailable')
      return {
        data: {
          success: true,
          data: {
            hold: false,
            outstanding_quota: 0,
            outstanding_cash: [],
            page: 1,
            page_size: 20,
            total: 0,
            items: [],
          },
        },
      }
    }
    const user = userEvent.setup()
    const view = renderStatus()

    expect(
      await screen.findByText('Unable to load refund recovery status')
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    await waitFor(() => expect(requestCount).toBe(2))
    await waitFor(() => expect(view.container).toBeEmptyDOMElement())
  })

  test('never renders provider, order, external, administrator, or other-user details', async () => {
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          hold: true,
          outstanding_quota: 0,
          outstanding_cash: [],
          page: 1,
          page_size: 20,
          total: 1,
          items: [
            {
              reference: 'RC-000456',
              kind: 'dispute',
              status: 'pending_review',
              stage: 'under_review',
              outstanding_quota: 0,
              outstanding_cash: [],
              created_at: 1_700_000_000,
              provider: 'PROVIDER-SECRET',
              order_no: 'ORDER-SECRET',
              external_ref: 'EXTERNAL-SECRET',
              initiator_id: 'ADMIN-SECRET',
              obligations: [{ user: 'OTHER-USER-SECRET' }],
            },
          ],
        },
      },
    })

    renderStatus()

    expect(await screen.findByText('RC-000456')).toBeVisible()
    expect(screen.queryByText('PROVIDER-SECRET')).not.toBeInTheDocument()
    expect(screen.queryByText('ORDER-SECRET')).not.toBeInTheDocument()
    expect(screen.queryByText('EXTERNAL-SECRET')).not.toBeInTheDocument()
    expect(screen.queryByText('ADMIN-SECRET')).not.toBeInTheDocument()
    expect(screen.queryByText('OTHER-USER-SECRET')).not.toBeInTheDocument()
  })

  test('loads later refund cases instead of silently hiding them', async () => {
    const requestedUrls: string[] = []
    apiClient.get = async (url) => {
      requestedUrls.push(url)
      const secondPage = url.includes('p=2')
      return {
        data: {
          success: true,
          data: {
            hold: true,
            outstanding_quota: 0,
            outstanding_cash: [],
            page: secondPage ? 2 : 1,
            page_size: 20,
            total: 21,
            items: [
              {
                reference: secondPage ? 'RC-000021' : 'RC-000020',
                kind: 'full_refund',
                status: 'pending_review',
                stage: 'under_review',
                outstanding_quota: 0,
                outstanding_cash: [],
                created_at: 1_700_000_000,
              },
            ],
          },
        },
      }
    }
    const user = userEvent.setup()

    renderStatus()

    expect(await screen.findByText('RC-000020')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Next page' }))

    expect(await screen.findByText('RC-000021')).toBeVisible()
    expect(requestedUrls).toEqual([
      '/api/growth/refund-recovery?p=1&page_size=20',
      '/api/growth/refund-recovery?p=2&page_size=20',
    ])
  })
})
