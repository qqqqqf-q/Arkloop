import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { useToast } from '@arkloop/shared'
import { motion } from 'framer-motion'
import {
  Blocks,
  Check,
  ChevronLeft,
  ChevronRight,
  Download,
  Loader2,
  Plus,
  RefreshCw,
} from 'lucide-react'
import {
  checkPluginRuntime,
  getPluginEnablement,
  getPluginRuntimeStatus,
  installPluginRuntime,
  listPlugins,
  setPluginEnabled,
  updatePluginSettings,
  type PluginEnablement,
  type PluginPackage,
  type PluginRuntimeState,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { SettingsPage } from './_SettingsLayout'
import { SettingsButton, SettingsIconButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'
import { SettingsSelect } from './_SettingsSelect'
import { SettingsSegmentedControl } from './_SettingsSegmentedControl'

type PluginTab = 'installed' | 'marketplace'

type PluginStatus = {
  enablement: PluginEnablement | null
  runtime: PluginRuntimeState | null
}

type LoadState = {
  plugins: PluginPackage[]
  statusByID: Record<string, PluginStatus>
}

type PluginSettingDefinition = {
  key: string
  type: string
  label: string
  defaultValue: unknown
  options: string[]
}

type PluginAction = 'install-runtime' | 'toggle-enabled' | 'update-setting' | 'check-runtime'

type BusyAction = {
  pluginID: string
  action: PluginAction
}

let activeBusyAction: BusyAction | null = null
const busyActionListeners = new Set<(action: BusyAction | null) => void>()

type Props = {
  accessToken: string
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function hasRuntime(manifest: Record<string, unknown>): boolean {
  if (Array.isArray(manifest.runtime)) return manifest.runtime.length > 0
  return isRecord(manifest.runtime) && Object.keys(manifest.runtime).length > 0
}

function sourceLabel(sourceKind: string, builtIn: string, custom: string) {
  return sourceKind === 'builtin' ? builtIn : custom
}

function setActiveBusyAction(action: BusyAction | null) {
  activeBusyAction = action
  busyActionListeners.forEach((listener) => listener(action))
}

function subscribeBusyAction(listener: (action: BusyAction | null) => void) {
  busyActionListeners.add(listener)
  return () => {
    busyActionListeners.delete(listener)
  }
}

function textValue(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

function settingDefinitions(manifest: Record<string, unknown>): PluginSettingDefinition[] {
  const rawSettings = manifest.settings
  if (!Array.isArray(rawSettings)) return []

  return rawSettings.flatMap((item) => {
    if (!isRecord(item)) return []
    const key = textValue(item.key)
    if (!key) return []
    const label = textValue(item.label) || key
    const type = textValue(item.type) || 'string'
    const options = Array.isArray(item.options)
      ? item.options.map((option) => textValue(option)).filter(Boolean)
      : []
    return [{ key, type, label, defaultValue: item.default, options }]
  })
}

function visibleSettingDefinitions(plugin: PluginPackage): PluginSettingDefinition[] {
  return settingDefinitions(plugin.manifest).filter((setting) => (
    plugin.id !== 'arkloop.plugins.cua' || setting.key !== 'auto_update_enabled'
  ))
}

function settingControlValue(definition: PluginSettingDefinition, status: PluginStatus) {
  const value = status.enablement?.settings?.[definition.key] ?? definition.defaultValue
  if (definition.type === 'boolean') return value === true ? 'true' : 'false'
  if (value === undefined || value === null) return ''
  return String(value)
}

function settingPayloadValue(definition: PluginSettingDefinition, value: string): unknown {
  switch (definition.type) {
    case 'boolean':
      return value === 'true'
    case 'number':
    case 'integer':
      return Number(value)
    default:
      return value
  }
}

function settingSelectOptions(
  definition: PluginSettingDefinition,
  labels: ReturnType<typeof useLocale>['t']['desktopSettings']['pluginsPage'],
) {
  if (definition.type === 'boolean') {
    return [
      { value: 'true', label: labels.enabled },
      { value: 'false', label: labels.disabled },
    ]
  }
  return definition.options.map((option) => ({
    value: option,
    label: option === '1' ? labels.enabled : option === '0' ? labels.disabled : option,
  }))
}

function runtimeStatusValue(status: PluginStatus, suffixes: string[]) {
  const value = runtimeStatusRawValue(status, suffixes)
  return typeof value === 'string' ? value.trim() : ''
}

function runtimeStatusRawValue(status: PluginStatus, suffixes: string[]) {
  const raw = status.runtime?.status_json
  if (!isRecord(raw)) return undefined
  for (const suffix of suffixes) {
    const value = raw[suffix]
    if (value !== undefined && value !== null && String(value).trim() !== '') return value
  }
  for (const [key, value] of Object.entries(raw)) {
    if (suffixes.some((suffix) => key.endsWith(`.${suffix}`)) && value !== undefined && value !== null && String(value).trim() !== '') {
      return value
    }
  }
  return undefined
}

function runtimeStatusBoolValue(status: PluginStatus, suffixes: string[]) {
  const value = runtimeStatusRawValue(status, suffixes)
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') {
    if (value === 'true') return true
    if (value === 'false') return false
  }
  return null
}

function formatRuntimeTimestamp(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function permissionDisplayValue(value: boolean | null, checked: boolean, labels: ReturnType<typeof useLocale>['t']['desktopSettings']['pluginsPage']) {
  if (!checked) return labels.checking
  if (value === null) return labels.unchecked
  return value ? labels.granted : labels.notGranted
}

function permissionTone(value: boolean | null, checked: boolean): 'neutral' | 'success' | 'error' {
  if (!checked || value === null) return 'neutral'
  return value ? 'success' : 'error'
}

function hasRuntimePermissionCheck(status: PluginStatus) {
  return runtimeStatusValue(status, ['permissions.checked_at']) !== ''
}

export function PluginsSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const { addToast } = useToast()
  const ds = t.desktopSettings
  const ps = ds.pluginsPage
  const [tab, setTab] = useState<PluginTab>('installed')
  const [state, setState] = useState<LoadState>({ plugins: [], statusByID: {} })
  const [loading, setLoading] = useState(true)
  const [busyAction, setBusyAction] = useState<BusyAction | null>(activeBusyAction)
  const [selectedPluginID, setSelectedPluginID] = useState<string | null>(null)
  const previousBusyActionRef = useRef<BusyAction | null>(activeBusyAction)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const plugins = await listPlugins(accessToken)
      const statusPairs = await Promise.all(
        plugins.map(async (plugin) => {
          const [enablement, runtime] = await Promise.all([
            getPluginEnablement(accessToken, plugin.id),
            getPluginRuntimeStatus(accessToken, plugin.id),
          ])
          return [plugin.id, { enablement, runtime }] as const
        }),
      )
      setState({ plugins, statusByID: Object.fromEntries(statusPairs) })
    } catch (error) {
      addToast(error instanceof Error ? error.message : ps.loadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, ps.loadFailed])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => subscribeBusyAction(setBusyAction), [])

  useEffect(() => {
    if (previousBusyActionRef.current && !busyAction) {
      void load()
    }
    previousBusyActionRef.current = busyAction
  }, [busyAction, load])

  const items = useMemo(() => state.plugins.filter((plugin) => plugin.is_active), [state.plugins])
  const selectedPlugin = useMemo(
    () => items.find((plugin) => plugin.id === selectedPluginID) ?? null,
    [items, selectedPluginID],
  )

  const installRuntime = useCallback(async (plugin: PluginPackage) => {
    setActiveBusyAction({ pluginID: plugin.id, action: 'install-runtime' })
    try {
      const runtime = await installPluginRuntime(accessToken, plugin.id)
      setState((current) => ({
        ...current,
        statusByID: {
          ...current.statusByID,
          [plugin.id]: { ...(current.statusByID[plugin.id] ?? { enablement: null }), runtime },
        },
      }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : ps.runtimeInstallFailed, 'error')
    } finally {
      setActiveBusyAction(null)
    }
  }, [accessToken, addToast, ps.runtimeInstallFailed])

  const toggleEnabled = useCallback(async (plugin: PluginPackage, enabled: boolean) => {
    setActiveBusyAction({ pluginID: plugin.id, action: 'toggle-enabled' })
    try {
      const enablement = await setPluginEnabled(accessToken, plugin.id, enabled)
      setState((current) => ({
        ...current,
        statusByID: {
          ...current.statusByID,
          [plugin.id]: { ...(current.statusByID[plugin.id] ?? { runtime: null }), enablement },
        },
      }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : enabled ? ps.enableFailed : ps.disableFailed, 'error')
    } finally {
      setActiveBusyAction(null)
    }
  }, [accessToken, addToast, ps.disableFailed, ps.enableFailed])

  const updateSetting = useCallback(async (
    plugin: PluginPackage,
    definition: PluginSettingDefinition,
    value: string,
  ) => {
    const previous = state.statusByID[plugin.id]?.enablement?.settings ?? {}
    const nextSettings = {
      ...previous,
      [definition.key]: settingPayloadValue(definition, value),
    }
    setActiveBusyAction({ pluginID: plugin.id, action: 'update-setting' })
    try {
      const enablement = await updatePluginSettings(accessToken, plugin.id, nextSettings)
      setState((current) => ({
        ...current,
        statusByID: {
          ...current.statusByID,
          [plugin.id]: { ...(current.statusByID[plugin.id] ?? { runtime: null }), enablement },
        },
      }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : ps.settingSaveFailed, 'error')
    } finally {
      setActiveBusyAction(null)
    }
  }, [accessToken, addToast, ps.settingSaveFailed, state.statusByID])

  const checkRuntime = useCallback(async (plugin: PluginPackage) => {
    setActiveBusyAction({ pluginID: plugin.id, action: 'check-runtime' })
    try {
      const runtime = await checkPluginRuntime(accessToken, plugin.id)
      setState((current) => ({
        ...current,
        statusByID: {
          ...current.statusByID,
          [plugin.id]: { ...(current.statusByID[plugin.id] ?? { enablement: null }), runtime },
        },
      }))
    } catch (error) {
      addToast(error instanceof Error ? error.message : ps.runtimeCheckFailed, 'error')
    } finally {
      setActiveBusyAction(null)
    }
  }, [accessToken, addToast, ps.runtimeCheckFailed])

  useEffect(() => {
    if (!selectedPlugin || selectedPlugin.id !== 'arkloop.plugins.cua' || busyAction) return
    const status = state.statusByID[selectedPlugin.id] ?? { enablement: null, runtime: null }
    if (status.runtime?.status !== 'installed' || hasRuntimePermissionCheck(status)) return
    void checkRuntime(selectedPlugin)
  }, [busyAction, checkRuntime, selectedPlugin, state.statusByID])

  if (selectedPlugin) {
    return (
      <SettingsPage title={ds.pluginsTitle} className="max-w-[760px]">
        <PluginDetailPage
          plugin={selectedPlugin}
          status={state.statusByID[selectedPlugin.id] ?? { enablement: null, runtime: null }}
          busyAction={busyAction?.pluginID === selectedPlugin.id ? busyAction.action : null}
          labels={ps}
          pageTitle={ds.pluginsTitle}
          onBack={() => setSelectedPluginID(null)}
          onInstallRuntime={() => void installRuntime(selectedPlugin)}
          onCheckRuntime={() => void checkRuntime(selectedPlugin)}
          onToggleEnabled={(enabled) => void toggleEnabled(selectedPlugin, enabled)}
          onUpdateSetting={(definition, value) => void updateSetting(selectedPlugin, definition, value)}
        />
      </SettingsPage>
    )
  }

  return (
    <SettingsPage title={ds.pluginsTitle} className="max-w-[760px]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SettingsSegmentedControl
          value={tab}
          onChange={setTab}
          options={[
            { value: 'installed', label: ps.installedTab },
            { value: 'marketplace', label: ps.marketplaceTab },
          ]}
        />
        <SettingsIconButton label={ps.refresh} onClick={() => void load()} disabled={loading}>
          {loading ? <Loader2 className="animate-spin" /> : <RefreshCw />}
        </SettingsIconButton>
      </div>

      {tab === 'installed' ? (
        loading ? (
          <div className="grid min-h-[220px] place-items-center rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] text-[var(--c-text-muted)]">
            <Loader2 size={18} className="animate-spin" />
          </div>
        ) : items.length === 0 ? (
          <div className="rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-5 py-6 text-sm text-[var(--c-text-tertiary)]">
            {ps.emptyInstalled}
          </div>
        ) : (
          <PluginList plugins={items} statusByID={state.statusByID} busyAction={busyAction} labels={ps} onOpen={setSelectedPluginID} onInstallRuntime={installRuntime} onToggleEnabled={toggleEnabled} />
        )
      ) : (
        <div className="rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-5 py-6">
          <div className="text-sm font-medium text-[var(--c-text-primary)]">{ps.emptyMarketplaceTitle}</div>
          <div className="mt-1 text-[12.5px] leading-5 text-[var(--c-text-tertiary)]">{ps.emptyMarketplace}</div>
        </div>
      )}
    </SettingsPage>
  )
}

function PluginList({
  plugins,
  statusByID,
  busyAction,
  labels,
  onOpen,
  onInstallRuntime,
  onToggleEnabled,
}: {
  plugins: PluginPackage[]
  statusByID: Record<string, PluginStatus>
  busyAction: BusyAction | null
  labels: ReturnType<typeof useLocale>['t']['desktopSettings']['pluginsPage']
  onOpen: (pluginID: string) => void
  onInstallRuntime: (plugin: PluginPackage) => void
  onToggleEnabled: (plugin: PluginPackage, enabled: boolean) => void
}) {
  return (
    <div className="grid gap-3">
      {plugins.map((plugin) => (
        <PluginListRow
          key={plugin.package_id}
          plugin={plugin}
          status={statusByID[plugin.id] ?? { enablement: null, runtime: null }}
          busyAction={busyAction?.pluginID === plugin.id ? busyAction.action : null}
          labels={labels}
          onOpen={() => onOpen(plugin.id)}
          onInstallRuntime={() => onInstallRuntime(plugin)}
          onToggleEnabled={(enabled) => onToggleEnabled(plugin, enabled)}
        />
      ))}
    </div>
  )
}

function PluginListRow({
  plugin,
  status,
  busyAction,
  labels,
  onOpen,
  onInstallRuntime,
  onToggleEnabled,
}: {
  plugin: PluginPackage
  status: PluginStatus
  busyAction: PluginAction | null
  labels: ReturnType<typeof useLocale>['t']['desktopSettings']['pluginsPage']
  onOpen: () => void
  onInstallRuntime: () => void
  onToggleEnabled: (enabled: boolean) => void
}) {
  const enabled = status.enablement?.enabled ?? false
  const runtimeStatus = status.runtime?.status ?? 'not_installed'
  const runtimeNeeded = hasRuntime(plugin.manifest)
  const runtimeReady = runtimeStatus === 'installed'
  const busy = busyAction !== null
  const installBusy = busyAction === 'install-runtime'
  const toggleBusy = busyAction === 'toggle-enabled'
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    onOpen()
  }

  return (
    <motion.div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={handleKeyDown}
      whileTap={{ scale: 0.972 }}
      transition={{ type: 'spring', stiffness: 680, damping: 20, mass: 0.38 }}
      className="cursor-pointer overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] outline-none transition-colors duration-[140ms] hover:bg-[var(--c-bg-deep)] focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]"
    >
      <div className="grid min-h-[76px] w-full grid-cols-1 items-center gap-3 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto]">
        <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-lg text-left">
          <PluginIcon />
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <h3 className="truncate text-[14px] font-semibold leading-5 text-[var(--c-text-primary)]">{plugin.display_name}</h3>
              <span className="shrink-0 rounded-md bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">
                {sourceLabel(plugin.source_kind, labels.builtIn, labels.custom)}
              </span>
            </div>
            <p className="mt-1 truncate text-[12.5px] leading-5 text-[var(--c-text-tertiary)]">
              {plugin.description || plugin.id}
            </p>
          </div>
          <ChevronRight size={16} className="text-[var(--c-text-muted)]" />
        </div>
        <div
          className="flex shrink-0 items-center gap-2 justify-self-end"
          onClick={(event) => event.stopPropagation()}
          onKeyDown={(event) => event.stopPropagation()}
        >
          {runtimeNeeded && !runtimeReady && (
            <SettingsButton
              icon={installBusy ? <Loader2 className="animate-spin" /> : <Download />}
              disabled={busy}
              onClick={onInstallRuntime}
            >
              {labels.installRuntime}
            </SettingsButton>
          )}
          <SettingsButton
            variant={enabled ? 'secondary' : 'primary'}
            icon={toggleBusy ? <Loader2 className="animate-spin" /> : enabled ? <Check /> : <Plus />}
            disabled={busy || (runtimeNeeded && !runtimeReady && !enabled)}
            onClick={() => onToggleEnabled(!enabled)}
          >
            {enabled ? labels.disable : labels.enable}
          </SettingsButton>
        </div>
      </div>
    </motion.div>
  )
}

function PluginDetailPage({
  plugin,
  status,
  busyAction,
  labels,
  pageTitle,
  onBack,
  onInstallRuntime,
  onCheckRuntime,
  onToggleEnabled,
  onUpdateSetting,
}: {
  plugin: PluginPackage
  status: PluginStatus
  busyAction: PluginAction | null
  labels: ReturnType<typeof useLocale>['t']['desktopSettings']['pluginsPage']
  pageTitle: string
  onBack: () => void
  onInstallRuntime: () => void
  onCheckRuntime: () => void
  onToggleEnabled: (enabled: boolean) => void
  onUpdateSetting: (definition: PluginSettingDefinition, value: string) => void
}) {
  const enabled = status.enablement?.enabled ?? false
  const runtimeStatus = status.runtime?.status ?? 'not_installed'
  const runtimeNeeded = hasRuntime(plugin.manifest)
  const runtimeReady = runtimeStatus === 'installed'
  const settings = visibleSettingDefinitions(plugin)
  const busy = busyAction !== null
  const installBusy = busyAction === 'install-runtime'
  const toggleBusy = busyAction === 'toggle-enabled'
  const checkBusy = busyAction === 'check-runtime'
  const helperAppName = runtimeStatusValue(status, ['helper_app_name', 'helperAppName'])
  const helperAppBundleID = runtimeStatusValue(status, ['helper_app_bundle_id', 'helperAppBundleID'])
  const showCUAPermissions = plugin.id === 'arkloop.plugins.cua' && runtimeReady
  const accessibilityPermission = runtimeStatusBoolValue(status, ['permissions.accessibility'])
  const screenRecordingPermission = runtimeStatusBoolValue(status, ['permissions.screen_recording'])
  const permissionCheckedAt = runtimeStatusValue(status, ['permissions.checked_at'])
  const permissionError = runtimeStatusValue(status, ['permissions.error'])

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <button
        type="button"
        onClick={onBack}
        className="inline-flex h-[32px] w-fit items-center gap-1.5 rounded-[6.5px] px-2 text-[13px] font-medium text-[var(--c-text-secondary)] transition-[background-color,transform] duration-[140ms] hover:bg-[var(--c-bg-deep)] active:scale-[0.97]"
      >
        <ChevronLeft size={15} />
        {pageTitle}
      </button>

      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <PluginIcon size="lg" />
          <div className="min-w-0">
            <h2 className="truncate text-[20px] font-semibold leading-7 text-[var(--c-text-primary)]">{plugin.display_name}</h2>
            {plugin.description && (
              <p className="mt-1 max-w-[560px] text-[13px] leading-5 text-[var(--c-text-secondary)]">{plugin.description}</p>
            )}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {runtimeNeeded && !runtimeReady && (
            <SettingsButton
              icon={installBusy ? <Loader2 className="animate-spin" /> : <Download />}
              disabled={busy}
              onClick={onInstallRuntime}
            >
              {labels.installRuntime}
            </SettingsButton>
          )}
          <SettingsButton
            variant={enabled ? 'secondary' : 'primary'}
            icon={toggleBusy ? <Loader2 className="animate-spin" /> : enabled ? <Check /> : <Plus />}
            disabled={busy || (runtimeNeeded && !runtimeReady && !enabled)}
            onClick={() => onToggleEnabled(!enabled)}
          >
            {enabled ? labels.disable : labels.enable}
          </SettingsButton>
        </div>
      </div>

      <PluginDetailSection title={labels.overview}>
        <PluginDetailCard>
          <PluginDetailRow label={labels.version}>
            <PluginValue value={plugin.version} />
          </PluginDetailRow>
          <PluginDetailRow label={labels.source}>
            <PluginValue value={sourceLabel(plugin.source_kind, labels.builtIn, labels.custom)} />
          </PluginDetailRow>
          <PluginDetailRow label={labels.status}>
            <div className="flex flex-wrap items-center justify-end gap-1.5">
              <PluginPill>{enabled ? labels.enabled : labels.disabled}</PluginPill>
              {runtimeNeeded && <PluginPill>{runtimeReady ? labels.ready : labels.needsSetup}</PluginPill>}
            </div>
          </PluginDetailRow>
          <PluginDetailRow label={labels.runtimeStatus}>
            <PluginValue value={runtimeNeeded ? runtimeStatus : labels.notRequired} />
          </PluginDetailRow>
        </PluginDetailCard>
      </PluginDetailSection>

      {showCUAPermissions && (
        <PluginDetailSection
          title={labels.permissionsSection}
          action={(
            <SettingsIconButton label={labels.checkPermissions} onClick={onCheckRuntime} disabled={busy}>
              {checkBusy ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            </SettingsIconButton>
          )}
        >
          <PluginDetailCard>
            {helperAppName && (
              <PluginDetailRow label={labels.permissionApp}>
                <PluginValue value={helperAppName} />
              </PluginDetailRow>
            )}
            {helperAppBundleID && (
              <PluginDetailRow label={labels.bundleId}>
                <PluginValue value={helperAppBundleID} mono />
              </PluginDetailRow>
            )}
            <PluginDetailRow label={labels.accessibilityPermission}>
              <PluginValue
                value={permissionDisplayValue(accessibilityPermission, permissionCheckedAt !== '', labels)}
                tone={permissionTone(accessibilityPermission, permissionCheckedAt !== '')}
              />
            </PluginDetailRow>
            <PluginDetailRow label={labels.screenRecordingPermission}>
              <PluginValue
                value={permissionDisplayValue(screenRecordingPermission, permissionCheckedAt !== '', labels)}
                tone={permissionTone(screenRecordingPermission, permissionCheckedAt !== '')}
              />
            </PluginDetailRow>
            <PluginDetailRow label={labels.permissionCheckedAt}>
              <PluginValue value={formatRuntimeTimestamp(permissionCheckedAt) || labels.checking} />
            </PluginDetailRow>
            {permissionError && (
              <PluginDetailRow label={labels.permissionError}>
                <PluginValue value={permissionError} tone="error" />
              </PluginDetailRow>
            )}
          </PluginDetailCard>
        </PluginDetailSection>
      )}

      {settings.length > 0 && (
        <PluginSettingsSection
          settings={settings}
          status={status}
          busy={busy}
          labels={labels}
          onUpdateSetting={onUpdateSetting}
        />
      )}
    </div>
  )
}

function PluginSettingsSection({
  settings,
  status,
  busy,
  labels,
  onUpdateSetting,
}: {
  settings: PluginSettingDefinition[]
  status: PluginStatus
  busy: boolean
  labels: ReturnType<typeof useLocale>['t']['desktopSettings']['pluginsPage']
  onUpdateSetting: (definition: PluginSettingDefinition, value: string) => void
}) {
  return (
    <PluginDetailSection title={labels.settingsSection}>
      <PluginDetailCard>
        {settings.map((setting) => {
          const value = settingControlValue(setting, status)
          const options = settingSelectOptions(setting, labels)
          const disabled = busy || status.enablement === null
          return (
	            <PluginDetailRow key={setting.key} label={setting.label}>
	              {options.length > 0 ? (
	                <div className="flex justify-end">
	                  <div className="w-[180px] max-w-full">
	                    <SettingsSelect
	                      value={value}
	                      options={options}
	                      onChange={(nextValue) => onUpdateSetting(setting, nextValue)}
	                      disabled={disabled}
	                      triggerClassName="h-9"
	                    />
	                  </div>
	                </div>
	              ) : (
	                <div className="flex justify-end">
	                  <div className="w-[240px] max-w-full">
	                    <SettingsInput
	                      variant="md"
	                      defaultValue={value}
	                      disabled={disabled}
	                      onBlur={(event) => {
	                        const nextValue = event.currentTarget.value
	                        if (nextValue !== value) onUpdateSetting(setting, nextValue)
	                      }}
	                    />
	                  </div>
	                </div>
	              )}
	            </PluginDetailRow>
          )
        })}
      </PluginDetailCard>
    </PluginDetailSection>
  )
}

function PluginIcon({ size = 'md' }: { size?: 'md' | 'lg' }) {
  return (
    <div
      className={[
        'grid shrink-0 place-items-center rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] text-[var(--c-text-secondary)]',
        size === 'lg' ? 'h-12 w-12' : 'h-10 w-10',
      ].join(' ')}
    >
      <Blocks size={size === 'lg' ? 21 : 18} />
    </div>
  )
}

function PluginDetailSection({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2.5">
      <div className="flex min-h-[32px] items-center justify-between gap-3 pl-2.5">
        <h4 className="text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h4>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {children}
    </section>
  )
}

function PluginDetailCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      {children}
    </div>
  )
}

function PluginDetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="relative grid items-center gap-3 px-5 py-4 sm:grid-cols-[minmax(0,1fr)_minmax(260px,390px)] sm:gap-6 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']">
      <div className="min-w-0 text-[13px] font-medium text-[var(--c-text-primary)]">{label}</div>
      <div className="min-w-0 sm:w-full sm:justify-self-end">{children}</div>
    </div>
  )
}

function PluginValue({
  value,
  mono,
  tone = 'neutral',
}: {
  value: string
  mono?: boolean
  tone?: 'neutral' | 'success' | 'error'
}) {
  const toneClass = tone === 'success'
    ? 'text-[var(--c-status-success)]'
    : tone === 'error'
      ? 'text-[var(--c-status-error)]'
      : 'text-[var(--c-text-secondary)]'
  return (
    <div
      className={[
        'truncate text-right text-[13px] font-medium leading-5',
        toneClass,
        mono ? 'font-mono text-[12px]' : '',
      ].filter(Boolean).join(' ')}
      title={value}
    >
      {value}
    </div>
  )
}

function PluginPill({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-md bg-[var(--c-bg-sub)] px-2 py-1 text-xs font-medium text-[var(--c-text-muted)]">
      {children}
    </span>
  )
}
