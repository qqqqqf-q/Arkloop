import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { RefreshCw } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { getDashboard, type DashboardData } from '../../api/dashboard'
import { isApiError } from '../../api'

function formatNumber(n: number): string {
  return n.toLocaleString()
}

function formatCost(n: number): string {
  return n.toFixed(4)
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-5 py-4">
      <span className="text-xs text-[var(--c-text-muted)]">{label}</span>
      <span className="text-2xl font-semibold tabular-nums text-[var(--c-text-primary)]">{value}</span>
    </div>
  )
}

export function DashboardPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.dashboard

  const [data, setData] = useState<DashboardData | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await getDashboard(accessToken)
      setData(result)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void load()
  }, [load])

  const actions = (
    <button
      onClick={load}
      disabled={loading}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
    >
      <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
      {tc.refresh}
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={actions} />
      <div className="flex flex-1 flex-col overflow-auto p-4">
        {data ? (
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatCard label={tc.cardTotalUsers} value={formatNumber(data.total_users)} />
            <StatCard label={tc.cardActiveUsers30d} value={formatNumber(data.active_users_30d)} />
            <StatCard label={tc.cardTotalRuns} value={formatNumber(data.total_runs)} />
            <StatCard label={tc.cardRunsToday} value={formatNumber(data.runs_today)} />
            <StatCard label={tc.cardInputTokens} value={formatNumber(data.total_input_tokens)} />
            <StatCard label={tc.cardOutputTokens} value={formatNumber(data.total_output_tokens)} />
            <StatCard label={tc.cardCostUSD} value={formatCost(data.total_cost_usd)} />
            <StatCard label={tc.cardActiveUsers} value={formatNumber(data.active_orgs)} />
          </div>
        ) : loading ? (
          <div className="flex flex-1 items-center justify-center">
            <span className="text-sm text-[var(--c-text-muted)]">{t.loading}</span>
          </div>
        ) : null}
      </div>
    </div>
  )
}
