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
import type { PublicProbeHistoryPoint, PublicProbePoint } from './types'

export const PUBLIC_PROBE_HISTORY_SIZE = 60

export function normalizeProbeHistory(
  history: readonly PublicProbePoint[]
): PublicProbePoint[] {
  const latestByTimestamp = new Map<number, PublicProbePoint>()
  for (const point of history) {
    latestByTimestamp.set(point.checked_at, point)
  }

  return [...latestByTimestamp.values()]
    .sort((left, right) => left.checked_at - right.checked_at)
    .slice(-PUBLIC_PROBE_HISTORY_SIZE)
}

export function padProbeHistory(
  history: readonly PublicProbePoint[],
  targetKey: string
): PublicProbeHistoryPoint[] {
  const realPoints = normalizeProbeHistory(history)
  const placeholderCount = PUBLIC_PROBE_HISTORY_SIZE - realPoints.length
  const placeholders: PublicProbeHistoryPoint[] = Array.from(
    { length: placeholderCount },
    (_, slotIndex) => ({
      id: `${targetKey}:placeholder:${slotIndex}`,
      placeholder: true,
      checked_at: null,
      state: 'unknown',
      ping_latency_ms: null,
      chat_latency_ms: null,
      error_code: null,
    })
  )

  return [
    ...placeholders,
    ...realPoints.map(
      (point): PublicProbeHistoryPoint => ({
        ...point,
        id: `${targetKey}:${point.checked_at}`,
        placeholder: false,
      })
    ),
  ]
}
