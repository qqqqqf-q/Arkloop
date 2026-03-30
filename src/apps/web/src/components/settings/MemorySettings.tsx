import { useState, useEffect, useCallback } from 'react'
import {
  Brain,
  Trash2,
  FileText,
  RefreshCw,
  CheckCircle,
  XCircle,
  HardDrive,
  Database,
} from 'lucide-react'
import { ConfirmDialog } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { MemoryEntry, MemoryConfig, OpenVikingDesktopConfig } from '@arkloop/shared/desktop'
import {
  bridgeClient,
  checkBridgeAvailable,
  type ModuleInfo,
  type ModuleStatus,
  type ModuleAction,
} from '../../api-bridge'
import {
  isApiError,
  listLlmProviders,
  resolveOpenVikingConfig,
  setSpawnProfile,
  type LlmProvider,
} from '../../api'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { SettingsModelDropdown } from './SettingsModelDropdown'
import type { LocaleStrings } from '../../locales'

const MEMORY_CONFIGURE_DEADLINE_MS = 120_000

function promiseWithTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const id = window.setTimeout(() => reject(new Error('__configure_timeout__')), ms)
    p.then(
      (v) => { window.clearTimeout(id); resolve(v) },
      (e) => { window.clearTimeout(id); reject(e) },
    )
  })
}

// ---------------------------------------------------------------------------
// Local helpers (minimal)
// ---------------------------------------------------------------------------

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' })
  } catch {
    return iso
  }
}

function categoryColor(category: string): string {
  const map: Record<string, string> = {
    profile: 'bg-blue-500/15 text-blue-400',
    preferences: 'bg-purple-500/15 text-purple-400',
    entities: 'bg-amber-500/15 text-amber-400',
    events: 'bg-green-500/15 text-green-400',
    cases: 'bg-red-500/15 text-red-400',
    patterns: 'bg-teal-500/15 text-teal-400',
    general: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',
  }
  return map[category] ?? 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]'
}

// Same status helpers as ModulesSettings.tsx
function moduleStatusColor(status: ModuleStatus): string {
  switch (status) {
    case 'running': return '#22c55e'
    case 'stopped':
    case 'installed_disconnected': return '#f59e0b'
    case 'error': return '#ef4444'
    default: return 'var(--c-text-muted)'
  }
}

function moduleStatusLabel(status: ModuleStatus, ds: LocaleStrings['desktopSettings']): string {
  switch (status) {
    case 'running': return ds.memoryModuleRunning
    case 'stopped': return ds.memoryModuleStopped
    case 'installed_disconnected': return ds.memoryModuleDisconnected
    case 'pending_bootstrap': return ds.memoryModulePending
    case 'error': return ds.memoryModuleError
    case 'not_installed': return ds.memoryModuleNotInstalled
    default: return status
  }
}

function actionForStatus(status: ModuleStatus): ModuleAction | null {
  switch (status) {
    case 'not_installed': return 'install'
    case 'stopped': return 'start'
    case 'running': return 'stop'
    case 'error': return 'restart'
    default: return null
  }
}

function actionLabel(action: ModuleAction, ds: LocaleStrings['desktopSettings']): string {
  switch (action) {
    case 'install': return ds.memoryModuleInstall
    case 'start': return ds.memoryModuleStart
    case 'stop': return ds.memoryModuleStop
    case 'restart': return ds.memoryModuleRestart
    default: return action
  }
}


// ---------------------------------------------------------------------------
// ModeCard — exact same pattern as ConnectionSettingsContent.tsx
// ---------------------------------------------------------------------------

type ModeCardProps = {
  icon: React.ElementType
  label: string
  desc: string
  selected: boolean
  onSelect: () => void
}

function ModeCard({ icon: Icon, label, desc, selected, onSelect }: ModeCardProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className="flex items-start gap-3 rounded-xl p-4 text-left transition-colors"
      style={{
        border: selected ? '1.5px solid var(--c-accent)' : '1px solid var(--c-border-subtle)',
        background: selected ? 'var(--c-bg-deep)' : 'var(--c-bg-page)',
      }}
    >
      <div
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg"
        style={{
          background: selected ? 'var(--c-accent)' : 'var(--c-bg-sub)',
          color: selected ? 'var(--c-accent-fg)' : 'var(--c-text-secondary)',
        }}
      >
        <Icon size={18} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium text-[var(--c-text-heading)]">{label}</div>
        <div className="mt-0.5 text-xs text-[var(--c-text-muted)]">{desc}</div>
      </div>
      <div
        className="mt-1 h-4 w-4 shrink-0 rounded-full"
        style={{
          border: selected ? '5px solid var(--c-accent)' : '1.5px solid var(--c-border-subtle)',
          background: selected ? 'var(--c-accent-fg)' : 'transparent',
        }}
      />
    </button>
  )
}

