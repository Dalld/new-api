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

import {
  GROUP_PROBE_STATES,
  type PublicGroupProbe,
  type PublicGroupProbeBucket,
  type PublicGroupProbeStatus,
} from './types'

const MAX_BUCKET_COUNT = 288

const stateSchema = z.enum(GROUP_PROBE_STATES)
const intervalMinutesSchema = z.number().int().min(5).max(1440)

const rawBucketSchema = z.object({
  started_at: z.number().int().nonnegative().optional(),
  bucket_start: z.number().int().nonnegative().optional(),
  timestamp: z.number().int().nonnegative().optional(),
  state: stateSchema,
  sample_count: z.number().int().nonnegative().optional(),
})

const rawGroupSchema = z.object({
  group: z.string().trim().min(1).max(128).optional(),
  group_name: z.string().trim().min(1).max(128).optional(),
  display_name: z.string().trim().min(1).max(128),
  model: z.string().trim().min(1).max(255).optional(),
  model_name: z.string().trim().min(1).max(255).optional(),
  state: stateSchema,
  availability: z.number().min(0).max(1).nullable().optional(),
  availability_24h: z.number().min(0).max(1).nullable().optional(),
  average_latency_ms: z.number().nonnegative().nullable().optional(),
  avg_latency_ms: z.number().nonnegative().nullable().optional(),
  sample_count: z.number().int().nonnegative().default(0),
  latest_checked_at: z.number().int().nonnegative().nullable().optional(),
  last_checked_at: z.number().int().nonnegative().nullable().optional(),
  stale: z.boolean().optional(),
  fresh: z.boolean().optional(),
  interval_minutes: intervalMinutesSchema.optional(),
  buckets: z.array(rawBucketSchema).max(MAX_BUCKET_COUNT),
})

const rawDataSchema = z.union([
  z.array(rawGroupSchema),
  z.object({
    generated_at: z.number().int().nonnegative().optional(),
    interval_minutes: intervalMinutesSchema.optional(),
    groups: z.array(rawGroupSchema),
  }),
])

const rawEnvelopeSchema = z.object({
  success: z.literal(true),
  data: rawDataSchema,
})

function coalesce<T>(...values: Array<T | null | undefined>): T | null {
  return (
    values.find((value): value is T => value !== undefined && value !== null) ??
    null
  )
}

function normalizeBuckets(
  buckets: z.infer<typeof rawBucketSchema>[]
): PublicGroupProbeBucket[] {
  const normalized: PublicGroupProbeBucket[] = []

  for (const bucket of buckets) {
    const startedAt = coalesce(
      bucket.started_at,
      bucket.bucket_start,
      bucket.timestamp
    )
    if (startedAt === null) continue
    normalized.push({
      startedAt,
      state: bucket.state,
      sampleCount: bucket.sample_count ?? 0,
    })
  }

  return normalized
}

export function parsePublicGroupProbeStatus(
  input: unknown,
  nowSeconds = Math.floor(Date.now() / 1000)
): PublicGroupProbeStatus {
  const envelope = rawEnvelopeSchema.parse(input)
  const rawData = envelope.data
  const generatedAt = Array.isArray(rawData)
    ? nowSeconds
    : (rawData.generated_at ?? nowSeconds)
  const defaultInterval = Array.isArray(rawData)
    ? null
    : (rawData.interval_minutes ?? null)
  const groups = Array.isArray(rawData) ? rawData : rawData.groups

  return {
    generatedAt,
    groups: groups.map((group): PublicGroupProbe => {
      const groupName = coalesce(group.group, group.group_name)
      const model = coalesce(group.model, group.model_name)
      if (!groupName || !model) {
        throw new Error('Public group probe response is missing an identifier')
      }
      const intervalMinutes = group.interval_minutes ?? defaultInterval
      if (intervalMinutes === null) {
        throw new Error('Public group probe response is missing an interval')
      }

      return {
        group: groupName,
        displayName: group.display_name,
        model,
        state: group.state,
        availability: coalesce(group.availability, group.availability_24h),
        averageLatencyMs: coalesce(
          group.average_latency_ms,
          group.avg_latency_ms
        ),
        sampleCount: group.sample_count,
        latestCheckedAt: coalesce(
          group.latest_checked_at,
          group.last_checked_at
        ),
        stale: group.stale ?? group.fresh === false,
        intervalMinutes,
        buckets: normalizeBuckets(group.buckets),
      }
    }),
  }
}

export async function getPublicGroupProbeStatus() {
  const response = await api.get('/api/status/probes', {
    skipErrorHandler: true,
  })
  return parsePublicGroupProbeStatus(response.data)
}

export const groupProbeStatusQueryOptions = () =>
  queryOptions({
    queryKey: ['public-group-probe-status'],
    queryFn: getPublicGroupProbeStatus,
    refetchInterval: 30_000,
    staleTime: 15_000,
    retry: 2,
  })
