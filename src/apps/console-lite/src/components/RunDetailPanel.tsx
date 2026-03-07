import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import type { AdminRunDetail, GlobalRun } from '../api/runs'
import { fetchRunEventsOnce, getRunDetail } from '../api/runs'
import { TurnView } from './TurnView'
import { buildTurns, type LlmTurn } from '../run-turns'
import { Badge, type BadgeVariant } from './Badge'
import { useLocale } from '../contexts/LocaleContext'
import { useToast } from './useToast'
import { isApiError } from '../api'

type Props = {
  run: GlobalRun | null
  agentName?: string
  accessToken: string
  onClose: () => void
}

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

function formatAbsoluteTime(value: string | undefined, locale: 'zh' | 'en', fallback: string): string {
  if (!value) return fallback
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(locale === 'zh' ? 'zh-CN' : 'en')
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
  const { addToast } = useToast()
  const [detail, setDetail] = useState<AdminRunDetail | null>(null)
  const [turns, setTurns] = useState<LlmTurn[]>([])
  const [loading, setLoading] = useState(false)
  const mountedRef = useRef(false)
  const requestRunIdRef = useRef<string | null>(null)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const load = useCallback(async () => {
    if (!run) return
    requestRunIdRef.current = run.run_id
    setLoading(true)
    setDetail(null)
    setTurns([])

    const [detailResult, eventsResult] = await Promise.allSettled([
      getRunDetail(run.run_id, accessToken),
      fetchRunEventsOnce(run.run_id, accessToken),
    ])

    if (!mountedRef.current || requestRunIdRef.current !== run.run_id) return

    if (detailResult.status === 'fulfilled') {
      setDetail(detailResult.value)
    }
    if (eventsResult.status === 'fulfilled') {
      setTurns(buildTurns(eventsResult.value))
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
      created: formatAbsoluteTime(detail?.created_at ?? run.created_at, locale, fallback),
      completed: formatAbsoluteTime(detail?.completed_at ?? run.completed_at, locale, fallback),
      failedAt: formatAbsoluteTime(detail?.failed_at ?? run.failed_at, locale, fallback),
      threadId: detail?.thread_id ?? run.thread_id,
    }
  }, [agentName, detail, fallback, locale, run])

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

        <div className="flex-1 space-y-4 overflow-y-auto p-4">
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

          <section>
            <h4 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--c-text-muted)]">
              {t.runs.sectionConversation}
            </h4>
            {loading ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.loading}
              </div>
            ) : turns.length === 0 ? (
              <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-text-muted)]">
                {t.runs.noConversation}
              </div>
            ) : (
              <div className="space-y-2.5">
                {turns.map((turn, index) => (
                  <TurnView key={turn.llmCallId || index} turn={turn} index={index} />
                ))}
              </div>
            )}
          </section>
        </div>
      </aside>
    </div>,
    document.body,
  )
}
