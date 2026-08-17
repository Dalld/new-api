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
import { beforeEach, describe, test } from 'node:test'

import type { TargetInput } from '../types'

const bunTestModule = 'bun:test'
const { mock } = await import(bunTestModule)

type ApiCall = {
  method: 'delete' | 'get' | 'post' | 'put'
  args: unknown[]
}

const apiCalls: ApiCall[] = []
let nextError: unknown

const config = {
  version: 7,
  enabled: true,
  ping_timeout_seconds: 8,
  chat_timeout_seconds: 45,
  degraded_latency_ms: 6_000,
  concurrency: 5,
  retention_days: 7,
  targets: [],
  channels: [],
}

async function record(method: ApiCall['method'], args: unknown[]) {
  apiCalls.push({ method, args })
  if (nextError !== undefined) {
    const error = nextError
    nextError = undefined
    throw error
  }
  return { data: { success: true, message: '', data: config } }
}

mock.module('@/lib/api', () => ({
  api: {
    delete: (...args: unknown[]) => record('delete', args),
    get: (...args: unknown[]) => record('get', args),
    post: (...args: unknown[]) => record('post', args),
    put: (...args: unknown[]) => record('put', args),
  },
}))

const {
  createPublicStatusProbeTarget,
  deletePublicStatusProbeTarget,
  getPublicStatusProbeConfig,
  updatePublicStatusProbeConfig,
  updatePublicStatusProbeTarget,
} = await import('../api')

const target: TargetInput = {
  enabled: true,
  group: 'Primary',
  display_name: 'Codex',
  model: 'gpt-5.5',
  protocol: 'openai_responses',
  channel_id: 42,
  key_index: 0,
}

describe('public status probe management API', () => {
  beforeEach(() => {
    apiCalls.length = 0
    nextError = undefined
  })

  test('uses the exact backend methods, paths, and payloads', async () => {
    assert.deepEqual(await getPublicStatusProbeConfig(), config)
    await updatePublicStatusProbeConfig({
      version: 7,
      enabled: false,
      ping_timeout_seconds: 15,
      chat_timeout_seconds: 60,
      degraded_latency_ms: 60_000,
      concurrency: 20,
      retention_days: 30,
    })
    await createPublicStatusProbeTarget(7, target)
    await updatePublicStatusProbeTarget('probe-a', 7, {
      ...target,
      enabled: false,
    })
    await deletePublicStatusProbeTarget('probe-a', 7)

    assert.deepEqual(apiCalls, [
      {
        method: 'get',
        args: [
          '/api/public-status-probe/config',
          { skipBusinessError: true, skipErrorHandler: true },
        ],
      },
      {
        method: 'put',
        args: [
          '/api/public-status-probe/config',
          {
            version: 7,
            enabled: false,
            ping_timeout_seconds: 15,
            chat_timeout_seconds: 60,
            degraded_latency_ms: 60_000,
            concurrency: 20,
            retention_days: 30,
          },
          { skipBusinessError: true, skipErrorHandler: true },
        ],
      },
      {
        method: 'post',
        args: [
          '/api/public-status-probe/targets',
          { version: 7, ...target },
          { skipBusinessError: true, skipErrorHandler: true },
        ],
      },
      {
        method: 'put',
        args: [
          '/api/public-status-probe/targets/probe-a',
          { version: 7, ...target, enabled: false },
          { skipBusinessError: true, skipErrorHandler: true },
        ],
      },
      {
        method: 'delete',
        args: [
          '/api/public-status-probe/targets/probe-a',
          {
            params: { version: 7 },
            skipBusinessError: true,
            skipErrorHandler: true,
          },
        ],
      },
    ])
  })

  test('maps a conflict without retrying the request', async () => {
    nextError = {
      isAxiosError: true,
      response: { status: 409, data: { message: 'private detail' } },
    }

    await assert.rejects(
      updatePublicStatusProbeTarget('probe-a', 7, target),
      (error: unknown) => {
        assert.equal(typeof error, 'object')
        assert.equal((error as { kind?: string }).kind, 'conflict')
        assert.equal((error as { retry?: boolean }).retry, false)
        return true
      }
    )
    assert.equal(apiCalls.length, 1)
  })

  test('maps invalid input before making a request', async () => {
    await assert.rejects(
      createPublicStatusProbeTarget(7, { ...target, group: '   ' }),
      (error: unknown) => {
        assert.equal((error as { kind?: string }).kind, 'validation')
        assert.equal((error as { retry?: boolean }).retry, false)
        return true
      }
    )
    assert.equal(apiCalls.length, 0)
  })

  test('rejects invalid versions before making a request', async () => {
    for (const version of [0, -1, 1.5]) {
      await assert.rejects(
        deletePublicStatusProbeTarget('probe-a', version),
        (error: unknown) => {
          assert.equal((error as { kind?: string }).kind, 'validation')
          return true
        }
      )
    }
    assert.equal(apiCalls.length, 0)
  })

  test('maps non-conflict failures as request errors without retrying', async () => {
    nextError = {
      isAxiosError: true,
      response: { status: 503, data: { message: 'private detail' } },
    }

    await assert.rejects(getPublicStatusProbeConfig(), (error: unknown) => {
      assert.equal((error as { kind?: string }).kind, 'request')
      assert.equal((error as { retry?: boolean }).retry, false)
      return true
    })
    assert.equal(apiCalls.length, 1)
  })
})
