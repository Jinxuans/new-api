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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { parseQuotaFromDollars } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { GrowthRefundCasesSection } from '../growth-refund-cases-section'
import type { PromotionRefundCase } from '../growth-refund-types'

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

function refundCase(
  overrides: Partial<PromotionRefundCase> = {}
): PromotionRefundCase {
  return {
    id: 1,
    provider: 'stripe',
    trade_no: 'order-1',
    refund_trade_no: 'refund-1',
    kind: 'partial_refund',
    paid_amount_minor: 2000,
    refunded_amount_minor: 1234,
    currency: 'CNY',
    top_up_id: 11,
    user_id: 21,
    quota_amount: 1_000_000,
    wallet_debited_quota: 500_000,
    debt_created_quota: 500_000,
    cash_debt_created_minor: 0,
    status: 'pending_review',
    requires_root_review: false,
    responsibility_fingerprint: 'fingerprint-1',
    commission_reconciliation_required: false,
    reason: 'refund recovery is incomplete',
    created_at: 1,
    obligations: [
      {
        id: 31,
        refund_case_id: 1,
        user_id: 21,
        account: 'refund_debt',
        asset: 'quota',
        amount: 500_000,
        recovered_amount: 0,
        waived_amount: 0,
        source_type: 'top_ups',
        source_id: 11,
        status: 'open',
        created_at: 1,
        updated_at: 1,
      },
    ],
    actions: [],
    ...overrides,
  }
}

function installRefundCases(items: PromotionRefundCase[]) {
  apiClient.get = async () => ({
    data: {
      success: true,
      data: { total: items.length, items },
    },
  })
}

async function expandCase(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    await screen.findByRole('button', {
      name: /order-1/,
    })
  )
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  toastMocks.error.mockReset()
  toastMocks.success.mockReset()
  useAuthStore.getState().auth.setUser(null)
})

