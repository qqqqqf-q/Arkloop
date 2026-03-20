import { useState, useCallback, useEffect, useRef } from 'react'
import {
  Plus,
  Trash2,
  Download,
  X,
  Loader2,
  ChevronDown,
  Check,
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
  isApiError,
} from '../../api'
import { routeAdvancedJsonFromAvailableCatalog } from '@arkloop/shared/llm/available-catalog-advanced-json'
import { useLocale } from '../../contexts/LocaleContext'

const VENDOR_PRESETS = [
  { key: 'openai_responses', provider: 'openai', openai_api_mode: 'responses' },
  { key: 'openai_chat_completions', provider: 'openai', openai_api_mode: 'chat_completions' },
  { key: 'anthropic_message', provider: 'anthropic', openai_api_mode: undefined },
  { key: 'gemini', provider: 'gemini', openai_api_mode: undefined },
] as const

type VendorPresetKey = (typeof VENDOR_PRESETS)[number]['key']

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

const INPUT_CLS =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

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
        className="flex w-full items-center justify-between rounded-md bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-deep)]"
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
    <div className="-m-6 flex overflow-hidden" style={{ height: 'calc(100% + 48px)' }}>
      {/* Provider list */}
      <div className="flex w-[200px] shrink-0 flex-col overflow-hidden border-r border-[var(--c-border-subtle)]">
        <div className="flex-1 overflow-y-auto px-2 py-1">
          <div className="flex flex-col gap-[3px]">
            {providers.map((pv) => (
              <button
                key={pv.id}
                onClick={() => setSelectedId(pv.id)}
                className={[
                  'flex h-[34px] items-center truncate rounded-[5px] px-3 text-left text-[13px] font-medium transition-colors',
                  selectedId === pv.id
                    ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
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
            className="flex h-8 w-full items-center justify-center gap-1.5 rounded-md text-[13px] text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <Plus size={14} />
            {p.addProvider}
          </button>
        </div>
        {error && <p className="px-2 pb-2 text-xs text-red-400">{error}</p>}
      </div>

      {/* Detail */}
      <div className="flex-1 overflow-y-auto p-5">
        {selected ? (
          <ProviderDetail
            key={selected.id}
            provider={selected}
            accessToken={accessToken}
            onUpdated={load}
            onDeleted={load}
            p={p}
          />
        ) : (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-[var(--c-text-muted)]">{p.noProviders}</p>
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
      })
      onCreated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const fieldLabelCls = 'block text-[11px] font-medium text-[var(--c-placeholder)] mb-1 pl-[2px]'
  const fieldInputCls = 'w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]'
  const fieldInputStyle = {
    border: '0.5px solid var(--c-border-auth)',
    height: '36px',
    padding: '0 14px',
    fontSize: '13px',
    fontWeight: 500,
    fontFamily: 'inherit',
  } as const

  return (
    <div
      className="overlay-fade-in fixed inset-0 z-50 flex items-center justify-center"
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
              style={fieldInputStyle}
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
              style={fieldInputStyle}
            />
          </div>
          <div className="col-span-2">
            <label className={fieldLabelCls}>{p.baseUrl}</label>
            <input
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value.slice(0, 500))}
              placeholder={p.baseUrlPlaceholder ?? 'https://api.example.com/v1'}
              className={fieldInputCls}
              style={fieldInputStyle}
              maxLength={500}
            />
            {baseUrl.trim() && !baseUrl.trim().startsWith('https://') && !baseUrl.trim().startsWith('http://') && (
              <span className="mt-1 block text-xs text-[var(--c-text-muted)]">需以 https:// 开头</span>
            )}
          </div>
        </div>

        {err && <p className="text-xs text-[var(--c-status-error-text)]">{err}</p>}

        <div className="flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded-[9px] px-4 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {p.cancel}
          </button>
          <button
            onClick={() => void handleSave()}
            disabled={saving || !name.trim() || !apiKey.trim()}
            className="rounded-[9px] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-opacity hover:opacity-90 disabled:opacity-50"
            style={{ background: 'var(--c-btn-bg)' }}
          >
            {saving ? <Loader2 size={14} className="animate-spin" /> : p.save}
          </button>
        </div>
      </div>
    </div>
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
    <div className="mx-auto max-w-2xl space-y-6">
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

      {err && <p className="text-xs text-red-400">{err}</p>}

      <div className="flex items-center justify-between border-b border-[var(--c-border-subtle)] pb-4">
        {confirmDelete ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--c-text-tertiary)]">{p.deleteProviderConfirm}</span>
            <button onClick={() => void handleDelete()} disabled={deleting} className="rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50">{p.deleteProvider}</button>
            <button onClick={() => setConfirmDelete(false)} className="rounded-md px-3 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">{p.cancel}</button>
          </div>
        ) : (
          <button onClick={() => setConfirmDelete(true)} className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500">
            <Trash2 size={12} />
          </button>
        )}
        <button onClick={() => void handleSave()} disabled={saving || !formName.trim()} className="rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50">
          {saving ? <Loader2 size={14} className="animate-spin" /> : p.save}
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
  const [available, setAvailable] = useState<AvailableModel[] | null>(null)
  const [loadingAvailable, setLoadingAvailable] = useState(false)
  const [importing, setImporting] = useState(false)
  const [deletingAll, setDeletingAll] = useState(false)
  const [addingModel, setAddingModel] = useState(false)
  const [newModel, setNewModel] = useState('')
  const [err, setErr] = useState('')
  const [search, setSearch] = useState('')

  const loadAvailable = useCallback(async () => {
    setLoadingAvailable(true)
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
    } catch { /* upstream unavailable */ } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, provider.id])

  useEffect(() => { void loadAvailable() }, [loadAvailable])

  const handleImportAll = async () => {
    if (!available) return
    setImporting(true)
    setErr('')
    try {
      const unconfigured = available.filter((am) => !am.configured)
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
      await Promise.all(toEnable.map((pm) => patchProviderModel(accessToken, provider.id, pm.id, { show_in_picker: true })))
      onChanged()
      void loadAvailable()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    } finally {
      setImporting(false)
    }
  }

  const handleAddModel = async () => {
    if (!newModel.trim()) return
    setErr('')
    try {
      await createProviderModel(accessToken, provider.id, { model: newModel.trim() })
      setNewModel('')
      setAddingModel(false)
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    }
  }

  const handleDeleteModel = async (modelId: string) => {
    try {
      await deleteProviderModel(accessToken, provider.id, modelId)
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    }
  }

  const handleDeleteAll = async () => {
    setDeletingAll(true)
    setErr('')
    for (const pm of provider.models) {
      try { await deleteProviderModel(accessToken, provider.id, pm.id) } catch { /* skip */ }
    }
    setDeletingAll(false)
    onChanged()
    void loadAvailable()
  }

  const handleTogglePicker = async (modelId: string, current: boolean) => {
    try {
      await patchProviderModel(accessToken, provider.id, modelId, { show_in_picker: !current })
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    }
  }

  const unconfiguredCount = available?.filter((am) => !am.configured).length ?? 0
  const filteredModels = search.trim()
    ? provider.models.filter((pm) => pm.model.toLowerCase().includes(search.trim().toLowerCase()))
    : provider.models

  return (
    <div>
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{p.modelsSection}</h4>
        <div className="flex items-center gap-2">
          {provider.models.length > 0 && (
            <button onClick={() => void handleDeleteAll()} disabled={deletingAll} className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500 disabled:opacity-50">
              {deletingAll ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
              {p.deleteAll ?? 'Delete all'}
            </button>
          )}
          {loadingAvailable && !available && <Loader2 size={12} className="animate-spin text-[var(--c-text-muted)]" />}
          {unconfiguredCount > 0 && (
            <button onClick={() => void handleImportAll()} disabled={importing} className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50">
              <Download size={12} />
              {importing ? (p.importing ?? '...') : `${p.importAll ?? 'Import all'} (${unconfiguredCount})`}
            </button>
          )}
          <button onClick={() => { setAddingModel(true); setNewModel('') }} className="rounded-md bg-[var(--c-btn-bg)] px-3 py-1.5 text-xs font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90">
            {p.addModel}
          </button>
        </div>
      </div>

      {addingModel && (
        <div className="mt-3 flex items-center gap-2">
          <input value={newModel} onChange={(e) => setNewModel(e.target.value)} placeholder={p.modelNamePlaceholder ?? 'Model name'} className={INPUT_CLS + ' flex-1'} onKeyDown={(e) => { if (e.key === 'Enter') void handleAddModel(); if (e.key === 'Escape') setAddingModel(false) }} autoFocus />
          <button onClick={() => setAddingModel(false)} className="rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"><X size={14} /></button>
        </div>
      )}

      {err && <p className="mt-2 text-xs text-red-400">{err}</p>}

      {provider.models.length > 0 && (
        <div className="mt-3">
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={p.searchProviders} className={INPUT_CLS + ' w-full'} />
        </div>
      )}

      <div className="mt-2 space-y-1 overflow-y-auto" style={{ maxHeight: '320px' }}>
        {provider.models.length === 0 && !addingModel ? (
          <p className="py-8 text-center text-sm text-[var(--c-text-muted)]">--</p>
        ) : filteredModels.length === 0 ? (
          <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">--</p>
        ) : (
          filteredModels.map((pm) => (
            <div key={pm.id} className="group flex items-center justify-between rounded-lg border border-[var(--c-border-subtle)] px-4 py-2.5">
              <div className="min-w-0 flex-1 flex items-center gap-1.5">
                <p className="truncate text-sm font-medium text-[var(--c-text-primary)]">{pm.model}</p>
                {pm.tags.includes('embedding') && (
                  <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium" style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-muted)' }}>emb</span>
                )}
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                {/* show_in_picker toggle */}
                <label className="relative inline-flex shrink-0 cursor-pointer items-center" title={pm.show_in_picker ? 'Hide from picker' : 'Show in picker'}>
                  <input type="checkbox" checked={pm.show_in_picker} onChange={() => void handleTogglePicker(pm.id, pm.show_in_picker)} className="peer sr-only" />
                  <span className="h-5 w-9 rounded-full transition-colors" style={{ background: pm.show_in_picker ? 'var(--c-btn-bg)' : 'var(--c-border-mid)' }} />
                  <span className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full transition-transform peer-checked:translate-x-4" style={{ background: pm.show_in_picker ? 'var(--c-btn-text)' : 'var(--c-bg-page)' }} />
                </label>
                {/* Delete */}
                <button onClick={() => void handleDeleteModel(pm.id)} className="rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-red-500">
                  <Trash2 size={14} />
                </button>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

function LabelField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{label}</label>
      {children}
    </div>
  )
}
