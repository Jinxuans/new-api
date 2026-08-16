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
import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { AffiliateRewardsCard } from '../affiliate-rewards-card'

const sidebarVisibility = vi.hoisted(() => ({ current: true }))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, ...props }: React.ComponentProps<'a'> & { to: string }) => (
    <a href={to} {...props} />
  ),
}))

vi.mock('@/hooks/use-sidebar-config', () => ({
  useIsSidebarModuleVisible: () => sidebarVisibility.current,
}))

describe('affiliate rewards wallet summary', () => {
  test('links to the canonical page without repeating balances or transfer actions', () => {
    render(<AffiliateRewardsCard />)

    expect(
      screen.getByRole('link', { name: 'View rewards & referrals' })
    ).toHaveAttribute('href', '/promotion')
    expect(screen.queryByText('Referral credit')).not.toBeInTheDocument()
    expect(screen.queryByText('Total earned')).not.toBeInTheDocument()
    expect(screen.queryByText('Invited users')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Transfer to Balance' })
    ).not.toBeInTheDocument()
  })

  test('hides the wallet entry when rewards and referrals are unavailable', () => {
    sidebarVisibility.current = false

    const { container } = render(<AffiliateRewardsCard loading={false} />)

    expect(container).toBeEmptyDOMElement()
    sidebarVisibility.current = true
  })
})
