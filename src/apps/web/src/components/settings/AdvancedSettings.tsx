import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowUpRight,
  BarChart3,
  Database,
  Download,
  ExternalLink,
  FileText,
  FolderOpen,
  Github,
  Globe,
  HardDrive,
  Import,
  Info,
  Loader2,
  Network,
  Package,
  RefreshCw,
  ScrollText,
  TerminalSquare,
  TrendingUp,
  type LucideIcon,
} from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { Modal, PillToggle, TabBar, formatDateTime, useTimeZone, useToast } from '@arkloop/shared'
import type {
  DesktopAdvancedOverview,
  DesktopExportSection,
  DesktopLogEntry,
  DesktopLogLevel,
  DesktopLogQuery,
} from '@arkloop/shared/desktop'
import type { MeDailyUsageItem, MeHourlyUsageItem, MeModelUsageItem, MeUsageSummary } from '../../api'
import { getMyDailyUsage, getMyHourlyUsage, getMyUsage, getMyUsageByModel } from '../../api'
import { useAppearance } from '../../contexts/AppearanceContext'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import type { ThemeDefinition } from '../../themes/types'
import { readDeveloperMode, writeDeveloperMode } from '../../storage'
import { SettingsSection } from './_SettingsSection'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { settingsInputCls } from './_SettingsInput'
import { SettingsLabel } from './_SettingsLabel'
import { SettingsSelect } from './_SettingsSelect'
import { ConnectionSettings } from './ConnectionSettings'
import { ModulesSettings } from './ModulesSettings'
import { UpdateSettingsContent } from './UpdateSettings'
import { secondaryButtonBorderStyle, secondaryButtonSmCls } from '../buttonStyles'

type AdvancedKey = 'about' | 'network' | 'usage' | 'modules' | 'data' | 'logs'

type Props = { accessToken: string }

type UsageState = {
  summary: MeUsageSummary | null
  daily: MeDailyUsageItem[]
  hourly: MeHourlyUsageItem[]
  byModel: MeModelUsageItem[]
}

const DESKTOP_EXPORT_SECTIONS: DesktopExportSection[] = [
  'settings',
  'providers',
  'history',
  'personas',
  'projects',
  'mcp',
  'themes',
]

