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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

import { buildPageQuery, createInviteLink } from '../lib'
import {
  parseSelfCommissionPageResponse,
  parseSelfInviteePageResponse,
} from '../schemas'

const componentSource = readFileSync(
  new URL('../index.tsx', import.meta.url),
  'utf8'
)
const apiSource = readFileSync(new URL('../api.ts', import.meta.url), 'utf8')
const transferHookSource = readFileSync(
  new URL('../hooks/use-affiliate-transfer.ts', import.meta.url),
  'utf8'
)

describe('my affiliate query helpers', () => {
  test('normalizes pagination and trims the search term', () => {
    assert.equal(
      buildPageQuery({
        page: 2,
        pageSize: 25,
        keyword: '  alice@example.com  ',
      }),
      'p=2&page_size=25&keyword=alice%40example.com'
    )
    assert.equal(
      buildPageQuery({ page: -2, pageSize: 500, keyword: '   ' }),
      'p=1&page_size=100'
    )
  })

  test('builds the current sign-up route and encodes the invitation code', () => {
    assert.equal(
      createInviteLink('https://example.com/dashboard', 'A B/?'),
      'https://example.com/sign-up?aff=A%20B%2F%3F'
    )
    assert.equal(createInviteLink('https://example.com', ''), '')
  })
})

describe('my affiliate response privacy', () => {
  test('accepts empty invitee and commission pages as successful responses', () => {
    const emptyPage = {
      success: true,
      data: {
        items: [],
        total: 0,
        page: 1,
        page_size: 10,
      },
    }

    assert.deepEqual(parseSelfInviteePageResponse(emptyPage), emptyPage)
    assert.deepEqual(parseSelfCommissionPageResponse(emptyPage), emptyPage)
  })

  test('rejects null items instead of hiding an invalid API response', () => {
    const invalidPage = {
      success: true,
      data: {
        items: null,
        total: 0,
        page: 1,
        page_size: 10,
      },
    }

    assert.deepEqual(parseSelfInviteePageResponse(invalidPage), {
      success: false,
    })
    assert.deepEqual(parseSelfCommissionPageResponse(invalidPage), {
      success: false,
    })
    assert.deepEqual(parseSelfInviteePageResponse({ success: true }), {
      success: false,
    })
  })

  test('keeps only invitee fields required by the page', () => {
    const response = parseSelfInviteePageResponse({
      success: true,
      data: {
        items: [
          {
            id: 7,
            username: 'invitee',
            display_name: 'Invitee',
            created_at: 1_700_000_000,
            email: 'private@example.com',
            inviter_id: 3,
            access_token: 'secret',
          },
        ],
        total: 1,
        page: 1,
        page_size: 10,
      },
    })

    assert.deepEqual(response.data?.items, [
      {
        id: 7,
        username: 'invitee',
        display_name: 'Invitee',
        created_at: 1_700_000_000,
      },
    ])
  })

  test('maps the frozen base quota and drops internal commission fields', () => {
    const response = parseSelfCommissionPageResponse({
      success: true,
      data: {
        items: [
          {
            id: 11,
            order_no: 'order-11',
            invitee_id: 7,
            invitee_username: 'invitee',
            recharge_quota: 500_000,
            commission_quota: 50_000,
            created_at: 1_700_000_100,
            payment_provider: 'stripe',
            raw_error: 'must never reach the page',
          },
        ],
        total: 1,
        page: 1,
        page_size: 10,
      },
    })

    assert.deepEqual(response.data?.items, [
      {
        id: 11,
        order_no: 'order-11',
        invitee_username: 'invitee',
        commission_base_quota: 500_000,
        commission_quota: 50_000,
        created_at: 1_700_000_100,
      },
    ])
  })
})

describe('my affiliate referral card', () => {
  test('keeps the black and gold card with icon copy actions', () => {
    assert.match(
      componentSource,
      /linear-gradient\(135deg, #1a1a1a 0%, #2d2410 40%, #1a1a1a 100%\)/
    )
    assert.equal(componentSource.match(/<CopyButton/g)?.length, 2)
    assert.doesNotMatch(componentSource, /navigator\.clipboard|from 'sonner'/)
  })

  test('renders all four current referral statistics', () => {
    for (const label of [
      `t('Invites')`,
      `t('Pending Rebate')`,
      `t('Total Rebate')`,
      `t('Referred Recharge Total')`,
    ]) {
      assert.ok(componentSource.includes(label), `missing statistic: ${label}`)
    }
  })

  test('does not present unavailable financial statistics as zero', () => {
    assert.match(componentSource, /const statsUnavailable =/)
    assert.match(
      componentSource,
      /summary \? String\(summary\.aff_count\) : '-'/
    )
    assert.match(
      componentSource,
      /rechargeTotal == null \? '-' : formatQuota\(rechargeTotal\)/
    )
    assert.match(componentSource, /t\('Failed to load referral data'\)/)
  })

  test('preserves URL state, mobile records, errors, and pagination', () => {
    assert.match(componentSource, /search: MyAffiliateSearchState/)
    assert.match(
      componentSource,
      /onSearchChange\(\{ \.\.\.search, \.\.\.patch \}\)/
    )
    assert.match(componentSource, /className='space-y-2 md:hidden'/)
    assert.match(componentSource, /activeQuery\.isError \|\| isInvalidResponse/)
    assert.match(componentSource, /t\('No invitation records'\)/)
    assert.match(componentSource, /t\('No commission records'\)/)
    assert.doesNotMatch(
      componentSource,
      /<EmptyState message=\{t\('No data'\)\}/
    )
    assert.match(componentSource, /\{activePage \? \(/)
    assert.match(componentSource, /total=\{activePage\.total\}/)
    assert.doesNotMatch(componentSource, /activePage\?\.total \?\? 0/)
    assert.match(componentSource, /search\.page > totalPages/)
    assert.match(componentSource, /<Pager/)
  })

  test('uses bounded responsive tracks in the referral card', () => {
    assert.match(
      componentSource,
      /className='grid min-w-0 gap-3 sm:grid-cols-2'/
    )
    assert.match(
      componentSource,
      /className='grid min-w-0 grid-cols-2 gap-2 lg:w-80/
    )
    assert.match(componentSource, /className='h-9 min-w-0 flex-1/)
  })

  test('owns the balance transfer workflow inside the pending rebate tile', () => {
    assert.match(componentSource, /key === 'pending'/)
    assert.match(componentSource, /t\('Transfer to Balance'\)/)
    assert.match(componentSource, /<ArrowRightLeft/)
    assert.match(componentSource, /<TransferDialog/)
    assert.match(
      componentSource,
      /availableQuota=\{summary\?\.aff_quota \?\? 0\}/
    )
  })

  test('refreshes the authoritative summary and global user after transfer', () => {
    assert.match(componentSource, /queryClient\.setQueryData/)
    assert.match(componentSource, /queryClient\.invalidateQueries/)
    assert.match(componentSource, /syncAuthenticatedUser\(refreshed\.data\)/)
    assert.match(componentSource, /setAuthUser\(\{/)
  })
})

describe('my affiliate transfer API', () => {
  test('uses the existing endpoint and handles messages locally', () => {
    assert.match(apiSource, /'\/api\/user\/aff_transfer'/)
    assert.match(apiSource, /skipBusinessError: true/)
    assert.match(apiSource, /skipErrorHandler: true/)
    assert.match(transferHookSource, /i18next\.t\('Transfer failed'\)/)
    assert.match(transferHookSource, /i18next\.t\('Transfer successful'\)/)
    assert.doesNotMatch(transferHookSource, /response\.message/)
  })
})
