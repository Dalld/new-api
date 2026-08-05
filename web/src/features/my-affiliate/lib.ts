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
import type { PageQueryInput } from './types'

export const DEFAULT_PAGE_SIZE = 10

function toBoundedInteger(value: number, fallback: number, maximum: number) {
  if (!Number.isFinite(value)) return fallback
  return Math.min(maximum, Math.max(1, Math.trunc(value)))
}

export function buildPageQuery(input: PageQueryInput): string {
  const params = new URLSearchParams({
    p: String(toBoundedInteger(input.page, 1, Number.MAX_SAFE_INTEGER)),
    page_size: String(toBoundedInteger(input.pageSize, DEFAULT_PAGE_SIZE, 100)),
  })
  const keyword = input.keyword?.trim()
  if (keyword) params.set('keyword', keyword)
  return params.toString()
}

export function createInviteLink(origin: string, affiliateCode: string) {
  const code = affiliateCode.trim()
  if (!code) return ''

  try {
    const normalizedOrigin = new URL(origin).origin
    return `${normalizedOrigin}/sign-up?aff=${encodeURIComponent(code)}`
  } catch {
    return ''
  }
}

export function getTotalPages(total: number, pageSize: number) {
  const safeTotal = Number.isFinite(total) ? Math.max(0, total) : 0
  const safePageSize = toBoundedInteger(pageSize, DEFAULT_PAGE_SIZE, 100)
  return Math.max(1, Math.ceil(safeTotal / safePageSize))
}
