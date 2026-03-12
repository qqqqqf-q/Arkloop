import { useState, useCallback, useEffect, useMemo, useRef, type PointerEvent as ReactPointerEvent } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  Loader2, Plus, Trash2, Settings, Search, RefreshCw, Download, X,
} from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { Modal } from '../components/Modal'
import { FormField } from '../components/FormField'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import {
  listLlmProviders,
  createLlmProvider,
  updateLlmProvider,
  deleteLlmProvider,
  createProviderModel,
  updateProviderModel,
  deleteProviderModel,
  listAvailableModels,
  type LlmProviderScope,
  type LlmProvider,
  type LlmProviderModel,
  type AvailableModel,
} from '../api/llm-providers'

type ClientType = 'openai_response' | 'openai_chat' | 'anthropic'

const CLIENT_TYPES: ClientType[] = ['openai_response', 'openai_chat', 'anthropic']

function clientTypeToBackend(ct: ClientType): { provider: string; openai_api_mode?: string } {
  switch (ct) {
    case 'openai_response': return { provider: 'openai', openai_api_mode: 'responses' }
    case 'openai_chat': return { provider: 'openai', openai_api_mode: 'chat_completions' }
    case 'anthropic': return { provider: 'anthropic' }
  }
}

function parseObjectJSON(raw: string): Record<string, unknown> {
  const parsed = JSON.parse(raw.trim() || '{}') as unknown
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('json must be object')
  }
  return parsed as Record<string, unknown>
}


