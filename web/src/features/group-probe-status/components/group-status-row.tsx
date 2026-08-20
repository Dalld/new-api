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
import {
  AlertTriangle,
  CheckCircle2,
  CircleHelp,
  ShieldAlert,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { padProbeHistory } from '../history'
import type { PublicProbeTarget } from '../types'
import { StatusTimeline } from './status-timeline'

type StatePresentation = {
  label: string
  className: string
  badgeClassName: string
  icon: typeof CheckCircle2
}

function getStatePresentation(state: string): StatePresentation {
  switch (state) {
    case 'operational':
      return {
        label: 'Operational',
        className: 'text-emerald-700 dark:text-emerald-400',
        badgeClassName:
          'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
        icon: CheckCircle2,
      }
    case 'degraded':
      return {
        label: 'Degraded',
        className: 'text-amber-700 dark:text-amber-400',
        badgeClassName:
          'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400',
        icon: AlertTriangle,
      }
    case 'validation_failed':
      return {
        label: 'Validation failed',
        className: 'text-red-700 dark:text-red-400',
        badgeClassName:
          'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-400',
        icon: ShieldAlert,
      }
    case 'failed':
      return {
        label: 'Unavailable',
        className: 'text-red-700 dark:text-red-400',
        badgeClassName:
          'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-400',
        icon: XCircle,
      }
    default:
      return {
        label: 'Unknown',
        className: 'text-muted-foreground',
        badgeClassName: 'border-border bg-muted text-muted-foreground',
        icon: CircleHelp,
      }
  }
}

function formatTimestamp(timestamp: number | null) {
  if (timestamp === null) return null
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(timestamp * 1000))
}

function formatAvailability(value: number | null) {
  if (value === null) return '--'
  return `${new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 2,
  }).format(value * 100)}%`
}

function formatLatency(value: number | null) {
  return value === null ? '--' : `${Math.round(value)} ms`
}

export function GroupStatusRow({
  target,
  generatedAt,
  intervalSeconds,
}: {
  target: PublicProbeTarget
  generatedAt: number
  intervalSeconds: number
}) {
  const { t } = useTranslation()
  const presentation = getStatePresentation(target.state)
  const StateIcon = presentation.icon
  const latestUpdate = formatTimestamp(target.latest_checked_at)
  const nextUpdate = formatTimestamp(target.next_check_at)
  const isStale =
    target.latest_checked_at !== null &&
    generatedAt - target.latest_checked_at > intervalSeconds * 2
  const points = padProbeHistory(target.history, target.key)

  return (
    <article
      className='border-border bg-card grid min-h-[22rem] w-full min-w-0 grid-rows-[auto_auto_auto_1fr_auto] gap-5 rounded-lg border p-5'
      data-state={target.state}
      data-testid='target-status-card'
    >
      <header className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0 flex-1'>
          <h3
            className='truncate text-base font-semibold'
            title={target.display_name}
          >
            {target.display_name}
          </h3>
          <p
            className='text-muted-foreground mt-1 truncate text-sm'
            title={target.model}
          >
            {target.model}
          </p>
        </div>
        <Badge
          variant='outline'
          className={cn(
            'h-6 max-w-[55%] shrink-0 gap-1 whitespace-normal',
            presentation.badgeClassName
          )}
          title={t(presentation.label)}
        >
          <StateIcon className='shrink-0' aria-hidden='true' />
          <span className='break-words'>{t(presentation.label)}</span>
        </Badge>
      </header>

      <dl className='grid min-w-0 grid-cols-2 gap-3'>
        <div className='bg-muted/45 min-w-0 rounded-md p-3'>
          <dt className='text-muted-foreground truncate text-xs'>
            {t('Conversation latency')}
          </dt>
          <dd className='mt-2 text-lg font-semibold tabular-nums'>
            {formatLatency(target.chat_latency_ms)}
          </dd>
        </div>
        <div className='bg-muted/45 min-w-0 rounded-md p-3'>
          <dt className='text-muted-foreground truncate text-xs'>
            {t('Ping latency')}
          </dt>
          <dd className='mt-2 text-lg font-semibold tabular-nums'>
            {formatLatency(target.ping_latency_ms)}
          </dd>
        </div>
      </dl>

      <div className='flex items-end justify-between gap-3'>
        <div>
          <p className='text-muted-foreground text-xs'>
            {t('Last 60 availability')}
          </p>
          <p className='mt-1 text-xl font-semibold tabular-nums'>
            {formatAvailability(target.availability)}
          </p>
        </div>
        {isStale && (
          <span className='text-xs font-medium text-amber-700 dark:text-amber-400'>
            {t('Stale')}
          </span>
        )}
      </div>

      <div className='min-w-0 self-end'>
        <StatusTimeline points={points} targetName={target.display_name} />
      </div>

      <footer className='text-muted-foreground grid min-h-9 grid-cols-2 gap-3 border-t pt-3 text-xs'>
        <div className='min-w-0'>
          <span className='block'>{t('Last checked')}</span>
          <strong className='text-foreground mt-0.5 block font-medium break-words'>
            {latestUpdate ?? t('No observations yet')}
          </strong>
        </div>
        <div className='min-w-0 text-right'>
          <span className='block'>{t('Next check')}</span>
          <strong className='text-foreground mt-0.5 block font-medium break-words'>
            {nextUpdate ?? '--'}
          </strong>
        </div>
      </footer>
    </article>
  )
}
