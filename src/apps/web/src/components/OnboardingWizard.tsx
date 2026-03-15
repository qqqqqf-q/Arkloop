import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import {
  CheckCircle,
  Download,
  Plus,
  Settings,
  X,
  XCircle,
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

type Step = 'welcome' | 'provider' | 'complete'

type Vendor = 'openai_responses' | 'openai_chat_completions' | 'anthropic'
type VerifyStatus = 'idle' | 'verifying' | 'verified' | 'failed'
type ModelImportStatus =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'empty'
  | 'importing'
  | 'done'
  | 'failed'

type Props = { onComplete: () => void }

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
  borderRadius: '10px',
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

export function OnboardingWizard({ onComplete }: Props) {
  const { t, locale } = useLocale()
  const ob = t.onboarding
  const api = getDesktopApi()

  const [step, setStep] = useState<Step>('welcome')

  // Sidecar readiness (auto-download if needed)
  const [sidecarReady, setSidecarReady] = useState<boolean | null>(null)
  const [downloadPhase, setDownloadPhase] = useState('')
  const [downloadPercent, setDownloadPercent] = useState(0)
  const [downloadError, setDownloadError] = useState('')

  // Provider state
  const [vendor, setVendor] = useState<Vendor>('openai_responses')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState(VENDOR_URLS.openai_responses)
  const [verifyStatus, setVerifyStatus] = useState<VerifyStatus>('idle')
  const [providerError, setProviderError] = useState<AppError | null>(null)

  // Model import state
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

  const [saving, setSaving] = useState(false)
  const apiKeyRef = useRef<HTMLInputElement>(null)

  const stepMeta = useMemo(() => {
    switch (step) {
      case 'provider':
        return { n: 1, total: 2 }
      case 'complete':
        return { n: 2, total: 2 }
      default:
        return null
    }
  }, [step])

  const importableModels = useMemo(
    () => availableModels.filter((model) => !model.configured),
    [availableModels],
  )

  const providerVerified = step === 'provider' && verifyStatus === 'verified' && !!createdProviderId

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

  // Check sidecar readiness and auto-download when entering provider step
  const ensureSidecar = useCallback(async () => {
    if (!api) return
    const available = await api.sidecar.isAvailable()
    if (available) {
      setSidecarReady(true)
      return
    }

    setSidecarReady(false)
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
      setSidecarReady(true)
    } catch (error) {
      unsub()
      setDownloadError(error instanceof Error ? error.message : ob.localDownloadFailed)
    }
  }, [api, ob.localDownloadFailed, ob.localDownloading, ob.localStarting])

  useEffect(() => {
    if (step === 'provider') {
      void ensureSidecar()
    }
  }, [ensureSidecar, step])

  useEffect(() => {
    if (step === 'provider' && sidecarReady) {
      const timer = setTimeout(() => apiKeyRef.current?.focus(), 420)
      return () => clearTimeout(timer)
    }
  }, [step, sidecarReady])

  const handleWelcomeNext = useCallback(async () => {
    if (!api) return
    const current = await api.config.get()
    await api.config.set({ ...current, mode: 'local' })
    setStep('provider')
  }, [api])

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

  const handleProviderDone = useCallback(() => {
    setStep('complete')
  }, [])

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
      <style>{`
        @keyframes onb-slide-in {
          from { opacity: 0; transform: translateX(24px); }
          to { opacity: 1; transform: translateX(0); }
        }
        .onb-check { appearance: none; width: 16px; height: 16px; border: 1.5px solid var(--c-border-subtle); border-radius: 4px; background: var(--c-bg-input); cursor: pointer; position: relative; flex-shrink: 0; transition: background 0.15s, border-color 0.15s; }
        .onb-check:checked { background: var(--c-btn-bg); border-color: var(--c-btn-bg); }
        .onb-check:checked::after { content: ''; position: absolute; left: 4.5px; top: 1.5px; width: 5px; height: 9px; border: solid var(--c-btn-text); border-width: 0 2px 2px 0; transform: rotate(45deg); }
      `}</style>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '48px 20px', position: 'relative', zIndex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: '24px', width: '100%', justifyContent: 'center' }}>
        <section style={{ width: providerVerified ? 'min(520px, 100%)' : 'min(520px, 100%)', flexShrink: 0 }}>
          {stepMeta && (
            <StepIndicator current={stepMeta.n} total={stepMeta.total} stepOf={ob.stepOf} />
          )}

          {/* Welcome */}
          <Reveal active={step === 'welcome'}>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: '28px', fontWeight: 500, color: 'var(--c-text-primary)', marginBottom: '8px' }}>
                {ob.welcomeTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '32px' }}>
                {ob.welcomeDesc}
              </div>
              <button type="button" onClick={() => void handleWelcomeNext()} style={primaryBtn}>
                {ob.getStarted}
              </button>
            </div>
          </Reveal>

          {/* Provider configuration (with inline sidecar download if needed) */}
          <Reveal active={step === 'provider'}>
            <div>
              {sidecarReady === false && !downloadError && (
                <div style={{ marginBottom: '24px' }}>
                  <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '12px' }}>
                    {downloadPhase || ob.localDownloading}
                  </div>
                  <ProgressBar percent={downloadPercent} />
                </div>
              )}

              {downloadError && (
                <div style={{ marginBottom: '24px' }}>
                  <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#ef4444', marginBottom: '12px' }}>
                    <XCircle size={14} />
                    {downloadError}
                  </div>
                  <button type="button" onClick={() => { setDownloadError(''); void ensureSidecar() }} style={primaryBtn}>
                    {ob.localRetryDownload}
                  </button>
                </div>
              )}

              {(sidecarReady === true || sidecarReady === null) && (
                <>
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
                      onClick={handleProviderDone}
                      style={primaryBtn}
                    >
                      {ob.next}
                    </button>
                  )}

                  <button
                    type="button"
                    onClick={verifyStatus === 'verified' ? () => setStep('welcome') : handleProviderDone}
                    style={ghostBtn}
                  >
                    {verifyStatus === 'verified' ? ob.back : ob.localProviderSkip}
                  </button>
                </>
              )}
            </div>
          </Reveal>

          {/* Completion */}
          <Reveal active={step === 'complete'}>
            <div style={{ textAlign: 'center' }}>
              <CheckCircle size={40} style={{ color: '#22c55e', margin: '0 auto 16px' }} />
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '8px' }}>
                {ob.completionTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '12px' }}>
                {ob.completionDesc}
              </div>
              <div
                className="flex items-center justify-center gap-2"
                style={{
                  fontSize: '13px',
                  color: 'var(--c-text-muted)',
                  marginBottom: '32px',
                  padding: '10px 16px',
                  borderRadius: '10px',
                  background: 'var(--c-bg-menu)',
                  border: '0.5px solid var(--c-border-subtle)',
                }}
              >
                <Settings size={14} />
                {ob.completionModulesHint}
              </div>
              <button type="button" onClick={() => void handleComplete()} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
                {saving ? <SpinnerIcon /> : ob.startChatting}
              </button>
            </div>
          </Reveal>
        </section>

        {/* Right side panel – model import (visible after provider verification) */}
        {providerVerified && (
          <div style={{
            width: 'min(360px, 40vw)',
            flexShrink: 0,
            alignSelf: 'flex-start',
            marginTop: '40px',
            animation: 'onb-slide-in 0.38s cubic-bezier(0.4,0,0.2,1) both',
          }}>
            <div style={{ ...sectionCardStyle, maxHeight: 'calc(100vh - 160px)', overflowY: 'auto' }}>
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
                  <div className="flex items-center justify-between" style={{ marginBottom: '8px' }}>
                    <label style={{ ...labelStyle, marginBottom: 0 }}>{ob.localAddModel}</label>
                    <button
                      type="button"
                      onClick={() => { setShowAddModelForm(false); setModelError(null) }}
                      style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--c-text-muted)', padding: '2px', display: 'flex' }}
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="flex flex-col gap-2">
                    <input
                      className={inputCls}
                      style={inputStyle}
                      type="text"
                      placeholder={ob.localManualModelPlaceholder}
                      value={manualModelName}
                      onChange={(event) => setManualModelName(event.target.value)}
                    />
                    <button
                      type="button"
                      onClick={() => void handleAddModel()}
                      disabled={!manualModelName.trim() || addingModel}
                      style={{ ...primaryBtn, minWidth: '132px' }}
                      className="disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {addingModel ? <SpinnerIcon /> : ob.localAddModel}
                    </button>
                  </div>
                </div>
              )}

              {showImportPanel && (
                <div style={{ ...sectionCardStyle, marginTop: '12px', background: 'var(--c-bg-page)', padding: '12px' }}>
                  <div className="flex items-center justify-between" style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '12px', fontWeight: 500, color: 'var(--c-text-secondary)' }}>{ob.localImportModels}</span>
                    <button
                      type="button"
                      onClick={() => setShowImportPanel(false)}
                      style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--c-text-muted)', padding: '2px', display: 'flex' }}
                    >
                      <X size={14} />
                    </button>
                  </div>
                  {modelImportStatus === 'loading' ? (
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
                          opacity: modelImportStatus === 'importing' ? 0.6 : 1,
                          pointerEvents: modelImportStatus === 'importing' ? 'none' : 'auto',
                          transition: 'opacity 0.2s',
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
                              className="onb-check"
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
                        disabled={selectedModelIds.size === 0 || modelImportStatus === 'importing'}
                        style={{ ...primaryBtn, marginTop: '12px' }}
                        className="disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {modelImportStatus === 'importing'
                          ? <><SpinnerIcon /> {ob.localImportingModels}</>
                          : <>{ob.localSelectModels}{selectedModelIds.size > 0 ? ` (${selectedModelIds.size})` : ''}</>}
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
                  {configuredModels.length > 0 && (
                    <span style={{ fontWeight: 400, color: 'var(--c-text-muted)', marginLeft: '6px' }}>
                      ({configuredModels.length})
                    </span>
                  )}
                </div>
                {configuredModels.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5" style={{ maxHeight: '68px', overflowY: 'auto' }}>
                    {configuredModels.slice(0, 6).map((model) => (
                      <span
                        key={model.id}
                        className="rounded-full px-2.5 py-0.5 text-xs"
                        style={{
                          border: '0.5px solid var(--c-border-subtle)',
                          background: 'var(--c-bg-page)',
                          color: 'var(--c-text-secondary)',
                        }}
                      >
                        {model.model}
                      </span>
                    ))}
                    {configuredModels.length > 6 && (
                      <span className="rounded-full px-2.5 py-0.5 text-xs" style={{ color: 'var(--c-text-muted)' }}>
                        +{configuredModels.length - 6}
                      </span>
                    )}
                  </div>
                ) : (
                  <div style={{ fontSize: '13px', color: 'var(--c-text-muted)' }}>
                    {ob.localNoConfiguredModels}
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
        </div>
      </div>

      <footer style={{ textAlign: 'center', padding: '16px', fontSize: '12px', color: 'var(--c-text-muted)', position: 'relative', zIndex: 1 }}>
        &copy; 2026 Arkloop
      </footer>
    </div>
  )
}
