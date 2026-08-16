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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { transferAllReferralCredit } from '../api'

const walletApi = vi.hoisted(() => ({
  getAffiliateCode: vi.fn(),
  transferAffiliateQuota: vi.fn(),
}))

vi.mock('@/features/wallet/api', () => walletApi)

beforeEach(() => {
  walletApi.transferAffiliateQuota.mockReset()
})

describe('promotion API success responses', () => {
  test('accepts a successful referral transfer with null data', async () => {
    walletApi.transferAffiliateQuota.mockResolvedValue({
      success: true,
      data: null,
    })

    await expect(transferAllReferralCredit(12_000)).resolves.toBeUndefined()
    expect(walletApi.transferAffiliateQuota).toHaveBeenCalledWith({
      quota: 12_000,
    })
  })
})
