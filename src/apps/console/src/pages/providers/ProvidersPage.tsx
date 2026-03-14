import { useCallback, useEffect, useMemo, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Copy, Loader2, Plus, Search, Settings, Trash2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { FormField } from '../../components/FormField'
import { Modal } from '../../components/Modal'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Badge } from '../../components/Badge'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
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
} from '../../api/llm-providers'

const INPUT_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
const TEXTAREA_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2 font-mono text-xs leading-relaxed text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
const BUTTON_PRIMARY_CLS =
  'rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50'
const BUTTON_DANGER_CLS =
  'rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-sm text-red-500 transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50'

type ApiFormat = 'openai_chat_completions' | 'openai_responses' | 'anthropic'

type ProviderFormState = {
  name: string
  apiFormat: ApiFormat
  apiKey: string
  baseUrl: string
  advancedJSON: string
}

function splitApiFormat(fmt: ApiFormat): { provider: string; openai_api_mode: string | null } {
  switch (fmt) {
    case 'openai_chat_completions': return { provider: 'openai', openai_api_mode: 'chat_completions' }
    case 'openai_responses': return { provider: 'openai', openai_api_mode: 'responses' }
    case 'anthropic': return { provider: 'anthropic', openai_api_mode: null }
  }
}

function toApiFormat(provider: string, mode: string | null | undefined): ApiFormat {
  if (provider === 'anthropic') return 'anthropic'
  if (mode === 'chat_completions') return 'openai_chat_completions'
  return 'openai_responses'
}

const API_FORMAT_LABELS: Record<ApiFormat, string> = {
  openai_chat_completions: 'OpenAI Chat Completions',
  openai_responses: 'OpenAI Responses',
  anthropic: 'Anthropic',
}

type ModelFormState = {
  model: string
  priority: string
  isDefault: boolean
  tags: string
  whenJSON: string
  advancedJSON: string
  multiplier: string
  costInput: string
  costOutput: string
  costCacheWrite: string
  costCacheRead: string
}

function emptyProviderForm(): ProviderFormState {
  return {
    name: '',
    apiFormat: 'openai_responses',
    apiKey: '',
    baseUrl: '',
    advancedJSON: '{}',
  }
}

function emptyModelForm(): ModelFormState {
  return {
    model: '',
    priority: '0',
    isDefault: false,
    tags: '',
    whenJSON: '{}',
    advancedJSON: '{}',
    multiplier: '1',
    costInput: '',
    costOutput: '',
    costCacheWrite: '',
    costCacheRead: '',
  }
}

function providerToForm(provider: LlmProvider): ProviderFormState {
  return {
    name: provider.name,
    apiFormat: toApiFormat(provider.provider, provider.openai_api_mode),
    apiKey: '',
    baseUrl: provider.base_url ?? '',
    advancedJSON: JSON.stringify(provider.advanced_json ?? {}, null, 2),
  }
}

function modelToForm(model: LlmProviderModel): ModelFormState {
  return {
    model: model.model,
    priority: String(model.priority ?? 0),
    isDefault: model.is_default,
    tags: (model.tags ?? []).join(', '),
    whenJSON: JSON.stringify(model.when ?? {}, null, 2),
    advancedJSON: JSON.stringify(model.advanced_json ?? {}, null, 2),
    multiplier: String(model.multiplier ?? 1),
    costInput: model.cost_per_1k_input != null ? String(model.cost_per_1k_input * 1000) : '',
    costOutput: model.cost_per_1k_output != null ? String(model.cost_per_1k_output * 1000) : '',
    costCacheWrite: model.cost_per_1k_cache_write != null ? String(model.cost_per_1k_cache_write * 1000) : '',
    costCacheRead: model.cost_per_1k_cache_read != null ? String(model.cost_per_1k_cache_read * 1000) : '',
  }
}

function parseObjectJSON(raw: string): Record<string, unknown> {
  const parsed = JSON.parse(raw.trim() || '{}') as unknown
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('json must be object')
  }
  return parsed as Record<string, unknown>
}

