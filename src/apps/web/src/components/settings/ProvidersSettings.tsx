import { useState, useCallback, useEffect, useMemo, useLayoutEffect, useRef, memo, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import {
  Plus,
  Trash2,
  Download,
  X,
  Loader2,
  Zap,
  SlidersHorizontal,
  Search,
  Pencil,
  Copy as CopyIcon,
} from 'lucide-react'
import {
  type LlmProvider,
  type LlmProviderModel,
  type AvailableModel,
  listLlmProviders,
  createLlmProvider,
  updateLlmProvider,
  deleteLlmProvider,
  copyLlmProvider,
  createProviderModel,
  deleteProviderModel,
  patchProviderModel,
  listAvailableModels,
  testLlmProviderModel,
  isApiError,
} from '../../api'
import { routeAdvancedJsonFromAvailableCatalog } from '@arkloop/shared/llm/available-catalog-advanced-json'
import { ConfirmDialog } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { ModelOptionsModal } from '../ModelOptionsModal'
import { AnimatedCheck } from '../AnimatedCheck'
import { SettingsButton, SettingsIconButton } from './_SettingsButton'
import { SettingsInput, SettingsSearchInput } from './_SettingsInput'
import { SettingsModalFrame } from './_SettingsModalFrame'
import { SettingsSelect } from './_SettingsSelect'
import { SettingsSegmentedControl } from './_SettingsSegmentedControl'
import { SettingsSwitch } from './_SettingsSwitch'
import {
  AdvancedOptionsDisclosure,
  HeadersEditor,
  advancedJSONSignature,
  readHeaderEntriesFromAdvancedJSON,
  writeHeaderEntriesToAdvancedJSON,
  type HeaderEntry,
} from './HeadersEditor'

const VENDOR_PRESETS = [
  { key: 'openai_responses', provider: 'openai', openai_api_mode: 'responses' },
  { key: 'openai_chat_completions', provider: 'openai', openai_api_mode: 'chat_completions' },
  { key: 'anthropic_message', provider: 'anthropic', openai_api_mode: undefined },
  { key: 'gemini', provider: 'gemini', openai_api_mode: undefined },
] as const

type VendorPresetKey = (typeof VENDOR_PRESETS)[number]['key']

const OPENVIKING_BACKEND_ADVANCED_KEY = 'openviking_backend'

type OpenVikingBackendKey = 'openai' | 'azure' | 'volcengine' | 'openai_compatible'

function isManagedLocalProvider(provider: LlmProvider): boolean {
  return provider.source === 'local' || provider.read_only === true
}

function localAuthModeLabel(provider: LlmProvider, p: ReturnType<typeof useLocale>['t']['adminProviders']): string | null {
  if (provider.auth_mode === 'api_key') return p.apiKey
  if (provider.auth_mode === 'oauth') return p.authModeOAuth
  return null
}

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

type ProviderActionError = {
  message: string
  code?: string
  traceId?: string
  details?: unknown
}

class AvailableModelsLoadError extends Error {
  readonly displayError: ProviderActionError

  constructor(displayError: ProviderActionError) {
    super(displayError.message)
    this.name = 'AvailableModelsLoadError'
    this.displayError = displayError
  }
}

function providerActionErrorFromUnknown(error: unknown, fallback: string): ProviderActionError {
  if (isApiError(error)) {
    return {
      message: error.message || fallback,
      code: error.code,
      traceId: error.traceId,
      details: error.details,
    }
  }
  if (error instanceof Error) {
    return { message: error.message || fallback }
  }
  return { message: fallback }
}

function formatProviderDetail(value: unknown): string {
  if (value == null) return String(value)
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value)
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function formatProviderActionError(error: ProviderActionError): string {
  const lines = [error.message]
  if (error.code) lines.push(`code: ${error.code}`)
  if (error.traceId) lines.push(`trace_id: ${error.traceId}`)
  if (error.details && typeof error.details === 'object') {
    for (const [key, value] of Object.entries(error.details)) {
      lines.push(`${key}: ${formatProviderDetail(value)}`)
    }
  }
  return lines.join('\n')
}

function isAvailableModelsLoadError(error: unknown): error is AvailableModelsLoadError {
  return error instanceof AvailableModelsLoadError
}

function VendorDropdown({
  value,
  onChange,
  p,
  triggerClassName,
}: {
  value: VendorPresetKey
  onChange: (v: VendorPresetKey) => void
  p: ReturnType<typeof useLocale>['t']['adminProviders']
  triggerClassName?: string
}) {
  return (
    <SettingsSelect
      value={value}
      options={VENDOR_PRESETS.map((preset) => ({
        value: preset.key,
        label: vendorLabel(preset.key, p),
      }))}
      onChange={(next) => onChange(next as VendorPresetKey)}
      triggerClassName={triggerClassName}
    />
  )
}

type Props = { accessToken: string }
type ProviderFilter = 'all' | 'enabled' | 'local' | 'cloud'

let cachedProviders: LlmProvider[] | null = null

export function ProvidersSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const p = t.adminProviders

  const [providers, setProviders] = useState<LlmProvider[]>(() => cachedProviders ?? [])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<ProviderFilter>('all')
  const [loading, setLoading] = useState(() => cachedProviders === null)
  const [error, setError] = useState('')
  const [showAddProvider, setShowAddProvider] = useState(false)
  const [copyingProviderId, setCopyingProviderId] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<LlmProvider | null>(null)
  const [deletingProviderId, setDeletingProviderId] = useState<string | null>(null)
  const [autoImportProviderId, setAutoImportProviderId] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const list = await listLlmProviders(accessToken)
      cachedProviders = list
      setProviders(list)
      setSelectedId((prev) => list.find((pv) => pv.id === prev) ? prev : null)
    } catch {
      setError(p.loadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, p.loadFailed])

  useEffect(() => { void load() }, [load])

  const selected = providers.find((pv) => pv.id === selectedId) ?? null
  const filterItems: { key: ProviderFilter; label: string }[] = [
    { key: 'all', label: p.filterAll ?? 'All' },
    { key: 'enabled', label: p.filterEnabled ?? 'Enabled' },
    { key: 'local', label: p.filterLocal ?? p.localProvider },
    { key: 'cloud', label: p.filterCloud ?? 'Cloud' },
  ]
  const filteredProviders = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    return providers.filter((provider) => {
      if (filter === 'enabled' && !provider.models.some((model) => model.show_in_picker)) return false
      if (filter === 'local' && !isManagedLocalProvider(provider)) return false
      if (filter === 'cloud' && isManagedLocalProvider(provider)) return false
      if (!normalized) return true
      return [
        provider.name,
        provider.base_url ?? '',
        provider.provider,
        provider.openai_api_mode ?? '',
      ].join(' ').toLowerCase().includes(normalized)
    })
  }, [filter, providers, query])

  const handleCopyProvider = useCallback(async (provider: LlmProvider) => {
    if (isManagedLocalProvider(provider)) return
    setError('')
    setCopyingProviderId(provider.id)
    try {
      await copyLlmProvider(accessToken, provider.id)
      await load()
    } catch {
      setError(p.saveFailed)
    } finally {
      setCopyingProviderId(null)
    }
  }, [accessToken, load, p.saveFailed])

  const handleDeleteProvider = useCallback(async () => {
    if (!deleteTarget || isManagedLocalProvider(deleteTarget)) return
    setError('')
    setDeletingProviderId(deleteTarget.id)
    try {
      await deleteLlmProvider(accessToken, deleteTarget.id)
      setSelectedId((prev) => prev === deleteTarget.id ? null : prev)
      setDeleteTarget(null)
      await load()
    } catch {
      setError(p.deleteFailed)
    } finally {
      setDeletingProviderId(null)
    }
  }, [accessToken, deleteTarget, load, p.deleteFailed])

  const handleProviderCreated = useCallback(async (provider: LlmProvider) => {
    setShowAddProvider(false)
    setSelectedId(provider.id)
    setAutoImportProviderId(provider.id)
    await load()
  }, [load])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-6 px-1 pb-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-[24px] font-semibold leading-tight text-[var(--c-text-heading)]">{p.providersPageTitle ?? p.title}</h2>
          <p className="mt-2 text-[13px] text-[var(--c-text-muted)]">{p.providersPageDesc ?? ''}</p>
        </div>
        <SettingsButton
          variant="primary"
          onClick={() => setShowAddProvider(true)}
          icon={<Plus size={14} />}
        >
          {p.addProvider}
        </SettingsButton>
      </div>

      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[220px] flex-1">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-tertiary)]" />
            <SettingsInput
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={p.searchProviders}
              className="pl-9"
            />
          </div>
          <SettingsSegmentedControl<ProviderFilter>
            options={filterItems.map((item) => ({ value: item.key, label: item.label }))}
            value={filter}
            onChange={setFilter}
          />
        </div>
        {error && <p className="text-xs text-[var(--c-status-error-text)]">{error}</p>}
      </div>

      {filteredProviders.length > 0 ? (
        <div className="grid gap-3 sm:grid-cols-2">
          {filteredProviders.map((provider) => (
            <ProviderSummaryCard
              key={provider.id}
              provider={provider}
              accessToken={accessToken}
              p={p}
              onOpen={() => setSelectedId(provider.id)}
              onCopy={() => void handleCopyProvider(provider)}
              onDelete={() => setDeleteTarget(provider)}
              copying={copyingProviderId === provider.id}
              deleting={deletingProviderId === provider.id}
            />
          ))}
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-12 text-center">
          <p className="text-sm font-medium text-[var(--c-text-primary)]">{p.noProviders}</p>
          <p className="mt-1 text-xs text-[var(--c-text-muted)]">{p.noProvidersDesc}</p>
        </div>
      )}

      {selected && (
        <SettingsModalFrame
          open
          title={selected.name}
          onClose={() => setSelectedId(null)}
          width={760}
        >
      <div className="mt-6 max-h-[min(78vh,820px)] overflow-y-auto pr-1">
            <ProviderDetail
              key={selected.id}
              provider={selected}
              accessToken={accessToken}
              onUpdated={load}
              p={p}
              showTitle={false}
              autoImportModels={autoImportProviderId === selected.id}
              onAutoImportStarted={() => setAutoImportProviderId(null)}
            />
          </div>
        </SettingsModalFrame>
      )}

      {showAddProvider && (
        <AddProviderModal
          accessToken={accessToken}
          p={p}
          onClose={() => setShowAddProvider(false)}
          onCreated={(provider) => { void handleProviderCreated(provider) }}
        />
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => {
          if (deletingProviderId === null) setDeleteTarget(null)
        }}
        onConfirm={() => void handleDeleteProvider()}
        title={p.deleteProvider}
        message={p.deleteProviderConfirm}
        confirmLabel={p.deleteProvider}
        cancelLabel={p.cancel}
        loading={deletingProviderId !== null}
      />
    </div>
  )
}

