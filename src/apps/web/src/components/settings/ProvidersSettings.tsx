import { useState, useCallback, useEffect, useMemo, useRef, memo } from 'react'
import { createPortal } from 'react-dom'
import {
  Plus,
  Trash2,
  Download,
  X,
  Loader2,
  ChevronDown,
  Check,
  Zap,
  SlidersHorizontal,
} from 'lucide-react'
import {
  type LlmProvider,
  type LlmProviderModel,
  type AvailableModel,
  listLlmProviders,
  createLlmProvider,
  updateLlmProvider,
  deleteLlmProvider,
  createProviderModel,
  deleteProviderModel,
  patchProviderModel,
  listAvailableModels,
  testLlmProviderModel,
  isApiError,
} from '../../api'
import { routeAdvancedJsonFromAvailableCatalog } from '@arkloop/shared/llm/available-catalog-advanced-json'
import { ConfirmDialog, PillToggle } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { ModelOptionsModal } from '../ModelOptionsModal'
import { AnimatedCheck } from '../AnimatedCheck'
import { secondaryButtonBorderStyle } from '../buttonStyles'

const VENDOR_PRESETS = [
  { key: 'openai_responses', provider: 'openai', openai_api_mode: 'responses' },
  { key: 'openai_chat_completions', provider: 'openai', openai_api_mode: 'chat_completions' },
  { key: 'anthropic_message', provider: 'anthropic', openai_api_mode: undefined },
  { key: 'gemini', provider: 'gemini', openai_api_mode: undefined },
] as const

type VendorPresetKey = (typeof VENDOR_PRESETS)[number]['key']

const OPENVIKING_BACKEND_ADVANCED_KEY = 'openviking_backend'

type OpenVikingBackendKey = 'openai' | 'azure' | 'volcengine' | 'openai_compatible'

function vendorLabel(
  key: string,
  p: { vendorOpenai: string; vendorOpenaiChat: string; vendorAnthropic: string; vendorGemini: string },
): string {
  const map: Record<string, string> = {
    openai_responses: p.vendorOpenai,
    openai_chat_completions: p.vendorOpenaiChat,
    anthropic_message: p.vendorAnthropic,
    gemini: p.vendorGemini,
  }
  return map[key] ?? key
}

function toVendorKey(provider: string, mode: string | null): VendorPresetKey {
  if (provider === 'anthropic') return 'anthropic_message'
  if (provider === 'gemini') return 'gemini'
  if (mode === 'chat_completions') return 'openai_chat_completions'
  return 'openai_responses'
}

function defaultOpenVikingBackendForVendor(provider: string): OpenVikingBackendKey {
  if (provider === 'anthropic' || provider === 'gemini') return 'openai_compatible'
  return 'openai'
}

function readOpenVikingBackend(provider: LlmProvider): OpenVikingBackendKey {
  const raw = provider.advanced_json?.[OPENVIKING_BACKEND_ADVANCED_KEY]
  if (raw === 'openai' || raw === 'azure' || raw === 'volcengine' || raw === 'openai_compatible') {
    return raw
  }
  if (raw === 'litellm') {
    return 'openai_compatible'
  }
  return defaultOpenVikingBackendForVendor(provider.provider)
}

function mergeProviderAdvancedJSON(
  current: Record<string, unknown> | null | undefined,
  backend: OpenVikingBackendKey,
): Record<string, unknown> {
  const next = { ...(current ?? {}) }
  next[OPENVIKING_BACKEND_ADVANCED_KEY] = backend
  return next
}

import { settingsInputCls } from './_SettingsInput'

const INPUT_CLS = settingsInputCls('sm')

