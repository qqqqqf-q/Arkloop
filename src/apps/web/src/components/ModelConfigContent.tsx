import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, ChevronRight, Download, X } from 'lucide-react'
import {
  type LlmProvider,
  type AvailableModel,
  listLlmProviders,
  createLlmProvider,
  updateLlmProvider,
  deleteLlmProvider,
  createProviderModel,
  deleteProviderModel,
  listAvailableModels,
  isApiError,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'

const VENDORS = ['openai', 'anthropic', 'gemini', 'deepseek'] as const

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

  if (loading) return <div className="text-sm text-[var(--c-text-tertiary)]">{t.loading}</div>

  return (
    <div className="flex h-full gap-0" style={{ minHeight: 420 }}>
      {/* provider list */}
      <div
        className="flex w-[180px] shrink-0 flex-col gap-1 overflow-y-auto pr-3"
        style={{ borderRight: '0.5px solid var(--c-border-subtle)' }}
      >
        {providers.map((p) => (
          <button
            key={p.id}
            onClick={() => setSelectedId(p.id)}
            className={[
              'flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors',
              selectedId === p.id
                ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)]',
            ].join(' ')}
          >
            <ChevronRight size={12} className={selectedId === p.id ? 'opacity-100' : 'opacity-0'} />
            <span className="truncate">{p.name}</span>
          </button>
        ))}
        <AddProviderButton accessToken={accessToken} onCreated={load} />
        {error && <p className="px-2 text-xs text-red-400">{error}</p>}
      </div>

      {/* detail */}
      <div className="flex-1 overflow-y-auto pl-4">
        {selected ? (
          <ProviderDetail
            key={selected.id}
            provider={selected}
            accessToken={accessToken}
            onUpdated={load}
            onDeleted={load}
          />
        ) : (
          <div className="flex flex-col items-center justify-center gap-2 py-16 text-sm text-[var(--c-text-tertiary)]">
            <p>{m.noProviders}</p>
            <p>{m.noProvidersDesc}</p>
          </div>
        )}
      </div>
    </div>
  )
}

// -- Add Provider Button --

