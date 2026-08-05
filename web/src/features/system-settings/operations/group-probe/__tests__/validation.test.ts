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
import { validateAndNormalizeGroupProbeSettings } from '../validation'

const t = (key: string, values?: Record<string, number>) =>
  Object.entries(values ?? {}).reduce(
    (message, [name, value]) => message.replace(`{{${name}}}`, String(value)),
    key
  )

const validSettings = (): GroupProbeSettings => ({
  enabled: true,
  interval_minutes: 10,
  retention_days: 7,
  timeout_seconds: 45,
  groups: [
    {
      group: ' codex ',
      display_name: ' Codex ',
      model: ' gpt-5.5 ',
      public: true,
    },
  ],
})

describe('group probe settings validation', () => {
  test('normalizes mapping text before submission', () => {
    const result = validateAndNormalizeGroupProbeSettings(validSettings(), t)

    assert.deepEqual(result.errors, {})
    assert.deepEqual(result.settings.groups[0], {
      group: 'codex',
      display_name: 'Codex',
      model: 'gpt-5.5',
      public: true,
    })
  })

  test('enforces numeric limits and integer values', () => {
    const result = validateAndNormalizeGroupProbeSettings(
      {
        ...validSettings(),
        interval_minutes: 4,
        retention_days: 91,
        timeout_seconds: 5.5,
      },
      t
    )

    assert.match(result.errors.interval_minutes ?? '', /5 to 1440/)
    assert.match(result.errors.retention_days ?? '', /1 to 90/)
    assert.match(result.errors.timeout_seconds ?? '', /5 to 120/)
  })

  test('rejects empty mappings and duplicate normalized group names', () => {
    const result = validateAndNormalizeGroupProbeSettings(
      {
        ...validSettings(),
        groups: [
          {
            group: 'codex',
            display_name: '',
            model: 'gpt-5.5',
            public: true,
          },
          {
            group: ' codex ',
            display_name: 'Duplicate',
            model: '',
            public: false,
          },
        ],
      },
      t
    )

    assert.equal(result.errors['groups.0.group'], 'Group must be unique.')
    assert.equal(
      result.errors['groups.0.display_name'],
      'Display name is required.'
    )
    assert.equal(result.errors['groups.1.group'], 'Group must be unique.')
    assert.equal(result.errors['groups.1.model'], 'Model is required.')
  })

  test('rejects more than 50 mappings', () => {
    const mapping = validSettings().groups[0]
    assert.ok(mapping)
    const result = validateAndNormalizeGroupProbeSettings(
      {
        ...validSettings(),
        groups: Array.from({ length: 51 }, (_, index) => ({
          ...mapping,
          group: `group-${index}`,
        })),
      },
      t
    )

    assert.match(result.errors.groups ?? '', /At most 50/)
  })
})
