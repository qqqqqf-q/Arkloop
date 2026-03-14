import { useState, useEffect, useCallback, useRef } from 'react'
import { Cloud, HardDrive, Server, CheckCircle, XCircle } from 'lucide-react'
import { Reveal, inputCls, inputStyle, labelStyle, SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { ConnectionMode } from '@arkloop/shared/desktop'
import { useLocale } from '../contexts/LocaleContext'
import { createLlmProvider, listAvailableModels, createProviderModel } from '../api'
import type { AvailableModel } from '../api'

type Step =
  | 'welcome'
  | 'mode'
  | 'saas'
  | 'local-download'
  | 'local-provider'
  | 'self-host'
  | 'complete'

type Vendor = 'openai_responses' | 'openai_chat_completions' | 'anthropic'
type VerifyStatus = 'idle' | 'verifying' | 'verified' | 'failed'
type TestStatus = 'idle' | 'testing' | 'connected' | 'failed'
type ModelImportStatus = 'idle' | 'loading' | 'ready' | 'importing' | 'done' | 'failed'

type Props = { onComplete: () => void }

const LOCAL_ACCESS_TOKEN = 'desktop-local-token'

const VENDOR_OPTIONS = [
  { key: 'openai_responses' as const, label: 'OpenAI (Responses)', provider: 'openai', openai_api_mode: 'responses' as string | undefined },
  { key: 'openai_chat_completions' as const, label: 'OpenAI (Chat Completions)', provider: 'openai', openai_api_mode: 'chat_completions' as string | undefined },
  { key: 'anthropic' as const, label: 'Anthropic', provider: 'anthropic', openai_api_mode: undefined as string | undefined },
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

function StepIndicator({ current, total, stepOf }: {
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

function ModeCard({ icon: Icon, label, desc, selected, onSelect }: {
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
      <div style={{ height: '100%', borderRadius: '2px', background: 'var(--c-btn-bg)', width: `${percent}%`, transition: 'width 0.3s ease' }} />
    </div>
  )
}

export function OnboardingWizard({ onComplete }: Props) {
  const { t } = useLocale()
  const ob = t.onboarding

  const [step, setStep] = useState<Step>('welcome')
  const [selectedMode, setSelectedMode] = useState<ConnectionMode | null>(null)

  // local download
  const [downloadPhase, setDownloadPhase] = useState('')
  const [downloadPercent, setDownloadPercent] = useState(0)
  const [downloadError, setDownloadError] = useState('')

  // local provider
  const [vendor, setVendor] = useState<Vendor>('openai_responses')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState(VENDOR_URLS.openai_responses)
  const [verifyStatus, setVerifyStatus] = useState<VerifyStatus>('idle')

  // model import
  const [modelImportStatus, setModelImportStatus] = useState<ModelImportStatus>('idle')
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([])
  const [selectedModelIds, setSelectedModelIds] = useState<Set<string>>(new Set())
  const [createdProviderId, setCreatedProviderId] = useState<string | null>(null)

  // self-host
  const [selfHostUrl, setSelfHostUrl] = useState('')
  const [testStatus, setTestStatus] = useState<TestStatus>('idle')

  const [saving, setSaving] = useState(false)
  const apiKeyRef = useRef<HTMLInputElement>(null)
  const selfHostRef = useRef<HTMLInputElement>(null)

  const api = getDesktopApi()

  // step number for indicator
  const stepMeta = (() => {
    switch (step) {
      case 'mode': return { n: 1, total: 3 }
      case 'saas':
      case 'local-download':
      case 'local-provider':
      case 'self-host': return { n: 2, total: 3 }
      case 'complete': return { n: 3, total: 3 }
      default: return null
    }
  })()

  // vendor change -> fill default url and reset model state
  const handleVendorChange = useCallback((v: Vendor) => {
    setVendor(v)
    setBaseUrl(VENDOR_URLS[v])
    setVerifyStatus('idle')
    setModelImportStatus('idle')
    setAvailableModels([])
    setSelectedModelIds(new Set())
    setCreatedProviderId(null)
  }, [])

  // save mode to config
  const saveMode = useCallback(async (mode: ConnectionMode, extra?: Record<string, unknown>) => {
    if (!api) return
    const current = await api.config.get()
    await api.config.set({ ...current, mode, ...extra })
  }, [api])

  // mode -> next step
  const handleModeNext = useCallback(async () => {
    if (!selectedMode || !api) return
    await saveMode(selectedMode)
    switch (selectedMode) {
      case 'saas': setStep('saas'); break
      case 'local': {
        const available = await api.sidecar.isAvailable()
        setStep(available ? 'local-provider' : 'local-download')
        break
      }
      case 'self-hosted': setStep('self-host'); break
    }
  }, [selectedMode, api, saveMode])

  // saas next
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
  }, [api, saveMode, onComplete])

  // local download
  const startDownload = useCallback(async () => {
    if (!api) return
    setDownloadError('')
    setDownloadPercent(0)
    setDownloadPhase(ob.localDownloading)

    const unsub = api.sidecar.onDownloadProgress((p) => {
      setDownloadPhase(p.phase)
      setDownloadPercent(p.percent)
      if (p.error) setDownloadError(p.error)
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
    } catch (err) {
      unsub()
      setDownloadError(err instanceof Error ? err.message : ob.localDownloadFailed)
    }
  }, [api, ob])

  useEffect(() => {
    if (step === 'local-download') startDownload()
  }, [step]) // eslint-disable-line react-hooks/exhaustive-deps

  // auto-focus local provider api key input
  useEffect(() => {
    if (step === 'local-provider') {
      const t = setTimeout(() => apiKeyRef.current?.focus(), 420)
      return () => clearTimeout(t)
    }
  }, [step])

  // auto-focus self-host url input
  useEffect(() => {
    if (step === 'self-host') {
      const t = setTimeout(() => selfHostRef.current?.focus(), 420)
      return () => clearTimeout(t)
    }
  }, [step])

  // verify provider key, create provider in backend, and fetch available models
  const handleVerify = useCallback(async () => {
    setVerifyStatus('verifying')
    try {
      const vendorOpt = VENDOR_OPTIONS.find((o) => o.key === vendor)!
      const trimmedUrl = baseUrl.replace(/\/$/, '') || undefined

      // Create the provider via the sidecar API
      const provider = await createLlmProvider(LOCAL_ACCESS_TOKEN, {
        name: vendorOpt.label,
        provider: vendorOpt.provider,
        api_key: apiKey,
        ...(trimmedUrl ? { base_url: trimmedUrl } : {}),
        ...(vendorOpt.openai_api_mode ? { openai_api_mode: vendorOpt.openai_api_mode } : {}),
      })
      setCreatedProviderId(provider.id)
      setVerifyStatus('verified')

      // Fetch available models from the provider
      setModelImportStatus('loading')
      try {
        const resp = await listAvailableModels(LOCAL_ACCESS_TOKEN, provider.id)
        const models = resp.models ?? []
        setAvailableModels(models)
        // Pre-select all unconfigured models
        setSelectedModelIds(new Set(models.filter((m) => !m.configured).map((m) => m.id)))
        setModelImportStatus(models.length > 0 ? 'ready' : 'done')
      } catch {
        setAvailableModels([])
        setModelImportStatus('failed')
      }
    } catch {
      setVerifyStatus('failed')
    }
  }, [apiKey, baseUrl, vendor])

  // toggle model selection
  const toggleModelSelection = useCallback((modelId: string) => {
    setSelectedModelIds((prev) => {
      const next = new Set(prev)
      if (next.has(modelId)) next.delete(modelId)
      else next.add(modelId)
      return next
    })
  }, [])

  // import selected models
  const handleImportModels = useCallback(async () => {
    if (!createdProviderId || selectedModelIds.size === 0) return
    setModelImportStatus('importing')
    try {
      const modelIds = Array.from(selectedModelIds)
      for (let i = 0; i < modelIds.length; i++) {
        await createProviderModel(LOCAL_ACCESS_TOKEN, createdProviderId, {
          model: modelIds[i],
          is_default: i === 0,
          priority: modelIds.length - i,
        })
      }
      setModelImportStatus('done')
    } catch {
      setModelImportStatus('failed')
    }
  }, [createdProviderId, selectedModelIds])

  // finish local provider step
  const handleProviderDone = useCallback(async () => {
    if (!api) return
    setSaving(true)
    try {
      const current = await api.config.get()
      const vendorOpt = VENDOR_OPTIONS.find((o) => o.key === vendor)!
      await api.config.set({
        ...current,
        mode: 'local' as ConnectionMode,
        local: { ...current.local, provider: { vendor: vendorOpt.provider, apiKey, baseUrl } },
      })
      setStep('complete')
    } finally {
      setSaving(false)
    }
  }, [api, vendor, apiKey, baseUrl])

  // test self-host connection
  const handleSelfHostTest = useCallback(async () => {
    setTestStatus('testing')
    try {
      const resp = await fetch(`${selfHostUrl.replace(/\/$/, '')}/healthz`, {
        signal: AbortSignal.timeout(5000),
      })
      setTestStatus(resp.ok ? 'connected' : 'failed')
    } catch {
      setTestStatus('failed')
    }
  }, [selfHostUrl])

  // save self-host and proceed
  const handleSelfHostDone = useCallback(async () => {
    setSaving(true)
    try {
      await saveMode('self-hosted', { selfHosted: { baseUrl: selfHostUrl.replace(/\/$/, '') } })
      setStep('complete')
    } finally {
      setSaving(false)
    }
  }, [saveMode, selfHostUrl])

  // finish onboarding
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

  return api ? (
    <div style={{ minHeight: '100vh', background: 'var(--c-bg-page)', display: 'flex', flexDirection: 'column', position: 'relative', overflow: 'hidden' }}>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '48px 20px', position: 'relative', zIndex: 1 }}>
        <section style={{ width: 'min(460px, 100%)' }}>

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
              <button type="button" onClick={() => setStep('mode')} style={primaryBtn}>
                {ob.getStarted}
              </button>
            </div>
          </Reveal>

          {/* Mode select */}
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

          {/* SaaS */}
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

          {/* Local download */}
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
                  <button type="button" onClick={startDownload} style={primaryBtn}>
                    {ob.localRetryDownload}
                  </button>
                  <button type="button" onClick={() => setStep('mode')} style={ghostBtn}>
                    {ob.back}
                  </button>
                </div>
              )}
            </div>
          </Reveal>

          {/* Local provider config */}
          <Reveal active={step === 'local-provider'}>
            <div>
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                {ob.localProviderTitle}
              </div>
              <div style={{ fontSize: '13px', color: 'var(--c-placeholder)', marginBottom: '20px' }}>
                {ob.localProviderDesc}
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '14px', marginBottom: '24px' }}>
                <div>
                  <label style={labelStyle}>{ob.localProviderVendor}</label>
                  <select
                    className={inputCls}
                    style={{ ...inputStyle, cursor: 'pointer' }}
                    value={vendor}
                    onChange={(e) => handleVendorChange(e.target.value as Vendor)}
                    disabled={verifyStatus === 'verified'}
                  >
                    {VENDOR_OPTIONS.map((opt) => (
                      <option key={opt.key} value={opt.key}>{opt.label}</option>
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
                    onChange={(e) => { setApiKey(e.target.value); setVerifyStatus('idle') }}
                    autoComplete="off"
                    disabled={verifyStatus === 'verified'}
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
                    onChange={(e) => { setBaseUrl(e.target.value); setVerifyStatus('idle') }}
                    disabled={verifyStatus === 'verified'}
                  />
                </div>
              </div>

              {/* verify status */}
              {verifyStatus === 'verified' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#22c55e', marginBottom: '12px' }}>
                  <CheckCircle size={14} /> {ob.localProviderVerified}
                </div>
              )}
              {verifyStatus === 'failed' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#ef4444', marginBottom: '12px' }}>
                  <XCircle size={14} /> {ob.localProviderFailed}
                </div>
              )}

              {/* Model import section — appears after provider is verified */}
              {verifyStatus === 'verified' && modelImportStatus !== 'idle' && (
                <div style={{ marginBottom: '16px' }}>
                  <div style={{ fontSize: '14px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '4px' }}>
                    {ob.localImportModels}
                  </div>
                  <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginBottom: '12px' }}>
                    {ob.localImportModelsDesc}
                  </div>

                  {modelImportStatus === 'loading' && (
                    <div className="flex items-center gap-2" style={{ fontSize: '13px', color: 'var(--c-text-muted)' }}>
                      <SpinnerIcon /> {ob.localImportingModels}
                    </div>
                  )}

                  {modelImportStatus === 'ready' && availableModels.length > 0 && (
                    <div>
                      <div style={{
                        maxHeight: '200px',
                        overflowY: 'auto',
                        border: '1px solid var(--c-border-subtle)',
                        borderRadius: '8px',
                        marginBottom: '12px',
                      }}>
                        {availableModels.filter((m) => !m.configured).map((model) => (
                          <label
                            key={model.id}
                            className="flex items-center gap-3 px-3 py-2 cursor-pointer"
                            style={{
                              borderBottom: '1px solid var(--c-border-subtle)',
                              fontSize: '13px',
                              color: 'var(--c-text-primary)',
                            }}
                          >
                            <input
                              type="checkbox"
                              checked={selectedModelIds.has(model.id)}
                              onChange={() => toggleModelSelection(model.id)}
                              style={{ accentColor: 'var(--c-btn-bg)' }}
                            />
                            <span>{model.name || model.id}</span>
                          </label>
                        ))}
                      </div>
                      <button
                        type="button"
                        onClick={handleImportModels}
                        disabled={selectedModelIds.size === 0}
                        style={primaryBtn}
                        className="disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {ob.localSelectModels}
                      </button>
                    </div>
                  )}

                  {modelImportStatus === 'importing' && (
                    <div className="flex items-center gap-2" style={{ fontSize: '13px', color: 'var(--c-text-muted)' }}>
                      <SpinnerIcon /> {ob.localImportingModels}
                    </div>
                  )}

                  {modelImportStatus === 'done' && (
                    <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#22c55e', marginBottom: '12px' }}>
                      <CheckCircle size={14} /> {ob.localModelsImported}
                    </div>
                  )}

                  {modelImportStatus === 'failed' && availableModels.length === 0 && (
                    <div className="flex items-center gap-2" style={{ fontSize: '13px', color: 'var(--c-text-muted)', marginBottom: '12px' }}>
                      {ob.localNoModels}
                    </div>
                  )}
                </div>
              )}

              {verifyStatus !== 'verified' ? (
                <button
                  type="button"
                  onClick={handleVerify}
                  disabled={!apiKey || verifyStatus === 'verifying'}
                  style={primaryBtn}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {verifyStatus === 'verifying' ? <><SpinnerIcon /> {ob.localProviderVerifying}</> : ob.localProviderVerify}
                </button>
              ) : (modelImportStatus === 'done' || modelImportStatus === 'failed') ? (
                <button type="button" onClick={handleProviderDone} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
                  {saving ? <SpinnerIcon /> : ob.next}
                </button>
              ) : null}

              <button type="button" onClick={handleProviderDone} style={ghostBtn}>
                {ob.localProviderSkip}
              </button>
            </div>
          </Reveal>

          {/* Self-host */}
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
                  onChange={(e) => { setSelfHostUrl(e.target.value); setTestStatus('idle') }}
                />
              </div>

              {testStatus === 'connected' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#22c55e', marginBottom: '12px' }}>
                  <CheckCircle size={14} /> {ob.selfHostConnected}
                </div>
              )}
              {testStatus === 'failed' && (
                <div className="flex items-center gap-2" style={{ fontSize: '13px', color: '#ef4444', marginBottom: '12px' }}>
                  <XCircle size={14} /> {ob.selfHostFailed}
                </div>
              )}

              {testStatus !== 'connected' ? (
                <button
                  type="button"
                  onClick={handleSelfHostTest}
                  disabled={!selfHostUrl || testStatus === 'testing'}
                  style={primaryBtn}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {testStatus === 'testing' ? <><SpinnerIcon /> {ob.selfHostTesting}</> : ob.selfHostTest}
                </button>
              ) : (
                <button type="button" onClick={handleSelfHostDone} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
                  {saving ? <SpinnerIcon /> : ob.next}
                </button>
              )}

              <button type="button" onClick={() => setStep('mode')} style={ghostBtn}>
                {ob.back}
              </button>
            </div>
          </Reveal>

          {/* Completion */}
          <Reveal active={step === 'complete'}>
            <div style={{ textAlign: 'center' }}>
              <CheckCircle size={40} style={{ color: '#22c55e', margin: '0 auto 16px' }} />
              <div style={{ fontSize: '18px', fontWeight: 500, color: 'var(--c-text-heading)', marginBottom: '8px' }}>
                {ob.completionTitle}
              </div>
              <div style={{ fontSize: '14px', color: 'var(--c-placeholder)', marginBottom: '32px' }}>
                {ob.completionDesc}
              </div>
              <button type="button" onClick={handleComplete} disabled={saving} style={primaryBtn} className="disabled:cursor-not-allowed disabled:opacity-50">
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
  ) : null
}