function AddProviderButton({ accessToken, onCreated }: { accessToken: string; onCreated: () => void }) {
  const { t } = useLocale()
  const m = t.models
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [vendor, setVendor] = useState<string>('openai')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const reset = () => { setName(''); setVendor('openai'); setApiKey(''); setBaseUrl(''); setErr(''); setOpen(false) }

  const handleSave = async () => {
    if (!name.trim() || !apiKey.trim()) return
    setSaving(true)
    setErr('')
    try {
      await createLlmProvider(accessToken, {
        name: name.trim(),
        provider: vendor,
        api_key: apiKey.trim(),
        base_url: baseUrl.trim() || undefined,
      })
      reset()
      onCreated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : m.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="mt-1 flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
      >
        <Plus size={13} />
        <span>{m.addProvider}</span>
      </button>
    )
  }

  return (
    <div className="mt-2 flex flex-col gap-2 rounded-lg p-2" style={{ background: 'var(--c-bg-deep)' }}>
      <InputField label={m.providerName} value={name} onChange={setName} placeholder="My OpenAI" />
      <label className="text-xs text-[var(--c-text-tertiary)]">{m.providerVendor}</label>
      <select
        value={vendor}
        onChange={(e) => setVendor(e.target.value)}
        className="h-8 rounded-md px-2 text-sm outline-none"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
      >
        {VENDORS.map((v) => (
          <option key={v} value={v}>{vendorLabel(v, m)}</option>
        ))}
      </select>
      <InputField label={m.apiKey} value={apiKey} onChange={setApiKey} placeholder={m.apiKeyPlaceholder} type="password" />
      <InputField label={m.baseUrl} value={baseUrl} onChange={setBaseUrl} placeholder={m.baseUrlPlaceholder} />
      {err && <p className="text-xs text-red-400">{err}</p>}
      <div className="flex gap-2">
        <button onClick={reset} className="flex-1 rounded-md py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-page)]">{m.cancel}</button>
        <button
          onClick={handleSave}
          disabled={saving || !name.trim() || !apiKey.trim()}
          className="flex-1 rounded-md py-1.5 text-sm transition-colors disabled:opacity-40"
          style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
        >
          {saving ? m.saving : m.save}
        </button>
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
  const [editMode, setEditMode] = useState(false)
  const [name, setName] = useState(provider.name)
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState(provider.base_url ?? '')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const handleSave = async () => {
    setSaving(true)
    setErr('')
    try {
      await updateLlmProvider(accessToken, provider.id, {
        name: name.trim() || undefined,
        api_key: apiKey.trim() || undefined,
        base_url: baseUrl.trim() || null,
      })
      setEditMode(false)
      setApiKey('')
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
    <div className="flex flex-col gap-5">
      {/* header */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-[var(--c-text-heading)]">{provider.name}</h3>
          <span className="text-xs text-[var(--c-text-tertiary)]">
            {vendorLabel(provider.provider, m)}
            {provider.key_prefix && <> &middot; {provider.key_prefix}***</>}
          </span>
        </div>
        <div className="flex gap-1">
          {!editMode && (
            <button
              onClick={() => setEditMode(true)}
              className="rounded-md px-2.5 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              {m.editProvider}
            </button>
          )}
          {confirmDelete ? (
            <div className="flex items-center gap-2">
              <span className="text-xs text-[var(--c-text-tertiary)]">{m.deleteProviderConfirm}</span>
              <button onClick={handleDelete} disabled={deleting} className="rounded-md px-2 py-1 text-xs text-red-400 transition-colors hover:bg-red-400/10">
                {m.deleteProvider}
              </button>
              <button onClick={() => setConfirmDelete(false)} className="rounded-md px-2 py-1 text-xs text-[var(--c-text-secondary)]">{m.cancel}</button>
            </div>
          ) : (
            <button
              onClick={() => setConfirmDelete(true)}
              className="rounded-md p-1 text-[var(--c-text-tertiary)] transition-colors hover:bg-red-400/10 hover:text-red-400"
            >
              <Trash2 size={14} />
            </button>
          )}
        </div>
      </div>

      {/* edit form */}
      {editMode && (
        <div className="flex flex-col gap-3 rounded-lg p-3" style={{ background: 'var(--c-bg-deep)' }}>
          <InputField label={m.providerName} value={name} onChange={setName} />
          <InputField label={m.apiKey} value={apiKey} onChange={setApiKey} placeholder={m.apiKeyPlaceholder} type="password" />
          <InputField label={m.baseUrl} value={baseUrl} onChange={setBaseUrl} placeholder={m.baseUrlPlaceholder} />
          {err && <p className="text-xs text-red-400">{err}</p>}
          <div className="flex gap-2">
            <button onClick={() => { setEditMode(false); setErr('') }} className="flex-1 rounded-md py-1.5 text-sm text-[var(--c-text-secondary)]">{m.cancel}</button>
            <button
              onClick={handleSave}
              disabled={saving}
              className="flex-1 rounded-md py-1.5 text-sm disabled:opacity-40"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              {saving ? m.saving : m.save}
            </button>
          </div>
        </div>
      )}

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
  const [importing, setImporting] = useState(false)
  const [addingModel, setAddingModel] = useState(false)
  const [newModel, setNewModel] = useState('')
  const [err, setErr] = useState('')

  const loadAvailable = useCallback(async () => {
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
    } catch {
      // upstream unavailable, ignore
    }
  }, [accessToken, provider.id])

  useEffect(() => { loadAvailable() }, [loadAvailable])

  const handleImportAll = async () => {
    if (!available) return
    setImporting(true)
    setErr('')
    try {
      const unconfigured = available.filter((am) => !am.configured)
      for (const am of unconfigured) {
        await createProviderModel(accessToken, provider.id, { model: am.id })
      }
      onChanged()
      loadAvailable()
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

  const unconfiguredCount = available?.filter((am) => !am.configured).length ?? 0

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-medium text-[var(--c-text-heading)]">{m.modelsSection}</h4>
        <div className="flex gap-1">
          {unconfiguredCount > 0 && (
            <button
              onClick={handleImportAll}
              disabled={importing}
              className="flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              <Download size={12} />
              {importing ? m.importing : `${m.importAll} (${unconfiguredCount})`}
            </button>
          )}
          <button
            onClick={() => setAddingModel(true)}
            className="flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          >
            <Plus size={12} />
            {m.addModel}
          </button>
        </div>
      </div>

      {addingModel && (
        <div className="flex items-center gap-2">
          <input
            value={newModel}
            onChange={(e) => setNewModel(e.target.value)}
            placeholder={m.modelNamePlaceholder}
            className="h-7 flex-1 rounded-md px-2 text-xs outline-none"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
            onKeyDown={(e) => { if (e.key === 'Enter') handleAddModel(); if (e.key === 'Escape') setAddingModel(false) }}
            autoFocus
          />
          <button onClick={() => setAddingModel(false)} className="text-[var(--c-text-tertiary)]"><X size={14} /></button>
        </div>
      )}

      {err && <p className="text-xs text-red-400">{err}</p>}

      {provider.models.length === 0 && !addingModel ? (
        <p className="text-xs text-[var(--c-text-tertiary)]">{m.noModels}</p>
      ) : (
        <div className="flex flex-col gap-0.5">
          {provider.models.map((pm) => (
            <div key={pm.id} className="group flex items-center justify-between rounded-md px-2 py-1.5 transition-colors hover:bg-[var(--c-bg-deep)]">
              <span className="text-sm text-[var(--c-text-heading)]">{pm.model}</span>
              <button
                onClick={() => handleDeleteModel(pm.id)}
                className="rounded p-0.5 text-[var(--c-text-tertiary)] opacity-0 transition-opacity group-hover:opacity-100 hover:text-red-400"
              >
                <Trash2 size={13} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// -- Shared Helpers --

function InputField({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  type?: string
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-[var(--c-text-tertiary)]">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 rounded-md px-2.5 text-sm outline-none"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
      />
    </div>
  )
}

function vendorLabel(vendor: string, m: { vendorOpenai: string; vendorAnthropic: string; vendorGoogle: string; vendorCustom: string }): string {
  const map: Record<string, string> = {
    openai: m.vendorOpenai,
    anthropic: m.vendorAnthropic,
    gemini: m.vendorGoogle,
    google: m.vendorGoogle,
    deepseek: 'DeepSeek',
  }
  return map[vendor] ?? m.vendorCustom
}
