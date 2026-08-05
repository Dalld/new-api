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
import { LoaderCircle, Play, Plus, RefreshCw, Save, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  SettingsFormGrid,
  SettingsSwitchField,
} from '../../components/settings-form-layout'
import { SettingsPageActionsPortal } from '../../components/settings-page-context'
import { SettingsSection } from '../../components/settings-section'
import type { GroupProbeMapping, GroupProbeSettings } from '../../types'
import {
  getGroupProbeSettings,
  runGroupProbe,
  updateGroupProbeSettings,
} from './api'
import {
  GROUP_PROBE_LIMITS,
  type GroupProbeValidationErrors,
  validateAndNormalizeGroupProbeSettings,
} from './validation'

const emptyMapping = (): GroupProbeMapping => ({
  group: '',
  display_name: '',
  model: '',
  public: false,
})

let nextMappingId = 0
const createMappingId = () => `group-probe-mapping-${++nextMappingId}`

const cloneSettings = (settings: GroupProbeSettings): GroupProbeSettings => ({
  ...settings,
  groups: settings.groups.map((mapping) => ({ ...mapping })),
})

type NumberField = 'interval_minutes' | 'retention_days' | 'timeout_seconds'

type MappingTextField = 'group' | 'display_name' | 'model'

function FieldError({ message }: { message?: string }) {
  if (!message) return null
  return <p className='text-destructive text-xs'>{message}</p>
}

