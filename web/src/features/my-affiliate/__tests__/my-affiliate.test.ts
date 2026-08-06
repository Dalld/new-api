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
            masked_username: 'inv**ee',
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
        masked_username: 'inv**ee',
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

describe('my affiliate referral plan layout', () => {
  test('uses the localized referral-plan title and a refresh action', () => {
    assert.match(
      componentSource,
      /SectionPageLayout\.Title[\s\S]*t\('Referral Program'\)/
    )
    assert.match(
      componentSource,
      /const handleRefresh = \(\) => \{[\s\S]*invalidateQueries\(\{ queryKey: \['my-affiliate'\] \}\)/
    )
    assert.match(componentSource, /aria-label=\{t\('Refresh'\)\}/)
    assert.match(componentSource, /title=\{t\('Refresh'\)\}/)
    assert.match(componentSource, /disabled=\{isRefreshing\}/)
    assert.match(componentSource, /<span>\{t\('Refresh'\)\}<\/span>/)
  })

  test('renders four semantic referral statistics with values and descriptions', () => {
    const statsBlock = componentSource.match(
      /const stats = \[([\s\S]*?)\n {2}\]\n\n {2}const isRefreshing/
    )?.[1]

    assert.ok(statsBlock, 'referral statistics block is missing')
    for (const key of ['invites', 'recharge', 'pending', 'total']) {
      assert.match(statsBlock, new RegExp(`key: '${key}'`))
    }
    for (const label of [
      `t('Total Invites')`,
      `t('Referred Recharge Total')`,
      `t('Pending Transfer')`,
      `t('Total Rebate')`,
    ]) {
      assert.ok(statsBlock.includes(label), `missing statistic: ${label}`)
    }
    assert.equal((statsBlock.match(/description: t\(/g) ?? []).length, 4)
  })

  test('renders the referral link, copy action, and reward information callout', () => {
    assert.match(componentSource, /t\('Your Referral Link'\)/)
    assert.match(componentSource, /id='affiliate-link'/)
    assert.match(componentSource, /readOnly/)
    assert.match(componentSource, /<CopyButton[\s\S]*value=\{inviteLink\}/)
    assert.match(componentSource, /tooltip=\{t\('Copy referral link'\)\}/)
    assert.match(componentSource, /aria-label=\{t\('Copy referral link'\)\}/)
    assert.match(componentSource, /t\('Referral Reward'\)/)
    assert.match(
      componentSource,
      /overview\?\.payment_compliance_confirmed/
    )
    assert.match(componentSource, /t\('Actual Recharge Rebate'\)/)
    assert.match(componentSource, /t\('Signup Bonus'\)/)
    assert.match(componentSource, /formatCommissionRate\(overview\.commission_rate\)/)
    assert.match(componentSource, /overview\.inviter_signup_reward_quota/)
    assert.match(componentSource, /overview\.invitee_signup_reward_quota/)
    assert.match(componentSource, /t\("Based on invitee's actual recharge amount"\)/)
    assert.match(componentSource, /Invitee actual recharge reward: \{\{rate\}\}/)
    assert.match(componentSource, /t\('Inviter'\)/)
    assert.match(componentSource, /t\('Invitee'\)/)
    assert.match(
      componentSource,
      /Referral rewards are currently disabled until payment compliance is confirmed\./
    )
    assert.match(componentSource, /<Info[\s\S]*aria-hidden='true'/)
  })

  test('keeps the reward history section and its two controlled tabs', () => {
    assert.match(componentSource, /t\('Reward History'\)/)
    assert.match(
      componentSource,
      /t\('Your referral records and reward status'\)/
    )
    assert.match(
      componentSource,
      /<Tabs value=\{search\.tab\} onValueChange=\{changeTab\}>/
    )
    assert.match(
      componentSource,
      /<TabsTrigger\s+[\s\S]*?value='invitees'[\s\S]*?t\('Invitees'\)/
    )
    assert.match(
      componentSource,
      /<TabsTrigger\s+[\s\S]*?value='commissions'[\s\S]*?t\('Commission Records'\)/
    )
  })

  test('does not present unavailable financial statistics as zero', () => {
    assert.match(componentSource, /const statsUnavailable =/)
    assert.match(
      componentSource,
      /overviewQuery\.data != null && !overviewQuery\.data\.success/
    )
    assert.match(
      componentSource,
      /overview \? String\(overview\.invitee_count\) : '-'/
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

  test('uses responsive full-width tracks in the referral card', () => {
    assert.match(
      componentSource,
      /className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'/
    )
    assert.match(
      componentSource,
      /className='flex w-full flex-col gap-5'/
    )
    assert.match(componentSource, /className='flex min-h-36 flex-col/)
    assert.match(componentSource, /className='min-h-\[16rem\]'/)
    assert.match(componentSource, /className='h-9 min-w-0 flex-1/)
    assert.match(componentSource, /aria-busy=\{isRefreshing\}/)
    assert.match(componentSource, /TabsList className='grid w-full grid-cols-2 sm:w-auto'/)
  })

  test('owns the balance transfer workflow inside the pending rebate tile', () => {
    assert.match(componentSource, /key: 'pending'/)
    assert.match(componentSource, /!summary \|\|\s+summary\.aff_quota <= 0/)
    assert.match(componentSource, /t\('Transfer to Balance'\)/)
    assert.match(componentSource, /<ArrowRightLeft/)
    assert.match(componentSource, /<TransferDialog/)
    assert.match(
      componentSource,
      /availableQuota=\{summary\?\.aff_quota \?\? 0\}/
    )
  })

  test('displays only the invitee ID and masked username', () => {
    assert.match(componentSource, /<TableHead>\{t\('User ID'\)\}<\/TableHead>/)
    assert.match(componentSource, /item\.masked_username \|\| '-'/)
    assert.doesNotMatch(componentSource, /item\.username/)
    assert.doesNotMatch(componentSource, /item\.display_name/)
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
