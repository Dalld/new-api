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
import { z } from 'zod'

import type {
  AffiliateRelation,
  ApiResponse,
  CommissionRecordDetail,
  PageData,
} from './types'

const nonNegativeInteger = z.number().int().nonnegative()
const positiveInteger = z.number().int().positive()

const relationSchema: z.ZodType<AffiliateRelation> = z.object({
  inviter_id: positiveInteger,
  username: z.string(),
  display_name: z.string(),
  invitee_count: nonNegativeInteger,
  aff_quota: nonNegativeInteger,
  aff_history_quota: nonNegativeInteger,
  created_at: nonNegativeInteger,
})

const commissionSchema: z.ZodType<CommissionRecordDetail> = z
  .object({
    id: positiveInteger,
    order_no: z.string(),
    inviter_id: positiveInteger,
    inviter_username: z.string(),
    invitee_id: positiveInteger,
    invitee_username: z.string(),
    payment_provider: z.string().default(''),
    paid_money: z.string().default(''),
    commission_rate: z.string().default(''),
    commission_base_quota: nonNegativeInteger.optional(),
    recharge_quota: nonNegativeInteger.optional(),
    commission_quota: nonNegativeInteger,
    created_at: nonNegativeInteger,
  })
  .transform(({ recharge_quota, ...record }) => ({
    ...record,
    commission_base_quota: record.commission_base_quota ?? recharge_quota ?? 0,
  }))

function pageSchema<T>(itemSchema: z.ZodType<T>): z.ZodType<PageData<T>> {
  return z.object({
    items: z.array(itemSchema),
    total: nonNegativeInteger,
    page: positiveInteger,
    page_size: positiveInteger,
  })
}

function parseApiResponse<T>(
  dataSchema: z.ZodType<T>,
  input: unknown
): ApiResponse<T> {
  const envelope = z
    .object({
      success: z.boolean(),
      data: z.unknown().optional(),
    })
    .safeParse(input)

  if (!envelope.success || !envelope.data.success) {
    return { success: false }
  }

  const data = dataSchema.safeParse(envelope.data.data)
  if (!data.success) return { success: false }

  return { success: true, data: data.data }
}

export function parseRelationPageResponse(input: unknown) {
  return parseApiResponse(pageSchema(relationSchema), input)
}

export function parseCommissionPageResponse(input: unknown) {
  return parseApiResponse(pageSchema(commissionSchema), input)
}
