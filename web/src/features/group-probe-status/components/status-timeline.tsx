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
  type CSSProperties,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import type { PublicProbeErrorCode, PublicProbeHistoryPoint } from '../types'

const pointStyles: Record<string, string> = {
  operational: 'bg-emerald-500 dark:bg-emerald-400',
  degraded: 'bg-amber-500 dark:bg-amber-400',
  failed: 'bg-red-500 dark:bg-red-400',
  validation_failed: 'bg-red-500 dark:bg-red-400',
  unknown: 'bg-muted-foreground/25',
}

const stateLabels: Record<string, string> = {
  operational: 'Operational',
  degraded: 'Degraded',
  failed: 'Unavailable',
  validation_failed: 'Validation failed',
  unknown: 'Unknown',
}

const errorLabels: Record<PublicProbeErrorCode, string> = {
  unsupported_provider: 'Provider is not supported',
  timeout: 'Request timed out',
  network_error: 'Network connection failed',
  provider_rejected: 'Provider rejected the request',
  empty_response: 'Provider returned an empty response',
  validation_failed: 'Response validation failed',
  invalid_target: 'Probe target is unavailable',
  response_too_large: 'Provider response was too large',
}

const POINT_MIN_WIDTH_PX = 24
const POINT_GAP_PX = 4
const END_EDGE_TOLERANCE_PX = 2
const HOVER_DELAY_MS = 100
const HOVER_CLOSE_DELAY_MS = 80

function getMaxScrollLeft(element: HTMLElement) {
  return Math.max(0, element.scrollWidth - element.clientWidth)
}

function getTimelineMinWidth(pointCount: number) {
  if (pointCount === 0) return 0
  return pointCount * POINT_MIN_WIDTH_PX + (pointCount - 1) * POINT_GAP_PX
}

function formatPointTime(timestamp: number | null) {
  if (timestamp === null) return null
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(new Date(timestamp * 1000))
}

function formatLatency(value: number | null) {
  return value === null ? '--' : `${Math.round(value)} ms`
}