function ProviderSummaryCard({
  provider,
  accessToken,
  p,
  onOpen,
  onCopy,
  onDelete,
  copying,
  deleting,
}: {
  provider: LlmProvider
  accessToken: string
  p: ReturnType<typeof useLocale>['t']['adminProviders']
  onOpen: () => void
  onCopy: () => void
  onDelete: () => void
  copying: boolean
  deleting: boolean
}) {
  const local = isManagedLocalProvider(provider)
  const enabledModels = provider.models.filter((model) => model.show_in_picker).length
  const apiMode = provider.openai_api_mode === 'chat_completions'
    ? p.vendorOpenaiChat
    : vendorLabel(toVendorKey(provider.provider, provider.openai_api_mode), p)
  const baseUrl = provider.base_url?.trim() || '—'

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    onOpen()
  }

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={handleKeyDown}
      className="group relative flex min-h-[138px] cursor-pointer flex-col rounded-xl bg-[var(--c-bg-input)] p-4 text-left outline-none transition-[border-color,box-shadow,background-color] duration-180 hover:[box-shadow:0_0_0_0.35px_var(--c-input-border-color-hover)] focus-visible:[box-shadow:0_0_0_1px_var(--c-input-border-color-hover)]"
      style={{ border: '0.5px solid var(--c-input-border-color)' }}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-[14px] font-semibold leading-tight text-[var(--c-text-primary)]">{provider.name}</h3>
          <p className="mt-1 truncate text-[11px] leading-tight text-[var(--c-text-muted)]">{apiMode}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <span className="rounded-md bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">
            {local ? p.localProvider : (p.filterCloud ?? 'Cloud')}
          </span>
          {provider.read_only && (
            <span className="rounded-md bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">
              {p.readOnlyProvider}
            </span>
          )}
        </div>
      </div>
      <div className="mt-4 min-w-0 space-y-2 pr-[152px]">
        <ProviderCardLine label={p.baseUrl} value={baseUrl} />
        <ProviderCardLine label={p.modelsSection} value={`${provider.models.length} / ${enabledModels}`} />
      </div>
      <div
        className="pointer-events-none absolute bottom-3 right-3 flex items-center gap-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100"
        onClick={(event) => event.stopPropagation()}
      >
        <SettingsIconButton
          label={p.editProvider}
          className="pointer-events-auto h-8 w-8"
          onClick={onOpen}
        >
          <Pencil size={12} />
        </SettingsIconButton>
        {!local && (
          <SettingsIconButton
            label={p.copyProvider ?? 'Copy provider'}
            className="pointer-events-auto h-8 w-8"
            onClick={onCopy}
            disabled={copying}
          >
            {copying ? <Loader2 size={12} className="animate-spin" /> : <CopyIcon size={12} />}
          </SettingsIconButton>
        )}
        <div className="pointer-events-auto">
          <ModelTestButton
            accessToken={accessToken}
            provider={provider}
            label={p.testModel ?? 'Test'}
            searchPlaceholder={p.searchProviders}
            iconOnly
          />
        </div>
        {!local && (
          <SettingsIconButton
            label={p.deleteProvider}
            className="pointer-events-auto h-8 w-8"
            danger
            onClick={onDelete}
            disabled={deleting}
          >
            {deleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
          </SettingsIconButton>
        )}
      </div>
    </div>
  )
}

function ProviderCardLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">{label}</div>
      <div className="mt-0.5 truncate text-[12px] font-medium leading-tight text-[var(--c-text-secondary)]">{value}</div>
    </div>
  )
}

// -- Add Provider Modal --

function AddProviderModal({ accessToken, p, onClose, onCreated }: {
  accessToken: string
  p: ReturnType<typeof useLocale>['t']['adminProviders']
  onClose: () => void
  onCreated: (provider: LlmProvider) => void
}) {
  const [name, setName] = useState('')
  const [preset, setPreset] = useState<VendorPresetKey>('openai_responses')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [headers, setHeaders] = useState<HeaderEntry[]>([])
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const handleSave = async () => {
    if (!name.trim() || !apiKey.trim()) return
    setSaving(true)
    setErr('')
    try {
      const v = VENDOR_PRESETS.find((vv) => vv.key === preset)!
      const provider = await createLlmProvider(accessToken, {
        name: name.trim(),
        provider: v.provider,
        api_key: apiKey.trim(),
        base_url: baseUrl.trim() || undefined,
        openai_api_mode: v.openai_api_mode,
        advanced_json: writeHeaderEntriesToAdvancedJSON(
          mergeProviderAdvancedJSON({}, defaultOpenVikingBackendForVendor(v.provider)),
          headers,
        ),
      })
      onCreated(provider)
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const fieldLabelCls = 'block text-[11px] font-medium text-[var(--c-placeholder)] mb-1 pl-[2px]'

  return (
    <SettingsModalFrame
      open
      title={p.addProvider}
      onClose={onClose}
      width={510}
      footer={(
        <>
          <SettingsButton size="modal" variant="secondary" onClick={onClose}>
            {p.cancel}
          </SettingsButton>
          <SettingsButton
            size="modal"
            variant="primary"
            onClick={() => void handleSave()}
            disabled={saving || !name.trim() || !apiKey.trim()}
            icon={saving ? <Loader2 size={14} className="animate-spin" /> : undefined}
          >
            {saving ? p.saving : p.save}
          </SettingsButton>
        </>
      )}
    >
        <div className="mt-6 grid grid-cols-2 gap-x-4 gap-y-4">
          <div>
            <label className={fieldLabelCls}>{p.providerName}</label>
            <SettingsInput
              variant="md"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My Provider"
            />
          </div>
          <div>
            <label className={fieldLabelCls}>{p.vendor}</label>
            <VendorDropdown value={preset} onChange={setPreset} p={p} triggerClassName="h-[35px]" />
          </div>
          <div className="col-span-2">
            <label className={fieldLabelCls}>{p.apiKey}</label>
            <SettingsInput
              variant="md"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={p.apiKeyPlaceholder}
            />
          </div>
          <div className="col-span-2">
            <label className={fieldLabelCls}>{p.baseUrl}</label>
            <SettingsInput
              variant="md"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value.slice(0, 500))}
              placeholder={p.baseUrlPlaceholder ?? 'https://api.example.com/v1'}
              maxLength={500}
            />
          </div>
          <AdvancedOptionsDisclosure
            label={p.advancedOptions ?? p.advancedConfig}
            open={advancedOpen}
            onToggle={() => setAdvancedOpen((value) => !value)}
          >
            <div>
              <label className={fieldLabelCls}>{p.headers ?? 'Headers'}</label>
              <HeadersEditor
                headers={headers}
                onChange={setHeaders}
                addLabel={p.addHeader ?? 'Add header'}
                keyPlaceholder={p.headerKeyPlaceholder ?? 'Header name'}
                valuePlaceholder={p.headerValuePlaceholder ?? 'Header value'}
              />
            </div>
          </AdvancedOptionsDisclosure>
        </div>

        {err && <p className="mt-3 text-xs text-[var(--c-status-error-text)]">{err}</p>}
    </SettingsModalFrame>
  )
}

