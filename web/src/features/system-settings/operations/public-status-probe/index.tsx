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
/* eslint-disable react-refresh/only-export-components */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { SettingsSection } from '../../components/settings-section'
import {
  createPublicStatusProbeTarget,
  deletePublicStatusProbeTarget,
  getPublicStatusProbeConfig,
  updatePublicStatusProbeConfig,
  updatePublicStatusProbeTarget,
} from './api'
import { GlobalSettingsForm } from './components/global-settings-form'
import { TargetsSection } from './components/targets-section'
import type {
  GlobalSettingsInput,
  PublicStatusProbeConfig,
  PublicStatusProbeTarget,
  TargetInput,
} from './types'
import { PublicStatusProbeClientError } from './validation'

export const publicStatusProbeConfigQueryKey = [
  'public-status-probe',
  'config',
] as const

type ConflictScope = 'global' | 'target' | null

function editableTarget(
  target: PublicStatusProbeTarget,
  enabled = target.enabled
): TargetInput {
  return {
    enabled,
    group: target.group,
    display_name: target.display_name,
    model: target.model,
    protocol: target.protocol,
    channel_id: target.channel_id,
    key_index: target.key_index,
  }
}

export function PublicStatusProbeSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const commandLocks = useRef(new Set<string>())
  const [conflictScope, setConflictScope] = useState<ConflictScope>(null)

  const configQuery = useQuery({
    queryKey: publicStatusProbeConfigQueryKey,
    queryFn: getPublicStatusProbeConfig,
    retry: false,
  })

  const replaceConfig = (config: PublicStatusProbeConfig) => {
    queryClient.setQueryData(publicStatusProbeConfigQueryKey, config)
    setConflictScope(null)
  }

  const globalMutation = useMutation({
    mutationFn: (input: GlobalSettingsInput & { version: number }) =>
      updatePublicStatusProbeConfig(input),
    onSuccess: replaceConfig,
  })
  const createMutation = useMutation({
    mutationFn: (input: { version: number; target: TargetInput }) =>
      createPublicStatusProbeTarget(input.version, input.target),
    onSuccess: replaceConfig,
  })
  const updateMutation = useMutation({
    mutationFn: (input: {
      key: string
      version: number
      target: TargetInput
    }) => updatePublicStatusProbeTarget(input.key, input.version, input.target),
    onSuccess: replaceConfig,
  })
  const deleteMutation = useMutation({
    mutationFn: (input: { key: string; version: number }) =>
      deletePublicStatusProbeTarget(input.key, input.version),
    onSuccess: replaceConfig,
  })

  async function runCommand(
    lockKey: string,
    scope: Exclude<ConflictScope, null>,
    command: () => Promise<PublicStatusProbeConfig>,
    bubbleValidation = false
  ) {
    if (commandLocks.current.has(lockKey)) return false
    commandLocks.current.add(lockKey)
    try {
      await command()
      return true
    } catch (error) {
      if (
        error instanceof PublicStatusProbeClientError &&
        error.kind === 'conflict'
      ) {
        setConflictScope(scope)
        await configQuery.refetch()
      } else if (
        error instanceof PublicStatusProbeClientError &&
        error.kind === 'validation'
      ) {
        if (bubbleValidation) throw error
        toast.error(t('Check the highlighted fields and try again.'))
      } else {
        toast.error(t('Failed to update public status probe configuration.'))
      }
      return false
    } finally {
      commandLocks.current.delete(lockKey)
    }
  }

  if (configQuery.isPending) {
    return (
      <SettingsSection title={t('Public Status Probe')}>
        <div aria-label={t('Loading public status probe configuration')}>
          <Skeleton className='mb-4 h-8 w-48' />
          <Skeleton className='mb-3 h-24 w-full' />
          <Skeleton className='h-36 w-full' />
        </div>
      </SettingsSection>
    )
  }

  if (configQuery.isError || !configQuery.data) {
    return (
      <SettingsSection title={t('Public Status Probe')}>
        <Alert variant='destructive'>
          <AlertCircle />
          <AlertTitle>
            {t('Failed to load public status probe configuration')}
          </AlertTitle>
          <AlertDescription>
            {t('Try loading the configuration again.')}
          </AlertDescription>
        </Alert>
        <Button
          type='button'
          variant='outline'
          className='w-fit'
          onClick={() => void configQuery.refetch()}
          disabled={configQuery.isFetching}
        >
          <RefreshCw data-icon='inline-start' />
          {configQuery.isFetching ? t('Retrying...') : t('Retry')}
        </Button>
      </SettingsSection>
    )
  }

  const config = configQuery.data
  const targetMutationPending =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending

  return (
    <SettingsSection title={t('Public Status Probe')}>
      <GlobalSettingsForm
        config={config}
        conflict={conflictScope === 'global'}
        isSaving={globalMutation.isPending}
        onSave={(values) =>
          runCommand('global', 'global', () =>
            globalMutation.mutateAsync({ version: config.version, ...values })
          )
        }
      />
      <TargetsSection
        targets={config.targets}
        channels={config.channels}
        conflict={conflictScope === 'target'}
        isMutating={targetMutationPending}
        onCreate={(target) =>
          runCommand(
            'target',
            'target',
            () =>
              createMutation.mutateAsync({ version: config.version, target }),
            true
          )
        }
        onUpdate={(key, target) =>
          runCommand(
            'target',
            'target',
            () =>
              updateMutation.mutateAsync({
                key,
                version: config.version,
                target,
              }),
            true
          )
        }
        onToggle={(target: PublicStatusProbeTarget) =>
          runCommand(`toggle:${target.key}`, 'target', () =>
            updateMutation.mutateAsync({
              key: target.key,
              version: config.version,
              target: editableTarget(target, !target.enabled),
            })
          )
        }
        onDelete={(target) =>
          runCommand('target', 'target', () =>
            deleteMutation.mutateAsync({
              key: target.key,
              version: config.version,
            })
          )
        }
      />
    </SettingsSection>
  )
}
