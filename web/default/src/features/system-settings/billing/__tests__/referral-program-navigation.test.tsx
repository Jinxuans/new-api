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
import { expect, test, vi } from 'vitest'

import { ReferralProgramAdminSection } from '../growth-program-sections'

vi.mock(
  '@/features/system-settings/general/referral-program-settings-section',
  () => ({
    ReferralProgramSettingsSection: () => <div>Referral settings form</div>,
  })
)

test('keeps operational reviews out of referral settings', () => {
  render(
    <ReferralProgramAdminSection
      defaultValues={{
        inviterRegistrationRewardQuota: 0,
        inviteeRegistrationRewardQuota: 0,
        inviteRebatePercentage: 0,
        inviteFirstRequestRewardQuota: 0,
        inviteFirstTopUpRewardQuota: 0,
        rebateFreezeDays: 0,
      }}
      complianceConfirmed={false}
    />
  )

  expect(
    screen.getByRole('heading', { name: 'Referral rules' })
  ).toBeInTheDocument()
  expect(screen.getByText('Referral settings form')).toBeInTheDocument()
  expect(
    screen.queryByRole('tab', { name: 'Withdrawal Reviews' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('tab', { name: 'Refund cases' })
  ).not.toBeInTheDocument()
})
