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
import { useLayoutEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import type { GroupProbeState, PublicGroupProbeBucket } from '../types'

const bucketStyles: Record<GroupProbeState, string> = {
  healthy: 'bg-emerald-500 dark:bg-emerald-400',
  degraded: 'bg-amber-500 dark:bg-amber-400',
  down: 'bg-red-500 dark:bg-red-400',
  unknown: 'bg-muted-foreground/25',
}

const stateLabels: Record<GroupProbeState, string> = {
  healthy: 'Healthy',
  degraded: 'Degraded',
  down: 'Unavailable',
  unknown: 'Unknown',
}

const BUCKET_MIN_WIDTH_PX = 8
const BUCKET_GAP_PX = 4
const END_EDGE_TOLERANCE_PX = 2

function getMaxScrollLeft(element: HTMLElement) {
  return Math.max(0, element.scrollWidth - element.clientWidth)
}

function getTimelineMinWidth(bucketCount: number) {
  if (bucketCount === 0) return 0
  return bucketCount * BUCKET_MIN_WIDTH_PX + (bucketCount - 1) * BUCKET_GAP_PX
}

function formatBucketTime(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(timestamp * 1000))
}

export function StatusTimeline({
  buckets,
  groupName,
  intervalMinutes,
}: {
  buckets: PublicGroupProbeBucket[]
  groupName: string
  intervalMinutes: number
}) {
  const { t } = useTranslation()
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const hasPositionedRef = useRef(false)
  const scrollPositionRef = useRef({ left: 0, atEnd: true })

  useLayoutEffect(() => {
    if (buckets.length === 0) return

    const element = scrollContainerRef.current
    if (!element) return

    const maxScrollLeft = getMaxScrollLeft(element)
    if (!hasPositionedRef.current) {
      element.scrollLeft = maxScrollLeft
      hasPositionedRef.current = true
      scrollPositionRef.current = { left: maxScrollLeft, atEnd: true }
      return
    }

    const nextScrollLeft = scrollPositionRef.current.atEnd
      ? maxScrollLeft
      : Math.min(scrollPositionRef.current.left, maxScrollLeft)
    element.scrollLeft = nextScrollLeft
    scrollPositionRef.current = {
      left: nextScrollLeft,
      atEnd: maxScrollLeft - nextScrollLeft <= END_EDGE_TOLERANCE_PX,
    }
  }, [buckets])

  const handleScroll = (element: HTMLDivElement) => {
    const maxScrollLeft = getMaxScrollLeft(element)
    scrollPositionRef.current = {
      left: element.scrollLeft,
      atEnd: maxScrollLeft - element.scrollLeft <= END_EDGE_TOLERANCE_PX,
    }
  }

  return (
    <div
      ref={scrollContainerRef}
      className='max-w-full overflow-x-auto pb-1'
      data-testid='status-timeline-scroll'
      onScroll={(event) => handleScroll(event.currentTarget)}
    >
      <div
        className='grid h-9 w-full gap-1'
        data-testid='status-timeline-grid'
        data-interval-minutes={intervalMinutes}
        style={{
          gridTemplateColumns: `repeat(${Math.max(buckets.length, 1)}, minmax(${BUCKET_MIN_WIDTH_PX}px, 1fr))`,
          minWidth: `${getTimelineMinWidth(buckets.length)}px`,
        }}
        aria-label={t('24-hour probe history for {{group}}', {
          group: groupName,
        })}
      >
        <TooltipProvider delay={150}>
          {buckets.map((bucket) => {
            const label = t(stateLabels[bucket.state])
            const time = formatBucketTime(bucket.startedAt)
            return (
              <Tooltip key={bucket.startedAt}>
                <TooltipTrigger
                  render={
                    <span
                      tabIndex={0}
                      aria-label={`${time}: ${label}`}
                      className={cn(
                        'block h-9 w-full rounded-sm outline-none ring-offset-2 transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-ring',
                        bucketStyles[bucket.state]
                      )}
                    />
                  }
                />
                <TooltipContent>
                  <span>{time}</span>
                  <span aria-hidden='true'>·</span>
                  <span>{label}</span>
                  <span aria-hidden='true'>·</span>
                  <span>
                    {t('{{count}} samples', { count: bucket.sampleCount })}
                  </span>
                </TooltipContent>
              </Tooltip>
            )
          })}
        </TooltipProvider>
      </div>
    </div>
  )
}