// -- Provider Detail --

function ProviderDetail({
  provider,
  accessToken,
  onUpdated,
  p,
  showTitle = true,
  autoImportModels = false,
  onAutoImportStarted,
}: {
  provider: LlmProvider
  accessToken: string
  onUpdated: () => void
  p: ReturnType<typeof useLocale>['t']['adminProviders']
  showTitle?: boolean
  autoImportModels?: boolean
  onAutoImportStarted?: () => void
}) {
  const [formPreset, setFormPreset] = useState<VendorPresetKey>(toVendorKey(provider.provider, provider.openai_api_mode))
  const [formName, setFormName] = useState(provider.name)
  const [formApiKey, setFormApiKey] = useState('')
  const [formBaseUrl, setFormBaseUrl] = useState(provider.base_url ?? '')
  const [formHeaders, setFormHeaders] = useState<HeaderEntry[]>(() => readHeaderEntriesFromAdvancedJSON(provider.advanced_json))
  const [err, setErr] = useState('')
  const autoSaveTimerRef = useRef<number | null>(null)
  const readOnly = isManagedLocalProvider(provider)

  useEffect(() => {
    setFormPreset(toVendorKey(provider.provider, provider.openai_api_mode))
    setFormName(provider.name)
    setFormApiKey('')
    setFormBaseUrl(provider.base_url ?? '')
    setFormHeaders(readHeaderEntriesFromAdvancedJSON(provider.advanced_json))
    setErr('')
  }, [provider.advanced_json, provider.base_url, provider.id, provider.name, provider.openai_api_mode, provider.provider])

  useEffect(() => {
    return () => {
      if (autoSaveTimerRef.current !== null) window.clearTimeout(autoSaveTimerRef.current)
    }
  }, [])

  const autoSaveProvider = useCallback(async () => {
    if (readOnly || !formName.trim()) return
    const selected = VENDOR_PRESETS.find((v) => v.key === formPreset)
    const nextBaseUrl = formBaseUrl.trim()
    const apiKey = formApiKey.trim()
    const providerChanged = selected?.provider !== provider.provider
    const modeChanged = (selected?.openai_api_mode ?? null) !== (provider.openai_api_mode ?? null)
    const nameChanged = formName.trim() !== provider.name
    const baseUrlChanged = nextBaseUrl !== (provider.base_url ?? '')
    const apiKeyChanged = apiKey !== ''
    const nextAdvancedJSON = writeHeaderEntriesToAdvancedJSON(
      mergeProviderAdvancedJSON(provider.advanced_json, readOpenVikingBackend(provider)),
      formHeaders,
    )
    const advancedChanged = advancedJSONSignature(nextAdvancedJSON) !== advancedJSONSignature(provider.advanced_json)
    if (!providerChanged && !modeChanged && !nameChanged && !baseUrlChanged && !apiKeyChanged && !advancedChanged) return

    setErr('')
    try {
      await updateLlmProvider(accessToken, provider.id, {
        name: formName.trim() || undefined,
        api_key: apiKey || undefined,
        base_url: nextBaseUrl || null,
        provider: selected?.provider,
        openai_api_mode: selected?.openai_api_mode ?? null,
        advanced_json: nextAdvancedJSON,
      })
      setFormApiKey('')
      onUpdated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : p.saveFailed)
    }
  }, [accessToken, formApiKey, formBaseUrl, formHeaders, formName, formPreset, onUpdated, p.saveFailed, provider, readOnly])

  useEffect(() => {
    if (readOnly) return
    if (autoSaveTimerRef.current !== null) window.clearTimeout(autoSaveTimerRef.current)
    autoSaveTimerRef.current = window.setTimeout(() => {
      autoSaveTimerRef.current = null
      void autoSaveProvider()
    }, 650)
    return () => {
      if (autoSaveTimerRef.current !== null) {
        window.clearTimeout(autoSaveTimerRef.current)
        autoSaveTimerRef.current = null
      }
    }
  }, [autoSaveProvider, readOnly])

  if (readOnly) {
    const authModeLabel = localAuthModeLabel(provider, p)
    return (
      <div className="min-w-0 space-y-6">
        {showTitle && <h3 className="pl-2.5 text-[19px] font-semibold leading-none text-[var(--c-text-heading)]">{provider.name}</h3>}
        <ProviderDetailSection title={p.providerConfig ?? p.advancedConfig}>
          <ProviderDetailCard>
            <ProviderDetailRow label={p.providerName}>
              <div className="flex flex-wrap items-center justify-end gap-1.5">
                <span className="rounded-md px-2 py-1 text-xs font-medium text-[var(--c-text-muted)]" style={{ background: 'var(--c-bg-sub)' }}>
                  {p.localProvider}
                </span>
                <span className="rounded-md px-2 py-1 text-xs font-medium text-[var(--c-text-muted)]" style={{ background: 'var(--c-bg-sub)' }}>
                  {p.readOnlyProvider}
                </span>
                {authModeLabel && (
                  <span className="rounded-md px-2 py-1 text-xs font-medium text-[var(--c-text-muted)]" style={{ background: 'var(--c-bg-sub)' }}>
                    {authModeLabel}
                  </span>
                )}
              </div>
            </ProviderDetailRow>
          </ProviderDetailCard>
        </ProviderDetailSection>
        <ModelsSection provider={provider} accessToken={accessToken} onChanged={onUpdated} p={p} readOnly />
      </div>
    )
  }

  return (
      <div className="min-w-0 space-y-6">
      {showTitle && <h3 className="pl-2.5 text-[19px] font-semibold leading-none text-[var(--c-text-heading)]">{provider.name}</h3>}

      <ProviderDetailSection title={p.providerConfig ?? p.advancedConfig}>
        <ProviderDetailCard>
          <ProviderDetailRow label={p.vendor}>
            <VendorDropdown value={formPreset} onChange={setFormPreset} p={p} />
          </ProviderDetailRow>
          <ProviderDetailRow label={p.providerName}>
            <SettingsInput value={formName} onChange={(e) => setFormName(e.target.value)} />
          </ProviderDetailRow>
          <ProviderDetailRow label={p.apiKey}>
            <div>
              <SettingsInput
                type="password"
                value={formApiKey}
                onChange={(e) => setFormApiKey(e.target.value)}
                placeholder={provider.key_prefix ? `${provider.key_prefix}${'*'.repeat(40)}` : p.apiKeyPlaceholder}
              />
            </div>
          </ProviderDetailRow>
          <ProviderDetailRow label={p.baseUrl}>
            <SettingsInput
              value={formBaseUrl}
              onChange={(e) => setFormBaseUrl(e.target.value.slice(0, 500))}
              placeholder={p.baseUrlPlaceholder ?? 'https://api.example.com/v1'}
              maxLength={500}
            />
          </ProviderDetailRow>
          <ProviderDetailRow label={p.headers ?? 'Headers'}>
            <HeadersEditor
              headers={formHeaders}
              onChange={setFormHeaders}
              addLabel={p.addHeader ?? 'Add header'}
              keyPlaceholder={p.headerKeyPlaceholder ?? 'Header name'}
              valuePlaceholder={p.headerValuePlaceholder ?? 'Header value'}
            />
          </ProviderDetailRow>
        </ProviderDetailCard>
        {err && <p className="pl-2.5 text-xs text-[var(--c-status-error-text)]">{err}</p>}
      </ProviderDetailSection>

      <ModelsSection
        provider={provider}
        accessToken={accessToken}
        onChanged={onUpdated}
        p={p}
        autoImportModels={autoImportModels}
        onAutoImportStarted={onAutoImportStarted}
      />
    </div>
  )
}

