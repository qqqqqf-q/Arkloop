import { useState, useCallback, useEffect, useMemo } from 'react'
import { Link, useOutletContext, useSearchParams } from 'react-router-dom'
import { RefreshCw, AlertTriangle } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { EmptyState } from '../../components/EmptyState'
import { formatDateTime, parseDateTimeLocalToUTC, useTimeZone, useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { listReports, type Report } from '../../api/reports'

const PAGE_SIZE = 50

type ReportFilters = {
  reportId: string
  threadId: string
  reporterId: string
  reporterEmail: string
  category: string
  feedback: string
  since: string
  until: string
}

const EMPTY_REPORT_FILTERS: ReportFilters = {
  reportId: '',
  threadId: '',
  reporterId: '',
  reporterEmail: '',
  category: '',
  feedback: '',
  since: '',
  until: '',
}

const CATEGORY_COLORS: Record<string, string> = {
  inaccurate: 'bg-amber-500/15 text-amber-400',
  out_of_date: 'bg-blue-500/15 text-blue-400',
  too_short: 'bg-purple-500/15 text-purple-400',
  too_long: 'bg-purple-500/15 text-purple-400',
  harmful_or_offensive: 'bg-red-500/15 text-red-400',
  wrong_sources: 'bg-orange-500/15 text-orange-400',
  product_suggestion: 'bg-emerald-500/15 text-emerald-400',
}

const CATEGORY_OPTIONS = Object.keys(CATEGORY_COLORS)

function CategoryBadge({ category }: { category: string }) {
  const color = CATEGORY_COLORS[category] ?? 'bg-gray-500/15 text-gray-400'
  return (
    <span className={`inline-block rounded-md px-1.5 py-0.5 text-[11px] font-medium ${color}`}>
      {category.replace(/_/g, ' ')}
    </span>
  )
}

function parseInitialFilters(searchParams: URLSearchParams): ReportFilters {
  return {
    ...EMPTY_REPORT_FILTERS,
    reportId: searchParams.get('report_id') ?? '',
    threadId: searchParams.get('thread_id') ?? '',
    reporterId: searchParams.get('reporter_id') ?? '',
    reporterEmail: searchParams.get('reporter_email') ?? '',
    category: searchParams.get('category') ?? '',
    feedback: searchParams.get('feedback') ?? '',
  }
}

function normalizeFilters(filters: ReportFilters): ReportFilters {
  return {
    ...filters,
    reportId: filters.reportId.trim(),
    threadId: filters.threadId.trim(),
    reporterId: filters.reporterId.trim(),
    reporterEmail: filters.reporterEmail.trim(),
    category: filters.category.trim(),
    feedback: filters.feedback.trim(),
  }
}

function toRFC3339(value: string, timeZone: string): string | undefined {
  if (!value.trim()) return undefined
  return parseDateTimeLocalToUTC(value, timeZone)
}

function countActiveFilters(filters: ReportFilters): number {
  const values = [
    filters.reportId,
    filters.threadId,
    filters.reporterId,
    filters.reporterEmail,
    filters.category,
    filters.feedback,
    filters.since,
    filters.until,
  ]
  return values.filter((value) => value.trim() !== '').length
}

export function ReportsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const { timeZone } = useTimeZone()
  const p = t.pages.reports
  const [searchParams] = useSearchParams()

  const [reports, setReports] = useState<Report[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [draftFilters, setDraftFilters] = useState<ReportFilters>(() => parseInitialFilters(searchParams))
  const [appliedFilters, setAppliedFilters] = useState<ReportFilters>(() => parseInitialFilters(searchParams))
  const [offset, setOffset] = useState(0)

  const fetchReports = useCallback(
    async (filters: ReportFilters, currentOffset: number) => {
      setLoading(true)
      try {
        const resp = await listReports(
          {
            report_id: filters.reportId || undefined,
            thread_id: filters.threadId || undefined,
            reporter_id: filters.reporterId || undefined,
            reporter_email: filters.reporterEmail || undefined,
            category: filters.category || undefined,
            feedback: filters.feedback || undefined,
            since: toRFC3339(filters.since, timeZone),
            until: toRFC3339(filters.until, timeZone),
            limit: PAGE_SIZE,
            offset: currentOffset,
          },
          accessToken,
        )
        setReports(resp.data)
        setTotal(resp.total)
      } catch (err) {
        addToast(isApiError(err) ? err.message : p.toastLoadFailed, 'error')
      } finally {
        setLoading(false)
      }
    },
    [accessToken, addToast, p.toastLoadFailed, timeZone],
  )

  useEffect(() => {
    void fetchReports(appliedFilters, offset)
  }, [fetchReports, appliedFilters, offset])

  const updateDraftFilter = useCallback(
    (key: keyof ReportFilters, value: string) => {
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
    const cleared = { ...EMPTY_REPORT_FILTERS }
    setDraftFilters(cleared)
    setAppliedFilters(cleared)
    setOffset(0)
  }, [])

  const handleRefresh = useCallback(() => {
    void fetchReports(appliedFilters, offset)
  }, [fetchReports, appliedFilters, offset])

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1
  const activeFilterCount = useMemo(() => countActiveFilters(appliedFilters), [appliedFilters])

  const actions = (
    <>
      <span className="rounded-md border border-[var(--c-border)] px-2 py-1 text-xs text-[var(--c-text-muted)]">
        {p.filterActiveCount(activeFilterCount)}
      </span>
      <button
        onClick={handleRefresh}
        disabled={loading}
        className="flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
      >
        <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
        {p.refresh}
      </button>
    </>
  )

  const filterInputCls = 'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={p.title} actions={actions} />

      <div className="border-b border-[var(--c-border-console)] px-6 py-3">
        <div className="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-4">
          <input
            type="text"
            placeholder={p.filterReportPlaceholder}
            value={draftFilters.reportId}
            onChange={(e) => updateDraftFilter('reportId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={p.filterThreadPlaceholder}
            value={draftFilters.threadId}
            onChange={(e) => updateDraftFilter('threadId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={p.filterReporterPlaceholder}
            value={draftFilters.reporterId}
            onChange={(e) => updateDraftFilter('reporterId', e.target.value)}
            className={filterInputCls}
          />
          <input
            type="text"
            placeholder={p.filterReporterEmailPlaceholder}
            value={draftFilters.reporterEmail}
            onChange={(e) => updateDraftFilter('reporterEmail', e.target.value)}
            className={filterInputCls}
          />
          <select
            value={draftFilters.category}
            onChange={(e) => updateDraftFilter('category', e.target.value)}
            className={filterInputCls}
          >
            <option value="">{p.filterCategoryAll}</option>
            {CATEGORY_OPTIONS.map((category) => (
              <option key={category} value={category}>
                {category.replace(/_/g, ' ')}
              </option>
            ))}
          </select>
          <input
            type="text"
            placeholder={p.filterFeedbackPlaceholder}
            value={draftFilters.feedback}
            onChange={(e) => updateDraftFilter('feedback', e.target.value)}
            className={filterInputCls}
          />
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{p.filterSinceLabel}</span>
            <input
              type="datetime-local"
              value={draftFilters.since}
              onChange={(e) => updateDraftFilter('since', e.target.value)}
              className={filterInputCls}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{p.filterUntilLabel}</span>
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
            {p.applyFilters}
          </button>
          <button
            onClick={handleResetFilters}
            className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            {p.resetFilters}
          </button>
        </div>
      </div>

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : reports.length === 0 ? (
          <EmptyState icon={<AlertTriangle size={28} />} message={p.empty} />
        ) : (
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-[var(--c-border-console)]">
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colCreatedAt}</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colReportId}</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colReporter}</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colThread}</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colCategories}</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">{p.colFeedback}</th>
              </tr>
            </thead>
            <tbody>
              {reports.map((r) => (
                <tr
                  key={r.id}
                  className="border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
                >
                  <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                    <span className="text-xs tabular-nums">
                      {formatDateTime(r.created_at, { includeZone: false })}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                    <span className="font-mono text-xs" title={r.id}>
                      {r.id}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                    <div className="flex flex-col gap-0.5">
                      <span className="text-xs">{r.reporter_email || '--'}</span>
                      <span className="font-mono text-[11px] text-[var(--c-text-muted)]" title={r.reporter_id}>
                        {r.reporter_id}
                      </span>
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]">
                    <div className="flex flex-col gap-1">
                      <span className="font-mono text-xs" title={r.thread_id || '--'}>
                        {r.thread_id || '--'}
                      </span>
                      {r.thread_id && (
                        <Link
                          to={`/runs?thread_id=${encodeURIComponent(r.thread_id)}`}
                          className="text-[11px] text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
                        >
                          {p.gotoRuns}
                        </Link>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-2.5">
                    <div className="flex flex-wrap gap-1">
                      {r.categories.map((c) => (
                        <CategoryBadge key={c} category={c} />
                      ))}
                    </div>
                  </td>
                  <td className="max-w-[300px] truncate px-4 py-2.5 text-[var(--c-text-secondary)]">
                    <span className="text-xs">{r.feedback ?? '--'}</span>
                  </td>
                </tr>
              ))}
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
              onClick={() => setOffset((prev) => Math.max(0, prev - PAGE_SIZE))}
              disabled={currentPage <= 1}
              className="rounded border border-[var(--c-border)] px-2.5 py-1 text-xs text-[var(--c-text-secondary)] disabled:opacity-40 hover:bg-[var(--c-bg-sub)]"
            >
              {p.prev}
            </button>
            <span className="flex items-center text-xs text-[var(--c-text-muted)]">
              {currentPage} / {totalPages}
            </span>
            <button
              onClick={() => setOffset((prev) => prev + PAGE_SIZE)}
              disabled={currentPage >= totalPages}
              className="rounded border border-[var(--c-border)] px-2.5 py-1 text-xs text-[var(--c-text-secondary)] disabled:opacity-40 hover:bg-[var(--c-bg-sub)]"
            >
              {p.next}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
