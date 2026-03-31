import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowUpRight,
  BarChart3,
  CheckCircle2,
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
  type LucideIcon,
} from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { useToast } from '@arkloop/shared'
import type {
  DesktopAdvancedOverview,
  DesktopLogEntry,
  DesktopLogLevel,
  DesktopLogQuery,
} from '@arkloop/shared/desktop'
import type { MeDailyUsageItem, MeModelUsageItem, MeUsageSummary } from '../../api'
import { getMyDailyUsage, getMyUsage, getMyUsageByModel } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { SettingsSection } from './_SettingsSection'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { settingsInputCls } from './_SettingsInput'
import { SettingsLabel } from './_SettingsLabel'
import { SettingsSelect } from './_SettingsSelect'
import { ConnectionSettings } from './ConnectionSettings'
import { ModulesSettings } from './ModulesSettings'
import { UpdateSettingsContent } from './UpdateSettings'

type AdvancedKey = 'about' | 'network' | 'usage' | 'modules' | 'data' | 'logs'

type Props = { accessToken: string }

type UsageState = {
  summary: MeUsageSummary | null
  daily: MeDailyUsageItem[]
  byModel: MeModelUsageItem[]
}

function formatUsd(value: number) {
  return `$${value.toFixed(4)}`
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
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

// -- Shared small components --

function MetricCard({ label, value, icon: Icon }: { label: string; value: string; icon: typeof Globe }) {
  return (
    <SettingsSection className="h-full">
      <div className="flex items-center gap-2 text-[var(--c-text-secondary)]">
        <Icon size={15} />
        <span className="text-xs">{label}</span>
      </div>
      <div className="mt-3 text-xl font-semibold text-[var(--c-text-heading)]">{value}</div>
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
    <div className="overflow-auto rounded-xl border border-[var(--c-border-subtle)]">
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

function LogBadge({ level }: { level: DesktopLogLevel }) {
  const color =
    level === 'error' ? 'var(--c-status-error)'
    : level === 'warn' ? 'var(--c-status-warning)'
    : level === 'debug' ? 'var(--c-accent)'
    : 'var(--c-text-secondary)'
  return (
    <span
      className="inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium uppercase"
      style={{ color, background: 'color-mix(in srgb, currentColor 12%, transparent)' }}
    >
      {level}
    </span>
  )
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
            {overview.iconPath ? (
              <img src={`file://${overview.iconPath}`} alt={overview.appName} className="h-full w-full object-cover" />
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
                className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
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
  const ds = t.desktopSettings
  const [usage, setUsage] = useState<UsageState>({ summary: null, daily: [], byModel: [] })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [year, setYear] = useState(defaultYear)
  const [month, setMonth] = useState(defaultMonth)

  const loadUsage = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const monthStart = `${year}-${String(month).padStart(2, '0')}-01`
      const nextMonth = new Date(Date.UTC(month === 12 ? year + 1 : year, month === 12 ? 0 : month, 1))
      const monthEnd = nextMonth.toISOString().slice(0, 10)
      const [summary, daily, byModel] = await Promise.all([
        getMyUsage(accessToken, year, month),
        getMyDailyUsage(accessToken, monthStart, monthEnd),
        getMyUsageByModel(accessToken, year, month),
      ])
      setUsage({ summary, daily, byModel })
    } catch (err) {
      setError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, year, month, t.requestFailed])

  useEffect(() => { void loadUsage() }, [loadUsage])

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-end justify-between gap-3">
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
          <button
            type="button"
            onClick={() => void loadUsage()}
            className={actionBtnCls()}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <RefreshCw size={14} />
            <span>{ds.advancedRefresh}</span>
          </button>
        </div>
      </div>

      {error && (
        <SettingsSection>
          <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</p>
        </SettingsSection>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label={ds.advancedUsageCost} value={loading ? '...' : formatUsd(usage.summary?.total_cost_usd ?? 0)} icon={Globe} />
        <MetricCard label={ds.advancedUsageMessages} value={loading ? '...' : formatNumber(usage.summary?.record_count ?? 0)} icon={TerminalSquare} />
        <MetricCard label={ds.advancedUsageInput} value={loading ? '...' : formatNumber(usage.summary?.total_input_tokens ?? 0)} icon={Download} />
        <MetricCard label={ds.advancedUsageOutput} value={loading ? '...' : formatNumber(usage.summary?.total_output_tokens ?? 0)} icon={ArrowUpRight} />
        <MetricCard label={ds.advancedUsageCacheRead} value={loading ? '...' : formatNumber(usage.summary?.total_cache_read_tokens ?? 0)} icon={FileText} />
        <MetricCard label={ds.advancedUsageCacheWrite} value={loading ? '...' : formatNumber(usage.summary?.total_cache_creation_tokens ?? 0)} icon={Database} />
        <MetricCard label={ds.advancedUsageCacheSaved} value={loading ? '...' : formatNumber(usage.summary?.total_cached_tokens ?? 0)} icon={CheckCircle2} />
        <MetricCard label={ds.advancedUsageMonth} value={`${year}-${String(month).padStart(2, '0')}`} icon={BarChart3} />
      </div>

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
    </div>
  )
}

function DataPane({ onReloadOverview }: { onReloadOverview: () => Promise<void> }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const { addToast } = useToast()
  const [actionLoading, setActionLoading] = useState<'choose' | 'export' | 'import' | null>(null)
  const [actionError, setActionError] = useState('')

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
    setActionLoading('export')
    setActionError('')
    try {
      await api.advanced.exportDataBundle()
      addToast(ds.advancedExportDone, 'success')
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, ds.advancedExportDone, t.requestFailed])

  const handleImport = useCallback(async () => {
    if (!api?.advanced) return
    setActionLoading('import')
    setActionError('')
    try {
      await api.advanced.importDataBundle()
      addToast(ds.advancedImportDone, 'success')
      await onReloadOverview()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, ds.advancedImportDone, onReloadOverview, t.requestFailed])

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
            onClick={() => void handleExport()}
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

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedLogs} description={ds.advancedLogsDesc} />

      <SettingsSection>
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

      <SettingsSection>
        {loading ? (
          <div className="flex min-h-[180px] items-center justify-center">
            <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : logs.length === 0 ? (
          <p className="text-sm text-[var(--c-text-muted)]">{ds.advancedLogsEmpty}</p>
        ) : (
          <div className="flex flex-col gap-3">
            {logs.map((entry, i) => (
              <div
                key={`${entry.source}-${entry.timestamp}-${i}`}
                className="rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-page)] px-4 py-3"
              >
                <div className="mb-2 flex flex-wrap items-center gap-2 text-xs text-[var(--c-text-muted)]">
                  <LogBadge level={entry.level} />
                  <span>{entry.source}</span>
                  <span>{entry.timestamp}</span>
                </div>
                <pre className="whitespace-pre-wrap break-words text-xs text-[var(--c-text-primary)]">{entry.message}</pre>
              </div>
            ))}
          </div>
        )}
      </SettingsSection>
    </div>
  )
}

// -- Main component --

export function AdvancedSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const now = useMemo(() => new Date(), [])
  const defaultYear = now.getUTCFullYear()
  const defaultMonth = now.getUTCMonth() + 1

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
  )
}
