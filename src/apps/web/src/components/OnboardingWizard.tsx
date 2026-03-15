import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import {
  Bot,
  CheckCircle,
  Cloud,
  Database,
  Download,
  Globe,
  HardDrive,
  Plus,
  Search,
  Server,
  Shield,
  XCircle,
  type LucideIcon,
} from 'lucide-react'
import { ErrorCallout, isApiError, type AppError } from '@arkloop/shared'
import {
  Reveal,
  inputCls,
  inputStyle,
  labelStyle,
  SpinnerIcon,
  normalizeError,
} from '@arkloop/shared/components/auth-ui'
import { getDesktopApi, getDesktopAccessToken } from '@arkloop/shared/desktop'
import type { ConnectionMode } from '@arkloop/shared/desktop'
import { useLocale } from '../contexts/LocaleContext'
import {
  createLlmProvider,
  createProviderModel,
  listAvailableModels,
  listLlmProviders,
  updateLlmProvider,
} from '../api'
import type {
  AvailableModel,
  LlmProvider,
  LlmProviderModel,
} from '../api'
import {
  bridgeClient,
  checkBridgeAvailable,
  type ModuleAction,
  type ModuleInfo,
  type ModuleStatus,
} from '../api-bridge'

type Step =
  | 'welcome'
  | 'mode'
  | 'saas'
  | 'local-download'
  | 'local-provider'
  | 'local-modules'
  | 'self-host'
  | 'complete'

type Vendor = 'openai_responses' | 'openai_chat_completions' | 'anthropic'
type VerifyStatus = 'idle' | 'verifying' | 'verified' | 'failed'
type TestStatus = 'idle' | 'testing' | 'connected' | 'failed'
type ModelImportStatus =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'empty'
  | 'importing'
  | 'done'
  | 'failed'
type OptionalModuleId =
  | 'sandbox-docker'
  | 'openviking'
  | 'searxng'
  | 'firecrawl'
  | 'browser'
type ModuleRunState = 'idle' | 'running' | 'done' | 'failed'

type Props = { onComplete: () => void }

type ModuleSpec = {
  id: OptionalModuleId
  category: ModuleInfo['category']
  icon: LucideIcon
  title: string
  description: string
  recommended?: boolean
  dependsOn?: OptionalModuleId[]
}

const LOCAL_ACCESS_TOKEN = getDesktopAccessToken() ?? 'arkloop-desktop-local-token'

const VENDOR_OPTIONS = [
  {
    key: 'openai_responses' as const,
    label: 'OpenAI (Responses)',
    provider: 'openai',
    openai_api_mode: 'responses' as string | undefined,
  },
  {
    key: 'openai_chat_completions' as const,
    label: 'OpenAI (Chat Completions)',
    provider: 'openai',
    openai_api_mode: 'chat_completions' as string | undefined,
  },
  {
    key: 'anthropic' as const,
    label: 'Anthropic',
    provider: 'anthropic',
    openai_api_mode: undefined as string | undefined,
  },
] as const

const VENDOR_URLS: Record<Vendor, string> = {
  openai_responses: 'https://api.openai.com/v1',
  openai_chat_completions: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com/v1',
}

const btnBase: React.CSSProperties = {
  height: '38px',
  borderRadius: '10px',
  border: 'none',
  cursor: 'pointer',
  fontSize: '14px',
  fontWeight: 500,
  fontFamily: 'inherit',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: '6px',
  width: '100%',
}

const primaryBtn: React.CSSProperties = {
  ...btnBase,
  background: 'var(--c-btn-bg)',
  color: 'var(--c-btn-text)',
}

const ghostBtn: React.CSSProperties = {
  ...btnBase,
  marginTop: '4px',
  background: 'transparent',
  color: 'var(--c-placeholder)',
}

const secondaryActionBtn: React.CSSProperties = {
  height: '30px',
  borderRadius: '999px',
  border: '0.5px solid var(--c-border-subtle)',
  background: 'var(--c-bg-page)',
  color: 'var(--c-text-secondary)',
  fontSize: '12px',
  fontWeight: 500,
  fontFamily: 'inherit',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: '6px',
  padding: '0 12px',
  cursor: 'pointer',
}

const sectionCardStyle: React.CSSProperties = {
  border: '0.5px solid var(--c-border-subtle)',
  borderRadius: '14px',
  background: 'var(--c-bg-menu)',
  padding: '14px',
}

function normalizeMode(mode?: string | null): string | null {
  const value = mode?.trim()
  return value ? value : null
}

function providerMatches(
  provider: LlmProvider,
  vendorOpt: (typeof VENDOR_OPTIONS)[number],
): boolean {
  return provider.provider === vendorOpt.provider
    && normalizeMode(provider.openai_api_mode) === normalizeMode(vendorOpt.openai_api_mode)
}

function mergeConfiguredModels(
  current: LlmProviderModel[],
  next: LlmProviderModel[],
): LlmProviderModel[] {
  const merged = new Map<string, LlmProviderModel>()
  for (const model of current) merged.set(model.model, model)
  for (const model of next) merged.set(model.model, model)
  return Array.from(merged.values())
}

function isInstalledStatus(status: ModuleStatus): boolean {
  return status !== 'not_installed'
}

function actionForModuleStatus(status: ModuleStatus): ModuleAction | null {
  switch (status) {
    case 'not_installed':
      return 'install'
    case 'stopped':
      return 'start'
    default:
      return null
  }
}

function StepIndicator({
  current,
  total,
  stepOf,
}: {
  current: number
  total: number
  stepOf: (c: number, t: number) => string
}) {
  return (
    <div style={{ fontSize: '12px', color: 'var(--c-text-muted)', marginBottom: '24px' }}>
      {stepOf(current, total)}
    </div>
  )
}

