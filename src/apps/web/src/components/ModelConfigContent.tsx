import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Download, Loader2, SlidersHorizontal } from 'lucide-react'
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
import { ModelOptionsModal } from './ModelOptionsModal'
import { SettingsButton, SettingsIconButton } from './settings/_SettingsButton'
import { SettingsInput } from './settings/_SettingsInput'
import { SettingsModalFrame } from './settings/_SettingsModalFrame'
import { SettingsSelect } from './settings/_SettingsSelect'
import { SettingsSwitch } from './settings/_SettingsSwitch'

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
          <SettingsButton
            variant="secondary"
            onClick={() => setShowAddProvider(true)}
            className="w-full"
            icon={<Plus size={14} />}
          >
            {m.addProvider}
          </SettingsButton>
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
    <SettingsModalFrame
      open
      title={m.addProvider}
      onClose={onClose}
      width={510}
      footer={(
        <>
          <SettingsButton size="modal" variant="secondary" onClick={onClose}>
            {m.cancel}
          </SettingsButton>
          <SettingsButton
            size="modal"
            variant="primary"
            onClick={handleSave}
            disabled={saving || !name.trim() || !apiKey.trim()}
            icon={saving ? <Loader2 size={14} className="animate-spin" /> : undefined}
          >
            {m.save}
          </SettingsButton>
        </>
      )}
    >
        <div className="mt-7 space-y-4">
          <FormField label={m.providerName}>
            <SettingsInput variant="md" value={name} onChange={(e) => setName(e.target.value)} placeholder="My Provider" />
          </FormField>

          <FormField label={m.providerVendor}>
            <SettingsSelect
              value={preset}
              onChange={(value) => setPreset(value as ProviderPresetKey)}
              options={PROVIDER_PRESETS.map((p) => ({ value: p.key, label: presetLabel(p.key, m) }))}
              triggerClassName="h-[35px]"
            />
          </FormField>

          <FormField label={m.apiKey}>
            <SettingsInput variant="md" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={m.apiKeyPlaceholder} />
          </FormField>

          <FormField label={m.baseUrl}>
            <SettingsInput variant="md" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder={m.baseUrlPlaceholder} />
          </FormField>
        </div>

        {err && <p className="mt-3 text-xs text-red-400">{err}</p>}
    </SettingsModalFrame>
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
          <SettingsSelect
            value={formPreset}
            onChange={(value) => setFormPreset(value as ProviderPresetKey)}
            options={PROVIDER_PRESETS.map((p) => ({ value: p.key, label: presetLabel(p.key, m) }))}
          />
        </FormField>

        <FormField label={m.providerName}>
          <SettingsInput value={formName} onChange={(e) => setFormName(e.target.value)} />
        </FormField>

        <FormField label={m.apiKey}>
          <SettingsInput
            type="password"
            value={formApiKey}
            onChange={(e) => setFormApiKey(e.target.value)}
            placeholder={provider.key_prefix ? `${provider.key_prefix}${'*'.repeat(40)}` : m.apiKeyPlaceholder}
          />
          {provider.key_prefix && (
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">
              {provider.key_prefix}{'*'.repeat(8)}
            </p>
          )}
        </FormField>

        <FormField label={m.baseUrl}>
          <SettingsInput value={formBaseUrl} onChange={(e) => setFormBaseUrl(e.target.value)} placeholder={m.baseUrlPlaceholder} />
        </FormField>
      </div>

      {err && <p className="text-xs text-red-400">{err}</p>}

      {/* action bar */}
      <div className="flex items-center justify-between border-b border-[var(--c-border-subtle)] pb-4">
        {confirmDelete ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--c-text-tertiary)]">{m.deleteProviderConfirm}</span>
            <SettingsButton
              variant="danger"
              onClick={handleDelete}
              disabled={deleting}
              icon={deleting ? <Loader2 size={12} className="animate-spin" /> : undefined}
            >
              {m.deleteProvider}
            </SettingsButton>
            <SettingsButton variant="secondary" onClick={() => setConfirmDelete(false)}>
              {m.cancel}
            </SettingsButton>
          </div>
        ) : (
          <SettingsIconButton
            label={m.deleteProvider}
            danger
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 size={12} />
          </SettingsIconButton>
        )}
        <SettingsButton
          variant="primary"
          onClick={handleSave}
          disabled={saving || !formName.trim()}
          icon={saving ? <Loader2 size={14} className="animate-spin" /> : undefined}
        >
          {m.save}
        </SettingsButton>
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
  const [availableError, setAvailableError] = useState('')
  const [importing, setImporting] = useState(false)
  const [deletingAll, setDeletingAll] = useState(false)
  const [creatingModel, setCreatingModel] = useState(false)
  const [err, setErr] = useState('')
  const [search, setSearch] = useState('')
  const [editingModel, setEditingModel] = useState<LlmProviderModel | null>(null)

  const loadAvailable = useCallback(async () => {
    setLoadingAvailable(true)
    setAvailableError('')
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
    } catch (e) {
      const message = isApiError(e) ? e.message : m.availableFetchFailed
      setAvailableError(message)
    } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, provider.id, m.availableFetchFailed])

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
      if (toEnable.length > 0) {
        await patchProviderModel(accessToken, provider.id, toEnable[0].id, { show_in_picker: true, is_default: true })
        await Promise.all(
          toEnable.slice(1).map((pm) =>
            patchProviderModel(accessToken, provider.id, pm.id, { show_in_picker: true })
          )
        )
      }
      onChanged()
      void loadAvailable()
    } catch (e) {
      setErr(isApiError(e) ? e.message : m.saveFailed)
    } finally {
      setImporting(false)
    }
  }

  const handleCreateModel = useCallback(async (payload: {
    model: string
    advancedJSON: Record<string, unknown> | null
    tags: string[]
  }) => {
    try {
      await createProviderModel(accessToken, provider.id, {
        model: payload.model,
        advanced_json: payload.advancedJSON ?? undefined,
        tags: payload.tags,
      })
      setCreatingModel(false)
      onChanged()
    } catch (e) {
      throw new Error(isApiError(e) ? e.message : m.saveFailed)
    }
  }, [accessToken, provider.id, m.saveFailed, onChanged])

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
      onChanged()
    } catch (e) {
      throw new Error(isApiError(e) ? e.message : m.saveFailed)
    }
  }, [accessToken, editingModel, m.saveFailed, onChanged, provider.id])

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
            <SettingsButton
              variant="danger"
              onClick={() => void handleDeleteAll()}
              disabled={deletingAll}
              icon={deletingAll ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
            >
              {m.deleteAll}
            </SettingsButton>
          )}
          {loadingAvailable && !available && (
            <Loader2 size={12} className="animate-spin text-[var(--c-text-muted)]" />
          )}
          {unconfiguredCount > 0 && (
            <SettingsButton
              variant="secondary"
              onClick={() => void handleImportAll()}
              disabled={importing}
              icon={<Download size={12} />}
            >
              {importing ? m.importing : `${m.importAll} (${unconfiguredCount})`}
            </SettingsButton>
          )}
          <SettingsButton
            variant="primary"
            onClick={() => setCreatingModel(true)}
          >
            {m.addModel}
          </SettingsButton>
        </div>
      </div>
      {availableError && (
        <p className="mt-1 text-xs text-red-400">{availableError}</p>
      )}

      {err && <p className="mt-2 text-xs text-red-400">{err}</p>}

      {provider.models.length > 0 && (
        <div className="mt-3">
          <SettingsInput
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={m.searchPlaceholder}
          />
        </div>
      )}

      <div className="mt-2 space-y-1 overflow-y-auto" style={{ maxHeight: '320px' }}>
        {provider.models.length === 0 ? (
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
                <SettingsSwitch checked={pm.show_in_picker} onChange={() => void handleTogglePicker(pm.id, pm.show_in_picker)} />
                <SettingsIconButton
                  label={m.modelOptionsTitle}
                  onClick={() => setEditingModel(pm)}
                >
                  <SlidersHorizontal size={14} />
                </SettingsIconButton>
                <SettingsIconButton
                  label={m.deleteModel}
                  danger
                  onClick={() => void handleDeleteModel(pm.id)}
                >
                  <Trash2 size={14} />
                </SettingsIconButton>
              </div>
            </div>
          ))
        )}
      </div>

      <ModelOptionsModal
        open={editingModel !== null}
        model={editingModel}
        availableModels={available}
        labels={{
          modelOptionsTitle: m.modelOptionsTitle,
          modelOptionsFor: m.modelOptionsFor,
          modelCapabilities: m.modelCapabilities,
          vision: m.vision,
          imageOutput: m.imageOutput,
          embedding: m.embedding,
          contextWindow: m.contextWindow,
          maxOutputTokens: m.maxOutputTokens,
          providerOptionsJson: m.providerOptionsJson,
          providerOptionsHint: m.providerOptionsHint,
          save: m.save,
          cancel: m.cancel,
          reset: m.reset,
          invalidJson: m.invalidJson,
          invalidNumber: m.invalidNumber,
          visionBridgeHint: m.visionBridgeHint,
          addModelTitle: m.addModelTitle,
          modelNameLabel: m.modelName,
          modelNamePlaceholder: m.modelNamePlaceholder,
        }}
        onClose={() => setEditingModel(null)}
        onSave={handleSaveModelOptions}
      />

      <ModelOptionsModal
        open={creatingModel}
        mode="create"
        model={null}
        availableModels={available}
        labels={{
          modelOptionsTitle: m.modelOptionsTitle,
          modelOptionsFor: m.modelOptionsFor,
          modelCapabilities: m.modelCapabilities,
          vision: m.vision,
          imageOutput: m.imageOutput,
          embedding: m.embedding,
          contextWindow: m.contextWindow,
          maxOutputTokens: m.maxOutputTokens,
          providerOptionsJson: m.providerOptionsJson,
          providerOptionsHint: m.providerOptionsHint,
          save: m.save,
          cancel: m.cancel,
          reset: m.reset,
          invalidJson: m.invalidJson,
          invalidNumber: m.invalidNumber,
          visionBridgeHint: m.visionBridgeHint,
          addModelTitle: m.addModelTitle,
          modelNameLabel: m.modelName,
          modelNamePlaceholder: m.modelNamePlaceholder,
        }}
        onClose={() => setCreatingModel(false)}
        onSave={handleSaveModelOptions}
        onCreate={handleCreateModel}
      />
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
