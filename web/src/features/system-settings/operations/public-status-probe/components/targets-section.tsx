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
import { AlertTriangle, Pencil, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import type {
  PublicStatusProbeChannel,
  PublicStatusProbeTarget,
  TargetInput,
} from '../types'
import { DeleteTargetDialog } from './delete-target-dialog'
import { TargetDialog } from './target-dialog'

type TargetsSectionProps = {
  targets: PublicStatusProbeTarget[]
  channels: PublicStatusProbeChannel[]
  conflict: boolean
  isMutating: boolean
  onCreate: (target: TargetInput) => Promise<boolean>
  onUpdate: (key: string, target: TargetInput) => Promise<boolean>
  onToggle: (target: PublicStatusProbeTarget) => Promise<boolean>
  onDelete: (target: PublicStatusProbeTarget) => Promise<boolean>
}

export function TargetsSection({
  targets,
  channels,
  conflict,
  isMutating,
  onCreate,
  onUpdate,
  onToggle,
  onDelete,
}: TargetsSectionProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingTarget, setEditingTarget] =
    useState<PublicStatusProbeTarget | null>(null)
  const [deletingTarget, setDeletingTarget] =
    useState<PublicStatusProbeTarget | null>(null)

  const groupedTargets = useMemo(() => {
    const groups = new Map<string, PublicStatusProbeTarget[]>()
    for (const target of targets) {
      const group = groups.get(target.group)
      if (group) group.push(target)
      else groups.set(target.group, [target])
    }
    return [...groups.entries()]
  }, [targets])
  const hasSelectableChannel = channels.some((channel) => channel.status === 1)

  function openCreate() {
    setEditingTarget(null)
    setDialogOpen(true)
  }

  function openEdit(target: PublicStatusProbeTarget) {
    setEditingTarget(target)
    setDialogOpen(true)
  }

  function handleDialogOpenChange(open: boolean) {
    setDialogOpen(open)
    if (!open) setEditingTarget(null)
  }

  return (
    <section
      aria-labelledby='public-probe-targets'
      className='border-border/70 space-y-4 border-t pt-5'
    >
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div>
          <h4 id='public-probe-targets' className='text-sm font-semibold'>
            {t('Probe targets')}
          </h4>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Targets are grouped for the public status page.')}
          </p>
        </div>
        <Button
          type='button'
          size='sm'
          onClick={openCreate}
          disabled={isMutating || targets.length >= 20 || !hasSelectableChannel}
        >
          <Plus data-icon='inline-start' />
          {t('Add target')}
        </Button>
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

      {groupedTargets.length === 0 ? (
        <div className='border-border/70 py-8 text-center text-sm'>
          <p className='font-medium'>{t('No probe targets configured')}</p>
          <p className='text-muted-foreground mt-1'>
            {hasSelectableChannel
              ? t('Add a target to begin monitoring a safe channel candidate.')
              : t('No safe channel candidates are currently available.')}
          </p>
        </div>
      ) : (
        <div className='space-y-6'>
          {groupedTargets.map(([group, groupTargets]) => (
            <section key={group} aria-label={group} className='space-y-2'>
              <div className='flex items-center gap-2'>
                <h5 className='min-w-0 text-sm font-medium break-all'>
                  {group}
                </h5>
                <Badge variant='outline'>{groupTargets.length}</Badge>
              </div>
              <div className='divide-border/70 divide-y border-y'>
                {groupTargets.map((target) => (
                  <div
                    key={target.key}
                    className='flex min-w-0 items-center gap-3 py-3'
                  >
                    <Switch
                      checked={target.enabled}
                      onCheckedChange={() => void onToggle(target)}
                      disabled={
                        isMutating ||
                        (!target.enabled &&
                          !channels.some(
                            (channel) =>
                              channel.id === target.channel_id &&
                              channel.status === 1
                          ))
                      }
                      aria-label={
                        target.enabled
                          ? t('Disable {{name}}', {
                              name: target.display_name,
                            })
                          : t('Enable {{name}}', {
                              name: target.display_name,
                            })
                      }
                    />
                    <div className='min-w-0 flex-1'>
                      <div className='flex min-w-0 flex-wrap items-center gap-2'>
                        <span className='truncate text-sm font-medium'>
                          {target.display_name}
                        </span>
                        {!target.enabled ? (
                          <Badge variant='secondary'>{t('Disabled')}</Badge>
                        ) : null}
                        <Badge
                          variant='outline'
                          className='shrink-0 font-mono text-[11px]'
                        >
                          {t('Key index')}: {target.key_index}
                        </Badge>
                        {!channels.some(
                          (channel) =>
                            channel.id === target.channel_id &&
                            channel.status === 1
                        ) ? (
                          <Badge variant='destructive'>
                            {t('Channel unavailable')}
                          </Badge>
                        ) : null}
                      </div>
                      <p className='text-muted-foreground mt-0.5 truncate text-xs'>
                        {target.channel.name || t('Unavailable channel')} ·{' '}
                        {target.model} · {target.protocol}
                      </p>
                    </div>
                    <div className='flex shrink-0 items-center gap-1'>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Edit {{name}}', {
                                name: target.display_name,
                              })}
                              disabled={isMutating}
                              onClick={() => openEdit(target)}
                            />
                          }
                        >
                          <Pencil />
                        </TooltipTrigger>
                        <TooltipContent>{t('Edit target')}</TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Delete {{name}}', {
                                name: target.display_name,
                              })}
                              disabled={isMutating}
                              onClick={() => setDeletingTarget(target)}
                            />
                          }
                        >
                          <Trash2 />
                        </TooltipTrigger>
                        <TooltipContent>{t('Delete target')}</TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          ))}
        </div>
      )}

      <TargetDialog
        open={dialogOpen}
        onOpenChange={handleDialogOpenChange}
        target={editingTarget}
        channels={channels}
        conflict={conflict}
        isSaving={isMutating}
        onSave={(values) =>
          editingTarget ? onUpdate(editingTarget.key, values) : onCreate(values)
        }
      />
      <DeleteTargetDialog
        target={deletingTarget}
        isDeleting={isMutating}
        onOpenChange={(open) => {
          if (!open) setDeletingTarget(null)
        }}
        onConfirm={async () => {
          if (!deletingTarget) return false
          return onDelete(deletingTarget)
        }}
      />
    </section>
  )
}
