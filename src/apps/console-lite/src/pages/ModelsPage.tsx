import { useState, useCallback, useEffect, useRef, type PointerEvent as ReactPointerEvent } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  Loader2, Plus, Trash2, Download, X, Search,
} from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { Modal } from '../components/Modal'
import { FormField } from '../components/FormField'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { isApiError } from '../api/client'
import {
  listLlmProviders,
  createLlmProvider,
  updateLlmProvider,
  deleteLlmProvider,
  createProviderModel,
  updateProviderModel,
  deleteProviderModel,
  listAvailableModels,
  routeAdvancedJsonFromAvailableCatalog,
  type LlmProviderScope,
  type LlmProvider,
  type AvailableModel,
} from '../api/llm-providers'

const PROVIDER_PRESETS = [
  { key: 'openai_responses', provider: 'openai', openai_api_mode: 'responses' },
  { key: 'openai_chat_completions', provider: 'openai', openai_api_mode: 'chat_completions' },
  { key: 'anthropic_message', provider: 'anthropic', openai_api_mode: undefined },
  { key: 'gemini', provider: 'gemini', openai_api_mode: undefined },
] as const

type ProviderPresetKey = typeof PROVIDER_PRESETS[number]['key']

function toPresetKey(provider: string, mode: string | null): ProviderPresetKey {
  if (provider === 'anthropic') return 'anthropic_message'
  if (provider === 'gemini') return 'gemini'
  if (mode === 'chat_completions') return 'openai_chat_completions'
  return 'openai_responses'
}

const inputCls =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

