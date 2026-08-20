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
  Activity,
  AlertCircle,
  CheckCircle2,
  Clock3,
  HelpCircle,
  RefreshCw,
  TriangleAlert,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { GroupStatusRow } from './components/group-status-row'
import type { PublicProbeData, PublicProbeTarget } from './types'

type AggregateState =
  | 'operational'
  | 'degraded'
  | 'failed'
  | 'partial'
  | 'stale'
  | 'unknown'

const aggregatePresentation: Record<
  AggregateState,
  { label: string; className: string; icon: typeof CheckCircle2 }
> = {
  operational: {
    label: 'All systems operational',
    className:
      'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
    icon: CheckCircle2,
  },
  degraded: {
    label: 'Degraded performance',
    className:
      'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400',
    icon: TriangleAlert,
  },
  failed: {
    label: 'Service unavailable',
    className: 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-400',
    icon: XCircle,
  },
  partial: {
    label: 'Partial outage',
    className:
      'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400',
    icon: AlertCircle,
  },
  stale: {
    label: 'Status data is stale',
    className:
      'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400',
    icon: Clock3,
  },
  unknown: {
    label: 'Collecting data',
    className: 'border-border bg-muted text-muted-foreground',
    icon: HelpCircle,
  },
}

function isTargetStale(
  target: PublicProbeTarget,
  generatedAt: number,
  intervalSeconds: number
) {
  return (
    target.latest_checked_at !== null &&
    generatedAt - target.latest_checked_at > intervalSeconds * 2
  )
}

function getAggregateState(data?: PublicProbeData): AggregateState {
  const completed =
    data?.targets.filter((target) => target.latest_checked_at !== null) ?? []
  if (!data || completed.length === 0) {
    return 'unknown'
  }

  const fresh = completed.filter(
    (target) => !isTargetStale(target, data.generated_at, data.interval_seconds)
  )
  if (fresh.length === 0) {
    return 'stale'
  }

  const knownFresh = fresh.filter((target) => target.state !== 'unknown')
  if (knownFresh.length === 0) {
    return 'unknown'
  }

  const hasSuccess = knownFresh.some(
    (target) => target.state === 'operational' || target.state === 'degraded'
  )
  const hasFailure = knownFresh.some((target) => {
    const state: string = target.state
    return state === 'failed' || state === 'validation_failed'
  })
  if (hasSuccess && hasFailure) {
    return 'partial'
  }
  if (hasFailure) {
    return 'failed'
  }
  if (knownFresh.some((target) => target.state === 'degraded')) {
    return 'degraded'
  }
  return 'operational'
}

function StatusPageSkeleton({ label }: { label: string }) {
  return (
    <div
      aria-label={label}
      aria-live='polite'
      className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'
      data-layout='stable'
      role='status'
    >
      <span className='sr-only'>{label}</span>
      {[0, 1, 2].map((item) => (
        <div
          key={item}
          className='border-border bg-card min-h-[22rem] space-y-6 rounded-lg border p-5'
        >
          <div className='flex items-start justify-between gap-4'>
            <div className='space-y-2'>
              <Skeleton className='h-5 w-28' />
              <Skeleton className='h-4 w-20' />
            </div>
            <Skeleton className='h-5 w-20' />
          </div>
          <div className='grid grid-cols-2 gap-3'>
            <Skeleton className='h-20 w-full' />
            <Skeleton className='h-20 w-full' />
          </div>
          <Skeleton className='h-14 w-full' />
          <Skeleton className='h-10 w-full' />
          <Skeleton className='h-9 w-full' />
        </div>
      ))}
    </div>
  )
}

function formatGeneratedAt(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(timestamp * 1000))
}

function groupTargetsByFirstAppearance(targets: PublicProbeTarget[]) {
  const groupedTargets = new Map<string, PublicProbeTarget[]>()

  for (const target of targets) {
    const group = groupedTargets.get(target.group)
    if (group) {
      group.push(target)
    } else {
      groupedTargets.set(target.group, [target])
    }
  }

  return groupedTargets
}

export type GroupProbeStatusContentProps = {
  data?: PublicProbeData
  isPending: boolean
  isError: boolean
  isFetching: boolean
  refetch: () => void | Promise<unknown>
}

