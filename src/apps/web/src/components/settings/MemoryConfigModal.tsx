import { useState, useEffect, useCallback } from 'react'
import { RefreshCw, CheckCircle, XCircle } from 'lucide-react'
import { Modal } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { MemoryConfig, OpenVikingDesktopConfig } from '@arkloop/shared/desktop'
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
import { secondaryButtonBorderStyle, secondaryButtonXsCls } from '../buttonStyles'
import { SettingsModelDropdown } from './SettingsModelDropdown'
import type { LocaleStrings } from '../../locales'

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MEMORY_CONFIGURE_DEADLINE_MS = 120_000

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

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
// Module status helpers
// ---------------------------------------------------------------------------

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
// Types
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

type ProviderModelOption = {
  value: string
  label: string
  model: string
  providerKind: string
  apiBase?: string
}

type OVSelectionField = {
  selector: keyof Pick<OpenVikingDesktopConfig, 'vlmSelector' | 'embeddingSelector' | 'rerankSelector'>
  model: keyof Pick<OpenVikingDesktopConfig, 'vlmModel' | 'embeddingModel' | 'rerankModel'>
  provider: keyof Pick<OpenVikingDesktopConfig, 'vlmProvider' | 'embeddingProvider' | 'rerankProvider'>
  apiKey: keyof Pick<OpenVikingDesktopConfig, 'vlmApiKey' | 'embeddingApiKey' | 'rerankApiKey'>
  apiBase: keyof Pick<OpenVikingDesktopConfig, 'vlmApiBase' | 'embeddingApiBase' | 'rerankApiBase'>
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

// ---------------------------------------------------------------------------
// Model option builders
// ---------------------------------------------------------------------------

function buildOpenVikingModelOptions(
  providers: LlmProvider[],
  filter: (provider: LlmProvider, model: LlmProvider['models'][number]) => boolean,
  options?: { requireShowInPicker?: boolean },
): ProviderModelOption[] {
  const requireShowInPicker = options?.requireShowInPicker ?? true
  return providers.flatMap((provider) =>
    provider.models
      .filter((model) => (!requireShowInPicker || model.show_in_picker) && filter(provider, model))
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
  rerank?: Awaited<ReturnType<typeof resolveOpenVikingConfig>>['rerank'],
): Record<string, unknown> {
  const params: Record<string, unknown> = {
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
  if (rerank) {
    params.rerank_provider = rerank.provider
    params.rerank_model = rerank.model
    params.rerank_api_key = rerank.api_key
    params.rerank_api_base = rerank.api_base
    params.rerank_extra_headers = rerank.extra_headers ?? {}
  }
  return params
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

// ---------------------------------------------------------------------------
// OVModuleCard
// ---------------------------------------------------------------------------

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
        <div className="flex items-center justify-between gap-2">
          <span className="min-w-0 flex-1 text-xs" style={{ color: statusLineColor }}>
            {statusLineText}
          </span>
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={() => onRefreshModules()}
              disabled={actionInProgress}
              className={`${secondaryButtonXsCls} rounded-md px-2`}
              style={secondaryButtonBorderStyle}
              title={ds.memoryRetryModuleList}
            >
              <RefreshCw size={14} className={statusChecking ? 'animate-spin' : ''} />
            </button>
            {action && statusReady && (
              <button
                type="button"
                onClick={() => onAction(action)}
                disabled={actionInProgress}
                className={`${secondaryButtonXsCls} rounded-md`}
                style={secondaryButtonBorderStyle}
              >
                {actionInProgress ? <SpinnerIcon /> : actionLabel(action, ds)}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// OVConfigForm
// ---------------------------------------------------------------------------

function OVConfigForm({ ov, providers, loadingProviders, onChange, onSave, saving, saveResult, ds }: OVConfigFormProps) {
  const vlmOptions = buildOpenVikingModelOptions(
    providers,
    (_provider, model) => !model.tags.includes('embedding'),
  )

  const embeddingOptions = buildOpenVikingModelOptions(
    providers,
    (_provider, model) => model.tags.includes('embedding'),
    { requireShowInPicker: false },
  )

  const rerankOptions = vlmOptions

  const currentVlm = resolveCurrentSelector(ov.vlmSelector, ov.vlmModel, vlmOptions)
  const currentEmb = resolveCurrentSelector(ov.embeddingSelector, ov.embeddingModel, embeddingOptions)
  const currentRerank = resolveCurrentSelector(ov.rerankSelector, ov.rerankModel, rerankOptions)

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

      <div>
        <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{ds.memoryRerankModel}</label>
        <p className="mb-1 text-xs text-[var(--c-text-muted)]">{ds.memoryRerankModelDesc}</p>
        <SettingsModelDropdown
          value={loadingProviders ? '' : currentRerank}
          placeholder={loadingProviders ? '...' : ds.memoryRerankOptional}
          disabled={loadingProviders}
          options={rerankOptions}
          onChange={(v) => onChange(applySelectedOption(ov, v, {
            selector: 'rerankSelector',
            model: 'rerankModel',
            provider: 'rerankProvider',
            apiKey: 'rerankApiKey',
            apiBase: 'rerankApiBase',
          }, rerankOptions))}
        />
      </div>

      <div className="flex items-center justify-end gap-3">
        <button
          onClick={onSave}
          disabled={saving}
          className="flex items-center gap-2 rounded-lg bg-[var(--c-btn-bg)] px-4 py-2 text-sm font-medium text-[var(--c-btn-text)] transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {saving && <SpinnerIcon />}
          {saving ? ds.memoryConfiguring : ds.memoryConfigureSave}
        </button>
        {saveResult === 'ok' && (
          <span className="flex items-center gap-1.5 text-xs text-green-400">
            <CheckCircle size={13} />{ds.memoryConfigured}
          </span>
        )}
        {saveResult === 'error' && (
          <span className="flex items-center gap-1.5 text-xs text-red-400">
            <XCircle size={13} />{ds.memoryConfigureError}
          </span>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// MemoryConfigModal
// ---------------------------------------------------------------------------

type Props = {
  open: boolean
  onClose: () => void
  accessToken?: string
  memConfig: MemoryConfig | null
  onConfigSaved: (config: MemoryConfig) => void
}

export function MemoryConfigModal({ open, onClose, accessToken, memConfig, onConfigSaved }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings

  // Bridge state
  const [bridgeOnline, setBridgeOnline] = useState<boolean | null>(null)
  const [ovModule, setOvModule] = useState<ModuleInfo | null>(null)
  const [moduleListProbe, setModuleListProbe] = useState<ModuleListProbe>('idle')
  const [actionInProgress, setActionInProgress] = useState(false)
  const [bridgeError, setBridgeError] = useState<string | null>(null)

  // OpenViking config draft
  const [ovDraft, setOvDraft] = useState<OpenVikingDesktopConfig>(memConfig?.openviking ?? {})
  const [configuring, setConfiguring] = useState(false)
  const [configureResult, setConfigureResult] = useState<'ok' | 'error' | null>(null)

  // Providers for model pickers
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [providersLoaded, setProvidersLoaded] = useState(false)

  const api = getDesktopApi()

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

  // Sync legacy selectors when providers load
  const syncLegacySelectors = useCallback((draft: OpenVikingDesktopConfig): OpenVikingDesktopConfig => {
    const vlmOptions = buildOpenVikingModelOptions(
      providers,
      (_provider, model) => !model.tags.includes('embedding'),
    )
    const embeddingOptions = buildOpenVikingModelOptions(
      providers,
      (_provider, model) => model.tags.includes('embedding'),
      { requireShowInPicker: false },
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
    const rerankOptions = buildOpenVikingModelOptions(
      providers,
      (_provider, model) => !model.tags.includes('embedding'),
    )
    const currentRerank = resolveCurrentSelector(draft.rerankSelector, draft.rerankModel, rerankOptions)
    if (currentRerank && currentRerank !== draft.rerankSelector) {
      next = applySelectedOption(next, currentRerank, {
        selector: 'rerankSelector',
        model: 'rerankModel',
        provider: 'rerankProvider',
        apiKey: 'rerankApiKey',
        apiBase: 'rerankApiBase',
      }, rerankOptions)
    }
    return next
  }, [providers])

  // Initialize draft from memConfig when modal opens
  useEffect(() => {
    if (open) {
      setOvDraft(memConfig?.openviking ?? {})
      setConfigureResult(null)
      setBridgeError(null)
      void loadBridge()
      void loadProviders()
    }
  }, [open, memConfig, loadBridge, loadProviders])

  useEffect(() => {
    if (providers.length === 0) return
    setOvDraft((prev) => {
      const next = syncLegacySelectors(prev)
      return JSON.stringify(next) === JSON.stringify(prev) ? prev : next
    })
  }, [providers, syncLegacySelectors])

  // Re-probe bridge when tab becomes visible
  useEffect(() => {
    if (!open) return
    const onVisible = () => {
      if (document.visibilityState === 'visible') void loadBridge()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [open, loadBridge])

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
          { requireShowInPicker: false },
        )
        const vlmSelector = resolveCurrentSelector(ovDraft.vlmSelector, ovDraft.vlmModel, vlmOptions)
        const embeddingSelector = resolveCurrentSelector(ovDraft.embeddingSelector, ovDraft.embeddingModel, embeddingOptions)
        if (!vlmSelector || !embeddingSelector) {
          throw new Error(ds.memoryConfigureMissingModels)
        }

        const rerankOptions = buildOpenVikingModelOptions(
          providers,
          (_provider, model) => !model.tags.includes('embedding'),
        )
        const rerankSelector = resolveCurrentSelector(ovDraft.rerankSelector, ovDraft.rerankModel, rerankOptions)

        const resolved = await resolveOpenVikingConfig(accessToken, {
          vlm_selector: vlmSelector,
          embedding_selector: embeddingSelector,
          embedding_dimension_hint: ovDraft.embeddingDimension,
          rerank_selector: rerankSelector || undefined,
        })
        if (!resolved.vlm || !resolved.embedding) {
          throw new Error(ds.memoryConfigureError)
        }

        const params = buildOpenVikingConfigureParams(
          resolved.vlm,
          resolved.embedding,
          resolved.rerank ?? undefined,
        )
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
          rerankSelector: resolved.rerank?.selector,
          rerankProvider: resolved.rerank?.provider,
          rerankModel: resolved.rerank?.model,
          rerankApiBase: resolved.rerank?.api_base,
          rerankApiKey: undefined,
        }
        setOvDraft(nextOvDraft)

        if (memConfig && api?.memory) {
          const updatedConfig: MemoryConfig = { ...memConfig, provider: 'openviking', openviking: nextOvDraft }
          await api.memory.setConfig(updatedConfig)
          onConfigSaved(updatedConfig)
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
  }, [accessToken, api, ds, loadBridge, memConfig, onConfigSaved, ovDraft, providers])

  const ovShowConfigure = Boolean(ovModule && ovModule.status !== 'not_installed')

  return (
    <Modal open={open} onClose={onClose} title={ds.memoryConfigureModalTitle} width="520px">
      <div className="flex flex-col gap-4">
        {bridgeError && (
          <div
            className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm"
            style={{ background: 'rgba(239,68,68,0.08)', color: '#ef4444' }}
          >
            <XCircle size={14} />{bridgeError}
          </div>
        )}

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
    </Modal>
  )
}