export function GroupProbeSettingsSection() {
  const { t } = useTranslation()
  const [settings, setSettings] = useState<GroupProbeSettings | null>(null)
  const [initialSettings, setInitialSettings] =
    useState<GroupProbeSettings | null>(null)
  const [errors, setErrors] = useState<GroupProbeValidationErrors>({})
  const [mappingIds, setMappingIds] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [saving, setSaving] = useState(false)
  const [running, setRunning] = useState(false)
  const [lastTaskId, setLastTaskId] = useState('')

  const loadSettings = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const response = await getGroupProbeSettings()
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to load probe settings'))
      }
      const next = cloneSettings(response.data)
      setSettings(next)
      setInitialSettings(cloneSettings(next))
      setMappingIds(next.groups.map(createMappingId))
      setErrors({})
    } catch (error) {
      setLoadError(
        error instanceof Error
          ? error.message
          : t('Failed to load probe settings')
      )
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void loadSettings()
  }, [loadSettings])

  const dirty = useMemo(
    () =>
      settings !== null &&
      initialSettings !== null &&
      JSON.stringify(settings) !== JSON.stringify(initialSettings),
    [initialSettings, settings]
  )

  const updateNumber = (field: NumberField, value: string) => {
    setSettings((current) =>
      current ? { ...current, [field]: Number(value) } : current
    )
    setErrors((current) => ({ ...current, [field]: '' }))
  }

  const updateMapping = (
    index: number,
    field: MappingTextField,
    value: string
  ) => {
    setSettings((current) => {
      if (!current) return current
      const groups = current.groups.map((mapping, mappingIndex) =>
        mappingIndex === index ? { ...mapping, [field]: value } : mapping
      )
      return { ...current, groups }
    })
    setErrors((current) =>
      Object.fromEntries(
        Object.entries(current).filter(([path]) =>
          field === 'group'
            ? !/^groups\.\d+\.group$/.test(path)
            : path !== `groups.${index}.${field}`
        )
      )
    )
  }

  const updatePublic = (index: number, value: boolean) => {
    setSettings((current) => {
      if (!current) return current
      const groups = current.groups.map((mapping, mappingIndex) =>
        mappingIndex === index ? { ...mapping, public: value } : mapping
      )
      return { ...current, groups }
    })
  }

  const addMapping = () => {
    if (!settings || settings.groups.length >= GROUP_PROBE_LIMITS.groups) return
    setSettings({ ...settings, groups: [...settings.groups, emptyMapping()] })
    setMappingIds((current) => [...current, createMappingId()])
  }

  const removeMapping = (index: number) => {
    setSettings((current) =>
      current
        ? {
            ...current,
            groups: current.groups.filter(
              (_, mappingIndex) => mappingIndex !== index
            ),
          }
        : current
    )
    setMappingIds((current) =>
      current.filter((_, mappingIndex) => mappingIndex !== index)
    )
    setErrors({})
  }

  const handleSave = async () => {
    if (!settings) return
    const validation = validateAndNormalizeGroupProbeSettings(
      settings,
      (key, values) => t(key, values)
    )
    setSettings(validation.settings)
    setErrors(validation.errors)
    if (Object.values(validation.errors).some(Boolean)) return

    setSaving(true)
    try {
      const response = await updateGroupProbeSettings(validation.settings)
      if (!response.success || !response.data) {
        throw new Error(t('Failed to save probe settings'))
      }
      const saved = cloneSettings(response.data)
      setSettings(saved)
      setInitialSettings(cloneSettings(saved))
      toast.success(t('Probe settings saved'))
    } catch {
      toast.error(t('Failed to save probe settings'))
    } finally {
      setSaving(false)
    }
  }

  const handleRun = async () => {
    setRunning(true)
    try {
      const response = await runGroupProbe()
      if (!response.success || !response.data?.task_id) {
        throw new Error(t('Failed to run group probe'))
      }
      setLastTaskId(response.data.task_id)
      toast.success(
        t(
          response.data.created
            ? 'Group probe queued'
            : 'Group probe is already queued'
        )
      )
    } catch {
      toast.error(t('Failed to run group probe'))
    } finally {
      setRunning(false)
    }
  }

  if (loading) {
    return (
      <SettingsSection title={t('Group Probe')}>
        <div aria-label={t('Loading probe settings')} className='space-y-3'>
          <Skeleton className='h-10 w-full' />
          <Skeleton className='h-28 w-full' />
        </div>
      </SettingsSection>
    )
  }

  if (loadError || !settings) {
    return (
      <SettingsSection title={t('Group Probe')}>
        <Alert variant='destructive'>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
        <Button type='button' variant='outline' onClick={loadSettings}>
          <RefreshCw data-icon='inline-start' />
          {t('Retry')}
        </Button>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Group Probe')}>
      <SettingsPageActionsPortal>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={handleRun}
          disabled={running}
        >
          {running ? (
            <LoaderCircle data-icon='inline-start' className='animate-spin' />
          ) : (
            <Play data-icon='inline-start' />
          )}
          <span>{t(running ? 'Starting...' : 'Run probe now')}</span>
        </Button>
        <Button
          type='button'
          size='sm'
          onClick={handleSave}
          disabled={saving || !dirty}
        >
          {saving ? (
            <LoaderCircle data-icon='inline-start' className='animate-spin' />
          ) : (
            <Save data-icon='inline-start' />
          )}
          <span>{t(saving ? 'Saving...' : 'Save Changes')}</span>
        </Button>
      </SettingsPageActionsPortal>

      <SettingsSwitchField
        checked={settings.enabled}
        onCheckedChange={(enabled) =>
          setSettings((current) =>
            current ? { ...current, enabled } : current
          )
        }
        label={t('Enable group probes')}
        description={t(
          'Run scheduled model checks for the configured channel groups.'
        )}
      />

      <SettingsFormGrid>
        {(
          [
            ['interval_minutes', 'Interval (minutes)'],
            ['retention_days', 'Retention (days)'],
            ['timeout_seconds', 'Timeout (seconds)'],
          ] as const
        ).map(([field, label]) => {
          const limits = GROUP_PROBE_LIMITS[field]
          return (
            <div key={field} className='grid min-w-0 gap-1.5'>
              <Label htmlFor={`group-probe-${field}`}>{t(label)}</Label>
              <Input
                id={`group-probe-${field}`}
                type='number'
                min={limits.min}
                max={limits.max}
                step={1}
                value={settings[field]}
                aria-invalid={Boolean(errors[field])}
                onChange={(event) => updateNumber(field, event.target.value)}
              />
              <FieldError message={errors[field]} />
            </div>
          )
        })}
      </SettingsFormGrid>

      <div className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div>
            <h4 className='text-sm font-medium'>{t('Probe groups')}</h4>
            <p className='text-muted-foreground text-xs'>
              {t('{{count}} of {{limit}} configured', {
                count: settings.groups.length,
                limit: GROUP_PROBE_LIMITS.groups,
              })}
            </p>
          </div>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={addMapping}
            disabled={settings.groups.length >= GROUP_PROBE_LIMITS.groups}
          >
            <Plus data-icon='inline-start' />
            {t('Add group')}
          </Button>
        </div>

        <FieldError message={errors.groups} />

        {settings.groups.length === 0 ? (
          <div className='text-muted-foreground border-y py-8 text-center text-sm'>
            {t('No probe groups configured')}
          </div>
        ) : (
          <div className='divide-y border-y'>
            {settings.groups.map((mapping, index) => (
              <div
                key={mappingIds[index]}
                data-testid='group-probe-mapping'
                className='grid min-w-0 gap-4 py-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto_auto] lg:items-start'
              >
                {(
                  [
                    ['group', 'Group'],
                    ['display_name', 'Display name'],
                    ['model', 'Model'],
                  ] as const
                ).map(([field, label]) => {
                  const error = errors[`groups.${index}.${field}`]
                  return (
                    <div key={field} className='grid min-w-0 gap-1.5'>
                      <Label htmlFor={`group-probe-${index}-${field}`}>
                        {t(label)}
                      </Label>
                      <Input
                        id={`group-probe-${index}-${field}`}
                        value={mapping[field]}
                        aria-invalid={Boolean(error)}
                        onChange={(event) =>
                          updateMapping(index, field, event.target.value)
                        }
                      />
                      <FieldError message={error} />
                    </div>
                  )
                })}

                <label className='flex min-h-9 items-center gap-2 self-end pb-0.5 text-sm lg:self-start lg:pt-7'>
                  <Checkbox
                    checked={mapping.public}
                    onCheckedChange={(checked) =>
                      updatePublic(index, checked === true)
                    }
                  />
                  <span>{t('Public')}</span>
                </label>

                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        className='text-destructive self-end lg:mt-6 lg:self-start'
                        aria-label={t('Delete group')}
                        onClick={() => removeMapping(index)}
                      />
                    }
                  >
                    <Trash2 />
                  </TooltipTrigger>
                  <TooltipContent>{t('Delete group')}</TooltipContent>
                </Tooltip>
              </div>
            ))}
          </div>
        )}
      </div>

      {lastTaskId ? (
        <p role='status' className='text-muted-foreground text-xs'>
          {t('Queued task:')}{' '}
          <code className='bg-muted rounded px-1 py-0.5'>{lastTaskId}</code>
        </p>
      ) : null}
    </SettingsSection>
  )
}
