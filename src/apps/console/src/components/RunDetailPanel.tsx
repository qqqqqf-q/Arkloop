import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { createPortal } from 'react-dom'
import { X, ChevronDown, ChevronRight, ArrowLeft } from 'lucide-react'
import { useLocation } from 'react-router-dom'
import type { GlobalRun, AdminRunDetail, AdminRunUsageItem, AdminRunUsageAggregate, RunEventRaw } from '../api/runs'
import { getAdminRunDetail, fetchRunEventsOnce } from '../api/runs'
import { TurnView } from './TurnView'
import { buildThreadTurns, buildTurns, type ThreadTurn } from '@arkloop/shared'
import { Badge, type BadgeVariant } from './Badge'
import { useLocale } from '../contexts/LocaleContext'

function statusVariant(status: string): BadgeVariant {
  switch (status) {
    case 'running': return 'warning'
    case 'completed': return 'success'
    case 'failed': return 'error'
    default: return 'neutral'
  }
}

function formatDuration(ms?: number): string {
  if (ms == null) return '—'
  const secs = Math.floor(ms / 1000)
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  return `${mins}m ${secs % 60}s`
}

function formatCost(usd?: number): string {
  if (usd == null) return '—'
  const decimals = Math.abs(usd) < 0.01 ? 6 : 4
  return `$${usd.toFixed(decimals)}`
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

type TabKey = 'thread' | 'execution' | 'events' | 'overview'

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
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">{turn.userText || 'Input unavailable'}</pre>
        </div>
      </div>
      <div className="rounded border border-[var(--c-border)] overflow-hidden">
        <div className="px-2.5 py-1.5 text-[11px] font-medium text-[var(--c-text-secondary)]">Assistant</div>
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-2">
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">{turn.assistantText || 'Assistant output unavailable'}</pre>
        </div>
      </div>
    </div>
  )
}

