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
import { useEffect, useMemo, useRef, useState } from 'react'
import { type Resolver, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { FormNavigationGuard } from '../../../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchField,
} from '../../../components/settings-form-layout'
import { safeNumberFieldProps } from '../../../utils/numeric-field'
import {
  PUBLIC_STATUS_PROBE_PROTOCOLS,
  type PublicStatusProbeChannel,
  type PublicStatusProbeProtocol,
  type PublicStatusProbeTarget,
  type TargetInput,
} from '../types'
import { targetFormSchema } from '../validation'

type TargetDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: PublicStatusProbeTarget | null
  channels: PublicStatusProbeChannel[]
  conflict: boolean
  isSaving: boolean
  onSave: (values: TargetInput) => Promise<boolean>
}

const TARGET_FORM_ID = 'public-status-probe-target-form'

const protocolLabels: Record<PublicStatusProbeProtocol, string> = {
  openai_chat: 'OpenAI Chat Completions',
  openai_responses: 'OpenAI Responses',
  anthropic_messages: 'Anthropic Messages',
  gemini_generate_content: 'Gemini Generate Content',
}

function createDefaultValues(
  target: PublicStatusProbeTarget | null,
  channels: PublicStatusProbeChannel[]
): TargetInput {
  if (target) {
    return {
      enabled: target.enabled,
      group: target.group,
      display_name: target.display_name,
      model: target.model,
      protocol: target.protocol,
      channel_id: target.channel_id,
      key_index: target.key_index,
    }
  }
  return {
    enabled: true,
    group: '',
    display_name: '',
    model: '',
    protocol: 'openai_chat',
    channel_id: channels.find((channel) => channel.status === 1)?.id ?? 0,
    key_index: 0,
  }
}

