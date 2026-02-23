import { useState, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { BarChart3 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/useToast'
import { useLocale } from '../../contexts/LocaleContext'
import { getOrgUsage, type UsageSummary } from '../../api/usage'
import { isApiError } from '../../api'

const MONTHS = [
  'January', 'February', 'March', 'April',
  'May', 'June', 'July', 'August',
  'September', 'October', 'November', 'December',
]

function buildYearOptions(): number[] {
  const current = new Date().getUTCFullYear()
  const years: number[] = []
  for (let y = 2024; y <= current + 1; y++) years.push(y)
  return years
}

type StatCardProps = {
  label: string
  value: string
}

function StatCard({ label, value }: StatCardProps) {
  return (
    <div className="flex flex-col gap-1.5 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-5 py-4">
      <span className="text-xs text-[var(--c-text-muted)]">{label}</span>
      <span className="text-2xl font-semibold tabular-nums text-[var(--c-text-primary)]">{value}</span>
    </div>
  )
}

function formatNumber(n: number): string {
  return n.toLocaleString()
}

function formatCost(n: number): string {
  return n.toFixed(4)
}

export function UsagePage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.usage

  const now = new Date()
  const [orgId, setOrgId] = useState('')
  const [year, setYear] = useState(now.getUTCFullYear())
  const [month, setMonth] = useState(now.getUTCMonth() + 1)
  const [orgIdError, setOrgIdError] = useState('')
  const [loading, setLoading] = useState(false)
  const [summary, setSummary] = useState<UsageSummary | null>(null)

  const years = buildYearOptions()

  const handleQuery = useCallback(async () => {
    const trimmed = orgId.trim()
    if (!trimmed) {
      setOrgIdError(tc.errOrgIdRequired)
      return
    }
    setOrgIdError('')
    setLoading(true)
    try {
      const result = await getOrgUsage(trimmed, year, month, accessToken)
      setSummary(result)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [orgId, year, month, accessToken, addToast, tc])

  const selectCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none'

  const filterBar = (
    <div className="flex items-center gap-2 flex-wrap">
      <div className="flex flex-col gap-0.5">
        <input
          type="text"
          value={orgId}
          onChange={(e) => {
            setOrgId(e.target.value)
            setOrgIdError('')
          }}
          placeholder={tc.placeholderOrgId}
          className={[
            'w-72 rounded-lg border px-3 py-1.5 text-xs text-[var(--c-text-primary)] bg-[var(--c-bg-deep2)] focus:outline-none placeholder:text-[var(--c-text-muted)]',
            orgIdError
              ? 'border-[var(--c-status-error-text)]'
              : 'border-[var(--c-border)]',
          ].join(' ')}
        />
      </div>

      <select
        value={year}
        onChange={(e) => setYear(Number(e.target.value))}
        className={selectCls}
      >
        {years.map((y) => (
          <option key={y} value={y}>{y}</option>
        ))}
      </select>

      <select
        value={month}
        onChange={(e) => setMonth(Number(e.target.value))}
        className={selectCls}
      >
        {MONTHS.map((name, i) => (
          <option key={i + 1} value={i + 1}>{name}</option>
        ))}
      </select>

      <button
        onClick={handleQuery}
        disabled={loading}
        className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
      >
        {loading ? '...' : tc.queryButton}
      </button>
    </div>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={filterBar} />

      {orgIdError && (
        <p className="px-4 pt-2 text-xs text-[var(--c-status-error-text)]">{orgIdError}</p>
      )}

      <div className="flex flex-1 flex-col overflow-auto p-4">
        {summary ? (
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatCard label={tc.cardInputTokens} value={formatNumber(summary.total_input_tokens)} />
            <StatCard label={tc.cardOutputTokens} value={formatNumber(summary.total_output_tokens)} />
            <StatCard label={tc.cardCostUSD} value={formatCost(summary.total_cost_usd)} />
            <StatCard label={tc.cardRecordCount} value={formatNumber(summary.record_count)} />
          </div>
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 text-[var(--c-text-muted)]">
            <BarChart3 size={28} />
            <p className="text-sm">{tc.emptyHint}</p>
          </div>
        )}
      </div>
    </div>
  )
}
