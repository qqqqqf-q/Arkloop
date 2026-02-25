import { useState, useEffect, useCallback, useRef } from 'react'
import { createPortal } from 'react-dom'
import { X, ChevronDown, ChevronRight } from 'lucide-react'
import type { GlobalRun, AdminRunDetail, RunEventRaw } from '../api/runs'
import { getAdminRunDetail, fetchRunEventsOnce } from '../api/runs'
import { TurnView, buildTurns } from './TurnView'
import { Badge, type BadgeVariant } from './Badge'

function statusVariant(status: string): BadgeVariant {
  switch (status) {
    case 'running': return 'warning'
    case 'completed': return 'success'
    case 'failed': return 'error'
    default: return 'neutral'
  }
}

function formatDuration(ms?: number): string {
  if (ms == null) return '--'
  const secs = Math.floor(ms / 1000)
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  return `${mins}m ${secs % 60}s`
}

function formatCost(usd?: number): string {
  if (usd == null) return '--'
  return `$${usd.toFixed(4)}`
}

type MetaRowProps = {
  label: string
  value: string | undefined | null
  mono?: boolean
}

function MetaRow({ label, value, mono }: MetaRowProps) {
  if (!value) return null
  return (
    <div className="flex items-baseline gap-3 py-1">
      <span className="w-28 shrink-0 text-xs text-[var(--c-text-muted)]">{label}</span>
      <span className={['text-xs text-[var(--c-text-secondary)]', mono ? 'font-mono' : ''].join(' ')}>
        {value}
      </span>
    </div>
  )
}

type SectionProps = {
  title: string
  badge?: string
  defaultOpen?: boolean
  onOpen?: () => void
  children: React.ReactNode
}

