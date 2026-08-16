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
import { describe, expect, test } from 'vitest'

import {
  createPromotionSubmissionSchema,
  createPromotionWithdrawalSchema,
} from '../schemas'

const t = (key: string) => key

describe('promotion form boundaries', () => {
  test('matches backend limits for platform and payout method', () => {
    const submission = createPromotionSubmissionSchema(t).safeParse({
      itemCode: 'tutorial',
      platform: 'x'.repeat(65),
      url: 'https://example.com/tutorial',
      remark: '',
    })
    const withdrawal = createPromotionWithdrawalSchema(t).safeParse({
      payoutMethod: 'x'.repeat(33),
      payoutAccount: 'account',
      remark: '',
    })

    expect(submission.error?.issues[0]?.message).toBe('Platform is too long')
    expect(withdrawal.error?.issues[0]?.message).toBe(
      'Payout method is too long'
    )
  })
})
