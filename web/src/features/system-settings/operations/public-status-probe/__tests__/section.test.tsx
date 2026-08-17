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
import { after, afterEach, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import type { PublicStatusProbeConfig } from '../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLFormElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'KeyboardEvent',
  'PointerEvent',
  'MouseEvent',
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
domWindow.HTMLElement.prototype.scrollIntoView = () => undefined

const bunTestModule = 'bun:test'
const { mock } = await import(bunTestModule)
const isolatedApi = {
  delete: async (..._args: unknown[]) => ({ data: {} }),
  get: async (..._args: unknown[]) => ({ data: {} }),
  post: async (..._args: unknown[]) => ({ data: {} }),
  put: async (..._args: unknown[]) => ({ data: {} }),
}
mock.module('@/lib/api', () => ({
  api: isolatedApi,
  getStatus: async () => ({}),
}))
mock.module('@/hooks', () => ({
  useMediaQuery: () => false,
}))
mock.module('@tanstack/react-router', () => ({
  useBlocker: () => ({ status: 'idle' }),
}))

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/api')
const { SettingsPageProvider } =
  await import('../../../components/settings-page-context')
const { PublicStatusProbeSettingsSection } = await import('../index')

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

type ApiMethod = (...args: unknown[]) => Promise<{ data: unknown }>
type MockableApi = {
  delete: ApiMethod
  get: ApiMethod
  post: ApiMethod
  put: ApiMethod
}
type RenderedSection = {
  actions: HTMLDivElement
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

const apiClient = api as unknown as MockableApi
const originalApi = {
  delete: apiClient.delete,
  get: apiClient.get,
  post: apiClient.post,
  put: apiClient.put,
}
let rendered: RenderedSection | null = null

const channel = {
  id: 42,
  name: 'Safe channel',
  type: 1,
  status: 1,
  models: 'gpt-5.5',
  is_multi_key: false,
  key_count: 1,
}

function makeConfig(
  overrides: Partial<PublicStatusProbeConfig> = {}
): PublicStatusProbeConfig {
  return {
    version: 7,
    enabled: true,
    ping_timeout_seconds: 8,
    chat_timeout_seconds: 45,
    degraded_latency_ms: 6_000,
    concurrency: 5,
    retention_days: 7,
    channels: [channel],
    targets: [
      {
        enabled: false,
        key: 'probe-codex',
        group: 'Primary',
        display_name: 'Codex',
        model: 'gpt-5.5',
        protocol: 'openai_responses',
        channel_id: 42,
        key_index: 0,
        channel,
      },
    ],
    ...overrides,
  }
}

function success(config: PublicStatusProbeConfig) {
  return { data: { success: true, message: '', data: config } }
}

async function waitForCondition(
  condition: () => boolean,
  failureMessage: string
) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (condition()) return
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
  }
  assert.fail(`${failureMessage}: ${document.body.textContent}`)
}

function findButton(text: string, required: true): HTMLButtonElement
function findButton(text: string, required: false): HTMLButtonElement | null
function findButton(text: string, required = true) {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.includes(text))
  if (required) assert.ok(button, `Expected button containing "${text}"`)
  return button ?? null
}

function getInput(labelText: string) {
  const label = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (candidate) => candidate.textContent?.trim() === labelText
  )
  assert.ok(label, `Expected label "${labelText}"`)
  const input = label
    .closest('[data-slot="form-item"]')
    ?.querySelector<HTMLInputElement>('input')
  assert.ok(input, `Expected input for "${labelText}"`)
  return input
}

async function changeInput(input: HTMLInputElement, value: string) {
  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(
      domWindow.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(valueSetter)
    valueSetter.call(input, value)
    input.dispatchEvent(
      new domWindow.Event('input', { bubbles: true }) as unknown as Event
    )
  })
}

async function renderSection() {
  const host = document.createElement('div')
  const actions = document.createElement('div')
  document.body.append(host, actions)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false, staleTime: Infinity },
    },
  })
  rendered = { actions, host, queryClient, root }
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <SettingsPageProvider
            actionsContainer={actions}
            suppressSectionHeader={false}
          >
            <PublicStatusProbeSettingsSection />
          </SettingsPageProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