function VendorDropdown({
  value,
  onChange,
  p,
}: {
  value: VendorPresetKey
  onChange: (v: VendorPresetKey) => void
  p: ReturnType<typeof useLocale>['t']['adminProviders']
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node) || btnRef.current?.contains(e.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="relative">
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen(v => !v)}
        className="flex w-full items-center justify-between rounded-lg bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-deep)]"
        style={{ border: '1px solid var(--c-border-subtle)' }}
      >
        <span className="truncate">{vendorLabel(value, p)}</span>
        <ChevronDown size={13} className="ml-2 shrink-0 text-[var(--c-text-muted)]" />
      </button>
      {open && (
        <div
          ref={menuRef}
          className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50 min-w-full"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          {VENDOR_PRESETS.map((v) => (
            <button
              key={v.key}
              type="button"
              onClick={() => { onChange(v.key); setOpen(false) }}
              className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ color: value === v.key ? 'var(--c-text-heading)' : 'var(--c-text-secondary)', fontWeight: value === v.key ? 500 : 400 }}
            >
              <span>{vendorLabel(v.key, p)}</span>
              {value === v.key && <Check size={13} className="shrink-0" />}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

type Props = { accessToken: string }

export function ProvidersSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const p = t.adminProviders

  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showAddProvider, setShowAddProvider] = useState(false)

  const firstLoadRef = useRef(true)

  const load = useCallback(async () => {
    try {
      const list = await listLlmProviders(accessToken)
      setProviders(list)
      if (firstLoadRef.current && list.length > 0) {
        setSelectedId(list[0].id)
        firstLoadRef.current = false
      } else {
        setSelectedId((prev) => list.find((pv) => pv.id === prev) ? prev : (list[0]?.id ?? null))
      }
    } catch {
      setError(p.loadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, p.loadFailed])

  useEffect(() => { void load() }, [load])

  const selected = providers.find((pv) => pv.id === selectedId) ?? null

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  return (
    <div className="-m-6 flex min-h-0 min-w-0 overflow-hidden" style={{ height: 'calc(100% + 48px)' }}>
      {/* Provider list */}
      <div className="flex w-[220px] shrink-0 flex-col overflow-hidden border-r border-[var(--c-border-subtle)] max-[1230px]:w-[180px] xl:w-[240px]">
        <div className="flex-1 overflow-y-auto px-2 py-1">
          <div className="flex flex-col gap-[3px]">
            {providers.map((pv) => (
              <button
                key={pv.id}
                onClick={() => setSelectedId(pv.id)}
                className={[
                  'flex h-[38px] items-center truncate rounded-lg px-2.5 text-left text-[14px] font-medium transition-all duration-[120ms] active:scale-[0.96]',
                  selectedId === pv.id
                    ? 'rounded-[10px] bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                    : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
                ].join(' ')}
              >
                {pv.name}
              </button>
            ))}
          </div>
        </div>
        <div className="border-t border-[var(--c-border-subtle)] px-3 py-3">
          <button
            onClick={() => setShowAddProvider(true)}
            className="flex h-10 w-full items-center justify-center gap-1.5 rounded-lg text-[13px] font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <Plus size={14} />
            {p.addProvider}
          </button>
        </div>
        {error && <p className="px-2 pb-2 text-xs text-[var(--c-status-error-text)]">{error}</p>}
      </div>

      {/* Detail */}
      <div className="min-w-0 flex-1 overflow-y-auto p-4 max-[1230px]:p-3 sm:p-5">
        {selected ? (
          <ProviderDetail
            provider={selected}
            accessToken={accessToken}
          onUpdated={load}
          onDeleted={load}
          p={p}
        />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3">
            <p className="text-sm text-[var(--c-text-muted)]">{p.noProviders}</p>
            <button
              onClick={() => setShowAddProvider(true)}
              className="flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)]"
              style={{ background: 'var(--c-btn-bg)' }}
            >
              <Plus size={14} />
              {p.addProvider}
            </button>
          </div>
        )}
      </div>

      {showAddProvider && (
        <AddProviderModal
          accessToken={accessToken}
          p={p}
          onClose={() => setShowAddProvider(false)}
          onCreated={() => { setShowAddProvider(false); void load() }}
        />
      )}
    </div>
  )
}

// -- Add Provider Modal --

function AddProviderModal({ accessToken, p, onClose, onCreated }: {
  accessToken: string
  p: ReturnType<typeof useLocale>['t']['adminProviders']
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [preset, setPreset] = useState<VendorPresetKey>('openai_responses')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const handleSave = async () => {
    if (!name.trim() || !apiKey.trim()) return
    setSaving(true)
    setErr('')
    try {
      const v = VENDOR_PRESETS.find((vv) => vv.key === preset)!
      await createLlmProvider(accessToken, {
        name: name.trim(),
        provider: v.provider,
        api_key: apiKey.trim(),
        base_url: baseUrl.trim() || undefined,
        openai_api_mode: v.openai_api_mode,
        advanced_json: mergeProviderAdvancedJSON({}, defaultOpenVikingBackendForVendor(v.provider)),
      })
      onCreated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const fieldLabelCls = 'block text-[11px] font-medium text-[var(--c-placeholder)] mb-1 pl-[2px]'
  const fieldInputCls = 'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)] focus:border-[var(--c-border)]'

  return createPortal(
    <div
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'var(--c-overlay)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex w-[460px] flex-col gap-5 rounded-[14px] p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-[15px] font-semibold text-[var(--c-text-heading)]">{p.addProvider}</h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <X size={14} />
          </button>
        </div>

        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <div>
            <label className={fieldLabelCls}>{p.providerName}</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My Provider"
              className={fieldInputCls}
            />
          </div>
          <div>
            <label className={fieldLabelCls}>{p.vendor}</label>
            <VendorDropdown value={preset} onChange={setPreset} p={p} />
          </div>
          <div className="col-span-2">
            <label className={fieldLabelCls}>{p.apiKey}</label>
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={p.apiKeyPlaceholder}
              className={fieldInputCls}
            />
          </div>
          <div className="col-span-2">
            <label className={fieldLabelCls}>{p.baseUrl}</label>
            <input
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value.slice(0, 500))}
              placeholder={p.baseUrlPlaceholder ?? 'https://api.example.com/v1'}
              className={fieldInputCls}
              maxLength={500}
            />
            {baseUrl.trim() && !baseUrl.trim().startsWith('https://') && !baseUrl.trim().startsWith('http://') && (
              <span className="mt-1 block text-xs text-[var(--c-text-muted)]">需以 https:// 开头</span>
            )}
          </div>
        </div>

        {err && <p className="mt-3 text-xs text-[var(--c-status-error-text)]">{err}</p>}

        <div className="flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded-lg px-4 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {p.cancel}
          </button>
          <button
            onClick={() => void handleSave()}
            disabled={saving || !name.trim() || !apiKey.trim()}
            className="flex items-center justify-center rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)] disabled:opacity-50"
            style={{ background: 'var(--c-btn-bg)' }}
          >
            <span className="relative flex items-center justify-center">
              <span className={`flex items-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-0' : 'opacity-100'}`}>{p.save}</span>
              <span className={`absolute inset-0 flex items-center justify-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-100' : 'opacity-0'}`}>
                <Loader2 size={14} className="animate-spin" />
                {p.saving}
              </span>
            </span>
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

// -- Provider Detail --

function ProviderDetail({ provider, accessToken, onUpdated, onDeleted, p }: {
  provider: LlmProvider
  accessToken: string
  onUpdated: () => void
  onDeleted: () => void
  p: ReturnType<typeof useLocale>['t']['adminProviders']
}) {
  const [formPreset, setFormPreset] = useState<VendorPresetKey>(toVendorKey(provider.provider, provider.openai_api_mode))
  const [formName, setFormName] = useState(provider.name)
  const [formApiKey, setFormApiKey] = useState('')
  const [formBaseUrl, setFormBaseUrl] = useState(provider.base_url ?? '')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    setFormPreset(toVendorKey(provider.provider, provider.openai_api_mode))
    setFormName(provider.name)
    setFormApiKey('')
    setFormBaseUrl(provider.base_url ?? '')
    setErr('')
    setConfirmDelete(false)
  }, [provider.id])

  const handleSave = async () => {
    setSaving(true)
    setErr('')
    try {
      const selected = VENDOR_PRESETS.find((v) => v.key === formPreset)
      await updateLlmProvider(accessToken, provider.id, {
        name: formName.trim() || undefined,
        api_key: formApiKey.trim() || undefined,
        base_url: formBaseUrl.trim() || null,
        provider: selected?.provider,
        openai_api_mode: selected?.openai_api_mode ?? null,
        advanced_json: mergeProviderAdvancedJSON(provider.advanced_json, readOpenVikingBackend(provider)),
      })
      setFormApiKey('')
      onUpdated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    setDeleting(true)
    try {
      await deleteLlmProvider(accessToken, provider.id)
      onDeleted()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  return (
    <div className="mx-auto min-w-0 max-w-2xl space-y-6">
      <h3 className="text-base font-semibold text-[var(--c-text-primary)]">{provider.name}</h3>

      <div className="space-y-4">
        <LabelField label={p.vendor}>
          <VendorDropdown value={formPreset} onChange={setFormPreset} p={p} />
        </LabelField>
        <LabelField label={p.providerName}>
          <input value={formName} onChange={(e) => setFormName(e.target.value)} className={INPUT_CLS} />
        </LabelField>
        <LabelField label={p.apiKey}>
          <input type="password" value={formApiKey} onChange={(e) => setFormApiKey(e.target.value)} placeholder={provider.key_prefix ? `${provider.key_prefix}${'*'.repeat(40)}` : p.apiKeyPlaceholder} className={INPUT_CLS} />
          {provider.key_prefix && <p className="mt-1 text-xs text-[var(--c-text-muted)]">{provider.key_prefix}{'*'.repeat(8)}</p>}
        </LabelField>
        <LabelField label={p.baseUrl}>
          <input
            value={formBaseUrl}
            onChange={(e) => setFormBaseUrl(e.target.value.slice(0, 500))}
            placeholder={p.baseUrlPlaceholder ?? 'https://api.example.com/v1'}
            className={INPUT_CLS}
            maxLength={500}
          />
          {formBaseUrl.trim() && !formBaseUrl.trim().startsWith('https://') && !formBaseUrl.trim().startsWith('http://') && (
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">需以 https:// 开头</p>
          )}
        </LabelField>
      </div>

      {err && <p className="text-xs text-[var(--c-status-error-text)]">{err}</p>}

      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--c-border-subtle)] pb-4">
        {confirmDelete ? (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-[var(--c-text-tertiary)]">{p.deleteProviderConfirm}</span>
            <button onClick={() => void handleDelete()} disabled={deleting} className="rounded-lg bg-red-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50">{p.deleteProvider}</button>
            <button onClick={() => setConfirmDelete(false)} className="rounded-lg px-3 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">{p.cancel}</button>
          </div>
        ) : (
          <button onClick={() => setConfirmDelete(true)} className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors duration-150 hover:border-red-500/30 hover:text-red-500">
            <Trash2 size={12} />
          </button>
        )}
        <button onClick={() => void handleSave()} disabled={saving || !formName.trim()} className="flex items-center justify-center rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)] disabled:opacity-50" style={{ background: 'var(--c-btn-bg)' }}>
          <span className="relative flex items-center justify-center">
            <span className={`flex items-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-0' : 'opacity-100'}`}>{p.save}</span>
            <span className={`absolute inset-0 flex items-center justify-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-100' : 'opacity-0'}`}>
              <Loader2 size={14} className="animate-spin" />
              {p.saving}
            </span>
          </span>
        </button>
      </div>

      <ModelsSection provider={provider} accessToken={accessToken} onChanged={onUpdated} p={p} />
    </div>
  )
}

// -- Models Section (same pattern as ModelConfigContent) --

function ModelsSection({ provider, accessToken, onChanged, p }: {
  provider: LlmProvider
  accessToken: string
  onChanged: () => void
  p: ReturnType<typeof useLocale>['t']['adminProviders']
}) {
  const { t } = useLocale()
  const [available, setAvailable] = useState<AvailableModel[] | null>(null)
  const [loadingAvailable, setLoadingAvailable] = useState(false)
  const [availableError, setAvailableError] = useState('')
  const [importing, setImporting] = useState(false)
  const [deletingAll, setDeletingAll] = useState(false)
  const [creatingModel, setCreatingModel] = useState(false)
  const [err, setErr] = useState('')
  const [search, setSearch] = useState('')
  const [editingModel, setEditingModel] = useState<LlmProviderModel | null>(null)
  const [hasLoadedAvailable, setHasLoadedAvailable] = useState(false)
  const [showDeleteAllConfirm, setShowDeleteAllConfirm] = useState(false)

  useEffect(() => {
    setAvailable(null)
    setHasLoadedAvailable(false)
    setSearch('')
    setEditingModel(null)
    setCreatingModel(false)
    setErr('')
    setAvailableError('')
    setShowDeleteAllConfirm(false)
  }, [provider.id])

  const loadAvailable = useCallback(async () => {
    setLoadingAvailable(true)
    setAvailableError('')
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
      setHasLoadedAvailable(true)
    } catch (e) {
      const message = isApiError(e) ? e.message : t.models.availableFetchFailed
      setAvailableError(message)
    } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, provider.id, t.models.availableFetchFailed])

  const ensureAvailableLoaded = useCallback(async (): Promise<AvailableModel[]> => {
    if (available !== null) return available
    setLoadingAvailable(true)
    setAvailableError('')
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
      setHasLoadedAvailable(true)
      return res.models
    } catch (e) {
      const message = isApiError(e) ? e.message : t.models.availableFetchFailed
      setAvailableError(message)
      throw e
    } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, available, provider.id, t.models.availableFetchFailed])

  const handleImportAll = async () => {
    setImporting(true)
    setErr('')
    try {
      const source = await ensureAvailableLoaded()
      const unconfigured = source.filter((am) => !am.configured)
      const byLowerId = new Map<string, AvailableModel>()
      for (const am of unconfigured) {
        const k = am.id.toLowerCase()
        if (!byLowerId.has(k)) byLowerId.set(k, am)
      }
      const toImport = [...byLowerId.values()]
      const embeddingIds = new Set(toImport.filter((am) => am.type === 'embedding').map((am) => am.id.toLowerCase()))
      const created: LlmProviderModel[] = []
      for (const am of toImport) {
        const isEmb = am.type === 'embedding'
        try {
          const pm = await createProviderModel(accessToken, provider.id, {
            model: am.id,
            show_in_picker: false,
            tags: isEmb ? ['embedding'] : undefined,
            advanced_json: routeAdvancedJsonFromAvailableCatalog(am),
          })
          created.push(pm)
        } catch (e) {
          if (isApiError(e) && e.code === 'llm_provider_models.model_conflict') continue
          throw e
        }
      }
      const toEnable = created.filter((pm) => pm.model.toLowerCase().includes('gpt-4o-mini') && !embeddingIds.has(pm.model.toLowerCase()))
      if (toEnable.length > 0) {
        try {
          await patchProviderModel(accessToken, provider.id, toEnable[0].id, { show_in_picker: true, is_default: true })
          await Promise.all(toEnable.slice(1).map((pm) => patchProviderModel(accessToken, provider.id, pm.id, { show_in_picker: true })))
        } catch { /* default-setting is best-effort */ }
      }
      onChanged()
      await loadAvailable()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    } finally {
      setImporting(false)
    }
  }

  const handleDeleteModel = useCallback(async (modelId: string) => {
    try {
      await deleteProviderModel(accessToken, provider.id, modelId)
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    }
  }, [accessToken, provider.id, onChanged, p.saveFailed])

  const handleDeleteAll = async () => {
    setDeletingAll(true)
    setErr('')
    let failed = 0
    for (const pm of provider.models) {
      try { await deleteProviderModel(accessToken, provider.id, pm.id) } catch { failed++ }
    }
    setDeletingAll(false)
    if (failed > 0) setErr(`${failed} model(s) failed to delete`)
    onChanged()
    setAvailable(null)
    setHasLoadedAvailable(false)
  }

  const handleTogglePicker = useCallback(async (modelId: string, current: boolean) => {
    try {
      await patchProviderModel(accessToken, provider.id, modelId, { show_in_picker: !current })
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    }
  }, [accessToken, provider.id, onChanged, p.saveFailed])

  const handleSaveModelOptions = useCallback(async (payload: {
    advancedJSON: Record<string, unknown> | null
    tags: string[]
  }) => {
    if (!editingModel) return
    try {
      await patchProviderModel(accessToken, provider.id, editingModel.id, {
        advanced_json: payload.advancedJSON,
        tags: payload.tags,
      })
      setEditingModel(null)
      onChanged()
    } catch (e) {
      throw new Error(isApiError(e) ? e.message : p.saveFailed)
    }
  }, [accessToken, editingModel, onChanged, p.saveFailed, provider.id])

  const unconfiguredCount = available?.filter((am) => !am.configured).length ?? 0
  const importDisabled = importing || loadingAvailable || (hasLoadedAvailable && unconfiguredCount === 0)
  const deleteAllDisabled = deletingAll || provider.models.length === 0
  const filteredModels = search.trim()
    ? provider.models.filter((pm) => pm.model.toLowerCase().includes(search.trim().toLowerCase()))
    : provider.models

  const INITIAL_BATCH = 30
  const BATCH_SIZE = 100

  const [visibleCount, setVisibleCount] = useState(INITIAL_BATCH)

  // filteredModels 变化时重置
  useEffect(() => {
    setVisibleCount(INITIAL_BATCH)
  }, [filteredModels.length, search])

  // 逐帧追加
  useEffect(() => {
    if (visibleCount >= filteredModels.length) return
    const id = requestAnimationFrame(() => {
      setVisibleCount((prev) => Math.min(prev + BATCH_SIZE, filteredModels.length))
    })
    return () => cancelAnimationFrame(id)
  }, [visibleCount, filteredModels.length])

  const visibleModels = filteredModels.slice(0, visibleCount)

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{p.modelsSection}</h4>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setShowDeleteAllConfirm(true)}
            disabled={deleteAllDisabled}
            className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-40"
            style={secondaryButtonBorderStyle}
          >
            {deletingAll ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
          </button>
          <button
            type="button"
            onClick={() => void handleImportAll()}
            disabled={importDisabled}
            className="button-secondary inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-3 text-sm font-medium text-[var(--c-text-secondary)] transition-colors disabled:cursor-not-allowed disabled:opacity-40"
            style={secondaryButtonBorderStyle}
          >
            {loadingAvailable || importing ? <Loader2 size={12} className="animate-spin" /> : <Download size={12} />}
            {unconfiguredCount > 0 && !importing && !loadingAvailable && `${p.importAll ?? 'Import all'} (${unconfiguredCount})`}
            {(loadingAvailable || importing) && (p.importing ?? '...')}
          </button>
          <ModelTestButton
            accessToken={accessToken}
            provider={provider}
            label={p.testModel ?? 'Test'}
            searchPlaceholder={p.searchProviders}
          />
          <button onClick={() => setCreatingModel(true)} className="button-primary inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-4 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] disabled:cursor-not-allowed disabled:opacity-40" style={{ background: 'var(--c-btn-bg)' }}>
            {p.addModel}
          </button>
        </div>
      </div>

      {err && <p className="mt-2 text-xs text-[var(--c-status-error-text)]">{err}</p>}
      {availableError && <p className="mt-2 text-xs text-[var(--c-status-error-text)]">{availableError}</p>}
      {hasLoadedAvailable && !loadingAvailable && !availableError && available !== null && available.length === 0 && (
        <p className="mt-2 text-xs text-[var(--c-text-muted)]">{t.models.noModelsAvailable}</p>
      )}

      {provider.models.length > 0 && (
        <div className="mt-3">
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={p.searchProviders} className={INPUT_CLS + ' w-full'} />
        </div>
      )}

      <div className="mt-2 space-y-1 overflow-y-auto" style={{ maxHeight: '320px' }}>
        {provider.models.length === 0 ? (
          <p className="py-8 text-center text-sm text-[var(--c-text-muted)]">--</p>
        ) : filteredModels.length === 0 ? (
          <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">--</p>
        ) : (
          visibleModels.map((pm) => (
            <ModelRow
              key={pm.id}
              pm={pm}
              onToggle={handleTogglePicker}
              onEdit={setEditingModel}
              onDelete={handleDeleteModel}
            />
          ))
        )}
      </div>

      {editingModel !== null && (
      <ModelOptionsModal
        open
        model={editingModel}
        availableModels={available}
        labels={{
          modelOptionsTitle: p.modelOptionsTitle ?? 'Model Options',
          modelOptionsFor: p.modelOptionsFor ?? 'Configure options for',
          modelCapabilities: p.modelCapabilities ?? 'Model Capabilities',
          modelType: p.modelType ?? 'Model Type',
          modelTypeChat: p.modelTypeChat ?? 'Chat',
          modelTypeEmbedding: p.modelTypeEmbedding ?? 'Embedding',
          modelTypeImage: p.modelTypeImage ?? 'Image',
          modelTypeAudio: p.modelTypeAudio ?? 'Audio',
          modelTypeModeration: p.modelTypeModeration ?? 'Moderation',
          modelTypeOther: p.modelTypeOther ?? 'Other',
          toolCalling: p.toolCalling ?? 'Tool Calling',
          reasoning: p.reasoning ?? 'Reasoning',
          defaultTemperature: p.defaultTemperature ?? 'Default Temperature',
          vision: p.vision ?? 'Vision',
          imageOutput: p.imageOutput ?? 'Image Output',
          embedding: p.embedding ?? 'Embedding',
          contextWindow: p.contextWindow ?? 'Context Window',
          maxOutputTokens: p.maxOutputTokens ?? 'Max Output Tokens',
          providerOptionsJson: p.providerOptionsJson ?? 'Provider Options (JSON)',
          providerOptionsHint: p.providerOptionsHint ?? 'Only provider-specific fields belong here. Model capability fields are managed above.',
          save: p.save,
          cancel: p.cancel,
          reset: p.reset ?? 'Reset',
          invalidJson: p.invalidJson ?? 'Provider options must be a JSON object',
          invalidNumber: p.invalidNumber ?? 'Context window, max output tokens, and temperature must be valid numbers',
          visionBridgeHint: t.models.visionBridgeHint,
          addModelTitle: t.models.addModelTitle ?? 'Add Model',
          modelNameLabel: t.models.modelName ?? 'Model name',
          modelNamePlaceholder: t.models.modelNamePlaceholder ?? 'e.g. gpt-4o',
        }}
        onClose={() => setEditingModel(null)}
        onSave={handleSaveModelOptions}
      />
      )}

      {creatingModel && (
      <ModelOptionsModal
        open
        mode="create"
        model={null}
        availableModels={available}
        labels={{
          modelOptionsTitle: p.modelOptionsTitle ?? 'Model Options',
          modelOptionsFor: p.modelOptionsFor ?? 'Configure options for',
          modelCapabilities: p.modelCapabilities ?? 'Model Capabilities',
          modelType: p.modelType ?? 'Model Type',
          modelTypeChat: p.modelTypeChat ?? 'Chat',
          modelTypeEmbedding: p.modelTypeEmbedding ?? 'Embedding',
          modelTypeImage: p.modelTypeImage ?? 'Image',
          modelTypeAudio: p.modelTypeAudio ?? 'Audio',
          modelTypeModeration: p.modelTypeModeration ?? 'Moderation',
          modelTypeOther: p.modelTypeOther ?? 'Other',
          toolCalling: p.toolCalling ?? 'Tool Calling',
          reasoning: p.reasoning ?? 'Reasoning',
          defaultTemperature: p.defaultTemperature ?? 'Default Temperature',
          vision: p.vision ?? 'Vision',
          imageOutput: p.imageOutput ?? 'Image Output',
          embedding: p.embedding ?? 'Embedding',
          contextWindow: p.contextWindow ?? 'Context Window',
          maxOutputTokens: p.maxOutputTokens ?? 'Max Output Tokens',
          providerOptionsJson: p.providerOptionsJson ?? 'Provider Options (JSON)',
          providerOptionsHint: p.providerOptionsHint ?? 'Only provider-specific fields belong here. Model capability fields are managed above.',
          save: p.save,
          cancel: p.cancel,
          reset: p.reset ?? 'Reset',
          invalidJson: p.invalidJson ?? 'Provider options must be a JSON object',
          invalidNumber: p.invalidNumber ?? 'Context window, max output tokens, and temperature must be valid numbers',
          visionBridgeHint: t.models.visionBridgeHint,
          addModelTitle: t.models.addModelTitle ?? 'Add Model',
          modelNameLabel: t.models.modelName ?? 'Model name',
          modelNamePlaceholder: t.models.modelNamePlaceholder ?? 'e.g. gpt-4o',
        }}
        onClose={() => setCreatingModel(false)}
        onSave={async () => {}}
        onCreate={async (payload) => {
          try {
            await createProviderModel(accessToken, provider.id, {
              model: payload.model,
              show_in_picker: false,
              tags: payload.tags.length > 0 ? payload.tags : undefined,
              advanced_json: payload.advancedJSON ?? undefined,
            })
            setCreatingModel(false)
            onChanged()
          } catch (e) {
            throw new Error(isApiError(e) ? e.message : p.saveFailed)
          }
        }}
      />
      )}

      <ConfirmDialog
        open={showDeleteAllConfirm}
        onClose={() => setShowDeleteAllConfirm(false)}
        onConfirm={() => {
          setShowDeleteAllConfirm(false)
          void handleDeleteAll()
        }}
        title={p.deleteAllConfirmTitle ?? 'Delete all models'}
        message={p.deleteAllConfirmDesc ?? 'This will remove every model under this provider. Continue?'}
        confirmLabel={p.deleteAll ?? 'Delete all'}
        loading={deletingAll}
      />
    </div>
  )
}

