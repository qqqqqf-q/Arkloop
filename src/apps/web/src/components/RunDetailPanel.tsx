import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useLocation } from 'react-router-dom'
import { TurnView, buildThreadTurns, buildTurns } from '@arkloop/shared'
import type { LlmTurn, RunEventRaw, ThreadTurn } from '@arkloop/shared'
import type { RunDetail, RunEvent } from '../api'
import { getRunDetail, listRunEvents } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  runId: string
  accessToken: string
  onClose: () => void
}

type TabKey = 'overview' | 'thread' | 'execution' | 'events'

function statusColor(status: string): string {
  switch (status) {
    case 'running': return 'var(--c-status-warning-text)'
    case 'completed': return 'var(--c-status-success-text)'
    case 'failed': return 'var(--c-status-error-text)'
    default: return 'var(--c-text-muted)'
  }
}

function MetaRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start gap-3 py-1">
      <span className="w-24 shrink-0 text-[11px] text-[var(--c-text-muted)]">{label}</span>
      <span className={['text-[11px] text-[var(--c-text-secondary)]', mono ? 'font-mono break-all' : 'break-words'].join(' ')}>
        {value}
      </span>
    </div>
  )
}

function formatTime(value: string | undefined): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    fractionalSecondDigits: 3,
  }).format(date)
}

function formatEventJSON(event: RunEvent): string {
  return JSON.stringify({
    event_id: event.event_id,
    run_id: event.run_id,
    seq: event.seq,
    ts: event.ts,
    type: event.type,
    tool_name: event.tool_name,
    error_class: event.error_class,
    data: event.data,
  }, null, 2)
}

function ThreadTurnCard({ turn, index }: { turn: ThreadTurn; index: number }) {
  return (
    <div
      className={[
        'space-y-2 rounded-lg border bg-[var(--c-bg-deep)] p-3',
        turn.isCurrent ? 'border-[var(--c-accent)] shadow-[inset_0_0_0_1px_var(--c-accent)]' : 'border-[var(--c-border)]',
      ].join(' ')}
    >
      <div className="flex items-center gap-2 text-xs text-[var(--c-text-muted)]">
        <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono font-medium text-[var(--c-text-secondary)]">
          Turn {index + 1}
        </span>
        {turn.isCurrent && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] text-[var(--c-text-secondary)]">
            current run
          </span>
        )}
      </div>

      <div className="rounded border border-[var(--c-border)] overflow-hidden">
        <div className="px-2.5 py-1.5 text-[11px] font-medium text-[var(--c-text-secondary)]">Input</div>
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-2">
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">
            {turn.userText || 'Input unavailable'}
          </pre>
        </div>
      </div>

      <div className="rounded border border-[var(--c-border)] overflow-hidden">
        <div className="px-2.5 py-1.5 text-[11px] font-medium text-[var(--c-text-secondary)]">Assistant</div>
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-2">
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">
            {turn.assistantText || 'Assistant output unavailable'}
          </pre>
        </div>
      </div>
    </div>
  )
}

