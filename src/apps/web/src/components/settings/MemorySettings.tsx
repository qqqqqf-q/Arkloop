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
import { listLlmProviders, type LlmProvider } from '../../api'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

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

function moduleStatusLabel(status: ModuleStatus): string {
  switch (status) {
    case 'running': return 'Running'
    case 'stopped': return 'Stopped'
    case 'installed_disconnected': return 'Disconnected'
    case 'pending_bootstrap': return 'Pending'
    case 'error': return 'Error'
    case 'not_installed': return 'Not installed'
    default: return status
  }
}

function actionForStatus(status: ModuleStatus): ModuleAction | null {
  switch (status) {
    case 'not_installed': return 'install'
    case 'stopped': return 'start'
    case 'running': return 'stop'
    default: return null
  }
}

function actionLabel(action: ModuleAction): string {
  switch (action) {
    case 'install': return 'Install'
    case 'start': return 'Start'
    case 'stop': return 'Stop'
    case 'restart': return 'Restart'
    default: return action
  }
}

const inputCls =
  'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

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
            <span className={`inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ${categoryColor(entry.category)}`}>
              {entry.category}
            </span>
            {entry.scope === 'agent' && (
              <span className="inline-flex items-center rounded-full bg-indigo-500/15 px-1.5 py-0.5 text-[10px] font-medium text-indigo-400">agent</span>
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

type OVModuleCardProps = {
  bridgeOnline: boolean | null
  module: ModuleInfo | null
  actionInProgress: boolean
  onAction: (action: ModuleAction) => void
}

function OVModuleCard({ bridgeOnline, module, actionInProgress, onAction }: OVModuleCardProps) {
  const status: ModuleStatus = module?.status ?? 'not_installed'
  const action = actionForStatus(status)

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
        <p className="text-xs text-[var(--c-text-muted)]">
          Installer Bridge is required to install and manage OpenViking.
        </p>
      ) : (
        <div className="flex items-center justify-between">
          <span className="text-xs" style={{ color: moduleStatusColor(status) }}>
            {moduleStatusLabel(status)}
          </span>
          {action && bridgeOnline && (
            <button
              onClick={() => onAction(action)}
              disabled={actionInProgress}
              className="rounded-md px-3 py-1.5 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-deep)', color: 'var(--c-text-secondary)' }}
            >
              {actionInProgress ? <SpinnerIcon /> : actionLabel(action)}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// OpenViking configure form
// ---------------------------------------------------------------------------

const EMBEDDING_PROVIDERS = ['openai', 'volcengine', 'jina', 'ollama'] as const

type OVConfigFormProps = {
  ov: OpenVikingDesktopConfig
  providers: LlmProvider[]
  onChange: (ov: OpenVikingDesktopConfig) => void
  onSave: () => void
  saving: boolean
  saveResult: 'ok' | 'error' | null
  ds: ReturnType<typeof useLocale>['t']['desktopSettings']
}

function OVConfigForm({ ov, providers, onChange, onSave, saving, saveResult, ds }: OVConfigFormProps) {
  // VLM: show_in_picker models (chat models the user has already enabled)
  const vlmOptions = providers.flatMap((p) =>
    p.models
      .filter((m) => m.show_in_picker)
      .map((m) => ({ value: `${p.name}^${m.model}`, label: `${p.name} · ${m.model}` })),
  )

  // Embedding: only models tagged 'embedding', but NOT restricted to show_in_picker.
  // This lets embedding-specific models (e.g. text-embedding-3-small) appear without
  // the user needing to manually toggle show_in_picker for them.
  const embeddingOptions = providers.flatMap((p) =>
    p.models
      .filter((m) => m.tags.includes('embedding'))
      .map((m) => ({ value: `${p.name}^${m.model}`, label: `${p.name} · ${m.model}` })),
  )

  const handleModelSelect = (
    value: string,
    field: { model: keyof OpenVikingDesktopConfig; provider: keyof OpenVikingDesktopConfig; apiBase: keyof OpenVikingDesktopConfig },
  ) => {
    if (!value) {
      onChange({ ...ov, [field.model]: undefined, [field.provider]: undefined })
      return
    }
    const parts = value.split('^')
    const modelName = parts.slice(1).join('^')
    const providerName = parts[0]
    const p = providers.find((pr) => pr.name === providerName)
    onChange({
      ...ov,
      [field.model]: modelName,
      [field.provider]: p?.provider ?? 'openai',
      [field.apiBase]: p?.base_url ?? (ov[field.apiBase] as string | undefined),
    })
  }

  const currentVlm = ov.vlmModel
    ? vlmOptions.find((o) => o.value.endsWith(`^${ov.vlmModel}`))?.value ?? ''
    : ''

  const currentEmb = ov.embeddingModel
    ? embeddingOptions.find((o) => o.value.endsWith(`^${ov.embeddingModel}`))?.value ?? ''
    : ''

  return (
    <div className="flex flex-col gap-4">
      {/* Root API Key */}
      <div>
        <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memoryOpenvikingRootApiKey}</label>
        <p className="mb-1 text-xs text-[var(--c-text-muted)]">{ds.memoryOpenvikingRootApiKeyDesc}</p>
        <input
          type="password"
          value={ov.rootApiKey ?? ''}
          onChange={(e) => onChange({ ...ov, rootApiKey: e.target.value || undefined })}
          className={inputCls}
          placeholder="optional"
        />
      </div>

      {/* VLM model */}
      <div>
        <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memoryToolModel}</label>
        <p className="mb-1 text-xs text-[var(--c-text-muted)]">{ds.memoryToolModelDesc}</p>
        <select
          value={currentVlm}
          onChange={(e) => handleModelSelect(e.target.value, { model: 'vlmModel', provider: 'vlmProvider', apiBase: 'vlmApiBase' })}
          className={inputCls}
        >
          <option value="">— {vlmOptions.length === 0 ? 'No models configured' : 'Select a model'} —</option>
          {vlmOptions.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        {ov.vlmModel && (
          <div className="mt-2 grid grid-cols-2 gap-2">
            <input type="password" value={ov.vlmApiKey ?? ''} onChange={(e) => onChange({ ...ov, vlmApiKey: e.target.value || undefined })} className={inputCls} placeholder="API Key" />
            <input type="text" value={ov.vlmApiBase ?? ''} onChange={(e) => onChange({ ...ov, vlmApiBase: e.target.value || undefined })} className={inputCls} placeholder="API Base URL" />
          </div>
        )}
      </div>

      {/* Embedding model */}
      <div>
        <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memoryEmbeddingModel}</label>
        <p className="mb-1 text-xs text-[var(--c-text-muted)]">{ds.memoryEmbeddingModelDesc}</p>
        <select
          value={currentEmb}
          onChange={(e) => handleModelSelect(e.target.value, { model: 'embeddingModel', provider: 'embeddingProvider', apiBase: 'embeddingApiBase' })}
          className={inputCls}
        >
          <option value="">— {embeddingOptions.length === 0 ? 'No models configured' : 'Select a model'} —</option>
          {embeddingOptions.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        {ov.embeddingModel && (
          <div className="mt-2 grid grid-cols-2 gap-2">
            <div>
              <label className="mb-1 block text-[10px] text-[var(--c-text-muted)]">{ds.memoryEmbeddingProvider}</label>
              <select value={ov.embeddingProvider ?? 'openai'} onChange={(e) => onChange({ ...ov, embeddingProvider: e.target.value })} className={inputCls}>
                {EMBEDDING_PROVIDERS.map((p) => <option key={p} value={p}>{p}</option>)}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-[10px] text-[var(--c-text-muted)]">{ds.memoryEmbeddingDimension}</label>
              <input type="number" value={ov.embeddingDimension ?? 1024} onChange={(e) => onChange({ ...ov, embeddingDimension: Number(e.target.value) || 1024 })} className={inputCls} min={1} />
            </div>
            <input type="password" value={ov.embeddingApiKey ?? ''} onChange={(e) => onChange({ ...ov, embeddingApiKey: e.target.value || undefined })} className={inputCls} placeholder={ds.memoryEmbeddingApiKey} />
            <input type="text" value={ov.embeddingApiBase ?? ''} onChange={(e) => onChange({ ...ov, embeddingApiBase: e.target.value || undefined })} className={inputCls} placeholder={ds.memoryEmbeddingApiBase} />
          </div>
        )}
      </div>

      {/* Save */}
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
  const { t } = useLocale()
  const ds = t.desktopSettings

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
  const [actionInProgress, setActionInProgress] = useState(false)
  const [bridgeError, setBridgeError] = useState<string | null>(null)

  // OpenViking config
  const [ovDraft, setOvDraft] = useState<OpenVikingDesktopConfig>({})
  const [configuring, setConfiguring] = useState(false)
  const [configureResult, setConfigureResult] = useState<'ok' | 'error' | null>(null)

  // Providers for model pickers
  const [providers, setProviders] = useState<LlmProvider[]>([])

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
      if (online) {
        const list = await bridgeClient.listModules()
        setOvModule(list.find((m) => m.id === 'openviking') ?? null)
      } else {
        setOvModule(null)
      }
    } catch (e) {
      setBridgeOnline(false)
      setBridgeError(e instanceof Error ? e.message : 'Bridge error')
    }
  }, [])

  const loadProviders = useCallback(async () => {
    if (!accessToken) return
    try { setProviders(await listLlmProviders(accessToken)) } catch { /* ignore */ }
  }, [accessToken])

  useEffect(() => {
    void loadData()
    void loadBridge()
    void loadProviders()
  }, [loadData, loadBridge, loadProviders])

  const saveConfig = useCallback(async (next: MemoryConfig) => {
    if (!api?.memory) return
    await api.memory.setConfig(next)
    setMemConfigState(next)
  }, [api])

  // Module action — identical to ModulesSettings.tsx handleAction
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
      await loadBridge()
    } catch (e) {
      setBridgeError(e instanceof Error ? e.message : `${action} failed`)
    } finally {
      setActionInProgress(false)
    }
  }, [loadBridge])

  const handleConfigure = useCallback(async () => {
    setConfiguring(true); setConfigureResult(null)
    try {
      const params: Record<string, string> = {}
      if (ovDraft.embeddingProvider)   params['embedding.provider']  = ovDraft.embeddingProvider
      if (ovDraft.embeddingModel)      params['embedding.model']     = ovDraft.embeddingModel
      if (ovDraft.embeddingApiKey)     params['embedding.api_key']   = ovDraft.embeddingApiKey
      if (ovDraft.embeddingApiBase)    params['embedding.api_base']  = ovDraft.embeddingApiBase
      if (ovDraft.embeddingDimension)  params['embedding.dimension'] = String(ovDraft.embeddingDimension)
      if (ovDraft.vlmProvider)  params['vlm.provider'] = ovDraft.vlmProvider
      if (ovDraft.vlmModel)     params['vlm.model']    = ovDraft.vlmModel
      if (ovDraft.vlmApiKey)    params['vlm.api_key']  = ovDraft.vlmApiKey
      if (ovDraft.vlmApiBase)   params['vlm.api_base'] = ovDraft.vlmApiBase
      if (ovDraft.rootApiKey)   params['root_api_key'] = ovDraft.rootApiKey

      const { operation_id } = await bridgeClient.performAction('openviking', 'configure', params)
      await new Promise<void>((resolve, reject) => {
        let done = false
        const stop = bridgeClient.streamOperation(operation_id, () => {}, (result) => {
          if (done) return; done = true; stop()
          if (result.status === 'completed') resolve()
          else reject(new Error(result.error ?? 'configure failed'))
        })
      })
      setConfigureResult('ok')
      if (memConfig) await saveConfig({ ...memConfig, provider: 'openviking', openviking: ovDraft })
      await loadBridge()
    } catch {
      setConfigureResult('error')
    } finally {
      setConfiguring(false)
    }
  }, [ovDraft, memConfig, saveConfig, loadBridge])

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
  const ovRunning = ovModule?.status === 'running'

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

          {/* ── OpenViking section ── */}
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
                module={ovModule}
                actionInProgress={actionInProgress}
                onAction={handleModuleAction}
              />

              {/* Configure form — only after module is running */}
              {ovRunning && (
                <OVConfigForm
                  ov={ovDraft}
                  providers={providers}
                  onChange={setOvDraft}
                  onSave={handleConfigure}
                  saving={configuring}
                  saveResult={configureResult}
                  ds={ds}
                />
              )}
            </div>
          )}

          {/* ── Memory content ── */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              {isLocal ? <Brain size={15} className="text-[var(--c-text-secondary)]" /> : <FileText size={15} className="text-[var(--c-text-secondary)]" />}
              <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">
                {isLocal ? ds.memoryEntriesTitle : ds.memorySnapshotTitle}
              </h4>
              {isLocal && entries.length > 0 && (
                <span className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium" style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-muted)' }}>
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
        </>
      )}

      <ConfirmDialog open={confirmDeleteId !== null} onClose={() => setConfirmDeleteId(null)} onConfirm={() => void handleDelete(confirmDeleteId!)} message={ds.memoryDeleteConfirm} confirmLabel="Delete" />
      <ConfirmDialog open={confirmClearAll} onClose={() => setConfirmClearAll(false)} onConfirm={() => void handleClearAll()} message={ds.memoryClearAllConfirm} confirmLabel="Delete" />
    </div>
  )
}