const ModelRow = memo(function ModelRow({ pm, onToggle, onEdit, onDelete }: {
  pm: LlmProviderModel
  onToggle: (id: string, current: boolean) => void
  onEdit: (pm: LlmProviderModel) => void
  onDelete: (id: string) => void
}) {
  return (
    <div
      className="group flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--c-border-subtle)] px-4 py-2.5"
      style={{ contentVisibility: 'auto', containIntrinsicBlockSize: '52px' }}
    >
      <div className="min-w-0 flex-1 flex items-center gap-1.5">
        <p className="truncate text-sm font-medium text-[var(--c-text-primary)]">{pm.model}</p>
        {pm.tags.includes('embedding') && (
          <span className="shrink-0 rounded-md px-2 py-0.5 text-xs font-medium" style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-muted)' }}>emb</span>
        )}
      </div>
      <div className="flex w-full shrink-0 items-center justify-end gap-1.5 sm:w-auto">
        <PillToggle checked={pm.show_in_picker} onChange={() => onToggle(pm.id, pm.show_in_picker)} />
        <button
          onClick={() => onEdit(pm)}
          className="rounded-md p-1.5 text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
        >
          <SlidersHorizontal size={14} />
        </button>
        <button
          onClick={() => onDelete(pm.id)}
          className="rounded-md p-1.5 text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-red-500"
        >
          <Trash2 size={14} />
        </button>
      </div>
    </div>
  )
})