describe('refund recovery workflow', () => {
  beforeEach(() => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })
  })

  test('terminates the linked subscription before Root review can complete', async () => {
    installRefundCases([
      refundCase({
        requires_root_review: true,
        quota_amount: 0,
        wallet_debited_quota: 0,
        debt_created_quota: 0,
        reason: 'subscription payment refund requires subscription recovery',
        obligations: [],
        subscription_order_id: 51,
        user_subscription_id: 61,
        subscription_plan_id: 71,
        subscription_status: 'active',
        subscription_start_time: 100,
        subscription_end_time: 200,
        subscription_amount_total: 1_000_000,
        subscription_amount_used: 250_000,
      }),
    ])
    const requests: unknown[] = []
    apiClient.post = async (_url, body) => {
      requests.push(body)
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)

    expect(screen.getByText('Recovery summary')).toBeVisible()
    expect(screen.getByText('Refunded principal')).toBeVisible()
    expect(screen.getByText('Debited immediately')).toBeVisible()
    expect(screen.getByText('Cash recovery obligation')).toBeVisible()
    expect(screen.getByText('Root review is required')).toBeVisible()
    expect(screen.getByText('No quantified obligation')).toBeVisible()
    expect(screen.getByText('Subscription entitlement')).toBeVisible()
    expect(
      screen.getByText('Resolve the subscription entitlement first')
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Complete Root review' })
    ).not.toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Terminate subscription' })
    )
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Subscription ID')).toHaveValue(61)
    expect(within(dialog).getByLabelText('Subscription ID')).toBeDisabled()
    await user.click(
      within(dialog).getByRole('button', { name: 'Terminate subscription' })
    )
    expect(await screen.findByText('A reason is required.')).toBeVisible()
    await user.type(
      within(dialog).getByLabelText('Reason'),
      'Refund confirmed; terminate the matching entitlement.'
    )
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Terminate subscription',
      })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      action: 'revoke_subscription_entitlement',
      expected_responsibility_fingerprint: 'fingerprint-1',
      obligation_id: 0,
      user_subscription_id: 61,
      amount: 0,
      remark: 'Refund confirmed; terminate the matching entitlement.',
    })
  })

  test('defaults an active subscription assessment to cash recovery', async () => {
    installRefundCases([
      refundCase({
        requires_root_review: true,
        obligations: [],
        subscription_order_id: 51,
        user_subscription_id: 61,
        subscription_plan_id: 71,
        subscription_status: 'active',
        responsible_users: [
          {
            user_id: 21,
            username: 'subscriber',
            is_top_up_user: true,
            is_commission_recipient: false,
          },
        ],
      }),
    ])
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    await user.click(
      screen.getByRole('button', {
        name: 'Create cash recovery obligation',
      })
    )

    const dialog = screen.getByRole('dialog')
    expect(
      within(dialog).getByRole('radio', { name: 'Cash debt' })
    ).toBeChecked()
    expect(within(dialog).getByLabelText('Currency')).toHaveValue('CNY')
    await user.type(within(dialog).getByLabelText(/Recovery amount/), '12.34')
    await user.type(
      within(dialog).getByLabelText('Evidence reference'),
      'subscription-cash-evidence'
    )
    await user.type(
      within(dialog).getByLabelText('Assessment reason'),
      'Provider evidence confirms the cash amount to recover.'
    )
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Create recovery obligation',
      })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      action: 'define_manual_obligation',
      asset: 'cash',
      currency: 'CNY',
      amount: 1234,
    })
  })

  test.each(['cancelled', 'expired'] as const)(
    'records an already-%s subscription without describing a second termination',
    async (subscriptionStatus) => {
      installRefundCases([
        refundCase({
          requires_root_review: true,
          obligations: [],
          subscription_order_id: 51,
          user_subscription_id: 61,
          subscription_plan_id: 71,
          subscription_status: subscriptionStatus,
        }),
      ])
      const user = userEvent.setup()

      render(<GrowthRefundCasesSection />)
      await expandCase(user)

      expect(
        screen.queryByRole('button', { name: 'Terminate subscription' })
      ).not.toBeInTheDocument()
      await user.click(
        screen.getByRole('button', {
          name: 'Record subscription disposition',
        })
      )
      const dialog = screen.getByRole('dialog')
      expect(
        within(dialog).getByRole('heading', {
          name: 'Record subscription disposition',
        })
      ).toBeVisible()
      expect(
        within(dialog).queryByText('Subscription ends immediately')
      ).not.toBeInTheDocument()
    }
  )

  test('keeps an active subscription when its assessed cash obligation was fully recovered', async () => {
    const cashObligation = {
      ...refundCase().obligations[0],
      asset: 'cash' as const,
      currency: 'CNY',
      amount: 1234,
      recovered_amount: 1234,
      waived_amount: 0,
      source_type: 'top_ups',
      source_id: 11,
      status: 'recovered' as const,
    }
    installRefundCases([
      refundCase({
        requires_root_review: true,
        obligations: [cashObligation],
        subscription_order_id: 51,
        user_subscription_id: 61,
        subscription_plan_id: 71,
        subscription_status: 'active',
      }),
    ])
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)

    expect(
      screen.getByText(
        'The subscription stays active because the assessed cash obligation was fully recovered.'
      )
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Terminate subscription' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Complete Root review' })
    ).toBeEnabled()
  })

  test('requires Root to quarantine an unknown commission before completing review', async () => {
    installRefundCases([
      refundCase({
        requires_root_review: true,
        commission_ledger_id: 32,
        commission_ledger_status: 'legacy_unknown',
        commission_reconciliation_required: true,
        obligations: [],
      }),
    ])
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    expect(
      screen.getByText('Quarantine unknown commission first')
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Complete Root review' })
    ).not.toBeInTheDocument()
    await user.click(
      screen.getByRole('button', { name: 'Quarantine unknown commission' })
    )
    const dialog = screen.getByRole('dialog')
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Quarantine unknown commission',
      })
    )
    expect(
      await screen.findByText('External reference is required.')
    ).toBeVisible()

    await user.type(
      within(dialog).getByLabelText('External reference'),
      'provider-audit-unknown-1'
    )
    await user.type(
      within(dialog).getByLabelText('Reason'),
      'Provider audit confirms this legacy state cannot be reconstructed.'
    )
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Quarantine unknown commission',
      })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      action: 'quarantine_unknown_commission',
      obligation_id: 0,
      amount: 0,
      external_ref: 'provider-audit-unknown-1',
      remark:
        'Provider audit confirms this legacy state cannot be reconstructed.',
      commission_ledger_id: 32,
      commission_ledger_status: 'legacy_unknown',
    })
  })

  test('allows Root to quarantine a newly changed commission status after review completion', async () => {
    installRefundCases([
      refundCase({
        requires_root_review: false,
        commission_ledger_id: 33,
        commission_ledger_status: '',
        commission_reconciliation_required: true,
        obligations: [],
      }),
    ])
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    expect(
      screen.getByRole('button', { name: 'Quarantine unknown commission' })
    ).toBeEnabled()

    await user.click(
      screen.getByRole('button', { name: 'Quarantine unknown commission' })
    )
    const dialog = screen.getByRole('dialog')
    await user.type(
      within(dialog).getByLabelText('External reference'),
      'incident-empty-status'
    )
    await user.type(
      within(dialog).getByLabelText('Reason'),
      'The legacy ledger has an empty status and needs a reviewed exception.'
    )
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Quarantine unknown commission',
      })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      action: 'quarantine_unknown_commission',
      expected_responsibility_fingerprint: 'fingerprint-1',
      commission_ledger_id: 33,
      commission_ledger_status: '',
    })
  })

  test('does not offer hold release to Admin while commission reconciliation is pending after Root review', async () => {
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'admin',
      role: ROLE.ADMIN,
    })
    installRefundCases([
      refundCase({
        requires_root_review: false,
        commission_ledger_id: 33,
        commission_ledger_status: '',
        commission_reconciliation_required: true,
        obligations: [],
      }),
    ])
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)

    expect(
      screen.getByText('Quarantine unknown commission first')
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Quarantine unknown commission' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Release account hold' })
    ).not.toBeInTheDocument()
  })

  test('distinguishes Root review completion from an obligation waiver', async () => {
    const waivedObligation = {
      ...refundCase().obligations[0],
      recovered_amount: 0,
      waived_amount: 500_000,
      status: 'waived' as const,
    }
    installRefundCases([
      refundCase({
        obligations: [waivedObligation],
        actions: [
          {
            id: 41,
            obligation_id: waivedObligation.id,
            user_id: 21,
            action: 'waive',
            asset: 'quota',
            amount: 500_000,
            actor_id: 1,
            remark: 'Debt waiver approved.',
            created_at: 2,
          },
          {
            id: 42,
            obligation_id: 0,
            user_id: 21,
            action: 'waive',
            amount: 0,
            actor_id: 1,
            remark: 'Responsibility assessment complete.',
            created_at: 3,
          },
        ],
      }),
    ])
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)

    expect(screen.getByText('Recovery waived')).toBeVisible()
    expect(screen.getByText('Root review completed')).toBeVisible()
  })

  test('creates a quantified obligation from verified evidence during Root review', async () => {
    installRefundCases([
      refundCase({
        requires_root_review: true,
        quota_amount: 0,
        wallet_debited_quota: 0,
        debt_created_quota: 0,
        obligations: [],
        responsible_users: [
          {
            user_id: 21,
            username: 'buyer',
            is_top_up_user: true,
            is_commission_recipient: false,
          },
          {
            user_id: 22,
            username: 'promoter',
            is_top_up_user: false,
            is_commission_recipient: true,
            is_rebate_recipient: true,
            is_invitation_reward_recipient: true,
            commission_ledger_id: 32,
            commission_amount_minor: 120,
            commission_quota: 600,
            commission_currency: 'CNY',
          },
        ],
      }),
    ])
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    await user.click(
      screen.getByRole('button', { name: 'Create recovery obligation' })
    )

    const dialog = screen.getByRole('dialog')
    const topUpUser = within(dialog).getByRole('radio', {
      name: /Top-up account/,
    })
    const commissionRecipient = within(dialog).getByRole('radio', {
      name: /Commission recipient · Rebate recipient · Invitation reward recipient/,
    })
    expect(topUpUser).toBeChecked()
    await user.click(commissionRecipient)
    expect(commissionRecipient).toBeChecked()
    expect(within(dialog).getByLabelText('Top-up ID')).toHaveValue(11)
    await user.type(within(dialog).getByLabelText(/Recovery amount/), '0.75')
    await user.type(
      within(dialog).getByLabelText('Evidence reference'),
      'provider-event-88'
    )
    await user.type(
      within(dialog).getByLabelText('Assessment reason'),
      'Provider evidence links this refund to the original API balance.'
    )
    await user.click(
      within(dialog).getByRole('button', {
        name: 'Create recovery obligation',
      })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      action: 'define_manual_obligation',
      obligation_id: 0,
      user_id: 22,
      top_up_id: 11,
      asset: 'quota',
      currency: '',
      amount: parseQuotaFromDollars(0.75),
      external_ref: 'provider-event-88',
    })
  })

  test('does not fall back to an unverified case user when no responsible account is returned', async () => {
    installRefundCases([
      refundCase({
        requires_root_review: true,
        obligations: [],
        responsible_users: [],
      }),
    ])
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    await user.click(
      screen.getByRole('button', { name: 'Create recovery obligation' })
    )

    const dialog = screen.getByRole('dialog')
    expect(
      within(dialog).getByText('No verified responsible account was found.')
    ).toBeVisible()
    expect(
      within(dialog).queryByRole('radio', {
        name: /Top-up account|Commission recipient|Refund account/,
      })
    ).not.toBeInTheDocument()
    expect(
      within(dialog).getByRole('button', {
        name: 'Create recovery obligation',
      })
    ).toBeDisabled()
  })

  test('retries a wallet debit with the same idempotency key after a lost response', async () => {
    installRefundCases([refundCase()])
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      if (requests.length === 1) {
        throw new Error('network unavailable')
      }
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    await user.click(screen.getByRole('button', { name: 'Debit API balance' }))

    const dialog = screen.getByRole('dialog')
    const amount = within(dialog).getByLabelText(/Recovery amount/)
    await user.clear(amount)
    await user.type(amount, '0.5')
    await user.click(
      within(dialog).getByRole('button', { name: 'Debit API balance' })
    )

    await waitFor(() => {
      expect(toastMocks.error).toHaveBeenCalledWith('network unavailable')
    })
    expect(screen.getByRole('dialog')).toBeVisible()

    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: 'Debit API balance',
      })
    )
    await waitFor(() => expect(requests).toHaveLength(2))

    expect(requests[0]?.amount).toBe(parseQuotaFromDollars(0.5))
    expect(requests[1]?.idempotency_key).toBe(requests[0]?.idempotency_key)
    expect(requests[1]).toMatchObject({
      action: 'retry_wallet_debit',
      obligation_id: 31,
    })
  })

  test('requires an external reference before recording an outside repayment', async () => {
    installRefundCases([refundCase()])
    const post = vi.fn<ApiPost>()
    apiClient.post = post
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await expandCase(user)
    await user.click(screen.getByRole('button', { name: 'Record repayment' }))
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: 'Record repayment',
      })
    )

    expect(
      await screen.findByText('External reference is required.')
    ).toBeVisible()
    expect(post).not.toHaveBeenCalled()
  })

  test('enables hold release only after every obligation is closed', async () => {
    const openCase = refundCase()
    const closedCase = refundCase({
      id: 2,
      trade_no: 'order-2',
      refund_trade_no: 'refund-2',
      obligations: [
        {
          ...refundCase().obligations[0],
          id: 32,
          refund_case_id: 2,
          recovered_amount: 500_000,
          status: 'recovered',
        },
      ],
    })
    installRefundCases([openCase, closedCase])
    const requests: Array<Record<string, unknown>> = []
    apiClient.post = async (_url, body) => {
      requests.push(body as Record<string, unknown>)
      return { data: { success: true, data: {} } }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await user.click(await screen.findByRole('button', { name: /order-1/ }))
    expect(
      screen.getByRole('button', { name: 'Release account hold' })
    ).toBeDisabled()

    await user.click(screen.getByRole('button', { name: /order-2/ }))
    const release = screen.getByRole('button', { name: 'Release account hold' })
    expect(release).toBeEnabled()
    await user.click(release)
    const dialog = screen.getByRole('dialog')
    await user.click(
      within(dialog).getByRole('button', { name: 'Release account hold' })
    )
    expect(await screen.findByText('A reason is required.')).toBeVisible()

    await user.type(
      within(dialog).getByLabelText('Reason'),
      'All obligations and disputes verified clear.'
    )
    await user.click(
      within(dialog).getByRole('button', { name: 'Release account hold' })
    )

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]).toMatchObject({
      action: 'release_hold',
      expected_responsibility_fingerprint: 'fingerprint-1',
      obligation_id: 0,
      amount: 0,
      remark: 'All obligations and disputes verified clear.',
    })
  })

  test('keeps Root decisions hidden while allowing Admin recovery and hold release', async () => {
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'admin',
      role: ROLE.ADMIN,
    })
    const rootReviewCase = refundCase({
      requires_root_review: true,
      commission_reconciliation_required: true,
      quota_amount: 0,
      wallet_debited_quota: 0,
      debt_created_quota: 0,
      obligations: [],
    })
    const recoverableCase = refundCase({
      id: 2,
      trade_no: 'order-2',
      refund_trade_no: 'refund-2',
      obligations: [
        {
          ...refundCase().obligations[0],
          id: 32,
          refund_case_id: 2,
        },
        {
          ...refundCase().obligations[0],
          id: 33,
          refund_case_id: 2,
          account: 'cash_commission',
          asset: 'cash',
          currency: 'CNY',
          amount: 800,
          source_type: 'promotion_commission_ledgers',
        },
      ],
    })
    const releasableCase = refundCase({
      id: 3,
      trade_no: 'order-3',
      refund_trade_no: 'refund-3',
      obligations: [
        {
          ...refundCase().obligations[0],
          id: 34,
          refund_case_id: 3,
          recovered_amount: 500_000,
          status: 'recovered',
        },
      ],
    })
    installRefundCases([rootReviewCase, recoverableCase, releasableCase])
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)

    await user.click(await screen.findByRole('button', { name: /order-1/ }))
    expect(
      screen.getByText('Quarantine unknown commission first')
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Create recovery obligation' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Complete Root review' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Quarantine unknown commission' })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /order-2/ }))
    expect(
      screen.getByRole('button', { name: 'Debit API balance' })
    ).toBeEnabled()
    expect(
      screen.getByRole('button', { name: 'Record repayment' })
    ).toBeEnabled()
    expect(
      screen.getByRole('button', { name: 'Record commission recovery' })
    ).toBeEnabled()
    expect(
      screen.queryByRole('button', { name: 'Waive debt' })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /order-3/ }))
    expect(
      screen.getByRole('button', { name: 'Release account hold' })
    ).toBeEnabled()
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
              refundCase({
                id: 2,
                trade_no: 'order-latest',
                refund_trade_no: 'refund-latest',
                status: 'resolved',
                reason: 'latest response',
                obligations: [],
              }),
            ],
          },
        },
      }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)
    await user.click(screen.getByLabelText('Refund case status'))
    await user.click(await screen.findByRole('option', { name: 'Resolved' }))
    expect(await screen.findByText(/order-latest/)).toBeVisible()

    await act(async () => {
      resolveFirstRequest?.({
        data: {
          success: true,
          data: {
            total: 1,
            items: [refundCase({ trade_no: 'order-stale' })],
          },
        },
      })
      await Promise.resolve()
    })

    expect(screen.getByText(/order-latest/)).toBeVisible()
    expect(screen.queryByText(/order-stale/)).not.toBeInTheDocument()
  })

  test('disables stale recovery actions while a new filter loads', async () => {
    let resolveResolved:
      | ((value: { data: Record<string, unknown> }) => void)
      | undefined
    let requestCount = 0
    apiClient.get = async () => {
      requestCount += 1
      if (requestCount > 1) {
        return new Promise((resolve) => {
          resolveResolved = resolve
        })
      }
      return {
        data: {
          success: true,
          data: { total: 1, items: [refundCase()] },
        },
      }
    }
    const user = userEvent.setup()

    render(<GrowthRefundCasesSection />)

    await user.click(await screen.findByRole('button', { name: /order-1/ }))
    const debit = screen.getByRole('button', { name: 'Debit API balance' })
    await user.click(screen.getByLabelText('Refund case status'))
    await user.click(await screen.findByRole('option', { name: 'Resolved' }))

    await waitFor(() => expect(requestCount).toBe(2))
    expect(debit).toBeDisabled()

    resolveResolved?.({
      data: { success: true, data: { total: 0, items: [] } },
    })
    expect(
      await screen.findByText('No refund cases match this status.')
    ).toBeVisible()
  })

  test('does not combine a load error with the empty refund-case state', async () => {
    apiClient.get = async () => {
      throw new Error('network unavailable')
    }

    render(<GrowthRefundCasesSection />)

    expect(
      await screen.findByText('Refund cases could not be loaded')
    ).toBeVisible()
    expect(
      screen.queryByText('No refund recovery cases')
    ).not.toBeInTheDocument()
  })
})
