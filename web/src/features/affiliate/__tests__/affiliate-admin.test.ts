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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { api } from '@/lib/api'

import { getAffiliateRelations, getCommissionRecords } from '../api'
import { buildAdminPageQuery, formatCommissionRate } from '../lib'
import {
  parseCommissionPageResponse,
  parseRelationPageResponse,
} from '../schemas'

describe('affiliate admin helpers', () => {
  test('builds bounded pagination parameters', () => {
    assert.equal(
      buildAdminPageQuery({ page: 3, pageSize: 20, keyword: ' order-42 ' }),
      'p=3&page_size=20&keyword=order-42'
    )
    assert.equal(formatCommissionRate('0.125'), '12.5%')
    assert.equal(formatCommissionRate('not-a-rate'), '-')
  })
})

describe('affiliate admin requests', () => {
  test('uses private local error handling for both administrator requests', async () => {
    const originalAdapter = api.defaults.adapter
    const requestedUrls: string[] = []
    api.defaults.adapter = async (config) => {
      requestedUrls.push(config.url ?? '')
      assert.equal(config.skipBusinessError, true)
      assert.equal(config.skipErrorHandler, true)
      return {
        config,
        data: {
          success: true,
          data: { page: 1, page_size: 10, total: 0, items: [] },
        },
        headers: {},
        status: 200,
        statusText: 'OK',
      }
    }

    try {
      const query = { page: 1, pageSize: 10 }
      assert.equal((await getAffiliateRelations(query)).success, true)
      assert.equal((await getCommissionRecords(query)).success, true)
      assert.deepEqual(requestedUrls, [
        '/api/affiliate/relations?p=1&page_size=10',
        '/api/affiliate/commissions?p=1&page_size=10',
      ])
    } finally {
      api.defaults.adapter = originalAdapter
    }
  })
})

describe('affiliate admin response privacy', () => {
  test('accepts the live successful empty commissions response', () => {
    assert.deepEqual(
      parseCommissionPageResponse({
        success: true,
        data: {
          page: 1,
          page_size: 10,
          total: 0,
          items: [],
        },
      }),
      {
        success: true,
        data: {
          page: 1,
          page_size: 10,
          total: 0,
          items: [],
        },
      }
    )
  })

  test('rejects malformed success envelopes instead of treating them as empty', () => {
    assert.deepEqual(parseCommissionPageResponse({ success: true }), {
      success: false,
    })
    assert.deepEqual(
      parseRelationPageResponse({
        success: true,
        data: { page: 1, page_size: 10, total: 0 },
      }),
      { success: false }
    )
  })

  test('drops backend failure details from parsed responses', () => {
    assert.deepEqual(
      parseCommissionPageResponse({
        success: false,
        message: 'database host and credentials',
      }),
      { success: false }
    )
  })

  test('keeps only relationship summary fields', () => {
    const response = parseRelationPageResponse({
      success: true,
      data: {
        items: [
          {
            inviter_id: 2,
            username: 'inviter',
            display_name: 'Inviter',
            invitee_count: 4,
            aff_quota: 10_000,
            aff_history_quota: 30_000,
            commission_base_quota: 50_000,
            commission_quota: 10_000,
            created_at: 1_700_000_000,
            email: 'private@example.com',
            aff_code: 'private-code',
            password: 'hash',
          },
        ],
        total: 1,
        page: 1,
        page_size: 10,
      },
    })

    assert.ok(response.success)
    assert.deepEqual(response.data.items, [
      {
        inviter_id: 2,
        username: 'inviter',
        display_name: 'Inviter',
        invitee_count: 4,
        aff_quota: 10_000,
        aff_history_quota: 30_000,
        created_at: 1_700_000_000,
      },
    ])
  })

  test('retains audit fields and strips provider internals', () => {
    const response = parseCommissionPageResponse({
      success: true,
      data: {
        items: [
          {
            id: 9,
            order_no: 'order-9',
            inviter_id: 2,
            inviter_username: 'inviter',
            invitee_id: 5,
            invitee_username: 'invitee',
            payment_provider: 'stripe',
            paid_money: '12.00',
            commission_rate: '0.125',
            commission_base_quota: 600_000,
            commission_quota: 75_000,
            created_at: 1_700_000_200,
            raw_error: 'private upstream detail',
            api_key: 'secret',
          },
        ],
        total: 1,
        page: 1,
        page_size: 10,
      },
    })

    assert.ok(response.success)
    assert.deepEqual(response.data.items, [
      {
        id: 9,
        order_no: 'order-9',
        inviter_id: 2,
        inviter_username: 'inviter',
        invitee_id: 5,
        invitee_username: 'invitee',
        payment_provider: 'stripe',
        paid_money: '12.00',
        commission_rate: '0.125',
        commission_base_quota: 600_000,
        commission_quota: 75_000,
        created_at: 1_700_000_200,
      },
    ])
  })
})
