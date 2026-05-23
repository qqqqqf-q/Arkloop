import { useCallback, useEffect, useMemo, useState } from 'react'
import { Download, Loader2, Pause, Play, RefreshCw } from 'lucide-react'
import { useToast } from '@arkloop/shared'
import {
  checkPluginRuntime,
  getPluginEnablement,
  installPluginRuntime,
  listPlugins,
  setPluginEnabled,
  updatePluginSettings,
  triggerActivityRecorderBuilder,
  type PluginEnablement,
  type PluginPackage,
  type PluginRuntimeState,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { SettingsButton, SettingsIconButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'
import { SettingsCard, SettingsGroup, SettingsPage, SettingsRow } from './_SettingsLayout'
import { SettingsSwitch } from './_SettingsSwitch'

const activityRecorderPluginID = 'arkloop.plugins.activity-recorder'

type ActivityRecorderStatus = {
  plugin: PluginPackage | null
  enablement: PluginEnablement | null
  runtime: PluginRuntimeState | null
}

type BusyAction = 'install' | 'toggle' | 'refresh' | 'settings' | 'build' | null

type SourceView = {
  key: string
  label: string
  setting: keyof Pick<RecorderSettings, 'enable_activity_record'>
  kind: 'context'
  daemonKeys?: string[]
}

const sources: SourceView[] = [
  { key: 'activity-record', label: 'Activity Record', setting: 'enable_activity_record', kind: 'context' },
]

type RecorderMode = 'lightweight' | 'full' | 'custom'

type RecorderSettings = {
  mode: RecorderMode
  enable_activity_record: boolean
  snapshot_compaction: boolean
  builder_interval_min: number
}

const defaultRecorderSettings: RecorderSettings = {
  mode: 'lightweight',
  enable_activity_record: true,
  snapshot_compaction: false,
  builder_interval_min: 300,
}

function toBool(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function toNumber(value: unknown, fallback: number): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const next = Number(value)
    if (Number.isFinite(next)) return next
  }
  return fallback
}

function currentSettings(enablement: PluginEnablement | null): RecorderSettings {
  const raw = enablement?.settings ?? {}
  const mode = raw.mode === 'full' || raw.mode === 'custom' || raw.mode === 'lightweight'
    ? raw.mode
    : defaultRecorderSettings.mode
  return {
    mode,
    enable_activity_record: toBool(raw.enable_activity_record, defaultRecorderSettings.enable_activity_record),
    snapshot_compaction: toBool(raw.snapshot_compaction, defaultRecorderSettings.snapshot_compaction),
    builder_interval_min: toNumber(raw.builder_interval_min, defaultRecorderSettings.builder_interval_min),
  }
}

function runtimeValue(runtime: PluginRuntimeState | null, key: string): string {
  const value = runtime?.status_json?.[key]
  if (value === undefined || value === null) return ''
  return String(value)
}

function runtimeBool(runtime: PluginRuntimeState | null, key: string): boolean | null {
  const value = runtime?.status_json?.[key]
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') {
    if (value === 'true') return true
    if (value === 'false') return false
  }
  return null
}