function Section({ title, badge, defaultOpen = true, onOpen, children }: SectionProps) {
  const [open, setOpen] = useState(defaultOpen)
  const triggered = useRef(false)

  const handleToggle = useCallback(() => {
    setOpen((v) => {
      const next = !v
      if (next && !triggered.current && onOpen) {
        triggered.current = true
        onOpen()
      }
      return next
    })
  }, [onOpen])

  return (
    <div className="border-t border-[var(--c-border-console)]">
      <button
        onClick={handleToggle}
        className="flex w-full items-center gap-2 px-4 py-3 text-left transition-colors hover:bg-[var(--c-bg-sub)]"
      >
        <span className="text-[var(--c-text-muted)]">
          {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
        </span>
        <span className="text-xs font-semibold uppercase tracking-wider text-[var(--c-text-muted)]">
          {title}
        </span>
        {badge && (
          <span className="ml-1 text-xs text-[var(--c-text-muted)]">{badge}</span>
        )}
      </button>
      {open && <div className="px-4 pb-4">{children}</div>}
    </div>
  )
}

type Props = {
  run: GlobalRun | null
  accessToken: string
  onClose: () => void
}

export function RunDetailPanel({ run, accessToken, onClose }: Props) {
  const [detail, setDetail] = useState<AdminRunDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [events, setEvents] = useState<RunEventRaw[] | null>(null)
  const [eventsLoading, setEventsLoading] = useState(false)

  // 面板打开时加载 summary
  useEffect(() => {
    if (!run) {
      setDetail(null)
      setEvents(null)
      return
    }
    setDetail(null)
    setEvents(null)
    setDetailLoading(true)
    getAdminRunDetail(run.run_id, accessToken)
      .then(setDetail)
      .catch(() => {/* 静默，面板仍可展示列表数据 */})
      .finally(() => setDetailLoading(false))
  }, [run, accessToken])

  // Conversation 展开时懒加载事件流
  const loadEvents = useCallback(() => {
    if (!run || events !== null) return
    setEventsLoading(true)
    fetchRunEventsOnce(run.run_id, accessToken)
      .then(setEvents)
      .catch(() => setEvents([]))
      .finally(() => setEventsLoading(false))
  }, [run, events, accessToken])

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() },
    [onClose],
  )
  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  if (!run) return null

  const d = detail
  const turns = events ? buildTurns(events) : []

  const conversationBadge = d
    ? `${d.events_stats.llm_turns} turn${d.events_stats.llm_turns !== 1 ? 's' : ''}` +
      (d.events_stats.tool_calls > 0 ? ` · ${d.events_stats.tool_calls} tool calls` : '') +
      (d.events_stats.provider_fallbacks > 0 ? ` · ${d.events_stats.provider_fallbacks} fallbacks` : '')
    : undefined

  const rawEventsBadge = d ? `${d.events_stats.total} events` : undefined

  return createPortal(
    <>
      {/* 半透明遮罩 */}
      <div
        className="fixed inset-0 z-40 bg-black/30"
        onClick={onClose}
      />
      {/* 侧边栏 */}
      <div className="fixed inset-y-0 right-0 z-50 flex w-[500px] max-w-full flex-col border-l border-[var(--c-border)] bg-[var(--c-bg-deep2)] shadow-2xl">
        {/* 顶部标题栏 */}
        <div className="flex shrink-0 items-center justify-between border-b border-[var(--c-border-console)] px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="font-mono text-xs text-[var(--c-text-muted)]" title={run.run_id}>
              {run.run_id.slice(0, 12)}…
            </span>
            <Badge variant={statusVariant(run.status)}>{run.status}</Badge>
            {(d?.duration_ms ?? run.duration_ms) != null && (
              <span className="text-xs text-[var(--c-text-muted)]">
                {formatDuration(d?.duration_ms ?? run.duration_ms)}
              </span>
            )}
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <X size={16} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {/* OVERVIEW */}
          <Section title="Overview" defaultOpen>
            {detailLoading && !d && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">Loading…</p>
            )}
            <div className="divide-y divide-[var(--c-border-console)]">
              <div className="pb-2">
                <MetaRow label="User" value={
                  (d?.created_by_user_name ?? run.created_by_user_name)
                    ? `${d?.created_by_user_name ?? run.created_by_user_name}${(d?.created_by_email ?? run.created_by_email) ? `  ·  ${d?.created_by_email ?? run.created_by_email}` : ''}`
                    : (d?.created_by_user_id ?? run.created_by_user_id)
                } />
                <MetaRow label="Thread" value={run.thread_id} mono />
                <MetaRow label="Org" value={run.org_id} mono />
                <MetaRow label="Skill" value={d?.skill_id ?? run.skill_id} />
              </div>
              <div className="pt-2">
                <MetaRow label="Model" value={d?.model ?? run.model} />
                <MetaRow
                  label="Provider"
                  value={
                    d?.provider_kind
                      ? d.api_mode ? `${d.provider_kind} · ${d.api_mode}` : d.provider_kind
                      : undefined
                  }
                />
                <MetaRow
                  label="Tokens"
                  value={
                    (d?.total_input_tokens ?? run.total_input_tokens) != null
                      ? `${d?.total_input_tokens ?? run.total_input_tokens} in / ${d?.total_output_tokens ?? run.total_output_tokens ?? 0} out`
                      : undefined
                  }
                />
                <MetaRow label="Cost" value={formatCost(d?.total_cost_usd ?? run.total_cost_usd)} />
              </div>
              <div className="pt-2">
                <MetaRow label="Created" value={new Date(run.created_at).toLocaleString()} />
                {(d?.completed_at ?? run.completed_at) && (
                  <MetaRow label="Completed" value={new Date((d?.completed_at ?? run.completed_at)!).toLocaleString()} />
                )}
                {(d?.failed_at ?? run.failed_at) && (
                  <MetaRow label="Failed at" value={new Date((d?.failed_at ?? run.failed_at)!).toLocaleString()} />
                )}
              </div>
            </div>
          </Section>

          {/* CONVERSATION — 默认折叠，展开时懒加载 */}
          <Section
            title="Conversation"
            badge={conversationBadge}
            defaultOpen={false}
            onOpen={loadEvents}
          >
            {eventsLoading && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">Loading…</p>
            )}
            {!eventsLoading && events !== null && turns.length === 0 && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">No LLM turns found</p>
            )}
            {turns.length > 0 && (
              <div className="space-y-3">
                {turns.map((turn, i) => (
                  <TurnView key={turn.llmCallId || i} turn={turn} index={i} />
                ))}
              </div>
            )}
          </Section>

          {/* RAW EVENTS — 调试用，始终折叠 */}
          <Section
            title="Raw Events"
            badge={rawEventsBadge}
            defaultOpen={false}
            onOpen={loadEvents}
          >
            {eventsLoading && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">Loading…</p>
            )}
            {!eventsLoading && events !== null && events.length === 0 && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">No events</p>
            )}
            {events && events.length > 0 && (
              <div className="space-y-1">
                {events.map((ev) => (
                  <RawEventRow key={ev.seq} event={ev} />
                ))}
              </div>
            )}
          </Section>
        </div>
      </div>
    </>,
    document.body,
  )
}

type RawEventRowProps = { event: RunEventRaw }

function RawEventRow({ event }: RawEventRowProps) {
  const [open, setOpen] = useState(false)
  const hasData = event.data && Object.keys(event.data).length > 0

  return (
    <div className="rounded border border-[var(--c-border)] overflow-hidden">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-[var(--c-bg-sub)]"
        disabled={!hasData}
      >
        <span className="w-6 shrink-0 text-right font-mono text-xs text-[var(--c-text-muted)]">
          {event.seq}
        </span>
        <span className="text-xs font-medium text-[var(--c-text-secondary)]">{event.type}</span>
        {event.tool_name && (
          <span className="text-xs text-[var(--c-text-muted)]">{event.tool_name}</span>
        )}
        {event.error_class && (
          <span className="ml-auto text-xs text-red-500">{event.error_class}</span>
        )}
        <span className="ml-auto text-xs text-[var(--c-text-muted)]">
          {new Date(event.ts).toLocaleTimeString()}
        </span>
      </button>
      {open && hasData && (
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2">
          <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-[var(--c-text-secondary)]">
            {JSON.stringify(event.data, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}