export function GroupProbeStatusContent({
  data,
  isPending,
  isError,
  isFetching,
  refetch,
}: GroupProbeStatusContentProps) {
  const { t } = useTranslation()
  const targets = data?.targets ?? []
  const groupedTargets = groupTargetsByFirstAppearance(targets)
  const aggregateState = getAggregateState(data)
  const aggregate = aggregatePresentation[aggregateState]
  const AggregateIcon = aggregate.icon
  const hasUnknownTargets = targets.some(
    (target) => target.latest_checked_at === null || target.state === 'unknown'
  )
  const hasStaleTargets =
    data?.targets.some((target) =>
      isTargetStale(target, data.generated_at, data.interval_seconds)
    ) ?? false

  let statusContent = null
  if (isPending && !data) {
    statusContent = <StatusPageSkeleton label={t('Loading status')} />
  } else if (targets.length > 0 && data) {
    statusContent = (
      <section
        aria-label={t('Public probe status')}
        className='space-y-8'
        data-layout='stable'
        data-testid='status-target-groups'
      >
        {[...groupedTargets].map(([group, groupTargets]) => (
          <section
            key={group}
            aria-label={group}
            className='min-w-0 space-y-4'
            data-testid='status-target-group'
          >
            <h2
              className='text-foreground text-lg font-semibold break-words sm:text-xl'
              data-testid='status-group-heading'
            >
              {group}
            </h2>
            <div
              className='grid min-w-0 grid-cols-1 items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3'
              data-testid='status-target-grid'
            >
              {groupTargets.map((target) => (
                <GroupStatusRow
                  key={target.key}
                  generatedAt={data.generated_at}
                  intervalSeconds={data.interval_seconds}
                  target={target}
                />
              ))}
            </div>
          </section>
        ))}
      </section>
    )
  } else if (!isError) {
    statusContent = (
      <div
        className='border-border flex min-h-[22rem] flex-col items-center justify-center gap-3 rounded-lg border text-center'
        data-layout='stable'
      >
        <Activity className='text-muted-foreground size-8' aria-hidden='true' />
        <h2 className='font-medium'>{t('No probe targets')}</h2>
        <p className='text-muted-foreground max-w-md px-5 text-sm'>
          {t('There are no public probe results to display.')}
        </p>
      </div>
    )
  } else {
    statusContent = (
      <div
        className='border-border flex min-h-[22rem] items-center justify-center rounded-lg border'
        data-layout='stable'
      />
    )
  }

  let noticeContent = (
    <p className='text-muted-foreground text-sm'>
      {t('Latest synthetic probe observations')}
    </p>
  )
  if (hasUnknownTargets) {
    noticeContent = (
      <Alert className='w-full'>
        <HelpCircle aria-hidden='true' />
        <AlertTitle>{t('Collecting data')}</AlertTitle>
        <AlertDescription>
          {t('Some targets do not have a completed observation yet.')}
        </AlertDescription>
      </Alert>
    )
  }
  if (hasStaleTargets) {
    noticeContent = (
      <Alert className='w-full'>
        <Clock3 aria-hidden='true' />
        <AlertTitle>{t('Status data is stale')}</AlertTitle>
        <AlertDescription>
          {t('One or more targets missed two expected probe intervals.')}
        </AlertDescription>
      </Alert>
    )
  }
  if (isError) {
    noticeContent = (
      <Alert variant='destructive' className='w-full'>
        <AlertCircle aria-hidden='true' />
        <AlertTitle>{t('Status could not be refreshed')}</AlertTitle>
        <AlertDescription>
          {data
            ? t('The last available result is still shown below.')
            : t('Probe status is temporarily unavailable. Please try again.')}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <div className='mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 sm:py-10'>
      <header
        className='border-border flex flex-col gap-4 border-b pb-6 sm:flex-row sm:flex-wrap sm:items-end sm:justify-between sm:gap-x-5'
        data-testid='status-page-header'
      >
        <div className='w-full min-w-0 space-y-3 sm:flex-1'>
          <div className='text-primary flex items-center gap-2 text-sm font-medium'>
            <Activity className='size-4' aria-hidden='true' />
            <span>{t('Synthetic probes')}</span>
          </div>
          <div className='flex flex-wrap items-center gap-3'>
            <h1 className='text-2xl font-semibold sm:text-3xl'>
              {t('Service status')}
            </h1>
            <Badge
              variant='outline'
              className={cn('h-6 gap-1.5', aggregate.className)}
              data-testid='aggregate-status'
            >
              <AggregateIcon aria-hidden='true' />
              {t(aggregate.label)}
            </Badge>
          </div>
        </div>

        <div
          className='flex min-h-9 w-full min-w-0 items-center justify-between gap-2 sm:w-auto sm:shrink-0 sm:justify-start'
          data-testid='status-updated-controls'
        >
          <div className='text-muted-foreground flex min-w-0 flex-1 items-center gap-2 text-sm sm:flex-none'>
            <Clock3 className='size-4 shrink-0' aria-hidden='true' />
            <span className='min-w-0 truncate sm:max-w-72'>
              {data
                ? t('Updated {{time}}', {
                    time: formatGeneratedAt(data.generated_at),
                  })
                : t('Waiting for status')}
            </span>
          </div>
          <Button
            variant='outline'
            size='icon-sm'
            disabled={isFetching}
            aria-label={t('Refresh status')}
            title={t('Refresh status')}
            onClick={() => void refetch()}
          >
            <RefreshCw
              className={cn('size-4', isFetching && 'animate-spin')}
              aria-hidden='true'
            />
          </Button>
        </div>
      </header>

      <div className='flex min-h-24 items-center py-3' data-layout='stable'>
        {noticeContent}
      </div>

      {statusContent}
    </div>
  )
}