// -- Models Section (same pattern as ModelConfigContent) --

function ModelsSection({
  provider,
  accessToken,
  onChanged,
  p,
  readOnly = false,
  autoImportModels = false,
  onAutoImportStarted,
}: {
  provider: LlmProvider
  accessToken: string
  onChanged: () => void
  p: ReturnType<typeof useLocale>['t']['adminProviders']
  readOnly?: boolean
  autoImportModels?: boolean
  onAutoImportStarted?: () => void
}) {
  const { t } = useLocale()
  const [available, setAvailable] = useState<AvailableModel[] | null>(null)
  const [loadingAvailable, setLoadingAvailable] = useState(false)
  const [availableError, setAvailableError] = useState<ProviderActionError | null>(null)
  const [importing, setImporting] = useState(false)
  const [deletingAll, setDeletingAll] = useState(false)
  const [creatingModel, setCreatingModel] = useState(false)
  const [actionError, setActionError] = useState<ProviderActionError | null>(null)
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
    setActionError(null)
    setAvailableError(null)
    setShowDeleteAllConfirm(false)
  }, [provider.id])

  const loadAvailable = useCallback(async () => {
    setLoadingAvailable(true)
    setAvailableError(null)
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
      setHasLoadedAvailable(true)
    } catch (e) {
      setAvailableError(providerActionErrorFromUnknown(e, t.models.availableFetchFailed))
    } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, provider.id, t.models.availableFetchFailed])

  const ensureAvailableLoaded = useCallback(async (): Promise<AvailableModel[]> => {
    if (available !== null) return available
    setLoadingAvailable(true)
    setAvailableError(null)
    try {
      const res = await listAvailableModels(accessToken, provider.id)
      setAvailable(res.models)
      setHasLoadedAvailable(true)
      return res.models
    } catch (e) {
      const displayError = providerActionErrorFromUnknown(e, t.models.availableFetchFailed)
      setAvailableError(displayError)
      throw new AvailableModelsLoadError(displayError)
    } finally {
      setLoadingAvailable(false)
    }
  }, [accessToken, available, provider.id, t.models.availableFetchFailed])

  const handleImportAll = async () => {
    setImporting(true)
    setActionError(null)
    try {
      const source = await ensureAvailableLoaded()
      const unconfigured = source.filter((am) => !am.configured)
      const byId = new Map<string, AvailableModel>()
      for (const am of unconfigured) {
        if (!byId.has(am.id)) byId.set(am.id, am)
      }
      const toImport = [...byId.values()]
      const embeddingIds = new Set(toImport.filter((am) => am.type === 'embedding').map((am) => am.id))
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
      const toEnable = created.filter((pm) => pm.model.toLowerCase().includes('gpt-4o-mini') && !embeddingIds.has(pm.model))
      if (toEnable.length > 0) {
        try {
          await patchProviderModel(accessToken, provider.id, toEnable[0].id, { show_in_picker: true, is_default: true })
          await Promise.all(toEnable.slice(1).map((pm) => patchProviderModel(accessToken, provider.id, pm.id, { show_in_picker: true })))
        } catch { /* default-setting is best-effort */ }
      }
      onChanged()
      await loadAvailable()
    } catch (e) {
      if (isAvailableModelsLoadError(e)) return
      setActionError(providerActionErrorFromUnknown(e, p.saveFailed))
    } finally {
      setImporting(false)
    }
  }

  useEffect(() => {
    if (!autoImportModels || readOnly) return
    onAutoImportStarted?.()
    void handleImportAll()
  }, [autoImportModels, readOnly, onAutoImportStarted])

  const handleDeleteModel = useCallback(async (modelId: string) => {
    try {
      await deleteProviderModel(accessToken, provider.id, modelId)
      onChanged()
    } catch (e) {
      setActionError(providerActionErrorFromUnknown(e, p.saveFailed))
    }
  }, [accessToken, provider.id, onChanged, p.saveFailed])

  const handleDeleteAll = async () => {
    setDeletingAll(true)
    setActionError(null)
    let failed = 0
    let firstError: ProviderActionError | null = null
    for (const pm of provider.models) {
      try {
        await deleteProviderModel(accessToken, provider.id, pm.id)
      } catch (e) {
        failed++
        if (!firstError) firstError = providerActionErrorFromUnknown(e, p.saveFailed)
      }
    }
    setDeletingAll(false)
    if (failed > 0) setActionError(firstError ?? { message: p.saveFailed })
    onChanged()
    setAvailable(null)
    setHasLoadedAvailable(false)
  }

  const handleTogglePicker = useCallback(async (modelId: string, current: boolean) => {
    try {
      await patchProviderModel(accessToken, provider.id, modelId, { show_in_picker: !current })
      onChanged()
    } catch (e) {
      setActionError(providerActionErrorFromUnknown(e, p.saveFailed))
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
      onChanged()
    } catch (e) {
      throw new Error(isApiError(e) ? e.message : p.saveFailed)
    }
  }, [accessToken, editingModel, onChanged, p.saveFailed, provider.id])

  const unconfiguredCount = available?.filter((am) => !am.configured).length ?? 0
  const importDisabled = importing || loadingAvailable || (hasLoadedAvailable && unconfiguredCount === 0)
  const deleteAllDisabled = deletingAll || provider.models.length === 0
  const sectionError = availableError ?? actionError
  const importButtonLabel =
    loadingAvailable || importing
      ? (p.importing ?? '...')
      : unconfiguredCount > 0
      ? `${p.importAll ?? 'Import all'} (${unconfiguredCount})`
      : hasLoadedAvailable
        ? ''
        : (p.importAll ?? 'Import models')
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
    <ProviderDetailSection title={p.modelsSection}>
      <ProviderDetailCard>
        <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
          <h4 className="text-[13px] font-medium text-[var(--c-text-primary)]">{p.modelsSection}</h4>
        {!readOnly && (
          <div className="flex flex-wrap items-center gap-2">
            <SettingsIconButton
              label={p.deleteAll ?? 'Delete all'}
              danger
              onClick={() => setShowDeleteAllConfirm(true)}
              disabled={deleteAllDisabled}
            >
              {deletingAll ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
            </SettingsIconButton>
            <SettingsButton
              variant="secondary"
              onClick={() => void handleImportAll()}
              disabled={importDisabled}
              className={importButtonLabel || availableError ? undefined : 'w-[32px] px-0'}
              icon={loadingAvailable || importing
                ? <Loader2 size={12} className="animate-spin" />
                : (
                    <>
                      {availableError && <X size={12} className="text-[var(--c-status-error-text)]" />}
                      <Download
                        size={12}
                        className={availableError ? 'text-[var(--c-status-error-text)]' : undefined}
                      />
                    </>
                  )}
            >
              {importButtonLabel}
            </SettingsButton>
            {sectionError && <ErrorDetailsButton error={sectionError} />}
            <ModelTestButton
              accessToken={accessToken}
              provider={provider}
              label={p.testModel ?? 'Test'}
              searchPlaceholder={p.searchProviders}
            />
            <SettingsButton variant="primary" onClick={() => setCreatingModel(true)}>
              {p.addModel}
            </SettingsButton>
          </div>
        )}
        </div>

      {!readOnly && hasLoadedAvailable && !loadingAvailable && !availableError && available !== null && available.length === 0 && (
        <p className="px-5 pb-4 text-xs text-[var(--c-text-muted)]">{t.models.noModelsAvailable}</p>
      )}

      {provider.models.length > 0 && (
        <div className="px-5 pb-3">
          <SettingsSearchInput value={search} onChange={(e) => setSearch(e.target.value)} placeholder={p.searchProviders} />
        </div>
      )}

      <div className="overflow-y-auto px-5" style={{ maxHeight: '430px' }}>
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
              readOnly={readOnly}
            />
          ))
        )}
      </div>
      </ProviderDetailCard>

      {!readOnly && editingModel !== null && (
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
          headers: p.headers ?? 'Headers',
          addHeader: p.addHeader ?? 'Add header',
          headerKeyPlaceholder: p.headerKeyPlaceholder ?? 'Header name',
          headerValuePlaceholder: p.headerValuePlaceholder ?? 'Header value',
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

      {!readOnly && creatingModel && (
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

      {!readOnly && (
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
      )}
    </ProviderDetailSection>
  )
}

const ModelRow = memo(function ModelRow({ pm, onToggle, onEdit, onDelete, readOnly = false }: {
  pm: LlmProviderModel
  onToggle: (id: string, current: boolean) => void
  onEdit: (pm: LlmProviderModel) => void
  onDelete: (id: string) => void
  readOnly?: boolean
}) {
  return (
    <div
      className="group relative flex flex-wrap items-center justify-between gap-2 px-0 py-3 [&+&]:before:absolute [&+&]:before:left-0 [&+&]:before:right-0 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']"
      style={{ contentVisibility: 'auto', containIntrinsicBlockSize: '52px' }}
    >
      <div className="min-w-0 flex-1 flex items-center gap-1.5">
        <p className="truncate text-sm font-medium text-[var(--c-text-primary)]">{pm.model}</p>
        {pm.tags.includes('embedding') && (
          <span className="shrink-0 rounded-md px-2 py-0.5 text-xs font-medium" style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-muted)' }}>emb</span>
        )}
      </div>
      <div className="flex w-full shrink-0 items-center justify-end gap-2 sm:w-auto">
        {readOnly ? (
          <SettingsSwitch checked={pm.show_in_picker} onChange={() => onToggle(pm.id, pm.show_in_picker)} />
        ) : (
          <>
            <SettingsSwitch checked={pm.show_in_picker} onChange={() => onToggle(pm.id, pm.show_in_picker)} />
            <SettingsIconButton
              label="Edit model"
              variant="plain"
              onClick={() => onEdit(pm)}
            >
              <SlidersHorizontal size={14} />
            </SettingsIconButton>
            <SettingsIconButton
              label="Delete model"
              variant="plain"
              danger
              onClick={() => onDelete(pm.id)}
            >
              <Trash2 size={14} />
            </SettingsIconButton>
          </>
        )}
      </div>
    </div>
  )
})

function ProviderDetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2.5">
      <h4 className="pl-2.5 text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h4>
      {children}
    </section>
  )
}

