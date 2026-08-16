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

import type { PublicProbeData, PublicProbePoint } from '../types'

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
  'MouseEvent',
  'PointerEvent',
  'KeyboardEvent',
  'FocusEvent',
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
const failedPoint: PublicProbePoint = {
  checked_at: generatedAt - 60,
  state: 'validation_failed',
  ping_latency_ms: 218,
  chat_latency_ms: null,
  error_code: 'validation_failed',
}
const operationalPoint: PublicProbePoint = {
  checked_at: generatedAt,
  state: 'operational',
  ping_latency_ms: 96,
  chat_latency_ms: 842,
  error_code: null,
}

const statusData: PublicProbeData = {
  generated_at: generatedAt,
  interval_seconds: 60,
  targets: [
    {
      key: 'codex-gpt-5-5',
      group: 'codex',
      display_name: 'Codex',
      model: 'gpt-5.5',
      state: 'operational',
      availability: 0.5,
      ping_latency_ms: 96,
      chat_latency_ms: 842,
      latest_checked_at: generatedAt,
      next_check_at: generatedAt + 60,
      history: [failedPoint, operationalPoint],
    },
  ],
}
const primaryTarget = statusData.targets[0]
assert.ok(primaryTarget)

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

function getPointButton(container: HTMLElement, pointId: string) {
  const button = container.querySelector<HTMLButtonElement>(
    `[data-point-id="${pointId}"]`
  )
  assert.ok(button)
  return button
}

function getOpenDetail() {
  return document.body.querySelector<HTMLElement>(
    '[data-testid="probe-point-detail"]'
  )
}

function getAggregateText(container: HTMLElement) {
  return (
    container.querySelector('[data-testid="aggregate-status"]')?.textContent ??
    ''
  )
}

async function wait(milliseconds: number) {
  await act(
    () => new Promise<void>((resolve) => setTimeout(resolve, milliseconds))
  )
}

