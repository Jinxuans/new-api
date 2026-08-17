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

import { formatMinorAmount, parseMinorAmount } from '../growth-refund-types'

describe('refund recovery cash input', () => {
  test.each([
    ['CNY', '10.23', 1023],
    ['JPY', '10', 10],
    ['KWD', '1.234', 1234],
    ['CNY', '0001.20', 120],
  ])(
    'parses %s amounts without floating-point rounding',
    (currency, input, expected) => {
      expect(parseMinorAmount(input, currency)).toBe(expected)
    }
  )

  test.each([
    ['CNY', '10.001'],
    ['JPY', '10.1'],
    ['KWD', '1.2345'],
    ['CNY', '1e3'],
    ['CNY', '-1.00'],
    ['CNY', '9007199254740992.00'],
  ])('rejects invalid or over-precise %s amounts', (currency, input) => {
    expect(parseMinorAmount(input, currency)).toBeNull()
  })

  test.each([
    ['CNY', 1023, 'CNY 10.23'],
    ['JPY', 10, 'JPY 10'],
    ['KWD', 1234, 'KWD 1.234'],
  ])(
    'formats %s using its own minor-unit scale',
    (currency, amount, expected) => {
      expect(formatMinorAmount(amount, currency).replaceAll(/\s/g, ' ')).toBe(
        expected
      )
    }
  )
})