export function ModelsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.models
  const scope: LlmProviderScope = 'platform'

  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [selectedId, setSelectedId] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [loading, setLoading] = useState(true)

  // provider detail form
  const [formName, setFormName] = useState('')
  const [formApiKey, setFormApiKey] = useState('')
  const [formBaseUrl, setFormBaseUrl] = useState('')
  const [saving, setSaving] = useState(false)

  // add provider modal
  const [showAddProvider, setShowAddProvider] = useState(false)
  const [newClientType, setNewClientType] = useState<ClientType>('openai_response')
  const [newName, setNewName] = useState('')
  const [newApiKey, setNewApiKey] = useState('')
  const [newBaseUrl, setNewBaseUrl] = useState('')
  const [newError, setNewError] = useState('')
  const [creating, setCreating] = useState(false)

  // delete provider
  const [deleteTarget, setDeleteTarget] = useState<LlmProvider | null>(null)
  const [deleting, setDeleting] = useState(false)

  // add model modal
  const [showAddModel, setShowAddModel] = useState(false)
  const [newModelName, setNewModelName] = useState('')
  const [newModelTags, setNewModelTags] = useState<string[]>([])
  const [newModelTagInput, setNewModelTagInput] = useState('')
  const [newModelDefault, setNewModelDefault] = useState(false)
  const [newModelAdvancedJSON, setNewModelAdvancedJSON] = useState('{}')
  const [newModelError, setNewModelError] = useState('')
  const [addingModel, setAddingModel] = useState(false)

  // model settings modal
  const [editModel, setEditModel] = useState<LlmProviderModel | null>(null)
  const [editModelName, setEditModelName] = useState('')
  const [editModelTags, setEditModelTags] = useState<string[]>([])
  const [editModelTagInput, setEditModelTagInput] = useState('')
  const [editModelDefault, setEditModelDefault] = useState(false)
  const [editModelAdvancedJSON, setEditModelAdvancedJSON] = useState('{}')
  const [editModelError, setEditModelError] = useState('')
  const [savingModel, setSavingModel] = useState(false)

  // delete model
  const [deleteModelTarget, setDeleteModelTarget] = useState<LlmProviderModel | null>(null)
  const [deletingModel, setDeletingModel] = useState(false)

  // import models modal
  const [showImport, setShowImport] = useState(false)
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([])
  const [importSelected, setImportSelected] = useState<Set<string>>(new Set())
  const [importLoading, setImportLoading] = useState(false)
  const [importError, setImportError] = useState('')
  const [importing, setImporting] = useState(false)
  const [importSearchQuery, setImportSearchQuery] = useState('')

  const firstLoadRef = useRef(true)

  // resizable sidebar
  const [sidebarWidth, setSidebarWidth] = useState(200)
  const resizingRef = useRef(false)

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

  const fetchProviders = useCallback(async () => {
    setLoading(true)
    try {
      const data = await listLlmProviders(accessToken, scope)
      setProviders(data)
      if (firstLoadRef.current && data.length > 0) {
        setSelectedId(data[0].id)
        firstLoadRef.current = false
      }
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void fetchProviders() }, [fetchProviders])

  const selected = providers.find((p) => p.id === selectedId)

  // sync form when selection changes
  useEffect(() => {
    if (!selected) return
    setFormName(selected.name)
    setFormApiKey('')
    setFormBaseUrl(selected.base_url ?? '')
  }, [selected])

  const filtered = providers.filter((p) =>
    !searchQuery || p.name.toLowerCase().includes(searchQuery.toLowerCase()),
  )

  // -- handlers --

  const handleSaveProvider = useCallback(async () => {
    if (!selected || saving) return
    const name = formName.trim()
    if (!name) return
    setSaving(true)
    try {
      const req: Record<string, unknown> = { name }
      if (formApiKey.trim()) req.api_key = formApiKey.trim()
      const baseUrl = formBaseUrl.trim()
      req.base_url = baseUrl || null
      await updateLlmProvider(selected.id, { ...req, scope }, accessToken)
      addToast(tc.toastUpdated, 'success')
      await fetchProviders()
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [selected, saving, formName, formApiKey, formBaseUrl, accessToken, fetchProviders, addToast, tc])

  const handleCreateProvider = useCallback(async () => {
    const name = newName.trim()
    const apiKey = newApiKey.trim()
    if (!name) { setNewError(tc.errNameRequired); return }
    if (!apiKey) { setNewError(tc.errApiKeyRequired); return }
    setCreating(true)
    setNewError('')
    try {
      const backend = clientTypeToBackend(newClientType)
      const baseUrl = newBaseUrl.trim()
      const created = await createLlmProvider({
        scope,
        name,
        provider: backend.provider,
        api_key: apiKey,
        ...(baseUrl ? { base_url: baseUrl } : {}),
        ...(backend.openai_api_mode ? { openai_api_mode: backend.openai_api_mode } : {}),
      }, accessToken)
      setShowAddProvider(false)
      setNewName('')
      setNewApiKey('')
      setNewBaseUrl('')
      setNewClientType('openai_response')
      addToast(tc.toastCreated, 'success')
      await fetchProviders()
      setSelectedId(created.id)
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setCreating(false)
    }
  }, [newName, newApiKey, newClientType, newBaseUrl, accessToken, fetchProviders, addToast, tc])

  const handleDeleteProvider = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteLlmProvider(deleteTarget.id, scope, accessToken)
      addToast(tc.toastDeleted, 'success')
      setDeleteTarget(null)
      if (selectedId === deleteTarget.id) {
        setSelectedId(providers.find((p) => p.id !== deleteTarget.id)?.id ?? '')
      }
      await fetchProviders()
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, selectedId, providers, accessToken, fetchProviders, addToast, tc])

  const handleAddModel = useCallback(async () => {
    if (!selected || addingModel) return
    const model = newModelName.trim()
    if (!model) { setNewModelError(tc.errModelRequired); return }
    let advancedJSON: Record<string, unknown>
    try {
      advancedJSON = parseObjectJSON(newModelAdvancedJSON)
    } catch {
      setNewModelError(tc.errAdvancedJsonInvalid)
      return
    }
    setAddingModel(true)
    setNewModelError('')
    try {
      await createProviderModel(selected.id, {
        scope,
        model,
        tags: newModelTags,
        is_default: newModelDefault,
        priority: 1,
        advanced_json: advancedJSON,
      }, accessToken)
      setShowAddModel(false)
      setNewModelName('')
      setNewModelTags([])
      setNewModelTagInput('')
      setNewModelDefault(false)
      setNewModelAdvancedJSON('{}')
      addToast(tc.toastCreated, 'success')
      await fetchProviders()
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setAddingModel(false)
    }
  }, [selected, addingModel, newModelName, newModelTags, newModelDefault, newModelAdvancedJSON, accessToken, fetchProviders, addToast, tc])

  const openEditModel = useCallback((m: LlmProviderModel) => {
    setEditModel(m)
    setEditModelName(m.model)
    setEditModelTags([...m.tags])
    setEditModelTagInput('')
    setEditModelDefault(m.is_default)
    setEditModelAdvancedJSON(JSON.stringify(m.advanced_json ?? {}, null, 2))
    setEditModelError('')
  }, [])

  const handleSaveModel = useCallback(async () => {
    if (!selected || !editModel || savingModel) return
    const model = editModelName.trim()
    if (!model) { setEditModelError(tc.errModelRequired); return }
    let advancedJSON: Record<string, unknown>
    try {
      advancedJSON = parseObjectJSON(editModelAdvancedJSON)
    } catch {
      setEditModelError(tc.errAdvancedJsonInvalid)
      return
    }
    setSavingModel(true)
    setEditModelError('')
    try {
      await updateProviderModel(selected.id, editModel.id, {
        scope,
        model,
        tags: editModelTags,
        is_default: editModelDefault,
        advanced_json: advancedJSON,
      }, accessToken)
      setEditModel(null)
      addToast(tc.toastUpdated, 'success')
      await fetchProviders()
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSavingModel(false)
    }
  }, [selected, editModel, savingModel, editModelName, editModelTags, editModelDefault, editModelAdvancedJSON, accessToken, fetchProviders, addToast, tc])

  const handleDeleteModel = useCallback(async () => {
    if (!selected || !deleteModelTarget) return
    setDeletingModel(true)
    try {
      await deleteProviderModel(selected.id, deleteModelTarget.id, scope, accessToken)
      setDeleteModelTarget(null)
      addToast(tc.toastDeleted, 'success')
      await fetchProviders()
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setDeletingModel(false)
    }
  }, [selected, deleteModelTarget, accessToken, fetchProviders, addToast, tc])

  const openImport = useCallback(async () => {
    if (!selected) return
    setShowImport(true)
    setImportLoading(true)
    setImportError('')
    setImportSelected(new Set())
    setImportSearchQuery('')
    try {
      const data = await listAvailableModels(selected.id, scope, accessToken)
      setAvailableModels(data.models.filter((m) => !m.configured))
    } catch {
      setImportError(tc.importModelsError)
      setAvailableModels([])
    } finally {
      setImportLoading(false)
    }
  }, [selected, accessToken, tc.importModelsError])

  const handleImport = useCallback(async () => {
    if (!selected || importing || importSelected.size === 0) return
    setImporting(true)
    try {
      for (const modelId of importSelected) {
        await createProviderModel(selected.id, {
          scope,
          model: modelId,
          priority: 1,
        }, accessToken)
      }
      setShowImport(false)
      addToast(tc.toastImported, 'success')
      await fetchProviders()
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setImporting(false)
    }
  }, [selected, importing, importSelected, accessToken, fetchProviders, addToast, tc])

  const toggleImportModel = useCallback((id: string) => {
    setImportSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const filteredImportModels = useMemo(() => {
    const keyword = importSearchQuery.trim().toLowerCase()
    if (!keyword) return availableModels
    return availableModels.filter((item) =>
      item.id.toLowerCase().includes(keyword) || item.name.toLowerCase().includes(keyword),
    )
  }, [availableModels, importSearchQuery])

  const inputCls =
    'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
  const editInputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  const clientTypeLabel = (ct: ClientType) => {
    switch (ct) {
      case 'openai_response': return tc.clientTypeOpenaiResponse
      case 'openai_chat': return tc.clientTypeOpenaiChat
      case 'anthropic': return tc.clientTypeAnthropic
    }
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />

      {loading ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      ) : (
        <div className="flex flex-1 overflow-hidden">
          {/* Left: provider list */}
          <div style={{ width: sidebarWidth }} className="shrink-0 flex flex-col overflow-hidden border-r border-[var(--c-border-console)]">
            <div className="p-2">
              <div className="relative">
                <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder={tc.searchProvider}
                  className="w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] py-1.5 pl-8 pr-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border-focus)]"
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
                      'flex h-[30px] items-center rounded-[5px] px-3 text-sm font-medium transition-colors text-left truncate',
                      p.id === selectedId
                        ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                        : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                    ].join(' ')}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            </div>
            <div className="border-t border-[var(--c-border-console)] px-3 py-3">
              <button
                onClick={() => {
                  setShowAddProvider(true)
                  setNewName('')
                  setNewApiKey('')
                  setNewBaseUrl('')
                  setNewClientType('openai_response')
                  setNewError('')
                }}
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
            className="w-[3px] shrink-0 cursor-col-resize bg-transparent transition-colors hover:bg-[var(--c-border-focus)]"
          />

          {/* Right: detail panel */}
          <div className="flex-1 overflow-y-auto p-6">
            {selected ? (
              <div className="mx-auto max-w-2xl space-y-6">
                {/* Provider name header */}
                <h3 className="text-base font-semibold text-[var(--c-text-primary)]">{selected.name}</h3>

                {/* Provider form */}
                <div className="space-y-4">
                  <div>
                    <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">{tc.name}</label>
                    <input type="text" value={formName} onChange={(e) => setFormName(e.target.value)} className={inputCls} />
                  </div>
                  <div>
                    <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">{tc.apiKey}</label>
                    <input
                      type="password"
                      value={formApiKey}
                      onChange={(e) => setFormApiKey(e.target.value)}
                      placeholder={selected.key_prefix ? `${selected.key_prefix}${'*'.repeat(40)}` : ''}
                      className={inputCls}
                    />
                    {selected.key_prefix && (
                      <p className="mt-1 text-xs text-[var(--c-text-muted)]">
                        {tc.currentKeyPrefix}: {selected.key_prefix}{'*'.repeat(8)}
                      </p>
                    )}
                  </div>
                  <div>
                    <label className="mb-1 block text-sm font-medium text-[var(--c-text-primary)]">{tc.baseUrl}</label>
                    <input type="text" value={formBaseUrl} onChange={(e) => setFormBaseUrl(e.target.value)} className={inputCls} />
                  </div>
                </div>

                {/* Provider actions */}
                <div className="flex items-center justify-between border-b border-[var(--c-border-console)] pb-4">
                  <button
                    onClick={() => setDeleteTarget(selected)}
                    className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-muted)] transition-colors hover:border-red-500/30 hover:text-red-500"
                  >
                    <Trash2 size={12} />
                  </button>
                  <button
                    onClick={handleSaveProvider}
                    disabled={saving || !formName.trim()}
                    className="rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
                  >
                    {saving ? <Loader2 size={14} className="animate-spin" /> : tc.saveChanges}
                  </button>
                </div>

                {/* Models section */}
                <div>
                  <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.modelsSection}</h4>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={openImport}
                        className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
                      >
                        <Download size={12} />
                        {tc.importModels}
                      </button>
                      <button
                        onClick={() => {
                          setShowAddModel(true)
                          setNewModelName('')
                          setNewModelTags([])
                          setNewModelTagInput('')
                          setNewModelDefault(false)
                          setNewModelAdvancedJSON('{}')
                          setNewModelError('')
                        }}
                        className="rounded-md bg-[var(--c-btn-bg)] px-3 py-1.5 text-xs font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90"
                      >
                        {tc.addModel}
                      </button>
                    </div>
                  </div>

                  <div className="mt-3 space-y-2">
                    {selected.models.length === 0 ? (
                      <p className="py-8 text-center text-sm text-[var(--c-text-muted)]">--</p>
                    ) : (
                      selected.models.map((m) => (
                        <div
                          key={m.id}
                          className="flex items-center justify-between rounded-lg border border-[var(--c-border-console)] px-4 py-3"
                        >
                          <div>
                            <p className="text-sm font-medium text-[var(--c-text-primary)]">
                              {m.model}
                              {m.is_default && (
                                <span className="ml-2 rounded-full bg-[var(--c-status-success-bg)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-status-success-text)]">
                                  {t.common.default}
                                </span>
                              )}
                            </p>
                            {m.tags.length > 0 && (
                              <div className="mt-1.5 flex flex-wrap gap-1">
                                {m.tags.map((tag) => (
                                  <span
                                    key={tag}
                                    className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[11px] text-[var(--c-text-secondary)]"
                                  >
                                    {tag}
                                  </span>
                                ))}
                              </div>
                            )}
                            {m.advanced_json && Object.keys(m.advanced_json).length > 0 && (
                              <p className="mt-1 text-[11px] text-[var(--c-text-muted)]">{tc.advancedJsonConfigured}</p>
                            )}
                          </div>
                          <div className="flex items-center gap-1">
                            <button
                              onClick={() => { void fetchProviders() }}
                              className="rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
                            >
                              <RefreshCw size={14} />
                            </button>
                            <button
                              onClick={() => openEditModel(m)}
                              className="rounded p-1.5 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
                            >
                              <Settings size={14} />
                            </button>
                            <button
                              onClick={() => setDeleteModelTarget(m)}
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
              </div>
            ) : (
              <div className="flex h-full items-center justify-center">
                <p className="text-sm text-[var(--c-text-muted)]">{tc.noProviders}</p>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Add Provider Modal */}
      <Modal
        open={showAddProvider}
        onClose={() => { if (!creating) setShowAddProvider(false) }}
        title={tc.addProviderTitle}
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.clientType} error={newError && !newClientType ? tc.errProviderRequired : ''}>
            <select
              value={newClientType}
              onChange={(e) => { setNewClientType(e.target.value as ClientType); setNewError('') }}
              className={editInputCls}
            >
              {CLIENT_TYPES.map((ct) => (
                <option key={ct} value={ct}>{clientTypeLabel(ct)}</option>
              ))}
            </select>
          </FormField>
          <FormField label={tc.name} error={newError && !newName.trim() ? tc.errNameRequired : ''}>
            <input
              type="text"
              value={newName}
              onChange={(e) => { setNewName(e.target.value); setNewError('') }}
              className={editInputCls}
            />
          </FormField>
          <FormField label={tc.apiKey} error={newError && !newApiKey.trim() ? tc.errApiKeyRequired : ''}>
            <input
              type="password"
              value={newApiKey}
              onChange={(e) => { setNewApiKey(e.target.value); setNewError('') }}
              className={editInputCls}
            />
          </FormField>
          <FormField label={tc.baseUrl}>
            <input
              type="text"
              value={newBaseUrl}
              onChange={(e) => setNewBaseUrl(e.target.value)}
              className={editInputCls}
            />
          </FormField>
          {newError && <p className="text-xs text-[var(--c-status-error-text)]">{newError}</p>}
          <div className="mt-2 flex justify-end gap-2">
            <button
              onClick={() => setShowAddProvider(false)}
              disabled={creating}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {t.common.cancel}
            </button>
            <button
              onClick={handleCreateProvider}
              disabled={creating}
              className="rounded-lg bg-[var(--c-btn-bg)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {creating ? '...' : tc.addProvider}
            </button>
          </div>
        </div>
      </Modal>

      {/* Delete Provider Dialog */}
      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDeleteProvider}
        title={tc.deleteProvider}
        message={deleteTarget ? tc.deleteProviderConfirm(deleteTarget.name) : ''}
        confirmLabel={t.common.delete}
        loading={deleting}
      />

      {/* Add Model Modal */}
      <Modal
        open={showAddModel}
        onClose={() => { if (!addingModel) setShowAddModel(false) }}
        title={tc.addModel}
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.modelName} error={newModelError}>
            <input
              type="text"
              value={newModelName}
              onChange={(e) => { setNewModelName(e.target.value); setNewModelError('') }}
              className={editInputCls}
            />
          </FormField>
          <TagInput
            label={tc.tags}
            tags={newModelTags}
            tagInput={newModelTagInput}
            onTagsChange={setNewModelTags}
            onTagInputChange={setNewModelTagInput}
            addTagPlaceholder={tc.addTag}
            inputCls={editInputCls}
          />
          <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={newModelDefault}
              onChange={(e) => setNewModelDefault(e.target.checked)}
              className="rounded"
            />
            {tc.setDefault}
          </label>
          <FormField label={tc.advancedJson}>
            <textarea
              value={newModelAdvancedJSON}
              onChange={(e) => { setNewModelAdvancedJSON(e.target.value); setNewModelError('') }}
              className={`${editInputCls} min-h-28 font-mono text-xs leading-relaxed`}
            />
          </FormField>
          <div className="mt-2 flex justify-end gap-2">
            <button
              onClick={() => setShowAddModel(false)}
              disabled={addingModel}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {t.common.cancel}
            </button>
            <button
              onClick={handleAddModel}
              disabled={addingModel}
              className="rounded-lg bg-[var(--c-btn-bg)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {addingModel ? '...' : tc.addModel}
            </button>
          </div>
        </div>
      </Modal>

      {/* Model Settings Modal */}
      <Modal
        open={!!editModel}
        onClose={() => { if (!savingModel) setEditModel(null) }}
        title={tc.modelSettings}
      >
        {editModel && (
          <div className="flex flex-col gap-4">
            <FormField label={tc.modelName} error={editModelError}>
              <input
                type="text"
                value={editModelName}
                onChange={(e) => { setEditModelName(e.target.value); setEditModelError('') }}
                className={editInputCls}
              />
            </FormField>
            <TagInput
              label={tc.tags}
              tags={editModelTags}
              tagInput={editModelTagInput}
              onTagsChange={setEditModelTags}
              onTagInputChange={setEditModelTagInput}
              addTagPlaceholder={tc.addTag}
              inputCls={editInputCls}
            />
            <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
              <input
                type="checkbox"
                checked={editModelDefault}
                onChange={(e) => setEditModelDefault(e.target.checked)}
                className="rounded"
              />
              {tc.setDefault}
            </label>
            <FormField label={tc.advancedJson}>
              <textarea
                value={editModelAdvancedJSON}
                onChange={(e) => { setEditModelAdvancedJSON(e.target.value); setEditModelError('') }}
                className={`${editInputCls} min-h-28 font-mono text-xs leading-relaxed`}
              />
            </FormField>
            <div className="mt-2 flex justify-end gap-2">
              <button
                onClick={() => setEditModel(null)}
                disabled={savingModel}
                className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {t.common.cancel}
              </button>
              <button
                onClick={handleSaveModel}
                disabled={savingModel}
                className="rounded-lg bg-[var(--c-btn-bg)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {savingModel ? '...' : t.common.save}
              </button>
            </div>
          </div>
        )}
      </Modal>

      {/* Delete Model Dialog */}
      <ConfirmDialog
        open={!!deleteModelTarget}
        onClose={() => setDeleteModelTarget(null)}
        onConfirm={handleDeleteModel}
        title={tc.deleteModel}
        message={deleteModelTarget ? tc.deleteModelConfirm(deleteModelTarget.model) : ''}
        confirmLabel={t.common.delete}
        loading={deletingModel}
      />

      {/* Import Models Modal */}
      <Modal
        open={showImport}
        onClose={() => { if (!importing) setShowImport(false) }}
        title={tc.importModelsTitle}
      >
        <div className="flex flex-col gap-4">
          {importLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
              <span className="ml-2 text-sm text-[var(--c-text-muted)]">{tc.importModelsLoading}</span>
            </div>
          ) : importError ? (
            <p className="py-4 text-center text-sm text-[var(--c-status-error-text)]">{importError}</p>
          ) : availableModels.length === 0 ? (
            <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">{tc.importModelsEmpty}</p>
          ) : (
            <>
              <div className="relative">
                <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
                <input
                  type="text"
                  value={importSearchQuery}
                  onChange={(e) => setImportSearchQuery(e.target.value)}
                  placeholder={tc.importSearchPlaceholder}
                  className={`${editInputCls} pl-9`}
                />
              </div>
              {filteredImportModels.length === 0 ? (
                <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">{tc.importModelsEmpty}</p>
              ) : (
                <div className="max-h-[400px] space-y-1 overflow-y-auto">
                  {filteredImportModels.map((m: AvailableModel) => (
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
                      <span className="text-sm text-[var(--c-text-primary)]">{m.id}</span>
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
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {t.common.cancel}
            </button>
            <button
              onClick={handleImport}
              disabled={importing || importSelected.size === 0}
              className="rounded-lg bg-[var(--c-btn-bg)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {importing ? '...' : tc.importSelected}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

function TagInput({
  label,
  tags,
  tagInput,
  onTagsChange,
  onTagInputChange,
  addTagPlaceholder,
  inputCls,
}: {
  label: string
  tags: string[]
  tagInput: string
  onTagsChange: (tags: string[]) => void
  onTagInputChange: (v: string) => void
  addTagPlaceholder: string
  inputCls: string
}) {
  const addTag = () => {
    const tag = tagInput.trim()
    if (tag && !tags.includes(tag)) {
      onTagsChange([...tags, tag])
    }
    onTagInputChange('')
  }

  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-xs font-medium text-[var(--c-text-tertiary)]">{label}</label>
      <div className="flex flex-wrap items-center gap-1.5">
        {tags.map((tag) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1 rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[11px] text-[var(--c-text-secondary)]"
          >
            {tag}
            <button
              type="button"
              onClick={() => onTagsChange(tags.filter((t) => t !== tag))}
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
            if (e.key === 'Enter') { e.preventDefault(); addTag() }
            if (e.key === 'Backspace' && !tagInput && tags.length > 0) {
              onTagsChange(tags.slice(0, -1))
            }
          }}
          onBlur={addTag}
          placeholder={addTagPlaceholder}
          className={`${inputCls} min-w-[100px] flex-1`}
        />
      </div>
    </div>
  )
}