function LabelField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{label}</label>
      {children}
    </div>
  )
}

function ModelTestButton({ accessToken, provider, label, searchPlaceholder }: {
  accessToken: string
  provider: LlmProvider
  label: string
  searchPlaceholder: string
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [testing, setTesting] = useState<string | null>(null)
  const [result, setResult] = useState<{ modelId: string; success: boolean; latency?: number; error?: string } | null>(null)
  const [showError, setShowError] = useState(false)

  const pickerModels = useMemo(
    () => provider.models.filter((m) => m.show_in_picker),
    [provider.models],
  )

  const filtered = useMemo(() => {
    if (!open) return []
    const q = search.trim().toLowerCase()
    return q ? pickerModels.filter((m) => m.model.toLowerCase().includes(q)) : pickerModels
  }, [open, search, pickerModels])

  const handleTest = async (model: LlmProviderModel) => {
    setTesting(model.id)
    setOpen(false)
    try {
      const res = await testLlmProviderModel(accessToken, provider.id, model.id)
      setResult({ modelId: model.id, success: res.success, latency: res.latency_ms ?? undefined, error: res.error ?? undefined })
    } catch (e) {
      setResult({ modelId: model.id, success: false, error: isApiError(e) ? e.message : 'Unknown error' })
    } finally {
      setTesting(null)
    }
  }

  return (
    <div className="relative flex items-center gap-2">
      <button
        type="button"
        onClick={() => {
          if (result?.success && !testing) { setResult(null); return }
          setOpen((prev) => { if (!prev) setSearch(''); return !prev })
        }}
        disabled={testing !== null || pickerModels.length === 0}
        className="button-secondary inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-3 text-sm font-medium text-[var(--c-text-secondary)] transition-colors disabled:cursor-not-allowed disabled:opacity-40"
        style={secondaryButtonBorderStyle}
      >
        {testing
          ? <Loader2 size={12} className="animate-spin" />
          : result
            ? result.success
              ? <AnimatedCheck size={12} color="var(--c-status-success-text)" />
              : <X size={12} className="text-[var(--c-status-error-text)]" />
            : <Zap size={12} strokeWidth={1.5} />}
        {label}
      </button>
      {result && !result.success && !testing && (
        <div className="relative">
          <button
            type="button"
            onClick={() => setShowError((v) => !v)}
            className="inline-flex h-8 shrink-0 items-center gap-1 rounded-lg px-2.5 text-xs text-[var(--c-status-error-text)] transition-colors hover:bg-[var(--c-bg-sub)]"
            style={secondaryButtonBorderStyle}
          >
            Error
          </button>
          {showError && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setShowError(false)} />
              <div
                className="dropdown-menu absolute right-0 top-[calc(100%+6px)] z-50 max-w-[320px] min-w-[200px]"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '10px',
                  padding: '12px',
                  background: 'var(--c-bg-menu)',
                  boxShadow: 'var(--c-dropdown-shadow)',
                  maxHeight: '160px',
                  overflowY: 'auto',
                }}
              >
                <pre className="whitespace-pre-wrap break-all text-xs text-[var(--c-text-secondary)]">{result?.error ?? ''}</pre>
              </div>
            </>
          )}
        </div>
      )}
      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div
            className="absolute right-0 top-[calc(100%+6px)] z-20 min-w-[220px] overflow-hidden dropdown-menu"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            <div style={{ padding: '4px 4px 2px' }}>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={searchPlaceholder}
                className="w-full rounded-md px-3 py-1.5 text-sm outline-none"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-deep)', color: 'var(--c-text-primary)' }}
              />
            </div>
            <div className="max-h-[280px] overflow-y-auto py-1">
              {filtered.length === 0 ? (
                <p className="px-3 py-2 text-sm text-[var(--c-text-muted)]">--</p>
              ) : filtered.map((model) => (
                <button
                  key={model.id}
                  type="button"
                  onClick={() => void handleTest(model)}
                  className="flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{
                    color: result?.modelId === model.id ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                    fontWeight: result?.modelId === model.id ? 600 : 400,
                  }}
                >
                  <span className="truncate">{model.model}</span>
                  {result?.modelId === model.id && result.success && <AnimatedCheck size={12} color="var(--c-status-success-text)" />}
                </button>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