function ProviderDetailCard({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div className={`overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]${className ? ` ${className}` : ''}`}>
      {children}
    </div>
  )
}

function ProviderDetailRow({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div className="relative grid items-center gap-3 px-5 py-4 sm:grid-cols-[minmax(0,1fr)_minmax(260px,390px)] sm:gap-6 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']">
      <div className="min-w-0 text-[13px] font-medium text-[var(--c-text-primary)]">{label}</div>
      <div className="min-w-0 sm:justify-self-end sm:w-full">{children}</div>
    </div>
  )
}

function ErrorDetailsButton({ error }: { error: ProviderActionError }) {
  const [open, setOpen] = useState(false)

  return (
    <div className="relative">
      <SettingsButton
        variant="danger"
        onClick={() => setOpen((v) => !v)}
        style={{ color: 'var(--c-status-error-text)' }}
      >
        Error
      </SettingsButton>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div
            className="dropdown-menu absolute right-0 top-[calc(100%+6px)] z-50 max-w-[360px] min-w-[240px]"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '12px',
              background: 'var(--c-bg-menu)',
              boxShadow: 'var(--c-dropdown-shadow)',
              maxHeight: '180px',
              overflowY: 'auto',
            }}
          >
            <pre className="whitespace-pre-wrap break-all text-xs text-[var(--c-text-secondary)]">{formatProviderActionError(error)}</pre>
          </div>
        </>
      )}
    </div>
  )
}