function runtimeNumber(runtime: PluginRuntimeState | null, key: string): number {
  const value = runtime?.status_json?.[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return 0
}

function daemonStatus(runtime: PluginRuntimeState | null, key: string): string {
  return runtimeValue(runtime, `${key}.daemon.status`) || 'unknown'
}

function sourceStatus(runtime: PluginRuntimeState | null, source: SourceView): string {
  if (source.key === 'activity-record') {
    const syncStatus = runtimeValue(runtime, 'activity_record.sync.status')
    if (syncStatus === 'running' || syncStatus === 'starting') return 'starting'
    if (syncStatus === 'error') return 'error'
    if (runtimeNumber(runtime, 'activity_record.db_records') > 0) return 'ready'
    return 'setup'
  }
  const keys = source.daemonKeys ?? []
  if (keys.length === 0) return ''
  const statuses = keys.map((key) => daemonStatus(runtime, key))
  if (statuses.every((status) => status === 'running')) return 'running'
  if (statuses.some((status) => status === 'running')) return 'partial'
  if (statuses.some((status) => status === 'starting')) return 'starting'
  if (statuses.some((status) => status === 'unknown')) return 'unknown'
  if (statuses.some((status) => status === 'error')) return 'error'
  if (statuses.every((status) => status === 'disabled')) return 'disabled'
  return statuses[0] ?? 'unknown'
}

function sourceTone(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'running' || status === 'ready') return 'success'
  if (status === 'partial' || status === 'starting' || status === 'unknown' || status === 'setup' || status === 'permission') return 'warning'
  if (status === 'stopped' || status === 'error') return 'error'
  return 'neutral'
}

function StatusPill({ children, tone = 'neutral' }: { children: string; tone?: 'success' | 'warning' | 'error' | 'neutral' }) {
  const colors = {
    success: 'bg-[color-mix(in_srgb,var(--c-status-success-text)_12%,var(--c-bg-input))] text-[var(--c-status-success-text)]',
    warning: 'bg-[color-mix(in_srgb,var(--c-status-warning-text)_12%,var(--c-bg-input))] text-[var(--c-status-warning-text)]',
    error: 'bg-[color-mix(in_srgb,var(--c-status-error-text)_12%,var(--c-bg-input))] text-[var(--c-status-error-text)]',
    neutral: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',
  }[tone]
  return (
    <span className={`inline-flex h-6 items-center rounded-md px-2 text-[11px] font-medium ${colors}`}>
      {children}
    </span>
  )
}

function SourceRow({
  source,
  runtime,
  settings,
  copy,
  disabled,
  onToggle,
}: {
  source: SourceView
  runtime: PluginRuntimeState | null
  settings: RecorderSettings
  copy: ReturnType<typeof useLocale>['t']['desktopSettings']['activityRecorderPage']
  disabled: boolean
  onToggle: (patch: Partial<RecorderSettings>) => void
}) {
  const status = sourceStatus(runtime, source)
  const checked = settings[source.setting]
  const showStatus = checked && (source.key === 'activity-record' || Boolean(source.daemonKeys && status !== 'running'))
  const description = {
    activity: copy.activitySource,
    context: copy.contextSource,
    tool: copy.toolSource,
  }[source.kind]
  return (
    <SettingsRow
      title={source.label}
      description={description}
      control={(
        <div className="flex items-center gap-3">
          {showStatus ? <StatusPill tone={sourceTone(status)}>{copy.statusLabels[status] ?? status}</StatusPill> : null}
          <SettingsSwitch
            checked={checked}
            disabled={disabled}
            onChange={(value) => onToggle({ [source.setting]: value, mode: 'custom' } as Partial<RecorderSettings>)}
          />
        </div>
      )}
    />
  )
}

