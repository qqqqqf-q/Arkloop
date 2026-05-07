import { useState, useEffect, useCallback } from 'react'
import {
  CheckCircle,
  ChevronDown,
  ChevronRight,
  RefreshCw,
  RotateCcw,
  Wifi,
  XCircle,
} from 'lucide-react'
import { ErrorCallout } from '@arkloop/shared'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type {
  ConnectionMode,
  DesktopConfig,
  LocalPortMode,
  SidecarRuntime,
} from '@arkloop/shared/desktop'
import { useLocale } from '../contexts/LocaleContext'
import { SettingsButton } from './settings/_SettingsButton'
import { SettingsInput } from './settings/_SettingsInput'
import { SettingsSelect } from './settings/_SettingsSelect'

type Props = {
  initialConfig?: DesktopConfig | null
}

const DEFAULT_RUNTIME: SidecarRuntime = {
  status: 'stopped',
  port: null,
  portMode: 'auto',
}

function StatusBadge({ status, t }: { status: SidecarRuntime['status']; t: Record<string, string> }) {
  const map: Record<SidecarRuntime['status'], { color: string; label: string }> = {
    running: { color: '#22c55e', label: t.running },
    stopped: { color: '#94a3b8', label: t.stopped },
    starting: { color: '#f59e0b', label: t.starting },
    crashed: { color: '#ef4444', label: t.crashed },
  }
  const { color, label } = map[status]
  return (
    <span className="inline-flex items-center gap-1.5 text-xs">
      <span className="h-2 w-2 rounded-full" style={{ background: color }} />
      <span style={{ color }}>{label}</span>
    </span>
  )
}

function FieldRow({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      <div className="text-sm text-[var(--c-text-secondary)]">{label}</div>
      <div className="font-mono text-sm text-[var(--c-text-primary)]">{value}</div>
    </div>
  )
}

function applyConfigToState(config: DesktopConfig, setters: {
  setMode: (mode: ConnectionMode) => void
  setSaasUrl: (value: string) => void
  setSelfHostedUrl: (value: string) => void
  setLocalPort: (value: string) => void
  setLocalPortMode: (value: LocalPortMode) => void
}) {
  setters.setMode(config.mode)
  setters.setSaasUrl(config.saas.baseUrl)
  setters.setSelfHostedUrl(config.selfHosted.baseUrl)
  setters.setLocalPort(String(config.local.port))
  setters.setLocalPortMode(config.local.portMode)
}

