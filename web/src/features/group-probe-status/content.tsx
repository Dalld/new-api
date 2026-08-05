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
import { Activity, AlertCircle, Clock3, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { GroupStatusRow } from './components/group-status-row'
import type { PublicGroupProbeStatus } from './types'

function StatusPageSkeleton({ label }: { label: string }) {
  return (
    <div aria-label={label} className='space-y-1'>
      {[0, 1, 2].map((item) => (
        <div
          key={item}
          className='grid min-h-48 gap-5 border-b py-7 lg:grid-cols-[minmax(13rem,1fr)_minmax(32rem,2.4fr)]'
        >
          <div className='space-y-4'>
            <Skeleton className='h-6 w-36' />
            <Skeleton className='h-4 w-24' />
            <Skeleton className='h-11 w-full max-w-56' />
          </div>
          <div className='space-y-3'>
            <Skeleton className='h-9 w-full' />
            <Skeleton className='h-4 w-3/4' />
          </div>
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

export type GroupProbeStatusContentProps = {
  data?: PublicGroupProbeStatus
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
  const groups = data?.groups ?? []
  const hasStaleData = groups.some((group) => group.stale)
  const hasPartialData = groups.some(
    (group) => group.state === 'unknown' || group.sampleCount < 3
  )
  let statusContent = null

  if (isPending) {
    statusContent = <StatusPageSkeleton label={t('Loading status')} />
  } else if (groups.length > 0) {
    statusContent = (
      <section aria-label={t('Public group probe status')}>
        {groups.map((group) => (
          <GroupStatusRow key={group.group} group={group} />
        ))}
      </section>
    )
  } else if (!isError) {
    statusContent = (
      <div className='flex min-h-64 flex-col items-center justify-center gap-3 border-y text-center'>
        <Activity className='text-muted-foreground size-8' aria-hidden='true' />
        <h2 className='font-medium'>{t('No public probe groups')}</h2>
        <p className='text-muted-foreground max-w-md text-sm'>
          {t('There are no public synthetic probe results to display.')}
        </p>
      </div>
    )
  }

  return (
    <main className='mx-auto w-full max-w-6xl px-4 py-10 sm:px-6 sm:py-14'>
      <header className='flex flex-col gap-5 border-b pb-7 sm:flex-row sm:items-end sm:justify-between'>
        <div className='space-y-2'>
          <div className='text-primary flex items-center gap-2 text-sm font-medium'>
            <Activity className='size-4' aria-hidden='true' />
            <span>{t('Synthetic probes')}</span>
          </div>
          <h1 className='text-3xl font-semibold'>{t('Group status')}</h1>
          <p className='text-muted-foreground max-w-2xl text-sm leading-6'>
            {t(
              'Availability shown here comes from scheduled synthetic requests, not real user traffic or an SLA.'
            )}
          </p>
        </div>
        <div className='text-muted-foreground flex min-h-9 items-center gap-2 text-sm'>
          <Clock3 className='size-4' aria-hidden='true' />
          <span>
            {data
              ? t('Updated {{time}}', {
                  time: formatGeneratedAt(data.generatedAt),
                })
              : t('Waiting for status')}
          </span>
        </div>
      </header>

      <div className='space-y-3 py-5' aria-live='polite'>
        {isError && (
          <Alert variant='destructive'>
            <AlertCircle aria-hidden='true' />
            <AlertTitle>{t('Status could not be refreshed')}</AlertTitle>
            <AlertDescription>
              {data
                ? t('The last available result is still shown below.')
                : t(
                    'Probe status is temporarily unavailable. Please try again.'
                  )}
            </AlertDescription>
            <Button
              variant='outline'
              size='sm'
              className='mt-3 w-fit'
              disabled={isFetching}
              onClick={() => void refetch()}
            >
              <RefreshCw className={isFetching ? 'animate-spin' : ''} />
              {t('Retry')}
            </Button>
          </Alert>
        )}

        {hasStaleData && (
          <Alert>
            <Clock3 aria-hidden='true' />
            <AlertTitle>{t('Some results are stale')}</AlertTitle>
            <AlertDescription>
              {t(
                'One or more groups have not reported within the expected probe interval.'
              )}
            </AlertDescription>
          </Alert>
        )}

        {hasPartialData && !hasStaleData && (
          <Alert>
            <AlertCircle aria-hidden='true' />
            <AlertTitle>{t('Partial history')}</AlertTitle>
            <AlertDescription>
              {t(
                'Some groups do not have enough recent observations to determine a current state.'
              )}
            </AlertDescription>
          </Alert>
        )}
      </div>

      {statusContent}
    </main>
  )
}