export function ActivityRecorderSettings({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const { addToast } = useToast()
  const copy = t.desktopSettings.activityRecorderPage
  const [status, setStatus] = useState<ActivityRecorderStatus>({ plugin: null, enablement: null, runtime: null })
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<BusyAction>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const plugins = await listPlugins(accessToken)
      const plugin = plugins.find((item) => item.id === activityRecorderPluginID) ?? null
      if (!plugin) {
        setStatus({ plugin: null, enablement: null, runtime: null })
        return
      }
      const [enablement, runtime] = await Promise.all([
        getPluginEnablement(accessToken, activityRecorderPluginID),
        checkPluginRuntime(accessToken, activityRecorderPluginID),
      ])
      setStatus({ plugin, enablement, runtime })
    } catch (error) {
      addToast(error instanceof Error ? error.message : copy.loadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, copy.loadFailed])

  useEffect(() => {
    void load()
  }, [load])

  const enabled = status.enablement?.enabled ?? false
  const runtimeReady = status.runtime?.status === 'installed'
  const settings = useMemo(() => currentSettings(status.enablement), [status.enablement])
  const runningCount = useMemo(
    () => sources.filter((source) => source.daemonKeys && settings[source.setting] && sourceStatus(status.runtime, source) === 'running').length,
    [settings, status.runtime],
  )
  const daemonSourceCount = useMemo(
    () => sources.filter((source) => source.daemonKeys && settings[source.setting]).length,
    [settings],
  )
  const enabledSourceCount = useMemo(
    () => sources.filter((source) => settings[source.setting]).length,
    [settings],
  )
  const builderRunning = runtimeBool(status.runtime, 'activity_recorder.builder.running') === true

  const install = useCallback(async () => {
    setBusy('install')
    try {
      const runtime = await installPluginRuntime(accessToken, activityRecorderPluginID)
      setStatus((current) => ({ ...current, runtime }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : copy.installFailed, 'error')
    } finally {
      setBusy(null)
    }
  }, [accessToken, addToast, copy.installFailed])

  const toggle = useCallback(async () => {
    setBusy('toggle')
    try {
      const enablement = await setPluginEnabled(accessToken, activityRecorderPluginID, !enabled)
      const runtime = await checkPluginRuntime(accessToken, activityRecorderPluginID)
      setStatus((current) => ({ ...current, enablement, runtime }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : copy.toggleFailed, 'error')
    } finally {
      setBusy(null)
    }
  }, [accessToken, addToast, copy.toggleFailed, enabled])

  const refresh = useCallback(async () => {
    setBusy('refresh')
    try {
      const runtime = await checkPluginRuntime(accessToken, activityRecorderPluginID)
      setStatus((current) => ({ ...current, runtime }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : copy.refreshFailed, 'error')
    } finally {
      setBusy(null)
    }
  }, [accessToken, addToast, copy.refreshFailed])

  const triggerBuilder = useCallback(async () => {
    setBusy('build')
    try {
      const result = await triggerActivityRecorderBuilder(accessToken)
      const runtime = await checkPluginRuntime(accessToken, activityRecorderPluginID)
      setStatus((current) => ({ ...current, runtime }))
      if (result.running) return
    } catch (error) {
      addToast(error instanceof Error ? error.message : copy.buildFailed, 'error')
    } finally {
      setBusy(null)
    }
  }, [accessToken, addToast, copy.buildFailed])

  const updateSettings = useCallback(async (patch: Partial<RecorderSettings>) => {
    const nextSettings = { ...settings, ...patch }
    setBusy('settings')
    try {
      const enablement = await updatePluginSettings(accessToken, activityRecorderPluginID, nextSettings)
      const runtime = enabled
        ? await checkPluginRuntime(accessToken, activityRecorderPluginID)
        : status.runtime
      setStatus((current) => ({ ...current, enablement, runtime }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : copy.settingsFailed, 'error')
    } finally {
      setBusy(null)
    }
  }, [accessToken, addToast, copy.settingsFailed, enabled, settings, status.runtime])

  const setCustom = useCallback((patch: Partial<RecorderSettings>) => {
    void updateSettings({ ...patch, mode: 'custom' })
  }, [updateSettings])

  const control = !runtimeReady ? (
    <SettingsButton
      variant="primary"
      icon={busy === 'install' ? <Loader2 className="animate-spin" /> : <Download />}
      disabled={busy !== null || loading || !status.plugin}
      onClick={() => void install()}
    >
      {copy.install}
    </SettingsButton>
  ) : (
    <SettingsButton
      variant={enabled ? 'secondary' : 'primary'}
      icon={busy === 'toggle' ? <Loader2 className="animate-spin" /> : enabled ? <Pause /> : <Play />}
      disabled={busy !== null || loading}
      onClick={() => void toggle()}
    >
      {enabled ? copy.disable : copy.enable}
    </SettingsButton>
  )

  return (
    <SettingsPage title={t.desktopSettings.activityRecorder} description={copy.description} className="max-w-[760px]">
      {loading ? (
        <div className="grid min-h-[220px] place-items-center rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] text-[var(--c-text-muted)]">
          <Loader2 size={18} className="animate-spin" />
        </div>
      ) : (
        <>
          <SettingsGroup title={copy.statusSection}>
            <SettingsCard>
              <SettingsRow
                title={copy.mainStatus}
                description={runtimeReady
                  ? copy.runningSources
                    .replace('{enabled}', String(enabledSourceCount))
                    .replace('{sources}', String(sources.length))
                    .replace('{running}', String(runningCount))
                    .replace('{daemons}', String(daemonSourceCount))
                  : copy.runtimeMissing}
                control={(
                  <div className="flex items-center gap-2">
                    <StatusPill tone={enabled ? 'success' : 'neutral'}>{enabled ? copy.enabled : copy.disabled}</StatusPill>
                    <StatusPill tone={runtimeReady ? 'success' : 'warning'}>{runtimeReady ? copy.ready : copy.notInstalled}</StatusPill>
                  </div>
                )}
              />
              <SettingsRow
                title={copy.runtime}
                description={status.runtime?.status ?? 'not_installed'}
                control={(
                  <div className="flex items-center gap-2">
                    <SettingsIconButton label={copy.refresh} onClick={() => void refresh()} disabled={busy !== null || !runtimeReady}>
                      {busy === 'refresh' ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                    </SettingsIconButton>
                    {control}
                  </div>
                )}
              />
            </SettingsCard>
          </SettingsGroup>

          <SettingsGroup title={copy.builderSection}>
            <SettingsCard>
              <SettingsRow
                title={copy.builderInterval}
                description={copy.builderIntervalDesc}
                control={(
                  <SettingsInput
                    variant="md"
                    defaultValue={String(settings.builder_interval_min)}
                    disabled={busy === 'settings'}
                    onBlur={(event) => {
                      const value = Number(event.currentTarget.value)
                      if (Number.isFinite(value) && value >= 5 && value !== settings.builder_interval_min) {
                        setCustom({ builder_interval_min: Math.round(value) })
                      }
                    }}
                    className="w-[132px]"
                  />
                )}
              />
              <SettingsRow
                title={copy.buildNow}
                description={copy.builderManualDesc}
                control={(
                  <div className="flex items-center gap-2">
                    <SettingsButton
                      variant="secondary"
                      icon={busy === 'build' || builderRunning ? <Loader2 className="animate-spin" /> : <Play />}
                      disabled={busy !== null || !runtimeReady || !enabled || builderRunning}
                      onClick={() => void triggerBuilder()}
                    >
                      {builderRunning ? copy.builderRunning : copy.buildNow}
                    </SettingsButton>
                  </div>
                )}
              />
            </SettingsCard>
          </SettingsGroup>

          <SettingsGroup title={copy.sourcesSection}>
            <SettingsCard>
              {sources.map((source) => (
                <SourceRow
                  key={source.key}
                  source={source}
                  runtime={status.runtime}
                  settings={settings}
                  copy={copy}
                  disabled={busy === 'settings'}
                  onToggle={setCustom}
                />
              ))}
            </SettingsCard>
          </SettingsGroup>

          {settings.enable_activity_record && runtimeBool(status.runtime, 'activity_record.ax_permission') === false && (
          <SettingsGroup title={copy.axPermissionSection}>
            <SettingsCard>
              <SettingsRow
                title={copy.axPermissionRequired}
                description={copy.axPermissionDesc}
                control={(
                  <SettingsButton
                    variant="primary"
                    onClick={() => openExternal('x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility')}
                  >
                    {copy.grantAccess}
                  </SettingsButton>
                )}
              />
            </SettingsCard>
          </SettingsGroup>
          )}
        </>
      )}
    </SettingsPage>
  )
}