export function StatusTimeline({
  points,
  targetName,
}: {
  points: PublicProbeHistoryPoint[]
  targetName: string
}) {
  const { t } = useTranslation()
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const hasPositionedRef = useRef(false)
  const scrollPositionRef = useRef({ left: 0, atEnd: true })
  const hoverOpenTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const hoverCloseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [activePointId, setActivePointId] = useState<string | null>(null)
  const pointIdentity = useMemo(
    () => points.map((point) => point.id).join('|'),
    [points]
  )

  const clearHoverTimers = useCallback(() => {
    if (hoverOpenTimerRef.current !== null) {
      clearTimeout(hoverOpenTimerRef.current)
      hoverOpenTimerRef.current = null
    }
    if (hoverCloseTimerRef.current !== null) {
      clearTimeout(hoverCloseTimerRef.current)
      hoverCloseTimerRef.current = null
    }
  }, [])

  useEffect(() => clearHoverTimers, [clearHoverTimers])

  useEffect(() => {
    if (
      activePointId !== null &&
      !points.some((point) => point.id === activePointId)
    ) {
      setActivePointId(null)
    }
  }, [activePointId, pointIdentity, points])

  useEffect(() => {
    if (activePointId === null) return

    const closeFromOutsidePointer = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Element)) return
      const segment = target.closest('[data-testid="status-segment"]')
      if (segment?.getAttribute('data-point-id') === activePointId) return
      if (target.closest('[data-testid="probe-point-detail"]')) return
      clearHoverTimers()
      setActivePointId(null)
    }

    document.addEventListener('pointerdown', closeFromOutsidePointer, true)
    return () =>
      document.removeEventListener('pointerdown', closeFromOutsidePointer, true)
  }, [activePointId, clearHoverTimers])

  useLayoutEffect(() => {
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
  }, [pointIdentity])

  const handleScroll = (element: HTMLDivElement) => {
    const maxScrollLeft = getMaxScrollLeft(element)
    scrollPositionRef.current = {
      left: element.scrollLeft,
      atEnd: maxScrollLeft - element.scrollLeft <= END_EDGE_TOLERANCE_PX,
    }
  }

  const scheduleHoverOpen = (pointId: string) => {
    clearHoverTimers()
    hoverOpenTimerRef.current = setTimeout(() => {
      setActivePointId(pointId)
      hoverOpenTimerRef.current = null
    }, HOVER_DELAY_MS)
  }

  const scheduleHoverClose = (pointId: string) => {
    if (hoverOpenTimerRef.current !== null) {
      clearTimeout(hoverOpenTimerRef.current)
      hoverOpenTimerRef.current = null
    }
    hoverCloseTimerRef.current = setTimeout(() => {
      setActivePointId((current) => (current === pointId ? null : current))
      hoverCloseTimerRef.current = null
    }, HOVER_CLOSE_DELAY_MS)
  }

  return (
    <div className='space-y-2'>
      <div
        ref={scrollContainerRef}
        className='max-w-full overflow-x-auto pb-1'
        data-testid='status-timeline-scroll'
        onScroll={(event) => handleScroll(event.currentTarget)}
      >
        <div
          className='grid h-10 w-full min-w-[var(--timeline-min-width)] gap-1 sm:min-w-0 sm:gap-0.5'
          data-testid='status-timeline-grid'
          style={
            {
              '--timeline-min-width': `${getTimelineMinWidth(points.length)}px`,
              gridTemplateColumns: `repeat(${points.length}, minmax(0, 1fr))`,
            } as CSSProperties
          }
          aria-label={t('Recent probe history for {{target}}', {
            target: targetName,
          })}
        >
          <TooltipProvider delay={HOVER_DELAY_MS}>
            {points.map((point) => {
              const stateLabel = t(stateLabels[point.state] ?? 'Unknown')
              const time = formatPointTime(point.checked_at)
              const timeLabel = time ?? t('No observation')
              const chatLatency = formatLatency(point.chat_latency_ms)
              const pingLatency = formatLatency(point.ping_latency_ms)
              const errorLabel = point.error_code
                ? t(errorLabels[point.error_code])
                : null
              const ariaLabel = [
                targetName,
                timeLabel,
                stateLabel,
                t('Conversation latency: {{latency}}', {
                  latency: chatLatency,
                }),
                t('Ping latency: {{latency}}', { latency: pingLatency }),
                errorLabel,
              ]
                .filter(Boolean)
                .join(', ')

              return (
                <Tooltip
                  key={point.id}
                  open={activePointId === point.id}
                  onOpenChange={(open) =>
                    setActivePointId((current) => {
                      if (open) return point.id
                      if (current === point.id) return null
                      return current
                    })
                  }
                >
                  <TooltipTrigger
                    render={
                      <button
                        type='button'
                        data-point-id={point.id}
                        data-point-state={point.state}
                        data-testid='status-segment'
                        aria-label={ariaLabel}
                        title={`${timeLabel}: ${stateLabel}`}
                        className={cn(
                          'flex h-10 w-full items-center justify-center rounded-sm p-0 outline-none ring-offset-2 transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-ring'
                        )}
                        onMouseEnter={() => scheduleHoverOpen(point.id)}
                        onMouseLeave={() => scheduleHoverClose(point.id)}
                        onFocus={() => {
                          clearHoverTimers()
                          setActivePointId(point.id)
                        }}
                        onClick={(event) => {
                          event.preventDefault()
                          clearHoverTimers()
                          setActivePointId(point.id)
                        }}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') {
                            event.preventDefault()
                            clearHoverTimers()
                            setActivePointId(point.id)
                          } else if (event.key === 'Escape') {
                            event.stopPropagation()
                            clearHoverTimers()
                            setActivePointId(null)
                          }
                        }}
                      >
                        <span
                          aria-hidden='true'
                          className={cn(
                            'block h-10 w-full max-w-2 rounded-[2px]',
                            pointStyles[point.state] ?? pointStyles.unknown
                          )}
                        />
                      </button>
                    }
                  />
                  <TooltipContent
                    className='bg-popover text-popover-foreground ring-foreground/10 w-64 max-w-[calc(100vw-2rem)] flex-col items-stretch gap-3 p-3 ring-1'
                    side='top'
                    sideOffset={8}
                    data-testid='probe-point-detail'
                    onMouseEnter={clearHoverTimers}
                    onMouseLeave={() => scheduleHoverClose(point.id)}
                  >
                    <div>
                      <p className='font-medium'>{timeLabel}</p>
                      <p className='text-muted-foreground mt-0.5 text-xs'>
                        {stateLabel}
                      </p>
                    </div>
                    <dl className='grid grid-cols-2 gap-2 text-xs'>
                      <div className='bg-muted rounded-md p-2'>
                        <dt className='text-muted-foreground'>
                          {t('Conversation latency')}
                        </dt>
                        <dd className='mt-1 font-medium tabular-nums'>
                          {chatLatency}
                        </dd>
                      </div>
                      <div className='bg-muted rounded-md p-2'>
                        <dt className='text-muted-foreground'>
                          {t('Ping latency')}
                        </dt>
                        <dd className='mt-1 font-medium tabular-nums'>
                          {pingLatency}
                        </dd>
                      </div>
                    </dl>
                    {errorLabel && (
                      <p className='text-destructive border-destructive/20 border-t pt-2 text-xs'>
                        {errorLabel}
                      </p>
                    )}
                  </TooltipContent>
                </Tooltip>
              )
            })}
          </TooltipProvider>
        </div>
      </div>

      <div className='text-muted-foreground flex items-center justify-between gap-4 text-xs'>
        <span>{t('Past')}</span>
        <span>{t('60 observations')}</span>
        <span>{t('Now')}</span>
      </div>
    </div>
  )
}
