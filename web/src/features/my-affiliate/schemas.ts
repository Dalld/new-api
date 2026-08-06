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
  ApiResponse,
  AffiliateTransferPolicy,
  PageData,
  SelfAffiliateSummary,
  SelfAffiliateOverview,
  SelfCommissionRecord,
  SelfInvitee,
} from './types'

const nonNegativeInteger = z.number().int().nonnegative()
const positiveInteger = z.number().int().positive()

const selfAffiliateSummarySchema: z.ZodType<SelfAffiliateSummary> = z.object({
  quota: nonNegativeInteger,
  aff_count: nonNegativeInteger,
  aff_quota: nonNegativeInteger,
  aff_history_quota: nonNegativeInteger,
})

const selfAffiliateOverviewSchema: z.ZodType<SelfAffiliateOverview> = z.object({
  invitee_count: nonNegativeInteger,
  commission_rate: z.number().finite().min(0).max(1),
  inviter_signup_reward_quota: nonNegativeInteger,
  invitee_signup_reward_quota: nonNegativeInteger,
  payment_compliance_confirmed: z.boolean(),
})

const affiliateTransferPolicySchema: z.ZodType<AffiliateTransferPolicy> =
  z.object({
    payment_compliance_confirmed: z.boolean().default(true),
  })

const selfInviteeSchema: z.ZodType<SelfInvitee> = z
  .object({
    id: positiveInteger,
    masked_username: z.string(),
    created_at: nonNegativeInteger,
  })
  .strip()

const selfCommissionRecordSchema: z.ZodType<SelfCommissionRecord> = z
  .object({
    id: positiveInteger,
    order_no: z.string(),
    invitee_username: z.string(),
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
  const parsed = z
    .object({
      success: z.boolean(),
      message: z.string().optional(),
      data: dataSchema.optional(),
    })
    .safeParse(input)

  if (!parsed.success) return { success: false }
  if (parsed.data.success && parsed.data.data === undefined) {
    return { success: false }
  }
  return parsed.data
}

export function parseAffiliateCodeResponse(input: unknown) {
  return parseApiResponse(z.string(), input)
}

export function parseSelfAffiliateSummaryResponse(input: unknown) {
  return parseApiResponse(selfAffiliateSummarySchema, input)
}

export function parseSelfAffiliateOverviewResponse(input: unknown) {
  return parseApiResponse(selfAffiliateOverviewSchema, input)
}

export function parseAffiliateTransferResponse(input: unknown) {
  return parseApiResponse(z.null(), input)
}

export function parseAffiliateTransferPolicyResponse(input: unknown) {
  return parseApiResponse(affiliateTransferPolicySchema, input)
}

export function parseRechargeTotalResponse(input: unknown) {
  return parseApiResponse(nonNegativeInteger, input)
}

export function parseSelfInviteePageResponse(input: unknown) {
  return parseApiResponse(pageSchema(selfInviteeSchema), input)
}

export function parseSelfCommissionPageResponse(input: unknown) {
  return parseApiResponse(pageSchema(selfCommissionRecordSchema), input)
}
