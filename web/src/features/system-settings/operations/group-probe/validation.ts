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
import type { GroupProbeSettings } from '../../types'

export const GROUP_PROBE_LIMITS = {
  interval_minutes: { min: 5, max: 1440 },
  retention_days: { min: 1, max: 90 },
  timeout_seconds: { min: 5, max: 120 },
  groups: 50,
} as const

export type GroupProbeValidationErrors = Record<string, string>

export type GroupProbeValidationTranslator = (
  key: string,
  values?: Record<string, number>
) => string

function validateInteger(
  errors: GroupProbeValidationErrors,
  field: keyof Pick<
    GroupProbeSettings,
    'interval_minutes' | 'retention_days' | 'timeout_seconds'
  >,
  value: number,
  t: GroupProbeValidationTranslator
) {
  const limits = GROUP_PROBE_LIMITS[field]
  if (!Number.isInteger(value) || value < limits.min || value > limits.max) {
    errors[field] = t('Must be an integer from {{min}} to {{max}}.', limits)
  }
}

export function validateAndNormalizeGroupProbeSettings(
  candidate: GroupProbeSettings,
  t: GroupProbeValidationTranslator
): {
  settings: GroupProbeSettings
  errors: GroupProbeValidationErrors
} {
  const settings: GroupProbeSettings = {
    ...candidate,
    groups: candidate.groups.map((mapping) => ({
      ...mapping,
      group: mapping.group.trim(),
      display_name: mapping.display_name.trim(),
      model: mapping.model.trim(),
    })),
  }
  const errors: GroupProbeValidationErrors = {}

  validateInteger(errors, 'interval_minutes', settings.interval_minutes, t)
  validateInteger(errors, 'retention_days', settings.retention_days, t)
  validateInteger(errors, 'timeout_seconds', settings.timeout_seconds, t)

  if (settings.groups.length > GROUP_PROBE_LIMITS.groups) {
    errors.groups = t('At most {{max}} groups are allowed.', {
      max: GROUP_PROBE_LIMITS.groups,
    })
  }

  const seenGroups = new Map<string, number>()
  settings.groups.forEach((mapping, index) => {
    const prefix = `groups.${index}`
    if (!mapping.group) errors[`${prefix}.group`] = t('Group is required.')
    if (!mapping.display_name) {
      errors[`${prefix}.display_name`] = t('Display name is required.')
    }
    if (!mapping.model) errors[`${prefix}.model`] = t('Model is required.')

    if (mapping.group) {
      const previousIndex = seenGroups.get(mapping.group)
      if (previousIndex !== undefined) {
        errors[`${prefix}.group`] = t('Group must be unique.')
        errors[`groups.${previousIndex}.group`] = t('Group must be unique.')
      } else {
        seenGroups.set(mapping.group, index)
      }
    }
  })

  return { settings, errors }
}
