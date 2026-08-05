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
export type AffiliateTab = 'relations' | 'commissions'

export interface AffiliateSearchState {
  page: number
  pageSize: number
  keyword: string
  tab: AffiliateTab
}

export interface AdminPageQueryInput {
  page: number
  pageSize: number
  keyword?: string
}

export type ApiResponse<T> =
  | {
      success: true
      data: T
    }
  | {
      success: false
    }

export interface PageData<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface AffiliateRelation {
  inviter_id: number
  username: string
  display_name: string
  invitee_count: number
  aff_quota: number
  aff_history_quota: number
  created_at: number
}

export interface CommissionRecordDetail {
  id: number
  order_no: string
  inviter_id: number
  inviter_username: string
  invitee_id: number
  invitee_username: string
  payment_provider: string
  paid_money: string
  commission_rate: string
  commission_base_quota: number
  commission_quota: number
  created_at: number
}
