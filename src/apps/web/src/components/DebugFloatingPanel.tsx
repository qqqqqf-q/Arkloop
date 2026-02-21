import { useState } from 'react'
import { Bug, X } from 'lucide-react'
import { LlmDebugPanel } from './LlmDebugPanel'
import { RunEventsPanel } from './RunEventsPanel'
import type { RunEvent, SSEClientState } from '../sse'

type Props = {
  events: RunEvent[]
  state: SSEClientState
  lastSeq: number
  error: Error | null
  activeRunId: string | null
  onReconnect: () => void
  onClear: () => void
}

export function DebugFloatingPanel({
  events,
  state,
  lastSeq,
  error,
  activeRunId,
  onReconnect,
  onClear,
}: Props) {
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<'events' | 'debug'>('events')

  if (events.length === 0 && !activeRunId) return null

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col items-end gap-2">
      {open && (
        <div className="w-[480px] max-h-[60vh] overflow-hidden rounded-2xl border border-[var(--c-border)] bg-[var(--c-bg-input)] shadow-2xl flex flex-col">
          <div className="flex items-center justify-between border-b border-[var(--c-border)] px-4 py-3">
            <div className="flex gap-1">
              <button
                onClick={() => setTab('events')}
                className={[
                  'rounded-lg px-3 py-1.5 text-xs font-medium transition-colors',
                  tab === 'events'
                    ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:text-[var(--c-text-secondary)]',
                ].join(' ')}
              >
                事件
              </button>
              <button
                onClick={() => setTab('debug')}
                className={[
                  'rounded-lg px-3 py-1.5 text-xs font-medium transition-colors',
                  tab === 'debug'
                    ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:text-[var(--c-text-secondary)]',
                ].join(' ')}
              >
                LLM 调试
              </button>
            </div>
            <button
              onClick={() => setOpen(false)}
              className="text-[var(--c-text-tertiary)] transition-opacity hover:opacity-70"
            >
              <X size={16} />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {tab === 'events' ? (
              <RunEventsPanel
                events={events}
                state={state}
                lastSeq={lastSeq}
                error={error}
                allowReconnect={activeRunId != null}
                onReconnect={onReconnect}
                onClear={onClear}
              />
            ) : (
              <LlmDebugPanel events={events} onClear={onClear} />
            )}
          </div>
        </div>
      )}

      <button
        onClick={() => setOpen((v) => !v)}
        className="flex h-10 w-10 items-center justify-center rounded-full border border-[var(--c-border)] bg-[var(--c-bg-input)] text-[var(--c-text-tertiary)] shadow-lg transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
        title="调试面板"
      >
        <Bug size={18} />
      </button>
    </div>
  )
}
