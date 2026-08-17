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
export const PUBLIC_STATUS_PROBE_PROTOCOLS = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const

export type PublicStatusProbeProtocol =
  (typeof PUBLIC_STATUS_PROBE_PROTOCOLS)[number]

export interface PublicStatusProbeChannel {
  id: number
  name: string
  type: number
  status: number
  models: string
  is_multi_key: boolean
  key_count: number
}

export interface TargetInput {
  enabled: boolean
  group: string
  display_name: string
  model: string
  protocol: PublicStatusProbeProtocol
  channel_id: number
  key_index: number
}

export interface PublicStatusProbeTarget extends TargetInput {
  key: string
  channel: PublicStatusProbeChannel
}

export interface GlobalSettingsInput {
  enabled: boolean
  ping_timeout_seconds: number
  chat_timeout_seconds: number
  degraded_latency_ms: number
  concurrency: number
  retention_days: number
}

export interface PublicStatusProbeConfig extends GlobalSettingsInput {
  version: number
  targets: PublicStatusProbeTarget[]
}

export type PublicStatusProbeErrorKind = 'validation' | 'conflict' | 'request'

export type PublicStatusProbeFieldErrors = Record<string, string[]>
