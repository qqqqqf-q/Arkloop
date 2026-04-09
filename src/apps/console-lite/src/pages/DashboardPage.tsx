import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { RefreshCw } from 'lucide-react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { formatDateTime, useTimeZone, useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { isApiError } from '../api'
import { getDashboard, getDailyUsage, type DashboardData, type DailyUsage } from '../api/dashboard'

function shiftDate(date: string, deltaDays: number): string {
  const current = new Date(`${date}T00:00:00Z`)
  current.setUTCDate(current.getUTCDate() + deltaDays)
  return current.toISOString().slice(0, 10)
}

function formatDate30d(timeZone: string): { start: string; end: string } {
  const end = formatDateTime(new Date(), { timeZone, includeZone: false }).slice(0, 10)
  return { start: shiftDate(end, -29), end }
}

function formatShortDate(dateStr: string): string {
  const [, m, d] = dateStr.split('-')
  return `${m}/${d}`
}

function formatTokens(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-5 py-4">
      <span className="text-xs text-[var(--c-text-muted)]">{label}</span>
      <span className="text-2xl font-semibold tabular-nums text-[var(--c-text-primary)]">{value}</span>
    </div>
  )
}

const INPUT_COLOR = '#6366f1'
const OUTPUT_COLOR = '#14b8a6'

export function DashboardPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { timeZone } = useTimeZone()
  const { t } = useLocale()
  const td = t.dashboard

  const [data, setData] = useState<DashboardData | null>(null)
  const [daily, setDaily] = useState<DailyUsage[] | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    const { start, end } = formatDate30d(timeZone)
    try {
      const [dashboardData, dailyData] = await Promise.all([
        getDashboard(accessToken),
        getDailyUsage(start, end, accessToken),
      ])
      setData(dashboardData)
      setDaily(dailyData)
    } catch (err) {
      addToast(isApiError(err) ? err.message : t.requestFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, t.requestFailed, timeZone])

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
      {td.refresh}
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={td.title} actions={actions} />
      <div className="flex flex-1 flex-col gap-6 overflow-auto p-4">
        {data ? (
          <>
            <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
              <StatCard label={td.runsTotal} value={data.total_runs.toLocaleString()} />
              <StatCard label={td.runsToday} value={data.runs_today.toLocaleString()} />
              <StatCard label={td.inputTokens} value={data.total_input_tokens.toLocaleString()} />
              <StatCard label={td.outputTokens} value={data.total_output_tokens.toLocaleString()} />
            </div>
            {daily && (
              <div className="rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-5">
                <h3 className="mb-4 text-sm font-medium text-[var(--c-text-secondary)]">
                  {td.tokenUsage30d}
                </h3>
                <ResponsiveContainer width="100%" height={280}>
                  <AreaChart data={daily}>
                    <defs>
                      <linearGradient id="gradInput" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor={INPUT_COLOR} stopOpacity={0.2} />
                        <stop offset="100%" stopColor={INPUT_COLOR} stopOpacity={0} />
                      </linearGradient>
                      <linearGradient id="gradOutput" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor={OUTPUT_COLOR} stopOpacity={0.2} />
                        <stop offset="100%" stopColor={OUTPUT_COLOR} stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <XAxis
                      dataKey="date"
                      tickFormatter={formatShortDate}
                      tick={{ fontSize: 11, fill: 'var(--c-text-muted)' }}
                      axisLine={{ stroke: 'var(--c-border)' }}
                      tickLine={false}
                      interval="preserveStartEnd"
                    />
                    <YAxis
                      tickFormatter={formatTokens}
                      tick={{ fontSize: 11, fill: 'var(--c-text-muted)' }}
                      axisLine={false}
                      tickLine={false}
                      width={56}
                    />
                    <Tooltip
                      contentStyle={{
                        background: 'var(--c-bg-deep2)',
                        border: '1px solid var(--c-border)',
                        borderRadius: 8,
                        fontSize: 12,
                      }}
                      labelFormatter={(label) => formatShortDate(String(label))}
                      formatter={(value) => (value as number).toLocaleString()}
                    />
                    <Legend
                      iconType="circle"
                      iconSize={8}
                      wrapperStyle={{ fontSize: 12, color: 'var(--c-text-muted)' }}
                    />
                    <Area
                      type="monotone"
                      dataKey="input_tokens"
                      name={td.inputTokens}
                      stroke={INPUT_COLOR}
                      fill="url(#gradInput)"
                      strokeWidth={1.5}
                    />
                    <Area
                      type="monotone"
                      dataKey="output_tokens"
                      name={td.outputTokens}
                      stroke={OUTPUT_COLOR}
                      fill="url(#gradOutput)"
                      strokeWidth={1.5}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            )}
          </>
        ) : loading ? (
          <div className="flex flex-1 items-center justify-center">
            <span className="text-sm text-[var(--c-text-muted)]">{t.common.loading}</span>
          </div>
        ) : null}
      </div>
    </div>
  )
}