export function RunDetailPanel({ run, accessToken, onClose }: Props) {
  const location = useLocation()
  const [activeTab, setActiveTab] = useState<TabKey>('thread')
  const [detail, setDetail] = useState<AdminRunDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [events, setEvents] = useState<RunEventRaw[] | null>(null)
  const [eventsLoading, setEventsLoading] = useState(false)
  const [navStack, setNavStack] = useState<GlobalRun[]>([])
  const { t, locale } = useLocale()
  const rt = t.pages.runs

  // 当前正在查看的 run（可能是子 run）
  const [activeRun, setActiveRun] = useState<GlobalRun | null>(null)

  // 外部 run prop 变化时，重置导航栈
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      setActiveRun(run)
      setNavStack([])
    })
    return () => cancelAnimationFrame(id)
  }, [run])

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

  const currentRun = activeRun

  // 面板打开时加载 summary
  useEffect(() => {
    if (!currentRun) {
      const id = requestAnimationFrame(() => {
        setDetail(null)
        setEvents(null)
      })
      return () => cancelAnimationFrame(id)
    }
    let cancelled = false
    const id = requestAnimationFrame(() => {
      if (cancelled) return
      setDetail(null)
      setEvents(null)
      setDetailLoading(true)
    })
    Promise.allSettled([getAdminRunDetail(currentRun.run_id, accessToken)])
      .then(([detailResult]) => {
        if (cancelled) return
        if (detailResult.status === 'fulfilled') setDetail(detailResult.value)
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setDetailLoading(false)
      })
    return () => {
      cancelled = true
      cancelAnimationFrame(id)
    }
  }, [currentRun, accessToken])

  // Conversation 展开时懒加载事件流
  const loadEvents = useCallback(() => {
    if (!currentRun || events !== null) return
    setEventsLoading(true)
    fetchRunEventsOnce(currentRun.run_id, accessToken)
      .then(setEvents)
      .catch(() => setEvents([]))
      .finally(() => setEventsLoading(false))
  }, [currentRun, events, accessToken])

  useEffect(() => {
    if (!((activeTab === 'execution' || activeTab === 'events') && events === null)) return
    const id = requestAnimationFrame(() => loadEvents())
    return () => cancelAnimationFrame(id)
  }, [activeTab, events, loadEvents])

  // 导航到子 Run
  const navigateToChild = useCallback((child: GlobalRun) => {
    if (!currentRun) return
    setNavStack((prev) => [...prev, currentRun])
    setActiveRun(child)
  }, [currentRun])

  // 返回上一级
  const navigateBack = useCallback(() => {
    setNavStack((prev) => {
      if (prev.length === 0) return prev
      const next = [...prev]
      const parent = next.pop()!
      setActiveRun(parent)
      return next
    })
  }, [])

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() },
    [onClose],
  )
  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  // currentRun 可能是子 run，所有渲染基于 currentRun
  const r = currentRun ?? run
  const d = detail
  const turns = useMemo(() => (events ? buildTurns(events) : []), [events])
  const threadTurns = useMemo(
    () => buildThreadTurns(d?.thread_messages ?? [], r?.run_id ?? '', d?.user_prompt),
    [d?.thread_messages, d?.user_prompt, r?.run_id],
  )
  const executionTurns = turns
  const threadLabel = locale.startsWith('zh') ? '对话线程' : 'Thread'
  const executionLabel = locale.startsWith('zh') ? '本轮执行' : 'Execution'

  const executionBadge =
    events !== null
      ? `${executionTurns.length} turn${executionTurns.length !== 1 ? 's' : ''}` +
        ((d?.events_stats.tool_calls ?? 0) > 0 ? ` · ${d!.events_stats.tool_calls} tool calls` : '') +
        ((d?.events_stats.provider_fallbacks ?? 0) > 0 ? ` · ${d!.events_stats.provider_fallbacks} fallbacks` : '')
      : d
        ? `${d.events_stats.llm_turns} turn${d.events_stats.llm_turns !== 1 ? 's' : ''}` +
          (d.events_stats.tool_calls > 0 ? ` · ${d.events_stats.tool_calls} tool calls` : '') +
          (d.events_stats.provider_fallbacks > 0 ? ` · ${d.events_stats.provider_fallbacks} fallbacks` : '')
        : undefined

  const rawEventsBadge = d ? `${d.events_stats.total} events` : undefined

  // Browser activity summary from turns
  const browserSummary = useMemo(() => {
    if (turns.length === 0) return null
    const browserCalls = turns.flatMap((t) =>
      t.toolCalls.filter((tc) => tc.toolName === 'browser'),
    )
    if (browserCalls.length === 0) return null
    const commands = browserCalls
      .map((tc) => (typeof tc.argsJSON?.command === 'string' ? tc.argsJSON.command : null))
      .filter((c): c is string => c !== null)
    const screenshotCount = browserCalls.filter(
      (tc) => tc.resultJSON?.has_screenshot === true,
    ).length
    const errorCount = browserCalls.filter((tc) => !!tc.errorClass).length
    return { total: browserCalls.length, commands, screenshotCount, errorCount }
  }, [turns])

  if (!r) return null

  const usageChildren = d?.children ?? []
  const hasUsageBreakdown = usageChildren.length > 0
  const usageAggregate = d?.total_aggregate

  const selfUsageItem: AdminRunUsageItem = {
    run_id: r.run_id,
    account_id: r.account_id,
    thread_id: r.thread_id,
    parent_run_id: r.parent_run_id,
    status: r.status,
    persona_id: d?.persona_id ?? r.persona_id,
    model: d?.model ?? r.model,
    provider_kind: d?.provider_kind,
    credential_name: d?.credential_name,
    persona_model: d?.persona_model,
    duration_ms: d?.duration_ms ?? r.duration_ms,
    total_input_tokens: d?.total_input_tokens ?? r.total_input_tokens,
    total_output_tokens: d?.total_output_tokens ?? r.total_output_tokens,
    total_cost_usd: d?.total_cost_usd ?? r.total_cost_usd,
    cache_hit_rate: r.cache_hit_rate,
    credits_used: r.credits_used,
    created_at: r.created_at,
    completed_at: d?.completed_at ?? r.completed_at,
    failed_at: d?.failed_at ?? r.failed_at,
  }

  return createPortal(
    <>
      {/* 半透明遮罩 */}
      <div
        className="fixed inset-0 z-40 bg-black/30"
        onClick={onClose}
      />
      {/* 侧边栏 */}
      <div className="fixed inset-y-0 right-0 z-50 flex w-[500px] max-w-full flex-col border-l border-[var(--c-border)] bg-[var(--c-bg-deep2)] shadow-2xl">
        {/* 面包屑导航（存在导航栈时显示） */}
        {navStack.length > 0 && (
          <div className="flex shrink-0 items-center gap-1.5 border-b border-[var(--c-border-console)] px-4 py-2 text-xs">
            <button
              onClick={navigateBack}
              className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            >
              <ArrowLeft size={12} />
              <span className="font-mono">{navStack[navStack.length - 1].run_id.slice(0, 8)}</span>
            </button>
            <span className="text-[var(--c-text-muted)]">/</span>
            <span className="font-mono text-[var(--c-text-secondary)]">{r.run_id.slice(0, 8)}</span>
          </div>
        )}

        {/* 顶部标题栏 */}
        <div className="flex shrink-0 items-center justify-between border-b border-[var(--c-border-console)] px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="font-mono text-xs text-[var(--c-text-muted)]" title={r.run_id}>
              {r.run_id.slice(0, 12)}...
            </span>
            <Badge variant={statusVariant(r.status)}>{r.status}</Badge>
            {(d?.duration_ms ?? r.duration_ms) != null && (
              <span className="text-xs text-[var(--c-text-muted)]">
                {formatDuration(d?.duration_ms ?? r.duration_ms)}
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

        <div className="flex shrink-0 border-b border-[var(--c-border-console)] px-4">
          {[
            { key: 'thread', label: threadLabel, count: threadTurns.length },
            { key: 'execution', label: executionLabel, count: executionTurns.length },
            { key: 'events', label: rt.sectionRawEvents, count: events?.length ?? 0 },
            { key: 'overview', label: rt.sectionOverview, count: 0 },
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

        <div className="flex-1 overflow-y-auto">
          {activeTab === 'overview' && (
          <>
          <Section title={rt.sectionOverview} defaultOpen>
            {detailLoading && !d && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.loading}</p>
            )}
            <div className="divide-y divide-[var(--c-border-console)]">
              <div className="pb-2">
                <MetaRow label={rt.labelUser} value={
                  (d?.created_by_user_name ?? r.created_by_user_name)
                    ? `${d?.created_by_user_name ?? r.created_by_user_name}${(d?.created_by_email ?? r.created_by_email) ? `  ·  ${d?.created_by_email ?? r.created_by_email}` : ''}`
                    : (d?.created_by_user_id ?? r.created_by_user_id)
                } />
                <MetaRow label={rt.labelThread} value={r.thread_id} mono />
                <MetaRow label={rt.labelAccount} value={r.account_id} mono />
                <MetaRow label={rt.labelPersona} value={d?.persona_id ?? r.persona_id} />
              </div>
              <div className="pt-2">
                <MetaRow label={rt.labelPersonaModel} value={d?.persona_model} />
                <MetaRow label={rt.labelCredential} value={d?.credential_name} />
                <MetaRow label={rt.labelModel} value={d?.model ?? r.model} />
                <MetaRow
                  label={rt.labelTokens}
                  value={
                    (d?.total_input_tokens ?? r.total_input_tokens) != null
                      ? `${d?.total_input_tokens ?? r.total_input_tokens} in / ${d?.total_output_tokens ?? r.total_output_tokens ?? 0} out`
                      : undefined
                  }
                />
                <MetaRow label={rt.labelCost} value={formatCost(d?.total_cost_usd ?? r.total_cost_usd)} />
                {r.credits_used != null && (
                  <MetaRow label={rt.labelCreditsUsed} value={String(r.credits_used)} />
                )}
                {r.cache_hit_rate != null && (
                  <MetaRow
                    label={rt.labelCacheHit}
                    value={`${(r.cache_hit_rate * 100).toFixed(0)}%`}
                  />
                )}
              </div>
              <div className="pt-2">
                <MetaRow label={rt.labelCreated} value={new Date(r.created_at).toLocaleString()} />
                {(d?.completed_at ?? r.completed_at) && (
                  <MetaRow label={rt.labelCompleted} value={new Date((d?.completed_at ?? r.completed_at)!).toLocaleString()} />
                )}
                {(d?.failed_at ?? r.failed_at) && (
                  <MetaRow label={rt.labelFailedAt} value={new Date((d?.failed_at ?? r.failed_at)!).toLocaleString()} />
                )}
              </div>
            </div>
          </Section>

          {/* USAGE BREAKDOWN */}
          {hasUsageBreakdown && (
            <Section
              title={rt.sectionUsage}
              badge={`${usageChildren.length + 1} runs`}
              defaultOpen
            >
              <UsageBreakdownTable
                self={selfUsageItem}
                children={usageChildren}
                aggregate={usageAggregate}
                onOpenRun={navigateToChild}
              />
            </Section>
          )}

          {/* BROWSER — 仅在有 browser 调用时显示 */}
          {browserSummary && (
            <Section
              title={rt.sectionBrowser ?? 'Browser'}
              badge={`${browserSummary.total} commands`}
              defaultOpen={false}
            >
              <div className="space-y-2">
                <div className="flex flex-wrap items-center gap-2 text-xs text-[var(--c-text-secondary)]">
                  <span>{browserSummary.total} commands</span>
                  {browserSummary.screenshotCount > 0 && (
                    <span className="text-green-500">{browserSummary.screenshotCount} screenshots</span>
                  )}
                  {browserSummary.errorCount > 0 && (
                    <span className="text-red-500">{browserSummary.errorCount} errors</span>
                  )}
                </div>
                <div className="space-y-0.5">
                  {browserSummary.commands.map((cmd, i) => (
                    <div key={i} className="rounded bg-[var(--c-bg-sub)] px-2 py-1 font-mono text-[11px] text-[var(--c-text-secondary)]">
                      {cmd}
                    </div>
                  ))}
                </div>
              </div>
            </Section>
          )}
          </>
          )}

          {activeTab === 'thread' && (
          <Section
            title={threadLabel}
            badge={`${threadTurns.length} turn${threadTurns.length !== 1 ? 's' : ''}`}
            defaultOpen
          >
            {detailLoading && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.loading}</p>
            )}
            {!detailLoading && threadTurns.length === 0 && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.noConversation}</p>
            )}
            {threadTurns.length > 0 && (
              <div className="space-y-3">
                {threadTurns.map((turn, i) => (
                  <ThreadTurnCard key={turn.key} turn={turn} index={i} />
                ))}
              </div>
            )}
          </Section>
          )}

          {activeTab === 'execution' && (
          <Section
            title={executionLabel}
            badge={executionBadge}
            defaultOpen
            onOpen={loadEvents}
          >
            {eventsLoading && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.loading}</p>
            )}
            {d?.user_prompt && (
              <UserPromptBlock prompt={d.user_prompt} label={rt.userPrompt} />
            )}
            {!eventsLoading && events !== null && executionTurns.length === 0 && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.noConversation}</p>
            )}
            {executionTurns.length > 0 && (
              <div className="space-y-3">
                {executionTurns.map((turn, i) => (
                  <TurnView key={turn.llmCallId || i} turn={turn} index={i} />
                ))}
              </div>
            )}
          </Section>
          )}

          {activeTab === 'events' && (
          <Section
            title={rt.sectionRawEvents}
            badge={rawEventsBadge}
            defaultOpen
            onOpen={loadEvents}
          >
            {eventsLoading && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.loading}</p>
            )}
            {!eventsLoading && events !== null && events.length === 0 && (
              <p className="py-2 text-xs text-[var(--c-text-muted)]">{rt.noEvents}</p>
            )}
            {events && events.length > 0 && (
              <div className="space-y-2">
                <p className="text-[11px] text-[var(--c-text-muted)]">
                  Events are shown in sequence order. Recorded time may differ from when the model started emitting output.
                </p>
                <RawEventsSectionContent events={events} />
              </div>
            )}
          </Section>
          )}
        </div>
      </div>
    </>,
    document.body,
  )
}

