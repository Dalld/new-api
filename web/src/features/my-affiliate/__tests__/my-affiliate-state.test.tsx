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
import { after, describe, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'

import type { MyAffiliateSearchState } from '../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { MyAffiliate } = await import('../index')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: {
    zh: {
      translation: {
        'No invitation records': '暂无邀请记录',
        'No commission records': '暂无返佣记录',
        'Failed to load referral data': '加载推荐数据失败',
        'Total: {{count}}': '共 {{count}} 条',
      },
    },
  },
  interpolation: { escapeValue: false },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const emptyPage = {
  success: true,
  data: { page: 1, page_size: 10, total: 0, items: [] },
}

function createAffiliateQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(['my-affiliate', 'code'], {
    success: true,
    data: '',
  })
  queryClient.setQueryData(['my-affiliate', 'summary'], {
    success: true,
    data: {
      quota: 0,
      aff_count: 0,
      aff_quota: 0,
      aff_history_quota: 0,
    },
  })
  queryClient.setQueryData(['my-affiliate', 'transfer-policy'], {
    success: true,
    data: { payment_compliance_confirmed: true },
  })
  queryClient.setQueryData(['my-affiliate', 'recharge-total'], {
    success: true,
    data: 0,
  })
  return queryClient
}

async function waitFor(condition: () => boolean, failureMessage: string) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (condition()) return
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
  }
  assert.fail(failureMessage)
}

describe('my affiliate request states', () => {
  after(() => domWindow.close())

  test('renders distinct empty states and a valid zero-result pager', async () => {
    const queryClient = createAffiliateQueryClient()
    queryClient.setQueryData(['my-affiliate', 'invitees', 1, 10, ''], emptyPage)
    queryClient.setQueryData(
      ['my-affiliate', 'commissions', 1, 10, ''],
      emptyPage
    )
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const render = async (search: MyAffiliateSearchState) =>
      act(async () =>
        root.render(
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <MyAffiliate search={search} onSearchChange={() => undefined} />
            </I18nextProvider>
          </QueryClientProvider>
        )
      )

    try {
      const baseSearch = { page: 1, pageSize: 10, keyword: '' }
      await render({ ...baseSearch, tab: 'invitees' })
      await waitFor(
        () => container.textContent?.includes('暂无邀请记录') === true,
        'invitee empty state was not rendered'
      )
      assert.match(container.textContent ?? '', /共 0 条/)
      assert.doesNotMatch(container.textContent ?? '', /加载推荐数据失败/)

      await render({ ...baseSearch, tab: 'commissions' })
      await waitFor(
        () => container.textContent?.includes('暂无返佣记录') === true,
        'commission empty state was not rendered'
      )
      assert.match(container.textContent ?? '', /共 0 条/)
      assert.doesNotMatch(container.textContent ?? '', /加载推荐数据失败/)
    } finally {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
    }
  })

  test('hides the pager for invalid responses', async () => {
    const queryClient = createAffiliateQueryClient()
    queryClient.setQueryData(['my-affiliate', 'invitees', 1, 10, ''], {
      success: false,
    })
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    try {
      await act(async () =>
        root.render(
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <MyAffiliate
                search={{ tab: 'invitees', page: 1, pageSize: 10, keyword: '' }}
                onSearchChange={() => undefined}
              />
            </I18nextProvider>
          </QueryClientProvider>
        )
      )
      await waitFor(
        () => container.textContent?.includes('加载推荐数据失败') === true,
        'error state was not rendered'
      )
      assert.doesNotMatch(container.textContent ?? '', /共 0 条/)
    } finally {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
    }
  })

  test('corrects an empty page beyond the available result range', async () => {
    const queryClient = createAffiliateQueryClient()
    queryClient.setQueryData(['my-affiliate', 'invitees', 999, 10, ''], {
      success: true,
      data: { ...emptyPage.data, page: 999 },
    })
    const changes: MyAffiliateSearchState[] = []
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    try {
      await act(async () =>
        root.render(
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <MyAffiliate
                search={{
                  tab: 'invitees',
                  page: 999,
                  pageSize: 10,
                  keyword: '',
                }}
                onSearchChange={(next) => changes.push(next)}
              />
            </I18nextProvider>
          </QueryClientProvider>
        )
      )
      await waitFor(
        () => changes.length > 0,
        'out-of-range page was not corrected'
      )
      assert.equal(changes[0]?.page, 1)
    } finally {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
    }
  })
})
