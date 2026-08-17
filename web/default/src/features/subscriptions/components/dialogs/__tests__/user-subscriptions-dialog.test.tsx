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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { UserSubscriptionsDialog } from '../user-subscriptions-dialog'

type ApiMethod = (
  url: string,
  payload?: unknown
) => Promise<{ data: Record<string, unknown> }>

const apiClient = api as unknown as {
  get: ApiMethod
  post: ApiMethod
  delete: ApiMethod
}
const originalGet = apiClient.get
const originalPost = apiClient.post
const originalDelete = apiClient.delete

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  apiClient.delete = originalDelete
})

describe('user subscription evidence retention', () => {
  test('requires idempotent invalidation evidence without exposing permanent deletion', async () => {
    const posts: Array<{ url: string; payload?: unknown }> = []
    const deletes: string[] = []
    apiClient.get = async (url) => {
      if (url === '/api/subscription/admin/plans') {
        return { data: { success: true, data: [] } }
      }
      if (url === '/api/subscription/admin/users/42/subscriptions') {
        return {
          data: {
            success: true,
            data: [
              {
                subscription: {
                  id: 7,
                  user_id: 42,
                  plan_id: 3,
                  status: 'active',
                  source: 'admin',
                  start_time: 1,
                  end_time: 4_000_000_000,
                  amount_total: 1_000_000,
                  amount_used: 0,
                },
              },
            ],
          },
        }
      }
      throw new Error(`Unexpected GET ${url}`)
    }
    apiClient.post = async (url, payload) => {
      posts.push({ url, payload })
      return { data: { success: true } }
    }
    apiClient.delete = async (url) => {
      deletes.push(url)
      return { data: { success: true } }
    }
    const onSuccess = vi.fn()
    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 42, username: 'customer' }}
        onSuccess={onSuccess}
      />
    )
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Actions' }))
    expect(screen.getByRole('menuitem', { name: /Invalidate/ })).toBeEnabled()
    expect(screen.queryByRole('menuitem', { name: /Delete/ })).toBeNull()

    await user.click(screen.getByRole('menuitem', { name: /Invalidate/ }))
    const dialog = screen.getByRole('alertdialog')
    const confirm = within(dialog).getByRole('button', { name: 'Invalidate' })
    expect(confirm).toBeDisabled()
    await user.type(
      within(dialog).getByPlaceholderText(
        'Explain why this subscription is invalidated.'
      ),
      'Verified entitlement correction'
    )
    expect(confirm).toBeEnabled()
    await user.click(confirm)

    await waitFor(() => {
      expect(posts).toEqual([
        {
          url: '/api/subscription/admin/user_subscriptions/7/invalidate',
          payload: {
            reason: 'Verified entitlement correction',
            idempotency_key: expect.any(String),
          },
        },
      ])
    })
    expect(deletes).toEqual([])
    expect(onSuccess).toHaveBeenCalledOnce()
  })

  test('requires a reason and sends idempotent evidence for a manual grant', async () => {
    const posts: Array<{ url: string; payload?: unknown }> = []
    apiClient.get = async (url) => {
      if (url === '/api/subscription/admin/plans') {
        return {
          data: {
            success: true,
            data: [
              {
                plan: {
                  id: 3,
                  title: 'Pro',
                  price_amount: 10,
                },
              },
            ],
          },
        }
      }
      if (url === '/api/subscription/admin/users/42/subscriptions') {
        return { data: { success: true, data: [] } }
      }
      throw new Error(`Unexpected GET ${url}`)
    }
    apiClient.post = async (url, payload) => {
      posts.push({ url, payload })
      return { data: { success: true, data: {} } }
    }

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 42, username: 'customer' }}
      />
    )
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('combobox', {
        name: /Select subscription plan/i,
      })
    )
    await user.click(screen.getByRole('option', { name: /Pro/ }))
    await user.click(screen.getByRole('button', { name: 'Grant subscription' }))

    const dialog = screen.getByRole('alertdialog')
    const confirm = within(dialog).getByRole('button', {
      name: 'Grant subscription',
    })
    expect(confirm).toBeDisabled()
    await user.type(
      within(dialog).getByPlaceholderText(
        'Explain why this subscription is granted without payment.'
      ),
      'Verified service recovery'
    )
    expect(confirm).toBeEnabled()
    await user.click(confirm)

    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]?.url).toBe('/api/subscription/admin/users/42/subscriptions')
    expect(posts[0]?.payload).toEqual({
      plan_id: 3,
      reason: 'Verified service recovery',
      idempotency_key: expect.any(String),
    })
  })

  test('requires a reason and records reset intent with an idempotency key', async () => {
    const posts: Array<{ url: string; payload?: unknown }> = []
    apiClient.get = async (url) => {
      if (url === '/api/subscription/admin/plans') {
        return {
          data: {
            success: true,
            data: [{ plan: { id: 3, title: 'Pro', price_amount: 10 } }],
          },
        }
      }
      if (url === '/api/subscription/admin/users/42/subscriptions') {
        return {
          data: {
            success: true,
            data: [
              {
                subscription: {
                  id: 7,
                  user_id: 42,
                  plan_id: 3,
                  status: 'active',
                  source: 'admin',
                  start_time: 1,
                  end_time: 4_000_000_000,
                  amount_total: 1_000_000,
                  amount_used: 300,
                },
              },
            ],
          },
        }
      }
      throw new Error(`Unexpected GET ${url}`)
    }
    apiClient.post = async (url, payload) => {
      posts.push({ url, payload })
      return {
        data: { success: true, data: { reset_count: 1 } },
      }
    }

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 42, username: 'customer' }}
      />
    )
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Actions' }))
    await user.click(screen.getByRole('menuitem', { name: /Reset quota/ }))
    const dialog = screen.getByRole('alertdialog')
    const confirm = within(dialog).getByRole('button', { name: 'Reset quota' })
    expect(confirm).toBeDisabled()
    await user.type(
      within(dialog).getByPlaceholderText(
        'Explain why this quota reset is necessary.'
      ),
      'Customer approved reset'
    )
    expect(confirm).toBeEnabled()
    await user.click(confirm)

    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]?.url).toBe(
      '/api/subscription/admin/users/42/subscriptions/reset'
    )
    expect(posts[0]?.payload).toEqual({
      plan_id: 3,
      advance_reset_time: true,
      reason: 'Customer approved reset',
      idempotency_key: expect.any(String),
    })
  })
})