function ModeCard({
  icon: Icon,
  label,
  desc,
  selected,
  onSelect,
}: {
  icon: typeof Cloud
  label: string
  desc: string
  selected: boolean
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className="flex items-start gap-3 rounded-xl p-4 text-left transition-colors"
      style={{
        width: '100%',
        cursor: 'pointer',
        fontFamily: 'inherit',
        border: selected ? '1.5px solid var(--c-text-secondary)' : '1px solid var(--c-border-subtle)',
        background: selected ? 'var(--c-bg-deep)' : 'var(--c-bg-page)',
      }}
    >
      <div
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg"
        style={{
          background: selected ? 'var(--c-btn-bg)' : 'var(--c-bg-sub)',
          color: selected ? 'var(--c-btn-text)' : 'var(--c-text-secondary)',
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
          border: selected ? '5px solid var(--c-btn-bg)' : '1.5px solid var(--c-border-subtle)',
          background: selected ? 'var(--c-bg-page)' : 'transparent',
        }}
      />
    </button>
  )
}

function ProgressBar({ percent }: { percent: number }) {
  return (
    <div style={{ height: '4px', borderRadius: '2px', background: 'var(--c-border-subtle)', overflow: 'hidden' }}>
      <div
        style={{
          height: '100%',
          borderRadius: '2px',
          background: 'var(--c-btn-bg)',
          width: `${percent}%`,
          transition: 'width 0.3s ease',
        }}
      />
    </div>
  )
}

function ModuleOptionCard({
  spec,
  selected,
  installed,
  disabled,
  runState,
  dependsLabel,
  recommendedLabel,
  installedLabel,
  installingLabel,
  requestFailedText,
  onSelect,
}: {
  spec: ModuleSpec
  selected: boolean
  installed: boolean
  disabled: boolean
  runState: ModuleRunState
  dependsLabel: string
  recommendedLabel: string
  installedLabel: string
  installingLabel: string
  requestFailedText: string
  onSelect: () => void
}) {
  const Icon = spec.icon
  const showSelected = installed || selected

  return (
    <button
      type="button"
      onClick={onSelect}
      disabled={disabled}
      className="rounded-2xl p-4 text-left transition-colors disabled:cursor-not-allowed"
      style={{
        width: '100%',
        border: showSelected ? '1px solid var(--c-text-secondary)' : '0.5px solid var(--c-border-subtle)',
        background: showSelected ? 'var(--c-bg-menu)' : 'var(--c-bg-page)',
        opacity: disabled && !installed ? 0.7 : 1,
      }}
    >
      <div className="flex items-start gap-3">
        <div
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl"
          style={{
            background: showSelected ? 'var(--c-btn-bg)' : 'var(--c-bg-sub)',
            color: showSelected ? 'var(--c-btn-text)' : 'var(--c-text-secondary)',
          }}
        >
          <Icon size={18} />
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-sm font-medium text-[var(--c-text-heading)]">{spec.title}</div>
              <div className="mt-1 text-xs leading-5 text-[var(--c-text-muted)]">{spec.description}</div>
            </div>
            <div
              className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full"
              style={{
                border: showSelected ? 'none' : '1px solid var(--c-border-subtle)',
                background: showSelected ? 'var(--c-btn-bg)' : 'transparent',
                color: showSelected ? 'var(--c-btn-text)' : 'transparent',
              }}
            >
              <CheckCircle size={14} />
            </div>
          </div>

          <div className="mt-3 flex flex-wrap gap-2">
            {spec.recommended && (
              <span className="rounded-full px-2 py-1 text-[11px]" style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-secondary)' }}>
                {recommendedLabel}
              </span>
            )}
            {spec.dependsOn?.length ? (
              <span className="rounded-full px-2 py-1 text-[11px]" style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-secondary)' }}>
                {dependsLabel} Sandbox
              </span>
            ) : null}
            {runState === 'running' ? (
              <span className="rounded-full px-2 py-1 text-[11px]" style={{ background: 'rgba(245, 158, 11, 0.14)', color: '#f59e0b' }}>
                {installingLabel}
              </span>
            ) : null}
            {runState === 'failed' ? (
              <span className="rounded-full px-2 py-1 text-[11px]" style={{ background: 'rgba(239, 68, 68, 0.14)', color: '#ef4444' }}>
                {requestFailedText}
              </span>
            ) : null}
            {(runState === 'done' || installed) ? (
              <span className="rounded-full px-2 py-1 text-[11px]" style={{ background: 'rgba(34, 197, 94, 0.14)', color: '#22c55e' }}>
                {installedLabel}
              </span>
            ) : null}
          </div>
        </div>
      </div>
    </button>
  )
}

async function runModuleAction(moduleId: string, action: ModuleAction): Promise<void> {
  const { operation_id } = await bridgeClient.performAction(moduleId, action)

  await new Promise<void>((resolve, reject) => {
    let finished = false
    const stop = bridgeClient.streamOperation(
      operation_id,
      () => {},
      (result) => {
        if (finished) return
        finished = true
        stop()
        if (result.status === 'completed') {
          resolve()
        } else {
          reject(new Error(result.error ?? `${action} failed`))
        }
      },
    )
  })
}

