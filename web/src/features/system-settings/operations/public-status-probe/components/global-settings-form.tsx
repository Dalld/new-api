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
import { zodResolver } from '@hookform/resolvers/zod'
import { AlertTriangle } from 'lucide-react'
import { useEffect } from 'react'
import { type Resolver, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchField,
} from '../../../components/settings-form-layout'
import { SettingsPageFormActions } from '../../../components/settings-page-context'
import { safeNumberFieldProps } from '../../../utils/numeric-field'
import type { GlobalSettingsInput, PublicStatusProbeConfig } from '../types'
import { globalSettingsSchema } from '../validation'

type GlobalSettingsFormProps = {
  config: PublicStatusProbeConfig
  conflict: boolean
  isSaving: boolean
  onSave: (values: GlobalSettingsInput) => Promise<boolean>
}

const numberFields = [
  {
    name: 'ping_timeout_seconds',
    label: 'Ping timeout (seconds)',
    min: 1,
    max: 15,
  },
  {
    name: 'chat_timeout_seconds',
    label: 'Conversation timeout (seconds)',
    min: 5,
    max: 60,
  },
  {
    name: 'degraded_latency_ms',
    label: 'Degraded latency threshold (ms)',
    min: 1,
    max: 60_000,
  },
  {
    name: 'concurrency',
    label: 'Probe concurrency',
    min: 1,
    max: 20,
  },
  {
    name: 'retention_days',
    label: 'History retention (days)',
    min: 1,
    max: 30,
  },
] as const

function toFormValues(config: PublicStatusProbeConfig): GlobalSettingsInput {
  return {
    enabled: config.enabled,
    ping_timeout_seconds: config.ping_timeout_seconds,
    chat_timeout_seconds: config.chat_timeout_seconds,
    degraded_latency_ms: config.degraded_latency_ms,
    concurrency: config.concurrency,
    retention_days: config.retention_days,
  }
}

export function GlobalSettingsForm({
  config,
  conflict,
  isSaving,
  onSave,
}: GlobalSettingsFormProps) {
  const { t } = useTranslation()
  const form = useForm<GlobalSettingsInput>({
    resolver: zodResolver(
      globalSettingsSchema
    ) as unknown as Resolver<GlobalSettingsInput>,
    defaultValues: toFormValues(config),
  })

  useEffect(() => {
    if (!form.formState.isDirty) form.reset(toFormValues(config))
  }, [config, form])

  async function handleSubmit(values: GlobalSettingsInput) {
    const saved = await onSave(values)
    if (saved) form.reset(values)
  }

  return (
    <section
      aria-labelledby='public-probe-global-settings'
      className='space-y-4'
    >
      <div>
        <h4 id='public-probe-global-settings' className='text-sm font-semibold'>
          {t('Global probe settings')}
        </h4>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Controls the public status probes without affecting routing.')}
        </p>
      </div>

      {conflict ? (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('Configuration changed')}</AlertTitle>
          <AlertDescription>
            {t('The latest version was loaded. Your unsaved values were kept.')}
          </AlertDescription>
        </Alert>
      ) : null}

      <Form {...form}>
        <SettingsForm
          onSubmit={form.handleSubmit(handleSubmit)}
          autoComplete='off'
        >
          <SettingsPageFormActions
            onSave={form.handleSubmit(handleSubmit)}
            isSaving={isSaving || form.formState.isSubmitting}
            isSaveDisabled={!form.formState.isDirty}
            saveLabel={t('Save probe settings')}
          />

          <SettingsSwitchField
            switchId='public-status-probe-enabled'
            checked={form.watch('enabled')}
            onCheckedChange={(checked) =>
              form.setValue('enabled', checked, { shouldDirty: true })
            }
            label={t('Enable public status probes')}
            description={t(
              'Disabled targets remain configured and visible here.'
            )}
            disabled={isSaving}
          />

          <SettingsFormGrid>
            <FormItem>
              <FormLabel>{t('Probe interval (seconds)')}</FormLabel>
              <FormControl>
                <Input value={60} readOnly aria-readonly='true' />
              </FormControl>
              <FormDescription>
                {t('The scheduler runs on a fixed 60-second interval.')}
              </FormDescription>
            </FormItem>

            {numberFields.map(({ name, label, min, max }) => (
              <FormField
                key={name}
                control={form.control}
                name={name}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t(label)}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={min}
                        max={max}
                        step={1}
                        disabled={isSaving}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </section>
  )
}
