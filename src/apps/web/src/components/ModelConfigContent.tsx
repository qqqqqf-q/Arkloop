import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Download, X, Loader2 } from 'lucide-react'
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
} from '../api'
import { routeAdvancedJsonFromAvailableCatalog } from '@arkloop/shared/llm/available-catalog-advanced-json'
import { useLocale } from '../contexts/LocaleContext'

const inputCls = 'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

const PROVIDER_PRESETS = [
  { key: 'openai_responses', provider: 'openai', openai_api_mode: 'responses' },
  { key: 'openai_chat_completions', provider: 'openai', openai_api_mode: 'chat_completions' },
  { key: 'anthropic_message', provider: 'anthropic', openai_api_mode: undefined },
] as const

type ProviderPresetKey = typeof PROVIDER_PRESETS[number]['key']

function presetLabel(key: string, m: { vendorOpenaiResponses: string; vendorOpenaiChatCompletions: string; vendorAnthropicMessage: string }): string {
  const map: Record<string, string> = {
    openai_responses: m.vendorOpenaiResponses,
    openai_chat_completions: m.vendorOpenaiChatCompletions,
    anthropic_message: m.vendorAnthropicMessage,
  }
  return map[key] ?? key
}

function toPresetKey(provider: string, mode: string | null): ProviderPresetKey {
  if (provider === 'anthropic') return 'anthropic_message'
  if (mode === 'chat_completions') return 'openai_chat_completions'
  return 'openai_responses'
}

type Props = {
  accessToken: string
}

export function ModelConfigContent({ accessToken }: Props) {
  const { t } = useLocale()
  const m = t.models
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showAddProvider, setShowAddProvider] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await listLlmProviders(accessToken)
      setProviders(list)
      setSelectedId((prev) => list.find((p) => p.id === prev) ? prev : (list[0]?.id ?? null))
    } catch {
      setError(m.loadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, m.loadFailed])

  useEffect(() => { load() }, [load])

  const selected = providers.find((p) => p.id === selectedId) ?? null

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  return (
    <div className="-m-6 flex overflow-hidden" style={{ height: 'calc(100% + 48px)' }}>
      {/* provider list */}
      <div className="flex w-[140px] shrink-0 flex-col overflow-hidden border-r border-[var(--c-border-subtle)]">
        <div className="flex-1 overflow-y-auto px-2 py-1">
          <div className="flex flex-col gap-[3px]">
            {providers.map((p) => (
              <button
                key={p.id}
                onClick={() => setSelectedId(p.id)}
                className={[
                  'flex h-[30px] items-center truncate rounded-[5px] px-3 text-left text-sm font-medium transition-colors',
                  selectedId === p.id
                    ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                ].join(' ')}
              >
                {p.name}
              </button>
            ))}
          </div>
        </div>
        <div className="border-t border-[var(--c-border-subtle)] px-3 py-3">
          <button
            onClick={() => setShowAddProvider(true)}
            className="flex h-7 w-full items-center justify-center gap-1.5 rounded-md text-sm text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <Plus size={14} />
            {m.addProvider}
          </button>
        </div>
        {error && <p className="px-2 pb-2 text-xs text-red-400">{error}</p>}
      </div>

      {/* detail */}
      <div className="flex-1 overflow-y-auto p-5">
        {selected ? (
          <ProviderDetail
            key={selected.id}
            provider={selected}
            accessToken={accessToken}
            onUpdated={load}
            onDeleted={load}
          />
        ) : (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-[var(--c-text-muted)]">{m.noProviders}</p>
          </div>
        )}
      </div>

      {/* add provider modal */}
      {showAddProvider && (
        <AddProviderModal
          accessToken={accessToken}
          onClose={() => setShowAddProvider(false)}
          onCreated={() => { setShowAddProvider(false); load() }}
        />
      )}
    </div>
  )
}

// -- Add Provider Modal --

function AddProviderModal({ accessToken, onClose, onCreated }: {
  accessToken: string
  onClose: () => void
  onCreated: () => void
}) {
  const { t } = useLocale()
  const m = t.models
  const [name, setName] = useState('')
  const [preset, setPreset] = useState<ProviderPresetKey>('openai_responses')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const handleSave = async () => {
    if (!name.trim() || !apiKey.trim()) return
    setSaving(true)
    setErr('')
    try {
      const p = PROVIDER_PRESETS.find((pp) => pp.key === preset)!
      await createLlmProvider(accessToken, {
        name: name.trim(),
        provider: p.provider,
        api_key: apiKey.trim(),
        base_url: baseUrl.trim() || undefined,
        openai_api_mode: p.openai_api_mode,
      })
      onCreated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : m.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/45"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="flex w-[420px] flex-col gap-4 rounded-xl bg-[var(--c-bg-deep)] p-5 shadow-lg">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-[var(--c-text-primary)]">{m.addProvider}</h3>
          <button onClick={onClose} className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]">
            <X size={16} />
          </button>
        </div>

        <div className="space-y-3">
          <FormField label={m.providerName}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="My Provider" className={inputCls} />
          </FormField>

          <FormField label={m.providerVendor}>
            <select
              value={preset}
              onChange={(e) => setPreset(e.target.value as ProviderPresetKey)}
              className={inputCls}
            >
              {PROVIDER_PRESETS.map((p) => (
                <option key={p.key} value={p.key}>{presetLabel(p.key, m)}</option>
              ))}
            </select>
          </FormField>

          <FormField label={m.apiKey}>
            <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={m.apiKeyPlaceholder} className={inputCls} />
          </FormField>

          <FormField label={m.baseUrl}>
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder={m.baseUrlPlaceholder} className={inputCls} />
          </FormField>
        </div>

        {err && <p className="text-xs text-red-400">{err}</p>}

        <div className="flex items-center justify-end gap-2 pt-1">
          <button
            onClick={onClose}
            className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            {m.cancel}
          </button>
          <button
            onClick={handleSave}
            disabled={saving || !name.trim() || !apiKey.trim()}
            className="rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
          >
            {saving ? <Loader2 size={14} className="animate-spin" /> : m.save}
          </button>
        </div>
      </div>
    </div>
  )
}

