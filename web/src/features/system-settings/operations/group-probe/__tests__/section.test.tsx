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
import { after, beforeEach, describe, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
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

const apiCalls: Array<{ method: string; body?: unknown }> = []
const toastErrors: string[] = []
let putFailureMessage = ''
let postFailureMessage = ''
const bunTestModule = 'bun:test'
const { mock } = await import(bunTestModule)

mock.module('@/lib/api', () => ({
  api: {
    get: async () => ({
      data: {
        success: true,
        data: {
          enabled: true,
          interval_minutes: 10,
          retention_days: 7,
          timeout_seconds: 45,
          groups: [
            {
              group: 'codex',
              display_name: 'Codex',
              model: 'gpt-5.5',
              public: true,
            },
          ],
        },
      },
    }),
    put: async (_url: string, body: unknown) => {
      apiCalls.push({ method: 'PUT', body })
      if (putFailureMessage) {
        return { data: { success: false, message: putFailureMessage } }
      }
      return { data: { success: true, data: body } }
    },
    post: async () => {
      apiCalls.push({ method: 'POST' })
      if (postFailureMessage) {
        return { data: { success: false, message: postFailureMessage } }
      }
      return {
        data: {
          success: true,
          data: { task_id: 'group-probe-task-1', created: true },
        },
      }
    },
  },
}))

mock.module('sonner', () => ({
  toast: {
    success: () => undefined,
    error: (message: string) => toastErrors.push(message),
  },
}))

const { act, useState } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { SettingsPageProvider } =
  await import('../../../components/settings-page-context')
const { GroupProbeSettingsSection } = await import('../index')

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

function Harness() {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)

  return (
    <I18nextProvider i18n={i18n}>
      <div ref={setActionsContainer} data-testid='settings-actions' />
      <SettingsPageProvider
        actionsContainer={actionsContainer}
        suppressSectionHeader={false}
      >
        <GroupProbeSettingsSection />
      </SettingsPageProvider>
    </I18nextProvider>
  )
}

async function renderSection() {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(<Harness />)
    await Promise.resolve()
    await Promise.resolve()
  })
  return {
    container,
    cleanup: async () => {
      await act(async () => root.unmount())
      container.remove()
    },
  }
}

function findButton(container: HTMLElement, label: string) {
  const button = [...container.querySelectorAll('button')].find((candidate) =>
    candidate.textContent?.includes(label)
  )
  assert.ok(button, `Expected button containing ${label}`)
  return button
}

function setInput(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    domWindow.HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(setter)
  setter.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('group probe settings section', () => {
  after(() => domWindow.close())
  beforeEach(() => {
    apiCalls.length = 0
    toastErrors.length = 0
    putFailureMessage = ''
    postFailureMessage = ''
  })

  test('adds and removes group mappings', async () => {
    const rendered = await renderSection()
    assert.equal(
      rendered.container.querySelectorAll('[data-testid="group-probe-mapping"]')
        .length,
      1
    )

    await act(async () => findButton(rendered.container, 'Add group').click())
    assert.equal(
      rendered.container.querySelectorAll('[data-testid="group-probe-mapping"]')
        .length,
      2
    )

    const deleteButton = rendered.container.querySelector<HTMLButtonElement>(
      'button[aria-label="Delete group"]'
    )
    assert.ok(deleteButton)
    await act(async () => deleteButton.click())
    assert.equal(
      rendered.container.querySelectorAll('[data-testid="group-probe-mapping"]')
        .length,
      1
    )
    await rendered.cleanup()
  })

  test('blocks duplicate groups before making the PUT request', async () => {
    const rendered = await renderSection()
    await act(async () => findButton(rendered.container, 'Add group').click())

    const secondGroup = rendered.container.querySelector<HTMLInputElement>(
      '#group-probe-1-group'
    )
    const secondDisplayName =
      rendered.container.querySelector<HTMLInputElement>(
        '#group-probe-1-display_name'
      )
    const secondModel = rendered.container.querySelector<HTMLInputElement>(
      '#group-probe-1-model'
    )
    assert.ok(secondGroup && secondDisplayName && secondModel)
    await act(async () => {
      setInput(secondGroup, ' codex ')
      setInput(secondDisplayName, 'Duplicate')
      setInput(secondModel, 'gpt-5.5')
    })

    await act(async () =>
      findButton(rendered.container, 'Save Changes').click()
    )
    assert.match(rendered.container.textContent ?? '', /Group must be unique/)
    assert.equal(
      apiCalls.some((call) => call.method === 'PUT'),
      false
    )
    await rendered.cleanup()
  })

  test('queues a manual run and displays its command task id', async () => {
    const rendered = await renderSection()

    await act(async () => {
      findButton(rendered.container, 'Run probe now').click()
      await Promise.resolve()
    })

    assert.equal(
      apiCalls.some((call) => call.method === 'POST'),
      true
    )
    const status = rendered.container.querySelector('[role="status"]')
    assert.match(status?.textContent ?? '', /group-probe-task-1/)
    await rendered.cleanup()
  })

  test('localizes save failures instead of exposing backend messages', async () => {
    putFailureMessage = 'no enabled ability for configured group'
    const rendered = await renderSection()

    const displayName = rendered.container.querySelector<HTMLInputElement>(
      '#group-probe-0-display_name'
    )
    assert.ok(displayName)
    await act(async () => setInput(displayName, 'Codex updated'))
    await act(async () => {
      findButton(rendered.container, 'Save Changes').click()
      await Promise.resolve()
    })

    assert.equal(toastErrors.at(-1), 'Failed to save probe settings')
    assert.equal(toastErrors.includes(putFailureMessage), false)
    await rendered.cleanup()
  })

  test('localizes manual run failures instead of exposing backend messages', async () => {
    postFailureMessage = 'internal scheduler unavailable'
    const rendered = await renderSection()

    await act(async () => {
      findButton(rendered.container, 'Run probe now').click()
      await Promise.resolve()
    })

    assert.equal(toastErrors.at(-1), 'Failed to run group probe')
    assert.equal(toastErrors.includes(postFailureMessage), false)
    await rendered.cleanup()
  })
})
