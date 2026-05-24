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
import { SettingsButton, SettingsIconButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'
import { SettingsCard, SettingsGroup, SettingsPage, SettingsRow } from './_SettingsLayout'
import { SettingsSegmentedControl } from './_SettingsSegmentedControl'
import { SettingsSelect } from './_SettingsSelect'
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
  setting: keyof Pick<RecorderSettings, 'enable_activity_record' | 'enable_screenpipe'>
  kind: 'screen' | 'context'
  daemonKeys?: string[]
}

const sources: SourceView[] = [
  { key: 'activity-record', label: 'Activity Record', setting: 'enable_activity_record', kind: 'context' },
  { key: 'screenpipe', label: 'Screenpipe', setting: 'enable_screenpipe', kind: 'screen', daemonKeys: ['screenpipe'] },
]

type RecorderMode = 'lightweight' | 'full' | 'custom'

type RecorderSettings = {
  mode: RecorderMode
  enable_activity_record: boolean
  enable_screenpipe: boolean
  enable_audio: boolean
  transcription_engine: string
  video_quality: string
  capture_interval_ms: number
  retention_days: number
  meeting_detector: boolean
  snapshot_compaction: boolean
  builder_interval_min: number
}

const defaultRecorderSettings: RecorderSettings = {
  mode: 'lightweight',
  enable_activity_record: true,
  enable_screenpipe: false,
  enable_audio: false,
  transcription_engine: 'disabled',
  video_quality: 'low',
  capture_interval_ms: 120000,
  retention_days: 3,
  meeting_detector: false,
  snapshot_compaction: false,
  builder_interval_min: 300,
}

const presetSettings: Record<RecorderMode, RecorderSettings> = {
  lightweight: defaultRecorderSettings,
  full: {
    mode: 'full',
    enable_activity_record: true,
    enable_screenpipe: true,
    enable_audio: true,
    transcription_engine: 'parakeet',
    video_quality: 'balanced',
    capture_interval_ms: 30000,
    retention_days: 14,
    meeting_detector: true,
    snapshot_compaction: true,
    builder_interval_min: 300,
  },
  custom: defaultRecorderSettings,
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
    enable_screenpipe: toBool(raw.enable_screenpipe, defaultRecorderSettings.enable_screenpipe),
    enable_audio: toBool(raw.enable_audio, defaultRecorderSettings.enable_audio),
    transcription_engine: typeof raw.transcription_engine === 'string' ? raw.transcription_engine : defaultRecorderSettings.transcription_engine,
    video_quality: typeof raw.video_quality === 'string' ? raw.video_quality : defaultRecorderSettings.video_quality,
    capture_interval_ms: toNumber(raw.capture_interval_ms, defaultRecorderSettings.capture_interval_ms),
    retention_days: toNumber(raw.retention_days, defaultRecorderSettings.retention_days),
    meeting_detector: toBool(raw.meeting_detector, defaultRecorderSettings.meeting_detector),
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
    screen: copy.screenSource,
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

  const setMode = useCallback((mode: RecorderMode) => {
    const preset = presetSettings[mode]
    void updateSettings(mode === 'custom' ? { mode } : preset)
  }, [updateSettings])

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

          <SettingsGroup title={copy.modeSection}>
            <SettingsCard>
              <SettingsRow
                title={copy.mode}
                description={copy.modeDescriptions[settings.mode]}
                control={(
                  <div className="flex w-[260px] max-w-full justify-end">
                    <SettingsSegmentedControl
                      value={settings.mode}
                      onChange={(value) => setMode(value as RecorderMode)}
                      options={[
                        { value: 'lightweight', label: copy.lightweight },
                        { value: 'full', label: copy.full },
                        { value: 'custom', label: copy.custom },
                      ]}
                    />
                  </div>
                )}
              />
            </SettingsCard>
          </SettingsGroup>

          <SettingsGroup title={copy.captureSection}>
            <SettingsCard>
              <SettingsRow
                title={copy.audio}
                description={copy.audioDesc}
                control={<SettingsSwitch checked={settings.enable_audio} disabled={busy === 'settings'} onChange={(value) => setCustom({ enable_audio: value })} />}
              />
              <SettingsRow
                title={copy.transcriptionEngine}
                description={copy.transcriptionEngineDesc}
                disabled={!settings.enable_audio}
                control={(
                  <div className="w-[190px] max-w-full">
                    <SettingsSelect
                      value={settings.transcription_engine}
                      disabled={busy === 'settings' || !settings.enable_audio}
                      onChange={(value) => setCustom({ transcription_engine: value })}
                      options={[
                        { value: 'disabled', label: 'disabled' },
                        { value: 'whisper-tiny', label: 'whisper-tiny' },
                        { value: 'parakeet', label: 'parakeet' },
                        { value: 'deepgram', label: 'deepgram' },
                        { value: 'openai-compatible', label: 'openai-compatible' },
                      ]}
                    />
                  </div>
                )}
              />
              <SettingsRow
                title={copy.videoQuality}
                description={copy.videoQualityDesc}
                control={(
                  <div className="w-[150px] max-w-full">
                    <SettingsSelect
                      value={settings.video_quality}
                      disabled={busy === 'settings'}
                      onChange={(value) => setCustom({ video_quality: value })}
                      options={[
                        { value: 'low', label: 'low' },
                        { value: 'balanced', label: 'balanced' },
                        { value: 'high', label: 'high' },
                      ]}
                    />
                  </div>
                )}
              />
              <SettingsRow
                title={copy.captureInterval}
                description={copy.captureIntervalDesc}
                control={(
                  <SettingsInput
                    variant="md"
                    defaultValue={String(settings.capture_interval_ms)}
                    disabled={busy === 'settings'}
                    onBlur={(event) => {
                      const value = Number(event.currentTarget.value)
                      if (Number.isFinite(value) && value > 0 && value !== settings.capture_interval_ms) {
                        setCustom({ capture_interval_ms: Math.round(value) })
                      }
                    }}
                    className="w-[132px]"
                  />
                )}
              />
              <SettingsRow
                title={copy.retentionDays}
                description={copy.retentionDaysDesc}
                control={(
                  <SettingsInput
                    variant="md"
                    defaultValue={String(settings.retention_days)}
                    disabled={busy === 'settings'}
                    onBlur={(event) => {
                      const value = Number(event.currentTarget.value)
                      if (Number.isFinite(value) && value >= 0 && value !== settings.retention_days) {
                        setCustom({ retention_days: Math.round(value) })
                      }
                    }}
                    className="w-[132px]"
                  />
                )}
              />
              <SettingsRow
                title={copy.meetingDetector}
                description={copy.meetingDetectorDesc}
                control={<SettingsSwitch checked={settings.meeting_detector} disabled={busy === 'settings'} onChange={(value) => setCustom({ meeting_detector: value })} />}
              />
              <SettingsRow
                title={copy.snapshotCompaction}
                description={copy.snapshotCompactionDesc}
                control={<SettingsSwitch checked={settings.snapshot_compaction} disabled={busy === 'settings'} onChange={(value) => setCustom({ snapshot_compaction: value })} />}
              />
            </SettingsCard>
          </SettingsGroup>
        </>
      )}
    </SettingsPage>
  )
}