function formatUsd(value: number) {
  return `$${value.toFixed(4)}`
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

function formatChartTick(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000 % 1 === 0 ? n / 1_000_000 : (n / 1_000_000).toFixed(1))}M`
  if (n >= 1_000) return `${(n / 1_000 % 1 === 0 ? n / 1_000 : (n / 1_000).toFixed(1))}K`
  return String(n)
}

function actionBtnCls(disabled?: boolean) {
  return [
    'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition-colors',
    disabled ? 'cursor-not-allowed opacity-50' : 'hover:bg-[var(--c-bg-deep)]',
  ].join(' ')
}

function primaryBtnCls(disabled?: boolean) {
  return [
    'inline-flex items-center gap-1.5 rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)]',
    'transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)]',
    disabled ? 'cursor-not-allowed opacity-50' : '',
  ].join(' ')
}

function getDateStringInTimeZone(value: string | Date, timeZone: string): string {
  return formatDateTime(value, { timeZone, includeZone: false }).slice(0, 10)
}

function shiftDateString(date: string, deltaDays: number): string {
  const current = new Date(`${date}T00:00:00Z`)
  current.setUTCDate(current.getUTCDate() + deltaDays)
  return current.toISOString().slice(0, 10)
}

function shiftDateStringYears(date: string, deltaYears: number): string {
  const current = new Date(`${date}T00:00:00Z`)
  current.setUTCFullYear(current.getUTCFullYear() + deltaYears)
  return current.toISOString().slice(0, 10)
}

function getWeekdayInTimeZone(value: string | Date, timeZone: string): number {
  const weekday = new Intl.DateTimeFormat('en-US', { timeZone, weekday: 'short' }).format(new Date(value))
  const map: Record<string, number> = { Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6 }
  return map[weekday] ?? 0
}

// -- Shared small components --

function MetricCard({ label, value, icon: Icon }: { label: string; value: string; icon: typeof Globe }) {
  return (
    <SettingsSection className="h-full min-w-0">
      <div className="flex items-center gap-2 text-[var(--c-text-secondary)]">
        <Icon size={15} />
        <span className="min-w-0 text-xs">{label}</span>
      </div>
      <div className="mt-3 min-w-0 break-all text-xl font-semibold text-[var(--c-text-heading)]">{value}</div>
    </SettingsSection>
  )
}

function UsageTable({
  headers,
  rows,
  emptyText,
}: {
  headers: string[]
  rows: Array<{ key: string; columns: string[] }>
  emptyText: string
}) {
  if (rows.length === 0) {
    return <p className="text-sm text-[var(--c-text-muted)]">{emptyText}</p>
  }
  return (
    <div className="min-w-0 overflow-auto rounded-xl border border-[var(--c-border-subtle)]">
      <table className="w-full text-sm">
        <thead style={{ background: 'var(--c-bg-page)' }}>
          <tr>
            {headers.map((h) => (
              <th key={h} className="px-4 py-3 text-left text-xs font-medium text-[var(--c-text-muted)]">{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key} className="border-t border-[var(--c-border-subtle)]">
              {row.columns.map((col, i) => (
                <td key={`${row.key}-${i}`} className="px-4 py-3 text-[var(--c-text-primary)]">{col}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function logLevelColor(level: DesktopLogLevel): string {
  switch (level) {
    case 'error': return '#f87171'
    case 'warn': return '#fbbf24'
    case 'debug': return '#a78bfa'
    case 'info': return '#60a5fa'
    default: return '#9ca3af'
  }
}

function logLevelTag(level: DesktopLogLevel): string {
  return level.toUpperCase().padEnd(5, ' ')
}

// -- Sub-panes --

function AboutPane({
  overview,
  loading,
  error,
}: {
  overview: DesktopAdvancedOverview | null
  loading: boolean
  error: string
}) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const [devMode, setDevMode] = useState(() => readDeveloperMode())

  if (loading) {
    return (
      <div className="flex min-h-[220px] items-center justify-center">
        <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  if (error) {
    return (
      <SettingsSection>
        <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</p>
      </SettingsSection>
    )
  }

  if (!overview) return null

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.about} description={ds.aboutDesc} />

      <SettingsSection>
        <div className="flex flex-wrap items-center gap-4">
          <div
            className="flex h-14 w-14 items-center justify-center overflow-hidden rounded-2xl bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {overview.iconDataUrl ? (
              <img src={overview.iconDataUrl} alt={overview.appName} className="h-full w-full object-cover" />
            ) : (
              <HardDrive size={22} className="text-[var(--c-text-muted)]" />
            )}
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-lg font-semibold text-[var(--c-text-heading)]">{overview.appName}</div>
            <div className="mt-0.5 text-sm text-[var(--c-text-secondary)]">{overview.appVersion}</div>
          </div>
          <div className="flex flex-wrap gap-2">
            {overview.links.map((link) => (
              <button
                key={link.url}
                type="button"
                onClick={() => openExternal(link.url)}
                className={secondaryButtonSmCls}
                style={secondaryButtonBorderStyle}
              >
                {link.label === 'GitHub' ? <Github size={14} /> : <ExternalLink size={14} />}
                <span>{link.label}</span>
              </button>
            ))}
          </div>
        </div>
      </SettingsSection>

      <SettingsSection>
        <UpdateSettingsContent />
      </SettingsSection>

      <SettingsSection>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">{ds.developerTitle}</div>
            <div className="text-xs text-[var(--c-text-muted)]">{ds.developerDesc}</div>
          </div>
          <PillToggle
            checked={devMode}
            onChange={(next) => {
              setDevMode(next)
              writeDeveloperMode(next)
            }}
          />
        </div>
      </SettingsSection>
    </div>
  )
}

function NetworkPane({ onReloadOverview }: { onReloadOverview: () => Promise<void> }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const [config, setConfig] = useState({
    proxyEnabled: false,
    proxyUrl: '',
    requestTimeoutMs: 30000,
    retryCount: 1,
    userAgent: '',
  })
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!api) return
    void api.config.get().then((next) => {
      setConfig({
        proxyEnabled: next.network.proxyEnabled,
        proxyUrl: next.network.proxyUrl ?? '',
        requestTimeoutMs: next.network.requestTimeoutMs ?? 30000,
        retryCount: next.network.retryCount ?? 1,
        userAgent: next.network.userAgent ?? '',
      })
    }).catch(() => {})
  }, [api])

  const handleSave = useCallback(async () => {
    if (!api) return
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      const current = await api.config.get()
      await api.config.set({
        ...current,
        network: {
          proxyEnabled: config.proxyEnabled,
          proxyUrl: config.proxyUrl.trim() || undefined,
          requestTimeoutMs: config.requestTimeoutMs,
          retryCount: config.retryCount,
          userAgent: config.userAgent.trim() || undefined,
        },
      })
      setSaved(true)
      void onReloadOverview()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setSaving(false)
      window.setTimeout(() => setSaved(false), 2000)
    }
  }, [api, config, onReloadOverview, t.requestFailed])

  const INPUT = settingsInputCls('sm')

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedNetwork} description={ds.advancedNetworkDesc} />

      <SettingsSection>
        <div className="flex flex-col gap-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <SettingsLabel>{ds.advancedNetworkProxyEnable}</SettingsLabel>
              <SettingsSelect
                value={config.proxyEnabled ? 'true' : 'false'}
                options={[
                  { value: 'false', label: ds.advancedDisabled },
                  { value: 'true', label: ds.advancedEnabled },
                ]}
                onChange={(v) => setConfig((p) => ({ ...p, proxyEnabled: v === 'true' }))}
              />
            </div>
            <div>
              <SettingsLabel>{ds.advancedNetworkProxyUrl}</SettingsLabel>
              <input
                value={config.proxyUrl}
                onChange={(e) => setConfig((p) => ({ ...p, proxyUrl: e.target.value }))}
                placeholder="http://127.0.0.1:7890"
                className={INPUT}
              />
            </div>
            <div>
              <SettingsLabel>{ds.advancedNetworkTimeout}</SettingsLabel>
              <input
                type="number"
                min={1000}
                max={300000}
                value={config.requestTimeoutMs}
                onChange={(e) => setConfig((p) => ({ ...p, requestTimeoutMs: Number(e.target.value) || 30000 }))}
                className={INPUT}
              />
            </div>
            <div>
              <SettingsLabel>{ds.advancedNetworkRetry}</SettingsLabel>
              <input
                type="number"
                min={0}
                max={10}
                value={config.retryCount}
                onChange={(e) => setConfig((p) => ({ ...p, retryCount: Number(e.target.value) || 0 }))}
                className={INPUT}
              />
            </div>
          </div>
          <div>
            <SettingsLabel>{ds.advancedNetworkUserAgent}</SettingsLabel>
            <input
              value={config.userAgent}
              onChange={(e) => setConfig((p) => ({ ...p, userAgent: e.target.value }))}
              placeholder="Arkloop Desktop"
              className={INPUT}
            />
          </div>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => void handleSave()}
              disabled={saving}
              className={primaryBtnCls(saving)}
              style={{ background: 'var(--c-btn-bg)' }}
            >
              {saving && <Loader2 size={14} className="animate-spin" />}
              <span>{saving ? ds.advancedSaving : ds.advancedSave}</span>
            </button>
            {saved && <span className="text-sm" style={{ color: 'var(--c-status-success)' }}>{ds.advancedSaved}</span>}
            {error && <span className="text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</span>}
          </div>
        </div>
      </SettingsSection>

      <ConnectionSettings />
    </div>
  )
}

function UsagePane({
  accessToken,
  defaultYear,
  defaultMonth,
}: {
  accessToken: string
  defaultYear: number
  defaultMonth: number
}) {
  const { t } = useLocale()
  const { timeZone } = useTimeZone()
  const ds = t.desktopSettings
  const [usage, setUsage] = useState<UsageState>({ summary: null, daily: [], hourly: [], byModel: [] })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [year, setYear] = useState(defaultYear)
  const [month, setMonth] = useState(defaultMonth)
  const [heatmapData, setHeatmapData] = useState<MeDailyUsageItem[]>([])
  const [chartTab, setChartTab] = useState<'spend' | 'trend' | 'model'>('spend')
  const [trendGranularity, setTrendGranularity] = useState<'hourly' | 'daily'>('hourly')
  const [chartTooltip, setChartTooltip] = useState<{ x: number; y: number; text: string } | null>(null)
  const chartRef = useRef<HTMLDivElement>(null)
  const heatmapScrollRef = useRef<HTMLDivElement>(null)
  const isShiftScrollingHeatmapRef = useRef(false)
  const heatmapShiftScrollResetRef = useRef<number | null>(null)

  const loadUsage = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const monthStart = `${year}-${String(month).padStart(2, '0')}-01`
      const nextMonthYear = month === 12 ? year + 1 : year
      const nextMonthValue = month === 12 ? 1 : month + 1
      const monthEnd = `${nextMonthYear}-${String(nextMonthValue).padStart(2, '0')}-01`
      const [summary, daily, hourly, byModel] = await Promise.all([
        getMyUsage(accessToken, year, month),
        getMyDailyUsage(accessToken, monthStart, monthEnd),
        getMyHourlyUsage(accessToken, monthStart, monthEnd),
        getMyUsageByModel(accessToken, year, month),
      ])
      setUsage({ summary, daily, hourly, byModel })
    } catch (err) {
      setError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, year, month, t.requestFailed])

  useEffect(() => { void loadUsage() }, [loadUsage])

  // heatmap: past 365 days, independent of year/month selection
  useEffect(() => {
    const todayStr = getDateStringInTimeZone(new Date(), timeZone)
    const startStr = shiftDateString(shiftDateStringYears(todayStr, -1), 1)
    void getMyDailyUsage(accessToken, startStr, todayStr)
      .then(setHeatmapData)
      .catch(() => {})
  }, [accessToken, timeZone])

  // build heatmap grid: 53 columns x 7 rows
  const heatmap = useMemo(() => {
    const tokenMap = new Map<string, number>()
    let totalTokens = 0
    for (const d of heatmapData) {
      const tokens = d.input_tokens + d.output_tokens
      tokenMap.set(d.date, tokens)
      totalTokens += tokens
    }
    const today = getDateStringInTimeZone(new Date(), timeZone)
    // end of current week (Saturday)
    const dayOfWeek = getWeekdayInTimeZone(new Date(), timeZone)
    const endDay = shiftDateString(today, 6 - dayOfWeek)
    // start: 52 weeks before the start of the week containing today
    const startDay = shiftDateString(endDay, -52 * 7 - 6)

    const weeks: Array<Array<{ date: string; value: number; inRange: boolean }>> = []
    let maxVal = 0
    let cursor = startDay
    while (cursor <= endDay) {
      const week: Array<{ date: string; value: number; inRange: boolean }> = []
      for (let d = 0; d < 7; d++) {
        const val = tokenMap.get(cursor) ?? 0
        const inRange = cursor <= today
        week.push({ date: cursor, value: val, inRange })
        if (val > maxVal) maxVal = val
        cursor = shiftDateString(cursor, 1)
      }
      weeks.push(week)
    }

    // month labels: place label at the first column where the 1st day of the month appears
    const monthNames = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
    // collect (col, monthIndex) for every 1st-of-month day found in the grid
    const firstOfMonthCols: Array<{ m: number; col: number }> = []
    for (let w = 0; w < weeks.length; w++) {
      for (const cell of weeks[w]) {
        if (cell.date.endsWith('-01')) {
          const m = Number(cell.date.slice(5, 7)) - 1
          firstOfMonthCols.push({ m, col: w })
        }
      }
    }
    // sort by column position, then deduplicate by month
    firstOfMonthCols.sort((a, b) => a.col - b.col)
    const seenMonths = new Set<number>()
    const monthLabels: Array<{ label: string; col: number }> = []
    let lastCol = -999
    for (const { m, col } of firstOfMonthCols) {
      if (seenMonths.has(m)) continue
      seenMonths.add(m)
      // skip if too close to previous label (less than 2 columns)
      if (col - lastCol < 2) continue
      monthLabels.push({ label: monthNames[m], col })
      lastCol = col
    }

    return { weeks, maxVal, monthLabels, totalTokens }
  }, [heatmapData, timeZone])

  function heatColor(value: number, max: number): string {
    if (value === 0 || max === 0) return 'var(--c-bg-deep)'
    const ratio = value / max
    if (ratio < 0.25) return '#0e4429'
    if (ratio < 0.5) return '#006d32'
    if (ratio < 0.75) return '#26a641'
    return '#39d353'
  }

  // measure chart container width
  const [chartWidth, setChartWidth] = useState(0)

  // SVG chart builders
  const compactChart = chartWidth > 0 && chartWidth < 560
  const chartPadding = compactChart
    ? { top: 20, right: 12, bottom: 26, left: 52 }
    : { top: 20, right: 16, bottom: 30, left: 60 }
  const chartH = 320
  const modelLabelMaxLength = compactChart ? 10 : chartWidth < 720 ? 14 : 18

  useEffect(() => {
    const node = heatmapScrollRef.current
    if (!node) return

    const handleWheel = (event: WheelEvent) => {
      if (!event.shiftKey) return
      const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
      if (delta === 0) return
      isShiftScrollingHeatmapRef.current = true
      node.scrollLeft += delta
      event.preventDefault()
      if (heatmapShiftScrollResetRef.current) window.clearTimeout(heatmapShiftScrollResetRef.current)
      heatmapShiftScrollResetRef.current = window.setTimeout(() => {
        isShiftScrollingHeatmapRef.current = false
      }, 120)
    }

    node.addEventListener('wheel', handleWheel, { passive: false })
    return () => {
      node.removeEventListener('wheel', handleWheel)
      if (heatmapShiftScrollResetRef.current) window.clearTimeout(heatmapShiftScrollResetRef.current)
    }
  }, [])

  function renderSpendDistChart(
    items: Array<{ label: string; input_tokens: number; output_tokens: number }>,
    width: number,
  ) {
    if (items.length === 0 || width <= 0) return null
    const cw = width - chartPadding.left - chartPadding.right
    const ch = chartH - chartPadding.top - chartPadding.bottom
    const totals = items.map((d) => d.input_tokens + d.output_tokens)
    const maxVal = Math.max(...totals, 1)
    const barW = Math.max(2, (cw / items.length) - 2)
    const step = cw / items.length

    // y-axis ticks
    const yTicks = [0, maxVal * 0.25, maxVal * 0.5, maxVal * 0.75, maxVal]

    return (
      <svg width={width} height={chartH} className="block">
        {/* grid lines */}
        {yTicks.map((v, i) => {
          const y = chartPadding.top + ch - (v / maxVal) * ch
          return (
            <g key={i}>
              <line x1={chartPadding.left} y1={y} x2={width - chartPadding.right} y2={y} stroke="var(--c-border-subtle)" strokeDasharray="3,3" />
              <text x={chartPadding.left - 6} y={y + 4} textAnchor="end" fill="var(--c-text-muted)" fontSize={compactChart ? 9 : 10}>{formatChartTick(Math.round(v))}</text>
            </g>
          )
        })}
        {/* bars */}
        {items.map((d, i) => {
          const barH = (totals[i] / maxVal) * ch
          const x = chartPadding.left + i * step + (step - barW) / 2
          const y = chartPadding.top + ch - barH
          return (
            <rect
              key={d.label}
              x={x}
              y={y}
              width={barW}
              height={Math.max(barH, 0)}
              rx={1}
              fill="var(--c-accent)"
              opacity={0.85}
              onMouseEnter={(e) => setChartTooltip({ x: e.clientX, y: e.clientY, text: `${d.label}: ${formatNumber(d.input_tokens + d.output_tokens)} tokens` })}
              onMouseLeave={() => setChartTooltip(null)}
            />
          )
        })}
        {/* x-axis labels (sparse) */}
        {items.filter((_, i) => i % Math.max(1, Math.floor(items.length / 6)) === 0).map((d) => {
          const idx = items.indexOf(d)
          const x = chartPadding.left + idx * step + step / 2
          return (
            <text key={d.label} x={x} y={chartH - 6} textAnchor="middle" fill="var(--c-text-muted)" fontSize={compactChart ? 9 : 10}>
              {d.label.includes(' ') ? d.label.slice(6) : d.label.slice(5)}
            </text>
          )
        })}
      </svg>
    )
  }

  function renderTrendChart(
    items: Array<{ label: string; input_tokens: number; output_tokens: number }>,
    width: number,
  ) {
    if (items.length === 0 || width <= 0) return null
    const cw = width - chartPadding.left - chartPadding.right
    const ch = chartH - chartPadding.top - chartPadding.bottom
    const totals = items.map((d) => d.input_tokens + d.output_tokens)
    const maxVal = Math.max(...totals, 1)
    const step = items.length > 1 ? cw / (items.length - 1) : cw

    const yTicks = [0, maxVal * 0.25, maxVal * 0.5, maxVal * 0.75, maxVal]

    const points = items.map((d, i) => {
      const x = chartPadding.left + (items.length > 1 ? i * step : cw / 2)
      const y = chartPadding.top + ch - (totals[i] / maxVal) * ch
      return { x, y, d }
    })
    const pathD = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x},${p.y}`).join(' ')

    return (
      <svg width={width} height={chartH} className="block">
        {yTicks.map((v, i) => {
          const y = chartPadding.top + ch - (v / maxVal) * ch
          return (
            <g key={i}>
              <line x1={chartPadding.left} y1={y} x2={width - chartPadding.right} y2={y} stroke="var(--c-border-subtle)" strokeDasharray="3,3" />
              <text x={chartPadding.left - 6} y={y + 4} textAnchor="end" fill="var(--c-text-muted)" fontSize={compactChart ? 9 : 10}>{formatChartTick(Math.round(v))}</text>
            </g>
          )
        })}
        <path d={pathD} fill="none" stroke="var(--c-accent)" strokeWidth={2} />
        {points.map((p) => (
          <circle
            key={p.d.label}
            cx={p.x}
            cy={p.y}
            r={3}
            fill="var(--c-accent)"
            onMouseEnter={(e) => setChartTooltip({ x: e.clientX, y: e.clientY, text: `${p.d.label}: ${formatNumber(p.d.input_tokens + p.d.output_tokens)} tokens` })}
            onMouseLeave={() => setChartTooltip(null)}
          />
        ))}
        {items.filter((_, i) => i % Math.max(1, Math.floor(items.length / 6)) === 0).map((d) => {
          const idx = items.indexOf(d)
          const x = chartPadding.left + (items.length > 1 ? idx * step : cw / 2)
          return (
            <text key={d.label} x={x} y={chartH - 6} textAnchor="middle" fill="var(--c-text-muted)" fontSize={compactChart ? 9 : 10}>
              {d.label.includes(' ') ? d.label.slice(6) : d.label.slice(5)}
            </text>
          )
        })}
      </svg>
    )
  }

  function renderModelChart(byModel: MeModelUsageItem[], width: number) {
    if (byModel.length === 0 || width <= 0) return null
    const maxVal = Math.max(...byModel.map((m) => m.cost_usd), 0.0001)
    const barH = compactChart ? 22 : 28
    const gap = compactChart ? 6 : 8
    const totalH = byModel.length * (barH + gap)
    const cw = width - chartPadding.left - chartPadding.right
    const colors = ['#4f8ff7', '#f5a623', '#7b61ff', '#36c5ab', '#ff6b81', '#c084fc', '#f472b6', '#34d399']

    return (
      <svg width={width} height={Math.max(totalH + chartPadding.top + 10, chartH)} className="block">
        {byModel.map((m, i) => {
          const y = chartPadding.top + i * (barH + gap)
          const barW = Math.max((m.cost_usd / maxVal) * cw, 2)
          const color = colors[i % colors.length]
          return (
            <g key={m.model}>
              <text x={chartPadding.left - 6} y={y + barH / 2 + 4} textAnchor="end" fill="var(--c-text-secondary)" fontSize={compactChart ? 10 : 11}>
                {m.model.length > modelLabelMaxLength ? m.model.slice(0, modelLabelMaxLength) + '...' : m.model}
              </text>
              <rect
                x={chartPadding.left}
                y={y}
                width={barW}
                height={barH}
                rx={4}
                fill={color}
                opacity={0.85}
                onMouseEnter={(e) => setChartTooltip({ x: e.clientX, y: e.clientY, text: `${m.model}: $${m.cost_usd.toFixed(4)}` })}
                onMouseLeave={() => setChartTooltip(null)}
              />
              <text x={width - 4} y={y + barH / 2 + 4} textAnchor="end" fill="var(--c-text-muted)" fontSize={compactChart ? 9 : 10}>
                ${m.cost_usd.toFixed(4)}
              </text>
            </g>
          )
        })}
      </svg>
    )
  }

  useEffect(() => {
    if (!chartRef.current) return
    const obs = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setChartWidth(entry.contentRect.width)
      }
    })
    obs.observe(chartRef.current)
    return () => obs.disconnect()
  }, [])

  const chartTabs: Array<{ key: 'spend' | 'trend' | 'model'; label: string }> = [
    { key: 'spend', label: ds.advancedUsageSpendDist },
    { key: 'trend', label: ds.advancedUsageTrend },
    { key: 'model', label: ds.advancedUsageModelDist },
  ]

  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <SettingsSectionHeader title={ds.advancedUsage} description={ds.advancedUsageDesc} />
        <div className="flex items-center gap-2">
          <SettingsSelect
            value={String(year)}
            options={Array.from({ length: 3 }, (_, i) => ({ value: String(defaultYear - i), label: String(defaultYear - i) }))}
            onChange={(v) => setYear(Number(v))}
          />
          <SettingsSelect
            value={String(month)}
            options={Array.from({ length: 12 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }))}
            onChange={(v) => setMonth(Number(v))}
          />
        </div>
      </div>

      {error && (
        <SettingsSection>
          <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</p>
        </SettingsSection>
      )}

      <div className="grid grid-cols-3 gap-4">
        <MetricCard label={ds.advancedUsageCost} value={loading ? '...' : formatUsd(usage.summary?.total_cost_usd ?? 0)} icon={Globe} />
        <MetricCard label={ds.advancedUsageMessages} value={loading ? '...' : formatNumber(usage.summary?.record_count ?? 0)} icon={TerminalSquare} />
        <MetricCard label={ds.advancedUsageInput} value={loading ? '...' : formatNumber(usage.summary?.total_input_tokens ?? 0)} icon={Download} />
        <MetricCard label={ds.advancedUsageOutput} value={loading ? '...' : formatNumber(usage.summary?.total_output_tokens ?? 0)} icon={ArrowUpRight} />
        <MetricCard label={ds.advancedUsageCacheRead} value={loading ? '...' : formatNumber(usage.summary?.total_cache_read_tokens ?? 0)} icon={FileText} />
        <MetricCard label={ds.advancedUsageCacheWrite} value={loading ? '...' : formatNumber(usage.summary?.total_cache_creation_tokens ?? 0)} icon={Database} />
      </div>

      {/* heatmap */}
      <SettingsSection overflow="visible">
        <SettingsSectionHeader title={ds.advancedUsageHeatmap} />
        <div
          ref={heatmapScrollRef}
          className="mt-4 max-w-full overflow-x-auto overflow-y-hidden pb-1"
          style={{ overscrollBehaviorX: 'contain' }}
          onPointerDown={() => {
            isShiftScrollingHeatmapRef.current = false
          }}
          onMouseMove={(event) => {
            if (isShiftScrollingHeatmapRef.current || !heatmapScrollRef.current) return
            if ((event.buttons & 1) !== 1) return
            heatmapScrollRef.current.scrollLeft -= event.movementX
          }}
        >
          <div style={{ minWidth: heatmap.weeks.length * 12 + 36 + 8 }}>
            {/* month labels */}
            <div className="relative text-[10px] text-[var(--c-text-muted)]">
              {heatmap.monthLabels.map((ml, i) => (
                <span
                  key={`${ml.label}-${i}`}
                  style={{ position: 'absolute', left: 36 + ml.col * 12 }}
                >
                  {ml.label}
                </span>
              ))}
              {/* spacer to give the div height */}
              <span className="invisible">M</span>
            </div>
            <div className="relative mt-4 flex gap-0.5" style={{ paddingLeft: 36 }}>
              {/* week day labels */}
              <div className="absolute left-0 top-0 flex flex-col text-[10px] text-[var(--c-text-muted)]" style={{ width: 32 }}>
                <span style={{ height: 12, lineHeight: '12px' }}>&nbsp;</span>
                <span style={{ height: 12, lineHeight: '12px' }}>Mon</span>
                <span style={{ height: 12, lineHeight: '12px' }}>&nbsp;</span>
                <span style={{ height: 12, lineHeight: '12px' }}>Wed</span>
                <span style={{ height: 12, lineHeight: '12px' }}>&nbsp;</span>
                <span style={{ height: 12, lineHeight: '12px' }}>Fri</span>
                <span style={{ height: 12, lineHeight: '12px' }}>&nbsp;</span>
              </div>
              {/* grid */}
              {heatmap.weeks.map((week, wi) => (
                <div key={wi} className="flex flex-col gap-0.5">
                  {week.map((cell) => (
                    <div
                      key={cell.date}
                      onMouseEnter={(e) => cell.inRange && cell.value > 0
                        ? setChartTooltip({ x: e.clientX, y: e.clientY, text: `${cell.date}: ${formatNumber(cell.value)} tokens` })
                        : undefined}
                      onMouseMove={(e) => chartTooltip ? setChartTooltip({ x: e.clientX, y: e.clientY, text: chartTooltip.text }) : undefined}
                      onMouseLeave={() => setChartTooltip(null)}
                      style={{
                        width: 10,
                        height: 10,
                        borderRadius: 2,
                        background: cell.inRange ? heatColor(cell.value, heatmap.maxVal) : 'transparent',
                      }}
                    />
                  ))}
                </div>
              ))}
            </div>
            {/* legend */}
            <div className="mt-3 flex items-center justify-between text-[10px] text-[var(--c-text-muted)]" style={{ paddingLeft: 36 }}>
              <span className="font-medium text-[var(--c-text-secondary)]">{formatNumber(heatmap.totalTokens)} Tokens</span>
              <div className="flex items-center gap-1.5">
                <span>Less</span>
                {['var(--c-bg-deep)', '#0e4429', '#006d32', '#26a641', '#39d353'].map((c) => (
                  <div key={c} style={{ width: 10, height: 10, borderRadius: 2, background: c }} />
                ))}
                <span>More</span>
              </div>
            </div>
          </div>
        </div>
      </SettingsSection>

      {/* model analytics charts */}
      <SettingsSection overflow="visible">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2 text-[var(--c-text-heading)]">
              <TrendingUp size={16} />
              <span className="text-sm font-semibold">{ds.advancedUsageModelAnalysis}</span>
            </div>
            {chartTab !== 'model' && (
              <TabBar
                tabs={[
                  { key: 'hourly' as const, label: ds.advancedUsageTrendHourly },
                  { key: 'daily' as const, label: ds.advancedUsageTrendDaily },
                ]}
                active={trendGranularity}
                onChange={setTrendGranularity}
              />
            )}
          </div>
          <TabBar
            tabs={chartTabs}
            active={chartTab}
            onChange={setChartTab}
          />
        </div>
        <div ref={chartRef} className="relative mt-4 min-h-[320px] min-w-0 overflow-x-hidden">
          {(chartTab === 'spend' || chartTab === 'trend') && (() => {
            const items = trendGranularity === 'hourly'
              ? usage.hourly.map((h) => {
                  const label = formatDateTime(h.hour, { timeZone, includeZone: false }).slice(5, 16)
                  return { label, input_tokens: h.input_tokens, output_tokens: h.output_tokens }
                })
              : usage.daily.map((d) => ({ label: d.date, input_tokens: d.input_tokens, output_tokens: d.output_tokens }))
            if (items.length === 0) return (
              <p className="flex h-[320px] items-center justify-center text-sm text-[var(--c-text-muted)]">
                {ds.advancedUsageEmpty}
              </p>
            )
            return chartTab === 'spend'
              ? renderSpendDistChart(items, chartWidth)
              : renderTrendChart(items, chartWidth)
          })()}
          {chartTab === 'model' && (
            usage.byModel.length === 0
              ? <p className="flex h-[320px] items-center justify-center text-sm text-[var(--c-text-muted)]">{ds.advancedUsageEmpty}</p>
              : renderModelChart(usage.byModel, chartWidth)
          )}
        </div>
      </SettingsSection>

      <SettingsSection>
        <SettingsSectionHeader title={ds.advancedUsageByModel} />
        <div className="mt-4">
          <UsageTable
            headers={[ds.advancedUsageModel, ds.advancedUsageCost, ds.advancedUsageTokens, ds.advancedUsageCache, ds.advancedUsageMessages]}
            rows={usage.byModel.map((row) => ({
              key: row.model,
              columns: [
                row.model,
                formatUsd(row.cost_usd),
                `${formatNumber(row.input_tokens)} / ${formatNumber(row.output_tokens)}`,
                `${formatNumber(row.cache_read_tokens)} / ${formatNumber(row.cache_creation_tokens)}`,
                formatNumber(row.record_count),
              ],
            }))}
            emptyText={ds.advancedUsageEmpty}
          />
        </div>
      </SettingsSection>

      <SettingsSection>
        <SettingsSectionHeader title={ds.advancedUsageDaily} />
        <div className="mt-4">
          <UsageTable
            headers={[ds.advancedUsageDate, ds.advancedUsageCost, ds.advancedUsageTokens, ds.advancedUsageMessages]}
            rows={usage.daily.map((row) => ({
              key: row.date,
              columns: [
                row.date,
                formatUsd(row.cost_usd),
                `${formatNumber(row.input_tokens)} / ${formatNumber(row.output_tokens)}`,
                formatNumber(row.record_count),
              ],
            }))}
            emptyText={ds.advancedUsageEmpty}
          />
        </div>
      </SettingsSection>

      {chartTooltip && (
        <div
          className="pointer-events-none fixed z-50 rounded-lg px-2.5 py-1.5 text-xs shadow-lg"
          style={{
            left: chartTooltip.x,
            top: chartTooltip.y - 8,
            transform: 'translate(-50%, -100%)',
            background: 'var(--c-bg-sub)',
            color: 'var(--c-text-primary)',
            border: '0.5px solid var(--c-border-subtle)',
            animation: 'tooltip-in 120ms ease-out',
          }}
        >
          {chartTooltip.text}
        </div>
      )}
    </div>
  )
}

