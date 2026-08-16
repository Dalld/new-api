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
import { queryOptions } from '@tanstack/react-query'
import { z } from 'zod'

import { api } from '@/lib/api'

import { normalizeProbeHistory } from './history'
import {
  PUBLIC_PROBE_ERROR_CODES,
  PUBLIC_PROBE_STATES,
  type PublicProbeData,
} from './types'

const unixTimestampSchema = z.number().int().positive()
const nullableLatencySchema = z.number().int().nonnegative().nullable()
const stateSchema = z.enum(PUBLIC_PROBE_STATES)
const errorCodeSchema = z.enum(PUBLIC_PROBE_ERROR_CODES).nullable()

function boundedIdentifierSchema(maxCodePoints: number) {
  return z
    .string()
    .min(1)
    .refine((value) => value === value.trim())
    .refine((value) => [...value].length <= maxCodePoints)
}

const probePointSchema = z
  .object({
    checked_at: unixTimestampSchema,
    state: stateSchema,
    ping_latency_ms: nullableLatencySchema,
    chat_latency_ms: nullableLatencySchema,
    error_code: errorCodeSchema,
  })
  .strict()

const probeTargetSchema = z
  .object({
    key: boundedIdentifierSchema(96),
    group: boundedIdentifierSchema(64),
    display_name: boundedIdentifierSchema(128),
    model: boundedIdentifierSchema(255),
    state: stateSchema,
    availability: z.number().min(0).max(1).nullable(),
    ping_latency_ms: nullableLatencySchema,
    chat_latency_ms: nullableLatencySchema,
    latest_checked_at: unixTimestampSchema.nullable(),
    next_check_at: unixTimestampSchema,
    history: z.array(probePointSchema).max(60),
  })
  .strict()

const probeTargetsSchema = z
  .array(probeTargetSchema)
  .max(20)
  .refine(
    (targets) => new Set(targets.map(({ key }) => key)).size === targets.length
  )

const publicProbeResponseSchema = z
  .object({
    success: z.literal(true),
    data: z
      .object({
        generated_at: unixTimestampSchema,
        interval_seconds: z.literal(60),
        targets: probeTargetsSchema,
      })
      .strict(),
  })
  .strict()

export function parsePublicProbeStatus(input: unknown): PublicProbeData {
  const { data } = publicProbeResponseSchema.parse(input)

  return {
    ...data,
    targets: data.targets.map((target) => ({
      ...target,
      history: normalizeProbeHistory(target.history),
    })),
  }
}

export const parsePublicGroupProbeStatus = parsePublicProbeStatus

export async function getPublicGroupProbeStatus() {
  const response = await api.get('/api/status/probes', {
    skipErrorHandler: true,
  })
  return parsePublicProbeStatus(response.data)
}

export const groupProbeStatusQueryOptions = () =>
  queryOptions({
    queryKey: ['public-group-probe-status'],
    queryFn: getPublicGroupProbeStatus,
    refetchInterval: 60_000,
    staleTime: 15_000,
    retry: 2,
  })
