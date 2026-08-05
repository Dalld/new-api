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

export const GROUP_PROBE_STATES = [
  'healthy',
  'degraded',
  'down',
  'unknown',
] as const

export type GroupProbeState = (typeof GROUP_PROBE_STATES)[number]

export type PublicGroupProbeBucket = {
  startedAt: number
  state: GroupProbeState
  sampleCount: number
}

export type PublicGroupProbe = {
  group: string
  displayName: string
  model: string
  state: GroupProbeState
  availability: number | null
  averageLatencyMs: number | null
  sampleCount: number
  latestCheckedAt: number | null
  stale: boolean
  intervalMinutes: number
  buckets: PublicGroupProbeBucket[]
}

export type PublicGroupProbeStatus = {
  generatedAt: number
  groups: PublicGroupProbe[]
}
