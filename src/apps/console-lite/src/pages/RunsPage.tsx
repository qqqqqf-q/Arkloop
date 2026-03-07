import { useCallback, useEffect, useMemo, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Play, RefreshCw } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import type { LiteAgent } from '../api/agents'
import { listLiteAgents } from '../api/agents'
import { isApiError } from '../api'
import { listRuns, type GlobalRun } from '../api/runs'
import { Badge, type BadgeVariant } from '../components/Badge'
import { DataTable, type Column } from '../components/DataTable'
import { PageHeader } from '../components/PageHeader'
import { RunDetailPanel } from '../components/RunDetailPanel'
import { useToast } from '../components/useToast'
import { useLocale } from '../contexts/LocaleContext'

const PAGE_SIZE = 50

type AgentNameMap = Record<string, string>

function buildAgentNameMap(agents: LiteAgent[]): AgentNameMap {
  return agents.reduce<AgentNameMap>((acc, agent) => {
    acc[agent.persona_key] = agent.display_name
    acc[agent.id] = agent.display_name
    return acc
  }, {})
}

function truncateId(value: string): string {
  return value.length > 8 ? `${value.slice(0, 8)}…` : value
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

function formatAbsoluteTime(value: string, locale: 'zh' | 'en'): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(locale === 'zh' ? 'zh-CN' : 'en')
}

function formatRelativeTime(value: string, locale: 'zh' | 'en', fallback: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return fallback

  const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000)
  const absSeconds = Math.abs(diffSeconds)
  const rtf = new Intl.RelativeTimeFormat(locale === 'zh' ? 'zh-CN' : 'en', { numeric: 'auto' })

  if (absSeconds < 60) return rtf.format(diffSeconds, 'second')
  const diffMinutes = Math.round(diffSeconds / 60)
  if (Math.abs(diffMinutes) < 60) return rtf.format(diffMinutes, 'minute')
  const diffHours = Math.round(diffMinutes / 60)
  if (Math.abs(diffHours) < 24) return rtf.format(diffHours, 'hour')
  const diffDays = Math.round(diffHours / 24)
  if (Math.abs(diffDays) < 30) return rtf.format(diffDays, 'day')
  const diffMonths = Math.round(diffDays / 30)
  if (Math.abs(diffMonths) < 12) return rtf.format(diffMonths, 'month')
  return rtf.format(Math.round(diffMonths / 12), 'year')
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

function resolveAgentName(personaId: string | undefined, agentNames: AgentNameMap, fallback: string): string {
  if (!personaId) return fallback
  return agentNames[personaId] ?? personaId
}

function resolveUserEmail(run: GlobalRun, fallback: string): string {
  return run.created_by_email ?? fallback
}

function statusLabel(status: string, runsText: ReturnType<typeof useLocale>['t']['runs']): string {
  switch (status) {
    case 'running':
      return runsText.statusRunning
    case 'completed':
      return runsText.statusCompleted
    case 'failed':
      return runsText.statusFailed
    case 'cancelled':
      return runsText.statusCancelled
    default:
      return runsText.statusUnknown
  }
}

