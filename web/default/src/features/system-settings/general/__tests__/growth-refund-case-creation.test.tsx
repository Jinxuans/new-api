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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

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

function installEmptyCaseList() {
  apiClient.get = async () => ({
    data: { success: true, data: { total: 0, items: [] } },
  })
}

async function openCreateDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    await screen.findByRole('button', { name: 'Create refund case' })
  )
  return screen.getByRole('dialog')
}

async function fillRequiredFields(
  user: ReturnType<typeof userEvent.setup>,
  dialog: HTMLElement
) {
  await user.type(within(dialog).getByLabelText('Local order number'), 'T-1')
  await user.type(within(dialog).getByLabelText('External reference'), 'RF-1')
  await user.type(within(dialog).getByLabelText('Currency'), 'cny')
  await user.type(
    within(dialog).getByLabelText('Evidence and reason'),
    'Verified against the provider settlement report.'
  )
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  toastMocks.error.mockReset()
  toastMocks.success.mockReset()
  useAuthStore.getState().auth.setUser(null)
})

describe('Root refund case creation', () => {
  beforeEach(() => {
    installEmptyCaseList()
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })
  })

  test('converts a partial amount to minor units and refreshes the case list', async () => {
    const requests: Array<Record<string, unknown>> = []
    let listRequests = 0
    apiClient.get = async () => {
      listRequests += 1
      return { data: { success: true, data: { total: 0, items: [] } } }
    }
    apiClient.post = async (url, body) => {
      expect(url).toBe('/api/growth/admin/refund-cases')
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: { id: 1 } } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    const dialog = await openCreateDialog(user)
    await fillRequiredFields(user, dialog)

    await user.click(within(dialog).getByLabelText('Refund kind'))
    await user.click(
      await screen.findByRole('option', { name: 'Partial refund' })
    )
    await user.type(within(dialog).getByLabelText('Refund amount'), '12.34')
    await user.click(within(dialog).getByLabelText('Partial amount mode'))
    await user.click(
      await screen.findByRole('option', { name: 'Cumulative refunded total' })
    )
    await user.click(
      within(dialog).getByRole('button', { name: 'Create refund case' })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toEqual({
      idempotency_key: expect.any(String),
      trade_no: 'T-1',
      external_ref: 'RF-1',
      intake_source: 'provider_refund',
      kind: 'partial_refund',
      refunded_amount_minor: 1234,
      currency: 'CNY',
      amount_is_cumulative: true,
      remark: 'Verified against the provider settlement report.',
    })
    await waitFor(() => expect(listRequests).toBe(2))
    expect(toastMocks.success).toHaveBeenCalledWith('Refund case created')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  test('reuses the idempotency key when an unchanged request is retried', async () => {
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      if (requests.length === 1) {
        throw new Error('network unavailable')
      }
      return { data: { success: true, data: { id: 1 } } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    const dialog = await openCreateDialog(user)
    await fillRequiredFields(user, dialog)

    await user.click(
      within(dialog).getByRole('button', { name: 'Create refund case' })
    )
    await waitFor(() => expect(toastMocks.error).toHaveBeenCalled())
    await user.click(
      within(dialog).getByRole('button', { name: 'Create refund case' })
    )

    await waitFor(() => expect(requests).toHaveLength(2))
    expect(requests[1]?.idempotency_key).toBe(requests[0]?.idempotency_key)
  })

  test('forces a chargeback to use the dispute kind', async () => {
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: { id: 1 } } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    const dialog = await openCreateDialog(user)
    await fillRequiredFields(user, dialog)
    await user.click(within(dialog).getByLabelText('Intake source'))
    await user.click(await screen.findByRole('option', { name: 'Chargeback' }))

    expect(within(dialog).getByLabelText('Refund kind')).toBeDisabled()
    expect(within(dialog).getByLabelText('Refund kind')).toHaveTextContent(
      'Dispute'
    )
    await user.click(
      within(dialog).getByRole('button', { name: 'Create refund case' })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      intake_source: 'chargeback',
      kind: 'dispute',
      refunded_amount_minor: 0,
      amount_is_cumulative: false,
    })
  })

  test('does not expose case creation to a non-Root administrator', async () => {
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'admin',
      role: ROLE.ADMIN,
    })

    render(<GrowthRefundCasesSection />)

    expect(await screen.findByText('No refund recovery cases')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Create refund case' })
    ).not.toBeInTheDocument()
  })
})
