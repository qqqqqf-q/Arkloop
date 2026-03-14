import { useState, useCallback, useEffect, useMemo, useRef } from 'react'
import {
  Plus,
  Trash2,
  Download,
  X,
  Loader2,
  Search,
  ChevronRight,
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
  listAvailableModels,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const VENDOR_PRESETS = [
  { key: 'openai_responses', provider: 'openai', openai_api_mode: 'responses' },
  { key: 'openai_chat_completions', provider: 'openai', openai_api_mode: 'chat_completions' },
  { key: 'anthropic_message', provider: 'anthropic', openai_api_mode: undefined },
] as const

type VendorPresetKey = (typeof VENDOR_PRESETS)[number]['key']

function vendorLabel(
  key: string,
  p: { vendorOpenai: string; vendorOpenaiChat: string; vendorAnthropic: string },
): string {
  const map: Record<string, string> = {
    openai_responses: p.vendorOpenai,
    openai_chat_completions: p.vendorOpenaiChat,
    anthropic_message: p.vendorAnthropic,
  }
  return map[key] ?? key
}

function toVendorKey(provider: string, mode: string | null): VendorPresetKey {
  if (provider === 'anthropic') return 'anthropic_message'
  if (mode === 'chat_completions') return 'openai_chat_completions'
  return 'openai_responses'
}

const INPUT_CLS =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

type Props = {
  accessToken: string
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ProvidersSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const p = t.adminProviders

  // -- provider list state --
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [selectedId, setSelectedId] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  // -- provider detail form --
  const [formName, setFormName] = useState('')
  const [formApiKey, setFormApiKey] = useState('')
  const [formBaseUrl, setFormBaseUrl] = useState('')
  const [saving, setSaving] = useState(false)

  // -- add provider modal --
  const [showAddProvider, setShowAddProvider] = useState(false)
  const [newVendor, setNewVendor] = useState<VendorPresetKey>('openai_responses')
  const [newName, setNewName] = useState('')
  const [newApiKey, setNewApiKey] = useState('')
  const [newBaseUrl, setNewBaseUrl] = useState('')
  const [newError, setNewError] = useState('')
  const [creating, setCreating] = useState(false)

  // -- delete provider --
  const [deleteTarget, setDeleteTarget] = useState<LlmProvider | null>(null)
  const [deleting, setDeleting] = useState(false)

  // -- add model modal --
  const [showAddModel, setShowAddModel] = useState(false)
  const [newModelName, setNewModelName] = useState('')
  const [newModelTags, setNewModelTags] = useState<string[]>([])
  const [newModelTagInput, setNewModelTagInput] = useState('')
  const [newModelDefault, setNewModelDefault] = useState(false)
  const [newModelError, setNewModelError] = useState('')
  const [addingModel, setAddingModel] = useState(false)

  // -- delete model --
  const [deleteModelTarget, setDeleteModelTarget] = useState<LlmProviderModel | null>(null)
  const [deletingModel, setDeletingModel] = useState(false)

  // -- import models modal --
  const [showImport, setShowImport] = useState(false)
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([])
  const [importSelected, setImportSelected] = useState<Set<string>>(new Set())
  const [importLoading, setImportLoading] = useState(false)
  const [importError, setImportError] = useState('')
  const [importing, setImporting] = useState(false)
  const [importSearch, setImportSearch] = useState('')

  const firstLoadRef = useRef(true)

  // -----------------------------------------------------------------------
  // Data fetching
  // -----------------------------------------------------------------------

  const fetchProviders = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const data = await listLlmProviders(accessToken)
      setProviders(data)
      if (firstLoadRef.current && data.length > 0) {
        setSelectedId(data[0].id)
        firstLoadRef.current = false
      }
    } catch {
      setLoadError(p.loadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, p.loadFailed])

  useEffect(() => {
    void fetchProviders()
  }, [fetchProviders])

  const selected = providers.find((pv) => pv.id === selectedId)

  // sync form when selection changes
  useEffect(() => {
    if (!selected) return
    setFormName(selected.name)
    setFormApiKey('')
    setFormBaseUrl(selected.base_url ?? '')
  }, [selected])

  const filtered = providers.filter(
    (pv) => !searchQuery || pv.name.toLowerCase().includes(searchQuery.toLowerCase()),
  )

  // -----------------------------------------------------------------------
  // Handlers — Provider CRUD
  // -----------------------------------------------------------------------

  const handleSaveProvider = useCallback(async () => {
    if (!selected || saving) return
    const name = formName.trim()
    if (!name) return
    setSaving(true)
    try {
      const req: Record<string, unknown> = { name }
      if (formApiKey.trim()) req.api_key = formApiKey.trim()
      req.base_url = formBaseUrl.trim() || null
      await updateLlmProvider(accessToken, selected.id, req)
      await fetchProviders()
    } catch {
      // error silently consumed — user can retry
    } finally {
      setSaving(false)
    }
  }, [selected, saving, formName, formApiKey, formBaseUrl, accessToken, fetchProviders])

  const handleCreateProvider = useCallback(async () => {
    const name = newName.trim()
    const apiKey = newApiKey.trim()
    if (!name) {
      setNewError(p.providerName)
      return
    }
    if (!apiKey) {
      setNewError(p.apiKey)
      return
    }
    setCreating(true)
    setNewError('')
    try {
      const preset = VENDOR_PRESETS.find((v) => v.key === newVendor)!
      const baseUrl = newBaseUrl.trim()
      const created = await createLlmProvider(accessToken, {
        name,
        provider: preset.provider,
        api_key: apiKey,
        ...(baseUrl ? { base_url: baseUrl } : {}),
        ...(preset.openai_api_mode ? { openai_api_mode: preset.openai_api_mode } : {}),
      })
      setShowAddProvider(false)
      setNewName('')
      setNewApiKey('')
      setNewBaseUrl('')
      setNewVendor('openai_responses')
      await fetchProviders()
      setSelectedId(created.id)
    } catch {
      setNewError(p.saveFailed)
    } finally {
      setCreating(false)
    }
  }, [newName, newApiKey, newVendor, newBaseUrl, accessToken, fetchProviders, p])

  const handleDeleteProvider = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteLlmProvider(accessToken, deleteTarget.id)
      setDeleteTarget(null)
      if (selectedId === deleteTarget.id) {
        setSelectedId(providers.find((pv) => pv.id !== deleteTarget.id)?.id ?? '')
      }
      await fetchProviders()
    } catch {
      // error silently consumed
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, selectedId, providers, accessToken, fetchProviders])

  // -----------------------------------------------------------------------
  // Handlers — Model CRUD
  // -----------------------------------------------------------------------

  const handleAddModel = useCallback(async () => {
    if (!selected || addingModel) return
    const model = newModelName.trim()
    if (!model) {
      setNewModelError(p.modelName)
      return
    }
    setAddingModel(true)
    setNewModelError('')
    try {
      await createProviderModel(accessToken, selected.id, {
        model,
        tags: newModelTags,
        is_default: newModelDefault,
        priority: 1,
      })
      setShowAddModel(false)
      setNewModelName('')
      setNewModelTags([])
      setNewModelTagInput('')
      setNewModelDefault(false)
      await fetchProviders()
    } catch {
      setNewModelError(p.saveFailed)
    } finally {
      setAddingModel(false)
    }
  }, [selected, addingModel, newModelName, newModelTags, newModelDefault, accessToken, fetchProviders, p])

  const handleDeleteModel = useCallback(async () => {
    if (!selected || !deleteModelTarget) return
    setDeletingModel(true)
    try {
      await deleteProviderModel(accessToken, selected.id, deleteModelTarget.id)
      setDeleteModelTarget(null)
      await fetchProviders()
    } catch {
      // error silently consumed
    } finally {
      setDeletingModel(false)
    }
  }, [selected, deleteModelTarget, accessToken, fetchProviders])

  // -----------------------------------------------------------------------
  // Handlers — Import models
  // -----------------------------------------------------------------------

  const openImport = useCallback(async () => {
    if (!selected) return
    setShowImport(true)
    setImportLoading(true)
    setImportError('')
    setImportSelected(new Set())
    setImportSearch('')
    try {
      const data = await listAvailableModels(accessToken, selected.id)
      setAvailableModels(data.models.filter((m) => !m.configured))
    } catch {
      setImportError(p.loadFailed)
      setAvailableModels([])
    } finally {
      setImportLoading(false)
    }
  }, [selected, accessToken, p.loadFailed])

  const handleImport = useCallback(async () => {
    if (!selected || importing || importSelected.size === 0) return
    setImporting(true)
    try {
      for (const modelId of importSelected) {
        await createProviderModel(accessToken, selected.id, {
          model: modelId,
          priority: 1,
        })
      }
      setShowImport(false)
      await fetchProviders()
    } catch {
      // error silently consumed
    } finally {
      setImporting(false)
    }
  }, [selected, importing, importSelected, accessToken, fetchProviders])

  const toggleImportModel = useCallback((id: string) => {
    setImportSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const filteredImportModels = useMemo(() => {
    const kw = importSearch.trim().toLowerCase()
    if (!kw) return availableModels
    return availableModels.filter(
      (m) => m.id.toLowerCase().includes(kw) || m.name.toLowerCase().includes(kw),
    )
  }, [availableModels, importSearch])

  // -----------------------------------------------------------------------
  // Render — Loading
  // -----------------------------------------------------------------------

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  if (loadError) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 py-16">
        <p className="text-sm text-red-400">{loadError}</p>
        <button
          onClick={() => void fetchProviders()}
          className="text-sm text-[var(--c-text-link)] hover:underline"
        >
          Retry
        </button>
      </div>
    )
  }

  // -----------------------------------------------------------------------
  // Render — Main layout
  // -----------------------------------------------------------------------

  return (
    <div className="-m-6 flex overflow-hidden" style={{ height: 'calc(100% + 48px)' }}>
      {/* ---- Left sidebar: provider list ---- */}
      <div className="flex w-[240px] shrink-0 flex-col overflow-hidden border-r border-[var(--c-border-subtle)]">
        {/* Search */}
        <div className="p-2">
          <div className="relative">
            <Search
              size={14}
              className="absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
            />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={p.searchProviders}
              className={`${INPUT_CLS} pl-8`}
            />
          </div>
        </div>

        {/* List */}
        <div className="flex-1 overflow-y-auto px-2">
          <div className="flex flex-col gap-[3px]">
            {filtered.map((pv) => (
              <button
                key={pv.id}
                onClick={() => setSelectedId(pv.id)}
                className={[
                  'flex h-[30px] items-center truncate rounded-[5px] px-3 text-left text-sm font-medium transition-colors',
                  pv.id === selectedId
                    ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                ].join(' ')}
              >
                {pv.name}
              </button>
            ))}
          </div>
        </div>

        {/* Add provider button */}
        <div className="border-t border-[var(--c-border-subtle)] px-3 py-3">
          <button
            onClick={() => {
              setShowAddProvider(true)
              setNewName('')
              setNewApiKey('')
              setNewBaseUrl('')
              setNewVendor('openai_responses')
              setNewError('')
            }}
            className="flex h-7 w-full items-center justify-center gap-1.5 rounded-md text-sm text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <Plus size={14} />
            {p.addProvider}
          </button>
        </div>
      </div>

      {/* ---- Right panel: provider detail ---- */}
      <div className="flex-1 overflow-y-auto p-5">
        {selected ? (
          <div className="mx-auto max-w-2xl space-y-6">
            {/* Provider name header */}
            <div className="flex items-center gap-2">
              <h3 className="text-base font-semibold text-[var(--c-text-primary)]">
                {selected.name}
              </h3>
              <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                {vendorLabel(
                  toVendorKey(selected.provider, selected.openai_api_mode),
                  p,
                )}
              </span>
            </div>

            {/* Provider edit form */}
            <div className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">
                  {p.providerName}
                </label>
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder={p.providerNamePlaceholder}
                  className={INPUT_CLS}
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">
                  {p.apiKey}
                </label>
                <input
                  type="password"
                  value={formApiKey}
                  onChange={(e) => setFormApiKey(e.target.value)}
                  placeholder={
                    selected.key_prefix
                      ? `${selected.key_prefix}${'*'.repeat(32)}`
                      : p.apiKeyPlaceholder
                  }
                  className={INPUT_CLS}
                />
                {selected.key_prefix && (
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">
                    {p.apiKeyHint}
                  </p>
                )}
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">
                  {p.baseUrl}
                </label>
                <input
                  type="text"
                  value={formBaseUrl}
                  onChange={(e) => setFormBaseUrl(e.target.value)}
                  placeholder={p.baseUrlPlaceholder}
                  className={INPUT_CLS}
                />
              </div>
            </div>

            {/* Provider action bar */}
            <div className="flex items-center justify-between border-b border-[var(--c-border-subtle)] pb-4">
              <button
                onClick={() => setDeleteTarget(selected)}
                className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500"
              >
                <Trash2 size={12} />
                {p.deleteProvider}
              </button>
              <button
                onClick={() => void handleSaveProvider()}
                disabled={saving || !formName.trim()}
                className="rounded-md bg-[var(--c-accent)] px-4 py-1.5 text-sm font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {saving ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  p.save
                )}
              </button>
            </div>

            {/* ---- Models section ---- */}
            <div>
              <div className="flex items-center justify-between">
                <h4 className="text-sm font-medium text-[var(--c-text-primary)]">
                  {p.modelsSection}
                </h4>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => void openImport()}
                    className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
                  >
                    <Download size={12} />
                    {p.importModels}
                  </button>
                  <button
                    onClick={() => {
                      setShowAddModel(true)
                      setNewModelName('')
                      setNewModelTags([])
                      setNewModelTagInput('')
                      setNewModelDefault(false)
                      setNewModelError('')
                    }}
                    className="inline-flex items-center gap-1.5 rounded-md bg-[var(--c-accent)] px-3 py-1.5 text-xs font-medium text-white transition-colors hover:opacity-90"
                  >
                    <Plus size={12} />
                    {p.addModel}
                  </button>
                </div>
              </div>

              <div className="mt-3 space-y-2">
                {selected.models.length === 0 ? (
                  <p className="py-8 text-center text-sm text-[var(--c-text-muted)]">
                    {p.noModels}
                  </p>
                ) : (
                  selected.models.map((m) => (
                    <div
                      key={m.id}
                      className="flex items-center justify-between rounded-xl border px-4 py-3"
                      style={{ borderColor: 'var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
                    >
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-[var(--c-text-primary)]">
                          {m.model}
                          {m.is_default && (
                            <span className="ml-2 rounded-full bg-[var(--c-accent)]/10 px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-accent)]">
                              {p.isDefault}
                            </span>
                          )}
                        </p>
                        {m.tags.length > 0 && (
                          <div className="mt-1.5 flex flex-wrap gap-1">
                            {m.tags.map((tag) => (
                              <span
                                key={tag}
                                className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] text-[var(--c-text-secondary)]"
                              >
                                {tag}
                              </span>
                            ))}
                          </div>
                        )}
                      </div>
                      <button
                        onClick={() => setDeleteModelTarget(m)}
                        className="ml-2 shrink-0 rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-red-500"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))
                )}
              </div>
            </div>
          </div>
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-2">
            <ChevronRight size={24} className="text-[var(--c-text-muted)]" />
            <p className="text-sm text-[var(--c-text-muted)]">{p.noProviders}</p>
            <p className="text-xs text-[var(--c-text-muted)]">{p.noProvidersDesc}</p>
          </div>
        )}
      </div>

      {/* ================================================================ */}
      {/* Modals — rendered inline (no portal)                             */}
      {/* ================================================================ */}

      {/* ---- Add Provider Modal ---- */}
      {showAddProvider && (
        <Overlay onClose={() => !creating && setShowAddProvider(false)}>
          <h3 className="mb-4 text-sm font-semibold text-[var(--c-text-primary)]">
            {p.addProvider}
          </h3>
          <div className="flex flex-col gap-4">
            <Field label={p.vendor}>
              <select
                value={newVendor}
                onChange={(e) => {
                  setNewVendor(e.target.value as VendorPresetKey)
                  setNewError('')
                }}
                className={INPUT_CLS}
              >
                {VENDOR_PRESETS.map((v) => (
                  <option key={v.key} value={v.key}>
                    {vendorLabel(v.key, p)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={p.providerName}>
              <input
                type="text"
                value={newName}
                onChange={(e) => {
                  setNewName(e.target.value)
                  setNewError('')
                }}
                placeholder={p.providerNamePlaceholder}
                className={INPUT_CLS}
              />
            </Field>
            <Field label={p.apiKey}>
              <input
                type="password"
                value={newApiKey}
                onChange={(e) => {
                  setNewApiKey(e.target.value)
                  setNewError('')
                }}
                placeholder={p.apiKeyPlaceholder}
                className={INPUT_CLS}
              />
            </Field>
            <Field label={p.baseUrl}>
              <input
                type="text"
                value={newBaseUrl}
                onChange={(e) => setNewBaseUrl(e.target.value)}
                placeholder={p.baseUrlPlaceholder}
                className={INPUT_CLS}
              />
            </Field>
            {newError && (
              <p className="text-xs text-red-400">{newError}</p>
            )}
            <div className="mt-2 flex justify-end gap-2">
              <button
                onClick={() => setShowAddProvider(false)}
                disabled={creating}
                className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {p.cancel}
              </button>
              <button
                onClick={() => void handleCreateProvider()}
                disabled={creating}
                className="rounded-md bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {creating ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  p.addProvider
                )}
              </button>
            </div>
          </div>
        </Overlay>
      )}

      {/* ---- Delete Provider Confirmation ---- */}
      {deleteTarget && (
        <Overlay onClose={() => !deleting && setDeleteTarget(null)}>
          <h3 className="mb-2 text-sm font-semibold text-[var(--c-text-primary)]">
            {p.deleteProvider}
          </h3>
          <p className="mb-4 text-sm text-[var(--c-text-secondary)]">
            {p.deleteProviderConfirm}
          </p>
          <div className="flex justify-end gap-2">
            <button
              onClick={() => setDeleteTarget(null)}
              disabled={deleting}
              className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {p.cancel}
            </button>
            <button
              onClick={() => void handleDeleteProvider()}
              disabled={deleting}
              className="rounded-md bg-red-600 px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50"
            >
              {deleting ? (
                <Loader2 size={14} className="animate-spin" />
              ) : (
                p.deleteProvider
              )}
            </button>
          </div>
        </Overlay>
      )}

      {/* ---- Add Model Modal ---- */}
      {showAddModel && (
        <Overlay onClose={() => !addingModel && setShowAddModel(false)}>
          <h3 className="mb-4 text-sm font-semibold text-[var(--c-text-primary)]">
            {p.addModel}
          </h3>
          <div className="flex flex-col gap-4">
            <Field label={p.modelName}>
              <input
                type="text"
                value={newModelName}
                onChange={(e) => {
                  setNewModelName(e.target.value)
                  setNewModelError('')
                }}
                placeholder={p.modelNamePlaceholder}
                className={INPUT_CLS}
              />
            </Field>
            <TagInput
              label={p.tags}
              tags={newModelTags}
              tagInput={newModelTagInput}
              onTagsChange={setNewModelTags}
              onTagInputChange={setNewModelTagInput}
              placeholder={p.tagsPlaceholder}
            />
            <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
              <input
                type="checkbox"
                checked={newModelDefault}
                onChange={(e) => setNewModelDefault(e.target.checked)}
                className="rounded"
              />
              {p.isDefault}
            </label>
            {newModelError && (
              <p className="text-xs text-red-400">{newModelError}</p>
            )}
            <div className="mt-2 flex justify-end gap-2">
              <button
                onClick={() => setShowAddModel(false)}
                disabled={addingModel}
                className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {p.cancel}
              </button>
              <button
                onClick={() => void handleAddModel()}
                disabled={addingModel}
                className="rounded-md bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {addingModel ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  p.addModel
                )}
              </button>
            </div>
          </div>
        </Overlay>
      )}

      {/* ---- Delete Model Confirmation ---- */}
      {deleteModelTarget && (
        <Overlay onClose={() => !deletingModel && setDeleteModelTarget(null)}>
          <h3 className="mb-2 text-sm font-semibold text-[var(--c-text-primary)]">
            {p.deleteModel}
          </h3>
          <p className="mb-4 text-sm text-[var(--c-text-secondary)]">
            {deleteModelTarget.model}
          </p>
          <div className="flex justify-end gap-2">
            <button
              onClick={() => setDeleteModelTarget(null)}
              disabled={deletingModel}
              className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {p.cancel}
            </button>
            <button
              onClick={() => void handleDeleteModel()}
              disabled={deletingModel}
              className="rounded-md bg-red-600 px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50"
            >
              {deletingModel ? (
                <Loader2 size={14} className="animate-spin" />
              ) : (
                p.deleteModel
              )}
            </button>
          </div>
        </Overlay>
      )}

      {/* ---- Import Models Modal ---- */}
      {showImport && (
        <Overlay onClose={() => !importing && setShowImport(false)}>
          <h3 className="mb-4 text-sm font-semibold text-[var(--c-text-primary)]">
            {p.importModels}
          </h3>
          <div className="flex flex-col gap-4">
            {importLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
                <span className="ml-2 text-sm text-[var(--c-text-muted)]">
                  {p.importing}
                </span>
              </div>
            ) : importError ? (
              <p className="py-4 text-center text-sm text-red-400">{importError}</p>
            ) : availableModels.length === 0 ? (
              <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">
                {p.noModels}
              </p>
            ) : (
              <>
                <div className="relative">
                  <Search
                    size={14}
                    className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
                  />
                  <input
                    type="text"
                    value={importSearch}
                    onChange={(e) => setImportSearch(e.target.value)}
                    placeholder={p.searchProviders}
                    className={`${INPUT_CLS} pl-9`}
                  />
                </div>
                {filteredImportModels.length === 0 ? (
                  <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">
                    {p.noModels}
                  </p>
                ) : (
                  <div className="max-h-[400px] space-y-1 overflow-y-auto">
                    {filteredImportModels.map((m) => (
                      <label
                        key={m.id}
                        className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-[var(--c-bg-sub)]"
                      >
                        <input
                          type="checkbox"
                          checked={importSelected.has(m.id)}
                          onChange={() => toggleImportModel(m.id)}
                          className="rounded"
                        />
                        <span className="text-sm text-[var(--c-text-primary)]">
                          {m.id}
                        </span>
                      </label>
                    ))}
                  </div>
                )}
              </>
            )}
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setShowImport(false)}
                disabled={importing}
                className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {p.cancel}
              </button>
              <button
                onClick={() => void handleImport()}
                disabled={importing || importSelected.size === 0}
                className="rounded-md bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {importing ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  <>
                    {p.importModels}
                    {importSelected.size > 0 && (
                      <span className="ml-1">({importSelected.size})</span>
                    )}
                  </>
                )}
              </button>
            </div>
          </div>
        </Overlay>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Inline helper components
