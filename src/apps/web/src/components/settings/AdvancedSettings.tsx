import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowUpRight,
  BarChart3,
  Blocks,
  CheckCircle2,
  Copy,
  Database,
  Download,
  ExternalLink,
  FileText,
  FolderOpen,
  Github,
  Globe,
  HardDrive,
  Import,
  Loader2,
  Network,
  Package,
  RefreshCw,
  ScrollText,
  Settings2,
  TerminalSquare,
  type LucideIcon,
} from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type {
  DesktopAdvancedOverview,
  DesktopLogEntry,
  DesktopLogLevel,
  DesktopLogQuery,
} from '@arkloop/shared/desktop'
import type {
  MeDailyUsageItem,
  MeModelUsageItem,
  MeUsageSummary,
} from '../../api'
import { getMyDailyUsage, getMyUsage, getMyUsageByModel } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { SettingsSection } from './_SettingsSection'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { ConnectionSettings } from './ConnectionSettings'
import { ModulesSettings } from './ModulesSettings'
import { ExtensionsSettings } from './ExtensionsSettings'
import { UpdateSettingsContent } from './UpdateSettings'

type AdvancedKey = 'overview' | 'data' | 'network' | 'usage' | 'logs' | 'modules' | 'extensions'

type Props = {
  accessToken: string
}

type UsageState = {
  summary: MeUsageSummary | null
  daily: MeDailyUsageItem[]
  byModel: MeModelUsageItem[]
}

function actionButtonCls(disabled?: boolean) {
  return [
    'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition-colors',
    disabled ? 'opacity-50 cursor-not-allowed' : 'hover:bg-[var(--c-bg-deep)]',
  ].join(' ')
}

function cardToneColor(tone?: 'default' | 'success' | 'warning' | 'danger') {
  switch (tone) {
    case 'success':
      return 'var(--c-status-success)'
    case 'warning':
      return 'var(--c-status-warning)'
    case 'danger':
      return 'var(--c-status-error)'
    default:
      return 'var(--c-text-secondary)'
  }
}

