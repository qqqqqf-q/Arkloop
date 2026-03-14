import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useToast } from '../useToast'
import type { PromptInjectionTexts } from './types'
import { SETTING_KEYS } from './types'

export interface SemanticSetupPanelProps {
  accessToken: string
  bridgeAvailable: boolean
  onSaved: () => void
  texts: PromptInjectionTexts
  setSetting: (key: string, value: string, token: string) => Promise<unknown>
  bridgeInstall: (variant: string) => Promise<{ operation_id: string }>
  formatError: (err: unknown) => string
}

export function SemanticSetupPanel({
  accessToken,
  bridgeAvailable,
  onSaved,
  texts,
  setSetting,
  bridgeInstall,
  formatError,
}: SemanticSetupPanelProps) {
  const { addToast } = useToast()

  const [mode, setMode] = useState<'local' | 'api'>('api')
  const [variant, setVariant] = useState<'22m' | '86m'>('22m')
  const [endpoint, setEndpoint] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [saving, setSaving] = useState(false)
  const [installError, setInstallError] = useState('')

  const handleSaveApi = async () => {
    if (!endpoint.trim()) return
    setSaving(true)
    try {
      await setSetting(SETTING_KEYS.SEMANTIC_PROVIDER, 'api', accessToken)
      await setSetting(SETTING_KEYS.SEMANTIC_API_ENDPOINT, endpoint.trim(), accessToken)
      if (apiKey.trim()) {
        await setSetting(SETTING_KEYS.SEMANTIC_API_KEY, apiKey.trim(), accessToken)
      }
      addToast(texts.toastUpdated, 'success')
      onSaved()
    } catch (err) {
      addToast(formatError(err), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleInstallLocal = async () => {
    setSaving(true)
    setInstallError('')
    try {
      await setSetting(SETTING_KEYS.SEMANTIC_PROVIDER, 'local', accessToken)
      const { operation_id } = await bridgeInstall(variant)
      addToast(`${texts.semanticInstallStarted} (${operation_id.slice(0, 8)})`, 'success')
      onSaved()
    } catch (err) {
      const msg = formatError(err)
      setInstallError(msg)
      addToast(msg, 'error')
    } finally {
      setSaving(false)
    }
  }

  const modeBtn = (value: 'local' | 'api', label: string) => (
    <button
      onClick={() => { setMode(value); setInstallError('') }}
      className={[
        'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
        mode === value
          ? 'bg-[var(--c-text-primary)] text-[var(--c-bg-card)]'
          : 'bg-[var(--c-bg-tag)] text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]',
      ].join(' ')}
    >
      {label}
    </button>
  )

  return (
    <div className="mt-2 rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-deep2)] p-4">
      <div className="mb-4 flex gap-2">
        {modeBtn('local', texts.semanticProviderLocal)}
        {modeBtn('api', texts.semanticProviderApi)}
      </div>

      {mode === 'local' && (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-[var(--c-text-muted)]">{texts.semanticLocalDesc}</p>
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-medium text-[var(--c-text-secondary)]">{texts.semanticModelVariant}</span>
            <div className="flex gap-2">
              {(['22m', '86m'] as const).map(v => (
                <button
                  key={v}
                  onClick={() => setVariant(v)}
                  className={[
                    'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
                    variant === v
                      ? 'bg-[var(--c-text-primary)] text-[var(--c-bg-card)]'
                      : 'bg-[var(--c-bg-tag)] text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]',
                  ].join(' ')}
                >
                  {v === '22m' ? texts.semanticModel22m : texts.semanticModel86m}
                </button>
              ))}
            </div>
          </div>
          {!bridgeAvailable && (
            <p className="text-xs text-[var(--c-status-warning-text)]">{texts.semanticBridgeRequired}</p>
          )}
          {installError && (
            <p className="text-xs text-[var(--c-status-error-text,red)]">{installError}</p>
          )}
          <button
            disabled={!bridgeAvailable || saving}
            onClick={() => void handleInstallLocal()}
            className={[
              'w-fit rounded-md border px-3 py-1.5 text-xs font-medium transition-colors',
              bridgeAvailable
                ? 'border-[var(--c-status-success-text)] text-[var(--c-status-success-text)] hover:bg-[var(--c-status-success-bg)]'
                : 'border-[var(--c-border-console)] text-[var(--c-text-muted)] opacity-50 cursor-not-allowed',
            ].join(' ')}
          >
            {saving ? <Loader2 size={12} className="inline animate-spin" /> : texts.actionInstallModel}
          </button>
        </div>
      )}

      {mode === 'api' && (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-[var(--c-text-muted)]">{texts.semanticApiDesc}</p>
          <input
            type="url"
            value={endpoint}
            onChange={e => setEndpoint(e.target.value)}
            placeholder={texts.semanticApiEndpointHint}
            className="rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-card)] px-3 py-2 text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none focus:ring-1 focus:ring-[var(--c-text-muted)]"
          />
          <input
            type="password"
            value={apiKey}
            onChange={e => setApiKey(e.target.value)}
            placeholder={texts.semanticApiKeyHint}
            className="rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-card)] px-3 py-2 text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none focus:ring-1 focus:ring-[var(--c-text-muted)]"
          />
          <button
            disabled={saving || !endpoint.trim()}
            onClick={() => void handleSaveApi()}
            className={[
              'w-fit rounded-md border px-3 py-1.5 text-xs font-medium transition-colors',
              endpoint.trim()
                ? 'border-[var(--c-status-success-text)] text-[var(--c-status-success-text)] hover:bg-[var(--c-status-success-bg)]'
                : 'border-[var(--c-border-console)] text-[var(--c-text-muted)] opacity-50 cursor-not-allowed',
            ].join(' ')}
          >
            {saving ? <Loader2 size={12} className="inline animate-spin" /> : texts.actionSave}
          </button>
        </div>
      )}
    </div>
  )
}
