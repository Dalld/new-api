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

import { Window } from 'happy-dom'

import type { PublicGroupProbeStatus } from '../types'

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
const { GroupProbeStatusContent } = await import('../content')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
  interpolation: { escapeValue: false },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const generatedAt = 2_000_001_600
const buckets = Array.from({ length: 144 }, (_, index) => ({
  startedAt: generatedAt - (143 - index) * 600,
  state: index < 46 ? ('healthy' as const) : ('degraded' as const),
  sampleCount: 1,
}))

const healthyData: PublicGroupProbeStatus = {
  generatedAt,
  groups: [
    {
      group: 'codex',
      displayName: 'Codex',
      model: 'gpt-5.5',
      state: 'healthy',
      availability: 0.975,
      averageLatencyMs: 821.4,
      sampleCount: 41,
      latestCheckedAt: generatedAt - 30,
      stale: false,
      intervalMinutes: 10,
      buckets,
    },
  ],
}
const healthyGroup = healthyData.groups[0]
assert.ok(healthyGroup)

function Harness(props: Parameters<typeof GroupProbeStatusContent>[0]) {
  return (
    <I18nextProvider i18n={i18n}>
      <GroupProbeStatusContent {...props} />
    </I18nextProvider>
  )
}

async function renderStatus(
  props: Partial<Parameters<typeof GroupProbeStatusContent>[0]> = {}
) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const render = async (
    nextProps: Partial<Parameters<typeof GroupProbeStatusContent>[0]>
  ) =>
    act(async () =>
      root.render(
        <Harness
          isPending={false}
          isError={false}
          isFetching={false}
          refetch={() => undefined}
          {...nextProps}
        />
      )
    )
  await render(props)
  return {
    container,
    rerender: render,
    cleanup: async () => {
      await act(async () => root.unmount())
      container.remove()
    },
  }
}

describe('public group probe status page', () => {
  after(() => domWindow.close())

  test('renders loading, error, and empty states', async () => {
    const loading = await renderStatus({ isPending: true })
    assert.ok(loading.container.querySelector('[aria-label="Loading status"]'))
    await loading.cleanup()

    const error = await renderStatus({ isError: true })
    assert.match(error.container.textContent ?? '', /temporarily unavailable/i)
    assert.equal(
      error.container.textContent?.includes('No public probe groups'),
      false
    )
    await error.cleanup()

    const empty = await renderStatus({ data: { generatedAt, groups: [] } })
    assert.match(empty.container.textContent ?? '', /No public probe groups/i)
    await empty.cleanup()
  })

  test('renders text-equivalent state, metrics, and the backend bucket count', async () => {
    const rendered = await renderStatus({ data: healthyData })
    const row = rendered.container.querySelector(
      '[data-testid="group-status-row"]'
    )
    assert.ok(row)
    assert.equal(row.getAttribute('data-state'), 'healthy')
    assert.match(row.textContent ?? '', /Healthy/)
    assert.match(row.textContent ?? '', /97\.5%/)
    assert.match(row.textContent ?? '', /821 ms/)
    assert.equal(row.querySelectorAll('[tabindex="0"]').length, 144)
    assert.ok(row.querySelector('[data-testid="status-timeline-scroll"]'))
    const grid = row.querySelector<HTMLElement>(
      '[data-testid="status-timeline-grid"]'
    )
    assert.ok(grid)
    assert.match(grid.style.gridTemplateColumns, /repeat\(144,/)
    assert.equal(grid.style.minWidth, '1724px')
    await rendered.cleanup()
  })

  test('renders 5, 30, and 60 minute timelines without forcing 48 buckets', async () => {
    for (const [intervalMinutes, bucketCount] of [
      [5, 288],
      [30, 48],
      [60, 24],
    ] as const) {
      const variableBuckets = Array.from(
        { length: bucketCount },
        (_, index) => ({
          startedAt:
            generatedAt - (bucketCount - 1 - index) * intervalMinutes * 60,
          state: 'healthy' as const,
          sampleCount: 1,
        })
      )
      const rendered = await renderStatus({
        data: {
          generatedAt,
          groups: [
            {
              ...healthyGroup,
              intervalMinutes,
              buckets: variableBuckets,
            },
          ],
        },
      })
      const grid = rendered.container.querySelector<HTMLElement>(
        '[data-testid="status-timeline-grid"]'
      )
      assert.ok(grid)
      assert.equal(grid.querySelectorAll('[tabindex="0"]').length, bucketCount)
      assert.match(
        grid.style.gridTemplateColumns,
        new RegExp(`repeat\\(${bucketCount},`)
      )
      await rendered.cleanup()
    }
  })

  test('opens at the current result without overriding later user scrolling', async () => {
    const prototype = domWindow.HTMLElement.prototype
    const originalScrollWidth = Object.getOwnPropertyDescriptor(
      prototype,
      'scrollWidth'
    )
    const originalClientWidth = Object.getOwnPropertyDescriptor(
      prototype,
      'clientWidth'
    )
    Object.defineProperty(prototype, 'scrollWidth', {
      configurable: true,
      get: () => 1800,
    })
    Object.defineProperty(prototype, 'clientWidth', {
      configurable: true,
      get: () => 600,
    })

    let rendered: Awaited<ReturnType<typeof renderStatus>> | undefined
    try {
      rendered = await renderStatus({ data: healthyData })
      const timeline = rendered.container.querySelector<HTMLElement>(
        '[data-testid="status-timeline-scroll"]'
      )
      assert.ok(timeline)
      assert.equal(timeline.scrollLeft, 1200)

      timeline.scrollLeft = 320
      timeline.dispatchEvent(new Event('scroll'))
      await rendered.rerender({
        data: {
          ...healthyData,
          generatedAt: generatedAt + 30,
          groups: [
            {
              ...healthyGroup,
              buckets: healthyGroup.buckets.map((bucket) => ({ ...bucket })),
            },
          ],
        },
      })
      assert.equal(timeline.scrollLeft, 320)
    } finally {
      await rendered?.cleanup()
      if (originalScrollWidth) {
        Object.defineProperty(prototype, 'scrollWidth', originalScrollWidth)
      } else {
        Reflect.deleteProperty(prototype, 'scrollWidth')
      }
      if (originalClientWidth) {
        Object.defineProperty(prototype, 'clientWidth', originalClientWidth)
      } else {
        Reflect.deleteProperty(prototype, 'clientWidth')
      }
    }
  })

  test('distinguishes stale and partial data notices', async () => {
    const stale = await renderStatus({
      data: {
        ...healthyData,
        groups: [{ ...healthyGroup, stale: true }],
      },
    })
    assert.match(stale.container.textContent ?? '', /Some results are stale/)
    await stale.cleanup()

    const partial = await renderStatus({
      data: {
        ...healthyData,
        groups: [
          {
            ...healthyGroup,
            state: 'unknown',
            sampleCount: 1,
          },
        ],
      },
    })
    assert.match(partial.container.textContent ?? '', /Partial history/)
    assert.match(partial.container.textContent ?? '', /Unknown/)
    await partial.cleanup()
  })

  test('does not render protected backend details', async () => {
    const rendered = await renderStatus({ data: healthyData })
    const text = rendered.container.textContent ?? ''
    for (const protectedTerm of [
      'channel_id',
      'channel name',
      'task_id',
      'error_message',
      'api key',
    ]) {
      assert.equal(text.toLowerCase().includes(protectedTerm), false)
    }
    await rendered.cleanup()
  })
})
