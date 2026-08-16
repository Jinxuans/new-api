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

import { parseSidebarModulesAdmin } from '../config'

describe('sidebar rewards module compatibility', () => {
  test.each([
    [{ rewards: false }, false],
    [{ invite: false }, false],
    [{ rewards: false, invite: true }, true],
    [{ rewards: true, invite: false }, true],
    [{ rewards: false, promotion: true }, true],
    [{ rewards: true, promotion: false }, false],
  ])(
    'normalizes legacy personal config %j to the canonical promotion flag',
    (personal, expected) => {
      const config = parseSidebarModulesAdmin(
        JSON.stringify({ personal: { enabled: true, ...personal } })
      )

      expect(config.personal.promotion).toBe(expected)
      expect(config.personal).not.toHaveProperty('rewards')
      expect(config.personal).not.toHaveProperty('invite')
    }
  )
})