export function RunsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { locale, t } = useLocale()
  const tr = t.runs

  const [runs, setRuns] = useState<GlobalRun[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(false)
  const [selectedRun, setSelectedRun] = useState<GlobalRun | null>(null)
  const [agentNames, setAgentNames] = useState<AgentNameMap>({})
  const [agentsLoaded, setAgentsLoaded] = useState(false)

  const loadPage = useCallback(async (currentOffset: number) => {
    setLoading(true)

    const [runsResult, agentsResult] = await Promise.allSettled([
      listRuns({ limit: PAGE_SIZE, offset: currentOffset }, accessToken),
      agentsLoaded ? Promise.resolve<LiteAgent[] | null>(null) : listLiteAgents(accessToken),
    ])

    if (agentsResult.status === 'fulfilled' && agentsResult.value) {
      setAgentNames(buildAgentNameMap(agentsResult.value))
      setAgentsLoaded(true)
    }

    if (runsResult.status === 'rejected') {
      addToast(isApiError(runsResult.reason) ? runsResult.reason.message : tr.toastLoadFailed, 'error')
      setLoading(false)
      return
    }

    const response = runsResult.value
    if (response.data.length === 0 && response.total > 0 && currentOffset >= response.total) {
      setOffset(Math.max(0, Math.floor((response.total - 1) / PAGE_SIZE) * PAGE_SIZE))
      setLoading(false)
      return
    }

    setRuns(response.data)
    setTotal(response.total)
    setSelectedRun((prev) => {
      if (!prev) return null
      return response.data.find((item) => item.run_id === prev.run_id) ?? null
    })
    setLoading(false)
  }, [accessToken, addToast, agentsLoaded, tr.toastLoadFailed])

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      void loadPage(offset)
    })
    return () => window.cancelAnimationFrame(frame)
  }, [loadPage, offset])

  const compactHeaderClassName = 'px-3 py-2 text-[11px]'
  const compactBodyClassName = 'px-3 py-2 text-xs leading-4'

  const columns = useMemo<Column<GlobalRun>[]>(() => [
    {
      key: 'run_id',
      header: tr.colId,
      cellClassName: 'whitespace-nowrap font-mono text-[11px]',
      render: (row) => <span title={row.run_id}>{truncateId(row.run_id)}</span>,
    },
    {
      key: 'agent',
      header: tr.colAgent,
      cellClassName: 'max-w-[120px]',
      render: (row) => {
        const value = resolveAgentName(row.persona_id, agentNames, tr.unknownAgent)
        return <span className="block truncate" title={value}>{value}</span>
      },
    },
    {
      key: 'user',
      header: tr.colUser,
      cellClassName: 'max-w-[160px]',
      render: (row) => {
        const value = resolveUserEmail(row, tr.emptyValue)
        return <span className="block truncate" title={value}>{value}</span>
      },
    },
    {
      key: 'model',
      header: tr.colModel,
      cellClassName: 'max-w-[180px]',
      render: (row) => {
        const value = row.model ?? tr.emptyValue
        return <span className="block truncate" title={value}>{value}</span>
      },
    },
    {
      key: 'status',
      header: tr.colStatus,
      render: (row) => (
        <Badge variant={statusVariant(row.status)}>{statusLabel(row.status, tr)}</Badge>
      ),
    },
    {
      key: 'tokens',
      header: tr.colTokens,
      cellClassName: 'whitespace-nowrap tabular-nums',
      render: (row) => formatTokenPair(row.total_input_tokens, row.total_output_tokens, tr.emptyValue),
    },
    {
      key: 'cache_hit_rate',
      header: tr.colCacheHit,
      cellClassName: 'whitespace-nowrap tabular-nums',
      render: (row) => formatPercent(row.cache_hit_rate, tr.emptyValue),
    },
    {
      key: 'credits_used',
      header: tr.colCredits,
      cellClassName: 'whitespace-nowrap tabular-nums',
      render: (row) => formatCount(row.credits_used, tr.emptyValue),
    },
    {
      key: 'created_at',
      header: tr.colTime,
      cellClassName: 'whitespace-nowrap',
      render: (row) => (
        <span title={formatAbsoluteTime(row.created_at, locale)}>
          {formatRelativeTime(row.created_at, locale, tr.emptyValue)}
        </span>
      ),
    },
    {
      key: 'actions',
      header: tr.colActions,
      cellClassName: 'whitespace-nowrap',
      render: (row) => (
        <button
          onClick={(event) => {
            event.stopPropagation()
            setSelectedRun(row)
          }}
          className="rounded border border-[var(--c-border)] px-2 py-0.5 text-[11px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-tag)]"
        >
          {tr.view}
        </button>
      ),
    },
  ], [agentNames, locale, tr])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1
  const selectedAgentName = selectedRun
    ? resolveAgentName(selectedRun.persona_id, agentNames, tr.unknownAgent)
    : undefined

  const actions = (
    <button
      onClick={() => void loadPage(offset)}
      disabled={loading}
      className="flex items-center gap-1 rounded-lg bg-[var(--c-bg-tag)] px-2.5 py-1 text-[11px] font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
    >
      <RefreshCw size={12} className={loading ? 'animate-spin' : ''} />
      {tr.refresh}
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tr.title} actions={actions} />

      <div className="flex flex-1 flex-col overflow-hidden">
        <div className="flex flex-1 flex-col overflow-auto">
          <DataTable
            columns={columns}
            data={runs}
            rowKey={(row) => row.run_id}
            loading={loading}
            loadingLabel={t.common.loading}
            emptyMessage={tr.empty}
            emptyIcon={<Play size={24} />}
            onRowClick={setSelectedRun}
            activeRowKey={selectedRun?.run_id}
            tableClassName="min-w-[1160px] text-xs"
            headerCellClassName={compactHeaderClassName}
            bodyCellClassName={compactBodyClassName}
          />
        </div>

        {total > 0 && (
          <div className="flex items-center justify-between border-t border-[var(--c-border-console)] px-4 py-2">
            <span className="text-[11px] text-[var(--c-text-muted)]">
              {tr.pageSummary(offset + 1, Math.min(offset + PAGE_SIZE, total), total)}
            </span>
            <div className="flex items-center gap-1.5">
              <button
                onClick={() => setOffset((prev) => Math.max(0, prev - PAGE_SIZE))}
                disabled={loading || currentPage <= 1}
                className="rounded border border-[var(--c-border)] px-2 py-0.5 text-[11px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40"
              >
                {tr.prev}
              </button>
              <span className="text-[11px] text-[var(--c-text-muted)]">
                {currentPage} / {totalPages}
              </span>
              <button
                onClick={() => setOffset((prev) => prev + PAGE_SIZE)}
                disabled={loading || currentPage >= totalPages}
                className="rounded border border-[var(--c-border)] px-2 py-0.5 text-[11px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40"
              >
                {tr.next}
              </button>
            </div>
          </div>
        )}
      </div>

      <RunDetailPanel
        key={selectedRun?.run_id ?? 'none'}
        run={selectedRun}
        agentName={selectedAgentName}
        accessToken={accessToken}
        onClose={() => setSelectedRun(null)}
      />
    </div>
  )
}
