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

import type { PublicStatusProbeConfig, TargetInput } from '../types'
import {
  globalSettingsSchema,
  mapPublicStatusProbeError,
  publicStatusProbeConfigSchema,
  targetFormSchema,
} from '../validation'

const validGlobalSettings = {
  enabled: true,
  ping_timeout_seconds: 8,
  chat_timeout_seconds: 45,
  degraded_latency_ms: 6_000,
  concurrency: 5,
  retention_days: 7,
}

const validTarget: TargetInput = {
  enabled: true,
  group: 'Primary',
  display_name: 'Codex',
  model: 'gpt-5.5',
  protocol: 'openai_chat',
  channel_id: 42,
  key_index: 0,
}

function validConfig(targetCount: number): PublicStatusProbeConfig {
  return {
    version: 1,
    ...validGlobalSettings,
    targets: Array.from({ length: targetCount }, (_, index) => ({
      ...validTarget,
      key: `probe-${index}`,
      channel: {
        id: 42,
        name: 'Channel',
        type: 1,
        status: 1,
        models: 'gpt-5.5',
        is_multi_key: false,
        key_count: 1,
      },
    })),
  }
}

describe('global public status probe settings validation', () => {
  test('accepts every inclusive numeric boundary', () => {
    assert.equal(
      globalSettingsSchema.safeParse({
        ...validGlobalSettings,
        ping_timeout_seconds: 1,
        chat_timeout_seconds: 5,
        degraded_latency_ms: 1,
        concurrency: 1,
        retention_days: 1,
      }).success,
      true
    )
    assert.equal(
      globalSettingsSchema.safeParse({
        ...validGlobalSettings,
        ping_timeout_seconds: 15,
        chat_timeout_seconds: 60,
        degraded_latency_ms: 60_000,
        concurrency: 20,
        retention_days: 30,
      }).success,
      true
    )
  })

  test('rejects values outside every numeric boundary and non-integers', () => {
    const invalidValues = {
      ping_timeout_seconds: [0, 16, 1.5],
      chat_timeout_seconds: [4, 61, 5.5],
      degraded_latency_ms: [0, 60_001, 1.5],
      concurrency: [0, 21, 1.5],
      retention_days: [0, 31, 1.5],
    } as const

    for (const [field, values] of Object.entries(invalidValues)) {
      for (const value of values) {
        assert.equal(
          globalSettingsSchema.safeParse({
            ...validGlobalSettings,
            [field]: value,
          }).success,
          false,
          `${field} accepted ${value}`
        )
      }
    }
  })
})

describe('public status probe target validation', () => {
  test('trims required text and accepts exactly the four protocols', () => {
    const parsed = targetFormSchema.parse({
      ...validTarget,
      group: ' Primary ',
      display_name: ' Codex ',
      model: ' gpt-5.5 ',
    })
    assert.equal(parsed.group, 'Primary')
    assert.equal(parsed.display_name, 'Codex')
    assert.equal(parsed.model, 'gpt-5.5')

    for (const protocol of [
      'openai_chat',
      'openai_responses',
      'anthropic_messages',
      'gemini_generate_content',
    ]) {
      assert.equal(
        targetFormSchema.safeParse({ ...validTarget, protocol }).success,
        true
      )
    }
    assert.equal(
      targetFormSchema.safeParse({
        ...validTarget,
        protocol: 'unsupported',
      }).success,
      false
    )
  })

  test('rejects blank and overlong required text', () => {
    for (const [field, value] of [
      ['group', '   '],
      ['display_name', ''],
      ['model', '\t'],
      ['group', 'g'.repeat(65)],
      ['display_name', 'd'.repeat(129)],
      ['model', 'm'.repeat(129)],
    ] as const) {
      assert.equal(
        targetFormSchema.safeParse({ ...validTarget, [field]: value }).success,
        false,
        `${field} accepted invalid text`
      )
    }
  })

  test('counts Unicode code points instead of UTF-16 code units', () => {
    assert.equal(
      targetFormSchema.safeParse({
        ...validTarget,
        group: '\u{1f680}'.repeat(64),
      }).success,
      true
    )
    assert.equal(
      targetFormSchema.safeParse({
        ...validTarget,
        group: '\u{1f680}'.repeat(65),
      }).success,
      false
    )
  })

  test('requires a positive integer channel and non-negative integer key index', () => {
    for (const channel_id of [0, -1, 1.5]) {
      assert.equal(
        targetFormSchema.safeParse({ ...validTarget, channel_id }).success,
        false
      )
    }
    for (const key_index of [-1, 0.5]) {
      assert.equal(
        targetFormSchema.safeParse({ ...validTarget, key_index }).success,
        false
      )
    }
    assert.equal(
      targetFormSchema.safeParse({ ...validTarget, key_index: 0 }).success,
      true
    )
  })

  test('accepts 20 targets and rejects 21', () => {
    assert.equal(
      publicStatusProbeConfigSchema.safeParse(validConfig(20)).success,
      true
    )
    assert.equal(
      publicStatusProbeConfigSchema.safeParse(validConfig(21)).success,
      false
    )
  })

  test('accepts orphan channel placeholders and rejects malformed target keys', () => {
    const orphan = validConfig(1)
    const firstTarget = orphan.targets[0]
    assert.ok(firstTarget)
    firstTarget.channel = {
      id: 42,
      name: '',
      type: 0,
      status: 2,
      models: '',
      is_multi_key: false,
      key_count: 0,
    }
    assert.equal(publicStatusProbeConfigSchema.safeParse(orphan).success, true)

    const paddedKey = validConfig(1)
    assert.ok(paddedKey.targets[0])
    paddedKey.targets[0].key = ' probe-0 '
    assert.equal(
      publicStatusProbeConfigSchema.safeParse(paddedKey).success,
      false
    )
  })

  test('rejects duplicate target keys', () => {
    const duplicate = validConfig(2)
    assert.ok(duplicate.targets[0])
    assert.ok(duplicate.targets[1])
    duplicate.targets[1].key = duplicate.targets[0].key
    assert.equal(
      publicStatusProbeConfigSchema.safeParse(duplicate).success,
      false
    )
  })
})

describe('public status probe error mapping', () => {
  test('distinguishes validation, conflict, and request failures', () => {
    const validationError = mapPublicStatusProbeError(
      targetFormSchema.safeParse({ ...validTarget, group: '' }).error
    )
    const conflictError = mapPublicStatusProbeError({
      isAxiosError: true,
      response: { status: 409 },
    })
    const serverValidationError = mapPublicStatusProbeError({
      isAxiosError: true,
      response: { status: 400 },
    })
    const requestError = mapPublicStatusProbeError(new Error('offline'))

    assert.equal(validationError.kind, 'validation')
    assert.equal(serverValidationError.kind, 'validation')
    assert.equal(conflictError.kind, 'conflict')
    assert.equal(requestError.kind, 'request')
    assert.equal(validationError.retry, false)
    assert.equal(conflictError.retry, false)
    assert.equal(requestError.retry, false)
  })
})
