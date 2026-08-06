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

import { USER_ROLE, USER_STATUS } from '../constants'
import type { User } from '../types'
import {
  filterInviterCandidates,
  hasEmptyInviter,
  shouldShowBindInviterAction,
} from './user-inviter-binding'

function user(overrides: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'user-1',
    display_name: 'User 1',
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    status: USER_STATUS.ENABLED,
    role: USER_ROLE.USER,
    ...overrides,
  }
}

describe('user inviter binding rules', () => {
  test('shows the action only to root for an unbound live target', () => {
    assert.equal(shouldShowBindInviterAction(USER_ROLE.ROOT, user()), true)
    assert.equal(shouldShowBindInviterAction(USER_ROLE.ADMIN, user()), false)
    assert.equal(shouldShowBindInviterAction(USER_ROLE.USER, user()), false)
    assert.equal(
      shouldShowBindInviterAction(USER_ROLE.ROOT, user({ inviter_id: 9 })),
      false
    )
    assert.equal(
      shouldShowBindInviterAction(
        USER_ROLE.ROOT,
        user({ DeletedAt: 'deleted' })
      ),
      false
    )
  })

  test('treats zero, missing, and null inviter IDs as unbound', () => {
    assert.equal(hasEmptyInviter(user({ inviter_id: 0 })), true)
    assert.equal(hasEmptyInviter(user()), true)
    assert.equal(hasEmptyInviter(user({ inviter_id: null as never })), true)
    assert.equal(hasEmptyInviter(user({ inviter_id: 3 })), false)
  })

  test('removes the target and deleted candidates but keeps disabled users', () => {
    const candidates = filterInviterCandidates(
      [
        user({ id: 7 }),
        user({ id: 8, status: USER_STATUS.DISABLED }),
        user({ id: 9, DeletedAt: 'deleted' }),
        user({ id: 10 }),
      ],
      7
    )

    assert.deepEqual(
      candidates.map((candidate) => candidate.id),
      [8, 10]
    )
  })
})
