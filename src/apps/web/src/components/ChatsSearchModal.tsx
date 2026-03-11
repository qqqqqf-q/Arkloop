import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, X, SquarePen } from 'lucide-react'
import type { ThreadResponse } from '../api'
import { searchThreads } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type DateGroup = {
  label: string
  threads: ThreadResponse[]
}

function groupByDate(threads: ThreadResponse[], labels: {
  today: string
  yesterday: string
  lastWeek: string
  earlier: string
}): DateGroup[] {
  const now = new Date()
  const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterdayStart = new Date(todayStart.getTime() - 86_400_000)
  const weekStart = new Date(todayStart.getTime() - 6 * 86_400_000)

  const buckets: [string, ThreadResponse[]][] = [
    [labels.today, []],
    [labels.yesterday, []],
    [labels.lastWeek, []],
    [labels.earlier, []],
  ]

  for (const thread of threads) {
    const d = new Date(thread.created_at)
    if (d >= todayStart) {
      buckets[0][1].push(thread)
    } else if (d >= yesterdayStart) {
      buckets[1][1].push(thread)
    } else if (d >= weekStart) {
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

export function ChatsSearchModal({ threads, accessToken, onClose }: Props) {
  const navigate = useNavigate()
  const { t } = useLocale()
  const [query, setQuery] = useState('')
  const [searchResults, setSearchResults] = useState<ThreadResponse[] | null>(null)
  const [searching, setSearching] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

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
      setSearchResults(null)
      setSearching(false)
      return
    }

    setSearching(true)
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
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, accessToken])

  const displayThreads = searchResults ?? threads

  const dateLabels = useMemo(() => ({
    today: t.searchToday,
    yesterday: t.searchYesterday,
    lastWeek: t.searchLastWeek,
    earlier: t.searchEarlier,
  }), [t])

  const groups = useMemo(() => groupByDate(displayThreads, dateLabels), [displayThreads, dateLabels])

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
