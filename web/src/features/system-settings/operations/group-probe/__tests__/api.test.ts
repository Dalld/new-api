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

import type { GroupProbeSettings } from '../../../types'

const calls: Array<{ method: string; url: string; body?: unknown }> = []
const bunTestModule = 'bun:test'
const { mock } = await import(bunTestModule)

mock.module('@/lib/api', () => ({
  api: {
    get: async (url: string) => {
      calls.push({ method: 'GET', url })
      return { data: { success: true, data: { enabled: false, groups: [] } } }
    },
    put: async (url: string, body: unknown) => {
      calls.push({ method: 'PUT', url, body })
      return { data: { success: true, data: body } }
    },
    post: async (url: string) => {
      calls.push({ method: 'POST', url })
      return {
        data: {
          success: true,
          data: { task_id: 'group-probe-1', created: true },
        },
      }
    },
  },
}))

const { getGroupProbeSettings, runGroupProbe, updateGroupProbeSettings } =
  await import('../api')

const settings: GroupProbeSettings = {
  enabled: true,
  interval_minutes: 10,
  retention_days: 7,
  timeout_seconds: 45,
  groups: [],
}

describe('group probe admin API', () => {
  test('uses the dedicated settings endpoints and preserves the payload', async () => {
    calls.length = 0
    await getGroupProbeSettings()
    await updateGroupProbeSettings(settings)

    assert.deepEqual(calls, [
      { method: 'GET', url: '/api/group-probe/settings' },
      {
        method: 'PUT',
        url: '/api/group-probe/settings',
        body: settings,
      },
    ])
  })

  test('queues a manual run through the dedicated command endpoint', async () => {
    calls.length = 0
    const response = await runGroupProbe()

    assert.deepEqual(calls, [{ method: 'POST', url: '/api/group-probe/run' }])
    assert.deepEqual(response.data, {
      task_id: 'group-probe-1',
      created: true,
    })
  })
})
