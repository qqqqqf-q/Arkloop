import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useLocation } from 'react-router-dom'
import type { AdminRunDetail, GlobalRun, RunEventRaw } from '../api/runs'
import { fetchRunEventsOnce, getRunDetail } from '../api/runs'
import { TurnView } from './TurnView'
import {
  buildRequestThreadTurns,
  buildTurns,
  formatDateTime,
  jsonStringifyForDebugDisplay,
  pickLogicalToolName,
  type LlmTurn,
  type RequestThreadTurn,
} from '@arkloop/shared'
import { Badge, type BadgeVariant } from './Badge'
import { useLocale } from '../contexts/LocaleContext'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../api'

type Props = {
  run: GlobalRun | null
  agentName?: string
  accessToken: string
  onClose: () => void
}

type TabKey = 'thread' | 'execution' | 'events' | 'overview'

function statusVariant(status: string): BadgeVariant {
  switch (status) {
    case 'running':
      return 'warning'
    case 'completed':
      return 'success'
    case 'failed':
      return 'error'
    default:
      return 'neutral'
  }
}

function MetaRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start gap-2.5 py-1">
      <span className="w-20 shrink-0 text-[11px] text-[var(--c-text-muted)]">{label}</span>
      <span className={['text-xs text-[var(--c-text-secondary)]', mono ? 'font-mono break-all' : 'break-words'].join(' ')}>
        {value}
      </span>
    </div>
  )
}

function formatAbsoluteTime(value: string | undefined, fallback: string): string {
  if (!value) return fallback
  return formatDateTime(value, { includeSeconds: true })
}

function formatEventJSON(event: RunEventRaw): string {
  const toolName = pickLogicalToolName(event.data, event.tool_name)
  return jsonStringifyForDebugDisplay(
    {
      event_id: event.event_id,
      run_id: event.run_id,
      seq: event.seq,
      ts: event.ts,
      type: event.type,
      tool_name: toolName || event.tool_name,
      error_class: event.error_class,
      data: event.data,
    },
    2,
  )
}

function displayToolName(event: RunEventRaw): string {
  return pickLogicalToolName(event.data, event.tool_name)
}


function ThreadTurnCard({ turn, index }: { turn: RequestThreadTurn; index: number }) {
  return (
    <div
      className={[
        'space-y-2 rounded-lg border bg-[var(--c-bg-deep)] p-3',
        turn.isCurrent ? 'border-[var(--c-accent)] shadow-[inset_0_0_0_1px_var(--c-accent)]' : 'border-[var(--c-border)]',
      ].join(' ')}
    >
      <div className="flex items-center gap-2 text-xs text-[var(--c-text-muted)]">
        <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono font-medium text-[var(--c-text-secondary)]">
          Thread {index + 1}
        </span>
        {turn.isCurrent && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] text-[var(--c-text-secondary)]">
            current run
          </span>
        )}
      </div>
      <div className="space-y-2">
        {turn.messages.map((message, messageIndex) => (
          <div key={`${turn.key}-${messageIndex}`} className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] p-3">
            <div className="mb-1 text-[11px] font-medium uppercase text-[var(--c-text-secondary)]">{message.role}</div>
            <pre className="whitespace-pre-wrap break-words text-[11px] text-[var(--c-text-secondary)]">{message.text || '∅'}</pre>
          </div>
        ))}
      </div>
      <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] p-3">
        <div className="mb-1 text-[11px] font-medium text-[var(--c-text-secondary)]">Assistant</div>
        <pre className="whitespace-pre-wrap break-words text-[11px] text-[var(--c-text-secondary)]">{turn.assistantText || 'Assistant output unavailable'}</pre>
      </div>
    </div>
  )
}

function formatCount(value: number | undefined, fallback: string): string {
  return value == null ? fallback : value.toLocaleString()
}

function formatPercent(value: number | undefined, fallback: string): string {
  if (value == null) return fallback
  return `${(value * 100).toFixed(0)}%`
}

function formatTokenPair(input: number | undefined, output: number | undefined, fallback: string): string {
  if (input == null && output == null) return fallback
  return `${input ?? 0} / ${output ?? 0}`
}

function statusText(status: string, t: ReturnType<typeof useLocale>['t']['runs']): string {
  switch (status) {
    case 'running':
      return t.statusRunning
    case 'completed':
      return t.statusCompleted
    case 'failed':
      return t.statusFailed
    case 'cancelled':
      return t.statusCancelled
    default:
      return t.statusUnknown
  }
}