function formatUsd(value: number) {
  return `$${value.toFixed(4)}`
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

function LogBadge({ level }: { level: DesktopLogLevel }) {
  const color = level === 'error'
    ? 'var(--c-status-error)'
    : level === 'warn'
      ? 'var(--c-status-warning)'
      : level === 'debug'
        ? 'var(--c-accent)'
        : 'var(--c-text-secondary)'
  return (
    <span
      className="inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium uppercase"
      style={{
        color,
        background: 'color-mix(in srgb, currentColor 12%, transparent)',
      }}
    >
      {level}
    </span>
  )
}

export function AdvancedSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const now = useMemo(() => new Date(), [])
  const defaultYear = now.getUTCFullYear()
  const defaultMonth = now.getUTCMonth() + 1
  const [activeKey, setActiveKey] = useState<AdvancedKey>('overview')
  const [overview, setOverview] = useState<DesktopAdvancedOverview | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [overviewError, setOverviewError] = useState('')
  const [usage, setUsage] = useState<UsageState>({ summary: null, daily: [], byModel: [] })
  const [usageLoading, setUsageLoading] = useState(false)
  const [usageError, setUsageError] = useState('')
  const [filterYear, setFilterYear] = useState(defaultYear)
  const [filterMonth, setFilterMonth] = useState(defaultMonth)
  const [logs, setLogs] = useState<DesktopLogEntry[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [logsError, setLogsError] = useState('')
  const [logSource, setLogSource] = useState<DesktopLogQuery['source']>('all')
  const [logLevel, setLogLevel] = useState<DesktopLogQuery['level']>('all')
  const [logSearch, setLogSearch] = useState('')
  const [dataFolder, setDataFolder] = useState<string | null>(null)
  const [dataActionError, setDataActionError] = useState('')
  const [dataActionInfo, setDataActionInfo] = useState('')
  const [dataActionLoading, setDataActionLoading] = useState<'choose' | 'export' | 'import' | null>(null)

  const loadOverview = useCallback(async () => {
    if (!api?.advanced) return
    setOverviewLoading(true)
    setOverviewError('')
    try {
      const data = await api.advanced.getOverview()
      setOverview(data)
    } catch (error) {
      setOverviewError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setOverviewLoading(false)
    }
  }, [api, t.requestFailed])

  const loadUsage = useCallback(async () => {
    setUsageLoading(true)
    setUsageError('')
    try {
      const monthStart = `${filterYear}-${String(filterMonth).padStart(2, '0')}-01`
      const monthEndDate = new Date(Date.UTC(filterMonth === 12 ? filterYear + 1 : filterYear, filterMonth === 12 ? 0 : filterMonth, 1))
      const monthEnd = monthEndDate.toISOString().slice(0, 10)
      const [summary, daily, byModel] = await Promise.all([
        getMyUsage(accessToken, filterYear, filterMonth),
        getMyDailyUsage(accessToken, monthStart, monthEnd),
        getMyUsageByModel(accessToken, filterYear, filterMonth),
      ])
      setUsage({ summary, daily, byModel })
    } catch (error) {
      setUsageError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setUsageLoading(false)
    }
  }, [accessToken, filterMonth, filterYear, t.requestFailed])

  const loadLogs = useCallback(async () => {
    if (!api?.advanced) return
    setLogsLoading(true)
    setLogsError('')
    try {
      const result = await api.advanced.listLogs({
        source: logSource,
        level: logLevel,
        search: logSearch,
        limit: 200,
      })
      setLogs(result.entries)
    } catch (error) {
      setLogsError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setLogsLoading(false)
    }
  }, [api, logLevel, logSearch, logSource, t.requestFailed])

  useEffect(() => {
    void loadOverview()
  }, [loadOverview])

  useEffect(() => {
    if (activeKey !== 'usage') return
    void loadUsage()
  }, [activeKey, loadUsage])

  useEffect(() => {
    if (activeKey !== 'logs') return
    void loadLogs()
  }, [activeKey, loadLogs])

  const navItems: Array<{ key: AdvancedKey; icon: LucideIcon; label: string }> = [
    { key: 'overview', icon: Settings2, label: ds.advancedOverview },
    { key: 'data', icon: Database, label: ds.advancedData },
    { key: 'network', icon: Network, label: ds.advancedNetwork },
    { key: 'usage', icon: BarChart3, label: ds.advancedUsage },
    { key: 'logs', icon: ScrollText, label: ds.advancedLogs },
    { key: 'modules', icon: Package, label: ds.advancedModules },
    { key: 'extensions', icon: Blocks, label: ds.advancedExtensions },
  ]

  const copyText = useCallback(async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      window.prompt('Copy', value)
    }
  }, [])

  const handleChooseDataFolder = useCallback(async () => {
    if (!api?.advanced) return
    setDataActionLoading('choose')
    setDataActionError('')
    try {
      const selected = await api.advanced.chooseDataFolder()
      setDataFolder(selected)
      setDataActionInfo(selected ? `${ds.advancedSelectedFolder}: ${selected}` : '')
    } catch (error) {
      setDataActionError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setDataActionLoading(null)
    }
  }, [api, ds.advancedSelectedFolder, t.requestFailed])

  const handleExportData = useCallback(async () => {
    if (!api?.advanced) return
    setDataActionLoading('export')
    setDataActionError('')
    try {
      const result = await api.advanced.exportDataBundle()
      setDataActionInfo(`${ds.advancedExportDone}: ${result.filePath}`)
    } catch (error) {
      setDataActionError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setDataActionLoading(null)
    }
  }, [api, ds.advancedExportDone, t.requestFailed])

  const handleImportData = useCallback(async () => {
    if (!api?.advanced) return
    setDataActionLoading('import')
    setDataActionError('')
    try {
      const result = await api.advanced.importDataBundle()
      setDataActionInfo(`${ds.advancedImportDone}: ${result.importedFrom}`)
      await loadOverview()
    } catch (error) {
      setDataActionError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setDataActionLoading(null)
    }
  }, [api, ds.advancedImportDone, loadOverview, t.requestFailed])

  return (
    <div className="flex min-h-0 gap-6">
      <div
        className="w-[220px] shrink-0 rounded-2xl p-3"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-sidebar)' }}
      >
        <div className="mb-3 px-2">
          <div className="text-sm font-semibold text-[var(--c-text-heading)]">{ds.advancedTitle}</div>
          <div className="mt-1 text-xs text-[var(--c-text-muted)]">{ds.advancedDesc}</div>
        </div>
        <div className="flex flex-col gap-1">
          {navItems.map(({ key, icon: Icon, label }) => (
            <button
              key={key}
              type="button"
              onClick={() => setActiveKey(key)}
              className={[
                'flex h-9 items-center gap-2.5 rounded-xl px-3 text-sm transition-colors',
                activeKey === key
                  ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                  : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
              ].join(' ')}
            >
              <Icon size={16} />
              <span>{label}</span>
            </button>
          ))}
        </div>
      </div>

      <div className="min-w-0 flex-1">
        {activeKey === 'overview' && (
          <div className="flex flex-col gap-6">
            <SettingsSectionHeader title={ds.advancedOverview} description={ds.advancedOverviewDesc} />
            {overviewLoading ? (
              <div className="flex min-h-[220px] items-center justify-center">
                <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
              </div>
            ) : overviewError ? (
              <SettingsSection>
                <p className="text-sm text-[var(--c-status-error)]">{overviewError}</p>
              </SettingsSection>
            ) : overview && (
              <>
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
                      <div className="mt-1 text-sm text-[var(--c-text-secondary)]">{ds.appVersion}: {overview.appVersion}</div>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {overview.links.map((link) => (
                        <button
                          key={link.url}
                          type="button"
                          onClick={() => openExternal(link.url)}
                          className={actionButtonCls()}
                          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                        >
                          {link.label === 'GitHub' ? <Github size={14} /> : <ExternalLink size={14} />}
                          <span>{link.label}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                </SettingsSection>

                <div className="grid gap-4 md:grid-cols-2">
                  {overview.status.map((item) => (
                    <SettingsSection key={item.label}>
                      <div className="text-xs text-[var(--c-text-muted)]">{item.label}</div>
                      <div className="mt-2 text-sm font-medium" style={{ color: cardToneColor(item.tone) }}>
                        {item.value}
                      </div>
                    </SettingsSection>
                  ))}
                </div>

                <SettingsSection>
                  <div className="grid gap-4 md:grid-cols-2">
                    <PathRow label={ds.advancedConfigPath} value={overview.configPath} onCopy={copyText} />
                    <PathRow label={ds.advancedDataDir} value={overview.dataDir} onCopy={copyText} />
                    <PathRow label={ds.advancedLogsDir} value={overview.logsDir} onCopy={copyText} />
                    <PathRow label={ds.advancedDatabasePath} value={overview.sqlitePath} onCopy={copyText} />
                  </div>
                </SettingsSection>

                <SettingsSection>
                  <div className="flex items-center justify-between gap-3">
                    <SettingsSectionHeader title={ds.advancedUpdateCenter} description={ds.advancedUpdateCenterDesc} />
                    <button
                      type="button"
                      onClick={() => void loadOverview()}
                      className={actionButtonCls()}
                      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                    >
                      <RefreshCw size={14} />
                      <span>{ds.advancedRefresh}</span>
                    </button>
                  </div>
                  <div className="mt-4">
                    <UpdateSettingsContent />
                  </div>
                </SettingsSection>

                {overview.usage && (
                  <SettingsSection>
                    <SettingsSectionHeader title={ds.advancedUsageSnapshot} />
                    <div className="mt-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                      <MetricCard label={ds.advancedUsageCost} value={formatUsd(overview.usage.total_cost_usd)} icon={Globe} />
                      <MetricCard label={ds.advancedUsageMessages} value={formatNumber(overview.usage.record_count)} icon={TerminalSquare} />
                      <MetricCard label={ds.advancedUsageInput} value={formatNumber(overview.usage.total_input_tokens)} icon={Download} />
                      <MetricCard label={ds.advancedUsageOutput} value={formatNumber(overview.usage.total_output_tokens)} icon={ArrowUpRight} />
                    </div>
                  </SettingsSection>
                )}
              </>
            )}
          </div>
        )}

        {activeKey === 'data' && (
          <div className="flex flex-col gap-6">
            <SettingsSectionHeader title={ds.advancedData} description={ds.advancedDataDesc} />
            <SettingsSection>
              <div className="grid gap-4 md:grid-cols-2">
                <PathRow label={ds.advancedConfigPath} value={overview?.configPath ?? '-'} onCopy={copyText} />
                <PathRow label={ds.advancedDatabasePath} value={overview?.sqlitePath ?? '-'} onCopy={copyText} />
                <PathRow label={ds.advancedLogsDir} value={overview?.logsDir ?? '-'} onCopy={copyText} />
                <PathRow label={ds.advancedDataDir} value={overview?.dataDir ?? '-'} onCopy={copyText} />
              </div>
            </SettingsSection>
            <SettingsSection>
              <div className="flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={() => void handleChooseDataFolder()}
                  disabled={dataActionLoading !== null}
                  className={actionButtonCls(dataActionLoading !== null)}
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <FolderOpen size={14} />
                  <span>{ds.advancedChooseFolder}</span>
                </button>
                <button
                  type="button"
                  onClick={() => void handleExportData()}
                  disabled={dataActionLoading !== null}
                  className={actionButtonCls(dataActionLoading !== null)}
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <Download size={14} />
                  <span>{ds.advancedExport}</span>
                </button>
                <button
                  type="button"
                  onClick={() => void handleImportData()}
                  disabled={dataActionLoading !== null}
                  className={actionButtonCls(dataActionLoading !== null)}
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <Import size={14} />
                  <span>{ds.advancedImport}</span>
                </button>
              </div>
              {(dataFolder || dataActionInfo) && (
                <p className="mt-4 text-sm text-[var(--c-text-secondary)]">{dataActionInfo || dataFolder}</p>
              )}
              {dataActionError && (
                <p className="mt-2 text-sm text-[var(--c-status-error)]">{dataActionError}</p>
              )}
            </SettingsSection>
          </div>
        )}

        {activeKey === 'network' && (
          <div className="flex flex-col gap-6">
            <SettingsSectionHeader title={ds.advancedNetwork} description={ds.advancedNetworkDesc} />
            <SettingsSection>
              <NetworkAdvancedPanel onReloadOverview={loadOverview} />
            </SettingsSection>
            <ConnectionSettings />
          </div>
        )}

        {activeKey === 'usage' && (
          <div className="flex flex-col gap-6">
            <div className="flex items-end justify-between gap-3">
              <SettingsSectionHeader title={ds.advancedUsage} description={ds.advancedUsageDesc} />
              <div className="flex items-center gap-2">
                <select
                  value={filterYear}
                  onChange={(e) => setFilterYear(Number(e.target.value))}
                  className="h-9 rounded-lg px-3 text-sm"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  {Array.from({ length: 3 }, (_, index) => defaultYear - index).map((year) => (
                    <option key={year} value={year}>{year}</option>
                  ))}
                </select>
                <select
                  value={filterMonth}
                  onChange={(e) => setFilterMonth(Number(e.target.value))}
                  className="h-9 rounded-lg px-3 text-sm"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  {Array.from({ length: 12 }, (_, index) => index + 1).map((month) => (
                    <option key={month} value={month}>{month}</option>
                  ))}
                </select>
                <button
                  type="button"
                  onClick={() => void loadUsage()}
                  className={actionButtonCls()}
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <RefreshCw size={14} />
                  <span>{ds.advancedRefresh}</span>
                </button>
              </div>
            </div>

            {usageError && (
              <SettingsSection>
                <p className="text-sm text-[var(--c-status-error)]">{usageError}</p>
              </SettingsSection>
            )}

            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <MetricCard label={ds.advancedUsageCost} value={usageLoading ? '...' : formatUsd(usage.summary?.total_cost_usd ?? 0)} icon={Globe} />
              <MetricCard label={ds.advancedUsageMessages} value={usageLoading ? '...' : formatNumber(usage.summary?.record_count ?? 0)} icon={TerminalSquare} />
              <MetricCard label={ds.advancedUsageInput} value={usageLoading ? '...' : formatNumber(usage.summary?.total_input_tokens ?? 0)} icon={Download} />
              <MetricCard label={ds.advancedUsageOutput} value={usageLoading ? '...' : formatNumber(usage.summary?.total_output_tokens ?? 0)} icon={ArrowUpRight} />
              <MetricCard label={ds.advancedUsageCacheRead} value={usageLoading ? '...' : formatNumber(usage.summary?.total_cache_read_tokens ?? 0)} icon={FileText} />
              <MetricCard label={ds.advancedUsageCacheWrite} value={usageLoading ? '...' : formatNumber(usage.summary?.total_cache_creation_tokens ?? 0)} icon={Database} />
              <MetricCard label={ds.advancedUsageCacheSaved} value={usageLoading ? '...' : formatNumber(usage.summary?.total_cached_tokens ?? 0)} icon={CheckCircle2} />
              <MetricCard label={ds.advancedUsageMonth} value={`${filterYear}-${String(filterMonth).padStart(2, '0')}`} icon={BarChart3} />
            </div>

            <SettingsSection>
              <SettingsSectionHeader title={ds.advancedUsageByModel} />
              <UsageTable
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
                headers={[
                  ds.advancedUsageModel,
                  ds.advancedUsageCost,
                  ds.advancedUsageTokens,
                  ds.advancedUsageCache,
                  ds.advancedUsageMessages,
                ]}
                emptyText={ds.advancedUsageEmpty}
              />
            </SettingsSection>

            <SettingsSection>
              <SettingsSectionHeader title={ds.advancedUsageDaily} />
              <UsageTable
                rows={usage.daily.map((row) => ({
                  key: row.date,
                  columns: [
                    row.date,
                    formatUsd(row.cost_usd),
                    `${formatNumber(row.input_tokens)} / ${formatNumber(row.output_tokens)}`,
                    formatNumber(row.record_count),
                  ],
                }))}
                headers={[
                  ds.advancedUsageDate,
                  ds.advancedUsageCost,
                  ds.advancedUsageTokens,
                  ds.advancedUsageMessages,
                ]}
                emptyText={ds.advancedUsageEmpty}
              />
            </SettingsSection>
          </div>
        )}

        {activeKey === 'logs' && (
          <div className="flex flex-col gap-6">
            <SettingsSectionHeader title={ds.advancedLogs} description={ds.advancedLogsDesc} />
            <SettingsSection>
              <div className="flex flex-wrap gap-3">
                <select
                  value={logSource}
                  onChange={(e) => setLogSource(e.target.value as DesktopLogQuery['source'])}
                  className="h-9 rounded-lg px-3 text-sm"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <option value="all">{ds.advancedLogsAllSources}</option>
                  <option value="main">{ds.advancedLogsMain}</option>
                  <option value="sidecar">{ds.advancedLogsSidecar}</option>
                </select>
                <select
                  value={logLevel}
                  onChange={(e) => setLogLevel(e.target.value as DesktopLogQuery['level'])}
                  className="h-9 rounded-lg px-3 text-sm"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <option value="all">{ds.advancedLogsAllLevels}</option>
                  <option value="info">info</option>
                  <option value="warn">warn</option>
                  <option value="error">error</option>
                  <option value="debug">debug</option>
                  <option value="other">other</option>
                </select>
                <input
                  type="text"
                  value={logSearch}
                  onChange={(e) => setLogSearch(e.target.value)}
                  placeholder={ds.advancedLogsSearchPlaceholder}
                  className="h-9 min-w-[240px] rounded-lg px-3 text-sm outline-none"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                />
                <button
                  type="button"
                  onClick={() => void loadLogs()}
                  className={actionButtonCls()}
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <RefreshCw size={14} />
                  <span>{ds.advancedRefresh}</span>
                </button>
              </div>
              {logsError && (
                <p className="mt-3 text-sm text-[var(--c-status-error)]">{logsError}</p>
              )}
            </SettingsSection>
            <SettingsSection>
              <div className="flex flex-col gap-3">
                {logsLoading ? (
                  <div className="flex min-h-[180px] items-center justify-center">
                    <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
                  </div>
                ) : logs.length === 0 ? (
                  <p className="text-sm text-[var(--c-text-muted)]">{ds.advancedLogsEmpty}</p>
                ) : (
                  logs.map((entry, index) => (
                    <div
                      key={`${entry.source}-${entry.timestamp}-${index}`}
                      className="rounded-xl px-4 py-3"
                      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                    >
                      <div className="mb-2 flex flex-wrap items-center gap-2 text-xs text-[var(--c-text-muted)]">
                        <LogBadge level={entry.level} />
                        <span>{entry.source}</span>
                        <span>{entry.timestamp}</span>
                      </div>
                      <pre className="whitespace-pre-wrap break-words text-xs text-[var(--c-text-primary)]">{entry.message}</pre>
                    </div>
                  ))
                )}
              </div>
            </SettingsSection>
          </div>
        )}

        {activeKey === 'modules' && (
          <div className="flex flex-col gap-6">
            <SettingsSectionHeader title={ds.advancedModules} description={ds.advancedModulesDesc} />
            <ModulesSettings />
          </div>
        )}

        {activeKey === 'extensions' && (
          <div className="flex flex-col gap-6">
            <SettingsSectionHeader title={ds.advancedExtensions} description={ds.advancedExtensionsDesc} />
            <ExtensionsSettings />
          </div>
        )}
      </div>
    </div>
  )
}

function MetricCard({
  label,
  value,
  icon: Icon,
}: {
  label: string
  value: string
  icon: typeof Globe
}) {
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

function PathRow({
  label,
  value,
  onCopy,
}: {
  label: string
  value: string
  onCopy: (value: string) => void
}) {
  return (
    <div
      className="rounded-xl px-4 py-3"
      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
    >
      <div className="mb-2 text-xs text-[var(--c-text-muted)]">{label}</div>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate text-xs text-[var(--c-text-primary)]">{value}</code>
        <button
          type="button"
          onClick={() => onCopy(value)}
          className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)]"
        >
          <Copy size={13} />
        </button>
      </div>
    </div>
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
    <div
      className="overflow-auto rounded-xl"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      <table className="w-full text-sm">
        <thead style={{ background: 'var(--c-bg-page)' }}>
          <tr>
            {headers.map((header) => (
              <th key={header} className="px-4 py-3 text-left text-xs font-medium text-[var(--c-text-muted)]">
                {header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key} style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
              {row.columns.map((column, index) => (
                <td key={`${row.key}-${index}`} className="px-4 py-3 text-[var(--c-text-primary)]">
                  {column}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function NetworkAdvancedPanel({ onReloadOverview }: { onReloadOverview: () => Promise<void> }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const [config, setConfig] = useState(() => ({
    proxyEnabled: false,
    proxyUrl: '',
    requestTimeoutMs: 30000,
    retryCount: 1,
    userAgent: '',
  }))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

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
    } catch (error) {
      setError(error instanceof Error ? error.message : t.requestFailed)
    } finally {
      setSaving(false)
      window.setTimeout(() => setSaved(false), 2000)
    }
  }, [api, config, onReloadOverview, t.requestFailed])

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 md:grid-cols-2">
        <label className="flex flex-col gap-2 text-sm text-[var(--c-text-secondary)]">
          <span>{ds.advancedNetworkProxyEnable}</span>
          <select
            value={config.proxyEnabled ? 'true' : 'false'}
            onChange={(e) => setConfig((prev) => ({ ...prev, proxyEnabled: e.target.value === 'true' }))}
            className="h-10 rounded-xl px-3 text-sm text-[var(--c-text-primary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            <option value="false">{ds.advancedDisabled}</option>
            <option value="true">{ds.advancedEnabled}</option>
          </select>
        </label>
        <label className="flex flex-col gap-2 text-sm text-[var(--c-text-secondary)]">
          <span>{ds.advancedNetworkProxyUrl}</span>
          <input
            value={config.proxyUrl}
            onChange={(e) => setConfig((prev) => ({ ...prev, proxyUrl: e.target.value }))}
            className="h-10 rounded-xl px-3 text-sm text-[var(--c-text-primary)] outline-none"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            placeholder="http://127.0.0.1:7890"
          />
        </label>
        <label className="flex flex-col gap-2 text-sm text-[var(--c-text-secondary)]">
          <span>{ds.advancedNetworkTimeout}</span>
          <input
            type="number"
            min={1000}
            max={300000}
            value={config.requestTimeoutMs}
            onChange={(e) => setConfig((prev) => ({ ...prev, requestTimeoutMs: Number(e.target.value) || 30000 }))}
            className="h-10 rounded-xl px-3 text-sm text-[var(--c-text-primary)] outline-none"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          />
        </label>
        <label className="flex flex-col gap-2 text-sm text-[var(--c-text-secondary)]">
          <span>{ds.advancedNetworkRetry}</span>
          <input
            type="number"
            min={0}
            max={10}
            value={config.retryCount}
            onChange={(e) => setConfig((prev) => ({ ...prev, retryCount: Number(e.target.value) || 0 }))}
            className="h-10 rounded-xl px-3 text-sm text-[var(--c-text-primary)] outline-none"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          />
        </label>
      </div>

      <label className="flex flex-col gap-2 text-sm text-[var(--c-text-secondary)]">
        <span>{ds.advancedNetworkUserAgent}</span>
        <input
          value={config.userAgent}
          onChange={(e) => setConfig((prev) => ({ ...prev, userAgent: e.target.value }))}
          className="h-10 rounded-xl px-3 text-sm text-[var(--c-text-primary)] outline-none"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          placeholder="Arkloop Desktop"
        />
      </label>

      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => void handleSave()}
          disabled={saving}
          className={actionButtonCls(saving)}
          style={{
            background: 'var(--c-accent)',
            color: 'var(--c-accent-fg)',
          }}
        >
          {saving ? <Loader2 size={14} className="animate-spin" /> : <CheckCircle2 size={14} />}
          <span>{saving ? ds.advancedSaving : ds.advancedSave}</span>
        </button>
        {saved && <span className="text-sm text-[var(--c-status-success)]">{ds.advancedSaved}</span>}
        {error && <span className="text-sm text-[var(--c-status-error)]">{error}</span>}
      </div>
    </div>
  )
}
