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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { GrowthRefundCasesSection } from '../growth-refund-cases-section'

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: toastMocks }))

type ApiGet = (
  url: string,
  config?: unknown
) => Promise<{ data: Record<string, unknown> }>
type ApiPost = (
  url: string,
  body?: unknown
) => Promise<{ data: Record<string, unknown> }>

const apiClient = api as unknown as { get: ApiGet; post: ApiPost }
const originalGet = apiClient.get
const originalPost = apiClient.post

function installRefundCaseFixture() {
  apiClient.get = async () => ({
    data: {
      success: true,
      data: {
        total: 1,
        items: [
          {
            id: 1,
            provider: 'stripe',
            trade_no: 'order-1',
            refund_trade_no: 'refund-1',
            kind: 'partial_refund',
            paid_amount_minor: 2000,
            refunded_amount_minor: 1234,
            currency: 'not-a-currency',
            status: 'pending_review',
            reason: 'manual recovery required',
            created_at: 1,
          },
        ],
      },
    },
  })
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  toastMocks.error.mockReset()
  toastMocks.success.mockReset()
})

describe('refund case review', () => {
  test('falls back for an invalid currency and explains that closing does not adjust balances', async () => {
    installRefundCaseFixture()

    render(<GrowthRefundCasesSection />)

    expect(await screen.findByText('NOT-A-CURRENCY 12.34')).toBeVisible()
    expect(
      screen.getByText(
        'Marking a case resolved only records the completed manual review. It does not change balances or commissions.'
      )
    ).toBeVisible()
    expect(screen.getByLabelText('Review note for refund 1')).toHaveAttribute(
      'maxlength',
      '1000'
    )
  })

  test('keeps the confirmation open and shows a toast when resolving fails', async () => {
    installRefundCaseFixture()
    apiClient.post = async () => {
      throw new Error('network unavailable')
    }

    render(<GrowthRefundCasesSection />)

    const note = await screen.findByLabelText('Review note for refund 1')
    fireEvent.change(note, { target: { value: 'quota adjusted manually' } })
    fireEvent.click(screen.getByRole('button', { name: 'Mark resolved' }))
    fireEvent.click(
      await screen.findByRole('button', {
        name: 'Confirm manual resolution',
      })
    )

    await waitFor(() => {
      expect(toastMocks.error).toHaveBeenCalledWith('network unavailable')
    })
    expect(screen.getByText('Mark this refund case resolved?')).toBeVisible()
  })

  test('keeps the latest filter result when an older request finishes last', async () => {
    let resolveFirstRequest:
      | ((value: { data: Record<string, unknown> }) => void)
      | undefined
    let requestCount = 0
    apiClient.get = async () => {
      requestCount += 1
      if (requestCount === 1) {
        return new Promise((resolve) => {
          resolveFirstRequest = resolve
        })
      }
      return {
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                id: 2,
                provider: 'stripe',
                trade_no: 'order-latest',
                refund_trade_no: 'refund-latest',
                kind: 'full_refund',
                paid_amount_minor: 2000,
                refunded_amount_minor: 2000,
                currency: 'CNY',
                status: 'resolved',
                reason: 'latest response',
                review_note: 'completed',
                created_at: 2,
              },
            ],
          },
        },
      }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)

    await user.click(screen.getByLabelText('Refund case status'))
    await user.click(await screen.findByRole('option', { name: 'Resolved' }))
    expect(await screen.findByText(/refund-latest/)).toBeVisible()

    await act(async () => {
      resolveFirstRequest?.({
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                id: 1,
                provider: 'stripe',
                trade_no: 'order-old',
                refund_trade_no: 'refund-old',
                kind: 'partial_refund',
                paid_amount_minor: 2000,
                refunded_amount_minor: 1000,
                currency: 'CNY',
                status: 'pending_review',
                reason: 'stale response',
                created_at: 1,
              },
            ],
          },
        },
      })
      await Promise.resolve()
    })

    expect(screen.getByText(/refund-latest/)).toBeVisible()
    expect(screen.queryByText(/refund-old/)).not.toBeInTheDocument()
  })
})
