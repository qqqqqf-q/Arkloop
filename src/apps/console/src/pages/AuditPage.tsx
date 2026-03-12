import { useState, useCallback, useEffect, Fragment } from 'react'
import { useOutletContext } from 'react-router-dom'
import { RefreshCw, ClipboardList, ChevronDown, ChevronRight } from 'lucide-react'
import type { ConsoleOutletContext } from '../layouts/ConsoleLayout'
import { PageHeader } from '../components/PageHeader'
import { EmptyState } from '../components/EmptyState'
import { useToast } from '@arkloop/shared'
import { listAuditLogs, type AuditLog } from '../api/audit'

const PAGE_SIZE = 50

// 已知的 action 类型，来自后端 audit writer
const ACTION_OPTIONS = [
  { value: '', label: 'All' },
  { value: 'auth.login', label: 'auth.login' },
  { value: 'auth.logout', label: 'auth.logout' },
  { value: 'auth.refresh', label: 'auth.refresh' },
  { value: 'auth.register', label: 'auth.register' },
  { value: 'runs.cancel', label: 'runs.cancel' },
  { value: 'threads.delete', label: 'threads.delete' },
  { value: 'api_keys.create', label: 'api_keys.create' },
  { value: 'api_keys.revoke', label: 'api_keys.revoke' },
  { value: 'org_invitations.create', label: 'org_invitations.create' },
  { value: 'org_invitations.accept', label: 'org_invitations.accept' },
  { value: 'org_invitations.revoke', label: 'org_invitations.revoke' },
]

function truncateId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

// datetime-local input value → RFC3339
function toRFC3339(localValue: string): string | undefined {
  if (!localValue) return undefined
  return new Date(localValue).toISOString()
}

type ExpandedRowProps = {
  log: AuditLog
}

function ExpandedRow({ log }: ExpandedRowProps) {
  const colCount = 7
  return (
    <tr className="bg-[var(--c-bg-deep2)]">
      <td colSpan={colCount} className="px-6 py-3">
        <pre className="overflow-auto rounded-md bg-[var(--c-bg-deep3,var(--c-bg-tag))] p-3 text-xs leading-relaxed text-[var(--c-text-secondary)]">
          {JSON.stringify(log.metadata, null, 2)}
        </pre>
      </td>
    </tr>
  )
}

export function AuditPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()

  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [actionFilter, setActionFilter] = useState('')
  const [sinceValue, setSinceValue] = useState('')
  const [untilValue, setUntilValue] = useState('')
  const [offset, setOffset] = useState(0)
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())

  const fetchLogs = useCallback(
    async (action: string, since: string, until: string, currentOffset: number) => {
      setLoading(true)
      try {
        const resp = await listAuditLogs(
          {
            action: action || undefined,
            since: toRFC3339(since),
            until: toRFC3339(until),
            limit: PAGE_SIZE,
            offset: currentOffset,
          },
          accessToken,
        )
        setLogs(resp.data)
        setTotal(resp.total)
      } catch {
        addToast('Failed to load audit logs', 'error')
      } finally {
        setLoading(false)
      }
    },
    [accessToken, addToast],
  )

  useEffect(() => {
    void fetchLogs(actionFilter, sinceValue, untilValue, offset)
  }, [fetchLogs, actionFilter, sinceValue, untilValue, offset])

  const handleActionChange = useCallback((value: string) => {
    setActionFilter(value)
    setOffset(0)
  }, [])

  const handleSinceChange = useCallback((value: string) => {
    setSinceValue(value)
    setOffset(0)
  }, [])

  const handleUntilChange = useCallback((value: string) => {
    setUntilValue(value)
    setOffset(0)
  }, [])

  const handleRefresh = useCallback(() => {
    void fetchLogs(actionFilter, sinceValue, untilValue, offset)
  }, [fetchLogs, actionFilter, sinceValue, untilValue, offset])

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }, [])

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const actions = (
    <>
      <select
        value={actionFilter}
        onChange={(e) => handleActionChange(e.target.value)}
        className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none"
      >
        {ACTION_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      <input
        type="datetime-local"
        value={sinceValue}
        onChange={(e) => handleSinceChange(e.target.value)}
        className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none"
        title="Since"
      />
      <span className="text-xs text-[var(--c-text-muted)]">–</span>
      <input
        type="datetime-local"
        value={untilValue}
        onChange={(e) => handleUntilChange(e.target.value)}
        className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none"
        title="Until"
      />
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
      <PageHeader title="Audit Logs" actions={actions} />

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : logs.length === 0 ? (
          <EmptyState icon={<ClipboardList size={28} />} message="No audit logs found" />
        ) : (
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-[var(--c-border-console)]">
                <th className="w-6 px-3 py-2.5" />
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Action</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Actor</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Target</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">IP</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Trace ID</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Created At</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log) => {
                const expanded = expandedIds.has(log.id)
                const hasMetadata = Object.keys(log.metadata).length > 0
                return (
                  <Fragment key={log.id}>
                    <tr
                      onClick={() => hasMetadata && toggleExpand(log.id)}
                      className={[
                        'border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]',
                        hasMetadata ? 'cursor-pointer' : '',
                      ].join(' ')}
                    >
                      <td className="w-6 px-3 py-2.5 text-[var(--c-text-muted)]">
                        {hasMetadata && (
                          expanded
                            ? <ChevronDown size={13} />
                            : <ChevronRight size={13} />
                        )}
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                        <span className="font-mono text-xs">{log.action}</span>
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                        {log.actor_user_id ? (
                          <span className="font-mono text-xs" title={log.actor_user_id}>
                            {truncateId(log.actor_user_id)}
                          </span>
                        ) : (
                          <span className="text-xs text-[var(--c-text-muted)]">--</span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                        {log.target_type || log.target_id ? (
                          <span className="text-xs">
                            {log.target_type ?? ''}
                            {log.target_type && log.target_id && '/'}
                            {log.target_id ? (
                              <span title={log.target_id}>{truncateId(log.target_id)}</span>
                            ) : null}
                          </span>
                        ) : (
                          <span className="text-xs text-[var(--c-text-muted)]">--</span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                        <span className="text-xs">{log.ip_address ?? '--'}</span>
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                        <span className="font-mono text-xs" title={log.trace_id}>
                          {truncateId(log.trace_id)}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                        <span className="text-xs tabular-nums">
                          {new Date(log.created_at).toLocaleString()}
                        </span>
                      </td>
                    </tr>
                    {expanded && <ExpandedRow log={log} />}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        )}
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
    </div>
  )
}