// ---------------------------------------------------------------------------

function Overlay({
  children,
  onClose,
}: {
  children: React.ReactNode
  onClose: () => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* backdrop */}
      <div
        className="absolute inset-0 bg-black/40"
        onClick={onClose}
        onKeyDown={(e) => e.key === 'Escape' && onClose()}
        role="presentation"
      />
      {/* panel */}
      <div
        className="relative z-10 w-full max-w-md rounded-xl p-5"
        style={{
          background: 'var(--c-bg-menu)',
          border: '0.5px solid var(--c-border-subtle)',
        }}
      >
        <button
          onClick={onClose}
          className="absolute right-3 top-3 rounded p-1 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-primary)]"
        >
          <X size={14} />
        </button>
        {children}
      </div>
    </div>
  )
}

function Field({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">
        {label}
      </label>
      {children}
    </div>
  )
}

function TagInput({
  label,
  tags,
  tagInput,
  onTagsChange,
  onTagInputChange,
  placeholder,
}: {
  label: string
  tags: string[]
  tagInput: string
  onTagsChange: (t: string[]) => void
  onTagInputChange: (v: string) => void
  placeholder: string
}) {
  const addTag = () => {
    const tag = tagInput.trim()
    if (tag && !tags.includes(tag)) {
      onTagsChange([...tags, tag])
    }
    onTagInputChange('')
  }

  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">
        {label}
      </label>
      <div className="flex flex-wrap items-center gap-1.5">
        {tags.map((tag) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1 rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] text-[var(--c-text-secondary)]"
          >
            {tag}
            <button
              type="button"
              onClick={() => onTagsChange(tags.filter((tt) => tt !== tag))}
              className="text-[var(--c-text-muted)] hover:text-[var(--c-text-primary)]"
            >
              <X size={10} />
            </button>
          </span>
        ))}
        <input
          type="text"
          value={tagInput}
          onChange={(e) => onTagInputChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              addTag()
            }
            if (e.key === 'Backspace' && !tagInput && tags.length > 0) {
              onTagsChange(tags.slice(0, -1))
            }
          }}
          onBlur={addTag}
          placeholder={placeholder}
          className={`${INPUT_CLS} min-w-[100px] flex-1`}
        />
      </div>
    </div>
  )
}