export function ModelsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.models
  const scope: LlmProviderScope = 'platform'

  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [showAddProvider, setShowAddProvider] = useState(false)

  const [sidebarWidth, setSidebarWidth] = useState(200)
  const resizingRef = useRef(false)
  const firstLoadRef = useRef(true)

  const handleResizeStart = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    resizingRef.current = true
    const startX = e.clientX
    const startW = sidebarWidth
    const onMove = (ev: PointerEvent) => {
      const delta = ev.clientX - startX
      setSidebarWidth(Math.max(140, Math.min(400, startW + delta)))
    }
    const onUp = () => {
      resizingRef.current = false
      document.removeEventListener('pointermove', onMove)
      document.removeEventListener('pointerup', onUp)
    }
    document.addEventListener('pointermove', onMove)
    document.addEventListener('pointerup', onUp)
  }, [sidebarWidth])

  const load = useCallback(async () => {
    try {
      const list = await listLlmProviders(accessToken, scope)
      setProviders(list)
      if (firstLoadRef.current && list.length > 0) {
        setSelectedId(list[0].id)
        firstLoadRef.current = false
      } else {
        setSelectedId((prev) => list.find((p) => p.id === prev) ? prev : (list[0]?.id ?? null))
      }
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void load() }, [load])

  const selected = providers.find((p) => p.id === selectedId) ?? null

  const filtered = providers.filter((p) =>
    !searchQuery || p.name.toLowerCase().includes(searchQuery.toLowerCase()),
  )

  if (loading) {
    return (
      <div className="flex h-full flex-col overflow-hidden">
        <PageHeader title={tc.title} />
        <div className="flex flex-1 items-center justify-center">
          <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />
      <div className="flex flex-1 overflow-hidden">
        {/* Left: provider list */}
        <div style={{ width: sidebarWidth }} className="flex shrink-0 flex-col overflow-hidden border-r border-[var(--c-border-subtle)]">
          <div className="p-2">
            <div className="relative">
              <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder={tc.searchProvider}
                className="w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] py-1.5 pl-8 pr-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]"
              />
            </div>
          </div>
          <div className="flex-1 overflow-y-auto px-2">
            <div className="flex flex-col gap-[3px]">
              {filtered.map((p) => (
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
              {tc.addProvider}
            </button>
          </div>
        </div>

        {/* Resize handle */}
        <div
          onPointerDown={handleResizeStart}
          className="w-[3px] shrink-0 cursor-col-resize bg-transparent transition-colors hover:bg-[var(--c-border)]"
        />

        {/* Right: detail panel */}
        <div className="flex-1 overflow-y-auto p-5">
          {selected ? (
            <ProviderDetail
              key={selected.id}
              provider={selected}
              accessToken={accessToken}
              scope={scope}
              onUpdated={load}
              onDeleted={load}
              tc={tc}
              t={t}
            />
          ) : (
            <div className="flex h-full items-center justify-center">
              <p className="text-sm text-[var(--c-text-muted)]">{tc.noProviders}</p>
            </div>
          )}
        </div>
      </div>

      {/* Add Provider Modal */}
      {showAddProvider && (
        <AddProviderModal
          accessToken={accessToken}
          scope={scope}
          tc={tc}
          t={t}
          onClose={() => setShowAddProvider(false)}
          onCreated={(id) => {
            setShowAddProvider(false)
            setSelectedId(id)
            void load()
          }}
        />
      )}
    </div>
  )
}

// -- Add Provider Modal --

function AddProviderModal({ accessToken, scope, tc, t, onClose, onCreated }: {
  accessToken: string
  scope: LlmProviderScope
  tc: ReturnType<typeof useLocale>['t']['models']
  t: ReturnType<typeof useLocale>['t']
  onClose: () => void
  onCreated: (id: string) => void
}) {
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
      const created = await createLlmProvider({
        scope,
        name: name.trim(),
        provider: p.provider,
        api_key: apiKey.trim(),
        base_url: baseUrl.trim() || undefined,
        openai_api_mode: p.openai_api_mode,
      }, accessToken)
      onCreated(created.id)
    } catch {
      setErr(tc.saveFailed ?? t.common.error)
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
          <h3 className="text-sm font-semibold text-[var(--c-text-primary)]">{tc.addProvider}</h3>
          <button onClick={onClose} className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]">
            <X size={16} />
          </button>
        </div>

        <div className="space-y-3">
          <LabelField label={tc.name}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="My Provider" className={inputCls} />
          </LabelField>

          <LabelField label={tc.clientType}>
            <select value={preset} onChange={(e) => setPreset(e.target.value as ProviderPresetKey)} className={inputCls}>
              {PROVIDER_PRESETS.map((p) => (
                <option key={p.key} value={p.key}>{presetLabel(p.key, tc)}</option>
              ))}
            </select>
          </LabelField>

          <LabelField label={tc.apiKey}>
            <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} className={inputCls} />
          </LabelField>

          <LabelField label={tc.baseUrl}>
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.openai.com/v1" className={inputCls} />
          </LabelField>
        </div>

        {err && <p className="text-xs text-red-400">{err}</p>}

        <div className="flex items-center justify-end gap-2 pt-1">
          <button onClick={onClose} className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">
            {t.common.cancel}
          </button>
          <button
            onClick={() => void handleSave()}
            disabled={saving || !name.trim() || !apiKey.trim()}
            className="rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
          >
            {saving ? <Loader2 size={14} className="animate-spin" /> : t.common.save}
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
  scope,
  onUpdated,
  onDeleted,
  tc,
  t,
}: {
  provider: LlmProvider
  accessToken: string
  scope: LlmProviderScope
  onUpdated: () => void
  onDeleted: () => void
  tc: ReturnType<typeof useLocale>['t']['models']
  t: ReturnType<typeof useLocale>['t']
}) {
  const [formPreset, setFormPreset] = useState<ProviderPresetKey>(toPresetKey(provider.provider, provider.openai_api_mode))
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
      const selected = PROVIDER_PRESETS.find((p) => p.key === formPreset)
      await updateLlmProvider(provider.id, {
        scope,
        name: formName.trim() || undefined,
        api_key: formApiKey.trim() || undefined,
        base_url: formBaseUrl.trim() || null,
        provider: selected?.provider,
        openai_api_mode: selected?.openai_api_mode ?? null,
      }, accessToken)
      setFormApiKey('')
      onUpdated()
    } catch {
      setErr(tc.saveFailed ?? t.common.error)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    setDeleting(true)
    try {
      await deleteLlmProvider(provider.id, scope, accessToken)
      onDeleted()
    } catch {
      setErr(tc.toastFailed)
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <h3 className="text-base font-semibold text-[var(--c-text-primary)]">{provider.name}</h3>

      {/* Provider form */}
      <div className="space-y-4">
        <LabelField label={tc.clientType}>
          <select value={formPreset} onChange={(e) => setFormPreset(e.target.value as ProviderPresetKey)} className={inputCls}>
            {PROVIDER_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>{presetLabel(p.key, tc)}</option>
            ))}
          </select>
        </LabelField>

        <LabelField label={tc.name}>
          <input value={formName} onChange={(e) => setFormName(e.target.value)} className={inputCls} />
        </LabelField>

        <LabelField label={tc.apiKey}>
          <input
            type="password"
            value={formApiKey}
            onChange={(e) => setFormApiKey(e.target.value)}
            placeholder={provider.key_prefix ? `${provider.key_prefix}${'*'.repeat(40)}` : ''}
            className={inputCls}
          />
          {provider.key_prefix && (
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">{provider.key_prefix}{'*'.repeat(8)}</p>
          )}
        </LabelField>

        <LabelField label={tc.baseUrl}>
          <input value={formBaseUrl} onChange={(e) => setFormBaseUrl(e.target.value)} placeholder="https://api.openai.com/v1" className={inputCls} />
        </LabelField>
      </div>

      {err && <p className="text-xs text-red-400">{err}</p>}

      {/* Action bar */}
      <div className="flex items-center justify-between border-b border-[var(--c-border-subtle)] pb-4">
        {confirmDelete ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--c-text-tertiary)]">{tc.deleteProviderConfirm(provider.name)}</span>
            <button onClick={() => void handleDelete()} disabled={deleting} className="rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50">
              {tc.deleteProvider}
            </button>
            <button onClick={() => setConfirmDelete(false)} className="rounded-md px-3 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">
              {t.common.cancel}
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
          onClick={() => void handleSave()}
          disabled={saving || !formName.trim()}
          className="rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
        >
          {saving ? <Loader2 size={14} className="animate-spin" /> : t.common.save}
        </button>
      </div>

      {/* Models */}
      <ModelsSection provider={provider} accessToken={accessToken} scope={scope} onChanged={onUpdated} tc={tc} t={t} />
    </div>
  )
}