afterEach(async () => {
  Object.assign(apiClient, originalApi)
  if (rendered) {
    await act(async () => rendered?.root.unmount())
    rendered.queryClient.clear()
    rendered.host.remove()
    rendered.actions.remove()
    rendered = null
  }
  document.body.replaceChildren()
})

after(() => domWindow.close())

describe('public status probe management section', () => {
  test('shows loading, retries a failed request, and keeps create available for an empty target list', async () => {
    let rejectFirst!: (reason: unknown) => void
    let requestCount = 0
    apiClient.get = async () => {
      requestCount += 1
      if (requestCount === 1) {
        return new Promise((_resolve, reject) => {
          rejectFirst = reject
        })
      }
      return success(makeConfig({ targets: [] }))
    }

    await renderSection()
    assert.ok(
      document.querySelector(
        '[aria-label="Loading public status probe configuration"]'
      )
    )

    await act(async () => rejectFirst(new Error('offline')))
    await waitForCondition(
      () => document.body.textContent?.includes('Failed to load') === true,
      'load error did not appear'
    )
    await act(async () => findButton('Retry', true).click())
    await waitForCondition(
      () => document.body.textContent?.includes('No probe targets') === true,
      'empty state did not appear'
    )
    assert.equal(requestCount, 2)
    assert.equal(findButton('Add target', true).disabled, false)
    assert.equal(document.body.textContent?.includes('Safe channel'), false)
  })

  test('saves global values, displays disabled groups, and maps toggle and named delete safely', async () => {
    let config = makeConfig()
    const putPayloads: Array<Record<string, unknown>> = []
    let deletedUrl = ''
    apiClient.get = async () => success(config)
    apiClient.put = async (url, payload) => {
      assert.equal(typeof url, 'string')
      assert.ok(payload && typeof payload === 'object')
      putPayloads.push(payload as Record<string, unknown>)
      if (url === '/api/public-status-probe/config') {
        config = makeConfig({
          ...(payload as PublicStatusProbeConfig),
          version: config.version + 1,
          channels: config.channels,
          targets: config.targets,
        })
      } else {
        const input = payload as Record<string, unknown>
        config = makeConfig({
          version: config.version + 1,
          targets: config.targets.map((target) => ({
            ...target,
            enabled: input.enabled as boolean,
          })),
        })
      }
      return success(config)
    }
    apiClient.delete = async (url) => {
      deletedUrl = String(url)
      config = makeConfig({ version: config.version + 1, targets: [] })
      return success(config)
    }

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('Codex') === true,
      'target did not load'
    )
    assert.equal(document.body.textContent?.includes('Disabled'), true)
    assert.equal(document.body.textContent?.includes('Primary'), true)
    assert.equal(getInput('Probe interval (seconds)').readOnly, true)

    await changeInput(getInput('Ping timeout (seconds)'), '9')
    await act(async () => findButton('Save probe settings', true).click())
    await waitForCondition(
      () => putPayloads.length === 1,
      'global save missing'
    )
    assert.equal(putPayloads[0]?.ping_timeout_seconds, 9)

    const toggle = document.querySelector<HTMLElement>(
      '[aria-label="Enable Codex"]'
    )
    assert.ok(toggle)
    await act(async () => toggle.click())
    await waitForCondition(
      () => putPayloads.length === 2,
      'toggle save missing'
    )
    assert.deepEqual(Object.keys(putPayloads[1] ?? {}).sort(), [
      'channel_id',
      'display_name',
      'enabled',
      'group',
      'key_index',
      'model',
      'protocol',
      'version',
    ])
    assert.equal('key' in (putPayloads[1] ?? {}), false)
    assert.equal('channel' in (putPayloads[1] ?? {}), false)

    const deleteButton = document.querySelector<HTMLElement>(
      '[aria-label="Delete Codex"]'
    )
    assert.ok(deleteButton)
    await act(async () => deleteButton.click())
    await waitForCondition(
      () => document.body.textContent?.includes('Delete "Codex"') === true,
      'named delete confirmation missing'
    )
    await act(async () => findButton('Delete target', true).click())
    await waitForCondition(() => deletedUrl !== '', 'delete request missing')
    assert.equal(deletedUrl, '/api/public-status-probe/targets/probe-codex')
  })

  test('disables create when no enabled safe channel candidate exists', async () => {
    apiClient.get = async () =>
      success(
        makeConfig({
          targets: [],
          channels: [{ ...channel, status: 2, name: 'Disabled channel' }],
        })
      )

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('No probe targets') === true,
      'empty state did not load'
    )
    assert.equal(findButton('Add target', true).disabled, true)
    assert.equal(
      document.body.textContent?.includes(
        'No safe channel candidates are currently available.'
      ),
      true
    )
  })

  test('blocks enabling an unavailable target and names settings switches', async () => {
    const unavailableChannel = { ...channel, status: 2, name: '' }
    const baseTarget = makeConfig().targets[0]
    assert.ok(baseTarget)
    apiClient.get = async () =>
      success(
        makeConfig({
          channels: [],
          targets: [
            {
              ...baseTarget,
              channel: unavailableChannel,
            },
          ],
        })
      )

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('Channel unavailable') === true,
      'unavailable target state did not load'
    )
    const targetSwitch = document.querySelector<HTMLElement>(
      '[aria-label="Enable Codex"]'
    )
    assert.ok(targetSwitch)
    assert.equal(
      targetSwitch.hasAttribute('disabled') ||
        targetSwitch.hasAttribute('data-disabled'),
      true
    )
    assert.equal(
      document.querySelector('label[for="public-status-probe-enabled"]')
        ?.textContent,
      'Enable public status probes'
    )
  })

  test('defaults create to an enabled candidate when disabled channels come first', async () => {
    apiClient.get = async () =>
      success(
        makeConfig({
          targets: [],
          channels: [
            { ...channel, status: 2, name: 'Disabled channel' },
            { ...channel, id: 43, name: 'Active channel' },
          ],
        })
      )

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('No probe targets') === true,
      'empty state did not load'
    )
    await act(async () => findButton('Add target', true).click())
    assert.equal(
      document.body.textContent?.includes('Active channel (#43) - Enabled'),
      true
    )
    assert.equal(
      document.body.textContent?.includes('Disabled channel (#42)'),
      false
    )
  })

  test('validates create fields, prevents duplicate submits, preserves immutable keys, and confirms dirty close', async () => {
    let config = makeConfig()
    let postCount = 0
    let resolvePost!: (value: { data: unknown }) => void
    apiClient.get = async () => success(config)
    apiClient.post = async () => {
      postCount += 1
      return new Promise((resolve) => {
        resolvePost = resolve
      })
    }
    apiClient.put = async () => success(config)

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('Codex') === true,
      'target did not load'
    )

    await act(async () => findButton('Add target', true).click())
    assert.equal(getInput('Group').getAttribute('maxlength'), null)
    assert.equal(getInput('Display name').getAttribute('maxlength'), null)
    assert.equal(getInput('Model').getAttribute('maxlength'), null)
    assert.equal(
      document.body.textContent?.includes('Safe channel (#42) - Enabled'),
      true
    )
    await act(async () => findButton('Save target', true).click())
    await waitForCondition(
      () => getInput('Group').getAttribute('aria-invalid') === 'true',
      'field validation did not appear'
    )
    assert.equal(postCount, 0)

    await changeInput(getInput('Group'), 'Secondary')
    await changeInput(getInput('Display name'), 'Claude')
    await changeInput(getInput('Model'), 'claude-sonnet')
    const saveTarget = findButton('Save target', true)
    await act(async () => {
      saveTarget.click()
      saveTarget.click()
    })
    await waitForCondition(() => postCount === 1, 'create request missing')
    assert.equal(postCount, 1)
    config = makeConfig({ version: 8 })
    await act(async () => resolvePost(success(config)))

    const edit = document.querySelector<HTMLElement>(
      '[aria-label="Edit Codex"]'
    )
    assert.ok(edit)
    await act(async () => edit.click())
    assert.equal(getInput('Immutable key').value, 'probe-codex')
    assert.equal(getInput('Immutable key').readOnly, true)
    await changeInput(getInput('Display name'), 'Codex draft')
    const beforeUnload = new domWindow.Event('beforeunload', {
      cancelable: true,
    })
    domWindow.dispatchEvent(beforeUnload)
    assert.equal(beforeUnload.defaultPrevented, true)
    assert.equal(
      document.querySelector('label[for="public-status-probe-target-enabled"]')
        ?.textContent,
      'Enabled'
    )
    await act(async () => findButton('Cancel', true).click())
    await waitForCondition(
      () =>
        document.body.textContent?.includes('Discard unsaved changes?') ===
        true,
      'dirty close confirmation missing'
    )
    assert.equal(getInput('Display name').value, 'Codex draft')
    await act(async () => findButton('Keep editing', true).click())
    assert.equal(getInput('Display name').value, 'Codex draft')
  })

  test('shows a form-level error when the backend rejects a valid target form', async () => {
    apiClient.get = async () => success(makeConfig({ targets: [] }))
    apiClient.post = async () => {
      throw { isAxiosError: true, response: { status: 400 } }
    }

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('No probe targets') === true,
      'empty target state did not load'
    )
    await act(async () => findButton('Add target', true).click())
    await changeInput(getInput('Group'), 'Primary')
    await changeInput(getInput('Display name'), 'Rejected target')
    await changeInput(getInput('Model'), 'unsupported-model')
    await act(async () => findButton('Save target', true).click())

    await waitForCondition(
      () =>
        document.body.textContent?.includes(
          'The selected channel, model, protocol, or key index is not valid for probing.'
        ) === true,
      'server validation error did not appear in the target form'
    )
    assert.equal(getInput('Display name').value, 'Rejected target')
  })

  test('refetches after a conflict without resetting the active dialog draft', async () => {
    let config = makeConfig()
    const baseTarget = config.targets[0]
    assert.ok(baseTarget)
    let getCount = 0
    let putCount = 0
    apiClient.get = async () => {
      getCount += 1
      return success(config)
    }
    const updatePayloads: Array<Record<string, unknown>> = []
    const updateUrls: string[] = []
    apiClient.put = async (url, payload) => {
      putCount += 1
      updateUrls.push(String(url))
      updatePayloads.push(payload as Record<string, unknown>)
      if (putCount > 1) {
        config = makeConfig({
          version: 9,
          targets: [
            {
              ...baseTarget,
              display_name: String(
                (payload as Record<string, unknown>).display_name
              ),
            },
          ],
        })
        return success(config)
      }
      config = makeConfig({
        version: 8,
        channels: [{ ...channel, name: 'Refreshed safe channel' }],
        targets: [{ ...baseTarget, channel: { ...channel } }],
      })
      throw { isAxiosError: true, response: { status: 409 } }
    }

    await renderSection()
    await waitForCondition(
      () => document.body.textContent?.includes('Codex') === true,
      'target did not load'
    )
    const edit = document.querySelector<HTMLElement>(
      '[aria-label="Edit Codex"]'
    )
    assert.ok(edit)
    await act(async () => edit.click())
    await changeInput(getInput('Display name'), 'Codex New')
    await act(async () => findButton('Save target', true).click())

    await waitForCondition(
      () =>
        document.body.textContent?.includes('Configuration changed') === true,
      'conflict alert missing'
    )
    assert.equal(putCount, 1)
    assert.equal(getCount, 2)
    assert.equal(getInput('Display name').value, 'Codex New')
    assert.equal(
      document.body.textContent?.includes('Refreshed safe channel'),
      true
    )
    assert.equal(document.body.textContent?.includes('base_url'), false)

    await act(async () => findButton('Save target', true).click())
    await waitForCondition(() => putCount === 2, 'second update did not run')
    assert.deepEqual(updateUrls, [
      '/api/public-status-probe/targets/probe-codex',
      '/api/public-status-probe/targets/probe-codex',
    ])
    assert.equal(updatePayloads[1]?.version, 8)
    assert.equal(updatePayloads[1]?.display_name, 'Codex New')
  })
})
