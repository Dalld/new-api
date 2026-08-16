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

function required<T>(value: T | undefined): T {
  assert.ok(value !== undefined)
  return value
}

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
  const latest = [...history]
    .sort((left, right) => left.checked_at - right.checked_at)
    .at(-1)
  const available = history.filter(
    ({ state }) => state === 'operational' || state === 'degraded'
  ).length
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
          state: latest?.state ?? 'unknown',
          availability: latest ? available / history.length : null,
          ping_latency_ms: latest?.ping_latency_ms ?? null,
          chat_latency_ms: latest?.chat_latency_ms ?? null,
          latest_checked_at: latest?.checked_at ?? null,
          next_check_at: generatedAt + 60,
          history,
        },
      ],
    },
  }
}

describe('public probe API', () => {
  test('decodes every completed state, unknown summaries, and nullable dual latencies', () => {
    const input = response([
      point(generatedAt - 60, {
        state: 'degraded',
        ping_latency_ms: null,
      }),
      point(generatedAt, { chat_latency_ms: null }),
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
    assert.deepEqual(parsePublicProbeStatus(input), input.data)
    const empty = response([])
    assert.deepEqual(parsePublicProbeStatus(empty), empty.data)
  })

  test('rejects unsuccessful, legacy, extra-field, and malformed responses', () => {
    const validTarget = required(response().data.targets[0])
    const missingKeyTarget: Partial<PublicProbeTarget> = {
      ...validTarget,
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
          targets: [{ ...validTarget, state: 'healthy' }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, ping_latency_ms: -1 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, availability: 1.01 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...validTarget,
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
              ...validTarget,
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
          targets: [{ ...validTarget, channel_id: 42 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...validTarget,
              history: [{ ...point(generatedAt), api_key: 'secret' }],
            },
          ],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, key: ' padded-key ' }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, key: 'k'.repeat(97) }],
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
          targets: [{ ...validTarget, history: {} }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...validTarget,
              history: [{ ...point(generatedAt), state: 'unknown' }],
            },
          ],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, history: [] }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [
            {
              ...validTarget,
              state: 'unknown',
              availability: null,
              latest_checked_at: null,
            },
          ],
        },
      },
      {
        ...response([]),
        data: {
          ...response([]).data,
          targets: [
            {
              ...required(response([]).data.targets[0]),
              state: 'operational',
            },
          ],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, state: 'failed' }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, latest_checked_at: generatedAt - 1 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, chat_latency_ms: null }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: [{ ...validTarget, availability: 0 }],
        },
      },
      {
        ...response(),
        data: {
          ...response().data,
          targets: Array.from({ length: 21 }, () => validTarget),
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
          targets: [validTarget, { ...validTarget }],
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
    assert.equal(required(parsedHistory[0]).checked_at, generatedAt + 5)
    assert.equal(required(parsedHistory.at(-1)).checked_at, generatedAt + 64)
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
    assert.equal(required(padded.at(-1)).id, `codex-gpt-5-5:${generatedAt}`)
    assert.equal(required(padded.at(-1)).state, 'validation_failed')
  })

  test('accepts empty and exactly 60-point API histories', () => {
    assert.deepEqual(
      required(parsePublicProbeStatus(response([])).targets[0]).history,
      []
    )

    const exactHistory = Array.from({ length: 60 }, (_, index) =>
      point(generatedAt + index)
    ).reverse()
    const parsed = parsePublicProbeStatus(response(exactHistory))
    const parsedHistory = required(parsed.targets[0]).history
    assert.equal(parsedHistory.length, 60)
    assert.equal(required(parsedHistory[0]).checked_at, generatedAt)
    assert.equal(required(parsedHistory.at(-1)).checked_at, generatedAt + 59)
  })

  test('left-pads short history to exactly 60 points', () => {
    const latest = point(generatedAt)
    const padded = padProbeHistory(
      [latest, point(generatedAt - 60)],
      'codex-gpt-5-5'
    )

    assert.equal(padded.length, 60)
    assert.equal(required(padded[0]).placeholder, true)
    assert.equal(required(padded[57]).id, 'codex-gpt-5-5:placeholder:57')
    assert.equal(required(padded[58]).checked_at, generatedAt - 60)
    assert.equal(required(padded.at(-1)).checked_at, latest.checked_at)
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
    assert.equal(required(first.at(-1)).id, `${targetKey}:${generatedAt}`)
    assert.equal(required(advanced[0]).id, `${targetKey}:placeholder:0`)
    assert.equal(
      required(advanced[56]).id,
      required(first[56]).id,
      'surviving placeholder slots retain their identity'
    )
    assert.equal(required(advanced.at(-2)).id, required(first.at(-1)).id)
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
