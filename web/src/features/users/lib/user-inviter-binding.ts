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
import { USER_ROLE, isUserDeleted } from '../constants'
import type { User } from '../types'

export function hasEmptyInviter(user: Pick<User, 'inviter_id'>): boolean {
  return !user.inviter_id || user.inviter_id === 0
}

export function shouldShowBindInviterAction(
  operatorRole: number | undefined,
  user: User
): boolean {
  return (
    operatorRole === USER_ROLE.ROOT &&
    !isUserDeleted(user) &&
    hasEmptyInviter(user)
  )
}

export function filterInviterCandidates(
  candidates: User[],
  targetUserId: number
): User[] {
  return candidates.filter((candidate) => {
    if (candidate.id === targetUserId) return false
    if (isUserDeleted(candidate)) return false
    return true
  })
}