function toGlobalRun(item: AdminRunUsageItem): GlobalRun {
  return {
    run_id: item.run_id,
    account_id: item.account_id,
    thread_id: item.thread_id,
    status: item.status,
    model: item.model,
    persona_id: item.persona_id,
    parent_run_id: item.parent_run_id,
    total_input_tokens: item.total_input_tokens,
    total_output_tokens: item.total_output_tokens,
    total_cost_usd: item.total_cost_usd,
    duration_ms: item.duration_ms,
    cache_hit_rate: item.cache_hit_rate,
    credits_used: item.credits_used,
    created_at: item.created_at,
    completed_at: item.completed_at,
    failed_at: item.failed_at,
    created_by_user_id: undefined,
    created_by_user_name: undefined,
    created_by_email: undefined,
  }
}

type UsageBreakdownTableProps = {
  self: AdminRunUsageItem
  children: AdminRunUsageItem[]
  aggregate?: AdminRunUsageAggregate
  onOpenRun?: (run: GlobalRun) => void
}

function stageLabel(rt: ReturnType<typeof useLocale>['t']['pages']['runs'], item: AdminRunUsageItem, isSelf: boolean): string {
  if (isSelf) return rt.usageStageMain
  if (item.persona_id === 'search-output') return rt.usageStageFinal
  return rt.usageStageChild
}

