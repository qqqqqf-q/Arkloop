import { useState, useCallback, useEffect, useMemo, type ReactNode } from 'react'
import { useOutletContext, useSearchParams } from 'react-router-dom'
import { RefreshCw, Play } from 'lucide-react'
import type { ConsoleOutletContext } from '../layouts/ConsoleLayout'
import { PageHeader } from '../components/PageHeader'
import { DataTable, type Column } from '../components/DataTable'
import { Badge, type BadgeVariant } from '../components/Badge'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { RunDetailPanel } from '../components/RunDetailPanel'
import { listRuns, cancelRun, type GlobalRun } from '../api/runs'

const PAGE_SIZE = 50

type RunFilters = {
  runId: string
  threadId: string
  userId: string
  projectId: string
  parentRunId: string
  status: string
  model: string
  personaId: string
  since: string
  until: string
}

const EMPTY_RUN_FILTERS: RunFilters = {
  runId: '',
  threadId: '',
  userId: '',
  projectId: '',
  parentRunId: '',
  status: '',
  model: '',
  personaId: '',
  since: '',
  until: '',
}

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
  const rem = secs % 60
  return `${mins}m ${rem}s`
}

function formatCost(usd?: number): string {
  if (usd == null) return '--'
  const decimals = Math.abs(usd) < 0.01 ? 6 : 4
  return `$${usd.toFixed(decimals)}`
}

function truncateId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

function parseInitialFilters(searchParams: URLSearchParams): RunFilters {
  return {
    ...EMPTY_RUN_FILTERS,
    runId: searchParams.get('run_id') ?? '',
    threadId: searchParams.get('thread_id') ?? '',
    userId: searchParams.get('user_id') ?? '',
    projectId: searchParams.get('project_id') ?? '',
    parentRunId: searchParams.get('parent_run_id') ?? '',
    status: searchParams.get('status') ?? '',
    model: searchParams.get('model') ?? '',
    personaId: searchParams.get('persona_id') ?? '',
  }
}

function normalizeFilters(filters: RunFilters): RunFilters {
  return {
    ...filters,
    runId: filters.runId.trim(),
    threadId: filters.threadId.trim(),
    userId: filters.userId.trim(),
    projectId: filters.projectId.trim(),
    parentRunId: filters.parentRunId.trim(),
    status: filters.status.trim(),
    model: filters.model.trim(),
    personaId: filters.personaId.trim(),
  }
}

function toRFC3339(value: string): string | undefined {
  if (!value.trim()) return undefined
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return undefined
  return parsed.toISOString()
}

function countActiveFilters(filters: RunFilters): number {
  const values = [
    filters.runId,
    filters.threadId,
    filters.userId,
    filters.projectId,
    filters.parentRunId,
    filters.status,
    filters.model,
    filters.personaId,
    filters.since,
    filters.until,
  ]
  return values.filter((value) => value.trim() !== '').length
}