export function RunDetailPanel({ run, agentName, accessToken, onClose }: Props) {
  const { locale, t } = useLocale()
  const location = useLocation()
  const { addToast } = useToast()
  const [activeTab, setActiveTab] = useState<TabKey>('thread')
  const [detail, setDetail] = useState<AdminRunDetail | null>(null)
  const [events, setEvents] = useState<RunEventRaw[] | null>(null)
  const [loading, setLoading] = useState(false)
  const mountedRef = useRef(false)
  const requestRunIdRef = useRef<string | null>(null)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
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
    if (!run) return
    requestRunIdRef.current = run.run_id
    setLoading(true)
    setDetail(null)
    setEvents(null)

    const [detailResult, eventsResult] = await Promise.allSettled([
      getRunDetail(run.run_id, accessToken),
      fetchRunEventsOnce(run.run_id, accessToken),
    ])

    if (!mountedRef.current || requestRunIdRef.current !== run.run_id) return

    if (detailResult.status === 'fulfilled') {
      setDetail(detailResult.value)
    }
    if (eventsResult.status === 'fulfilled') {
      setEvents(eventsResult.value)
    }

    if (detailResult.status === 'rejected' || eventsResult.status === 'rejected') {
      const reason = detailResult.status === 'rejected'
        ? detailResult.reason
        : eventsResult.status === 'rejected'
          ? eventsResult.reason
          : null
      addToast(
        reason && isApiError(reason) ? reason.message : t.runs.toastDetailLoadFailed,
        'error',
      )
    }

    setLoading(false)
  }, [accessToken, addToast, run, t.runs.toastDetailLoadFailed])

  useEffect(() => {
    if (!run) {
      requestRunIdRef.current = null
      return undefined
    }
    const frame = window.requestAnimationFrame(() => {
      void load()
    })
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      window.cancelAnimationFrame(frame)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [load, onClose, run])

  const fallback = t.runs.emptyValue
  const status = detail?.status ?? run?.status ?? 'unknown'
  const turns: LlmTurn[] = useMemo(
    () => buildTurns((events ?? []).map((event) => ({
      ...event,
      tool_name: displayToolName(event) || event.tool_name,
    }))),
    [events],
  )
  const threadTurns = useMemo(() => buildRequestThreadTurns(turns), [turns])
  const executionTurns = useMemo(() => turns, [turns])
  const threadLabel = locale.startsWith('zh') ? '对话线程' : 'Thread'
  const executionLabel = locale.startsWith('zh') ? '本轮执行' : 'Execution'
  const overview = useMemo(() => {
    if (!run) return null
    return {
      runId: detail?.run_id ?? run.run_id,
      agent: agentName ?? detail?.persona_id ?? run.persona_id ?? fallback,
      user: detail?.created_by_email ?? run.created_by_email ?? fallback,
      model: detail?.model ?? run.model ?? fallback,
      tokens: formatTokenPair(detail?.total_input_tokens ?? run.total_input_tokens, detail?.total_output_tokens ?? run.total_output_tokens, fallback),
      cacheHit: formatPercent(detail?.cache_hit_rate ?? run.cache_hit_rate, fallback),
      credits: formatCount(detail?.credits_used ?? run.credits_used, fallback),
      created: formatAbsoluteTime(detail?.created_at ?? run.created_at, fallback),
      completed: formatAbsoluteTime(detail?.completed_at ?? run.completed_at, fallback),
      failedAt: formatAbsoluteTime(detail?.failed_at ?? run.failed_at, fallback),
      threadId: detail?.thread_id ?? run.thread_id,
    }
  }, [agentName, detail, fallback, run])

  if (!run || !overview) return null

  return createPortal(
    <div className="fixed inset-0 z-50 bg-black/30" onClick={onClose}>
      <aside
        className="fixed inset-y-0 right-0 z-50 flex w-[500px] max-w-full flex-col border-l border-[var(--c-border)] bg-[var(--c-bg-deep2)] shadow-2xl"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex min-h-[46px] items-center justify-between border-b border-[var(--c-border-console)] px-4">
          <div className="min-w-0">
            <h3 className="truncate text-xs font-medium text-[var(--c-text-primary)]">{t.runs.detailTitle}</h3>
            <p className="mt-0.5 truncate font-mono text-[11px] text-[var(--c-text-muted)]">{overview.runId}</p>
          </div>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            title={t.common.cancel}
          >
            <X size={14} />
          </button>
        </div>

        <div className="flex border-b border-[var(--c-border-console)] px-4">
          {[
            { key: 'thread', label: threadLabel, count: threadTurns.length },
            { key: 'execution', label: executionLabel, count: executionTurns.length },
            { key: 'events', label: t.runs.sectionEvents, count: events?.length ?? 0 },
            { key: 'overview', label: t.runs.sectionOverview, count: 0 },
          ].map((tab) => (
            <button
              key={tab.key}
              type="button"
              onClick={() => setActiveTab(tab.key as TabKey)}
              className={[
                'mr-4 border-b-2 py-2.5 text-xs font-medium transition-colors',
                activeTab === tab.key
                  ? 'border-[var(--c-accent)] text-[var(--c-text-primary)]'
                  : 'border-transparent text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]',
              ].join(' ')}
            >
              {tab.label}
              {tab.count > 0 && (
                <span className="ml-1.5 rounded bg-[var(--c-bg-sub)] px-1 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                  {tab.count}
                </span>
              )}
            </button>
          ))}
        </div>

        <div className="flex-1 space-y-4 overflow-y-auto p-4">
          {activeTab === 'overview' && (
          <section>
            <h4 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--c-text-muted)]">
              {t.runs.sectionOverview}
            </h4>
            <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3">
              <div className="mb-2 flex items-center gap-2">
                <Badge variant={statusVariant(status)}>{statusText(status, t.runs)}</Badge>
              </div>
              <MetaRow label={t.runs.labelRunId} value={overview.runId} mono />
              <MetaRow label={t.runs.labelAgent} value={overview.agent} />
              <MetaRow label={t.runs.labelUser} value={overview.user} />
              <MetaRow label={t.runs.labelModel} value={overview.model} />
              <MetaRow label={t.runs.labelTokens} value={overview.tokens} />
              <MetaRow label={t.runs.labelCacheHit} value={overview.cacheHit} />
              <MetaRow label={t.runs.labelCredits} value={overview.credits} />
              <MetaRow label={t.runs.labelCreated} value={overview.created} />
              <MetaRow label={t.runs.labelCompleted} value={overview.completed} />
              <MetaRow label={t.runs.labelFailedAt} value={overview.failedAt} />
              <MetaRow label={t.runs.labelThread} value={overview.threadId} mono />
            </div>
          </section>
          )}

          {activeTab === 'thread' && (
          <section>
            <h4 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--c-text-muted)]">
              {threadLabel}
            </h4>
            {loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.loading}
              </div>
            ) : threadTurns.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.noConversation}
              </div>
            ) : (
              <div className="space-y-2.5">
                {threadTurns.map((turn, index) => (
                  <ThreadTurnCard key={turn.key} turn={turn} index={index} />
                ))}
              </div>
            )}
          </section>
          )}

          {activeTab === 'execution' && (
          <section>
            <h4 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--c-text-muted)]">
              {executionLabel}
            </h4>
            {loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.loading}
              </div>
            ) : executionTurns.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.noConversation}
              </div>
            ) : (
              <div className="space-y-2.5">
                {executionTurns.map((turn, index) => (
                  <TurnView key={turn.llmCallId || index} turn={turn} index={index} />
                ))}
              </div>
            )}
          </section>
          )}

          {activeTab === 'events' && (
          <section>
            <h4 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--c-text-muted)]">
              {t.runs.sectionEvents}
            </h4>
            {!loading && events && events.length > 0 && (
              <p className="mb-2 text-[11px] text-[var(--c-text-muted)]">
                Events are listed in sequence. Timestamps are when events were stored.
              </p>
            )}
            {loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.loading}
              </div>
            ) : !events || events.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.noEvents}
              </div>
            ) : (
              <div className="space-y-2.5">
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
                        {displayToolName(event) && (
                          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                            {displayToolName(event)}
                          </span>
                        )}
                        {event.error_class && (
                          <span className="rounded bg-[var(--c-status-danger-bg)] px-1.5 py-0.5 text-[10px] text-[var(--c-status-danger-text)]">
                            {event.error_class}
                          </span>
                        )}
                        <span className="ml-auto text-[10px] tabular-nums text-[var(--c-text-muted)]">
                          {formatAbsoluteTime(event.ts, fallback)}
                        </span>
                      </div>
                    </summary>
                    <div className="border-t border-[var(--c-border)] px-3 py-2">
                      <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded bg-[var(--c-bg-deep2)] p-2 text-[11px] text-[var(--c-text-secondary)]">
                        {formatEventJSON(event)}
                      </pre>
                    </div>
                  </details>
                ))}
              </div>
            )}
          </section>
          )}
        </div>
      </aside>
    </div>,
    document.body,
  )
}
