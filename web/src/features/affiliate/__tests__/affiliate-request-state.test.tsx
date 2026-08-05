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

import { api } from '@/lib/api'

import type { AffiliateSearchState } from '../types'

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
const { Affiliate } = await import('../index')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: {
    zh: {
      translation: {
        'Affiliate Management': '返佣管理',
        'Referral Summary': '邀请汇总',
        'Commission Details': '返佣明细',
        'Failed to load affiliate data': '加载推荐计划数据失败',
        Retry: '重试',
        'No data': '暂无数据',
      },
    },
  },
  interpolation: { escapeValue: false },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

async function waitForText(container: HTMLElement, text: string) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (container.textContent?.includes(text)) return
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
  }
  assert.fail(
    `Timed out waiting for ${JSON.stringify(text)} in ${JSON.stringify(container.textContent)}`
  )
}

function findButton(container: HTMLElement, text: string) {
  const button = [...container.querySelectorAll('button')].find((candidate) =>
    candidate.textContent?.includes(text)
  )
  assert.ok(button, `Expected button ${JSON.stringify(text)}`)
  return button
}

describe('affiliate administrator request states', () => {
  after(() => domWindow.close())

  test('isolates tab failures and retries only the active request', async () => {
    const originalAdapter = api.defaults.adapter
    let relationRequests = 0
    let commissionRequests = 0
    api.defaults.adapter = async (config) => {
      const isRelations = config.url?.startsWith('/api/affiliate/relations')
      if (isRelations) relationRequests += 1
      else commissionRequests += 1

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

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(['affiliate', 'relations', 1, 10, ''], {
      success: false,
    })
    queryClient.setQueryData(['affiliate', 'commissions', 1, 10, ''], {
      success: true,
      data: { page: 1, page_size: 10, total: 0, items: [] },
    })
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const renderAffiliate = async (search: AffiliateSearchState) =>
      act(async () =>
        root.render(
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <Affiliate search={search} onSearchChange={() => undefined} />
            </I18nextProvider>
          </QueryClientProvider>
        )
      )
    const baseSearch = {
      page: 1,
      pageSize: 10,
      keyword: '',
    }

    try {
      await renderAffiliate({ ...baseSearch, tab: 'relations' })

      await waitForText(container, '加载推荐计划数据失败')
      assert.equal(relationRequests, 0)
      assert.equal(commissionRequests, 0)

      await renderAffiliate({ ...baseSearch, tab: 'commissions' })
      await waitForText(container, '暂无数据')
      assert.equal(
        container.textContent?.includes('加载推荐计划数据失败'),
        false
      )
      assert.equal(commissionRequests, 0)

      await renderAffiliate({ ...baseSearch, tab: 'relations' })
      await waitForText(container, '加载推荐计划数据失败')
      const requestsBeforeRetry = relationRequests
      await act(async () => findButton(container, '重试').click())
      await waitForText(container, '暂无数据')
      assert.equal(relationRequests, requestsBeforeRetry + 1)
      assert.equal(commissionRequests, 0)
    } finally {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
      api.defaults.adapter = originalAdapter
    }
  })
})
