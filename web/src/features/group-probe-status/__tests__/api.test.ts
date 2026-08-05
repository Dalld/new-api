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

const bunTestModule = 'bun:test'
const { mock } = await import(bunTestModule)

mock.module('@/lib/api', () => ({
  api: {
    get: async () => ({ data: { success: true, data: [] } }),
  },
}))

const { groupProbeStatusQueryOptions, parsePublicGroupProbeStatus } =
  await import('../api')

const generatedAt = 2_000_001_600

describe('public group probe API', () => {
  test('uses the public endpoint query cadence', () => {
    const options = groupProbeStatusQueryOptions()

    assert.deepEqual(options.queryKey, ['public-group-probe-status'])
    assert.equal(options.refetchInterval, 30_000)
    assert.equal(options.staleTime, 15_000)
  })

  test('preserves the variable buckets returned for each probe interval', () => {
    for (const [intervalMinutes, bucketCount] of [
      [5, 288],
      [10, 144],
      [30, 48],
      [60, 24],
    ] as const) {
      const buckets = Array.from({ length: bucketCount }, (_, index) => ({
        started_at:
          generatedAt - (bucketCount - 1 - index) * intervalMinutes * 60,
        state:
          index === bucketCount - 1
            ? ('healthy' as const)
            : ('unknown' as const),
        sample_count: index === bucketCount - 1 ? 1 : 0,
      }))
      const result = parsePublicGroupProbeStatus({
        success: true,
        data: {
          generated_at: generatedAt,
          interval_minutes: intervalMinutes,
          groups: [
            {
              group_name: `interval-${intervalMinutes}`,
              display_name: `Interval ${intervalMinutes}`,
              model_name: 'gpt-5.5',
              state: 'healthy',
              interval_minutes: intervalMinutes,
              buckets,
            },
          ],
        },
      })

      assert.equal(result.groups[0]?.intervalMinutes, intervalMinutes)
      assert.equal(result.groups[0]?.buckets.length, bucketCount)
      assert.deepEqual(result.groups[0]?.buckets[0], {
        startedAt: buckets[0]?.started_at,
        state: 'unknown',
        sampleCount: 0,
      })
      assert.deepEqual(result.groups[0]?.buckets.at(-1), {
        startedAt: generatedAt,
        state: 'healthy',
        sampleCount: 1,
      })
    }
  })

  test('normalizes bucket fields without rebuilding the timeline', () => {
    const result = parsePublicGroupProbeStatus({
      success: true,
      message: 'ok',
      request_id: 'public-request-id',
      data: {
        generated_at: generatedAt,
        interval_minutes: 10,
        channel_id: 999,
        groups: [
          {
            group_name: 'codex',
            display_name: 'Codex',
            model_name: 'gpt-5.5',
            state: 'healthy',
            availability_24h: 0.975,
            avg_latency_ms: 821.4,
            sample_count: 41,
            last_checked_at: generatedAt - 30,
            fresh: true,
            channel_id: 42,
            channel_name: 'private-upstream',
            task_id: 77,
            error_message: 'secret-key-value',
            api_key: 'sk-private',
            buckets: [
              {
                bucket_start: generatedAt - 1800,
                state: 'degraded',
                sample_count: 2,
                error: 'must not escape',
              },
              {
                bucket_start: generatedAt,
                state: 'healthy',
                sample_count: 1,
                channel_id: 42,
              },
            ],
          },
        ],
      },
    })

    assert.equal(result.groups.length, 1)
    assert.equal(result.groups[0]?.buckets.length, 2)
    assert.deepEqual(result.groups[0]?.buckets, [
      {
        startedAt: generatedAt - 1800,
        state: 'degraded',
        sampleCount: 2,
      },
      { startedAt: generatedAt, state: 'healthy', sampleCount: 1 },
    ])

    const serialized = JSON.stringify(result)
    for (const protectedValue of [
      'channel_id',
      'private-upstream',
      'task_id',
      'secret-key-value',
      'sk-private',
      'public-request-id',
    ]) {
      assert.equal(serialized.includes(protectedValue), false)
    }
  })

  test('rejects unsuccessful and malformed public responses', () => {
    assert.throws(() =>
      parsePublicGroupProbeStatus({ success: false, data: [] })
    )
    assert.throws(() =>
      parsePublicGroupProbeStatus({
        success: true,
        data: [
          {
            display_name: 'Missing identifier',
            model: 'gpt-5.5',
            state: 'healthy',
            buckets: [],
          },
        ],
      })
    )
    assert.throws(() =>
      parsePublicGroupProbeStatus({
        success: true,
        data: {
          generated_at: generatedAt,
          groups: [
            {
              group_name: 'missing-interval',
              display_name: 'Missing interval',
              model_name: 'gpt-5.5',
              state: 'healthy',
              buckets: [],
            },
          ],
        },
      })
    )
    assert.throws(() =>
      parsePublicGroupProbeStatus({
        success: true,
        data: {
          generated_at: generatedAt,
          interval_minutes: 5,
          groups: [
            {
              group_name: 'too-many-buckets',
              display_name: 'Too many buckets',
              model_name: 'gpt-5.5',
              state: 'healthy',
              buckets: Array.from({ length: 289 }, (_, index) => ({
                started_at: generatedAt + index,
                state: 'unknown',
              })),
            },
          ],
        },
      })
    )
  })
})
