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

import type {
  PublicProbePoint,
  PublicProbeResponse,
  PublicProbeTarget,
} from '../types'

const bunTestModule = 'bun:test'
const { mock } = await import(bunTestModule)
const apiRequests: Array<{
  url: string
  config: { skipErrorHandler?: boolean }
}> = []

mock.module('@/lib/api', () => ({
  api: {
    get: async (url: string, config: { skipErrorHandler?: boolean } = {}) => {
      apiRequests.push({ url, config })
      return {
        data: {
          success: true,
          data: { generated_at: 1, interval_seconds: 60, targets: [] },
        },
      }
    },
  },
}))

const { groupProbeStatusQueryOptions, parsePublicProbeStatus } =
  await import('../api')
const { normalizeProbeHistory, padProbeHistory } = await import('../history')

const generatedAt = 2_000_001_600

function point(
  checkedAt: number,
  overrides: Partial<PublicProbePoint> = {}
): PublicProbePoint {
  return {
    checked_at: checkedAt,
    state: 'operational',
    ping_latency_ms: 123,
    chat_latency_ms: 456,
    error_code: null,
    ...overrides,
  }
}

function response(
  history: PublicProbePoint[] = [point(generatedAt - 30)]
): PublicProbeResponse {
  return {
    success: true,
    data: {
      generated_at: generatedAt,
      interval_seconds: 60,
      targets: [
        {
          key: 'codex-gpt-5-5',
          group: 'codex',
          display_name: 'Codex',
          model: 'gpt-5.5',
          state: 'operational',
          availability: 1,
          ping_latency_ms: 123,
          chat_latency_ms: 456,
          latest_checked_at: generatedAt - 30,
          next_check_at: generatedAt + 60,
          history,
        },
      ],
    },
  }
}