// ---------------------------------------------------------------------------
// Entry card (local SQLite)
// ---------------------------------------------------------------------------

function EntryCard({ entry, onDelete }: { entry: MemoryEntry; onDelete: (id: string) => void }) {
  const content = entry.content.replace(/^\[.*?\]\s*/, '').trim() || entry.content
  return (
    <div
      className="group relative rounded-xl"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
    >
      <div className="flex items-start gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${categoryColor(entry.category)}`}>
              {entry.category}
            </span>
            {entry.scope === 'agent' && (
              <span className="inline-flex items-center rounded-md bg-indigo-500/15 px-2 py-0.5 text-xs font-medium text-indigo-400">agent</span>
            )}
            {entry.key && <span className="text-[10px] text-[var(--c-text-muted)]">{entry.key}</span>}
          </div>
          <p className="text-sm text-[var(--c-text-primary)]">{content}</p>
          <p className="text-[10px] text-[var(--c-text-muted)]">{formatDate(entry.created_at)}</p>
        </div>
        <button
          onClick={() => onDelete(entry.id)}
          className="mt-0.5 shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] opacity-0 transition-[opacity,color] duration-100 group-hover:opacity-100 hover:text-red-400"
        >
          <Trash2 size={13} />
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Snapshot view (OpenViking)
// ---------------------------------------------------------------------------

function SnapshotView({ snapshot }: { snapshot: string }) {
  if (!snapshot) {
    return (
      <div
        className="flex flex-col items-center justify-center rounded-xl py-14"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <Brain size={28} className="mb-3 text-[var(--c-text-muted)]" />
        <p className="text-sm text-[var(--c-text-muted)]">No memory snapshot available yet.</p>
      </div>
    )
  }
  return (
    <pre
      className="overflow-auto rounded-xl p-4 text-xs leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)', maxHeight: 360 }}
    >
      {snapshot}
    </pre>
  )
}

// ---------------------------------------------------------------------------
// OpenViking module card — same logic as ModulesSettings.tsx
// ---------------------------------------------------------------------------

type ModuleListProbe = 'idle' | 'loading' | 'ready' | 'failed'

type OVModuleCardProps = {
  bridgeOnline: boolean | null
  bridgeOfflineHint: string
  module: ModuleInfo | null
  moduleListProbe: ModuleListProbe
  actionInProgress: boolean
  onAction: (action: ModuleAction) => void
  onRefreshModules: () => void
  ds: LocaleStrings['desktopSettings']
}

function OVModuleCard({
  bridgeOnline,
  bridgeOfflineHint,
  module,
  moduleListProbe,
  actionInProgress,
  onAction,
  onRefreshModules,
  ds,
}: OVModuleCardProps) {
  const statusReady = bridgeOnline === true && moduleListProbe === 'ready'
  const statusChecking = bridgeOnline === true && (moduleListProbe === 'loading' || moduleListProbe === 'idle')
  const listFailed = bridgeOnline === true && moduleListProbe === 'failed'

  const effectiveStatus: ModuleStatus | null = statusChecking || (listFailed && !module)
    ? null
    : (module?.status ?? 'not_installed')

  const action = effectiveStatus ? actionForStatus(effectiveStatus) : null

  let statusLineColor = 'var(--c-text-muted)'
  let statusLineText = ds.memoryModuleChecking
  if (effectiveStatus) {
    statusLineColor = moduleStatusColor(effectiveStatus)
    statusLineText = moduleStatusLabel(effectiveStatus, ds)
  } else if (listFailed && !module) {
    statusLineColor = '#ef4444'
    statusLineText = ds.memoryModuleDockerUnavailable
  }

  return (
    <div
      className="flex flex-col gap-3 rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      <div className="flex items-center gap-2">
        <div
          className="h-2 w-2 rounded-full"
          style={{ background: bridgeOnline === null ? 'var(--c-text-muted)' : bridgeOnline ? '#22c55e' : '#ef4444' }}
        />
        <span className="text-xs text-[var(--c-text-muted)]">
          Installer Bridge {bridgeOnline === null ? '...' : bridgeOnline ? 'Online' : 'Offline'}
        </span>
      </div>

      {bridgeOnline === false ? (
        <p className="text-xs text-[var(--c-text-muted)]">{bridgeOfflineHint}</p>
      ) : bridgeOnline === null ? (
        <p className="text-xs text-[var(--c-text-muted)]">{ds.memoryModuleChecking}</p>
      ) : (
        <>
          <div className="flex items-center justify-between gap-2">
            <span className="min-w-0 flex-1 text-xs" style={{ color: statusLineColor }}>
              {statusLineText}
            </span>
            <div className="flex shrink-0 items-center gap-2">
              <button
                type="button"
                onClick={() => onRefreshModules()}
                disabled={actionInProgress}
                className="rounded-md px-2 py-1.5 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                style={{ border: '0.5px solid var(--c-border-subtle)', color: 'var(--c-text-secondary)' }}
                title={ds.memoryRetryModuleList}
              >
                <RefreshCw size={14} className={statusChecking ? 'animate-spin' : ''} />
              </button>
              {action && statusReady && (
                <button
                  type="button"
                  onClick={() => onAction(action)}
                  disabled={actionInProgress}
                  className="rounded-md px-3 py-1.5 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-deep)', color: 'var(--c-text-secondary)' }}
                >
                  {actionInProgress ? <SpinnerIcon /> : actionLabel(action, ds)}
                </button>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// OpenViking configure form
// ---------------------------------------------------------------------------

type ProviderModelOption = {
  value: string
  label: string
  model: string
  providerKind: string
  apiBase?: string
}

type OVSelectionField = {
  selector: keyof Pick<OpenVikingDesktopConfig, 'vlmSelector' | 'embeddingSelector'>
  model: keyof Pick<OpenVikingDesktopConfig, 'vlmModel' | 'embeddingModel'>
  provider: keyof Pick<OpenVikingDesktopConfig, 'vlmProvider' | 'embeddingProvider'>
  apiKey: keyof Pick<OpenVikingDesktopConfig, 'vlmApiKey' | 'embeddingApiKey'>
  apiBase: keyof Pick<OpenVikingDesktopConfig, 'vlmApiBase' | 'embeddingApiBase'>
}

function buildOpenVikingModelOptions(
  providers: LlmProvider[],
  filter: (provider: LlmProvider, model: LlmProvider['models'][number]) => boolean,
): ProviderModelOption[] {
  return providers.flatMap((provider) =>
    provider.models
      .filter((model) => filter(provider, model))
      .map((model) => ({
        value: `${provider.name}^${model.model}`,
        label: `${provider.name} / ${model.model}`,
        model: model.model,
        providerKind: provider.provider,
        apiBase: provider.base_url ?? undefined,
      })),
  )
}

function buildOpenVikingConfigureParams(
  vlm: NonNullable<Awaited<ReturnType<typeof resolveOpenVikingConfig>>['vlm']>,
  embedding: NonNullable<Awaited<ReturnType<typeof resolveOpenVikingConfig>>['embedding']>,
): Record<string, unknown> {
  return {
    embedding_provider: embedding.provider,
    embedding_model: embedding.model,
    embedding_api_key: embedding.api_key,
    embedding_api_base: embedding.api_base,
    embedding_extra_headers: embedding.extra_headers ?? {},
    embedding_dimension: String(embedding.dimension),
    vlm_provider: vlm.provider,
    vlm_model: vlm.model,
    vlm_api_key: vlm.api_key,
    vlm_api_base: vlm.api_base,
    vlm_extra_headers: vlm.extra_headers ?? {},
  }
}

function resolveCurrentSelector(
  selector: string | undefined,
  model: string | undefined,
  options: ProviderModelOption[],
): string {
  const exact = selector?.trim()
  if (exact && options.some((option) => option.value === exact)) {
    return exact
  }
  const modelName = model?.trim()
  if (!modelName) {
    return ''
  }
  const matches = options.filter((option) => option.model === modelName)
  return matches.length === 1 ? matches[0].value : ''
}

function applySelectedOption(
  ov: OpenVikingDesktopConfig,
  value: string,
  field: OVSelectionField,
  options: ProviderModelOption[],
): OpenVikingDesktopConfig {
  if (!value) {
    return {
      ...ov,
      [field.selector]: undefined,
      [field.model]: undefined,
      [field.provider]: undefined,
      [field.apiKey]: undefined,
      [field.apiBase]: undefined,
    }
  }
  const option = options.find((item) => item.value === value)
  return {
    ...ov,
    [field.selector]: value,
    [field.model]: option?.model ?? value.split('^').slice(1).join('^'),
    [field.provider]: option?.providerKind ?? undefined,
    [field.apiKey]: undefined,
    [field.apiBase]: option?.apiBase ?? undefined,
  }
}

type OVConfigFormProps = {
  ov: OpenVikingDesktopConfig
  providers: LlmProvider[]
  loadingProviders: boolean
  onChange: (ov: OpenVikingDesktopConfig) => void
  onSave: () => void
  saving: boolean
  saveResult: 'ok' | 'error' | null
  ds: ReturnType<typeof useLocale>['t']['desktopSettings']
}

function OVConfigForm({ ov, providers, loadingProviders, onChange, onSave, saving, saveResult, ds }: OVConfigFormProps) {
  const vlmOptions = buildOpenVikingModelOptions(
    providers,
    (_provider, model) => !model.tags.includes('embedding'),
  )

  const embeddingOptions = buildOpenVikingModelOptions(
    providers,
    (_provider, model) => model.tags.includes('embedding'),
  )

  const currentVlm = resolveCurrentSelector(ov.vlmSelector, ov.vlmModel, vlmOptions)
  const currentEmb = resolveCurrentSelector(ov.embeddingSelector, ov.embeddingModel, embeddingOptions)

  return (
    <div className="flex flex-col gap-4">
      <div>
        <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memoryToolModel}</label>
        <p className="mb-1 text-xs text-[var(--c-text-muted)]">{ds.memoryToolModelDesc}</p>
        <SettingsModelDropdown
          value={loadingProviders ? '' : currentVlm}
          placeholder={loadingProviders ? '…' : (vlmOptions.length === 0 ? ds.memoryNoCompatibleModels : ds.memorySelectModel)}
          disabled={loadingProviders}
          options={vlmOptions}
          onChange={(v) => onChange(applySelectedOption(ov, v, {
            selector: 'vlmSelector',
            model: 'vlmModel',
            provider: 'vlmProvider',
            apiKey: 'vlmApiKey',
            apiBase: 'vlmApiBase',
          }, vlmOptions))}
        />
        {!loadingProviders && vlmOptions.length === 0 && (
          <p className="mt-2 text-xs text-[var(--c-text-muted)]">{ds.memoryNoCompatibleModels}</p>
        )}
      </div>

      <div>
        <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memoryEmbeddingModel}</label>
        <p className="mb-1 text-xs text-[var(--c-text-muted)]">{ds.memoryEmbeddingModelDesc}</p>
        <SettingsModelDropdown
          value={loadingProviders ? '' : currentEmb}
          placeholder={loadingProviders ? '…' : (embeddingOptions.length === 0 ? ds.memoryNoEmbeddingModels : ds.memorySelectModel)}
          disabled={loadingProviders}
          options={embeddingOptions}
          onChange={(v) => onChange(applySelectedOption(ov, v, {
            selector: 'embeddingSelector',
            model: 'embeddingModel',
            provider: 'embeddingProvider',
            apiKey: 'embeddingApiKey',
            apiBase: 'embeddingApiBase',
          }, embeddingOptions))}
        />
        {!loadingProviders && embeddingOptions.length === 0 && (
          <p className="mt-2 text-xs text-[var(--c-text-muted)]">{ds.memoryNoEmbeddingModels}</p>
        )}
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={onSave}
          disabled={saving}
          className="flex items-center gap-2 rounded-lg bg-[var(--c-btn-bg)] px-4 py-2 text-sm font-medium text-[var(--c-btn-text)] transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {saving && <SpinnerIcon />}
          {saving ? ds.memoryConfiguring : ds.memoryConfigureSave}
        </button>
        {saveResult === 'ok' && (
          <span className="flex items-center gap-1.5 text-xs text-green-400"><CheckCircle size={13} />{ds.memoryConfigured}</span>
        )}
        {saveResult === 'error' && (
          <span className="flex items-center gap-1.5 text-xs text-red-400"><XCircle size={13} />{ds.memoryConfigureError}</span>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

type Props = { accessToken?: string }

export function MemorySettings({ accessToken }: Props) {
  const { t, locale } = useLocale()
  const ds = t.desktopSettings
  const localMemoryToggleNote = locale === 'zh'
    ? '桌面本地记忆模式会在每轮结束后自动提交，此开关只有在 OpenViking 模式下才生效。'
    : 'Desktop local memory mode always commits each turn, so this switch only has an effect when OpenViking is selected.'

  const [memConfig, setMemConfigState] = useState<MemoryConfig | null>(null)
  const [entries, setEntries] = useState<MemoryEntry[]>([])
  const [snapshot, setSnapshot] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)
  const [confirmClearAll, setConfirmClearAll] = useState(false)

  // Bridge — same pattern as ModulesSettings
  const [bridgeOnline, setBridgeOnline] = useState<boolean | null>(null)
  const [ovModule, setOvModule] = useState<ModuleInfo | null>(null)
  const [moduleListProbe, setModuleListProbe] = useState<ModuleListProbe>('idle')
  const [actionInProgress, setActionInProgress] = useState(false)
  const [bridgeError, setBridgeError] = useState<string | null>(null)

  // OpenViking config
  const [ovDraft, setOvDraft] = useState<OpenVikingDesktopConfig>({})
  const [configuring, setConfiguring] = useState(false)
  const [configureResult, setConfigureResult] = useState<'ok' | 'error' | null>(null)

  // Providers for model pickers
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [providersLoaded, setProvidersLoaded] = useState(false)

  const api = getDesktopApi()

  const loadData = useCallback(async (quiet = false) => {
    if (!api?.memory) { setLoading(false); return }
    if (!quiet) setLoading(true); else setRefreshing(true)
    try {
      const cfg = await api.memory.getConfig()
      setMemConfigState(cfg)
      setOvDraft(cfg.openviking ?? {})
      if (cfg.enabled) {
        const listResp = await api.memory.list()
        setEntries(listResp.entries ?? [])
        if (cfg.provider === 'openviking') {
          const snap = await api.memory.getSnapshot()
          setSnapshot(snap.memory_block ?? '')
        }
      }
    } catch { /* ignore */ } finally {
      setLoading(false); setRefreshing(false)
    }
  }, [api])

  const loadBridge = useCallback(async () => {
    setBridgeError(null)
    try {
      const online = await checkBridgeAvailable()
      setBridgeOnline(online)
      if (!online) {
        setModuleListProbe('idle')
        return
      }
      setModuleListProbe('loading')
      try {
        const list = await bridgeClient.listModules()
        setOvModule(list.find((m) => m.id === 'openviking') ?? null)
        setModuleListProbe('ready')
      } catch (e) {
        setBridgeError(e instanceof Error ? e.message : 'Bridge error')
        setModuleListProbe('failed')
      }
    } catch (e) {
      setBridgeOnline(false)
      setModuleListProbe('idle')
      setBridgeError(e instanceof Error ? e.message : 'Bridge error')
    }
  }, [])

  const loadProviders = useCallback(async () => {
    if (!accessToken) { setProvidersLoaded(true); return }
    try { setProviders(await listLlmProviders(accessToken)) } catch { /* ignore */ }
    finally { setProvidersLoaded(true) }
  }, [accessToken])

  const syncLegacySelectors = useCallback((draft: OpenVikingDesktopConfig): OpenVikingDesktopConfig => {
    const vlmOptions = buildOpenVikingModelOptions(
      providers,
      (_provider, model) => !model.tags.includes('embedding'),
    )
    const embeddingOptions = buildOpenVikingModelOptions(
      providers,
      (_provider, model) => model.tags.includes('embedding'),
    )

    let next = draft
    const currentVlm = resolveCurrentSelector(draft.vlmSelector, draft.vlmModel, vlmOptions)
    if (currentVlm && currentVlm !== draft.vlmSelector) {
      next = applySelectedOption(next, currentVlm, {
        selector: 'vlmSelector',
        model: 'vlmModel',
        provider: 'vlmProvider',
        apiKey: 'vlmApiKey',
        apiBase: 'vlmApiBase',
      }, vlmOptions)
    }
    const currentEmbedding = resolveCurrentSelector(draft.embeddingSelector, draft.embeddingModel, embeddingOptions)
    if (currentEmbedding && currentEmbedding !== draft.embeddingSelector) {
      next = applySelectedOption(next, currentEmbedding, {
        selector: 'embeddingSelector',
        model: 'embeddingModel',
        provider: 'embeddingProvider',
        apiKey: 'embeddingApiKey',
        apiBase: 'embeddingApiBase',
      }, embeddingOptions)
    }
    return next
  }, [providers])

  useEffect(() => {
    void loadData()
    void loadBridge()
    void loadProviders()
  }, [loadData, loadBridge, loadProviders])

  useEffect(() => {
    if (providers.length === 0) {
      return
    }
    setOvDraft((prev) => {
      const next = syncLegacySelectors(prev)
      return JSON.stringify(next) === JSON.stringify(prev) ? prev : next
    })
  }, [providers, syncLegacySelectors])

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === 'visible') void loadBridge()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [loadBridge])

  const saveConfig = useCallback(async (next: MemoryConfig) => {
    if (!api?.memory) return
    await api.memory.setConfig(next)
    setMemConfigState(next)
  }, [api])

  const handleModuleAction = useCallback(async (action: ModuleAction) => {
    setActionInProgress(true); setBridgeError(null)
    try {
      const { operation_id } = await bridgeClient.performAction('openviking', action)
      await new Promise<void>((resolve, reject) => {
        let done = false
        const stop = bridgeClient.streamOperation(operation_id, () => {}, (result) => {
          if (done) return; done = true; stop()
          if (result.status === 'completed') resolve()
          else reject(new Error(result.error ?? `${action} failed`))
        })
      })
    } catch (e) {
      setBridgeError(e instanceof Error ? e.message : `${action} failed`)
    } finally {
      await new Promise((r) => setTimeout(r, 1500))
      await loadBridge()
      setActionInProgress(false)
    }
  }, [loadBridge])

  const handleConfigure = useCallback(async () => {
    setConfiguring(true)
    setConfigureResult(null)
    setBridgeError(null)
    try {
      await promiseWithTimeout((async () => {
        if (!accessToken) {
          throw new Error(ds.memoryConfigureError)
        }

        const vlmOptions = buildOpenVikingModelOptions(
          providers,
          (_provider, model) => !model.tags.includes('embedding'),
        )
        const embeddingOptions = buildOpenVikingModelOptions(
          providers,
          (_provider, model) => model.tags.includes('embedding'),
        )
        const vlmSelector = resolveCurrentSelector(ovDraft.vlmSelector, ovDraft.vlmModel, vlmOptions)
        const embeddingSelector = resolveCurrentSelector(ovDraft.embeddingSelector, ovDraft.embeddingModel, embeddingOptions)
        if (!vlmSelector || !embeddingSelector) {
          throw new Error(ds.memoryConfigureMissingModels)
        }

        const resolved = await resolveOpenVikingConfig(accessToken, {
          vlm_selector: vlmSelector,
          embedding_selector: embeddingSelector,
          embedding_dimension_hint: ovDraft.embeddingDimension,
        })
        if (!resolved.vlm || !resolved.embedding) {
          throw new Error(ds.memoryConfigureError)
        }

        const params = buildOpenVikingConfigureParams(resolved.vlm, resolved.embedding)
        const { operation_id } = await bridgeClient.performAction('openviking', 'configure', params)
        await new Promise<void>((resolve, reject) => {
          let done = false
          const timeout = setTimeout(() => {
            if (done) return
            done = true
            reject(new Error('configure timeout'))
          }, 45_000)
          const stop = bridgeClient.streamOperation(operation_id, () => {}, (result) => {
            if (done) return
            done = true
            clearTimeout(timeout)
            stop()
            if (result.status === 'completed') resolve()
            else reject(new Error(result.error ?? 'configure failed'))
          })
        })
        setConfigureResult('ok')
        const nextOvDraft: OpenVikingDesktopConfig = {
          ...ovDraft,
          vlmSelector: resolved.vlm.selector,
          vlmProvider: resolved.vlm.provider,
          vlmModel: resolved.vlm.model,
          vlmApiBase: resolved.vlm.api_base,
          vlmApiKey: undefined,
          embeddingSelector: resolved.embedding.selector,
          embeddingProvider: resolved.embedding.provider,
          embeddingModel: resolved.embedding.model,
          embeddingApiBase: resolved.embedding.api_base,
          embeddingApiKey: undefined,
          embeddingDimension: resolved.embedding.dimension,
        }
        setOvDraft(nextOvDraft)
        if (memConfig) {
          await saveConfig({ ...memConfig, provider: 'openviking', openviking: nextOvDraft })
        }
        await setSpawnProfile(accessToken, 'tool', resolved.vlm.selector)
        await loadBridge()
      })(), MEMORY_CONFIGURE_DEADLINE_MS)
    } catch (error) {
      const timedOut = error instanceof Error && error.message === '__configure_timeout__'
      setBridgeError(
        timedOut
          ? ds.memoryConfigureTimeout
          : isApiError(error)
            ? error.message
            : error instanceof Error
              ? error.message
              : ds.memoryConfigureError,
      )
      setConfigureResult('error')
    } finally {
      setConfiguring(false)
    }
  }, [accessToken, ds, loadBridge, memConfig, ovDraft, providers, saveConfig])

  const handleDelete = useCallback(async (id: string) => {
    if (!api?.memory) return
    try { await api.memory.delete(id); setEntries((p) => p.filter((e) => e.id !== id)) } catch { /* ignore */ }
    setConfirmDeleteId(null)
  }, [api])

  const handleClearAll = useCallback(async () => {
    if (!api?.memory) return
    for (const e of entries) { try { await api.memory.delete(e.id) } catch { /* ignore */ } }
    setEntries([]); setConfirmClearAll(false)
  }, [api, entries])

  // ---------------------------------------------------------------------------

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />
        <div className="flex items-center justify-center py-20"><SpinnerIcon /></div>
      </div>
    )
  }

  if (!api?.memory) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />
        <div className="rounded-xl bg-[var(--c-bg-menu)] py-16 text-center text-sm text-[var(--c-text-muted)]" style={{ border: '0.5px solid var(--c-border-subtle)' }}>
          Not available outside Desktop mode.
        </div>
      </div>
    )
  }

  const enabled = memConfig?.enabled ?? true
  const isLocal = (memConfig?.provider ?? 'local') !== 'openviking'
  const ovShowConfigure = Boolean(
    ovModule && ovModule.status !== 'not_installed',
  )

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />

      {/* ── Enable Memory toggle ── */}
      <div
        className="flex items-center justify-between rounded-xl px-4 py-3"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <div className="flex-1 pr-4">
          <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryEnabled}</p>
          <p className="text-xs text-[var(--c-text-muted)]">{ds.memoryEnabledDesc}</p>
        </div>
        {/* Toggle — same pattern as ModelConfigContent.tsx */}
        <label className="relative inline-flex shrink-0 cursor-pointer items-center">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => { if (memConfig) void saveConfig({ ...memConfig, enabled: e.target.checked }) }}
            className="peer sr-only"
          />
          <span className="h-5 w-9 rounded-full transition-colors" style={{ background: enabled ? 'var(--c-btn-bg)' : 'var(--c-border-mid)' }} />
          <span className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full transition-transform peer-checked:translate-x-4" style={{ background: enabled ? 'var(--c-btn-text)' : 'var(--c-bg-page)' }} />
        </label>
      </div>

      {enabled && (
        <>
          {/* ── Memory System — ModeCard from ConnectionSettingsContent ── */}
          <div className="flex flex-col gap-2">
            <p className="text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memorySystem}</p>
            <ModeCard
              icon={HardDrive}
              label={ds.memorySystemSimple}
              desc={ds.memorySystemSimpleDesc}
              selected={isLocal}
              onSelect={() => { if (memConfig) void saveConfig({ ...memConfig, provider: 'local' }) }}
            />
            <ModeCard
              icon={Database}
              label={ds.memorySystemOpenViking}
              desc={ds.memorySystemOpenVikingDesc}
              selected={!isLocal}
              onSelect={() => { if (memConfig) void saveConfig({ ...memConfig, provider: 'openviking' }) }}
            />
          </div>

          {memConfig && (
            <>
              <div className="flex flex-col gap-2">
                <p className="text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memorySummarizeSectionTitle}</p>
                <div
                  className="flex items-center justify-between rounded-xl px-4 py-3"
                  style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
                >
                  <div className="flex-1 pr-4">
                    <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryAutoSummarizeLabel}</p>
                    <p className="text-xs text-[var(--c-text-muted)]">{ds.memoryAutoSummarizeDesc}</p>
                  </div>
                  <label
                    className={`relative inline-flex shrink-0 items-center ${isLocal ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}`}
                  >
                    <input
                      type="checkbox"
                      checked={memConfig.memoryCommitEachTurn !== false}
                      disabled={isLocal}
                      onChange={(e) => {
                        if (isLocal) return
                        void saveConfig({ ...memConfig, memoryCommitEachTurn: e.target.checked })
                      }}
                      className="peer sr-only"
                    />
                    <span
                      className="h-5 w-9 rounded-full transition-colors"
                      style={{
                        background: memConfig.memoryCommitEachTurn !== false ? 'var(--c-btn-bg)' : 'var(--c-border-mid)',
                      }}
                    />
                    <span
                      className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full transition-transform peer-checked:translate-x-4"
                      style={{
                        background: memConfig.memoryCommitEachTurn !== false ? 'var(--c-btn-text)' : 'var(--c-bg-page)',
                      }}
                    />
                  </label>
                </div>
                {isLocal && (
                  <p className="text-[10px] text-[var(--c-text-muted)]">{localMemoryToggleNote}</p>
                )}
              </div>

              {!isLocal && (
                <div className="flex flex-col gap-4">
                  {bridgeError && (
                    <div className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm" style={{ background: 'rgba(239,68,68,0.08)', color: '#ef4444' }}>
                      <XCircle size={14} />{bridgeError}
                    </div>
                  )}

                  {/* Module card — mirrors ModulesSettings */}
                  <OVModuleCard
                    bridgeOnline={bridgeOnline}
                    bridgeOfflineHint={ds.modulesOffline}
                    module={ovModule}
                    moduleListProbe={moduleListProbe}
                    actionInProgress={actionInProgress}
                    onAction={handleModuleAction}
                    onRefreshModules={() => void loadBridge()}
                    ds={ds}
                  />

                  {ovShowConfigure && (
                    <OVConfigForm
                      ov={ovDraft}
                      providers={providers}
                      loadingProviders={!providersLoaded}
                      onChange={setOvDraft}
                      onSave={handleConfigure}
                      saving={configuring}
                      saveResult={configureResult}
                      ds={ds}
                    />
                  )}
                </div>
              )}
            </>
          )}
        </>
      )}

      {/* ── Memory content ── */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {isLocal ? <Brain size={15} className="text-[var(--c-text-secondary)]" /> : <FileText size={15} className="text-[var(--c-text-secondary)]" />}
          <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">
            {isLocal ? ds.memoryEntriesTitle : ds.memorySnapshotTitle}
          </h4>
          {isLocal && entries.length > 0 && (
            <span className="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium" style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-muted)' }}>
              {entries.length}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {isLocal && entries.length > 0 && (
            <button onClick={() => setConfirmClearAll(true)} className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-red-400 transition-colors hover:bg-red-500/10">
              <Trash2 size={12} />{ds.memoryClearAll}
            </button>
          )}
          <button onClick={() => void loadData(true)} disabled={refreshing} className="shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-40">
            <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {isLocal ? (
        entries.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-xl py-14" style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}>
            <Brain size={28} className="mb-3 text-[var(--c-text-muted)]" />
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryEmptyTitle}</p>
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">{ds.memoryEmptyDesc}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {entries.map((e) => <EntryCard key={e.id} entry={e} onDelete={(id) => setConfirmDeleteId(id)} />)}
          </div>
        )
      ) : (
        <SnapshotView snapshot={snapshot} />
      )}
  <ConfirmDialog open={confirmDeleteId !== null} onClose={() => setConfirmDeleteId(null)} onConfirm={() => void handleDelete(confirmDeleteId!)} message={ds.memoryDeleteConfirm} confirmLabel="Delete" />
  <ConfirmDialog open={confirmClearAll} onClose={() => setConfirmClearAll(false)} onConfirm={() => void handleClearAll()} message={ds.memoryClearAllConfirm} confirmLabel="Delete" />
    </div>
  )
}
