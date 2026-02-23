import { useState, useCallback, useEffect, type ReactNode } from 'react'
import { useOutletContext } from 'react-router-dom'
import { RefreshCw, Play } from 'lucide-react'
import type { ConsoleOutletContext } from '../layouts/ConsoleLayout'
import { PageHeader } from '../components/PageHeader'
import { DataTable, type Column } from '../components/DataTable'
import { Badge, type BadgeVariant } from '../components/Badge'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '../components/useToast'
import { listRuns, cancelRun, type GlobalRun } from '../api/runs'

const PAGE_SIZE = 50

const STATUS_OPTIONS = [
  { value: '', label: 'All' },
  { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'cancelled', label: 'Cancelled' },
]

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
  return `$${usd.toFixed(4)}`
}

function truncateId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

export function RunsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()

  const [runs, setRuns] = useState<GlobalRun[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [offset, setOffset] = useState(0)
  const [cancelTarget, setCancelTarget] = useState<GlobalRun | null>(null)
  const [cancelling, setCancelling] = useState(false)

  const fetchRuns = useCallback(
    async (status: string, currentOffset: number) => {
      setLoading(true)
      try {
        const resp = await listRuns(
          { status: status || undefined, limit: PAGE_SIZE, offset: currentOffset },
          accessToken,
        )
        setRuns(resp.data)
        setTotal(resp.total)
      } catch {
        addToast('Failed to load runs', 'error')
      } finally {
        setLoading(false)
      }
    },
    [accessToken, addToast],
  )

  useEffect(() => {
    void fetchRuns(statusFilter, offset)
  }, [fetchRuns, statusFilter, offset])

  const handleStatusChange = useCallback((value: string) => {
    setStatusFilter(value)
    setOffset(0)
  }, [])

  const handleRefresh = useCallback(() => {
    void fetchRuns(statusFilter, offset)
  }, [fetchRuns, statusFilter, offset])

  const handleCancelConfirm = useCallback(async () => {
    if (!cancelTarget) return
    setCancelling(true)
    try {
      await cancelRun(cancelTarget.run_id, accessToken)
      setCancelTarget(null)
      void fetchRuns(statusFilter, offset)
    } catch {
      addToast('Failed to cancel run', 'error')
    } finally {
      setCancelling(false)
    }
  }, [cancelTarget, accessToken, fetchRuns, statusFilter, offset, addToast])

  const columns: Column<GlobalRun>[] = [
    {
      key: 'run_id',
      header: 'ID',
      render: (row) => (
        <span className="font-mono text-xs" title={row.run_id}>
          {truncateId(row.run_id)}
        </span>
      ),
    },
    {
      key: 'thread_id',
      header: 'Thread',
      render: (row) => (
        <span className="font-mono text-xs" title={row.thread_id}>
          {truncateId(row.thread_id)}
        </span>
      ),
    },
    {
      key: 'status',
      header: 'Status',
      render: (row) => (
        <Badge variant={statusVariant(row.status)}>{row.status}</Badge>
      ),
    },
    {
      key: 'model',
      header: 'Model',
      render: (row) => (
        <span className="text-xs">{row.model ?? '--'}</span>
      ),
    },
    {
      key: 'duration',
      header: 'Duration',
      render: (row) => (
        <span className="text-xs tabular-nums">{formatDuration(row.duration_ms)}</span>
      ),
    },
    {
      key: 'tokens',
      header: 'Tokens (in/out)',
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
      header: 'Cost',
      render: (row) => (
        <span className="text-xs tabular-nums">{formatCost(row.total_cost_usd)}</span>
      ),
    },
    {
      key: 'created_at',
      header: 'Created At',
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
            onClick={() => setCancelTarget(row)}
            className="rounded px-2 py-0.5 text-xs text-[var(--c-text-muted)] transition-colors hover:bg-red-100 hover:text-red-600 dark:hover:bg-red-900/30 dark:hover:text-red-400"
          >
            Cancel
          </button>
        )
      },
    },
  ]

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const actions = (
    <>
      <select
        value={statusFilter}
        onChange={(e) => handleStatusChange(e.target.value)}
        className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none"
      >
        {STATUS_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      <button
        onClick={handleRefresh}
        disabled={loading}
        className="flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
      >
        <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
        Refresh
      </button>
    </>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title="Runs" actions={actions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={runs}
          rowKey={(row) => row.run_id}
          loading={loading}
          emptyMessage="No runs found"
          emptyIcon={<Play size={28} />}
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
              Prev
            </button>
            <span className="flex items-center text-xs text-[var(--c-text-muted)]">
              {currentPage} / {totalPages}
            </span>
            <button
              onClick={() => setOffset((p) => p + PAGE_SIZE)}
              disabled={currentPage >= totalPages}
              className="rounded border border-[var(--c-border)] px-2.5 py-1 text-xs text-[var(--c-text-secondary)] disabled:opacity-40 hover:bg-[var(--c-bg-sub)]"
            >
              Next
            </button>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={cancelTarget !== null}
        onClose={() => setCancelTarget(null)}
        onConfirm={() => void handleCancelConfirm()}
        title="Cancel Run"
        message={`Cancel run ${cancelTarget ? truncateId(cancelTarget.run_id) : ''}?`}
        confirmLabel="Cancel Run"
        loading={cancelling}
      />
    </div>
  )
}
