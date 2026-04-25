import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, X, SquarePen } from 'lucide-react'
import type { ThreadResponse } from '../api'
import { searchThreads } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { isPerfDebugEnabled, recordPerfDuration, recordPerfValue } from '../perfDebug'
import { useTimeZone } from '@arkloop/shared'

type DateGroup = {
  label: string
  threads: ThreadResponse[]
}

const MS_PER_DAY = 86_400_000

function getZonedMidnight(value: string | Date, timeZone: string): number | null {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(date)
  const year = Number(parts.find((part) => part.type === 'year')?.value ?? '')
  const month = Number(parts.find((part) => part.type === 'month')?.value ?? '')
  const day = Number(parts.find((part) => part.type === 'day')?.value ?? '')
  if ([year, month, day].some((n) => Number.isNaN(n))) return null
  return Date.UTC(year, month - 1, day)
}

function groupByDate(threads: ThreadResponse[], labels: {
  today: string
  yesterday: string
  lastWeek: string
  earlier: string
}, timeZone: string): DateGroup[] {
  const now = new Date()
  const todayStart = getZonedMidnight(now, timeZone)
  const yesterdayStart = todayStart === null ? null : todayStart - MS_PER_DAY
  const weekStart = todayStart === null ? null : todayStart - 6 * MS_PER_DAY

  const buckets: [string, ThreadResponse[]][] = [
    [labels.today, []],
    [labels.yesterday, []],
    [labels.lastWeek, []],
    [labels.earlier, []],
  ]

  for (const thread of threads) {
    const threadStart = getZonedMidnight(thread.created_at, timeZone)
    if (threadStart === null) {
      buckets[3][1].push(thread)
      continue
    }
    if (todayStart !== null && threadStart >= todayStart) {
      buckets[0][1].push(thread)
    } else if (yesterdayStart !== null && threadStart >= yesterdayStart) {
      buckets[1][1].push(thread)
    } else if (weekStart !== null && threadStart >= weekStart) {
      buckets[2][1].push(thread)
    } else {
      buckets[3][1].push(thread)
    }
  }

  return buckets
    .filter(([, items]) => items.length > 0)
    .map(([label, items]) => ({ label, threads: items }))
}

type Props = {
  threads: ThreadResponse[]
  accessToken: string
  onClose: () => void
}

const INITIAL_VISIBLE_THREAD_COUNT = 18

