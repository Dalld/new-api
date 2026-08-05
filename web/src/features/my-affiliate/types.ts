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
export type MyAffiliateTab = 'invitees' | 'commissions'

export interface MyAffiliateSearchState {
  page: number
  pageSize: number
  keyword: string
  tab: MyAffiliateTab
}

export interface PageQueryInput {
  page: number
  pageSize: number
  keyword?: string
}

export interface ApiResponse<T> {
  success: boolean
  message?: string
  data?: T
}

export interface PageData<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface SelfAffiliateSummary {
  quota: number
  aff_count: number
  aff_quota: number
  aff_history_quota: number
}

export interface AffiliateTransferRequest {
  quota: number
}

export type AffiliateTransferResponse = ApiResponse<null>

export interface AffiliateTransferPolicy {
  payment_compliance_confirmed: boolean
}

export interface SelfInvitee {
  id: number
  username: string
  display_name: string
  created_at: number
}

export interface SelfCommissionRecord {
  id: number
  order_no: string
  invitee_username: string
  commission_base_quota: number
  commission_quota: number
  created_at: number
}