describe('public probe API', () => {
  test('decodes every public state and nullable dual latencies', () => {
    const input = response([
      point(generatedAt - 60, {
        state: 'degraded',
        ping_latency_ms: null,
      }),
      point(generatedAt, {
        state: 'unknown',
        chat_latency_ms: null,
        error_code: 'timeout',
      }),
      point(generatedAt + 60, {
        state: 'validation_failed',
        error_code: 'validation_failed',
      }),
      point(generatedAt + 120, {
        state: 'failed',
        ping_latency_ms: null,
        chat_latency_ms: null,
        error_code: 'network_error',
      }),
    ])
    input.data.targets[0]!.state = 'validation_failed'
    input.data.targets[0]!.availability = null
    input.data.targets[0]!.ping_latency_ms = null
    input.data.targets[0]!.chat_latency_ms = null
    input.data.targets[0]!.latest_checked_at = null

    assert.deepEqual(parsePublicProbeStatus(input), input.data)
  })

  test('rejects unsuccessful, legacy, extra-field, and malformed responses', () => {
    const missingKeyTarget: Partial<PublicProbeTarget> = {
      ...response().data.targets[0]!,
    }
    delete missingKeyTarget.key

    const invalidResponses: unknown[] = [
      { success: false, message: 'unavailable' },
      { success: true, data: [] },
      { success: true, data: { generated_at: 1, groups: [] } },
      { ...response(), request_id: 'not-in-public-contract' },
      {
        ...response(),
        data: { ...response().data, interval_seconds: 30 },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], state: 'healthy' }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], ping_latency_ms: -1 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], availability: 1.01 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...response().data.targets[0],
              history: [point(-1)],
            },
          ],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...response().data.targets[0],
              history: [
                { ...point(generatedAt), error_code: 'raw_provider_error' },
              ],
            },
          ],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], channel_id: 42 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...response().data.targets[0],
              history: [{ ...point(generatedAt), api_key: 'secret' }],
            },
          ],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], key: ' padded-key ' }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], key: 'k'.repeat(97) }],
        },
      },
      {
        ...response(),
        data: { ...response().data, targets: {} },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...response().data.targets[0], history: {} }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: Array.from({ length: 21 }, () => response().data.targets[0]),
        },
      },
      response(
        Array.from({ length: 61 }, (_, index) => point(generatedAt + index))
      ),
      {
        ...response(),
        data: { ...response().data, generated_at: 'not-a-number' },
      },
      {
        ...response(),
        data: { ...response().data, generated_at: 0 },
      },
      {
        ...response(),
        data: { ...response().data, targets: [missingKeyTarget] },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            response().data.targets[0],
            { ...response().data.targets[0] },
          ],
        },
      },
    ]

    for (const invalidResponse of invalidResponses) {
      assert.throws(() => parsePublicProbeStatus(invalidResponse))
    }
  })

  test('sorts and defensively truncates helper input to the latest 60 points', () => {
    const history = Array.from({ length: 65 }, (_, index) =>
      point(generatedAt + index)
    ).reverse()
    const parsedHistory = normalizeProbeHistory(history)

    assert.equal(parsedHistory.length, 60)
    assert.equal(parsedHistory[0]!.checked_at, generatedAt + 5)
    assert.equal(parsedHistory.at(-1)!.checked_at, generatedAt + 64)
  })

  test('keeps the last input item for duplicate checked_at values', () => {
    const duplicate = point(generatedAt, {
      state: 'validation_failed',
      error_code: 'validation_failed',
    })
    const normalized = normalizeProbeHistory([
      point(generatedAt, { state: 'failed', error_code: 'timeout' }),
      point(generatedAt - 60),
      duplicate,
    ])

    assert.deepEqual(normalized, [point(generatedAt - 60), duplicate])
    const padded = padProbeHistory(
      [point(generatedAt), duplicate],
      'codex-gpt-5-5'
    )
    assert.equal(padded.length, 60)
    assert.equal(padded.at(-1)!.id, `codex-gpt-5-5:${generatedAt}`)
    assert.equal(padded.at(-1)!.state, 'validation_failed')
  })

  test('accepts empty and exactly 60-point API histories', () => {
    assert.deepEqual(
      parsePublicProbeStatus(response([])).targets[0]!.history,
      []
    )

    const exactHistory = Array.from({ length: 60 }, (_, index) =>
      point(generatedAt + index)
    ).reverse()
    const parsed = parsePublicProbeStatus(response(exactHistory))
    assert.equal(parsed.targets[0]!.history.length, 60)
    assert.equal(parsed.targets[0]!.history[0]!.checked_at, generatedAt)
    assert.equal(
      parsed.targets[0]!.history.at(-1)!.checked_at,
      generatedAt + 59
    )
  })

  test('left-pads short history to exactly 60 points', () => {
    const latest = point(generatedAt)
    const padded = padProbeHistory(
      [latest, point(generatedAt - 60)],
      'codex-gpt-5-5'
    )

    assert.equal(padded.length, 60)
    assert.equal(padded[0]!.placeholder, true)
    assert.equal(padded[57]!.id, 'codex-gpt-5-5:placeholder:57')
    assert.equal(padded[58]!.checked_at, generatedAt - 60)
    assert.equal(padded.at(-1)!.checked_at, latest.checked_at)
  })

  test('uses stable target-and-time IDs for real points and fixed slot IDs for placeholders', () => {
    const targetKey = 'codex-gpt-5-5'
    const history = [point(generatedAt - 60), point(generatedAt)]
    const first = padProbeHistory(history, targetKey)
    const repeated = padProbeHistory([...history].reverse(), targetKey)
    const advanced = padProbeHistory(
      [point(generatedAt + 60), ...history],
      targetKey
    )

    assert.deepEqual(
      first.map(({ id }) => id),
      repeated.map(({ id }) => id)
    )
    assert.equal(first.at(-1)!.id, `${targetKey}:${generatedAt}`)
    assert.equal(advanced[0]!.id, `${targetKey}:placeholder:0`)
    assert.equal(
      advanced[56]!.id,
      first[56]!.id,
      'surviving placeholder slots retain their identity'
    )
    assert.equal(advanced.at(-2)!.id, first.at(-1)!.id)
  })

  test('uses the public endpoint, request options, query cadence, and retry policy', async () => {
    apiRequests.length = 0
    const options = groupProbeStatusQueryOptions()

    assert.deepEqual(options.queryKey, ['public-group-probe-status'])
    assert.equal(options.refetchInterval, 60_000)
    assert.equal(options.staleTime, 15_000)
    assert.equal(options.retry, 2)
    assert.equal(typeof options.queryFn, 'function')

    const result = await (options.queryFn as () => Promise<unknown>)()
    assert.deepEqual(apiRequests, [
      {
        url: '/api/status/probes',
        config: { skipErrorHandler: true },
      },
    ])
    assert.deepEqual(result, {
      generated_at: 1,
      interval_seconds: 60,
      targets: [],
    })
  })
})
