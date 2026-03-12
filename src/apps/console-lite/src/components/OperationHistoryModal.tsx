import { useState, useEffect, useRef, useCallback } from 'react'
import {
  X,
  Loader2,
  CheckCircle2,
  XCircle,
  Trash2,
} from 'lucide-react'
import {
  useOperations,
  type OperationRecord,
} from '../contexts/OperationContext'
import type { ModuleAction } from '../api/bridge'

type Props = {
  onClose: () => void
}

const ACTION_LABELS: Record<ModuleAction, string> = {
  install: 'Install',
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
  configure_connection: 'Configure',
  bootstrap_defaults: 'Bootstrap',
}

function relativeTime(ts: number): string {
  const diff = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (diff < 5) return 'just now'
  if (diff < 60) return `${diff}s ago`
  const mins = Math.floor(diff / 60)
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  return `${hrs}h ago`
}

function StatusIcon({ status }: { status: OperationRecord['status'] }) {
  switch (status) {
    case 'running':
      return <Loader2 size={13} className="shrink-0 animate-spin text-[var(--c-text-muted)]" />
    case 'completed':
      return <CheckCircle2 size={13} className="shrink-0 text-emerald-500" />
    case 'failed':
      return <XCircle size={13} className="shrink-0 text-red-500" />
  }
}

export function OperationHistoryModal({ onClose }: Props) {
  const { operations, activeCount, clearCompleted, cancelOperation } = useOperations()
  const [selectedId, setSelectedId] = useState<string | null>(
    () => operations[0]?.id ?? null,
  )
  const logEndRef = useRef<HTMLDivElement>(null)
  const [, setTick] = useState(0)

  const selected = operations.find((op) => op.id === selectedId) ?? null

  // Auto-scroll logs for running operations
  useEffect(() => {
    if (selected?.status === 'running') {
      logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [selected?.logs.length, selected?.status])

  // Refresh relative times every 10s
  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), 10_000)
    return () => clearInterval(id)
  }, [])

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [onClose])

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === e.currentTarget) onClose()
    },
    [onClose],
  )

  const completedCount = operations.length - activeCount

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-[2px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={handleOverlayClick}
    >
      <div
        className="modal-enter flex overflow-hidden rounded-2xl shadow-2xl bg-[var(--c-bg-page)]"
        style={{
          width: '720px',
          height: '480px',
          boxShadow: 'inset 0 0 0 0.5px var(--c-modal-ring)',
        }}
      >
        {/* Left sidebar */}
        <div
          className="flex w-[200px] shrink-0 flex-col bg-[var(--c-bg-sidebar)]"
          style={{ borderRight: '0.5px solid var(--c-border-console)' }}
        >
          <div className="flex items-center gap-2 px-4 py-4">
            <span className="text-sm font-semibold text-[var(--c-text-heading)]">
              Operations
            </span>
            {operations.length > 0 && (
              <span className="rounded-full bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
                {operations.length}
              </span>
            )}
          </div>

          <div className="flex-1 overflow-y-auto px-2">
            <div className="flex flex-col gap-[2px]">
              {operations.map((op) => (
                <button
                  key={op.id}
                  onClick={() => setSelectedId(op.id)}
                  className={[
                    'flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors',
                    selectedId === op.id
                      ? 'bg-[var(--c-bg-sub)]'
                      : 'hover:bg-[var(--c-bg-sub)]',
                  ].join(' ')}
                >
                  <div className="flex items-center gap-1.5">
                    <StatusIcon status={op.status} />
                    <span className="truncate text-xs font-medium text-[var(--c-text-primary)]">
                      {op.moduleName}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 pl-[19px]">
                    <span className="text-[10px] text-[var(--c-text-muted)]">
                      {ACTION_LABELS[op.action]}
                    </span>
                    <span className="text-[10px] text-[var(--c-text-muted)] opacity-60">
                      · {relativeTime(op.startedAt)}
                    </span>
                  </div>
                </button>
              ))}
              {operations.length === 0 && (
                <p className="px-2 py-4 text-center text-xs text-[var(--c-text-muted)]">
                  No operations yet
                </p>
              )}
            </div>
          </div>

          {completedCount > 0 && (
            <div
              className="px-3 py-3"
              style={{ borderTop: '0.5px solid var(--c-border-console)' }}
            >
              <button
                onClick={clearCompleted}
                className="flex w-full items-center justify-center gap-1.5 rounded-md px-2 py-1.5 text-[11px] text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
              >
                <Trash2 size={11} />
                Clear completed
              </button>
            </div>
          )}
        </div>

        {/* Right content */}
        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="flex items-center gap-2">
              {selected ? (
                <>
                  <h2 className="text-sm font-medium text-[var(--c-text-heading)]">
                    {ACTION_LABELS[selected.action]} {selected.moduleName}
                  </h2>
                  <StatusBadge status={selected.status} error={selected.error} />
                </>
              ) : (
                <h2 className="text-sm font-medium text-[var(--c-text-heading)]">
                  Operations
                </h2>
              )}
            </div>
            <div className="flex items-center gap-2">
              {selected?.status === 'running' && (
                <button
                  onClick={() => void cancelOperation(selected.id)}
                  className="rounded-md bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-500 transition-colors hover:bg-red-500/20"
                >
                  Force Stop
                </button>
              )}
              <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <X size={15} />
              </button>
            </div>
          </div>

          <div className="flex-1 overflow-hidden p-4">
            {selected ? (
              <div
                className="h-full overflow-y-auto rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-page)] p-3"
              >
                {selected.logs.length === 0 && selected.status === 'running' && (
                  <span className="text-xs text-[var(--c-text-muted)]">
                    Waiting for output…
                  </span>
                )}
                {selected.logs.map((line, i) => (
                  <div
                    key={i}
                    className="font-mono text-xs leading-5 text-[var(--c-text-muted)]"
                  >
                    {line}
                  </div>
                ))}
                <div ref={logEndRef} />
              </div>
            ) : (
              <div className="flex h-full items-center justify-center">
                <p className="text-xs text-[var(--c-text-muted)]">
                  Select an operation to view logs
                </p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function StatusBadge({
  status,
  error,
}: {
  status: OperationRecord['status']
  error?: string
}) {
  switch (status) {
    case 'running':
      return (
        <span className="flex items-center gap-1 rounded-full bg-blue-500/10 px-2 py-0.5 text-[10px] font-medium text-blue-500">
          <Loader2 size={10} className="animate-spin" />
          Running
        </span>
      )
    case 'completed':
      return (
        <span className="flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-500">
          <CheckCircle2 size={10} />
          Completed
        </span>
      )
    case 'failed':
      return (
        <span
          className="flex items-center gap-1 rounded-full bg-red-500/10 px-2 py-0.5 text-[10px] font-medium text-red-500"
          title={error}
        >
          <XCircle size={10} />
          Failed
        </span>
      )
  }
}