// -- Models Section (matches web ModelConfigContent) --

function ModelsSection({
  provider,
  accessToken,
  scope,
  onChanged,
  tc,
  t,
}: {
  provider: LlmProvider
  accessToken: string
  scope: LlmProviderScope
  onChanged: () => void
  tc: ReturnType<typeof useLocale>['t']['models']
  t: ReturnType<typeof useLocale>['t']
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
      const res = await listAvailableModels(provider.id, scope, accessToken)
      setAvailable(res.models)
    } catch {
      // upstream unavailable
    } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, provider.id, scope])

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
      for (const am of toImport) {
        const isEmb = am.type === 'embedding'
        try {
          await createProviderModel(provider.id, {
            scope,
            model: am.id,
            show_in_picker: false,
            tags: isEmb ? ['embedding'] : undefined,
            advanced_json: routeAdvancedJsonFromAvailableCatalog(am),
          }, accessToken)
        } catch (e) {
          if (isApiError(e) && e.code === 'llm_provider_models.model_conflict') continue
          throw e
        }
      }
      onChanged()
      void loadAvailable()
    } catch {
      setErr(tc.saveFailed ?? t.common.error)
    } finally {
      setImporting(false)
    }
  }

  const handleAddModel = async () => {
    if (!newModel.trim()) return
    setErr('')
    try {
      await createProviderModel(provider.id, { scope, model: newModel.trim() }, accessToken)
      setNewModel('')
      setAddingModel(false)
      onChanged()
    } catch {
      setErr(tc.saveFailed ?? t.common.error)
    }
  }

  const handleDeleteModel = async (modelId: string) => {
    try {
      await deleteProviderModel(provider.id, modelId, scope, accessToken)
      onChanged()
    } catch {
      setErr(tc.toastFailed)
    }
  }

  const handleDeleteAll = async () => {
    setDeletingAll(true)
    setErr('')
    for (const pm of provider.models) {
      try { await deleteProviderModel(provider.id, pm.id, scope, accessToken) } catch { /* skip */ }
    }
    setDeletingAll(false)
    onChanged()
    void loadAvailable()
  }

  const handleTogglePicker = async (modelId: string, current: boolean) => {
    try {
      await updateProviderModel(provider.id, modelId, { scope, show_in_picker: !current }, accessToken)
      onChanged()
    } catch {
      setErr(tc.saveFailed ?? t.common.error)
    }
  }

  const unconfiguredCount = available?.filter((am) => !am.configured).length ?? 0
  const filteredModels = search.trim()
    ? provider.models.filter((pm) => pm.model.toLowerCase().includes(search.trim().toLowerCase()))
    : provider.models

  return (
    <div>
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.modelsSection}</h4>
        <div className="flex items-center gap-2">
          {provider.models.length > 0 && (
            <button
              onClick={() => void handleDeleteAll()}
              disabled={deletingAll}
              className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500 disabled:opacity-50"
            >
              {deletingAll ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
              {tc.deleteAll ?? 'Delete all'}
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
              {importing ? (tc.importing ?? '...') : `${tc.importAll ?? 'Import all'} (${unconfiguredCount})`}
            </button>
          )}
          <button
            onClick={() => { setAddingModel(true); setNewModel('') }}
            className="rounded-md bg-[var(--c-btn-bg)] px-3 py-1.5 text-xs font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90"
          >
            {tc.addModel}
          </button>
        </div>
      </div>

      {addingModel && (
        <div className="mt-3 flex items-center gap-2">
          <input
            value={newModel}
            onChange={(e) => setNewModel(e.target.value)}
            placeholder={tc.modelName}
            className={inputCls + ' flex-1'}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAddModel(); if (e.key === 'Escape') setAddingModel(false) }}
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
            placeholder={tc.searchProvider}
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
              <div className="flex items-center gap-1.5 shrink-0">
                {/* show_in_picker toggle */}
                <label
                  className="relative inline-flex shrink-0 cursor-pointer items-center"
                  title={pm.show_in_picker ? (tc.hideFromPicker ?? 'Hide from picker') : (tc.showInPicker ?? 'Show in picker')}
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
                {/* Delete */}
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

function presetLabel(key: string, tc: ReturnType<typeof useLocale>['t']['models']): string {
  const map: Record<string, string> = {
    openai_responses: tc.clientTypeOpenaiResponse ?? 'OpenAI Responses',
    openai_chat_completions: tc.clientTypeOpenaiChat ?? 'OpenAI Chat Completions',
    anthropic_message: tc.clientTypeAnthropic ?? 'Anthropic Messages',
    gemini: tc.clientTypeGemini ?? 'Google Gemini',
  }
  return map[key] ?? key
}

function LabelField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs font-medium text-[var(--c-text-tertiary)]">{label}</label>
      {children}
    </div>
  )
}
