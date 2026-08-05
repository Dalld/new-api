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

import type { HeaderNavModules } from '@/lib/nav-modules'

import { buildTopNavLinks } from './use-top-nav-links'

const translate = (key: string) => key

const disabledModules: HeaderNavModules = {
  home: false,
  console: false,
  pricing: { enabled: false, requireAuth: true },
  rankings: { enabled: false, requireAuth: true },
  docs: false,
  about: false,
  status: false,
}

describe('public top navigation', () => {
  test('always includes the public channel status link', () => {
    const links = buildTopNavLinks(translate, disabledModules, undefined, false)

    assert.deepEqual(links, [{ title: 'Channel Status', href: '/status' }])
    assert.equal('requiresAuth' in links[0], false)
  })

  test('keeps channel status public for authenticated users', () => {
    const links = buildTopNavLinks(translate, disabledModules, undefined, true)
    const statusLink = links.find((link) => link.href === '/status')

    assert.deepEqual(statusLink, {
      title: 'Channel Status',
      href: '/status',
    })
  })
})
