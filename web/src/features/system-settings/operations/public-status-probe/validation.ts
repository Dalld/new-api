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
import axios from 'axios'
import { z } from 'zod'

import {
  PUBLIC_STATUS_PROBE_PROTOCOLS,
  type GlobalSettingsInput,
  type PublicStatusProbeChannel,
  type PublicStatusProbeConfig,
  type PublicStatusProbeErrorKind,
  type PublicStatusProbeFieldErrors,
  type PublicStatusProbeTarget,
  type TargetInput,
} from './types'

const positiveVersionSchema = z.number().int().positive()

function isGoSpace(value: string) {
  const codePoint = value.codePointAt(0)
  return (
    (codePoint !== undefined && codePoint >= 0x09 && codePoint <= 0x0d) ||
    codePoint === 0x20 ||
    codePoint === 0x85 ||
    codePoint === 0xa0 ||
    codePoint === 0x1680 ||
    (codePoint !== undefined && codePoint >= 0x2000 && codePoint <= 0x200a) ||
    codePoint === 0x2028 ||
    codePoint === 0x2029 ||
    codePoint === 0x202f ||
    codePoint === 0x205f ||
    codePoint === 0x3000
  )
}

function trimGoSpace(value: string) {
  const codePoints = [...value]
  let start = 0
  let end = codePoints.length
  while (start < end && isGoSpace(codePoints[start] ?? '')) start += 1
  while (end > start && isGoSpace(codePoints[end - 1] ?? '')) end -= 1
  return codePoints.slice(start, end).join('')
}

function requiredTrimmedString(maxCodePoints: number) {
  return z
    .string()
    .transform(trimGoSpace)
    .pipe(
      z
        .string()
        .min(1)
        .refine((value) => [...value].length <= maxCodePoints)
    )
}

function canonicalString(maxCodePoints: number) {
  return z
    .string()
    .min(1)
    .refine((value) => value === trimGoSpace(value))
    .refine((value) => [...value].length <= maxCodePoints)
}

export const globalSettingsSchema = z
  .object({
    enabled: z.boolean(),
    ping_timeout_seconds: z.number().int().min(1).max(15),
    chat_timeout_seconds: z.number().int().min(5).max(60),
    degraded_latency_ms: z.number().int().min(1).max(60_000),
    concurrency: z.number().int().min(1).max(20),
    retention_days: z.number().int().min(1).max(30),
  })
  .strict() satisfies z.ZodType<GlobalSettingsInput>

export const targetFormSchema = z
  .object({
    enabled: z.boolean(),
    group: requiredTrimmedString(64),
    display_name: requiredTrimmedString(128),
    model: requiredTrimmedString(128),
    protocol: z.enum(PUBLIC_STATUS_PROBE_PROTOCOLS),
    channel_id: z.number().int().positive(),
    key_index: z.number().int().nonnegative(),
  })
  .strict() satisfies z.ZodType<TargetInput>

export const versionedGlobalSettingsSchema = globalSettingsSchema.extend({
  version: positiveVersionSchema,
})

export const publicStatusProbeChannelSchema = z
  .object({
    id: z.number().int().positive(),
    name: z.string(),
    type: z.number().int().nonnegative(),
    status: z.number().int().nonnegative(),
    models: z.string(),
    is_multi_key: z.boolean(),
    key_count: z.number().int().nonnegative(),
  })
  .strict() satisfies z.ZodType<PublicStatusProbeChannel>

export const publicStatusProbeTargetSchema = z
  .object({
    enabled: z.boolean(),
    key: canonicalString(96),
    group: canonicalString(64),
    display_name: canonicalString(128),
    model: canonicalString(128),
    protocol: z.enum(PUBLIC_STATUS_PROBE_PROTOCOLS),
    channel_id: z.number().int().positive(),
    key_index: z.number().int().nonnegative(),
    channel: publicStatusProbeChannelSchema,
  })
  .strict() satisfies z.ZodType<PublicStatusProbeTarget>

const publicStatusProbeTargetsSchema = z
  .array(publicStatusProbeTargetSchema)
  .max(20)
  .superRefine((targets, context) => {
    const keys = new Set<string>()
    for (const [index, target] of targets.entries()) {
      if (keys.has(target.key)) {
        context.addIssue({
          code: 'custom',
          message: 'duplicate target key',
          path: [index, 'key'],
        })
      }
      keys.add(target.key)
    }
  })

export const publicStatusProbeConfigSchema = globalSettingsSchema.extend({
  version: positiveVersionSchema,
  targets: publicStatusProbeTargetsSchema,
  channels: z.array(publicStatusProbeChannelSchema),
}) satisfies z.ZodType<PublicStatusProbeConfig>

export const publicStatusProbeSuccessResponseSchema = z
  .object({
    success: z.literal(true),
    message: z.string(),
    data: publicStatusProbeConfigSchema,
  })
  .strict()

export class PublicStatusProbeClientError extends Error {
  readonly retry = false

  constructor(
    readonly kind: PublicStatusProbeErrorKind,
    readonly fieldErrors: PublicStatusProbeFieldErrors = {}
  ) {
    super(`public status probe ${kind} error`)
    this.name = 'PublicStatusProbeClientError'
  }
}

function zodFieldErrors(error: z.ZodError): PublicStatusProbeFieldErrors {
  const fieldErrors: PublicStatusProbeFieldErrors = {}
  for (const issue of error.issues) {
    const field = issue.path.join('.') || '_form'
    fieldErrors[field] = [...(fieldErrors[field] ?? []), issue.message]
  }
  return fieldErrors
}

export function mapPublicStatusProbeError(
  error: unknown
): PublicStatusProbeClientError {
  if (error instanceof PublicStatusProbeClientError) return error
  if (error instanceof z.ZodError) {
    return new PublicStatusProbeClientError('validation', zodFieldErrors(error))
  }
  if (axios.isAxiosError(error)) {
    if (error.response?.status === 400) {
      return new PublicStatusProbeClientError('validation')
    }
    if (error.response?.status === 409) {
      return new PublicStatusProbeClientError('conflict')
    }
  }
  return new PublicStatusProbeClientError('request')
}

export function parsePublicStatusProbeResponse(input: unknown) {
  const result = publicStatusProbeSuccessResponseSchema.safeParse(input)
  if (!result.success) {
    throw new PublicStatusProbeClientError('request')
  }
  return result.data.data
}
