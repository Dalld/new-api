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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { Share2 } from 'lucide-react'

import { isModuleEnabled } from './use-sidebar-config'
import { buildSidebarData } from './use-sidebar-data'

describe('affiliate sidebar entries', () => {
  test('places user and admin entries in their role-filtered root groups', () => {
    const data = buildSidebarData((key) => key)
    const personal = data.navGroups.find((group) => group.id === 'personal')
    const admin = data.navGroups.find((group) => group.id === 'admin')

    assert.ok(personal)
    assert.ok(admin)

    const userEntry = personal.items.find(
      (item) => 'url' in item && item.url === '/my-affiliate'
    )
    const adminEntry = admin.items.find(
      (item) => 'url' in item && item.url === '/affiliate'
    )

    assert.ok(userEntry)
    assert.equal(userEntry.icon, Share2)
    assert.ok(adminEntry)
    assert.equal(adminEntry.icon, Share2)
  })

  test('applies admin and user module visibility to affiliate routes', () => {
    const enabled = {
      personal: { enabled: true, referral: true },
      admin: { enabled: true, affiliate: true },
    }

    assert.equal(isModuleEnabled('/my-affiliate', enabled, null), true)
    assert.equal(isModuleEnabled('/affiliate', enabled, null), true)
    assert.equal(
      isModuleEnabled('/my-affiliate', enabled, {
        personal: { enabled: true, referral: false },
      }),
      false
    )
    assert.equal(
      isModuleEnabled(
        '/affiliate',
        { ...enabled, admin: { enabled: true, affiliate: false } },
        null
      ),
      false
    )
  })
})