function parseTags(raw: string): string[] {
  return raw.split(',').map((item) => item.trim()).filter(Boolean)
}

function parseOptionalNumber(raw: string): number | undefined {
  const trimmed = raw.trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : undefined
}

function selectorText(providerName: string, model: string): string {
  return `${providerName}^${model}`
}

export function ProvidersPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
	const tc = t.pages.credentials

	const [providers, setProviders] = useState<LlmProvider[]>([])
	const [scope, setScope] = useState<LlmProviderScope>('platform')
	const [projectId, setProjectId] = useState('')
	const [selectedId, setSelectedId] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [loading, setLoading] = useState(true)

  const [providerForm, setProviderForm] = useState<ProviderFormState>(emptyProviderForm)
  const [savingProvider, setSavingProvider] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<ProviderFormState>(emptyProviderForm)
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<LlmProvider | null>(null)
  const [deleting, setDeleting] = useState(false)

  const [editingModel, setEditingModel] = useState<LlmProviderModel | null>(null)
  const [modelForm, setModelForm] = useState<ModelFormState>(emptyModelForm)
  const [modelOpen, setModelOpen] = useState(false)
  const [modelError, setModelError] = useState('')
  const [savingModel, setSavingModel] = useState(false)

  const [deleteModelTarget, setDeleteModelTarget] = useState<LlmProviderModel | null>(null)
  const [deletingModel, setDeletingModel] = useState(false)

  const [showImport, setShowImport] = useState(false)
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([])
  const [importSelected, setImportSelected] = useState<Set<string>>(new Set())
  const [importLoading, setImportLoading] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  const [importSearchQuery, setImportSearchQuery] = useState('')

  const load = useCallback(async (keepSelectedId?: string) => {
    setLoading(true)
    try {
		const data = await listLlmProviders(accessToken, scope, projectId || undefined)
      setProviders(data)
      const preferredId = keepSelectedId?.trim() ?? ''
      if (preferredId && data.some((item) => item.id === preferredId)) {
        setSelectedId(preferredId)
      } else if (data.length > 0) {
        setSelectedId(data[0].id)
      } else {
        setSelectedId('')
      }
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
	}, [accessToken, addToast, projectId, scope, tc.toastLoadFailed])

  useEffect(() => {
    void load()
  }, [load])

  const selectedProvider = useMemo(
    () => providers.find((item) => item.id === selectedId) ?? null,
    [providers, selectedId],
  )

  useEffect(() => {
    if (!selectedProvider) return
    setProviderForm(providerToForm(selectedProvider))
  }, [selectedProvider])

  const filteredProviders = useMemo(
    () => providers.filter((item) => item.name.toLowerCase().includes(searchQuery.trim().toLowerCase())),
    [providers, searchQuery],
  )

  const filteredImportModels = useMemo(() => {
    const keyword = importSearchQuery.trim().toLowerCase()
    if (!keyword) return availableModels
    return availableModels.filter((item) =>
      item.id.toLowerCase().includes(keyword) || item.name.toLowerCase().includes(keyword),
    )
  }, [availableModels, importSearchQuery])

  const handleSaveProvider = useCallback(async () => {
    if (!selectedProvider) return
    const name = providerForm.name.trim()
    if (!name) {
      addToast(tc.errRequired, 'error')
      return
    }

    let advancedJSON: Record<string, unknown> | null = null
    try {
      advancedJSON = parseObjectJSON(providerForm.advancedJSON)
    } catch {
      addToast(tc.errInvalidJson('advanced_json'), 'error')
      return
    }

    setSavingProvider(true)
    try {
      const { provider, openai_api_mode } = splitApiFormat(providerForm.apiFormat)
      await updateLlmProvider(selectedProvider.id, {
		scope,
		project_id: projectId || undefined,
        name,
        provider,
        api_key: providerForm.apiKey.trim() || undefined,
        base_url: providerForm.baseUrl.trim() || null,
        openai_api_mode,
        advanced_json: advancedJSON,
      }, accessToken)
      await load(selectedProvider.id)
      addToast(tc.toastCredUpdated, 'success')
      setProviderForm((prev) => ({ ...prev, apiKey: '' }))
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastCredUpdateFailed, 'error')
    } finally {
      setSavingProvider(false)
    }
  }, [accessToken, addToast, load, projectId, providerForm, scope, selectedProvider, tc])

  const handleCreateProvider = useCallback(async () => {
    const name = createForm.name.trim()
    const apiKey = createForm.apiKey.trim()
    if (!name || !apiKey) {
      setCreateError(tc.errRequired)
      return
    }

    let advancedJSON: Record<string, unknown> | null = null
    try {
      advancedJSON = parseObjectJSON(createForm.advancedJSON)
    } catch {
      setCreateError(tc.errInvalidJson('advanced_json'))
      return
    }

    const { provider, openai_api_mode } = splitApiFormat(createForm.apiFormat)

    setCreating(true)
    try {
      const created = await createLlmProvider({
		scope,
		project_id: projectId || undefined,
        name,
        provider,
        api_key: apiKey,
        base_url: createForm.baseUrl.trim() || undefined,
        openai_api_mode: openai_api_mode ?? undefined,
        advanced_json: advancedJSON,
      }, accessToken)
      setCreateOpen(false)
      setCreateForm(emptyProviderForm())
      setCreateError('')
      await load(created.id)
      addToast(tc.toastCreated, 'success')
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastLoadFailed)
    } finally {
      setCreating(false)
    }
  }, [accessToken, addToast, createForm, load, projectId, scope, tc])

  const handleDeleteProvider = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
		await deleteLlmProvider(deleteTarget.id, scope, accessToken, projectId || undefined)
      setDeleteTarget(null)
      await load(selectedId === deleteTarget.id ? '' : selectedId)
      addToast(tc.toastDeleted, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [accessToken, addToast, deleteTarget, load, projectId, scope, selectedId, tc])

  const openCreateModel = useCallback(() => {
    setEditingModel(null)
    setModelForm(emptyModelForm())
    setModelError('')
    setModelOpen(true)
  }, [])

  const openEditModel = useCallback((model: LlmProviderModel) => {
    setEditingModel(model)
    setModelForm(modelToForm(model))
    setModelError('')
    setModelOpen(true)
  }, [])

  const handleSaveModel = useCallback(async () => {
    if (!selectedProvider) return
    const modelName = modelForm.model.trim()
    if (!modelName) {
      setModelError(tc.errRequired)
      return
    }

    let whenJSON: Record<string, unknown>
    let advancedJSON: Record<string, unknown>
    try {
      whenJSON = parseObjectJSON(modelForm.whenJSON)
    } catch {
      setModelError(tc.errInvalidJson(modelName))
      return
    }
    try {
      advancedJSON = parseObjectJSON(modelForm.advancedJSON)
    } catch {
      setModelError(tc.errInvalidAdvancedJson)
      return
    }

    const payload = {
      model: modelName,
      priority: Number(modelForm.priority.trim() || '0'),
      is_default: modelForm.isDefault,
      tags: parseTags(modelForm.tags),
      when: whenJSON,
      advanced_json: advancedJSON,
      multiplier: parseOptionalNumber(modelForm.multiplier),
      cost_per_1k_input: parseOptionalNumber(modelForm.costInput) != null ? parseOptionalNumber(modelForm.costInput)! / 1000 : undefined,
      cost_per_1k_output: parseOptionalNumber(modelForm.costOutput) != null ? parseOptionalNumber(modelForm.costOutput)! / 1000 : undefined,
      cost_per_1k_cache_write: parseOptionalNumber(modelForm.costCacheWrite) != null ? parseOptionalNumber(modelForm.costCacheWrite)! / 1000 : undefined,
      cost_per_1k_cache_read: parseOptionalNumber(modelForm.costCacheRead) != null ? parseOptionalNumber(modelForm.costCacheRead)! / 1000 : undefined,
    }

    setSavingModel(true)
    try {
      if (editingModel) {
		await updateProviderModel(selectedProvider.id, editingModel.id, { ...payload, scope, project_id: projectId || undefined }, accessToken)
        addToast(tc.toastRouteUpdated, 'success')
      } else {
		await createProviderModel(selectedProvider.id, { ...payload, scope, project_id: projectId || undefined }, accessToken)
        addToast(tc.toastRouteCreated, 'success')
      }
      setModelOpen(false)
      setEditingModel(null)
      setModelError('')
      await load(selectedProvider.id)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastRouteUpdateFailed, 'error')
    } finally {
      setSavingModel(false)
    }
  }, [accessToken, addToast, editingModel, load, modelForm, projectId, scope, selectedProvider, tc])

  const handleDeleteModel = useCallback(async () => {
    if (!selectedProvider || !deleteModelTarget) return
    setDeletingModel(true)
    try {
		await deleteProviderModel(selectedProvider.id, deleteModelTarget.id, scope, accessToken, projectId || undefined)
      setDeleteModelTarget(null)
      await load(selectedProvider.id)
      addToast(tc.toastDeletedRoute, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastRouteUpdateFailed, 'error')
    } finally {
      setDeletingModel(false)
    }
  }, [accessToken, addToast, deleteModelTarget, load, projectId, scope, selectedProvider, tc])

  const openImport = useCallback(async () => {
    if (!selectedProvider) return
    setShowImport(true)
    setImportLoading(true)
    setImporting(false)
    setImportError('')
    setImportSearchQuery('')
    setImportSelected(new Set())
    try {
		const data = await listAvailableModels(selectedProvider.id, scope, accessToken, projectId || undefined)
      setAvailableModels(data.models.filter((item) => !item.configured))
    } catch (err) {
      setAvailableModels([])
      setImportError(isApiError(err) ? err.message : tc.importModelsError)
    } finally {
      setImportLoading(false)
    }
  }, [accessToken, projectId, scope, selectedProvider, tc.importModelsError])

  const toggleImportModel = useCallback((modelID: string) => {
    setImportSelected((prev) => {
      const next = new Set(prev)
      if (next.has(modelID)) next.delete(modelID)
      else next.add(modelID)
      return next
    })
  }, [])

  const handleImport = useCallback(async () => {
    if (!selectedProvider || importing || importSelected.size === 0) return
    setImporting(true)
    try {
		for (const modelID of importSelected) {
			await createProviderModel(selectedProvider.id, {
				scope,
				project_id: projectId || undefined,
				model: modelID,
          priority: 1,
          is_default: false,
        }, accessToken)
      }
      setShowImport(false)
      await load(selectedProvider.id)
      addToast(tc.toastRouteCreated, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.importModelsError, 'error')
    } finally {
      setImporting(false)
    }
  }, [accessToken, addToast, importSelected, importing, load, projectId, scope, selectedProvider, tc])

  const copySelector = useCallback(async (providerName: string, modelName: string) => {
    try {
      await navigator.clipboard.writeText(selectorText(providerName, modelName))
      addToast(tc.toastCopied, 'success')
    } catch {
      addToast(tc.toastCopyFailed, 'error')
    }
  }, [addToast, tc.toastCopied, tc.toastCopyFailed])

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={(
          <div className="flex items-center justify-end gap-2 whitespace-nowrap">
            <label className="shrink-0 text-xs text-[var(--c-text-muted)]">{tc.fieldScope}</label>
            <select
              value={scope}
              onChange={(e) => setScope(e.target.value as LlmProviderScope)}
              className={`${INPUT_CLS} w-[112px] py-1 text-xs`}
            >
              <option value="platform">{tc.scopePlatform}</option>
              <option value="project">{tc.scopeAccount}</option>
            </select>
            {scope === 'project' && (
              <input
                value={projectId}
                onChange={(e) => setProjectId(e.target.value)}
                placeholder="Account ID"
                className={`${INPUT_CLS} w-[160px] py-1 text-xs`}
              />
            )}
            <button
              onClick={() => {
                setCreateForm(emptyProviderForm())
                setCreateError('')
                setCreateOpen(true)
              }}
              className="flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <Plus size={13} />
              {tc.addCredential}
            </button>
          </div>
        )}
      />

      <div className="flex flex-1 overflow-hidden">
        <aside className="flex w-[300px] shrink-0 flex-col border-r border-[var(--c-border)] bg-[var(--c-bg-panel)]">
          <div className="border-b border-[var(--c-border)] p-4">
            <div className="relative">
              <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
              <input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder={tc.searchPlaceholder}
                className={`${INPUT_CLS} pl-9`}
              />
            </div>
          </div>
          <div className="flex-1 overflow-y-auto p-2">
            {loading && providers.length === 0 ? (
              <div className="px-3 py-4 text-sm text-[var(--c-text-muted)]">{t.loading}</div>
            ) : filteredProviders.length === 0 ? (
              <div className="px-3 py-4 text-sm text-[var(--c-text-muted)]">{tc.empty}</div>
            ) : (
              <div className="space-y-1">
                {filteredProviders.map((provider) => {
                  const active = provider.id === selectedId
                  return (
                    <button
                      key={provider.id}
                      onClick={() => setSelectedId(provider.id)}
                      className={`w-full rounded-lg border px-3 py-2 text-left transition-colors ${active
                        ? 'border-[var(--c-border-focus)] bg-[var(--c-bg-sub)]'
                        : 'border-transparent hover:bg-[var(--c-bg-sub)]'
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium text-[var(--c-text-primary)]">{provider.name}</div>
                          <div className="truncate text-xs text-[var(--c-text-muted)]">{API_FORMAT_LABELS[toApiFormat(provider.provider, provider.openai_api_mode)]}</div>
                        </div>
                        <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] text-[var(--c-text-muted)]">
                          {provider.models.length}
                        </span>
                      </div>
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        </aside>

        <main className="flex-1 overflow-y-auto p-6">
          {!selectedProvider ? (
            <div className="flex h-full items-center justify-center text-sm text-[var(--c-text-muted)]">{tc.empty}</div>
          ) : (
            <div className="mx-auto flex max-w-[980px] flex-col gap-6">
              <section className="rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-card)] p-5">
                <div className="mb-4 flex items-center justify-between gap-3">
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.editCredTitle}</h3>
                  <div className="flex shrink-0 items-center gap-2">
                    <button
                      onClick={() => setDeleteTarget(selectedProvider)}
                      className={BUTTON_DANGER_CLS}
                    >
                      <Trash2 size={14} />
                    </button>
                    <button
                      onClick={() => void handleSaveProvider()}
                      disabled={savingProvider}
                      className={BUTTON_PRIMARY_CLS}
                    >
                      {savingProvider ? '...' : tc.editCredSave}
                    </button>
                  </div>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <FormField label={tc.fieldName}>
                    <input
                      className={INPUT_CLS}
                      value={providerForm.name}
                      onChange={(e) => setProviderForm((prev) => ({ ...prev, name: e.target.value }))}
                    />
                  </FormField>
                  <FormField label={tc.fieldProvider}>
                    <select
                      className={INPUT_CLS}
                      value={providerForm.apiFormat}
                      onChange={(e) => setProviderForm((prev) => ({ ...prev, apiFormat: e.target.value as ApiFormat }))}
                    >
                      <option value="openai_chat_completions">OpenAI Chat Completions</option>
                      <option value="openai_responses">OpenAI Responses</option>
                      <option value="anthropic">Anthropic</option>
                    </select>
                  </FormField>
                  <FormField label={tc.fieldBaseUrl}>
                    <input
                      className={INPUT_CLS}
                      value={providerForm.baseUrl}
                      onChange={(e) => setProviderForm((prev) => ({ ...prev, baseUrl: e.target.value }))}
                    />
                  </FormField>
                </div>

                <div className="mt-4 grid gap-4">
                  <FormField label={tc.fieldApiKeyOptional}>
                    <input
                      className={INPUT_CLS}
                      value={providerForm.apiKey}
                      onChange={(e) => setProviderForm((prev) => ({ ...prev, apiKey: e.target.value }))}
                    />
                  </FormField>
                  <FormField label={tc.fieldAdvancedJson}>
                    <textarea
                      className={TEXTAREA_CLS}
                      rows={8}
                      value={providerForm.advancedJSON}
                      onChange={(e) => setProviderForm((prev) => ({ ...prev, advancedJSON: e.target.value }))}
                    />
                  </FormField>
                </div>
              </section>

              <section className="rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-card)] p-5">
                <div className="mb-4 flex items-center justify-between gap-3">
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.fieldRoutes}</h3>
                  <div className="flex shrink-0 items-center gap-2">
                    <button onClick={() => void openImport()} className={BUTTON_PRIMARY_CLS}>
                      {tc.importModels}
                    </button>
                    <button onClick={openCreateModel} className={BUTTON_PRIMARY_CLS}>
                      {tc.addRoute}
                    </button>
                  </div>
                </div>
                {selectedProvider.models.length === 0 ? (
                  <div className="text-sm text-[var(--c-text-muted)]">{tc.emptyRoutes}</div>
                ) : (
                  <div className="space-y-3">
                    {selectedProvider.models.map((model) => (
                      <div key={model.id} className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-panel)] p-4">
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <div className="truncate font-mono text-sm text-[var(--c-text-primary)]">{model.model}</div>
                              {model.is_default && <Badge variant="success">{tc.routeDefault}</Badge>}
                              <span className="text-xs text-[var(--c-text-muted)]">priority {model.priority}</span>
                            </div>
                            <button
                              onClick={() => void copySelector(selectedProvider.name, model.model)}
                              className="mt-2 flex items-center gap-1 text-xs text-[var(--c-text-muted)] hover:text-[var(--c-text-primary)]"
                            >
                              <Copy size={12} />
                              {selectorText(selectedProvider.name, model.model)}
                            </button>
                            <div className="mt-2 grid gap-1 text-xs text-[var(--c-text-secondary)] md:grid-cols-2">
                              <div>{tc.routeWhen}: {JSON.stringify(model.when ?? {})}</div>
                              <div>{tc.routeAdvancedJson}: {model.advanced_json && Object.keys(model.advanced_json).length > 0 ? tc.routeAdvancedConfigured : '--'}</div>
                              <div>{tc.routeMultiplier}: {model.multiplier ?? 1}</div>
                              <div>{tc.routeCostInput}: {model.cost_per_1k_input != null ? (model.cost_per_1k_input * 1000) : '--'}</div>
                              <div>{tc.routeCostOutput}: {model.cost_per_1k_output != null ? (model.cost_per_1k_output * 1000) : '--'}</div>
                              <div>{tc.routeCostCacheWrite}: {model.cost_per_1k_cache_write != null ? (model.cost_per_1k_cache_write * 1000) : '--'}</div>
                              <div>{tc.routeCostCacheRead}: {model.cost_per_1k_cache_read != null ? (model.cost_per_1k_cache_read * 1000) : '--'}</div>
                            </div>
                          </div>
                          <div className="flex shrink-0 items-center gap-2">
                            <button onClick={() => openEditModel(model)} className={BUTTON_PRIMARY_CLS}>
                              <Settings size={14} />
                            </button>
                            <button onClick={() => setDeleteModelTarget(model)} className={BUTTON_DANGER_CLS}>
                              <Trash2 size={14} />
                            </button>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            </div>
          )}
        </main>
      </div>

      <Modal open={createOpen} onClose={() => !creating && setCreateOpen(false)} title={tc.modalTitle} width="640px">
        <div className="flex flex-col gap-4">
          <div className="grid gap-4 md:grid-cols-2">
            <FormField label={tc.fieldName}>
              <input className={INPUT_CLS} value={createForm.name} onChange={(e) => { setCreateForm((prev) => ({ ...prev, name: e.target.value })); setCreateError('') }} />
            </FormField>
            <FormField label={tc.fieldProvider}>
              <select className={INPUT_CLS} value={createForm.apiFormat} onChange={(e) => { setCreateForm((prev) => ({ ...prev, apiFormat: e.target.value as ApiFormat })); setCreateError('') }}>
                <option value="openai_chat_completions">OpenAI Chat Completions</option>
                <option value="openai_responses">OpenAI Responses</option>
                <option value="anthropic">Anthropic</option>
              </select>
            </FormField>
            <FormField label={tc.fieldApiKey}>
              <input className={INPUT_CLS} value={createForm.apiKey} onChange={(e) => { setCreateForm((prev) => ({ ...prev, apiKey: e.target.value })); setCreateError('') }} />
            </FormField>
            <FormField label={tc.fieldBaseUrl}>
              <input className={INPUT_CLS} value={createForm.baseUrl} onChange={(e) => { setCreateForm((prev) => ({ ...prev, baseUrl: e.target.value })); setCreateError('') }} />
            </FormField>
          </div>
          <FormField label={tc.fieldAdvancedJson}>
            <textarea className={TEXTAREA_CLS} rows={8} value={createForm.advancedJSON} onChange={(e) => { setCreateForm((prev) => ({ ...prev, advancedJSON: e.target.value })); setCreateError('') }} />
          </FormField>
          {createError ? <div className="text-sm text-red-500">{createError}</div> : null}
          <div className="flex justify-end gap-2">
            <button onClick={() => setCreateOpen(false)} className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">{tc.cancel}</button>
            <button onClick={() => void handleCreateProvider()} disabled={creating} className={BUTTON_PRIMARY_CLS}>{creating ? '...' : tc.create}</button>
          </div>
        </div>
      </Modal>

      <Modal open={modelOpen} onClose={() => !savingModel && setModelOpen(false)} title={editingModel ? tc.editRoutesTitle : tc.addRoute} width="760px">
        <div className="flex flex-col gap-4">
          <div className="grid gap-4 md:grid-cols-2">
            <FormField label={tc.routeModel}>
              <input className={INPUT_CLS} value={modelForm.model} onChange={(e) => { setModelForm((prev) => ({ ...prev, model: e.target.value })); setModelError('') }} />
            </FormField>
            <FormField label={tc.routePriority}>
              <input type="number" className={INPUT_CLS} value={modelForm.priority} onChange={(e) => { setModelForm((prev) => ({ ...prev, priority: e.target.value })); setModelError('') }} />
            </FormField>
            <FormField label={tc.routeMultiplier}>
              <input type="number" step="0.01" min="0" className={INPUT_CLS} value={modelForm.multiplier} onChange={(e) => { setModelForm((prev) => ({ ...prev, multiplier: e.target.value })); setModelError('') }} />
            </FormField>
            <FormField label={tc.fieldTags}>
              <input className={INPUT_CLS} value={modelForm.tags} onChange={(e) => { setModelForm((prev) => ({ ...prev, tags: e.target.value })); setModelError('') }} />
            </FormField>
          </div>
          <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input type="checkbox" checked={modelForm.isDefault} onChange={(e) => setModelForm((prev) => ({ ...prev, isDefault: e.target.checked }))} className="rounded" />
            {tc.routeDefault}
          </label>
          <FormField label={tc.routeWhen}>
            <textarea className={TEXTAREA_CLS} rows={5} value={modelForm.whenJSON} onChange={(e) => { setModelForm((prev) => ({ ...prev, whenJSON: e.target.value })); setModelError('') }} />
          </FormField>
          <FormField label={tc.routeAdvancedJson}>
            <textarea className={TEXTAREA_CLS} rows={6} value={modelForm.advancedJSON} onChange={(e) => { setModelForm((prev) => ({ ...prev, advancedJSON: e.target.value })); setModelError('') }} />
          </FormField>
          <div className="grid gap-4 md:grid-cols-2">
            <FormField label={tc.routeCostInput}>
              <input type="number" step="0.0001" min="0" className={INPUT_CLS} value={modelForm.costInput} onChange={(e) => setModelForm((prev) => ({ ...prev, costInput: e.target.value }))} />
            </FormField>
            <FormField label={tc.routeCostOutput}>
              <input type="number" step="0.0001" min="0" className={INPUT_CLS} value={modelForm.costOutput} onChange={(e) => setModelForm((prev) => ({ ...prev, costOutput: e.target.value }))} />
            </FormField>
            <FormField label={tc.routeCostCacheWrite}>
              <input type="number" step="0.0001" min="0" className={INPUT_CLS} value={modelForm.costCacheWrite} onChange={(e) => setModelForm((prev) => ({ ...prev, costCacheWrite: e.target.value }))} />
            </FormField>
            <FormField label={tc.routeCostCacheRead}>
              <input type="number" step="0.0001" min="0" className={INPUT_CLS} value={modelForm.costCacheRead} onChange={(e) => setModelForm((prev) => ({ ...prev, costCacheRead: e.target.value }))} />
            </FormField>
          </div>
          {modelError ? <div className="text-sm text-red-500">{modelError}</div> : null}
          <div className="flex justify-end gap-2">
            <button onClick={() => setModelOpen(false)} className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]">{tc.cancel}</button>
            <button onClick={() => void handleSaveModel()} disabled={savingModel} className={BUTTON_PRIMARY_CLS}>{savingModel ? '...' : tc.editRoutesSave}</button>
          </div>
        </div>
      </Modal>

      <Modal open={showImport} onClose={() => { if (!importing) setShowImport(false) }} title={tc.importModelsTitle} width="640px">
        <div className="flex flex-col gap-4">
          {importLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
              <span className="ml-2 text-sm text-[var(--c-text-muted)]">{tc.importModelsLoading}</span>
            </div>
          ) : importError ? (
            <p className="py-4 text-center text-sm text-red-500">{importError}</p>
          ) : availableModels.length === 0 ? (
            <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">{tc.importModelsEmpty}</p>
          ) : (
            <>
              <div className="relative">
                <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
                <input
                  value={importSearchQuery}
                  onChange={(e) => setImportSearchQuery(e.target.value)}
                  placeholder={tc.importSearchPlaceholder}
                  className={`${INPUT_CLS} pl-9`}
                />
              </div>
              {filteredImportModels.length === 0 ? (
                <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">{tc.importModelsEmpty}</p>
              ) : (
                <div className="max-h-[400px] space-y-1 overflow-y-auto">
                  {filteredImportModels.map((model) => (
                    <label key={model.id} className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-[var(--c-bg-sub)]">
                      <input
                        type="checkbox"
                        checked={importSelected.has(model.id)}
                        onChange={() => toggleImportModel(model.id)}
                        className="rounded"
                      />
                      <span className="text-sm text-[var(--c-text-primary)]">{model.id}</span>
                    </label>
                  ))}
                </div>
              )}
            </>
          )}
          <div className="flex justify-end gap-2">
            <button onClick={() => setShowImport(false)} disabled={importing} className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50">{tc.cancel}</button>
            <button onClick={() => void handleImport()} disabled={importing || importSelected.size === 0} className={BUTTON_PRIMARY_CLS}>
              {importing ? '...' : tc.importSelected}
            </button>
          </div>
        </div>
      </Modal>

      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDeleteProvider}
        title={tc.deleteTitle}
        message={deleteTarget ? tc.deleteMessage(deleteTarget.name) : ''}
        confirmLabel={tc.deleteConfirm}
        loading={deleting}
      />

      <ConfirmDialog
        open={!!deleteModelTarget}
        onClose={() => setDeleteModelTarget(null)}
        onConfirm={handleDeleteModel}
        title={tc.deleteRouteTitle}
        message={deleteModelTarget ? tc.deleteRouteMessage(deleteModelTarget.model) : ''}
        confirmLabel={tc.deleteRouteConfirm}
        loading={deletingModel}
      />
    </div>
  )
}