function DataPane({ onReloadOverview }: { onReloadOverview: () => Promise<void> }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const { addToast } = useToast()
  const { customThemeId, customThemes, saveCustomTheme, setActiveCustomTheme } = useAppearance()
  const [actionLoading, setActionLoading] = useState<'choose' | 'export' | 'import' | null>(null)
  const [actionError, setActionError] = useState('')
  const [exportDialogOpen, setExportDialogOpen] = useState(false)
  const [selectedSections, setSelectedSections] = useState<DesktopExportSection[]>(DESKTOP_EXPORT_SECTIONS)

  const exportOptions = [
    { key: 'settings' as const, label: ds.advancedExportSettings },
    { key: 'providers' as const, label: ds.advancedExportProviders },
    { key: 'history' as const, label: ds.advancedExportHistory },
    { key: 'personas' as const, label: ds.advancedExportPersonas },
    { key: 'projects' as const, label: ds.advancedExportProjects },
    { key: 'mcp' as const, label: ds.advancedExportMcp },
    { key: 'themes' as const, label: ds.advancedExportThemes },
  ]

  const toggleSection = useCallback((section: DesktopExportSection) => {
    setSelectedSections((prev) => (
      prev.includes(section)
        ? prev.filter((item) => item !== section)
        : [...prev, section]
    ))
  }, [])

  const handleChoose = useCallback(async () => {
    if (!api?.advanced) return
    setActionLoading('choose')
    setActionError('')
    try {
      const selected = await api.advanced.chooseDataFolder()
      if (selected) addToast(ds.advancedSelectedFolder, 'success')
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, ds.advancedSelectedFolder, t.requestFailed])

  const handleExport = useCallback(async () => {
    if (!api?.advanced) return
    if (selectedSections.length === 0) return
    setActionLoading('export')
    setActionError('')
    try {
      const result = await api.advanced.exportDataBundle({
        sections: selectedSections,
        themes: {
          customThemeId,
          customThemes: selectedSections.includes('themes') ? customThemes : {},
        },
      })
      if (result.canceled) {
        addToast(ds.advancedExportCanceled, 'neutral')
        setExportDialogOpen(false)
        return
      }
      addToast(ds.advancedExportDone, 'success')
      setExportDialogOpen(false)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, customThemeId, customThemes, ds.advancedExportCanceled, ds.advancedExportDone, selectedSections, t.requestFailed])

  const handleImport = useCallback(async () => {
    if (!api?.advanced) return
    setActionLoading('import')
    setActionError('')
    try {
      const result = await api.advanced.importDataBundle()
      if (result.canceled) {
        addToast(ds.advancedImportCanceled, 'neutral')
        return
      }
      if (result.themes?.customThemes && typeof result.themes.customThemes === 'object') {
        for (const value of Object.values(result.themes.customThemes)) {
          if (value && typeof value === 'object' && 'id' in value && typeof value.id === 'string') {
            saveCustomTheme(value as ThemeDefinition)
          }
        }
        if (result.themes.customThemeId) {
          setActiveCustomTheme(result.themes.customThemeId)
        }
      }
      addToast(ds.advancedImportDone, 'success')
      await onReloadOverview()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, ds.advancedImportCanceled, ds.advancedImportDone, onReloadOverview, saveCustomTheme, setActiveCustomTheme, t.requestFailed])

  const busy = actionLoading !== null

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedData} description={ds.advancedDataDesc} />

      <SettingsSection>
        <div className="flex flex-wrap gap-3">
          <button
            type="button"
            onClick={() => void handleChoose()}
            disabled={busy}
            className={actionBtnCls(busy)}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <FolderOpen size={14} />
            <span>{ds.advancedChooseFolder}</span>
          </button>
          <button
            type="button"
            onClick={() => setExportDialogOpen(true)}
            disabled={busy}
            className={actionBtnCls(busy)}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <Download size={14} />
            <span>{ds.advancedExport}</span>
          </button>
          <button
            type="button"
            onClick={() => void handleImport()}
            disabled={busy}
            className={actionBtnCls(busy)}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <Import size={14} />
            <span>{ds.advancedImport}</span>
          </button>
        </div>
        {actionError && <p className="mt-2 text-sm" style={{ color: 'var(--c-status-error)' }}>{actionError}</p>}
      </SettingsSection>

      <Modal
        open={exportDialogOpen}
        onClose={() => {
          if (actionLoading !== 'export') setExportDialogOpen(false)
        }}
        title={ds.advancedExportTitle}
        width="520px"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-[var(--c-text-secondary)]">{ds.advancedExportDesc}</p>
          <div className="flex items-center justify-between text-sm">
            <span className="text-[var(--c-text-secondary)]">{selectedSections.length} / {exportOptions.length}</span>
            <div className="flex gap-2">
              <button
                type="button"
                className={actionBtnCls()}
                onClick={() => setSelectedSections(DESKTOP_EXPORT_SECTIONS)}
              >
                {ds.advancedExportSelectAll}
              </button>
              <button
                type="button"
                className={actionBtnCls()}
                onClick={() => setSelectedSections([])}
              >
                {ds.advancedExportClearAll}
              </button>
            </div>
          </div>
          <div className="grid gap-2">
            {exportOptions.map((option) => {
              const checked = selectedSections.includes(option.key)
              return (
                <label
                  key={option.key}
                  className="flex items-center gap-3 rounded-xl px-3 py-2 text-sm"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => toggleSection(option.key)}
                  />
                  <span className="text-[var(--c-text-primary)]">{option.label}</span>
                </label>
              )
            })}
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setExportDialogOpen(false)}
              disabled={actionLoading === 'export'}
              className={actionBtnCls(actionLoading === 'export')}
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {ds.advancedCancel}
            </button>
            <button
              type="button"
              onClick={() => void handleExport()}
              disabled={actionLoading === 'export' || selectedSections.length === 0}
              className={primaryBtnCls(actionLoading === 'export' || selectedSections.length === 0)}
              style={{ background: 'var(--c-btn-bg)' }}
            >
              {actionLoading === 'export' && <Loader2 size={14} className="animate-spin" />}
              <span>{ds.advancedExport}</span>
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

function LogsPane() {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const [logs, setLogs] = useState<DesktopLogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [source, setSource] = useState<DesktopLogQuery['source']>('all')
  const [level, setLevel] = useState<DesktopLogQuery['level']>('all')
  const [search, setSearch] = useState('')
  const termRef = useRef<HTMLDivElement>(null)

  const loadLogs = useCallback(async () => {
    if (!api?.advanced) return
    setLoading(true)
    setError('')
    try {
      const result = await api.advanced.listLogs({ source, level, search, limit: 200 })
      setLogs(result.entries)
    } catch (err) {
      setError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setLoading(false)
    }
  }, [api, source, level, search, t.requestFailed])

  useEffect(() => { void loadLogs() }, [loadLogs])

  useEffect(() => {
    if (termRef.current) termRef.current.scrollTop = termRef.current.scrollHeight
  }, [logs])

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedLogs} description={ds.advancedLogsDesc} />

      <SettingsSection overflow="visible">
        <div className="flex flex-wrap gap-3">
          <SettingsSelect
            value={source ?? 'all'}
            options={[
              { value: 'all', label: ds.advancedLogsAllSources },
              { value: 'main', label: ds.advancedLogsMain },
              { value: 'sidecar', label: ds.advancedLogsSidecar },
            ]}
            onChange={(v) => setSource(v as DesktopLogQuery['source'])}
          />
          <SettingsSelect
            value={level ?? 'all'}
            options={[
              { value: 'all', label: ds.advancedLogsAllLevels },
              { value: 'info', label: 'info' },
              { value: 'warn', label: 'warn' },
              { value: 'error', label: 'error' },
              { value: 'debug', label: 'debug' },
              { value: 'other', label: 'other' },
            ]}
            onChange={(v) => setLevel(v as DesktopLogQuery['level'])}
          />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={ds.advancedLogsSearchPlaceholder}
            className={settingsInputCls('sm') + ' h-9 min-w-[200px]'}
          />
          <button
            type="button"
            onClick={() => void loadLogs()}
            className={actionBtnCls()}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <RefreshCw size={14} />
            <span>{ds.advancedRefresh}</span>
          </button>
        </div>
        {error && <p className="mt-3 text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</p>}
      </SettingsSection>

      <div
        ref={termRef}
        className="min-h-[320px] max-h-[600px] overflow-auto rounded-xl p-4"
        style={{
          background: '#0d1117',
          border: '1px solid #21262d',
          fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
        }}
      >
        {loading ? (
          <div className="flex min-h-[180px] items-center justify-center">
            <Loader2 size={18} className="animate-spin" style={{ color: '#8b949e' }} />
          </div>
        ) : logs.length === 0 ? (
          <span style={{ color: '#8b949e', fontSize: 12 }}>{ds.advancedLogsEmpty}</span>
        ) : (
          <div className="flex flex-col">
            {logs.map((entry, i) => (
              <div
                key={`${entry.source}-${entry.timestamp}-${i}`}
                className="whitespace-pre-wrap break-all py-[1px] leading-[20px]"
                style={{ fontSize: 12 }}
              >
                <span style={{ color: '#8b949e' }}>{entry.timestamp}</span>
                <span style={{ color: '#30363d' }}> | </span>
                <span style={{ color: logLevelColor(entry.level), fontWeight: 500 }}>{logLevelTag(entry.level)}</span>
                <span style={{ color: '#30363d' }}> | </span>
                <span style={{ color: '#58a6ff' }}>{entry.source}</span>
                <span style={{ color: '#30363d' }}> | </span>
                <span style={{ color: entry.level === 'error' ? '#f87171' : '#c9d1d9' }}>{entry.message}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// -- Main component --

export function AdvancedSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const { timeZone } = useTimeZone()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const now = useMemo(() => getDateStringInTimeZone(new Date(), timeZone), [timeZone])
  const defaultYear = Number(now.slice(0, 4))
  const defaultMonth = Number(now.slice(5, 7))

  const [activeKey, setActiveKey] = useState<AdvancedKey>('about')
  const [overview, setOverview] = useState<DesktopAdvancedOverview | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [overviewError, setOverviewError] = useState('')

  const loadOverview = useCallback(async () => {
    if (!api?.advanced) return
    setOverviewLoading(true)
    setOverviewError('')
    try {
      const data = await api.advanced.getOverview()
      setOverview(data)
    } catch (err) {
      setOverviewError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setOverviewLoading(false)
    }
  }, [api, t.requestFailed])

  useEffect(() => { void loadOverview() }, [loadOverview])

  const navItems: Array<{ key: AdvancedKey; icon: LucideIcon; label: string }> = [
    { key: 'about', icon: Info, label: ds.about },
    { key: 'network', icon: Network, label: ds.advancedNetwork },
    { key: 'usage', icon: BarChart3, label: ds.advancedUsage },
    { key: 'modules', icon: Package, label: ds.advancedModules },
    { key: 'data', icon: Database, label: ds.advancedData },
    { key: 'logs', icon: ScrollText, label: ds.advancedLogs },
  ]

  return (
    <div className="-m-6 flex min-h-0 min-w-0 overflow-hidden" style={{ height: 'calc(100% + 48px)' }}>
      <div className="flex w-[160px] shrink-0 flex-col overflow-hidden border-r border-[var(--c-border-subtle)] max-[1230px]:w-[140px] xl:w-[180px]">
        <div className="flex-1 overflow-y-auto px-2 py-1">
          <div className="flex flex-col gap-[3px]">
            {navItems.map(({ key, icon: Icon, label }) => (
              <button
                key={key}
                type="button"
                onClick={() => setActiveKey(key)}
                className={[
                  'flex h-[38px] items-center gap-2.5 truncate rounded-lg px-2.5 text-left text-[14px] font-medium transition-all duration-[120ms] active:scale-[0.96]',
                  activeKey === key
                    ? 'rounded-[10px] bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                    : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
                ].join(' ')}
              >
                <Icon size={16} />
                <span>{label}</span>
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="min-w-0 flex-1 overflow-y-auto p-4 max-[1230px]:p-3 sm:p-5">
        <div className="mx-auto min-w-0 max-w-4xl">
          {activeKey === 'about' && (
            <AboutPane
              overview={overview}
              loading={overviewLoading}
              error={overviewError}
            />
          )}
          {activeKey === 'network' && <NetworkPane onReloadOverview={loadOverview} />}
          {activeKey === 'usage' && (
            <UsagePane
              accessToken={accessToken}
              defaultYear={defaultYear}
              defaultMonth={defaultMonth}
            />
          )}
          {activeKey === 'modules' && (
            <div className="flex flex-col gap-6">
              <SettingsSectionHeader title={ds.advancedModules} description={ds.advancedModulesDesc} />
              <ModulesSettings />
            </div>
          )}
          {activeKey === 'data' && (
            <DataPane onReloadOverview={loadOverview} />
          )}
          {activeKey === 'logs' && <LogsPane />}
        </div>
      </div>
    </div>
  )
}