describe('public probe status page', () => {
  after(() => domWindow.close())

  test('keeps loading, error, empty, and partial states in a stable shell', async () => {
    const loading = await renderStatus({ isPending: true })
    assert.ok(loading.container.querySelector('[aria-label="Loading status"]'))
    assert.ok(loading.container.querySelector('[data-layout="stable"]'))
    await loading.cleanup()

    const error = await renderStatus({ isError: true })
    assert.match(error.container.textContent ?? '', /temporarily unavailable/i)
    assert.ok(error.container.querySelector('[data-layout="stable"]'))
    await error.cleanup()

    const empty = await renderStatus({
      data: { ...statusData, targets: [] },
    })
    assert.match(empty.container.textContent ?? '', /No probe targets/i)
    assert.ok(empty.container.querySelector('[data-layout="stable"]'))
    await empty.cleanup()

    const partial = await renderStatus({
      data: {
        ...statusData,
        targets: [
          primaryTarget,
          {
            ...primaryTarget,
            key: 'gemini-flash',
            display_name: 'Gemini',
            state: 'unknown',
            availability: null,
            latest_checked_at: null,
            history: [],
          },
        ],
      },
    })
    assert.match(partial.container.textContent ?? '', /Collecting data/i)
    assert.equal(
      partial.container.querySelectorAll('[data-testid="target-status-card"]')
        .length,
      2
    )
    await partial.cleanup()

    const unknown = await renderStatus({
      data: {
        ...statusData,
        targets: [
          {
            ...primaryTarget,
            state: 'unknown',
          },
        ],
      },
    })
    assert.match(
      unknown.container.querySelector('[data-testid="aggregate-status"]')
        ?.textContent ?? '',
      /Collecting data/i
    )
    await unknown.cleanup()
  })

  test('renders responsive target cards with dual latency, availability, and exactly 60 buttons', async () => {
    const rendered = await renderStatus({ data: statusData })
    const grid = rendered.container.querySelector<HTMLElement>(
      '[data-testid="status-target-grid"]'
    )
    assert.ok(grid)
    assert.match(grid.className, /md:grid-cols-2/)
    assert.match(grid.className, /xl:grid-cols-3/)

    const card = grid.querySelector<HTMLElement>(
      '[data-testid="target-status-card"]'
    )
    assert.ok(card)
    assert.match(card.textContent ?? '', /Conversation latency/i)
    assert.match(card.textContent ?? '', /842 ms/)
    assert.match(card.textContent ?? '', /Ping latency/i)
    assert.match(card.textContent ?? '', /96 ms/)
    assert.match(card.textContent ?? '', /Last 60 availability/i)
    assert.match(card.textContent ?? '', /50\.0%/)
    assert.match(card.className, /min-w-0/)

    const cardHeader = card.querySelector<HTMLElement>('header')
    assert.ok(cardHeader)
    assert.match(cardHeader.className, /flex-col/)
    assert.match(cardHeader.className, /sm:flex-row/)

    const stateBadge = cardHeader.querySelector<HTMLElement>(
      '[data-slot="badge"]'
    )
    assert.ok(stateBadge)
    assert.match(stateBadge.className, /max-w-full/)
    assert.match(stateBadge.className, /sm:max-w-\[55%\]/)

    const cardFooter = card.querySelector<HTMLElement>('footer')
    assert.ok(cardFooter)
    assert.match(cardFooter.className, /grid-cols-1/)
    assert.match(cardFooter.className, /sm:grid-cols-2/)

    const buttons = card.querySelectorAll<HTMLButtonElement>(
      '[data-testid="status-segment"]'
    )
    assert.equal(buttons.length, 60)
    assert.ok([...buttons].every((button) => button.tabIndex === 0))
    assert.ok(
      [...buttons].every(
        (button) => (button.getAttribute('aria-label') ?? '').length > 10
      )
    )

    const timelineGrid = card.querySelector<HTMLElement>(
      '[data-testid="status-timeline-grid"]'
    )
    assert.ok(timelineGrid)
    assert.match(timelineGrid.className, /sm:min-w-0/)
    assert.match(timelineGrid.className, /sm:gap-0\.5/)
    assert.equal(
      timelineGrid.style.getPropertyValue('--timeline-min-width'),
      '1676px'
    )
    assert.equal(
      timelineGrid.style.gridTemplateColumns,
      'repeat(60, minmax(0, 1fr))'
    )
    assert.match(buttons[0]?.firstElementChild?.className ?? '', /w-full/)
    assert.match(buttons[0]?.firstElementChild?.className ?? '', /max-w-2/)
    await rendered.cleanup()
  })

  test('derives aggregate status from fresh, stale, and mixed targets', async () => {
    const scenarios: Array<{
      label: RegExp
      data: PublicProbeData
    }> = [
      { label: /All systems operational/i, data: statusData },
      {
        label: /Degraded performance/i,
        data: {
          ...statusData,
          targets: [{ ...primaryTarget, state: 'degraded' }],
        },
      },
      {
        label: /Service unavailable/i,
        data: {
          ...statusData,
          targets: [{ ...primaryTarget, state: 'failed' }],
        },
      },
      {
        label: /Partial outage/i,
        data: {
          ...statusData,
          targets: [
            primaryTarget,
            { ...primaryTarget, key: 'failed-target', state: 'failed' },
          ],
        },
      },
      {
        label: /Status data is stale/i,
        data: { ...statusData, generated_at: generatedAt + 121 },
      },
      {
        label: /Collecting data/i,
        data: {
          ...statusData,
          targets: [
            {
              ...primaryTarget,
              state: 'unknown',
              availability: null,
              ping_latency_ms: null,
              chat_latency_ms: null,
              latest_checked_at: null,
              history: [],
            },
          ],
        },
      },
    ]

    for (const scenario of scenarios) {
      const rendered = await renderStatus({ data: scenario.data })
      assert.match(getAggregateText(rendered.container), scenario.label)
      await rendered.cleanup()
    }
  })

  test('refreshes on demand and disables the control while fetching', async () => {
    let refreshes = 0
    const rendered = await renderStatus({
      data: statusData,
      refetch: () => {
        refreshes++
      },
    })
    const refresh = rendered.container.querySelector<HTMLButtonElement>(
      'button[aria-label="Refresh status"]'
    )
    assert.ok(refresh)

    await act(async () => refresh.click())
    assert.equal(refreshes, 1)

    await rendered.rerender({ data: statusData, isFetching: true })
    assert.equal(refresh.disabled, true)
    assert.match(
      refresh.querySelector('svg')?.getAttribute('class') ?? '',
      /animate-spin/
    )
    await rendered.cleanup()
  })

  test('opens sanitized point details after a 100ms mouse hover', async () => {
    const rendered = await renderStatus({ data: statusData })
    const button = getPointButton(
      rendered.container,
      `codex-gpt-5-5:${failedPoint.checked_at}`
    )

    await act(async () =>
      button.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    )
    await wait(70)
    assert.equal(getOpenDetail(), null)
    await wait(50)

    const detail = getOpenDetail()
    assert.ok(detail)
    assert.equal(button.dataset.pointState, 'validation_failed')
    assert.match(detail.textContent ?? '', /Validation failed/i)
    assert.match(detail.textContent ?? '', /Conversation latency/i)
    assert.match(detail.textContent ?? '', /Ping latency/i)
    assert.match(detail.textContent ?? '', /218 ms/)
    assert.match(detail.textContent ?? '', /Response validation failed/i)
    assert.equal(detail.textContent?.includes('validation_failed'), false)
    await rendered.cleanup()
  })

  test('supports focus, Enter, Escape, touch tap, and outside dismissal', async () => {
    const rendered = await renderStatus({ data: statusData })
    const button = getPointButton(
      rendered.container,
      `codex-gpt-5-5:${operationalPoint.checked_at}`
    )

    await act(async () => button.focus())
    assert.ok(getOpenDetail())
    await act(async () =>
      button.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
      )
    )
    assert.equal(getOpenDetail(), null)

    await act(async () =>
      button.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      )
    )
    assert.ok(getOpenDetail())
    await act(async () =>
      button.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
      )
    )

    await act(async () => {
      button.dispatchEvent(
        new PointerEvent('pointerdown', {
          bubbles: true,
          pointerType: 'touch',
        })
      )
      button.click()
    })
    assert.ok(getOpenDetail())
    await act(async () =>
      document.body.dispatchEvent(
        new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })
      )
    )
    assert.equal(getOpenDetail(), null)
    await rendered.cleanup()
  })

  test('preserves an active point, focus, and horizontal scroll across refresh', async () => {
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
      get: () => 900,
    })
    Object.defineProperty(prototype, 'clientWidth', {
      configurable: true,
      get: () => 420,
    })

    let rendered: Awaited<ReturnType<typeof renderStatus>> | undefined
    try {
      rendered = await renderStatus({ data: statusData })
      const timeline = rendered.container.querySelector<HTMLElement>(
        '[data-testid="status-timeline-scroll"]'
      )
      assert.ok(timeline)
      assert.equal(timeline.scrollLeft, 480)

      timeline.scrollLeft = 190
      timeline.dispatchEvent(new Event('scroll'))
      const pointId = `codex-gpt-5-5:${operationalPoint.checked_at}`
      const button = getPointButton(rendered.container, pointId)
      await act(async () => button.focus())
      assert.ok(getOpenDetail())

      await rendered.rerender({
        data: {
          ...statusData,
          generated_at: generatedAt + 15,
          targets: statusData.targets.map((target) => ({
            ...target,
            history: target.history.map((point) => ({ ...point })),
          })),
        },
      })

      const refreshedButton = getPointButton(rendered.container, pointId)
      assert.equal(document.activeElement, refreshedButton)
      assert.equal(timeline.scrollLeft, 190)
      assert.ok(getOpenDetail())
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

  test('shows stale and mixed fresh failures without exposing private details', async () => {
    const stale = await renderStatus({
      data: {
        ...statusData,
        generated_at: generatedAt + 121,
      },
    })
    assert.match(stale.container.textContent ?? '', /Status data is stale/i)
    await stale.cleanup()

    const partial = await renderStatus({
      data: {
        ...statusData,
        targets: [
          primaryTarget,
          {
            ...primaryTarget,
            key: 'anthropic-sonnet',
            display_name: 'Anthropic',
            state: 'failed',
            latest_checked_at: generatedAt,
            history: [failedPoint],
          },
        ],
      },
    })
    assert.match(partial.container.textContent ?? '', /Partial outage/i)
    const text = partial.container.textContent?.toLowerCase() ?? ''
    for (const privateTerm of [
      'channel_id',
      'base_url',
      'provider key',
      'stack trace',
    ]) {
      assert.equal(text.includes(privateTerm), false)
    }
    await partial.cleanup()
  })
})
