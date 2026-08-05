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
import { AlertTriangle, CheckCircle2, CircleHelp, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { GroupProbeState, PublicGroupProbe } from '../types'
import { StatusTimeline } from './status-timeline'

const statePresentation: Record<
  GroupProbeState,
  { label: string; className: string; icon: typeof CheckCircle2 }
> = {
  healthy: {
    label: 'Healthy',
    className: 'text-emerald-700 dark:text-emerald-400',
    icon: CheckCircle2,
  },
  degraded: {
    label: 'Degraded',
    className: 'text-amber-700 dark:text-amber-400',
    icon: AlertTriangle,
  },
  down: {
    label: 'Unavailable',
    className: 'text-red-700 dark:text-red-400',
    icon: XCircle,
  },
  unknown: {
    label: 'Unknown',
    className: 'text-muted-foreground',
    icon: CircleHelp,
  },
}

function formatTimestamp(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(timestamp * 1000))
}

function formatAvailability(value: number | null) {
  if (value === null) return '—'
  return `${new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 2,
  }).format(value * 100)}%`
}

export function GroupStatusRow({ group }: { group: PublicGroupProbe }) {
  const { t } = useTranslation()
  const presentation = statePresentation[group.state]
  const StateIcon = presentation.icon
  const latestUpdate =
    group.latestCheckedAt === null
      ? t('No observations yet')
      : formatTimestamp(group.latestCheckedAt)

  return (
    <article
      className='grid min-h-48 gap-5 border-b py-7 last:border-b-0 lg:grid-cols-[minmax(13rem,1fr)_minmax(32rem,2.4fr)] lg:items-center'
      data-state={group.state}
      data-testid='group-status-row'
    >
      <div className='min-w-0 space-y-4'>
        <div className='flex min-w-0 items-start justify-between gap-4 lg:block'>
          <div className='min-w-0'>
            <h2
              className='truncate text-lg font-semibold'
              title={group.displayName}
            >
              {group.displayName}
            </h2>
            <p
              className='text-muted-foreground mt-1 truncate text-sm'
              title={group.model}
            >
              {group.model}
            </p>
          </div>
          <div
            className={cn(
              'mt-0 flex shrink-0 items-center gap-1.5 text-sm font-medium lg:mt-3',
              presentation.className
            )}
          >
            <StateIcon className='size-4' aria-hidden='true' />
            <span>{t(presentation.label)}</span>
          </div>
        </div>

        <dl className='grid grid-cols-2 gap-x-5 gap-y-3 text-sm'>
          <div>
            <dt className='text-muted-foreground'>{t('24h availability')}</dt>
            <dd className='mt-0.5 font-medium tabular-nums'>
              {formatAvailability(group.availability)}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>{t('Average latency')}</dt>
            <dd className='mt-0.5 font-medium tabular-nums'>
              {group.averageLatencyMs === null
                ? '—'
                : `${Math.round(group.averageLatencyMs)} ms`}
            </dd>
          </div>
        </dl>
      </div>

      <div className='min-w-0 space-y-3'>
        <StatusTimeline
          buckets={group.buckets}
          groupName={group.displayName}
          intervalMinutes={group.intervalMinutes}
        />
        <div className='text-muted-foreground flex min-h-5 flex-wrap justify-between gap-x-4 gap-y-1 text-xs'>
          <span>{t('24 hours ago')}</span>
          <span>
            {group.stale && (
              <strong className='text-amber-700 dark:text-amber-400'>
                {t('Stale')} ·{' '}
              </strong>
            )}
            {t('Last update: {{time}}', {
              time: latestUpdate,
            })}
          </span>
          <span>{t('Now')}</span>
        </div>
      </div>
    </article>
  )
}