function cacheLabel(item: AdminRunUsageItem): string {
  if (item.cache_hit_rate == null) return '—'
  return `${(item.cache_hit_rate * 100).toFixed(0)}%`
}

function cacheTitle(item: AdminRunUsageItem): string | undefined {
  const parts: string[] = []
  if (item.cache_read_tokens != null) parts.push(`read ${item.cache_read_tokens}`)
  if (item.cache_creation_tokens != null) parts.push(`write ${item.cache_creation_tokens}`)
  if (item.cached_tokens != null) parts.push(`cached ${item.cached_tokens}`)
  return parts.length > 0 ? parts.join(' · ') : undefined
}

function UsageBreakdownTable({ self, children, aggregate, onOpenRun }: UsageBreakdownTableProps) {
  const { t } = useLocale()
  const rt = t.pages.runs

  const rows: Array<{ item: AdminRunUsageItem; isSelf: boolean }> = [
    { item: self, isSelf: true },
    ...children.map((c) => ({ item: c, isSelf: false })),
  ]

  return (
    <div className="space-y-2">
      <div className="overflow-x-auto rounded-lg border border-[var(--c-border)]">
        <table className="min-w-[860px] w-full text-xs">
          <thead className="bg-[var(--c-bg-sub)] text-[var(--c-text-muted)]">
            <tr className="text-left">
              <th className="w-24 whitespace-nowrap px-3 py-2 font-medium">{rt.usageColStage}</th>
              <th className="min-w-[280px] whitespace-nowrap px-3 py-2 font-medium">{rt.usageColModel}</th>
              <th className="w-40 whitespace-nowrap px-3 py-2 font-medium">{rt.usageColTokens}</th>
              <th className="w-28 whitespace-nowrap px-3 py-2 font-medium">{rt.usageColCost}</th>
              <th className="w-24 whitespace-nowrap px-3 py-2 font-medium">{rt.usageColCache}</th>
              <th className="min-w-[260px] whitespace-nowrap px-3 py-2 font-medium">{rt.usageColRun}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[var(--c-border-console)]">
            {rows.map(({ item, isSelf }) => {
              const inp = item.total_input_tokens ?? 0
              const out = item.total_output_tokens ?? 0
              const modelText = item.model ?? '—'
              const providerText = item.provider_kind ? ` · ${item.provider_kind}` : ''

              return (
                <tr key={item.run_id} className="bg-[var(--c-bg-deep2)]">
                  <td className="whitespace-nowrap px-3 py-2 text-[var(--c-text-secondary)]">
                    {stageLabel(rt, item, isSelf)}
                  </td>
                  <td className="px-3 py-2 text-[var(--c-text-secondary)]" title={modelText}>
                    <div className="truncate">
                      {modelText}
                      <span className="text-[var(--c-text-muted)]">{providerText}</span>
                    </div>
                    {(item.credential_name || item.persona_model) && (
                      <div
                        className="truncate text-[11px] text-[var(--c-text-muted)]"
                        title={item.credential_name ?? item.persona_model}
                      >
                        {item.credential_name ?? item.persona_model}
                      </div>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 tabular-nums text-[var(--c-text-secondary)]">
                    {inp} / {out}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 tabular-nums text-[var(--c-text-secondary)]">
                    {formatCost(item.total_cost_usd)}
                  </td>
                  <td
                    className={[
                      'whitespace-nowrap px-3 py-2 tabular-nums',
                      item.cache_hit_rate != null ? 'text-[var(--c-status-success-text)]' : 'text-[var(--c-text-muted)]',
                    ].join(' ')}
                    title={cacheTitle(item)}
                  >
                    {cacheLabel(item)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2">
                    {onOpenRun ? (
                      <button
                        onClick={() => onOpenRun(toGlobalRun(item))}
                        className="font-mono text-[11px] text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]"
                        title={item.run_id}
                      >
                        {item.run_id}
                      </button>
                    ) : (
                      <span className="font-mono text-[11px] text-[var(--c-text-muted)]" title={item.run_id}>
                        {item.run_id}
                      </span>
                    )}
                  </td>
                </tr>
              )
            })}

            {aggregate && (
              <tr className="bg-[var(--c-bg-sub)]">
                <td className="px-3 py-2 font-medium text-[var(--c-text-secondary)]">{rt.usageTotal}</td>
                <td className="px-3 py-2 text-[var(--c-text-muted)]" />
                <td className="px-3 py-2 tabular-nums font-medium text-[var(--c-text-secondary)]">
                  {(aggregate.total_input_tokens ?? 0)} / {(aggregate.total_output_tokens ?? 0)}
                </td>
                <td className="px-3 py-2 tabular-nums font-medium text-[var(--c-text-secondary)]">
                  {formatCost(aggregate.total_cost_usd)}
                </td>
                <td className="px-3 py-2 text-[var(--c-text-muted)]" />
                <td className="px-3 py-2 text-[var(--c-text-muted)]" />
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

type RawEventRowProps = { event: RunEventRaw }

function formatEventTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    fractionalSecondDigits: 3,
  }).format(date)
}

function UserPromptBlock({ prompt, label }: { prompt: string; label: string }) {
  const [open, setOpen] = useState(false)
  const preview = prompt.slice(0, 100) + (prompt.length > 100 ? '…' : '')

  return (
    <div className="mb-3 rounded border border-[var(--c-border)] overflow-hidden">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-start gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--c-bg-sub)]"
      >
        <span className="shrink-0 rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-xs font-medium text-[var(--c-text-secondary)]">
          {label}
        </span>
        {!open && (
          <span className="truncate text-xs text-[var(--c-text-muted)]">{preview}</span>
        )}
      </button>
      {open && (
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2">
          <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-[var(--c-text-secondary)]">
            {prompt}
          </pre>
        </div>
      )}
    </div>
  )
}

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
        <span className="ml-auto text-xs text-[var(--c-text-muted)]" title={`recorded ${event.ts}`}>
          recorded {formatEventTime(event.ts)}
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

// ---- Raw Events 过滤与 Delta 合并 ----

const EVENT_CATEGORIES: Record<string, string[]> = {
  LLM:       ['llm.request', 'run.route.selected', 'run.provider_fallback', 'run.llm.retry'],
  Tools:     ['tool.call', 'tool.result', 'tool.denied'],
  Lifecycle: ['run.started', 'run.completed', 'run.failed', 'run.cancelled', 'run.cancel_requested'],
  Streaming: ['message.delta', 'llm.response.chunk', 'run.segment.start', 'run.segment.end'],
  Input:     ['run.input_requested', 'run.input_provided'],
}

function categoryOf(type: string): string {
  for (const [cat, types] of Object.entries(EVENT_CATEGORIES)) {
    if (types.includes(type)) return cat
  }
  return 'Other'
}

type DeltaGroup = {
  kind: 'delta_group'
  events: RunEventRaw[]
  mergedText: string
  seqStart: number
  seqEnd: number
}

type RawEventItem = { kind: 'single'; event: RunEventRaw } | DeltaGroup

function groupAndFilterEvents(
  events: RunEventRaw[],
  enabledCategories: Set<string>,
): RawEventItem[] {
  const filtered = events.filter((ev) => enabledCategories.has(categoryOf(ev.type)))
  const items: RawEventItem[] = []
  let deltaBuffer: RunEventRaw[] = []

  const flushDeltas = () => {
    if (deltaBuffer.length === 0) return
    if (deltaBuffer.length === 1) {
      items.push({ kind: 'single', event: deltaBuffer[0] })
    } else {
      const merged = deltaBuffer
        .map((e) => String((e.data as Record<string, unknown>).content_delta ?? ''))
        .join('')
      items.push({
        kind: 'delta_group',
        events: deltaBuffer,
        mergedText: merged,
        seqStart: deltaBuffer[0].seq,
        seqEnd: deltaBuffer[deltaBuffer.length - 1].seq,
      })
    }
    deltaBuffer = []
  }

  for (const ev of filtered) {
    if (ev.type === 'message.delta' || ev.type === 'llm.response.chunk') {
      deltaBuffer.push(ev)
    } else {
      flushDeltas()
      items.push({ kind: 'single', event: ev })
    }
  }
  flushDeltas()
  return items
}

function DeltaGroupRow({ group }: { group: DeltaGroup }) {
  const [open, setOpen] = useState(false)
  const preview = group.mergedText.slice(0, 80) + (group.mergedText.length > 80 ? '...' : '')

  return (
    <div className="rounded border border-[var(--c-border)] overflow-hidden">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-[var(--c-bg-sub)]"
      >
        <span className="w-6 shrink-0 text-right font-mono text-xs text-[var(--c-text-muted)]">
          {group.seqStart}..{group.seqEnd}
        </span>
        <span className="text-xs font-medium text-[var(--c-text-secondary)]">
          message.delta x{group.events.length}
        </span>
        {!open && (
          <span className="ml-1 truncate text-xs text-[var(--c-text-muted)]">{preview}</span>
        )}
      </button>
      {open && (
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2">
          <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-[var(--c-text-secondary)]">
            {group.mergedText}
          </pre>
        </div>
      )}
    </div>
  )
}

type FilterChipProps = {
  label: string
  count: number
  active: boolean
  onToggle: () => void
}

function FilterChip({ label, count, active, onToggle }: FilterChipProps) {
  return (
    <button
      onClick={onToggle}
      className={[
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium transition-colors',
        active
          ? 'bg-[var(--c-text-secondary)] text-[var(--c-bg-deep2)]'
          : 'bg-[var(--c-bg-sub)] text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]',
      ].join(' ')}
    >
      {label}
      <span className="tabular-nums">{count}</span>
    </button>
  )
}

type RawEventsSectionContentProps = {
  events: RunEventRaw[]
}

function RawEventsSectionContent({ events }: RawEventsSectionContentProps) {
  const [enabled, setEnabled] = useState<Set<string>>(() => {
    const all = new Set(Object.keys(EVENT_CATEGORIES))
    all.add('Other')
    all.delete('Streaming')
    return all
  })

  const categoryCounts = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const ev of events) {
      const cat = categoryOf(ev.type)
      counts[cat] = (counts[cat] ?? 0) + 1
    }
    return counts
  }, [events])

  const toggle = useCallback((cat: string) => {
    setEnabled((prev) => {
      const next = new Set(prev)
      if (next.has(cat)) next.delete(cat)
      else next.add(cat)
      return next
    })
  }, [])

  const items = useMemo(() => groupAndFilterEvents(events, enabled), [events, enabled])

  const categories = [...Object.keys(EVENT_CATEGORIES)]
  if (categoryCounts['Other']) categories.push('Other')

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1">
        {categories.map((cat) => (
          <FilterChip
            key={cat}
            label={cat}
            count={categoryCounts[cat] ?? 0}
            active={enabled.has(cat)}
            onToggle={() => toggle(cat)}
          />
        ))}
      </div>
      <div className="space-y-1">
        {items.map((item) =>
          item.kind === 'delta_group' ? (
            <DeltaGroupRow key={`dg-${item.seqStart}`} group={item} />
          ) : (
            <RawEventRow key={item.event.seq} event={item.event} />
          ),
        )}
        {items.length === 0 && (
          <p className="py-2 text-xs text-[var(--c-text-muted)]">--</p>
        )}
      </div>
    </div>
  )
}
