import { useCallback, useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { listRuns, type Run } from '../../api'
import { RunDetailPanel } from '../RunDetailPanel'
import { useLocale } from '../../contexts/LocaleContext'
import { formatDateTime } from '@arkloop/shared'

type Props = {
  accessToken: string
}

const PAGE_SIZE = 50

function truncateId(value: string): string {
  return value.length > 8 ? `${value.slice(0, 8)}…` : value
}

function statusColor(status: string): string {
  switch (status) {
    case 'running': return 'var(--c-status-warning-text)'
    case 'completed': return 'var(--c-status-success-text)'
    case 'failed': return 'var(--c-status-error-text)'
    case 'interrupted': return 'var(--c-status-error-text)'
    default: return 'var(--c-text-muted)'
  }
}

function formatRelativeTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000)
  const abs = Math.abs(diffSeconds)
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  if (abs < 60) return rtf.format(diffSeconds, 'second')
  const diffMinutes = Math.round(diffSeconds / 60)
  if (Math.abs(diffMinutes) < 60) return rtf.format(diffMinutes, 'minute')
  const diffHours = Math.round(diffMinutes / 60)
  if (Math.abs(diffHours) < 24) return rtf.format(diffHours, 'hour')
  const diffDays = Math.round(diffHours / 24)
  if (Math.abs(diffDays) < 30) return rtf.format(diffDays, 'day')
  return rtf.format(Math.round(diffDays / 30), 'month')
}

function formatAbsoluteTime(value: string): string {
  return formatDateTime(value, { includeZone: false })
}

export function RunsSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings

  const [runs, setRuns] = useState<Run[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)

  const loadPage = useCallback(async (currentOffset: number) => {
    setLoading(true)
    setError(null)
    try {
      const resp = await listRuns(accessToken, { limit: PAGE_SIZE, offset: currentOffset })
      setRuns(resp.data)
      setTotal(resp.total)
    } catch {
      setError(ds.runsHistoryLoadError)
    } finally {
      setLoading(false)
    }
  }, [accessToken, ds.runsHistoryLoadError])

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => { void loadPage(offset) })
    return () => window.cancelAnimationFrame(frame)
  }, [loadPage, offset])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
            {ds.runsHistory}
          </h3>
          <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
            {ds.runsHistoryDesc}
          </p>
        </div>
        <button
          onClick={() => void loadPage(offset)}
          disabled={loading}
          className="flex shrink-0 items-center gap-1.5 rounded-lg bg-[var(--c-bg-deep)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:text-[var(--c-text-primary)] disabled:opacity-50"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <RefreshCw size={12} className={loading ? 'animate-spin' : ''} />
          {ds.runsHistoryRefresh}
        </button>
      </div>

      {error && (
        <div className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep)] p-3 text-xs text-[var(--c-status-error-text)]">
          {error}
        </div>
      )}

      <div
        className="overflow-hidden rounded-xl"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <table className="w-full text-xs">
          <thead>
            <tr style={{ borderBottom: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-deep)' }}>
              <th className="px-3 py-2 text-left font-medium text-[var(--c-text-muted)]">{ds.runsHistoryColId}</th>
              <th className="px-3 py-2 text-left font-medium text-[var(--c-text-muted)]">{ds.runsHistoryColModel}</th>
              <th className="px-3 py-2 text-left font-medium text-[var(--c-text-muted)]">{ds.runsHistoryColStatus}</th>
              <th className="px-3 py-2 text-left font-medium text-[var(--c-text-muted)] tabular-nums">{ds.runsHistoryColTokens}</th>
              <th className="px-3 py-2 text-left font-medium text-[var(--c-text-muted)]">{ds.runsHistoryColTime}</th>
            </tr>
          </thead>
          <tbody>
            {loading && runs.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-[var(--c-text-muted)]">
                  {ds.runsHistoryLoading}
                </td>
              </tr>
            ) : runs.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-[var(--c-text-muted)]">
                  {ds.runsHistoryEmpty}
                </td>
              </tr>
            ) : (
              runs.map((run) => (
                <tr
                  key={run.run_id}
                  onClick={() => setSelectedRunId(run.run_id)}
                  className="cursor-pointer transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{
                    borderTop: '0.5px solid var(--c-border-subtle)',
                    background: selectedRunId === run.run_id ? 'var(--c-bg-deep)' : undefined,
                  }}
                >
                  <td className="px-3 py-2 font-mono text-[11px] text-[var(--c-text-muted)]" title={run.run_id}>
                    {truncateId(run.run_id)}
                  </td>
                  <td className="max-w-[140px] px-3 py-2 text-[var(--c-text-secondary)]">
                    <span className="block truncate" title={run.model}>{run.model ?? '—'}</span>
                  </td>
                  <td className="px-3 py-2">
                    <span className="font-medium" style={{ color: statusColor(run.status) }}>
                      {run.status}
                    </span>
                  </td>
                  <td className="px-3 py-2 tabular-nums text-[var(--c-text-muted)]">
                    {run.total_input_tokens != null || run.total_output_tokens != null
                      ? `${run.total_input_tokens ?? 0} / ${run.total_output_tokens ?? 0}`
                      : '—'}
                  </td>
                  <td className="px-3 py-2 text-[var(--c-text-muted)]" title={formatAbsoluteTime(run.created_at)}>
                    {formatRelativeTime(run.created_at)}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>

        {total > PAGE_SIZE && (
          <div
            className="flex items-center justify-between px-3 py-2"
            style={{ borderTop: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-deep)' }}
          >
            <span className="text-[11px] text-[var(--c-text-muted)]">
              {currentPage} / {totalPages}
            </span>
            <div className="flex items-center gap-1.5">
              <button
                onClick={() => setOffset((prev) => Math.max(0, prev - PAGE_SIZE))}
                disabled={loading || currentPage <= 1}
                className="rounded border px-2 py-0.5 text-[11px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40"
                style={{ borderColor: 'var(--c-border-subtle)' }}
              >
                {ds.runsHistoryPrev}
              </button>
              <button
                onClick={() => setOffset((prev) => prev + PAGE_SIZE)}
                disabled={loading || currentPage >= totalPages}
                className="rounded border px-2 py-0.5 text-[11px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40"
                style={{ borderColor: 'var(--c-border-subtle)' }}
              >
                {ds.runsHistoryNext}
              </button>
            </div>
          </div>
        )}
      </div>

      {selectedRunId && (
        <RunDetailPanel
          runId={selectedRunId}
          accessToken={accessToken}
          onClose={() => setSelectedRunId(null)}
        />
      )}
    </div>
  )
}
