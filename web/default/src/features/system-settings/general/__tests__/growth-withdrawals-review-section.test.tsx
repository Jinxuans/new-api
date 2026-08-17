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
import { afterEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { GrowthWithdrawalsReviewSection } from '../growth-withdrawals-review-section'

type ApiGet = (
  url: string,
  config?: unknown
) => Promise<{ data: Record<string, unknown> }>
type ApiPost = (
  url: string,
  data?: unknown
) => Promise<{ data: Record<string, unknown> }>

const apiClient = api as unknown as { get: ApiGet; post: ApiPost }
const originalGet = apiClient.get
const originalPost = apiClient.post

function expectWithdrawalDetails(
  dialog: HTMLElement,
  details: {
    id: number
    amount: string
    method: string
    account: string
    reference: string
  }
) {
  expect(within(dialog).getByText(`Withdrawal: #${details.id}`)).toBeVisible()
  expect(within(dialog).getByText(`Amount: ${details.amount}`)).toBeVisible()
  expect(
    within(dialog).getByText(`Payout method: ${details.method}`)
  ).toBeVisible()
  expect(
    within(dialog).getByText(`Payout account: ${details.account}`)
  ).toBeVisible()
  expect(
    within(dialog).getByText(`Trade no: ${details.reference}`)
  ).toBeVisible()
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
})