export function RunsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const rt = t.pages.runs
  const [searchParams] = useSearchParams()

  const statusOptions = useMemo(() => [
    { value: '', label: rt.filterAll },
    { value: 'running', label: rt.filterRunning },
    { value: 'completed', label: rt.filterCompleted },
    { value: 'failed', label: rt.filterFailed },
    { value: 'cancelled', label: rt.filterCancelled },
  ], [rt])

  const [runs, setRuns] = useState<GlobalRun[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [draftFilters, setDraftFilters] = useState<RunFilters>(() => parseInitialFilters(searchParams))
  const [appliedFilters, setAppliedFilters] = useState<RunFilters>(() => parseInitialFilters(searchParams))
  const [offset, setOffset] = useState(0)
  const [cancelTarget, setCancelTarget] = useState<GlobalRun | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [selectedRun, setSelectedRun] = useState<GlobalRun | null>(null)

  const fetchRuns = useCallback(
    async (filters: RunFilters, currentOffset: number) => {
      setLoading(true)
      try {
        const resp = await listRuns(
          {
            run_id: filters.runId || undefined,
            thread_id: filters.threadId || undefined,
            user_id: filters.userId || undefined,
            account_id: filters.projectId || undefined,
            parent_run_id: filters.parentRunId || undefined,
            status: filters.status || undefined,
            model: filters.model || undefined,
            persona_id: filters.personaId || undefined,
            since: toRFC3339(filters.since),
            until: toRFC3339(filters.until),
            limit: PAGE_SIZE,
            offset: currentOffset,
          },
          accessToken,
        )
        setRuns(resp.data)
        setTotal(resp.total)
      } catch (err) {
        addToast(isApiError(err) ? err.message : rt.toastLoadFailed, 'error')
      } finally {
        setLoading(false)
      }
    },
    [accessToken, addToast, rt.toastLoadFailed],
  )

  useEffect(() => {
    void fetchRuns(appliedFilters, offset)
  }, [fetchRuns, appliedFilters, offset])

  const updateDraftFilter = useCallback(
    (key: keyof RunFilters, value: string) => {
      setDraftFilters((prev) => ({ ...prev, [key]: value }))
    },
    [],
  )

  const handleApplyFilters = useCallback(() => {
    const normalized = normalizeFilters(draftFilters)
    setDraftFilters(normalized)
    setAppliedFilters(normalized)
    setOffset(0)
  }, [draftFilters])

  const handleResetFilters = useCallback(() => {
    const cleared = { ...EMPTY_RUN_FILTERS }
    setDraftFilters(cleared)
    setAppliedFilters(cleared)
    setOffset(0)
  }, [])

  const handleRefresh = useCallback(() => {
    void fetchRuns(appliedFilters, offset)
  }, [fetchRuns, appliedFilters, offset])

  const handleCancelConfirm = useCallback(async () => {
    if (!cancelTarget) return
    setCancelling(true)
    try {
      await cancelRun(cancelTarget.run_id, accessToken)
      setCancelTarget(null)
      void fetchRuns(appliedFilters, offset)
    } catch (err) {
      addToast(isApiError(err) ? err.message : rt.toastCancelFailed, 'error')
    } finally {
      setCancelling(false)
    }
  }, [cancelTarget, accessToken, fetchRuns, appliedFilters, offset, addToast, rt.toastCancelFailed])

  const columns: Column<GlobalRun>[] = [
    {
      key: 'run_id',
      header: rt.colId,
      render: (row) => (
        <span className="font-mono text-xs" title={row.run_id}>
          {truncateId(row.run_id)}
        </span>
      ),
    },
    {
      key: 'user',
      header: rt.colUser,
      render: (row) => (
        <div className="flex flex-col">
          <span className="text-xs">{row.created_by_user_name ?? '--'}</span>
          {row.created_by_email && (
            <span className="text-xs text-[var(--c-text-muted)]">{row.created_by_email}</span>
          )}
        </div>
      ),
    },
    {
      key: 'project_id',
      header: rt.colProject,
      render: (row) => (
        <span className="font-mono text-xs" title={row.account_id}>
          {truncateId(row.account_id)}
        </span>
      ),
    },
    {
      key: 'thread_id',
      header: rt.colThread,
      render: (row) => (
        <span className="font-mono text-xs" title={row.thread_id}>
          {truncateId(row.thread_id)}
        </span>
      ),
    },
    {
      key: 'status',
      header: rt.colStatus,
      render: (row) => (
        <Badge variant={statusVariant(row.status)}>{row.status}</Badge>
      ),
    },
    {
      key: 'model',
      header: rt.colModel,
      render: (row) => (
        <span className="inline-block max-w-[120px] truncate text-xs" title={row.model ?? undefined}>
          {row.model ?? '--'}
        </span>
      ),
    },
    {
      key: 'persona_id',
      header: rt.colPersona,
      render: (row) => (
        <span className="inline-block max-w-[140px] truncate text-xs" title={row.persona_id ?? undefined}>
          {row.persona_id ?? '--'}
        </span>
      ),
    },
    {
      key: 'duration',
      header: rt.colDuration,
      render: (row) => (
        <span className="text-xs tabular-nums">{formatDuration(row.duration_ms)}</span>
      ),
    },
    {
      key: 'tokens',
      header: rt.colTokens,
      render: (row) => {
        const inp = row.total_input_tokens
        const out = row.total_output_tokens
        if (inp == null && out == null) return <span className="text-xs">--</span>
        return (
          <span className="text-xs tabular-nums">
            {inp ?? 0} / {out ?? 0}
          </span>
        )
      },
    },
    {
      key: 'cost',
      header: rt.colCost,
      render: (row) => (
        <span className="text-xs tabular-nums">{formatCost(row.total_cost_usd)}</span>
      ),
    },
    {
      key: 'cache_hit_rate',
      header: rt.colCacheHit,
      render: (row) =>
        row.cache_hit_rate != null ? (
          <span className="text-xs tabular-nums text-[var(--c-status-success-text)]">
            {(row.cache_hit_rate * 100).toFixed(0)}%
          </span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'credits_used',
      header: rt.colCredits,
      render: (row) =>
        row.credits_used != null ? (
          <span className="text-xs tabular-nums">{row.credits_used}</span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'created_at',
      header: rt.colCreatedAt,
      render: (row) => (
        <span className="text-xs tabular-nums">
          {new Date(row.created_at).toLocaleString()}
        </span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row): ReactNode => {
        if (row.status !== 'running') return null
        return (
          <button
            onClick={(e) => { e.stopPropagation(); setCancelTarget(row) }}
            className="rounded px-2 py-0.5 text-xs text-[var(--c-text-muted)] transition-colors hover:bg-red-100 hover:text-red-600 dark:hover:bg-red-900/30 dark:hover:text-red-400"
          >
            {rt.cancel}
          </button>
        )
      },
    },
  ]

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1
  const activeFilterCount = useMemo(() => countActiveFilters(appliedFilters), [appliedFilters])

  const actions = (
    <>
      <span className="rounded-md border border-[var(--c-border)] px-2 py-1 text-xs text-[var(--c-text-muted)]">
        {rt.filterActiveCount(activeFilterCount)}
      </span>
      <button
        onClick={handleRefresh}
        disabled={loading}
        className="flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
      >
        <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
        {rt.refresh}
      </button>
    </>
  )

  const filterInputCls = 'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={rt.title} actions={actions} />

      <div className="border-b border-[var(--c-border-console)] px-6 py-3">
        <div className="grid grid-cols-1 items-start gap-2 md:grid-cols-2 xl:grid-cols-5">
          <input
            type="text"
            placeholder={rt.filterRunPlaceholder}
            value={draftFilters.runId}
            onChange={(e) => updateDraftFilter('runId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={rt.filterThreadPlaceholder}
            value={draftFilters.threadId}
            onChange={(e) => updateDraftFilter('threadId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={rt.filterUserPlaceholder}
            value={draftFilters.userId}
            onChange={(e) => updateDraftFilter('userId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={rt.filterProjectPlaceholder}
            value={draftFilters.projectId}
            onChange={(e) => updateDraftFilter('projectId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={rt.filterParentRunPlaceholder}
            value={draftFilters.parentRunId}
            onChange={(e) => updateDraftFilter('parentRunId', e.target.value)}
            className={filterInputCls}
          />
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{rt.filterStatusLabel}</span>
            <select
              value={draftFilters.status}
              onChange={(e) => updateDraftFilter('status', e.target.value)}
              className={filterInputCls}
            >
              {statusOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{rt.filterModelLabel}</span>
            <input
              type="text"
              placeholder={rt.filterModelPlaceholder}
              value={draftFilters.model}
              onChange={(e) => updateDraftFilter('model', e.target.value)}
              className={filterInputCls}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{rt.filterPersonaLabel}</span>
            <input
              type="text"
              placeholder={rt.filterPersonaPlaceholder}
              value={draftFilters.personaId}
              onChange={(e) => updateDraftFilter('personaId', e.target.value)}
              className={filterInputCls}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{rt.filterSinceLabel}</span>
            <input
              type="datetime-local"
              value={draftFilters.since}
              onChange={(e) => updateDraftFilter('since', e.target.value)}
              className={filterInputCls}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{rt.filterUntilLabel}</span>
            <input
              type="datetime-local"
              value={draftFilters.until}
              onChange={(e) => updateDraftFilter('until', e.target.value)}
              className={filterInputCls}
            />
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button
            onClick={handleApplyFilters}
            className="rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            {rt.applyFilters}
          </button>
          <button
            onClick={handleResetFilters}
            className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            {rt.resetFilters}
          </button>
        </div>
      </div>

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={runs}
          rowKey={(row) => row.run_id}
          loading={loading}
          emptyMessage={rt.empty}
          emptyIcon={<Play size={28} />}
          onRowClick={setSelectedRun}
          activeRowKey={selectedRun?.run_id}
        />
      </div>

      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t border-[var(--c-border-console)] px-4 py-2">
          <span className="text-xs text-[var(--c-text-muted)]">
            {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
          </span>
          <div className="flex gap-2">
            <button
              onClick={() => setOffset((p) => Math.max(0, p - PAGE_SIZE))}
              disabled={currentPage <= 1}
              className="rounded border border-[var(--c-border)] px-2.5 py-1 text-xs text-[var(--c-text-secondary)] disabled:opacity-40 hover:bg-[var(--c-bg-sub)]"
            >
              {rt.prev}
            </button>
            <span className="flex items-center text-xs text-[var(--c-text-muted)]">
              {currentPage} / {totalPages}
            </span>
            <button
              onClick={() => setOffset((p) => p + PAGE_SIZE)}
              disabled={currentPage >= totalPages}
              className="rounded border border-[var(--c-border)] px-2.5 py-1 text-xs text-[var(--c-text-secondary)] disabled:opacity-40 hover:bg-[var(--c-bg-sub)]"
            >
              {rt.next}
            </button>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={cancelTarget !== null}
        onClose={() => setCancelTarget(null)}
        onConfirm={() => void handleCancelConfirm()}
        title={rt.cancelTitle}
        message={rt.cancelMessage(cancelTarget ? truncateId(cancelTarget.run_id) : '')}
        confirmLabel={rt.cancelConfirm}
        loading={cancelling}
      />

      <RunDetailPanel
        run={selectedRun}
        accessToken={accessToken}
        onClose={() => setSelectedRun(null)}
      />
    </div>
  )
}
