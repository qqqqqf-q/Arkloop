import { useState, useEffect, useCallback } from 'react'
import { XCircle, Search } from 'lucide-react'
import { Modal } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { MemoryConfig, NowledgeDesktopConfig } from '@arkloop/shared/desktop'
import { SettingsButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'
import { SettingsLabel } from './_SettingsLabel'
import type { LocaleStrings } from '../../locales'

// ---------------------------------------------------------------------------
// NowledgeDetectButton — probe 本地 nmem 实例
// ---------------------------------------------------------------------------

const NOWLEDGE_LOCAL_URL = 'http://127.0.0.1:14242'

function NowledgeDetectButton({
  onDetected,
  ds,
}: {
  onDetected: (url: string) => void
  ds: LocaleStrings['desktopSettings']
}) {
  const [state, setState] = useState<'idle' | 'detecting' | 'found' | 'notfound'>('idle')

  const detect = useCallback(async () => {
    setState('detecting')
    try {
      const res = await fetch(`${NOWLEDGE_LOCAL_URL}/health`, { signal: AbortSignal.timeout(3000) })
      if (res.ok) {
        onDetected(NOWLEDGE_LOCAL_URL)
        setState('found')
        setTimeout(() => setState('idle'), 2500)
        return
      }
    } catch { /* unreachable */ }
    setState('notfound')
    setTimeout(() => setState('idle'), 2500)
  }, [onDetected])

  return (
    <button
      type="button"
      disabled={state === 'detecting'}
      onClick={() => void detect()}
      className="inline-flex items-center gap-1 text-xs disabled:opacity-50"
      style={{ color: state === 'found' ? '#22c55e' : state === 'notfound' ? '#ef4444' : 'var(--c-accent)', background: 'none', border: 'none', padding: 0, cursor: state === 'detecting' ? 'default' : 'pointer' }}
    >
      {state === 'detecting' ? <SpinnerIcon /> : <Search size={11} />}
      {state === 'found' ? ds.memoryNowledgeDetected : state === 'notfound' ? ds.memoryNowledgeNotFound : ds.memoryNowledgeDetect}
    </button>
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

export function MemoryConfigModal({ open, onClose, memConfig, onConfigSaved }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings

  const [nowledgeDraft, setNowledgeDraft] = useState<NowledgeDesktopConfig>(memConfig?.nowledge ?? {})
  const [configuring, setConfiguring] = useState(false)
  const [configureResult, setConfigureResult] = useState<'ok' | 'error' | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)

  const api = getDesktopApi()

  // Initialize draft from memConfig when modal opens
  useEffect(() => {
    if (open) {
      const nd = memConfig?.nowledge ?? {}
      setNowledgeDraft(nd)
      setConfigureResult(null)
      setSaveError(null)
      // 打开弹窗且尚未填 baseUrl 时，自动检测本地实例
      if (!nd.baseUrl) {
        void fetch(`${NOWLEDGE_LOCAL_URL}/health`, { signal: AbortSignal.timeout(3000) })
          .then((res) => {
            if (res.ok) setNowledgeDraft((prev) => ({ ...prev, baseUrl: NOWLEDGE_LOCAL_URL, apiKey: prev.apiKey ?? '' }))
          })
          .catch(() => { /* 未检测到，静默 */ })
      }
    }
  }, [open, memConfig])

  const handleSaveNowledge = useCallback(async () => {
    if (!api?.memory || !memConfig) return
    setConfiguring(true)
    setConfigureResult(null)
    setSaveError(null)
    try {
      if (!(nowledgeDraft.baseUrl ?? '').trim()) {
        throw new Error(ds.memoryNowledgeMissingBaseUrl)
      }
      const updatedConfig: MemoryConfig = {
        ...memConfig,
        provider: 'nowledge',
        nowledge: {
          ...nowledgeDraft,
          baseUrl: nowledgeDraft.baseUrl?.trim(),
          apiKey: nowledgeDraft.apiKey?.trim(),
          requestTimeoutMs: nowledgeDraft.requestTimeoutMs && nowledgeDraft.requestTimeoutMs > 0
            ? nowledgeDraft.requestTimeoutMs
            : 30000,
        },
      }
      await api.memory.setConfig(updatedConfig)
      onConfigSaved(updatedConfig)
      setConfigureResult('ok')
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : ds.memoryConfigureError)
      setConfigureResult('error')
    } finally {
      setConfiguring(false)
    }
  }, [api, ds.memoryConfigureError, ds.memoryNowledgeMissingBaseUrl, memConfig, nowledgeDraft, onConfigSaved])

  return (
    <Modal open={open} onClose={onClose} title={ds.memoryConfigureModalTitle} width="520px">
      <div className="flex flex-col gap-4">
        {saveError && (
          <div
            className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm"
            style={{ background: 'rgba(239,68,68,0.08)', color: '#ef4444' }}
          >
            <XCircle size={14} />{saveError}
          </div>
        )}

        <div className="flex flex-col gap-4">
          {/* Base URL */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between">
              <SettingsLabel htmlFor="nowledge-base-url">{ds.memoryNowledgeBaseUrl}</SettingsLabel>
              <NowledgeDetectButton
                onDetected={(url) => setNowledgeDraft((prev) => ({ ...prev, baseUrl: url, apiKey: '' }))}
                ds={ds}
              />
            </div>
            <SettingsInput
              id="nowledge-base-url"
              value={nowledgeDraft.baseUrl ?? ''}
              onChange={(e) => setNowledgeDraft((prev) => ({ ...prev, baseUrl: e.target.value }))}
              placeholder="http://127.0.0.1:14242"
              variant="md"
            />
          </div>

          {/* API Key */}
          <div className="flex flex-col gap-1.5">
            <SettingsLabel htmlFor="nowledge-api-key">
              {ds.memoryNowledgeApiKey}
              <span className="ml-1 font-normal text-[var(--c-text-muted)]">{ds.memoryNowledgeApiKeyHint}</span>
            </SettingsLabel>
            <SettingsInput
              id="nowledge-api-key"
              value={nowledgeDraft.apiKey ?? ''}
              onChange={(e) => setNowledgeDraft((prev) => ({ ...prev, apiKey: e.target.value }))}
              type="password"
              placeholder="nmem_..."
              variant="md"
            />
          </div>

          {/* Timeout */}
          <div className="flex flex-col gap-1.5">
            <SettingsLabel htmlFor="nowledge-timeout">{ds.memoryNowledgeTimeoutMs}</SettingsLabel>
            <SettingsInput
              id="nowledge-timeout"
              value={nowledgeDraft.requestTimeoutMs ?? 30000}
              onChange={(e) => setNowledgeDraft((prev) => ({ ...prev, requestTimeoutMs: Number(e.target.value) || undefined }))}
              type="number"
              variant="md"
            />
          </div>

          <div className="flex justify-end">
            <SettingsButton
              variant="primary"
              onClick={() => void handleSaveNowledge()}
              disabled={configuring}
              icon={configuring ? <SpinnerIcon /> : undefined}
            >
              {configureResult === 'ok' ? ds.memoryNowledgeSaved : ds.memoryConfigureButton}
            </SettingsButton>
          </div>
        </div>
      </div>
    </Modal>
  )
}