describe('withdrawal review actions', () => {
  test('requires an approved withdrawal to enter processing before it can be paid', async () => {
    const user = userEvent.setup()
    const calls: Array<{ url: string; data?: unknown }> = []
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 9,
              user_id: 42,
              currency: 'CNY',
              gross_amount_cents: 1200,
              fee_amount_cents: 0,
              tax_amount_cents: 0,
              net_amount_cents: 1200,
              status: 'approved',
              payout_method: 'bank',
              payout_account_snapshot: JSON.stringify({
                payout_account: 'ending 1234',
              }),
              applied_at: 1,
            },
          ],
        },
      },
    })
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      return { data: { success: true } }
    }

    render(<GrowthWithdrawalsReviewSection />)

    expect(
      await screen.findByRole('button', { name: 'Start payout #9' })
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Reject request #9' })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: /Confirm payout completed/ })
    ).not.toBeInTheDocument()

    await user.type(
      screen.getByRole('textbox', { name: 'Trade no #9' }),
      'bank-ref-42'
    )
    await user.click(screen.getByRole('button', { name: 'Start payout #9' }))

    const confirmation = screen.getByRole('alertdialog')
    expectWithdrawalDetails(confirmation, {
      id: 9,
      amount: 'CNY 12.00',
      method: 'bank',
      account: 'ending 1234',
      reference: 'bank-ref-42',
    })
    expect(
      within(confirmation).getByText(/moves CNY 12.00 into processing/)
    ).toBeVisible()
    await user.click(
      within(confirmation).getByRole('button', { name: 'Start payout #9' })
    )

    expect(calls).toContainEqual({
      url: '/api/growth/admin/withdrawals/9/initiate',
      data: { trade_no: 'bank-ref-42', review_note: '' },
    })
  })

  test('keeps payout failure reasons separate from notes entered in earlier states', async () => {
    const user = userEvent.setup()
    const calls: Array<{ url: string; data?: unknown }> = []
    let currentStatus = 'pending_review'
    let tradeNo: string | undefined
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 12,
              user_id: 45,
              currency: 'CNY',
              gross_amount_cents: 3200,
              fee_amount_cents: 0,
              tax_amount_cents: 0,
              net_amount_cents: 3200,
              status: currentStatus,
              payout_method: 'bank',
              payout_account_snapshot: JSON.stringify({
                payout_account: 'ending 9012',
              }),
              trade_no: tradeNo,
              applied_at: 1,
            },
          ],
        },
      },
    })
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      if (url.endsWith('/approve')) {
        currentStatus = 'approved'
      } else if (url.endsWith('/initiate')) {
        currentStatus = 'processing'
        tradeNo = 'bank-ref-12'
      } else if (url.endsWith('/failed')) {
        currentStatus = 'failed'
      }
      return { data: { success: true } }
    }

    render(<GrowthWithdrawalsReviewSection />)

    await user.type(
      await screen.findByRole('textbox', { name: 'Review note #12' }),
      'approved note'
    )
    await user.click(
      screen.getByRole('button', { name: 'Approve request #12' })
    )
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Approve request #12',
      })
    )

    expect(
      await screen.findByRole('button', { name: 'Start payout #12' })
    ).toBeVisible()
    expect(calls).toContainEqual({
      url: '/api/growth/admin/withdrawals/12/approve',
      data: { review_note: 'approved note' },
    })

    await user.type(
      screen.getByRole('textbox', { name: 'Trade no #12' }),
      'bank-ref-12'
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Review note #12' }),
      'started note'
    )
    await user.click(screen.getByRole('button', { name: 'Start payout #12' }))
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Start payout #12',
      })
    )

    expect(
      await screen.findByRole('button', { name: 'Mark payout failed #12' })
    ).toBeVisible()
    expect(calls).toContainEqual({
      url: '/api/growth/admin/withdrawals/12/initiate',
      data: {
        trade_no: 'bank-ref-12',
        review_note: 'started note',
      },
    })

    const failureReason = screen.getByRole('textbox', {
      name: 'Failure reason #12',
    })
    expect(failureReason).toHaveValue('')
    await user.click(
      screen.getByRole('button', { name: 'Mark payout failed #12' })
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

    await user.type(failureReason, 'bank rejected')
    await user.click(
      screen.getByRole('button', { name: 'Mark payout failed #12' })
    )
    const failureConfirmation = screen.getByRole('alertdialog')
    expect(
      within(failureConfirmation).getByText('Failure reason: bank rejected')
    ).toBeVisible()
    await user.click(
      within(failureConfirmation).getByRole('button', {
        name: 'Mark payout failed #12',
      })
    )

    await waitFor(() => {
      expect(calls).toContainEqual({
        url: '/api/growth/admin/withdrawals/12/failed',
        data: {
          trade_no: 'bank-ref-12',
          failure_note: 'bank rejected',
        },
      })
    })
  })

  test('shows the immutable payout timeline and blocks rejection after payout starts', async () => {
    const user = userEvent.setup()
    const calls: Array<{ url: string; data?: unknown }> = []
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 10,
              user_id: 43,
              currency: 'CNY',
              gross_amount_cents: 1800,
              fee_amount_cents: 0,
              tax_amount_cents: 0,
              net_amount_cents: 1800,
              status: 'processing',
              payout_method: 'bank',
              payout_account_snapshot: JSON.stringify({
                payout_account: 'ending 5678',
              }),
              trade_no: 'bank-ref-43',
              payout_initiated_at: 3,
              applied_at: 1,
              operations: [
                {
                  id: 1,
                  action: 'approved',
                  actor_type: 'admin',
                  actor_id: 7,
                  created_at: 2,
                },
                {
                  id: 2,
                  action: 'payout_initiated',
                  actor_type: 'admin',
                  actor_id: 7,
                  external_reference: 'bank-ref-43',
                  created_at: 3,
                },
              ],
            },
          ],
        },
      },
    })
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      return { data: { success: true } }
    }

    render(<GrowthWithdrawalsReviewSection />)

    expect(
      await screen.findByRole('button', {
        name: 'Confirm payout completed #10',
      })
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Mark payout failed #10' })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Reject request' })
    ).not.toBeInTheDocument()

    await user.click(screen.getByText('2 recorded steps'))
    const payoutStep = screen.getByText('Payout started').closest('li')
    expect(payoutStep).not.toBeNull()
    expect(
      within(payoutStep as HTMLElement).getByText(/bank-ref-43/)
    ).toBeVisible()

    await user.click(
      screen.getByRole('button', { name: 'Confirm payout completed #10' })
    )
    expectWithdrawalDetails(screen.getByRole('alertdialog'), {
      id: 10,
      amount: 'CNY 18.00',
      method: 'bank',
      account: 'ending 5678',
      reference: 'bank-ref-43',
    })
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Cancel',
      })
    )

    await user.click(
      screen.getByRole('button', { name: 'Mark payout failed #10' })
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

    await user.type(
      screen.getByRole('textbox', { name: 'Failure reason #10' }),
      'The receiving bank rejected the transfer.'
    )
    await user.click(
      screen.getByRole('button', { name: 'Mark payout failed #10' })
    )
    const failureConfirmation = screen.getByRole('alertdialog')
    expectWithdrawalDetails(failureConfirmation, {
      id: 10,
      amount: 'CNY 18.00',
      method: 'bank',
      account: 'ending 5678',
      reference: 'bank-ref-43',
    })
    expect(
      within(failureConfirmation).getByText(
        'Failure reason: The receiving bank rejected the transfer.'
      )
    ).toBeVisible()
    expect(
      within(failureConfirmation).getByText(
        /returns CNY 18.00 from reserved commission to available commission/
      )
    ).toBeVisible()
    await user.click(
      within(failureConfirmation).getByRole('button', {
        name: 'Mark payout failed #10',
      })
    )
    await waitFor(() => {
      expect(calls).toContainEqual({
        url: '/api/growth/admin/withdrawals/10/failed',
        data: {
          trade_no: 'bank-ref-43',
          failure_note: 'The receiving bank rejected the transfer.',
        },
      })
    })
  })

  test('labels imported and reconstructed timeline entries without a fake System actor', async () => {
    const user = userEvent.setup()
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 11,
              user_id: 44,
              currency: 'CNY',
              gross_amount_cents: 1800,
              fee_amount_cents: 0,
              tax_amount_cents: 0,
              net_amount_cents: 1800,
              status: 'paid',
              payout_method: 'bank',
              applied_at: 1,
              operations: [
                {
                  id: 1,
                  action: 'submitted',
                  actor_type: 'user',
                  actor_id: 44,
                  reconstructed: true,
                  created_at: 1,
                },
                {
                  id: 2,
                  action: 'paid',
                  actor_type: 'legacy',
                  actor_id: 0,
                  reconstructed: true,
                  created_at: 2,
                },
                {
                  id: 3,
                  action: 'approved',
                  actor_type: 'admin',
                  actor_id: 7,
                  reconstructed: false,
                  created_at: 3,
                },
              ],
            },
          ],
        },
      },
    })

    render(<GrowthWithdrawalsReviewSection />)

    await user.click(await screen.findByText('3 recorded steps'))

    const reconstructedStep = screen
      .getByText('Withdrawal submitted')
      .closest('li')
    const historicalImportStep = screen
      .getByText('Payout completed')
      .closest('li')
    const recordedAdminStep = screen
      .getByText('Withdrawal approved')
      .closest('li')

    expect(reconstructedStep).not.toBeNull()
    expect(historicalImportStep).not.toBeNull()
    expect(recordedAdminStep).not.toBeNull()
    expect(
      within(reconstructedStep as HTMLElement).getByText(/Reconstructed record/)
    ).toBeVisible()
    expect(
      within(historicalImportStep as HTMLElement).getByText(/Historical import/)
    ).toBeVisible()
    expect(
      within(recordedAdminStep as HTMLElement).getByText(/Admin #7/)
    ).toBeVisible()
    expect(screen.queryByText(/System #0/)).not.toBeInTheDocument()
  })

  test('requires explicit confirmation before approving or rejecting a withdrawal', async () => {
    const user = userEvent.setup()
    const calls: Array<{ url: string; data?: unknown }> = []
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 11,
              user_id: 44,
              currency: 'CNY',
              gross_amount_cents: 2500,
              fee_amount_cents: 0,
              tax_amount_cents: 0,
              net_amount_cents: 2500,
              status: 'pending_review',
              payout_method: 'alipay',
              payout_account_snapshot: JSON.stringify({
                payout_account: 'user@example.com',
              }),
              applied_at: 1,
            },
          ],
        },
      },
    })
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      return { data: { success: true } }
    }

    render(<GrowthWithdrawalsReviewSection />)

    await user.click(
      await screen.findByRole('button', { name: 'Approve request #11' })
    )
    const approval = screen.getByRole('alertdialog')
    expectWithdrawalDetails(approval, {
      id: 11,
      amount: 'CNY 25.00',
      method: 'alipay',
      account: 'user@example.com',
      reference: '-',
    })
    expect(calls).toHaveLength(0)
    await user.click(within(approval).getByRole('button', { name: 'Cancel' }))

    await user.type(
      screen.getByRole('textbox', { name: 'Review note #11' }),
      'Account information did not pass review.'
    )
    await user.click(screen.getByRole('button', { name: 'Reject request #11' }))
    const rejection = screen.getByRole('alertdialog')
    expectWithdrawalDetails(rejection, {
      id: 11,
      amount: 'CNY 25.00',
      method: 'alipay',
      account: 'user@example.com',
      reference: '-',
    })
    expect(
      within(rejection).getByText(
        'Review note: Account information did not pass review.'
      )
    ).toBeVisible()
    expect(
      within(rejection).getByText(
        /returns CNY 25.00 from reserved commission to available commission/
      )
    ).toBeVisible()
    expect(calls).toHaveLength(0)

    await user.click(
      within(rejection).getByRole('button', { name: 'Reject request #11' })
    )
    await waitFor(() => {
      expect(calls).toContainEqual({
        url: '/api/growth/admin/withdrawals/11/reject',
        data: {
          review_note: 'Account information did not pass review.',
        },
      })
    })
  })

  test('disables stale withdrawal actions while a new filter loads', async () => {
    let resolveApproved:
      | ((value: { data: Record<string, unknown> }) => void)
      | undefined
    let requestCount = 0
    apiClient.get = async () => {
      requestCount += 1
      if (requestCount > 1) {
        return new Promise((resolve) => {
          resolveApproved = resolve
        })
      }
      return {
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                id: 13,
                user_id: 45,
                currency: 'CNY',
                net_amount_cents: 3000,
                status: 'pending_review',
                payout_method: 'bank',
                applied_at: 1,
              },
            ],
          },
        },
      }
    }
    const user = userEvent.setup()

    render(<GrowthWithdrawalsReviewSection />)

    const approve = await screen.findByRole('button', {
      name: 'Approve request #13',
    })
    await user.click(screen.getByLabelText('Withdrawal status'))
    await user.click(
      await screen.findByRole('option', { name: 'Approved awaiting payout' })
    )

    await waitFor(() => expect(requestCount).toBe(2))
    expect(approve).toBeDisabled()
    expect(screen.getByRole('table')).toHaveAttribute('aria-busy', 'true')

    resolveApproved?.({
      data: { success: true, data: { total: 0, items: [] } },
    })
    expect(
      await screen.findByText('No withdrawal requests to review')
    ).toBeVisible()
  })

  test('handles a rejected withdrawal request promise without closing the dialog', async () => {
    apiClient.get = async () => ({
      data: {
        success: true,
        data: {
          total: 1,
          items: [
            {
              id: 14,
              user_id: 46,
              currency: 'CNY',
              net_amount_cents: 3200,
              status: 'pending_review',
              payout_method: 'bank',
              applied_at: 1,
            },
          ],
        },
      },
    })
    apiClient.post = async () => {
      throw new Error('network unavailable')
    }
    const user = userEvent.setup()

    render(<GrowthWithdrawalsReviewSection />)

    await user.click(
      await screen.findByRole('button', { name: 'Approve request #14' })
    )
    const dialog = screen.getByRole('alertdialog')
    const confirm = within(dialog).getByRole('button', {
      name: 'Approve request #14',
    })
    await user.click(confirm)

    await waitFor(() => expect(confirm).toBeEnabled())
    expect(dialog).toBeVisible()
  })

  test('shows a recoverable error instead of an empty withdrawal list', async () => {
    apiClient.get = async () => {
      throw new Error('network unavailable')
    }

    render(<GrowthWithdrawalsReviewSection />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'No data available'
    )
    expect(screen.getByText('Refresh the list and try again.')).toBeVisible()
    expect(
      screen.queryByText('No withdrawal requests to review')
    ).not.toBeInTheDocument()
  })
})
