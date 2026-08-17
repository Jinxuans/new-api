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
import { renderHook } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import { useSidebarData } from '@/hooks/use-sidebar-data'
import { ROLE } from '@/lib/roles'
import { Route } from '@/routes/_authenticated/funds-operations/index'
import { useAuthStore } from '@/stores/auth-store'

function setRole(role: number) {
  useAuthStore.getState().auth.setUser({
    id: role,
    username: `role-${role}`,
    role,
  })
}

function getRedirectTarget() {
  try {
    const beforeLoad = Route.options.beforeLoad as (() => unknown) | undefined
    beforeLoad?.()
  } catch (error) {
    return (error as { options?: { to?: string } }).options?.to
  }
  return undefined
}

afterEach(() => {
  useAuthStore.getState().auth.setUser(null)
})

describe('funds operations access', () => {
  test.each([ROLE.ADMIN, ROLE.SUPER_ADMIN])(
    'allows role %s to open the funds workspace',
    (role) => {
      setRole(role)
      expect(getRedirectTarget()).toBeUndefined()
    }
  )

  test('rejects non-admin users', () => {
    setRole(ROLE.USER)
    expect(getRedirectTarget()).toBe('/403')
  })

  test('keeps funds operations admin-visible and system settings Root-only', () => {
    const { result } = renderHook(() => useSidebarData())
    const adminItems =
      result.current.navGroups.find((group) => group.id === 'admin')?.items ||
      []
    const fundsOperations = adminItems.find(
      (item) => 'url' in item && item.url === '/funds-operations'
    )
    const systemSettings = adminItems.find(
      (item) => 'url' in item && item.url === '/system-settings/site'
    )

    expect(fundsOperations).toMatchObject({
      title: 'Funds Operations',
      requiredRole: ROLE.ADMIN,
    })
    expect(systemSettings).toMatchObject({
      title: 'System Settings',
      requiredRole: ROLE.SUPER_ADMIN,
    })
  })
})