export function ConnectionSettingsContent({ initialConfig = null }: Props) {
  const { t, locale } = useLocale()
  const ct = t.connection
  const api = getDesktopApi()

  const [configSnapshot, setConfigSnapshot] = useState<DesktopConfig | null>(null)
  const [mode, setMode] = useState<ConnectionMode>('local')
  const [saasUrl, setSaasUrl] = useState('')
  const [selfHostedUrl, setSelfHostedUrl] = useState('')
  const [localPort, setLocalPort] = useState('19001')
  const [localPortMode, setLocalPortMode] = useState<LocalPortMode>('auto')
  const [sidecarRuntime, setSidecarRuntime] = useState<SidecarRuntime>(DEFAULT_RUNTIME)
  const [testResult, setTestResult] = useState<'connected' | 'failed' | null>(null)
  const [saving, setSaving] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [localError, setLocalError] = useState('')

  useEffect(() => {
    if (!api) return

    const applyConfig = (config: DesktopConfig) => {
      setConfigSnapshot(config)
      applyConfigToState(config, {
        setMode,
        setSaasUrl,
        setSelfHostedUrl,
        setLocalPort,
        setLocalPortMode,
      })
    }

    if (initialConfig) {
      applyConfig(initialConfig)
    } else {
      void api.config.get().then(applyConfig)
    }
    void api.sidecar.getRuntime().then(setSidecarRuntime)

    const unsubConfig = api.config.onChanged((config) => {
      applyConfig(config)
    })
    const unsubRuntime = api.sidecar.onRuntimeChanged((runtime) => {
      setSidecarRuntime(runtime)
    })

    return () => {
      unsubConfig()
      unsubRuntime()
    }
  }, [api, initialConfig])

  useEffect(() => {
    if (!api || mode !== 'local' || sidecarRuntime.status !== 'running') return
    const id = window.setInterval(() => {
      void api.sidecar.getRuntime().then(setSidecarRuntime)
    }, 10000)
    return () => window.clearInterval(id)
  }, [api, mode, sidecarRuntime.status])

  const handleSave = useCallback(async () => {
    if (!api) return

    const parsedPort = Number.parseInt(localPort, 10)
    if (mode === 'local' && localPortMode === 'manual' && (!Number.isInteger(parsedPort) || parsedPort < 1 || parsedPort > 65535)) {
      setLocalError(locale === 'zh'
        ? `${ct.manualPort}必须在 1 到 65535 之间。`
        : `${ct.manualPort} must be between 1 and 65535.`)
      return
    }

    setSaving(true)
    setLocalError('')
    try {
      const current = await api.config.get()
      await api.config.set({
        ...current,
        mode,
        saas: { baseUrl: saasUrl },
        selfHosted: { baseUrl: selfHostedUrl },
        local: {
          port: mode === 'local' && localPortMode === 'manual'
            ? parsedPort
            : current.local.port,
          portMode: localPortMode,
        },
      })
    } catch (error) {
      setLocalError(error instanceof Error ? error.message : t.requestFailed)
      const current = await api.config.get()
      setConfigSnapshot(current)
      applyConfigToState(current, {
        setMode,
        setSaasUrl,
        setSelfHostedUrl,
        setLocalPort,
        setLocalPortMode,
      })
      const runtime = await api.sidecar.getRuntime().catch(() => DEFAULT_RUNTIME)
      setSidecarRuntime(runtime)
    } finally {
      setSaving(false)
    }
  }, [api, ct.manualPort, locale, localPort, localPortMode, mode, saasUrl, selfHostedUrl, t.requestFailed])

  const handleTest = useCallback(async () => {
    setTestResult(null)

    let url: string
    if (mode === 'local') {
      const config = await api?.config.get()
      const port = sidecarRuntime.port
        ?? config?.local.port
        ?? (Number.parseInt(localPort, 10) || 19001)
      url = `http://127.0.0.1:${port}`
    } else if (mode === 'saas') {
      url = saasUrl
    } else {
      url = selfHostedUrl
    }

    try {
      const resp = await fetch(`${url}/healthz`, { signal: AbortSignal.timeout(5000) })
      setTestResult(resp.ok ? 'connected' : 'failed')
    } catch {
      setTestResult('failed')
    }
  }, [api, localPort, mode, saasUrl, selfHostedUrl, sidecarRuntime.port])

  const handleRestart = useCallback(async () => {
    if (!api) return
    setLocalError('')
    try {
      await api.sidecar.restart()
    } catch (error) {
      setLocalError(error instanceof Error ? error.message : t.requestFailed)
      const runtime = await api.sidecar.getRuntime().catch(() => DEFAULT_RUNTIME)
      setSidecarRuntime(runtime)
    }
  }, [api, t.requestFailed])

  const handleRestoreAuto = useCallback(async () => {
    if (!api) return
    setLocalPortMode('auto')
    setLocalError('')
    setSaving(true)
    try {
      const current = await api.config.get()
      await api.config.set({
        ...current,
        local: {
          ...current.local,
          portMode: 'auto',
        },
      })
    } catch (error) {
      setLocalError(error instanceof Error ? error.message : t.requestFailed)
      const current = await api.config.get()
      setConfigSnapshot(current)
      applyConfigToState(current, {
        setMode,
        setSaasUrl,
        setSelfHostedUrl,
        setLocalPort,
        setLocalPortMode,
      })
      const runtime = await api.sidecar.getRuntime().catch(() => DEFAULT_RUNTIME)
      setSidecarRuntime(runtime)
    } finally {
      setSaving(false)
    }
  }, [api, t.requestFailed])

  if (!api) return null

  const effectivePort = sidecarRuntime.port
    ?? configSnapshot?.local.port
    ?? (Number.parseInt(localPort, 10) || 19001)
  const currentPortMode = configSnapshot?.local.portMode ?? sidecarRuntime.portMode
  const runtimeError = localError || sidecarRuntime.lastError

  return (
    <div className="flex flex-col gap-6">

      {mode === 'local' && (
        <div className="flex flex-col gap-3">
          <div
            className="rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="flex items-center justify-between">
              <span className="text-sm text-[var(--c-text-secondary)]">{ct.status}</span>
              <div className="flex items-center gap-2">
                <StatusBadge status={sidecarRuntime.status} t={ct} />
                <button
                  type="button"
                  onClick={() => void handleRestart()}
                  className="flex h-6 items-center gap-1 rounded-md px-2 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
                >
                  <RefreshCw size={12} />
                  <span>{ct.restart}</span>
                </button>
              </div>
            </div>
          </div>

          <FieldRow label={ct.currentPort} value={String(effectivePort)} />
          <FieldRow label={ct.portMode} value={currentPortMode === 'auto' ? ct.portModeAuto : ct.portModeManual} />

          <button
            type="button"
            onClick={() => setShowAdvanced((value) => !value)}
            className="flex items-center gap-2 rounded-xl bg-[var(--c-bg-menu)] px-4 py-3 text-sm text-[var(--c-text-secondary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {showAdvanced ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            <span>{ct.advanced}</span>
          </button>

          {showAdvanced && (
            <div
              className="flex flex-col gap-4 rounded-xl bg-[var(--c-bg-menu)] px-4 py-4"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <div className="text-sm text-[var(--c-text-secondary)]">{ct.advancedDesc}</div>

              <div className="flex flex-col gap-2">
                <label className="text-sm text-[var(--c-text-secondary)]">{ct.portMode}</label>
                <SettingsSelect
                  value={localPortMode}
                  onChange={(value) => setLocalPortMode(value as LocalPortMode)}
                  options={[
                    { value: 'auto', label: ct.portModeAuto },
                    { value: 'manual', label: ct.portModeManual },
                  ]}
                />
              </div>

              {localPortMode === 'manual' && (
                <div className="flex flex-col gap-2">
                  <label className="text-sm text-[var(--c-text-secondary)]">{ct.manualPort}</label>
                  <SettingsInput
                    variant="md"
                    type="number"
                    min={1}
                    max={65535}
                    value={localPort}
                    onChange={(event) => setLocalPort(event.target.value)}
                  />
                  <div className="text-xs text-[var(--c-text-muted)]">{ct.manualPortHint}</div>
                </div>
              )}

              {(runtimeError || localPortMode === 'manual') && (
                <SettingsButton
                  onClick={() => void handleRestoreAuto()}
                  disabled={saving}
                  icon={<RotateCcw size={13} />}
                >
                  {ct.restoreAutoPort}
                </SettingsButton>
              )}
            </div>
          )}

          {runtimeError && (
            <ErrorCallout
              error={{ message: runtimeError }}
              locale={locale}
              requestFailedText={runtimeError}
            />
          )}
        </div>
      )}

      {mode === 'saas' && (
        <div className="flex flex-col gap-2">
          <label className="text-sm text-[var(--c-text-secondary)]">{ct.baseUrl}</label>
          <input
            type="url"
            value={saasUrl}
            onChange={(event) => setSaasUrl(event.target.value)}
            className="h-9 rounded-lg px-3 text-sm text-[var(--c-text-primary)] outline-none"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          />
        </div>
      )}

      {mode === 'self-hosted' && (
        <div className="flex flex-col gap-2">
          <label className="text-sm text-[var(--c-text-secondary)]">{ct.baseUrl}</label>
          <SettingsInput
            variant="md"
            type="url"
            value={selfHostedUrl}
            onChange={(event) => setSelfHostedUrl(event.target.value)}
            placeholder="https://your-server.com"
          />
        </div>
      )}

      <div className="flex items-center gap-2">
        <SettingsButton
          variant="primary"
          onClick={() => void handleSave()}
          disabled={saving}
        >
          {ct.save}
        </SettingsButton>
        <SettingsButton
          onClick={() => void handleTest()}
          icon={<Wifi size={13} />}
        >
          {ct.testConnection}
        </SettingsButton>
        {testResult === 'connected' && (
          <span className="flex items-center gap-1 text-xs" style={{ color: '#22c55e' }}>
            <CheckCircle size={13} /> {ct.connected}
          </span>
        )}
        {testResult === 'failed' && (
          <span className="flex items-center gap-1 text-xs" style={{ color: '#ef4444' }}>
            <XCircle size={13} /> {ct.failed}
          </span>
        )}
      </div>
    </div>
  )
}