export function ChatsSearchModal({ threads, accessToken, onClose }: Props) {
  const navigate = useNavigate()
  const { t } = useLocale()
  const { timeZone } = useTimeZone()
  const [query, setQuery] = useState('')
  const [searchResults, setSearchResults] = useState<ThreadResponse[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [renderedThreadCount, setRenderedThreadCount] = useState(() =>
    Math.min(threads.length, INITIAL_VISIBLE_THREAD_COUNT),
  )
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const allowOutsideCloseRef = useRef(false)
  const openMarkerRef = useRef<{
    startedAt: number
    sample: Record<string, string | number | boolean | null | undefined>
  } | null>(null)

  useLayoutEffect(() => {
    if (!isPerfDebugEnabled() || typeof performance === 'undefined') return
    const marker = (window as Window & {
      __arkloopSearchOpenStarted?: {
        startedAt: number
        sample: Record<string, string | number | boolean | null | undefined>
      }
    }).__arkloopSearchOpenStarted
    if (!marker) return
    openMarkerRef.current = marker
    recordPerfDuration('desktop_search_modal_mount_commit', performance.now() - marker.startedAt, {
      ...marker.sample,
      threadCount: threads.length,
      phase: 'commit',
    })
    ;(window as Window & { __arkloopSearchOpenStarted?: unknown }).__arkloopSearchOpenStarted = undefined
  }, [threads.length])

  useEffect(() => {
    if (!isPerfDebugEnabled()) return
    recordPerfValue('desktop_search_modal_render_count', 1, 'count', {
      threadCount: threads.length,
      renderedThreadCount,
      queryLength: query.length,
      searching,
      resultCount: searchResults?.length ?? threads.length,
    })
  })

  useEffect(() => {
    inputRef.current?.focus()
    allowOutsideCloseRef.current = false
    const unlockId = requestAnimationFrame(() => {
      allowOutsideCloseRef.current = true
    })
    if (!isPerfDebugEnabled() || typeof performance === 'undefined') return
    const marker = openMarkerRef.current
    if (!marker) return
    const frameId = requestAnimationFrame(() => {
      recordPerfDuration('desktop_search_modal_first_frame', performance.now() - marker.startedAt, {
        ...marker.sample,
        threadCount: threads.length,
        phase: 'frame',
      })
      openMarkerRef.current = null
    })
    return () => {
      cancelAnimationFrame(unlockId)
      cancelAnimationFrame(frameId)
    }
  }, [threads.length])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // 全文搜索：debounce 300ms 后调后端
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)

    const q = query.trim()
    if (!q) {
      const id = requestAnimationFrame(() => {
        setSearchResults(null)
        setSearching(false)
      })
      return () => {
        if (debounceRef.current) clearTimeout(debounceRef.current)
        cancelAnimationFrame(id)
      }
    }

    const pendingId = requestAnimationFrame(() => setSearching(true))
    debounceRef.current = setTimeout(() => {
      void searchThreads(accessToken, q, 'chat').then((results) => {
        setSearchResults(results)
      }).catch(() => {
        setSearchResults([])
      }).finally(() => {
        setSearching(false)
      })
    }, 300)

    return () => {
      cancelAnimationFrame(pendingId)
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, accessToken])

  const displayThreads = searchResults ?? threads
  const visibleThreadCount = Math.min(renderedThreadCount, displayThreads.length)
  const visibleThreads = useMemo(
    () => displayThreads.slice(0, visibleThreadCount),
    [displayThreads, visibleThreadCount],
  )

  useEffect(() => {
    const shouldDeferTail = displayThreads.length > INITIAL_VISIBLE_THREAD_COUNT
    const nextInitialCount = shouldDeferTail ? INITIAL_VISIBLE_THREAD_COUNT : displayThreads.length
    setRenderedThreadCount(nextInitialCount)
    if (!shouldDeferTail) return
    const startedAt = typeof performance !== 'undefined' ? performance.now() : 0
    const frameId = requestAnimationFrame(() => {
      setRenderedThreadCount(displayThreads.length)
      if (isPerfDebugEnabled() && typeof performance !== 'undefined') {
        recordPerfDuration('desktop_search_modal_tail_fill', performance.now() - startedAt, {
          totalThreadCount: displayThreads.length,
          initialThreadCount: nextInitialCount,
          queryLength: query.length,
          searching,
        })
      }
    })
    return () => cancelAnimationFrame(frameId)
  }, [displayThreads, query.length, searching])

  const dateLabels = useMemo(() => ({
    today: t.searchToday,
    yesterday: t.searchYesterday,
    lastWeek: t.searchLastWeek,
    earlier: t.searchEarlier,
  }), [t])

  const groups = useMemo(() => {
    const next = groupByDate(visibleThreads, dateLabels, timeZone)
    return next
  }, [dateLabels, timeZone, visibleThreads])

  const handleThreadClick = useCallback(
    (threadId: string) => {
      navigate(`/t/${threadId}`)
    },
    [navigate],
  )

  const handleNewChat = useCallback(() => {
    navigate('/')
  }, [navigate])

  return (
    <div
      className="overlay-fade-in fixed inset-0 z-50 flex items-start justify-center pt-[120px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => {
        if (!allowOutsideCloseRef.current) return
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        className="modal-enter flex w-full max-w-[520px] flex-col overflow-hidden rounded-xl"
        style={{
          background: 'var(--c-bg-menu)',
          border: '1px solid var(--c-border)',
          boxShadow: '0 8px 24px rgba(0,0,0,0.22)',
          maxHeight: '70vh',
        }}
      >
        {/* 搜索输入 */}
        <div
          className="flex items-center gap-3 px-4 py-3"
          style={{ borderBottom: '1px solid var(--c-border)' }}
        >
          <Search size={16} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t.searchChatsPlaceholder}
            className="flex-1 bg-transparent text-sm outline-none"
            style={{
              color: 'var(--c-text-primary)',
              caretColor: 'var(--c-text-primary)',
            }}
          />
          {query ? (
            <button
              onClick={() => setQuery('')}
              className="flex items-center justify-center transition-opacity hover:opacity-70"
              style={{ color: 'var(--c-text-muted)' }}
            >
              <X size={14} />
            </button>
          ) : (
            <button
              onClick={onClose}
              className="flex items-center justify-center transition-opacity hover:opacity-70"
              style={{ color: 'var(--c-text-muted)' }}
            >
              <X size={16} />
            </button>
          )}
        </div>

        {/* 结果区 */}
        <div className="flex-1 overflow-y-auto">
          <div className="p-2">
            <button
              onClick={handleNewChat}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-[9px] text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ color: 'var(--c-text-secondary)' }}
            >
              <span
                className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full"
                style={{ background: 'var(--c-bg-plus)' }}
              >
                <SquarePen size={11} />
              </span>
              <span>{t.newChat}</span>
            </button>
          </div>

          {!searching && groups.length > 0 && (
            <div className="pb-2">
              {groups.map(({ label, threads: groupItems }) => (
                <div key={label}>
                  <div
                    className="px-4 py-[6px] text-xs font-medium"
                    style={{ color: 'var(--c-text-muted)' }}
                  >
                    {label}
                  </div>
                  <div className="flex flex-col gap-[2px] px-2">
                    {groupItems.map((thread) => (
                      <button
                        key={thread.id}
                        onClick={() => handleThreadClick(thread.id)}
                        className="flex w-full items-center rounded-lg px-3 py-[8px] text-left text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
                        style={{ color: 'var(--c-text-secondary)' }}
                      >
                        <span className="truncate">{thread.title ?? t.untitled}</span>
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}

          {!searching && query.trim() && groups.length === 0 && (
            <div
              className="px-4 py-8 text-center text-sm"
              style={{ color: 'var(--c-text-muted)' }}
            >
              {t.searchNoResults}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
