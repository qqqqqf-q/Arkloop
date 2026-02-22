import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, X, Plus } from 'lucide-react'
import type { ThreadResponse } from '../api'

type DateGroup = {
  label: string
  threads: ThreadResponse[]
}

function groupByDate(threads: ThreadResponse[]): DateGroup[] {
  const now = new Date()
  const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterdayStart = new Date(todayStart.getTime() - 86_400_000)
  const weekStart = new Date(todayStart.getTime() - 6 * 86_400_000)

  const buckets: [string, ThreadResponse[]][] = [
    ['今天', []],
    ['昨天', []],
    ['最近7天', []],
    ['更早', []],
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
  onClose: () => void
}

export function ChatsSearchModal({ threads, onClose }: Props) {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

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

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return threads
    return threads.filter((t) => (t.title ?? '').toLowerCase().includes(q))
  }, [threads, query])

  const groups = useMemo(() => groupByDate(filtered), [filtered])

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
      className="fixed inset-0 z-50 flex items-start justify-center pt-[120px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        className="flex w-full max-w-[520px] flex-col overflow-hidden rounded-xl"
        style={{
          background: 'var(--c-bg-menu)',
          border: '1px solid var(--c-border)',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
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
            placeholder="搜索会话..."
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
                <Plus size={11} />
              </span>
              <span>New chat</span>
            </button>
          </div>

          {groups.length > 0 && (
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
                        <span className="truncate">{thread.title ?? '未命名会话'}</span>
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}

          {query.trim() && groups.length === 0 && (
            <div
              className="px-4 py-8 text-center text-sm"
              style={{ color: 'var(--c-text-muted)' }}
            >
              无匹配结果
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
