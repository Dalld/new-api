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
import { api } from '@/lib/api'

import { buildPageQuery } from './lib'
import {
  parseAffiliateCodeResponse,
  parseAffiliateTransferPolicyResponse,
  parseAffiliateTransferResponse,
  parseRechargeTotalResponse,
  parseSelfAffiliateSummaryResponse,
  parseSelfAffiliateOverviewResponse,
  parseSelfCommissionPageResponse,
  parseSelfInviteePageResponse,
} from './schemas'
import type { AffiliateTransferRequest, PageQueryInput } from './types'

export async function getAffiliateCode() {
  const response = await api.get<unknown>('/api/user/aff')
  return parseAffiliateCodeResponse(response.data)
}

export async function getSelfAffiliateSummary() {
  const response = await api.get<unknown>('/api/user/self', {
    skipErrorHandler: true,
  })
  return parseSelfAffiliateSummaryResponse(response.data)
}

export async function getSelfAffiliateOverview() {
  const response = await api.get<unknown>('/api/user/aff/overview', {
    skipErrorHandler: true,
  })
  return parseSelfAffiliateOverviewResponse(response.data)
}

export async function getAffiliateTransferPolicy() {
  const response = await api.get<unknown>('/api/user/topup/info', {
    skipErrorHandler: true,
  })
  return parseAffiliateTransferPolicyResponse(response.data)
}

export async function transferAffiliateQuota(
  request: AffiliateTransferRequest
) {
  const response = await api.post<unknown>('/api/user/aff_transfer', request, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return parseAffiliateTransferResponse(response.data)
}

export async function getSelfRechargeTotal() {
  const response = await api.get<unknown>('/api/user/aff/recharge_total')
  return parseRechargeTotalResponse(response.data)
}

export async function getSelfInvitees(input: PageQueryInput) {
  const response = await api.get<unknown>(
    `/api/user/aff/invitees?${buildPageQuery(input)}`
  )
  return parseSelfInviteePageResponse(response.data)
}

export async function getSelfCommissions(input: PageQueryInput) {
  const response = await api.get<unknown>(
    `/api/user/aff/commissions?${buildPageQuery(input)}`
  )
  return parseSelfCommissionPageResponse(response.data)
}