export function RunDetailPanel({ runId, accessToken, onClose }: Props) {
  const { t, locale } = useLocale()
  const location = useLocation()
  const ds = t.desktopSettings
  const [activeTab, setActiveTab] = useState<TabKey>('thread')
  const [detail, setDetail] = useState<RunDetail | null>(null)
  const [events, setEvents] = useState<RunEvent[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const mountedRef = useRef(false)
  const requestIdRef = useRef<string | null>(null)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const locationRef = useRef<string | null>(null)

  useEffect(() => {
    const signature = `${location.pathname}?${location.search}#${location.hash}`
    if (locationRef.current == null) {
      locationRef.current = signature
      return
    }
    if (locationRef.current !== signature) {
      locationRef.current = signature
      onClose()
    }
  }, [location.hash, location.pathname, location.search, onClose])

  const load = useCallback(async () => {
    requestIdRef.current = runId
    setLoading(true)
    setDetail(null)
    setEvents(null)
    setLoadError(false)

    const [detailResult, eventsResult] = await Promise.allSettled([
      getRunDetail(accessToken, runId),
      listRunEvents(accessToken, runId, { follow: false }),
    ])

    if (!mountedRef.current || requestIdRef.current !== runId) return

    if (detailResult.status === 'fulfilled') setDetail(detailResult.value)
    if (eventsResult.status === 'fulfilled') setEvents(eventsResult.value)

    if (detailResult.status === 'rejected' && eventsResult.status === 'rejected') {
      setLoadError(true)
    }

    setLoading(false)
  }, [accessToken, runId])

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => { void load() })
    const handleKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', handleKey)
    return () => {
      window.cancelAnimationFrame(frame)
      document.removeEventListener('keydown', handleKey)
    }
  }, [load, onClose])

  const turns: LlmTurn[] = useMemo(
    () => buildTurns((events ?? []) as unknown as RunEventRaw[]),
    [events],
  )
  const threadTurns = useMemo(
    () => buildThreadTurns(detail?.thread_messages ?? [], runId, detail?.user_prompt),
    [detail?.thread_messages, detail?.user_prompt, runId],
  )
  const executionTurns = useMemo(() => {
    return turns
  }, [turns])
  const executionLabel = locale.startsWith('zh') ? '本轮执行' : 'Execution'
  const threadLabel = locale.startsWith('zh') ? '对话线程' : 'Thread'

  const tabs: { key: TabKey; label: string }[] = [
    { key: 'thread', label: threadLabel },
    { key: 'execution', label: executionLabel },
    { key: 'events', label: ds.runDetailEvents },
    { key: 'overview', label: ds.runDetailOverview },
  ]

  return createPortal(
    <aside
      className="fixed inset-y-0 right-0 z-50 flex w-[520px] max-w-full flex-col border-l border-[var(--c-border)]"
      style={{ background: 'var(--c-bg-menu, #1a1a1a)', WebkitAppRegion: 'no-drag' } as React.CSSProperties}
    >
        {/* Header */}
        <div className="flex min-h-[46px] items-center justify-between border-b border-[var(--c-border)] px-4">
          <div className="min-w-0">
            <h3 className="truncate text-xs font-semibold text-[var(--c-text-primary)]">
              {ds.runDetailTitle}
            </h3>
            <p className="mt-0.5 truncate font-mono text-[10px] text-[var(--c-text-muted)]">
              {runId}
            </p>
          </div>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <X size={14} />
          </button>
        </div>

        {/* Tabs */}
        <div className="flex border-b border-[var(--c-border)] px-4">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              type="button"
              onClick={() => setActiveTab(tab.key)}
              className={[
                'mr-4 border-b-2 py-2.5 text-xs font-medium transition-colors',
                activeTab === tab.key
                  ? 'border-[var(--c-accent)] text-[var(--c-text-primary)]'
                  : 'border-transparent text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]',
              ].join(' ')}
            >
              {tab.label}
              {tab.key === 'events' && events != null && (
                <span className="ml-1.5 rounded bg-[var(--c-bg-sub)] px-1 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                  {events.length}
                </span>
              )}
              {tab.key === 'thread' && threadTurns.length > 0 && (
                <span className="ml-1.5 rounded bg-[var(--c-bg-sub)] px-1 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                  {threadTurns.length}
                </span>
              )}
              {tab.key === 'execution' && executionTurns.length > 0 && (
                <span className="ml-1.5 rounded bg-[var(--c-bg-sub)] px-1 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                  {executionTurns.length}
                </span>
              )}
            </button>
          ))}
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4">
          {loadError && (
            <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-status-error-text)]">
              {ds.runDetailLoadError}
            </div>
          )}

          {!loadError && activeTab === 'overview' && (
            <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3">
              {loading ? (
                <p className="text-xs text-[var(--c-text-muted)]">Loading…</p>
              ) : (
                <>
                  {detail && (
                    <div className="mb-2">
                      <span
                        className="rounded px-2 py-0.5 text-[11px] font-medium"
                        style={{
                          background: 'var(--c-bg-sub)',
                          color: statusColor(detail.status),
                        }}
                      >
                        {detail.status}
                      </span>
                    </div>
                  )}
                  <MetaRow label="Run ID" value={runId} mono />
                  {detail?.model && <MetaRow label="Model" value={detail.model} />}
                  {detail?.persona_id && <MetaRow label="Persona" value={detail.persona_id} />}
                  {detail?.created_by_user_name && <MetaRow label="User" value={detail.created_by_user_name} />}
                  {detail?.created_by_email && <MetaRow label="Email" value={detail.created_by_email} />}
                  {(detail?.total_input_tokens != null || detail?.total_output_tokens != null) && (
                    <MetaRow
                      label="Tokens"
                      value={`${detail.total_input_tokens ?? 0} in / ${detail.total_output_tokens ?? 0} out`}
                    />
                  )}
                  {detail?.total_cost_usd != null && (
                    <MetaRow label="Cost" value={`$${detail.total_cost_usd.toFixed(6)}`} />
                  )}
                  {detail?.cache_hit_rate != null && (
                    <MetaRow label="Cache" value={`${(detail.cache_hit_rate * 100).toFixed(0)}%`} />
                  )}
                  {detail?.duration_ms != null && (
                    <MetaRow label="Duration" value={`${(detail.duration_ms / 1000).toFixed(2)}s`} />
                  )}
                  {detail?.created_at && <MetaRow label="Created" value={formatTime(detail.created_at)} />}
                  {detail?.completed_at && <MetaRow label="Completed" value={formatTime(detail.completed_at)} />}
                  {detail?.failed_at && <MetaRow label="Failed at" value={formatTime(detail.failed_at)} />}
                  {!detail && !loading && (
                    <p className="text-xs text-[var(--c-text-muted)]">
                      Overview unavailable (admin access required).
                    </p>
                  )}
                </>
              )}
            </div>
          )}

          {!loadError && activeTab === 'thread' && (
            loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                Loading…
              </div>
            ) : threadTurns.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                No thread turns.
              </div>
            ) : (
              <div className="space-y-2.5">
                {threadTurns.map((turn, index) => (
                  <ThreadTurnCard key={turn.key} turn={turn} index={index} />
                ))}
              </div>
            )
          )}

          {!loadError && activeTab === 'execution' && (
            loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                Loading…
              </div>
            ) : executionTurns.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                No execution turns.
              </div>
            ) : (
              <div className="space-y-2.5">
                {executionTurns.map((turn, index) => (
                  <TurnView key={turn.llmCallId || index} turn={turn} index={index} />
                ))}
              </div>
            )
          )}

          {!loadError && activeTab === 'events' && (
            loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                Loading…
              </div>
            ) : !events || events.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {ds.runDetailNoEvents}
              </div>
            ) : (
              <>
                <p className="mb-2 text-[11px] text-[var(--c-text-muted)]">
                  Events are shown in sequence order. Recorded time may differ from when the model started emitting output.
                </p>
                <div className="space-y-1.5">
                  {events.map((event) => (
                    <details
                      key={event.event_id}
                      className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)]"
                    >
                      <summary className="cursor-pointer list-none px-3 py-2">
                        <div className="flex flex-wrap items-center gap-2 text-xs">
                          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--c-text-muted)]">
                            #{event.seq}
                          </span>
                          <span className="font-mono text-[var(--c-text-secondary)]">{event.type}</span>
                          {event.tool_name && (
                            <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                              {event.tool_name}
                            </span>
                          )}
                          {event.error_class && (
                            <span
                              className="rounded px-1.5 py-0.5 text-[10px]"
                              style={{ background: 'var(--c-status-danger-bg)', color: 'var(--c-status-danger-text)' }}
                            >
                              {event.error_class}
                            </span>
                          )}
                          <span className="ml-auto text-[10px] text-[var(--c-text-muted)]">
                            recorded {formatTime(event.ts)}
                          </span>
                        </div>
                      </summary>
                      <div className="border-t border-[var(--c-border)] px-3 py-2">
                        <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded bg-[var(--c-bg-deep2)] p-2 font-mono text-[11px] text-[var(--c-text-secondary)]">
                          {formatEventJSON(event)}
                        </pre>
                      </div>
                    </details>
                  ))}
                </div>
              </>
            )
          )}
        </div>
    </aside>,
    document.body,
  )
}