export function TargetDialog({
  open,
  onOpenChange,
  target,
  channels,
  conflict,
  isSaving,
  onSave,
}: TargetDialogProps) {
  const { t } = useTranslation()
  const [discardOpen, setDiscardOpen] = useState(false)
  const sessionRef = useRef<string | null>(null)
  const submitRef = useRef(false)
  const form = useForm<TargetInput>({
    resolver: zodResolver(targetFormSchema) as unknown as Resolver<TargetInput>,
    defaultValues: createDefaultValues(target, channels),
  })
  const { isDirty, isSubmitting } = form.formState

  const selectorChannels = useMemo(() => {
    const selectable = channels.filter((channel) => channel.status === 1)
    if (!target) return selectable
    const current = channels.find(
      (channel) => channel.id === target.channel_id
    ) ?? {
      ...target.channel,
      id: target.channel_id,
      name: target.channel.name || t('Unavailable channel'),
    }
    if (selectable.some((channel) => channel.id === current.id)) {
      return selectable
    }
    return [...selectable, current]
  }, [channels, target, t])

  const channelItems = useMemo(
    () =>
      selectorChannels.map((channel) => ({
        value: String(channel.id),
        label: `${channel.name || t('Unavailable channel')} (#${channel.id}) - ${
          channel.status === 1 ? t('Enabled') : t('Disabled')
        }`,
      })),
    [selectorChannels, t]
  )
  const protocolItems = useMemo(
    () =>
      PUBLIC_STATUS_PROBE_PROTOCOLS.map((protocol) => ({
        value: protocol,
        label: t(protocolLabels[protocol]),
      })),
    [t]
  )
  const selectedChannelId = form.watch('channel_id')
  const selectedChannel = selectorChannels.find(
    (channel) => channel.id === selectedChannelId
  )
  const maxKeyIndex = selectedChannel?.is_multi_key
    ? Math.max(0, selectedChannel.key_count - 1)
    : 0

  useEffect(() => {
    let session: string | null = null
    if (open) session = target ? `edit:${target.key}` : 'create'
    if (session === null) {
      sessionRef.current = null
      return
    }
    if (sessionRef.current === session) return
    sessionRef.current = session
    form.reset(createDefaultValues(target, channels))
    setDiscardOpen(false)
  }, [channels, form, open, target])

  function requestOpenChange(nextOpen: boolean) {
    if (nextOpen) {
      onOpenChange(true)
      return
    }
    if (isDirty && !isSaving) {
      setDiscardOpen(true)
      return
    }
    onOpenChange(false)
  }

  async function handleSubmit(values: TargetInput) {
    if (submitRef.current) return
    submitRef.current = true
    form.clearErrors('root.server')
    try {
      if (await onSave(values)) {
        form.reset(values)
        onOpenChange(false)
      }
    } catch {
      form.setError('root.server', {
        type: 'server',
        message: t(
          'The selected channel, model, protocol, or key index is not valid for probing.'
        ),
      })
    } finally {
      submitRef.current = false
    }
  }

  return (
    <>
      <FormNavigationGuard
        when={open && isDirty && !isSaving}
        title={t('Unsaved probe target')}
        message={t('Leave without saving this probe target?')}
      />
      <Dialog
        open={open}
        onOpenChange={requestOpenChange}
        title={target ? t('Edit probe target') : t('Add probe target')}
        description={t(
          'Configure how this target appears on the public status page.'
        )}
        contentClassName='sm:max-w-2xl'
        contentHeight='auto'
        showCloseButton
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => requestOpenChange(false)}
              disabled={isSaving}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              form={TARGET_FORM_ID}
              disabled={isSaving || isSubmitting}
            >
              {isSaving ? t('Saving...') : t('Save target')}
            </Button>
          </>
        }
      >
        {conflict ? (
          <Alert className='mb-4'>
            <AlertTriangle />
            <AlertTitle>{t('Configuration changed')}</AlertTitle>
            <AlertDescription>
              {t(
                'The latest version was loaded. Your unsaved values were kept.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <Form {...form}>
          <SettingsForm
            id={TARGET_FORM_ID}
            onSubmit={form.handleSubmit(handleSubmit)}
            autoComplete='off'
          >
            <SettingsSwitchField
              switchId='public-status-probe-target-enabled'
              checked={form.watch('enabled')}
              onCheckedChange={(checked) =>
                form.setValue('enabled', checked, { shouldDirty: true })
              }
              label={t('Enabled')}
              description={t(
                'Disabled targets stay visible to administrators.'
              )}
              disabled={isSaving}
            />

            {form.formState.errors.root?.server?.message ? (
              <p role='alert' className='text-destructive text-sm'>
                {form.formState.errors.root.server.message}
              </p>
            ) : null}

            {target ? (
              <FormItem>
                <FormLabel>{t('Immutable key')}</FormLabel>
                <FormControl>
                  <Input value={target.key} readOnly aria-readonly='true' />
                </FormControl>
                <FormDescription>
                  {t('This key preserves probe history and cannot be changed.')}
                </FormDescription>
              </FormItem>
            ) : null}

            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='group'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Group')}</FormLabel>
                    <FormControl>
                      <Input disabled={isSaving} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='display_name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Display name')}</FormLabel>
                    <FormControl>
                      <Input disabled={isSaving} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='channel_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Channel')}</FormLabel>
                    <Select
                      items={channelItems}
                      value={field.value > 0 ? String(field.value) : null}
                      onValueChange={(value) => {
                        field.onChange(Number(value))
                        form.setValue('key_index', 0, { shouldDirty: true })
                      }}
                      disabled={isSaving || selectorChannels.length === 0}
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue
                            placeholder={t('Select a safe channel candidate')}
                          />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {selectorChannels.map((channel) => (
                            <SelectItem
                              key={channel.id}
                              value={String(channel.id)}
                            >
                              {channel.name || t('Unavailable channel')} (#
                              {channel.id}){' - '}
                              {channel.status === 1
                                ? t('Enabled')
                                : t('Disabled')}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t('Only server-approved channel metadata is shown.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='model'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Model')}</FormLabel>
                    <FormControl>
                      <Input disabled={isSaving} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='protocol'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Protocol')}</FormLabel>
                    <Select
                      items={protocolItems}
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={isSaving}
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {PUBLIC_STATUS_PROBE_PROTOCOLS.map((protocol) => (
                            <SelectItem key={protocol} value={protocol}>
                              {t(protocolLabels[protocol])}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='key_index'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Key index')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={maxKeyIndex}
                        step={1}
                        disabled={
                          isSaving ||
                          !selectedChannel?.is_multi_key ||
                          selectedChannel.key_count <= 1
                        }
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {selectedChannel?.is_multi_key
                        ? t('Available key indexes: 0 to {{max}}', {
                            max: maxKeyIndex,
                          })
                        : t('Single-key channels always use index 0.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGrid>
          </SettingsForm>
        </Form>
      </Dialog>

      <ConfirmDialog
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        title={t('Discard unsaved changes?')}
        desc={t('Your changes to this probe target will be lost.')}
        cancelBtnText={t('Keep editing')}
        confirmText={t('Discard changes')}
        destructive
        handleConfirm={() => {
          setDiscardOpen(false)
          form.reset(createDefaultValues(target, channels))
          onOpenChange(false)
        }}
      />
    </>
  )
}
