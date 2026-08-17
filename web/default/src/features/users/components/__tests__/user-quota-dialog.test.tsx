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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { parseQuotaFromDollars } from '@/lib/format'

import { UserQuotaDialog } from '../user-quota-dialog'

type ApiPost = (
  url: string,
  payload: unknown
) => Promise<{ data: { success: boolean; message?: string } }>

const apiClient = api as unknown as { post: ApiPost }
const originalPost = apiClient.post

function renderDialog(currentQuota = 1_000_000) {
  const onOpenChange = vi.fn()
  const onSuccess = vi.fn()
  render(
    <UserQuotaDialog
      open
      onOpenChange={onOpenChange}
      userId={42}
      currentQuota={currentQuota}
      onSuccess={onSuccess}
    />
  )
  return { onOpenChange, onSuccess }
}

afterEach(() => {
  apiClient.post = originalPost
})

describe('administrator quota adjustment', () => {
  test('requires a finite valid amount and an audit reason before submitting', async () => {
    const requests: Array<{ url: string; payload: Record<string, unknown> }> =
      []
    apiClient.post = async (url, payload) => {
      requests.push({ url, payload: payload as Record<string, unknown> })
      return { data: { success: true } }
    }
    const { onOpenChange, onSuccess } = renderDialog()
    const user = userEvent.setup()
    const confirm = screen.getByRole('button', { name: 'Confirm' })

    expect(confirm).toBeDisabled()
    await user.type(screen.getByLabelText(/Amount/), '2')
    expect(confirm).toBeDisabled()

    await user.type(screen.getByLabelText('Reason'), '  Manual correction  ')
    expect(confirm).toBeEnabled()
    await user.click(confirm)

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toEqual({
      url: '/api/user/manage',
      payload: {
        id: 42,
        action: 'add_quota',
        mode: 'add',
        value: parseQuotaFromDollars(2),
        remark: 'Manual correction',
        idempotency_key: expect.any(String),
      },
    })
    expect(requests[0]?.payload.idempotency_key).not.toBe('')
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(onSuccess).toHaveBeenCalledOnce()
  })

  test('rejects negative and unchanged overrides while allowing zero as a real override', async () => {
    apiClient.post = async () => ({ data: { success: false } })
    renderDialog(parseQuotaFromDollars(1))
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Override' }))
    const amount = screen.getByLabelText(/Amount/)
    const reason = screen.getByLabelText('Reason')
    const confirm = screen.getByRole('button', { name: 'Confirm' })
    await user.type(reason, 'Correct imported balance')

    await user.type(amount, '-1')
    expect(confirm).toBeDisabled()

    await user.clear(amount)
    await user.type(amount, '1')
    expect(confirm).toBeDisabled()

    await user.clear(amount)
    await user.type(amount, '0')
    expect(confirm).toBeEnabled()
  })

  test('reuses a key after failure and replaces it when the payload changes', async () => {
    const payloads: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, payload) => {
      payloads.push(payload as Record<string, unknown>)
      return { data: { success: false, message: 'retry' } }
    }
    renderDialog()
    const user = userEvent.setup()
    const amount = screen.getByLabelText(/Amount/)
    const reason = screen.getByLabelText('Reason')
    const confirm = screen.getByRole('button', { name: 'Confirm' })

    await user.type(amount, '2')
    await user.type(reason, 'First reason')
    await user.click(confirm)
    await waitFor(() => expect(payloads).toHaveLength(1))

    await user.click(confirm)
    await waitFor(() => expect(payloads).toHaveLength(2))
    expect(payloads[1]?.idempotency_key).toBe(payloads[0]?.idempotency_key)

    await user.type(reason, ' changed')
    await user.click(confirm)
    await waitFor(() => expect(payloads).toHaveLength(3))
    expect(payloads[2]?.idempotency_key).not.toBe(payloads[1]?.idempotency_key)
  })

  test('replaces the key after closing or completing an adjustment', async () => {
    const payloads: Array<Record<string, unknown>> = []
    let succeeds = false
    apiClient.post = async (_url, payload) => {
      payloads.push(payload as Record<string, unknown>)
      return { data: { success: succeeds } }
    }
    renderDialog()
    const user = userEvent.setup()
    const fillForm = async () => {
      await user.type(screen.getByLabelText(/Amount/), '2')
      await user.type(screen.getByLabelText('Reason'), 'Manual correction')
    }

    await fillForm()
    await user.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(payloads).toHaveLength(1))

    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await fillForm()
    succeeds = true
    await user.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(payloads).toHaveLength(2))
    expect(payloads[1]?.idempotency_key).not.toBe(payloads[0]?.idempotency_key)

    await fillForm()
    await user.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(payloads).toHaveLength(3))
    expect(payloads[2]?.idempotency_key).not.toBe(payloads[1]?.idempotency_key)
  })
})