export function OnboardingWizard({ onComplete }: Props) {
  const { t, locale } = useLocale()
  const ob = t.onboarding
  const api = getDesktopApi()

  const [step, setStep] = useState<Step>('welcome')
  const [selectedMode, setSelectedMode] = useState<ConnectionMode | null>(null)

  const [downloadPhase, setDownloadPhase] = useState('')
  const [downloadPercent, setDownloadPercent] = useState(0)
  const [downloadError, setDownloadError] = useState('')

  const [vendor, setVendor] = useState<Vendor>('openai_responses')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState(VENDOR_URLS.openai_responses)
  const [verifyStatus, setVerifyStatus] = useState<VerifyStatus>('idle')
  const [providerError, setProviderError] = useState<AppError | null>(null)

  const [modelImportStatus, setModelImportStatus] = useState<ModelImportStatus>('idle')
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([])
  const [selectedModelIds, setSelectedModelIds] = useState<Set<string>>(new Set())
  const [createdProviderId, setCreatedProviderId] = useState<string | null>(null)
  const [configuredModels, setConfiguredModels] = useState<LlmProviderModel[]>([])
  const [modelError, setModelError] = useState<AppError | null>(null)
  const [showImportPanel, setShowImportPanel] = useState(false)
  const [showAddModelForm, setShowAddModelForm] = useState(false)
  const [manualModelName, setManualModelName] = useState('')
  const [addingModel, setAddingModel] = useState(false)

  const [bridgeOnline, setBridgeOnline] = useState<boolean | null>(null)
  const [modulesLoading, setModulesLoading] = useState(false)
  const [optionalModules, setOptionalModules] = useState<ModuleInfo[]>([])
  const [selectedModuleIds, setSelectedModuleIds] = useState<Set<OptionalModuleId>>(
    () => new Set(['sandbox-docker']),
  )
  const [moduleRunStates, setModuleRunStates] = useState<Record<string, ModuleRunState>>({})
  const [installingModules, setInstallingModules] = useState(false)
  const [moduleError, setModuleError] = useState<AppError | null>(null)

  const [selfHostUrl, setSelfHostUrl] = useState('')
  const [testStatus, setTestStatus] = useState<TestStatus>('idle')

  const [saving, setSaving] = useState(false)
  const apiKeyRef = useRef<HTMLInputElement>(null)
  const selfHostRef = useRef<HTMLInputElement>(null)

  const moduleSpecs = useMemo<ModuleSpec[]>(() => [
    {
      id: 'sandbox-docker',
      category: 'sandbox',
      icon: Shield,
      title: ob.localModulesSandboxTitle,
      description: ob.localModulesSandboxDesc,
      recommended: true,
    },
    {
      id: 'openviking',
      category: 'memory',
      icon: Database,
      title: ob.localModulesMemoryTitle,
      description: ob.localModulesMemoryDesc,
    },
    {
      id: 'searxng',
      category: 'search',
      icon: Search,
      title: ob.localModulesSearchTitle,
      description: ob.localModulesSearchDesc,
    },
    {
      id: 'firecrawl',
      category: 'search',
      icon: Globe,
      title: ob.localModulesCrawlerTitle,
      description: ob.localModulesCrawlerDesc,
    },
    {
      id: 'browser',
      category: 'browser',
      icon: Bot,
      title: ob.localModulesBrowserTitle,
      description: ob.localModulesBrowserDesc,
      dependsOn: ['sandbox-docker'],
    },
  ], [ob])

  const isLocalFlow = selectedMode === 'local'
    || step === 'local-download'
    || step === 'local-provider'
    || step === 'local-modules'

  const totalSteps = isLocalFlow ? 4 : 3

  const stepMeta = useMemo(() => {
    switch (step) {
      case 'mode':
        return { n: 1, total: totalSteps }
      case 'saas':
      case 'local-download':
      case 'local-provider':
      case 'self-host':
        return { n: 2, total: totalSteps }
      case 'local-modules':
        return { n: 3, total: 4 }
      case 'complete':
        return { n: totalSteps, total: totalSteps }
      default:
        return null
    }
  }, [step, totalSteps])

  const importableModels = useMemo(
    () => availableModels.filter((model) => !model.configured),
    [availableModels],
  )

  const orderedModules = useMemo(() => {
    return moduleSpecs.map((spec) => {
      const found = optionalModules.find((mod) => mod.id === spec.id)
      return {
        spec,
        module: found ?? {
          id: spec.id,
          name: spec.title,
          description: spec.description,
          category: spec.category,
          status: 'not_installed' as ModuleStatus,
          capabilities: {
            installable: true,
            configurable: true,
            healthcheck: true,
            bootstrap_supported: false,
            external_admin_supported: false,
            privileged_required: false,
          },
          depends_on: spec.dependsOn ?? [],
          mutually_exclusive: [],
        },
      }
    })
  }, [moduleSpecs, optionalModules])

  const moduleQueue = useMemo(() => {
    return orderedModules
      .filter(({ spec }) => selectedModuleIds.has(spec.id))
      .map(({ module }) => {
        const action = actionForModuleStatus(module.status)
        return action ? { id: module.id, action } : null
      })
      .filter((item): item is { id: string; action: ModuleAction } => item != null)
  }, [orderedModules, selectedModuleIds])

  const sectionWidth = step === 'local-modules'
    ? 'min(760px, 100%)'
    : 'min(520px, 100%)'

  const resetProviderState = useCallback(() => {
    setVerifyStatus('idle')
    setProviderError(null)
    setModelError(null)
    setModelImportStatus('idle')
    setAvailableModels([])
    setSelectedModelIds(new Set())
    setCreatedProviderId(null)
    setConfiguredModels([])
    setShowImportPanel(false)
    setShowAddModelForm(false)
    setManualModelName('')
  }, [])

  const handleVendorChange = useCallback((nextVendor: Vendor) => {
    setVendor(nextVendor)
    setBaseUrl(VENDOR_URLS[nextVendor])
    resetProviderState()
  }, [resetProviderState])

  const saveMode = useCallback(async (mode: ConnectionMode, extra?: Record<string, unknown>) => {
    if (!api) return
    const current = await api.config.get()
    await api.config.set({ ...current, mode, ...extra })
  }, [api])

  const handleModeNext = useCallback(async () => {
    if (!selectedMode || !api) return
    await saveMode(selectedMode)
    switch (selectedMode) {
      case 'saas':
        setStep('saas')
        break
      case 'local': {
        const available = await api.sidecar.isAvailable()
        setStep(available ? 'local-provider' : 'local-download')
        break
      }
      case 'self-hosted':
        setStep('self-host')
        break
    }
  }, [selectedMode, api, saveMode])

  const handleSaasNext = useCallback(async () => {
    if (!api) return
    setSaving(true)
    try {
      await saveMode('saas')
      await api.onboarding.complete()
      onComplete()
    } finally {
      setSaving(false)
    }
  }, [api, onComplete, saveMode])

  const startDownload = useCallback(async () => {
    if (!api) return
    setDownloadError('')
    setDownloadPercent(0)
    setDownloadPhase(ob.localDownloading)

    const unsub = api.sidecar.onDownloadProgress((progress) => {
      setDownloadPhase(progress.phase)
      setDownloadPercent(progress.percent)
      if (progress.error) setDownloadError(progress.error)
    })

    try {
      const result = await api.sidecar.download()
      unsub()
      if (!result.ok) {
        setDownloadError(ob.localDownloadFailed)
        return
      }
      setDownloadPhase(ob.localStarting)
      await api.sidecar.restart()
      setStep('local-provider')
    } catch (error) {
      unsub()
      setDownloadError(error instanceof Error ? error.message : ob.localDownloadFailed)
    }
  }, [api, ob.localDownloadFailed, ob.localDownloading, ob.localStarting])

  useEffect(() => {
    if (step === 'local-download') {
      void startDownload()
    }
  }, [startDownload, step])

  useEffect(() => {
    if (step !== 'local-provider') return
    const timer = setTimeout(() => apiKeyRef.current?.focus(), 420)
    return () => clearTimeout(timer)
  }, [step])

  useEffect(() => {
    if (step !== 'self-host') return
    const timer = setTimeout(() => selfHostRef.current?.focus(), 420)
    return () => clearTimeout(timer)
  }, [step])

  const upsertProviderCredential = useCallback(async (): Promise<LlmProvider> => {
    const vendorOpt = VENDOR_OPTIONS.find((option) => option.key === vendor)!
    const trimmedUrl = baseUrl.trim().replace(/\/$/, '')
    const providers = await listLlmProviders(LOCAL_ACCESS_TOKEN)
    const existing = providers.find((provider) =>
      provider.name === vendorOpt.label && providerMatches(provider, vendorOpt))
      ?? providers.find((provider) => providerMatches(provider, vendorOpt))

    if (existing) {
      return await updateLlmProvider(LOCAL_ACCESS_TOKEN, existing.id, {
        name: vendorOpt.label,
        provider: vendorOpt.provider,
        api_key: apiKey.trim(),
        base_url: trimmedUrl || null,
        openai_api_mode: vendorOpt.openai_api_mode ?? null,
      })
    }

    try {
      return await createLlmProvider(LOCAL_ACCESS_TOKEN, {
        name: vendorOpt.label,
        provider: vendorOpt.provider,
        api_key: apiKey.trim(),
        ...(trimmedUrl ? { base_url: trimmedUrl } : {}),
        ...(vendorOpt.openai_api_mode ? { openai_api_mode: vendorOpt.openai_api_mode } : {}),
      })
    } catch (error) {
      if (!isApiError(error) || error.code !== 'llm_providers.name_conflict') {
        throw error
      }

      const latestProviders = await listLlmProviders(LOCAL_ACCESS_TOKEN)
      const conflicted = latestProviders.find((provider) =>
        provider.name === vendorOpt.label && providerMatches(provider, vendorOpt))
        ?? latestProviders.find((provider) => provider.name === vendorOpt.label)

      if (!conflicted) throw error

      return await updateLlmProvider(LOCAL_ACCESS_TOKEN, conflicted.id, {
        name: vendorOpt.label,
        provider: vendorOpt.provider,
        api_key: apiKey.trim(),
        base_url: trimmedUrl || null,
        openai_api_mode: vendorOpt.openai_api_mode ?? null,
      })
    }
  }, [apiKey, baseUrl, vendor])

  const refreshAvailableModels = useCallback(async (
    providerId: string,
    options?: { openPanel?: boolean },
  ) => {
    if (options?.openPanel) setShowImportPanel(true)
    setModelImportStatus('loading')
    setModelError(null)
    try {
      const response = await listAvailableModels(LOCAL_ACCESS_TOKEN, providerId)
      const models = response.models ?? []
      const importable = models.filter((model) => !model.configured)
      setAvailableModels(models)
      setSelectedModelIds(new Set(importable.map((model) => model.id)))
      setModelImportStatus(importable.length > 0 ? 'ready' : 'empty')
    } catch (error) {
      setAvailableModels([])
      setSelectedModelIds(new Set())
      setModelImportStatus('failed')
      setModelError(normalizeError(error, t.requestFailed))
    }
  }, [t.requestFailed])

  const handleVerify = useCallback(async () => {
    setVerifyStatus('verifying')
    setProviderError(null)
    setModelError(null)
    try {
      const provider = await upsertProviderCredential()
      setCreatedProviderId(provider.id)
      setConfiguredModels(provider.models ?? [])
      setVerifyStatus('verified')
      await refreshAvailableModels(provider.id, { openPanel: true })
    } catch (error) {
      setVerifyStatus('failed')
      setProviderError(normalizeError(error, t.requestFailed))
    }
  }, [refreshAvailableModels, t.requestFailed, upsertProviderCredential])

  const handleOpenImportModels = useCallback(async () => {
    if (!createdProviderId) return
    await refreshAvailableModels(createdProviderId, { openPanel: true })
    setShowAddModelForm(false)
  }, [createdProviderId, refreshAvailableModels])

  const toggleModelSelection = useCallback((modelId: string) => {
    setSelectedModelIds((current) => {
      const next = new Set(current)
      if (next.has(modelId)) next.delete(modelId)
      else next.add(modelId)
      return next
    })
  }, [])

  const handleImportModels = useCallback(async () => {
    if (!createdProviderId || selectedModelIds.size === 0) return
    setModelImportStatus('importing')
    setModelError(null)

    try {
      const ids = Array.from(selectedModelIds)
      const imported: LlmProviderModel[] = []
      for (const [index, modelId] of ids.entries()) {
        const created = await createProviderModel(LOCAL_ACCESS_TOKEN, createdProviderId, {
          model: modelId,
          is_default: configuredModels.length === 0 && index === 0,
          priority: Math.max(ids.length - index, 1),
        })
        imported.push(created)
      }

      setConfiguredModels((current) => mergeConfiguredModels(current, imported))
      setAvailableModels((current) =>
        current.map((model) =>
          ids.includes(model.id) ? { ...model, configured: true } : model))
      setSelectedModelIds(new Set())
      setShowImportPanel(false)
      setModelImportStatus('done')
    } catch (error) {
      setModelImportStatus('failed')
      setModelError(normalizeError(error, t.requestFailed))
    }
  }, [configuredModels.length, createdProviderId, selectedModelIds, t.requestFailed])

  const handleAddModel = useCallback(async () => {
    const model = manualModelName.trim()
    if (!createdProviderId || !model) return

    setAddingModel(true)
    setModelError(null)
    try {
      const created = await createProviderModel(LOCAL_ACCESS_TOKEN, createdProviderId, {
        model,
        is_default: configuredModels.length === 0,
      })
      setConfiguredModels((current) => mergeConfiguredModels(current, [created]))
      setAvailableModels((current) =>
        current.map((item) =>
          item.id === model ? { ...item, configured: true } : item))
      setManualModelName('')
      setShowAddModelForm(false)
      setModelImportStatus('done')
    } catch (error) {
      setModelError(normalizeError(error, t.requestFailed))
    } finally {
      setAddingModel(false)
    }
  }, [configuredModels.length, createdProviderId, manualModelName, t.requestFailed])

  const handleProviderDone = useCallback(async () => {
    if (!api) return
    setSaving(true)
    try {
      await saveMode('local')
      setStep('local-modules')
    } finally {
      setSaving(false)
    }
  }, [api, saveMode])

  const loadOptionalModules = useCallback(async () => {
    const fallbackModules = moduleSpecs.map<ModuleInfo>((spec) => ({
      id: spec.id,
      name: spec.title,
      description: spec.description,
      category: spec.category,
      status: 'not_installed',
      capabilities: {
        installable: true,
        configurable: true,
        healthcheck: true,
        bootstrap_supported: false,
        external_admin_supported: false,
        privileged_required: false,
      },
      depends_on: spec.dependsOn ?? [],
      mutually_exclusive: [],
    }))

    setModulesLoading(true)
    setModuleError(null)
    try {
      const online = await checkBridgeAvailable()
      setBridgeOnline(online)
      if (!online) {
        setOptionalModules(fallbackModules)
        return
      }

      const remoteModules = await bridgeClient.listModules()
      setOptionalModules(
        fallbackModules.map((fallback) =>
          remoteModules.find((remote) => remote.id === fallback.id) ?? fallback),
      )
    } catch (error) {
      setBridgeOnline(false)
      setOptionalModules(fallbackModules)
      setModuleError(normalizeError(error, t.requestFailed))
    } finally {
      setModulesLoading(false)
    }
  }, [moduleSpecs, t.requestFailed])

  useEffect(() => {
    if (step !== 'local-modules') return
    let cancelled = false

    void (async () => {
      await loadOptionalModules()
      if (cancelled) return
      setModuleRunStates({})
    })()

    return () => {
      cancelled = true
    }
  }, [loadOptionalModules, step])

  useEffect(() => {
    if (step !== 'local-modules' || orderedModules.length === 0) return

    setSelectedModuleIds((current) => {
      const next = new Set(current)
      for (const { spec, module } of orderedModules) {
        if (isInstalledStatus(module.status)) {
          next.add(spec.id)
        }
      }
      if (next.has('browser')) next.add('sandbox-docker')
      return next
    })
  }, [orderedModules, step])

  const handleToggleModule = useCallback((moduleId: OptionalModuleId) => {
    if (installingModules || bridgeOnline === false) return

    const module = orderedModules.find((item) => item.spec.id === moduleId)?.module
    if (module && isInstalledStatus(module.status)) return

    setSelectedModuleIds((current) => {
      const next = new Set(current)
      if (next.has(moduleId)) next.delete(moduleId)
      else next.add(moduleId)

      if (moduleId === 'browser' && next.has('browser')) next.add('sandbox-docker')
      if (moduleId === 'sandbox-docker' && !next.has('sandbox-docker')) next.delete('browser')

      return next
    })
  }, [bridgeOnline, installingModules, orderedModules])

  const handleModulesContinue = useCallback(async () => {
    if (bridgeOnline !== true || moduleQueue.length === 0) {
      setStep('complete')
      return
    }

    setInstallingModules(true)
    setModuleError(null)
    setModuleRunStates(
      moduleQueue.reduce<Record<string, ModuleRunState>>((acc, item) => {
        acc[item.id] = 'idle'
        return acc
      }, {}),
    )

    try {
      for (const item of moduleQueue) {
        setModuleRunStates((current) => ({ ...current, [item.id]: 'running' }))
        try {
          await runModuleAction(item.id, item.action)
          setModuleRunStates((current) => ({ ...current, [item.id]: 'done' }))
        } catch (error) {
          setModuleRunStates((current) => ({ ...current, [item.id]: 'failed' }))
          throw error
        }
      }

      await loadOptionalModules()
      setStep('complete')
    } catch (error) {
      setModuleError(normalizeError(error, t.requestFailed))
    } finally {
      setInstallingModules(false)
    }
  }, [bridgeOnline, loadOptionalModules, moduleQueue, t.requestFailed])

  const handleSelfHostTest = useCallback(async () => {
    setTestStatus('testing')
    try {
      const response = await fetch(`${selfHostUrl.replace(/\/$/, '')}/healthz`, {
        signal: AbortSignal.timeout(5000),
      })
      setTestStatus(response.ok ? 'connected' : 'failed')
    } catch {
      setTestStatus('failed')
    }
  }, [selfHostUrl])

  const handleSelfHostDone = useCallback(async () => {
    setSaving(true)
    try {
      await saveMode('self-hosted', { selfHosted: { baseUrl: selfHostUrl.replace(/\/$/, '') } })
      setStep('complete')
    } finally {
      setSaving(false)
    }
  }, [saveMode, selfHostUrl])

  const handleComplete = useCallback(async () => {
    if (!api) return
    setSaving(true)
    try {
      await api.onboarding.complete()
      onComplete()
    } finally {
      setSaving(false)
    }
  }, [api, onComplete])

  if (!api) return null

  return (
    <div style={{ minHeight: '100vh', background: 'var(--c-bg-page)', display: 'flex', flexDirection: 'column', position: 'relative', overflow: 'hidden' }}>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '48px 20px', position: 'relative', zIndex: 1 }}>
        <section style={{ width: sectionWidth }}>
          {stepMeta && (
            <StepIndicator current={stepMeta.n} total={stepMeta.total} stepOf={ob.stepOf} />
          )}

          <Reveal active={step === 'welcome'}>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: '28px', fontWeight: 500, color: 'var(--c-text-primary)', marginBottom: '8px' }}>
                {ob.welcomeTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '32px' }}>
                {ob.welcomeDesc}
              </div>
              <button type="button" onClick={() => setStep('mode')} style={primaryBtn}>
                {ob.getStarted}
              </button>
            </div>
          </Reveal>

          <Reveal active={step === 'mode'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.modeTitle}
              </div>
              <div style={{ fontSize: '13px', color: 'var(--c-placeholder)', marginBottom: '20px' }}>
                {ob.modeDesc}
              </div>

              <div className="flex flex-col gap-3" style={{ marginBottom: '24px' }}>
                <ModeCard icon={Cloud} label={ob.saasTitle} desc={ob.saasDesc} selected={selectedMode === 'saas'} onSelect={() => setSelectedMode('saas')} />
                <ModeCard icon={HardDrive} label={ob.localTitle} desc={ob.localDesc} selected={selectedMode === 'local'} onSelect={() => setSelectedMode('local')} />
                <ModeCard icon={Server} label={ob.selfHostTitle} desc={ob.selfHostDesc} selected={selectedMode === 'self-hosted'} onSelect={() => setSelectedMode('self-hosted')} />
              </div>

              <button
                type="button"
                onClick={handleModeNext}
                disabled={!selectedMode}
                style={primaryBtn}
                className="disabled:cursor-not-allowed disabled:opacity-50"
              >
                {ob.next}
              </button>
              <button type="button" onClick={() => setStep('welcome')} style={ghostBtn}>
                {ob.back}
              </button>
            </div>
          </Reveal>

          <Reveal active={step === 'saas'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.saasTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '32px' }}>
                {ob.saasLoginHint}
              </div>
              <button type="button" onClick={handleSaasNext} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
                {saving ? <SpinnerIcon /> : ob.next}
              </button>
              <button type="button" onClick={() => setStep('mode')} style={ghostBtn}>
                {ob.back}
              </button>
            </div>
          </Reveal>

          <Reveal active={step === 'local-download'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.localTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '24px' }}>
                {downloadPhase || ob.localDownloading}
              </div>

              <ProgressBar percent={downloadPercent} />

              {downloadError && (
                <div style={{ marginTop: '16px' }}>
                  <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#ef4444', marginBottom: '12px' }}>
                    <XCircle size={14} />
                    {downloadError}
                  </div>
                  <button type="button" onClick={() => void startDownload()} style={primaryBtn}>
                    {ob.localRetryDownload}
                  </button>
                  <button type="button" onClick={() => setStep('mode')} style={ghostBtn}>
                    {ob.back}
                  </button>
                </div>
              )}
            </div>
          </Reveal>

          <Reveal active={step === 'local-provider'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.localProviderTitle}
              </div>
              <div style={{ fontSize: '13px', color: 'var(--c-placeholder)', marginBottom: '20px' }}>
                {ob.localProviderDesc}
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '14px', marginBottom: '18px' }}>
                <div>
                  <label style={labelStyle}>{ob.localProviderVendor}</label>
                  <select
                    className={inputCls}
                    style={{ ...inputStyle, cursor: 'pointer' }}
                    value={vendor}
                    onChange={(event) => handleVendorChange(event.target.value as Vendor)}
                  >
                    {VENDOR_OPTIONS.map((option) => (
                      <option key={option.key} value={option.key}>{option.label}</option>
                    ))}
                  </select>
                </div>

                <div>
                  <label style={labelStyle}>{ob.localProviderApiKey}</label>
                  <input
                    ref={apiKeyRef}
                    className={inputCls}
                    style={inputStyle}
                    type="password"
                    placeholder={ob.localProviderApiKeyPlaceholder}
                    value={apiKey}
                    onChange={(event) => {
                      setApiKey(event.target.value)
                      resetProviderState()
                    }}
                    autoComplete="off"
                  />
                </div>

                <div>
                  <label style={labelStyle}>{ob.localProviderBaseUrl}</label>
                  <input
                    className={inputCls}
                    style={inputStyle}
                    type="text"
                    placeholder={ob.localProviderBaseUrlPlaceholder}
                    value={baseUrl}
                    onChange={(event) => {
                      setBaseUrl(event.target.value)
                      resetProviderState()
                    }}
                  />
                </div>
              </div>

              {verifyStatus === 'verified' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#22c55e', marginBottom: '12px' }}>
                  <CheckCircle size={14} />
                  {ob.localProviderVerified}
                </div>
              )}

              {verifyStatus === 'failed' && !providerError && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#ef4444', marginBottom: '12px' }}>
                  <XCircle size={14} />
                  {ob.localProviderFailed}
                </div>
              )}

              {providerError && (
                    <ErrorCallout error={providerError} locale={locale} requestFailedText={t.requestFailed} />
              )}

              {createdProviderId && verifyStatus === 'verified' && (
                <div style={{ ...sectionCardStyle, marginTop: '16px', marginBottom: '16px' }}>
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <div style={{ fontSize: '14px', fontWeight: 500, color: 'var(--c-text-heading)' }}>
                        {ob.localImportModels}
                      </div>
                      <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginTop: '4px' }}>
                        {ob.localImportModelsDesc}
                      </div>
                    </div>

                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        onClick={() => void handleOpenImportModels()}
                        style={secondaryActionBtn}
                      >
                        <Download size={12} />
                        {ob.localImportModels}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setShowAddModelForm((current) => !current)
                          setShowImportPanel(false)
                          setModelError(null)
                        }}
                        style={secondaryActionBtn}
                      >
                        <Plus size={12} />
                        {ob.localAddModel}
                      </button>
                    </div>
                  </div>

                  {modelImportStatus === 'done' && (
                    <div className="mt-3 flex items-center gap-2 text-sm" style={{ color: '#22c55e' }}>
                      <CheckCircle size={14} />
                      {ob.localModelsImported}
                    </div>
                  )}

                  {modelError && (
                    <ErrorCallout error={modelError} locale={locale} requestFailedText={t.requestFailed} />
                  )}

                  {showAddModelForm && (
                    <div style={{ ...sectionCardStyle, marginTop: '12px', background: 'var(--c-bg-page)', padding: '12px' }}>
                      <label style={labelStyle}>{ob.localAddModel}</label>
                      <div className="flex flex-col gap-2 md:flex-row">
                        <input
                          className={inputCls}
                          style={{ ...inputStyle, flex: 1 }}
                          type="text"
                          placeholder={ob.localManualModelPlaceholder}
                          value={manualModelName}
                          onChange={(event) => setManualModelName(event.target.value)}
                        />
                        <button
                          type="button"
                          onClick={() => void handleAddModel()}
                          disabled={!manualModelName.trim() || addingModel}
                          style={{ ...primaryBtn, width: 'auto', minWidth: '132px' }}
                          className="disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {addingModel ? <SpinnerIcon /> : ob.localAddModel}
                        </button>
                      </div>
                    </div>
                  )}

                  {showImportPanel && (
                    <div style={{ ...sectionCardStyle, marginTop: '12px', background: 'var(--c-bg-page)', padding: '12px' }}>
                      {modelImportStatus === 'loading' || modelImportStatus === 'importing' ? (
                        <div className="flex items-center gap-2 text-sm text-[var(--c-text-muted)]">
                          <SpinnerIcon />
                          {ob.localImportingModels}
                        </div>
                      ) : importableModels.length > 0 ? (
                        <>
                          <div
                            style={{
                              maxHeight: '220px',
                              overflowY: 'auto',
                              border: '0.5px solid var(--c-border-subtle)',
                              borderRadius: '12px',
                              background: 'var(--c-bg-menu)',
                            }}
                          >
                            {importableModels.map((model, index) => (
                              <label
                                key={model.id}
                                className="flex cursor-pointer items-center gap-3 px-3 py-2.5"
                                style={{
                                  borderBottom: index < importableModels.length - 1 ? '0.5px solid var(--c-border-subtle)' : 'none',
                                }}
                              >
                                <input
                                  type="checkbox"
                                  checked={selectedModelIds.has(model.id)}
                                  onChange={() => toggleModelSelection(model.id)}
                                  style={{ accentColor: 'var(--c-btn-bg)' }}
                                />
                                <span style={{ fontSize: '13px', color: 'var(--c-text-primary)' }}>
                                  {model.name || model.id}
                                </span>
                              </label>
                            ))}
                          </div>

                          <button
                            type="button"
                            onClick={() => void handleImportModels()}
                            disabled={selectedModelIds.size === 0}
                            style={{ ...primaryBtn, marginTop: '12px' }}
                            className="disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {ob.localSelectModels}
                            {selectedModelIds.size > 0 ? ` (${selectedModelIds.size})` : ''}
                          </button>
                        </>
                      ) : (
                        <div style={{ fontSize: '13px', color: 'var(--c-text-muted)' }}>
                          {ob.localNoImportableModels}
                        </div>
                      )}
                    </div>
                  )}

                  <div style={{ marginTop: '14px' }}>
                    <div style={{ fontSize: '12px', fontWeight: 500, color: 'var(--c-text-secondary)', marginBottom: '8px' }}>
                      {ob.localConfiguredModels}
                    </div>
                    {configuredModels.length > 0 ? (
                      <div className="flex flex-wrap gap-2">
                        {configuredModels.map((model) => (
                          <span
                            key={model.id}
                            className="rounded-full px-3 py-1 text-xs"
                            style={{
                              border: '0.5px solid var(--c-border-subtle)',
                              background: 'var(--c-bg-page)',
                              color: 'var(--c-text-secondary)',
                            }}
                          >
                            {model.model}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <div style={{ fontSize: '13px', color: 'var(--c-text-muted)' }}>
                        {ob.localNoConfiguredModels}
                      </div>
                    )}
                  </div>
                </div>
              )}

              {verifyStatus !== 'verified' ? (
                <button
                  type="button"
                  onClick={() => void handleVerify()}
                  disabled={!apiKey.trim() || verifyStatus === 'verifying'}
                  style={primaryBtn}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {verifyStatus === 'verifying'
                    ? <><SpinnerIcon /> {ob.localProviderVerifying}</>
                    : ob.localProviderVerify}
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => void handleProviderDone()}
                  disabled={saving}
                  style={primaryBtn}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {saving ? <SpinnerIcon /> : ob.next}
                </button>
              )}

              <button
                type="button"
                onClick={() => (verifyStatus === 'verified' ? setStep('mode') : void handleProviderDone())}
                style={ghostBtn}
              >
                {verifyStatus === 'verified' ? ob.back : ob.localProviderSkip}
              </button>
            </div>
          </Reveal>

          <Reveal active={step === 'local-modules'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.localModulesTitle}
              </div>
              <div style={{ fontSize: '13px', color: 'var(--c-placeholder)', marginBottom: '20px' }}>
                {ob.localModulesDesc}
              </div>

              {bridgeOnline === false && (
                <div style={{ ...sectionCardStyle, marginBottom: '16px', fontSize: '13px', color: 'var(--c-text-muted)' }}>
                  {ob.localModulesInstallerOffline}
                </div>
              )}

              {moduleError && (
                <ErrorCallout error={moduleError} locale={locale} requestFailedText={t.requestFailed} />
              )}

              {modulesLoading ? (
                <div style={{ ...sectionCardStyle, marginBottom: '16px' }}>
                  <div className="flex items-center gap-2 text-sm text-[var(--c-text-muted)]">
                    <SpinnerIcon />
                    {ob.localModulesInstalling}
                  </div>
                </div>
              ) : (
                <div className="grid gap-3 md:grid-cols-2" style={{ marginBottom: '16px' }}>
                  {orderedModules.map(({ spec, module }) => (
                    <ModuleOptionCard
                      key={spec.id}
                      spec={spec}
                      selected={selectedModuleIds.has(spec.id)}
                      installed={isInstalledStatus(module.status)}
                      disabled={installingModules || bridgeOnline === false}
                      runState={moduleRunStates[spec.id] ?? 'idle'}
                      dependsLabel={ob.localModulesDependsOn}
                      recommendedLabel={ob.localModulesRecommended}
                      installedLabel={ob.localModulesInstalled}
                      installingLabel={ob.localModulesInstalling}
                      requestFailedText={t.requestFailed}
                      onSelect={() => handleToggleModule(spec.id)}
                    />
                  ))}
                </div>
              )}

              <button
                type="button"
                onClick={() => void handleModulesContinue()}
                disabled={installingModules || modulesLoading}
                style={primaryBtn}
                className="disabled:cursor-not-allowed disabled:opacity-50"
              >
                {installingModules
                  ? <><SpinnerIcon /> {ob.localModulesInstalling}</>
                  : (bridgeOnline === true && moduleQueue.length > 0 ? ob.localModulesContinue : ob.next)}
              </button>
              <button type="button" onClick={() => setStep('complete')} style={ghostBtn}>
                {ob.localModulesSkip}
              </button>
            </div>
          </Reveal>

          <Reveal active={step === 'self-host'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.selfHostTitle}
              </div>
              <div style={{ fontSize: '13px', color: 'var(--c-placeholder)', marginBottom: '20px' }}>
                {ob.selfHostDesc}
              </div>

              <div style={{ marginBottom: '24px' }}>
                <label style={labelStyle}>{ob.selfHostUrlLabel}</label>
                <input
                  ref={selfHostRef}
                  className={inputCls}
                  style={inputStyle}
                  type="url"
                  placeholder={ob.selfHostUrlPlaceholder}
                  value={selfHostUrl}
                  onChange={(event) => {
                    setSelfHostUrl(event.target.value)
                    setTestStatus('idle')
                  }}
                />
              </div>

              {testStatus === 'connected' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#22c55e', marginBottom: '12px' }}>
                  <CheckCircle size={14} />
                  {ob.selfHostConnected}
                </div>
              )}
              {testStatus === 'failed' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#ef4444', marginBottom: '12px' }}>
                  <XCircle size={14} />
                  {ob.selfHostFailed}
                </div>
              )}

              {testStatus !== 'connected' ? (
                <button
                  type="button"
                  onClick={() => void handleSelfHostTest()}
                  disabled={!selfHostUrl || testStatus === 'testing'}
                  style={primaryBtn}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {testStatus === 'testing' ? <><SpinnerIcon /> {ob.selfHostTesting}</> : ob.selfHostTest}
                </button>
              ) : (
                <button type="button" onClick={() => void handleSelfHostDone()} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
                  {saving ? <SpinnerIcon /> : ob.next}
                </button>
              )}

              <button type="button" onClick={() => setStep('mode')} style={ghostBtn}>
                {ob.back}
              </button>
            </div>
          </Reveal>

          <Reveal active={step === 'complete'}>
            <div style={{ textAlign: 'center' }}>
              <CheckCircle size={40} style={{ color: '#22c55e', margin: '0 auto 16px' }} />
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '8px' }}>
                {ob.completionTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '32px' }}>
                {ob.completionDesc}
              </div>
              <button type="button" onClick={() => void handleComplete()} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
                {saving ? <SpinnerIcon /> : ob.startChatting}
              </button>
            </div>
          </Reveal>
        </section>
      </div>

      <footer style={{ textAlign: 'center', padding: '16px', fontSize: '12px', color: 'var(--c-text-muted)', position: 'relative', zIndex: 1 }}>
        &copy; 2026 Arkloop
      </footer>
    </div>
  )
}