// -- Provider Detail --

function ProviderDetail({
  provider,
  accessToken,
  onUpdated,
  onDeleted,
}: {
  provider: LlmProvider
  accessToken: string
  onUpdated: () => void
  onDeleted: () => void
}) {
  const { t } = useLocale()
  const m = t.models
  const [formName, setFormName] = useState(provider.name)
  const [formApiKey, setFormApiKey] = useState('')
  const [formBaseUrl, setFormBaseUrl] = useState(provider.base_url ?? '')
  const [formPreset, setFormPreset] = useState<ProviderPresetKey>(toPresetKey(provider.provider, provider.openai_api_mode))
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const handleSave = async () => {
    setSaving(true)
    setErr('')
    try {
      const selected = PROVIDER_PRESETS.find((p) => p.key === formPreset)
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
      setErr(isApiError(e) ? e.message : m.saveFailed)
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
      setErr(isApiError(e) ? e.message : m.deleteFailed)
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      {/* provider name */}
      <h3 className="text-base font-semibold text-[var(--c-text-primary)]">{provider.name}</h3>

      {/* provider form (always visible, like console-lite) */}
      <div className="space-y-4">
        <FormField label={m.providerVendor}>
          <select value={formPreset} onChange={(e) => setFormPreset(e.target.value as ProviderPresetKey)} className={inputCls}>
            {PROVIDER_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>{presetLabel(p.key, m)}</option>
            ))}
          </select>
        </FormField>

        <FormField label={m.providerName}>
          <input value={formName} onChange={(e) => setFormName(e.target.value)} className={inputCls} />
        </FormField>

        <FormField label={m.apiKey}>
          <input
            type="password"
            value={formApiKey}
            onChange={(e) => setFormApiKey(e.target.value)}
            placeholder={provider.key_prefix ? `${provider.key_prefix}${'*'.repeat(40)}` : m.apiKeyPlaceholder}
            className={inputCls}
          />
          {provider.key_prefix && (
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">
              {provider.key_prefix}{'*'.repeat(8)}
            </p>
          )}
        </FormField>

        <FormField label={m.baseUrl}>
          <input value={formBaseUrl} onChange={(e) => setFormBaseUrl(e.target.value)} placeholder={m.baseUrlPlaceholder} className={inputCls} />
        </FormField>
      </div>

      {err && <p className="text-xs text-red-400">{err}</p>}

      {/* action bar */}
      <div className="flex items-center justify-between border-b border-[var(--c-border-subtle)] pb-4">
        {confirmDelete ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--c-text-tertiary)]">{m.deleteProviderConfirm}</span>
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50"
            >
              {m.deleteProvider}
            </button>
            <button onClick={() => setConfirmDelete(false)} className="rounded-md px-3 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">
              {m.cancel}
            </button>
          </div>
        ) : (
          <button
            onClick={() => setConfirmDelete(true)}
            className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500"
          >
            <Trash2 size={12} />
          </button>
        )}
        <button
          onClick={handleSave}
          disabled={saving || !formName.trim()}
          className="rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
        >
          {saving ? <Loader2 size={14} className="animate-spin" /> : m.save}
        </button>
      </div>

      {/* models */}
      <ModelsSection provider={provider} accessToken={accessToken} onChanged={onUpdated} />
    </div>
  )
}

// -- Models Section --

