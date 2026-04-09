import { useState, useCallback, useEffect, useRef } from 'react'
import { useOutletContext } from 'react-router-dom'
import { RefreshCw, Shield, Search, X } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { EmptyState } from '../../components/EmptyState'
import { formatDateTime, useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { listAccessLog, type AccessLogEntry, type AccessLogParams } from '../../api/access-log'

const PAGE_SIZE = 50

const METHOD_OPTIONS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']
const RISK_OPTIONS = [20, 50, 80]

type AppliedFilters = {
  method: string
  path: string
  ip: string
  riskMin: number
}

const emptyFilters: AppliedFilters = { method: '', path: '', ip: '', riskMin: 0 }

function hasActiveFilters(f: AppliedFilters): boolean {
  return f.method !== '' || f.path !== '' || f.ip !== '' || f.riskMin > 0
}

function riskBadge(score: number, t: { riskLow: string; riskMedium: string; riskHigh: string; riskCritical: string }) {
  if (score >= 80) {
    return <span className="rounded-full bg-red-500/15 px-2 py-0.5 text-[10px] font-medium text-red-400">{score} {t.riskCritical}</span>
  }
  if (score >= 50) {
    return <span className="rounded-full bg-orange-500/15 px-2 py-0.5 text-[10px] font-medium text-orange-400">{score} {t.riskHigh}</span>
  }
  if (score >= 20) {
    return <span className="rounded-full bg-yellow-500/15 px-2 py-0.5 text-[10px] font-medium text-yellow-400">{score} {t.riskMedium}</span>
  }
  return <span className="rounded-full bg-green-500/15 px-2 py-0.5 text-[10px] font-medium text-green-400">{score} {t.riskLow}</span>
}

function identityLabel(entry: AccessLogEntry, anonLabel: string) {
  if (entry.identity_type === 'jwt') {
    const label = entry.username || entry.user_id?.slice(0, 8) || '--'
    return (
      <span className="font-mono text-xs" title={`user: ${entry.user_id}\naccount: ${entry.account_id}`}>
        {label}
      </span>
    )
  }
  if (entry.identity_type === 'api_key') {
    return (
      <span className="text-xs" title={`account: ${entry.account_id}`}>
        API Key / {entry.account_id?.slice(0, 8) || '--'}
      </span>
    )
  }
  return <span className="text-xs text-[var(--c-text-muted)]">{anonLabel}</span>
}

function locationLabel(entry: AccessLogEntry) {
  const parts = [entry.city, entry.country].filter(Boolean)
  return parts.length > 0 ? parts.join(', ') : '--'
}

function statusColor(code: number): string {
  if (code >= 500) return 'text-red-400'
  if (code >= 400) return 'text-orange-400'
  if (code >= 300) return 'text-yellow-400'
  return 'text-green-400'
}

const selectClass = 'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none'
const inputClass = 'w-28 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none'

export function AccessLogPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()

  const [entries, setEntries] = useState<AccessLogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [nextBefore, setNextBefore] = useState<string | undefined>()

  // 草稿状态：用户编辑中，未提交
  const [draft, setDraft] = useState<AppliedFilters>(emptyFilters)
  // 已应用状态：触发实际请求
  const [applied, setApplied] = useState<AppliedFilters>(emptyFilters)

  const p = t.pages.accessLog

  const fetchWithFilters = useCallback(
    async (filters: AppliedFilters, before?: string, append = false) => {
      setLoading(true)
      try {
        const params: AccessLogParams = { limit: PAGE_SIZE }
        if (before) params.before = before
        if (filters.method) params.method = filters.method
        if (filters.path) params.path = filters.path
        if (filters.ip) params.ip = filters.ip
        if (filters.riskMin > 0) params.risk_min = filters.riskMin

        const resp = await listAccessLog(params, accessToken)
        if (append) {
          setEntries((prev) => [...prev, ...resp.data])
        } else {
          setEntries(resp.data)
        }
        setHasMore(resp.has_more)
        setNextBefore(resp.next_before)
      } catch {
        addToast(p.toastLoadFailed, 'error')
      } finally {
        setLoading(false)
      }
    },
    [accessToken, addToast, p.toastLoadFailed],
  )

  // 初始加载
  const initialLoadDone = useRef(false)
  useEffect(() => {
    if (!initialLoadDone.current) {
      initialLoadDone.current = true
      void fetchWithFilters(emptyFilters)
    }
  }, [fetchWithFilters])

  const handleApply = useCallback(() => {
    setApplied(draft)
    setNextBefore(undefined)
    void fetchWithFilters(draft)
  }, [draft, fetchWithFilters])

  const handleClear = useCallback(() => {
    const reset = emptyFilters
    setDraft(reset)
    setApplied(reset)
    setNextBefore(undefined)
    void fetchWithFilters(reset)
  }, [fetchWithFilters])

  const handleRefresh = useCallback(() => {
    setNextBefore(undefined)
    void fetchWithFilters(applied)
  }, [applied, fetchWithFilters])

  const handleLoadMore = useCallback(() => {
    if (nextBefore) {
      void fetchWithFilters(applied, nextBefore, true)
    }
  }, [applied, fetchWithFilters, nextBefore])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply],
  )

  const active = hasActiveFilters(applied)

  const filterBar = (
    <div className="flex flex-wrap items-center gap-2 border-b border-[var(--c-border-console)] px-4 py-2.5">
      <select
        value={draft.method}
        onChange={(e) => setDraft((d) => ({ ...d, method: e.target.value }))}
        className={selectClass}
      >
        <option value="">{p.filterMethod}: {p.filterAll}</option>
        {METHOD_OPTIONS.map((m) => (
          <option key={m} value={m}>{m}</option>
        ))}
      </select>

      <input
        type="text"
        placeholder={p.filterPath}
        value={draft.path}
        onChange={(e) => setDraft((d) => ({ ...d, path: e.target.value }))}
        onKeyDown={handleKeyDown}
        className={inputClass}
      />

      <input
        type="text"
        placeholder={p.filterIP}
        value={draft.ip}
        onChange={(e) => setDraft((d) => ({ ...d, ip: e.target.value }))}
        onKeyDown={handleKeyDown}
        className={inputClass}
      />

      <select
        value={draft.riskMin}
        onChange={(e) => setDraft((d) => ({ ...d, riskMin: Number(e.target.value) }))}
        className={selectClass}
      >
        <option value={0}>{p.filterRiskMin}: {p.filterAll}</option>
        {RISK_OPTIONS.map((v) => (
          <option key={v} value={v}>&ge; {v}</option>
        ))}
      </select>

      <button
        onClick={handleApply}
        disabled={loading}
        className="flex items-center gap-1.5 rounded-lg bg-[var(--c-primary,#6366f1)] px-2.5 py-1.5 text-xs font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
      >
        <Search size={12} />
        {p.apply}
      </button>

      {active && (
        <button
          onClick={handleClear}
          disabled={loading}
          className="flex items-center gap-1 text-xs text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-50"
        >
          <X size={12} />
          {p.clearFilters}
        </button>
      )}
    </div>
  )

  const actions = (
    <button
      onClick={handleRefresh}
      disabled={loading}
      className="flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
    >
      <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
      Refresh
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={p.title} actions={actions} />
      {filterBar}

      <div className="flex flex-1 flex-col overflow-auto">
        {loading && entries.length === 0 ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">{t.loading}</p>
          </div>
        ) : entries.length === 0 ? (
          <EmptyState
            icon={<Shield size={28} />}
            message={active ? p.emptyFiltered : p.empty}
          />
        ) : (
          <>
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-[var(--c-border-console)]">
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colTimestamp}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colIdentity}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colIP}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colLocation}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colMethod}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colPath}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colStatus}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colDuration}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colUserAgent}</th>
                  <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colRisk}</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr
                    key={entry.id}
                    className="border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
                  >
                    <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                      <span className="text-xs tabular-nums">
                        {formatDateTime(entry.timestamp, { includeSeconds: true, includeZone: false })}
                      </span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                      {identityLabel(entry, p.identityAnonymous)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                      <span className="font-mono text-xs">{entry.client_ip || '--'}</span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                      <span className="text-xs">{locationLabel(entry)}</span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                      <span className="font-mono text-xs">{entry.method}</span>
                    </td>
                    <td className="max-w-[200px] truncate px-4 py-2.5 text-[var(--c-text-secondary)]" title={entry.path}>
                      <span className="font-mono text-xs">{entry.path}</span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5">
                      <span className={`font-mono text-xs ${statusColor(entry.status_code)}`}>{entry.status_code}</span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                      <span className="text-xs tabular-nums">{entry.duration_ms}ms</span>
                    </td>
                    <td className="max-w-[200px] truncate px-4 py-2.5 text-[var(--c-text-secondary)]" title={entry.user_agent}>
                      <span className="text-xs">{entry.user_agent || '--'}</span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5">
                      {riskBadge(entry.risk_score, p)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            {hasMore && (
              <div className="flex justify-center py-4">
                <button
                  onClick={handleLoadMore}
                  disabled={loading}
                  className="rounded-lg border border-[var(--c-border)] px-4 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                >
                  {loading ? t.loading : p.loadMore}
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
