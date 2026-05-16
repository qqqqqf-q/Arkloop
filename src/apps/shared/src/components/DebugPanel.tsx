import { useState, useEffect, useLayoutEffect, useRef, useCallback, useMemo } from 'react'
import { createPortal } from 'react-dom'
import { X, Trash2, Minimize2, Maximize2, Pause, Play } from 'lucide-react'
import { debugBus, type DebugEntry } from '../debug-bus'

type ViewMode = 'compact' | 'detail'

type Props = {
  open: boolean
  onClose: () => void
}

function formatTs(ts: number): string {
  const d = new Date(ts)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${ms}`
}

function truncateData(data: unknown, max = 120): string {
  const raw = typeof data === 'string' ? data : JSON.stringify(data)
  if (!raw) return ''
  return raw.length > max ? raw.slice(0, max) + '...' : raw
}

function EntryRow({
  entry,
  prevTs,
  mode,
  expanded,
  onToggle,
}: {
  entry: DebugEntry
  prevTs: number | null
  mode: ViewMode
  expanded: boolean
  onToggle: () => void
}) {
  const delta = prevTs !== null ? entry.ts - prevTs : null

  return (
    <div
      className="px-3 py-1.5 text-xs font-mono cursor-pointer transition-colors duration-100 hover:bg-[var(--c-bg-sub)]"
      style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      onClick={onToggle}
    >
      <div className="flex items-center gap-2">
        <span className="text-[var(--c-text-muted)] shrink-0">{formatTs(entry.ts)}</span>
        {delta !== null && (
          <span
            className="shrink-0 tabular-nums"
            style={{
              minWidth: 52,
              textAlign: 'right',
              color:
                delta > 500
                  ? 'var(--c-status-danger-text)'
                  : delta > 200
                    ? 'var(--c-status-warn-text)'
                    : 'var(--c-text-muted)',
            }}
          >
            +{delta}ms
          </span>
        )}
        <span
          className="shrink-0 rounded-md px-2 py-0.5 text-[10px] font-medium"
          style={{
            background: 'var(--c-bg-sub)',
            color: 'var(--c-text-muted)',
          }}
        >
          {entry.type}
        </span>
        {entry.source && (
          <span className="text-[10px] text-[var(--c-text-tertiary)]">
            {entry.source}
          </span>
        )}
        {mode === 'compact' && !expanded && (
          <span className="truncate text-[var(--c-text-tertiary)]">
            {truncateData(entry.data)}
          </span>
        )}
      </div>
      {(mode === 'detail' || expanded) && (
        <pre
          className="mt-1 whitespace-pre-wrap break-all overflow-x-auto"
          style={{
            color: 'var(--c-text-secondary)',
            fontSize: 11,
            lineHeight: 1.4,
            maxHeight: 200,
            overflowY: 'auto',
          }}
        >
          {typeof entry.data === 'string' ? entry.data : JSON.stringify(entry.data, null, 2)}
        </pre>
      )}
    </div>
  )
}

const MODE_TABS: { key: ViewMode; label: string }[] = [
  { key: 'compact', label: 'Compact' },
  { key: 'detail', label: 'Detail' },
]

function ModeSwitch({ value, onChange }: { value: ViewMode; onChange: (v: ViewMode) => void }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [pill, setPill] = useState({ left: 0, width: 0 })
  const [animate, setAnimate] = useState(false)

  useLayoutEffect(() => {
    const el = containerRef.current
    if (!el) return
    const btn = el.querySelector<HTMLButtonElement>(`[data-mode="${value}"]`)
    if (!btn) return
    setPill({ left: btn.offsetLeft, width: btn.offsetWidth })
  }, [value])

  useLayoutEffect(() => {
    const id = requestAnimationFrame(() => setAnimate(true))
    return () => cancelAnimationFrame(id)
  }, [])

  return (
    <div
      ref={containerRef}
      className="relative flex h-8 shrink-0 items-center rounded-lg p-[2px]"
      style={{ background: 'var(--c-mode-switch-track)' }}
    >
      <span
        className="pointer-events-none absolute top-[2px] bottom-[2px] rounded-[7px]"
        style={{
          left: pill.left,
          width: pill.width,
          background: 'var(--c-mode-switch-pill)',
          border: '0.5px solid var(--c-mode-switch-border)',
          transition: animate ? 'left 150ms, width 150ms' : 'none',
        }}
      />
      {MODE_TABS.map((tab) => (
        <button
          key={tab.key}
          data-mode={tab.key}
          onClick={() => onChange(tab.key)}
          className="relative z-10 h-full rounded-[7px] px-3 text-xs transition-colors duration-200"
          style={{
            color: value === tab.key
              ? 'var(--c-mode-switch-active-text)'
              : 'var(--c-mode-switch-inactive-text)',
            fontWeight: 450,
          }}
        >
          {tab.label}
        </button>
      ))}
    </div>
  )
}

// secondary button: same as AddProviderModal cancel button, h-8 to align with input
const secondaryBtnCls =
  'inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-3 text-xs font-medium text-[var(--c-text-secondary)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)]'

// close / minimize: same as AddProviderModal close button
const windowBtnCls =
  'flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]'

export function DebugPanel({ open, onClose }: Props) {
  if (!open) return null
  return <DebugPanelContent onClose={onClose} />
}

function DebugPanelContent({ onClose }: { onClose: () => void }) {
  const [entries, setEntries] = useState<DebugEntry[]>(() => [...debugBus.snapshot()])
  const [mode, setMode] = useState<ViewMode>('compact')
  const [paused, setPaused] = useState(false)
  const [minimized, setMinimized] = useState(false)
  const [expandedIdx, setExpandedIdx] = useState<Set<number>>(new Set())
  const [filter, setFilter] = useState('')

  const [pos, setPos] = useState({ x: window.innerWidth - 560, y: 80 })
  const [size, setSize] = useState({ w: 520, h: 420 })
  const dragging = useRef(false)
  const resizing = useRef(false)
  const dragOffset = useRef({ x: 0, y: 0 })
  const listRef = useRef<HTMLDivElement>(null)
  const autoScroll = useRef(true)

  useEffect(() => {
    if (paused) return
    const unsub = debugBus.subscribe((entry) => {
      setEntries((prev) => {
        const next = [...prev, entry]
        return next.length > 500 ? next.slice(next.length - 500) : next
      })
    })
    return unsub
  }, [paused])

  useEffect(() => {
    if (!autoScroll.current || !listRef.current) return
    listRef.current.scrollTop = listRef.current.scrollHeight
  }, [entries])

  const handleListScroll = useCallback(() => {
    if (!listRef.current) return
    const el = listRef.current
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }, [])

  const handleDragStart = useCallback(
    (e: React.MouseEvent) => {
      if ((e.target as HTMLElement).closest('button, input')) return
      dragging.current = true
      dragOffset.current = { x: e.clientX - pos.x, y: e.clientY - pos.y }
      e.preventDefault()
    },
    [pos],
  )

  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    resizing.current = true
    dragOffset.current = { x: e.clientX, y: e.clientY }
    e.preventDefault()
    e.stopPropagation()
  }, [])

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (dragging.current) {
        setPos({
          x: Math.max(0, Math.min(e.clientX - dragOffset.current.x, window.innerWidth - 100)),
          y: Math.max(0, Math.min(e.clientY - dragOffset.current.y, window.innerHeight - 60)),
        })
      }
      if (resizing.current) {
        const dx = e.clientX - dragOffset.current.x
        const dy = e.clientY - dragOffset.current.y
        setSize((prev) => ({
          w: Math.max(360, prev.w + dx),
          h: Math.max(240, prev.h + dy),
        }))
        dragOffset.current = { x: e.clientX, y: e.clientY }
      }
    }
    const handleMouseUp = () => {
      dragging.current = false
      resizing.current = false
    }
    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
    return () => {
      window.removeEventListener('mousemove', handleMouseMove)
      window.removeEventListener('mouseup', handleMouseUp)
    }
  }, [])

  const filtered = useMemo(() => {
    if (!filter) return entries
    const lower = filter.toLowerCase()
    return entries.filter(
      (e) =>
        e.type.toLowerCase().includes(lower) ||
        (e.source && e.source.toLowerCase().includes(lower)),
    )
  }, [entries, filter])

  const handleClear = useCallback(() => {
    debugBus.clear()
    setEntries([])
    setExpandedIdx(new Set())
  }, [])

  const panel = (
    <div
      className="fixed flex flex-col overflow-hidden rounded-[14px]"
      style={{
        left: pos.x,
        top: pos.y,
        width: minimized ? 280 : size.w,
        height: minimized ? 'auto' : size.h,
        zIndex: 9999,
        background: 'var(--c-bg-page)',
        border: '0.5px solid var(--c-border-subtle)',
        boxShadow: '0 8px 24px rgba(0,0,0,.12), 0 2px 8px rgba(0,0,0,.08)',
        transition: minimized ? 'width 200ms, height 200ms' : undefined,
      }}
    >
      {/* title row */}
      <div
        className="flex items-center justify-between px-5 py-3 shrink-0 select-none"
        style={{
          borderBottom: '0.5px solid var(--c-border-subtle)',
          cursor: dragging.current ? 'grabbing' : 'grab',
        }}
        onMouseDown={handleDragStart}
      >
        <span className="text-[15px] font-semibold text-[var(--c-text-heading)]">Debug</span>
        <div className="flex items-center gap-1">
          <button
            onClick={() => setMinimized(!minimized)}
            className={windowBtnCls}
            title={minimized ? 'Expand' : 'Minimize'}
          >
            {minimized ? <Maximize2 size={14} /> : <Minimize2 size={14} />}
          </button>
          <button onClick={onClose} className={windowBtnCls} title="Close">
            <X size={14} />
          </button>
        </div>
      </div>

      {/* toolbar */}
      {!minimized && (
        <div
          className="flex items-center gap-2 px-5 py-2.5 shrink-0"
          style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
        >
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter..."
            className="h-8 flex-1 min-w-0 rounded-lg bg-[var(--c-bg-input)] px-3 text-xs text-[var(--c-text-primary)] outline-none transition-colors duration-150 placeholder:text-[var(--c-placeholder)] focus:border-[var(--c-border)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          />
          <ModeSwitch value={mode} onChange={setMode} />
          <button
            onClick={() => setPaused(!paused)}
            className={secondaryBtnCls}
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              ...(paused ? { color: 'var(--c-status-warn-text)' } : {}),
            }}
          >
            {paused ? <Play size={14} /> : <Pause size={14} />}
            {paused ? 'Resume' : 'Pause'}
          </button>
          <button
            onClick={handleClear}
            className={secondaryBtnCls}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <Trash2 size={14} />
            Clear
          </button>
        </div>
      )}

      {/* entries list */}
      {!minimized && (
        <>
          <div
            ref={listRef}
            className="flex-1 overflow-y-auto"
            onScroll={handleListScroll}
          >
            {filtered.length === 0 && (
              <div className="flex items-center justify-center h-full text-xs text-[var(--c-text-muted)]">
                No entries
              </div>
            )}
            {filtered.map((entry, i) => (
              <EntryRow
                key={i}
                entry={entry}
                prevTs={i > 0 ? filtered[i - 1].ts : null}
                mode={mode}
                expanded={expandedIdx.has(i)}
                onToggle={() => {
                  setExpandedIdx((prev) => {
                    const next = new Set(prev)
                    if (next.has(i)) next.delete(i)
                    else next.add(i)
                    return next
                  })
                }}
              />
            ))}
          </div>

          {/* status bar */}
          <div
            className="flex items-center justify-between px-5 py-1.5 text-xs shrink-0"
            style={{
              borderTop: '0.5px solid var(--c-border-subtle)',
              color: 'var(--c-text-muted)',
            }}
          >
            <span>
              {filtered.length} entries{filter ? ' (filtered)' : ''}
            </span>
            {paused && (
              <span
                className="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium"
                style={{ background: 'var(--c-status-warn-bg)', color: 'var(--c-status-warn-text)' }}
              >
                PAUSED
              </span>
            )}
          </div>

          {/* resize handle */}
          <div
            className="absolute right-0 bottom-0 cursor-se-resize"
            style={{ width: 16, height: 16 }}
            onMouseDown={handleResizeStart}
          >
            <svg width="16" height="16" viewBox="0 0 16 16" style={{ opacity: 0.3 }}>
              <path d="M14 2L2 14M14 7L7 14M14 12L12 14" stroke="var(--c-text-muted)" strokeWidth="1.5" />
            </svg>
          </div>
        </>
      )}
    </div>
  )

  return createPortal(panel, document.body)
}
