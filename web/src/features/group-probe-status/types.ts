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

export const PUBLIC_PROBE_STATES = [
  'operational',
  'degraded',
  'validation_failed',
  'failed',
  'unknown',
] as const

export type PublicProbeState = (typeof PUBLIC_PROBE_STATES)[number]

export const PUBLIC_PROBE_ERROR_CODES = [
  'unsupported_provider',
  'timeout',
  'network_error',
  'provider_rejected',
  'empty_response',
  'validation_failed',
  'invalid_target',
  'response_too_large',
] as const

export type PublicProbeErrorCode = (typeof PUBLIC_PROBE_ERROR_CODES)[number]

export type PublicProbePoint = {
  checked_at: number
  state: PublicProbeState
  ping_latency_ms: number | null
  chat_latency_ms: number | null
  error_code: PublicProbeErrorCode | null
}

export type PublicProbeTarget = {
  key: string
  group: string
  display_name: string
  model: string
  state: PublicProbeState
  availability: number | null
  ping_latency_ms: number | null
  chat_latency_ms: number | null
  latest_checked_at: number | null
  next_check_at: number
  history: PublicProbePoint[]
}

export type PublicProbeData = {
  generated_at: number
  interval_seconds: 60
  targets: PublicProbeTarget[]
}

export type PublicProbeResponse = {
  success: true
  data: PublicProbeData
}

export type PublicProbeHistoryPoint =
  | (PublicProbePoint & {
      id: string
      placeholder: false
    })
  | {
      id: string
      placeholder: true
      checked_at: null
      state: 'unknown'
      ping_latency_ms: null
      chat_latency_ms: null
      error_code: null
    }