function ModelTestButton({ accessToken, provider, label, searchPlaceholder, iconOnly = false }: {
  accessToken: string
  provider: LlmProvider
  label: string
  searchPlaceholder: string
  iconOnly?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [testing, setTesting] = useState<string | null>(null)
  const [result, setResult] = useState<{ modelId: string; success: boolean; latency?: number; error?: ProviderActionError } | null>(null)
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({})
  const triggerRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const pickerModels = useMemo(
    () => provider.models.filter((m) => m.show_in_picker),
    [provider.models],
  )

  const filtered = useMemo(() => {
    if (!open) return []
    const q = search.trim().toLowerCase()
    return q ? pickerModels.filter((m) => m.model.toLowerCase().includes(q)) : pickerModels
  }, [open, search, pickerModels])

  const positionMenu = useCallback(() => {
    const trigger = triggerRef.current
    if (!trigger || typeof window === 'undefined') return
    const rect = trigger.getBoundingClientRect()
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight
    const gap = 6
    const margin = 12
    const width = Math.min(260, Math.max(220, viewportWidth - margin * 2))
    const estimatedHeight = Math.min(288, Math.max(92, pickerModels.length * 37 + 48))
    const spaceBelow = viewportHeight - rect.bottom - margin - gap
    const spaceAbove = rect.top - margin - gap
    const openAbove = spaceBelow < Math.min(estimatedHeight, 180) && spaceAbove > spaceBelow
    const maxHeight = Math.min(288, Math.max(92, openAbove ? spaceAbove : spaceBelow))
    const top = openAbove ? Math.max(margin, rect.top - gap - maxHeight) : Math.min(rect.bottom + gap, viewportHeight - margin - maxHeight)
    const left = Math.min(Math.max(margin, rect.right - width), viewportWidth - margin - width)
    setMenuStyle({
      position: 'fixed',
      top,
      left,
      width,
      maxHeight,
      zIndex: 10000,
    })
  }, [pickerModels.length])

  useEffect(() => {
    if (!open) return
    const closeOnPointer = (event: MouseEvent) => {
      const target = event.target as Node
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', closeOnPointer, true)
    return () => document.removeEventListener('mousedown', closeOnPointer, true)
  }, [open])

  useLayoutEffect(() => {
    if (!open) return
    positionMenu()
    window.addEventListener('resize', positionMenu)
    window.addEventListener('scroll', positionMenu, true)
    return () => {
      window.removeEventListener('resize', positionMenu)
      window.removeEventListener('scroll', positionMenu, true)
    }
  }, [open, positionMenu, filtered.length])

  const handleTest = async (model: LlmProviderModel) => {
    setTesting(model.id)
    setOpen(false)
    try {
      const res = await testLlmProviderModel(accessToken, provider.id, model.id)
      setResult({
        modelId: model.id,
        success: res.success,
        latency: res.latency_ms ?? undefined,
        error: res.error ? { message: res.error } : undefined,
      })
    } catch (e) {
      setResult({ modelId: model.id, success: false, error: providerActionErrorFromUnknown(e, 'Unknown error') })
    } finally {
      setTesting(null)
    }
  }

  const handleTrigger = () => {
    if (result?.success && !testing) { setResult(null); return }
    setOpen((prev) => {
      if (!prev) {
        setSearch('')
        positionMenu()
      }
      return !prev
    })
  }

  const icon = testing
    ? <Loader2 size={12} className="animate-spin" />
    : result
      ? result.success
        ? <AnimatedCheck size={12} color="var(--c-status-success-text)" />
        : <X size={12} className="text-[var(--c-status-error-text)]" />
      : <Zap size={12} strokeWidth={1.5} />

  return (
    <div ref={triggerRef} className="relative flex items-center gap-2">
      {iconOnly ? (
        <SettingsIconButton
          label={label}
          onClick={handleTrigger}
          disabled={testing !== null || pickerModels.length === 0}
        >
          {icon}
        </SettingsIconButton>
      ) : (
        <SettingsButton
          variant="secondary"
          onClick={handleTrigger}
          disabled={testing !== null || pickerModels.length === 0}
          icon={icon}
        >
          {label}
        </SettingsButton>
      )}
      {result && !result.success && !testing && (
        <ErrorDetailsButton error={result.error ?? { message: 'Unknown error' }} />
      )}
      {open && createPortal(
        <>
          <div className="fixed inset-0" style={{ zIndex: 9999 }} onMouseDown={() => setOpen(false)} />
          <div
            ref={menuRef}
            className="dropdown-menu overflow-hidden"
            onMouseDown={(event) => event.stopPropagation()}
            style={{
              ...menuStyle,
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            <div style={{ padding: '4px 4px 2px' }}>
              <SettingsSearchInput
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={searchPlaceholder}
              />
            </div>
            <div className="overflow-y-auto py-1" style={{ maxHeight: `calc(${String(menuStyle.maxHeight ?? 288)}px - 48px)` }}>
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
        </>,
        document.body,
      )}
    </div>
  )
}