function ModelsSection({
  provider,
  accessToken,
  onChanged,
}: {
  provider: LlmProvider
  accessToken: string
  onChanged: () => void
}) {
  const { t } = useLocale()
  const m = t.models
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
    } catch {
      // upstream unavailable
    } finally {
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
      const embeddingIds = new Set(
        toImport.filter((am) => am.type === 'embedding').map((am) => am.id.toLowerCase()),
      )
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
      const toEnable = created.filter(
        (pm) =>
          pm.model.toLowerCase().includes('gpt-4o-mini') &&
          !embeddingIds.has(pm.model.toLowerCase()),
      )
      await Promise.all(
        toEnable.map((pm) =>
          patchProviderModel(accessToken, provider.id, pm.id, { show_in_picker: true })
        )
      )
      onChanged()
      void loadAvailable()
    } catch (e) {
      setErr(isApiError(e) ? e.message : m.saveFailed)
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
      setErr(isApiError(e) ? e.message : m.saveFailed)
    }
  }

  const handleDeleteModel = async (modelId: string) => {
    try {
      await deleteProviderModel(accessToken, provider.id, modelId)
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : m.deleteFailed)
    }
  }

  const handleDeleteAll = async () => {
    setDeletingAll(true)
    setErr('')
    const toDelete = [...provider.models]
    let anyFailed = false
    for (const pm of toDelete) {
      try {
        await deleteProviderModel(accessToken, provider.id, pm.id)
      } catch (e) {
        if (isApiError(e) && e.code === 'llm_provider_models.not_found') continue
        anyFailed = true
      }
    }
    setDeletingAll(false)
    onChanged()
    void loadAvailable()
    if (anyFailed) setErr(m.deleteFailed)
  }

  const handleTogglePicker = async (modelId: string, current: boolean) => {
    try {
      await patchProviderModel(accessToken, provider.id, modelId, { show_in_picker: !current })
      onChanged()
    } catch (e) {
      setErr(isApiError(e) ? e.message : m.saveFailed)
    }
  }

  const unconfiguredCount = available?.filter((am) => !am.configured).length ?? 0
  const filteredModels = search.trim()
    ? provider.models.filter((pm) => pm.model.toLowerCase().includes(search.trim().toLowerCase()))
    : provider.models

  return (
    <div>
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{m.modelsSection}</h4>
        <div className="flex items-center gap-2">
          {provider.models.length > 0 && (
            <button
              onClick={() => void handleDeleteAll()}
              disabled={deletingAll}
              className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500 disabled:opacity-50"
            >
              {deletingAll ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
              {m.deleteAll}
            </button>
          )}
          {loadingAvailable && !available && (
            <Loader2 size={12} className="animate-spin text-[var(--c-text-muted)]" />
          )}
          {unconfiguredCount > 0 && (
            <button
              onClick={() => void handleImportAll()}
              disabled={importing}
              className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              <Download size={12} />
              {importing ? m.importing : `${m.importAll} (${unconfiguredCount})`}
            </button>
          )}
          <button
            onClick={() => { setAddingModel(true); setNewModel('') }}
            className="rounded-md bg-[var(--c-btn-bg)] px-3 py-1.5 text-xs font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90"
          >
            {m.addModel}
          </button>
        </div>
      </div>

      {addingModel && (
        <div className="mt-3 flex items-center gap-2">
          <input
            value={newModel}
            onChange={(e) => setNewModel(e.target.value)}
            placeholder={m.modelNamePlaceholder}
            className={inputCls + ' flex-1'}
            onKeyDown={(e) => { if (e.key === 'Enter') handleAddModel(); if (e.key === 'Escape') setAddingModel(false) }}
            autoFocus
          />
          <button onClick={() => setAddingModel(false)} className="rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]">
            <X size={14} />
          </button>
        </div>
      )}

      {err && <p className="mt-2 text-xs text-red-400">{err}</p>}

      {provider.models.length > 0 && (
        <div className="mt-3">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={m.searchPlaceholder}
            className={inputCls + ' w-full'}
          />
        </div>
      )}

      <div className="mt-2 space-y-1 overflow-y-auto" style={{ maxHeight: '320px' }}>
        {provider.models.length === 0 && !addingModel ? (
          <p className="py-8 text-center text-sm text-[var(--c-text-muted)]">--</p>
        ) : filteredModels.length === 0 ? (
          <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">--</p>
        ) : (
          filteredModels.map((pm) => (
            <div
              key={pm.id}
              className="group flex items-center justify-between rounded-lg border border-[var(--c-border-subtle)] px-4 py-2.5"
            >
              <div className="min-w-0 flex-1 flex items-center gap-1.5">
                <p className="truncate text-sm font-medium text-[var(--c-text-primary)]">{pm.model}</p>
                {pm.tags.includes('embedding') && (
                  <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium" style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-muted)' }}>emb</span>
                )}
              </div>
              <div className="flex items-center gap-1.5 flex-shrink-0">
                <label
                  className="relative inline-flex shrink-0 cursor-pointer items-center"
                  title={pm.show_in_picker ? m.hideFromPicker : m.showInPicker}
                >
                  <input
                    type="checkbox"
                    checked={pm.show_in_picker}
                    onChange={() => void handleTogglePicker(pm.id, pm.show_in_picker)}
                    className="peer sr-only"
                  />
                  <span
                    className="h-5 w-9 rounded-full transition-colors"
                    style={{ background: pm.show_in_picker ? 'var(--c-btn-bg)' : 'var(--c-border-mid)' }}
                  />
                  <span
                    className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full transition-transform peer-checked:translate-x-4"
                    style={{ background: pm.show_in_picker ? 'var(--c-btn-text)' : 'var(--c-bg-page)' }}
                  />
                </label>
                <button
                  onClick={() => void handleDeleteModel(pm.id)}
                  className="rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-red-500"
                >
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

// -- Shared --

function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{label}</label>
      {children}
    </div>
  )
}
