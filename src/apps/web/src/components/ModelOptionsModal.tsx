import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Brain, Eye, Image as ImageIcon, Loader2, Wrench } from 'lucide-react'
import { AutoResizeTextarea, FormField } from '@arkloop/shared'
import type { AvailableModel, LlmProviderModel } from '../api'
import {
  AVAILABLE_CATALOG_ADVANCED_KEY,
  getAvailableCatalogFromAdvancedJson,
  mergeAvailableCatalogIntoAdvancedJson,
  routeAdvancedJsonFromAvailableCatalog,
  stripAvailableCatalogFromAdvancedJson,
} from '@arkloop/shared/llm/available-catalog-advanced-json'
import { SettingsButton } from './settings/_SettingsButton'
import { SettingsInput, settingsInputCls } from './settings/_SettingsInput'
import { SettingsModalFrame } from './settings/_SettingsModalFrame'
import { SettingsSelect } from './settings/_SettingsSelect'
import { SettingsSwitch } from './settings/_SettingsSwitch'
import {
  HeadersEditor,
  readHeaderEntriesFromAdvancedJSON,
  stripHeaderEntriesFromAdvancedJSON,
  writeHeaderEntriesToAdvancedJSON,
  type HeaderEntry,
} from './settings/HeadersEditor'

type Labels = {
  modelOptionsTitle: string
  modelOptionsFor: string
  modelCapabilities: string
  modelType?: string
  modelTypeChat?: string
  modelTypeEmbedding?: string
  modelTypeImage?: string
  modelTypeAudio?: string
  modelTypeModeration?: string
  modelTypeOther?: string
  vision: string
  imageOutput: string
  embedding: string
  toolCalling?: string
  reasoning?: string
  defaultTemperature?: string
  contextWindow: string
  maxOutputTokens: string
  providerOptionsJson: string
  providerOptionsHint: string
  headers?: string
  addHeader?: string
  headerKeyPlaceholder?: string
  headerValuePlaceholder?: string
  save: string
  cancel: string
  reset: string
  invalidJson: string
  invalidNumber: string
  visionBridgeHint: string
  addModelTitle: string
  modelNameLabel: string
  modelNamePlaceholder: string
}

type Props = {
  open: boolean
  mode?: 'create' | 'edit'
  model: LlmProviderModel | null
  availableModels: AvailableModel[] | null
  labels: Labels
  onClose: () => void
  onSave: (payload: {
    advancedJSON: Record<string, unknown> | null
    tags: string[]
  }) => Promise<void>
  onCreate?: (payload: {
    model: string
    advancedJSON: Record<string, unknown> | null
    tags: string[]
  }) => Promise<void>
}

type DraftState = {
  modelName: string
  modelType: string
  vision: boolean
  imageOutput: boolean
  embedding: boolean
  toolCalling: boolean
  reasoning: boolean
  contextWindow: string
  maxOutputTokens: string
  defaultTemperature: string
  headers: HeaderEntry[]
  providerOptionsJSON: string
}

type SavePayload = {
  advancedJSON: Record<string, unknown> | null
  tags: string[]
}

const TEXTAREA_CLS =
  `${settingsInputCls('md')} h-auto min-h-[180px] resize-y py-2 font-mono text-[13px] leading-relaxed`

function normalizePositiveIntegerInput(value: string): string {
  const trimmed = value.trim()
  if (trimmed === '') return ''
  if (!/^\d+$/.test(trimmed)) return trimmed
  const parsed = Number.parseInt(trimmed, 10)
  return Number.isFinite(parsed) && parsed > 0 ? String(parsed) : trimmed
}

function normalizeFloatInput(value: string): string {
  const trimmed = value.trim()
  if (trimmed === '') return ''
  if (!/^-?\d+(?:\.\d+)?$/.test(trimmed)) return trimmed
  const parsed = Number.parseFloat(trimmed)
  return Number.isFinite(parsed) ? String(parsed) : trimmed
}

function resolvedModelType(model: LlmProviderModel | null, catalog: Record<string, unknown> | null): string {
  if (typeof catalog?.type === 'string' && catalog.type.trim() !== '') {
    return catalog.type.trim()
  }
  if (model?.tags.includes('embedding')) return 'embedding'
  return 'chat'
}

function deriveAutoCatalog(model: LlmProviderModel | null, availableModels: AvailableModel[] | null): Record<string, unknown> | null {
  if (!model || !availableModels) return null
  const matched = availableModels.find((item) => item.id.toLowerCase() === model.model.toLowerCase())
  return matched ? routeAdvancedJsonFromAvailableCatalog(matched)[AVAILABLE_CATALOG_ADVANCED_KEY] as Record<string, unknown> : null
}

function deriveDraft(model: LlmProviderModel | null): DraftState {
  if (!model) {
    return {
      modelName: '',
      modelType: 'chat',
      vision: false,
      imageOutput: false,
      embedding: false,
      toolCalling: true,
      reasoning: false,
      contextWindow: '',
      maxOutputTokens: '',
      defaultTemperature: '',
      headers: [],
      providerOptionsJSON: '{}',
    }
  }
  const catalog = getAvailableCatalogFromAdvancedJson(model.advanced_json)
  const rest = stripHeaderEntriesFromAdvancedJSON(stripAvailableCatalogFromAdvancedJson(model.advanced_json))
  const modelType = resolvedModelType(model, catalog)
  const inputModalities = Array.isArray(catalog?.input_modalities) ? catalog.input_modalities : []
  const outputModalities = Array.isArray(catalog?.output_modalities) ? catalog.output_modalities : []
  const overrideVal = catalog?.context_length_override
  const catalogVal = catalog?.context_length
  const contextLength = typeof overrideVal === 'number' ? String(overrideVal)
    : typeof catalogVal === 'number' ? String(catalogVal)
    : ''
  return {
    modelName: '',
    modelType,
    vision: inputModalities.includes('image'),
    imageOutput: outputModalities.includes('image'),
    embedding: modelType === 'embedding',
    toolCalling: catalog?.tool_calling !== false,
    reasoning: catalog?.reasoning === true,
    contextWindow: contextLength,
    maxOutputTokens: typeof catalog?.max_output_tokens === 'number' ? String(catalog.max_output_tokens) : '',
    defaultTemperature: typeof catalog?.default_temperature === 'number' ? String(catalog.default_temperature) : '',
    headers: readHeaderEntriesFromAdvancedJSON(model.advanced_json),
    providerOptionsJSON: JSON.stringify(rest, null, 2),
  }
}

function buildCatalog(model: LlmProviderModel, draft: DraftState): Record<string, unknown> | null {
  const currentCatalog = getAvailableCatalogFromAdvancedJson(model.advanced_json) ?? {}
  const nextCatalog: Record<string, unknown> = { ...currentCatalog }

  if (typeof nextCatalog.id !== 'string' || nextCatalog.id.trim() === '') {
    nextCatalog.id = model.model
  }
  if (typeof nextCatalog.name !== 'string' || nextCatalog.name.trim() === '') {
    nextCatalog.name = model.model
  }

  const inputModalities = new Set<string>(
    Array.isArray(currentCatalog.input_modalities)
      ? currentCatalog.input_modalities
        .filter((item): item is string => typeof item === 'string')
        .map((item) => item.trim())
        .filter(Boolean)
      : [],
  )
  const outputModalities = new Set<string>(
    Array.isArray(currentCatalog.output_modalities)
      ? currentCatalog.output_modalities
        .filter((item): item is string => typeof item === 'string')
        .map((item) => item.trim())
        .filter(Boolean)
      : [],
  )

  if (draft.vision) inputModalities.add('image')
  else inputModalities.delete('image')
  if (draft.imageOutput) outputModalities.add('image')
  else outputModalities.delete('image')
  if (inputModalities.size > 0) nextCatalog.input_modalities = [...inputModalities]
  else delete nextCatalog.input_modalities
  if (outputModalities.size > 0) nextCatalog.output_modalities = [...outputModalities]
  else delete nextCatalog.output_modalities

  if (draft.contextWindow.trim() !== '') {
    nextCatalog.context_length_override = Number.parseInt(draft.contextWindow.trim(), 10)
  } else {
    delete nextCatalog.context_length_override
  }
  if (draft.maxOutputTokens.trim() !== '') {
    nextCatalog.max_output_tokens = Number.parseInt(draft.maxOutputTokens.trim(), 10)
  } else {
    delete nextCatalog.max_output_tokens
  }
  if (draft.defaultTemperature.trim() !== '') {
    nextCatalog.default_temperature = Number.parseFloat(draft.defaultTemperature.trim())
  } else {
    delete nextCatalog.default_temperature
  }

  if (draft.modelType !== 'chat') nextCatalog.type = draft.modelType
  else delete nextCatalog.type

  if (draft.toolCalling) nextCatalog.tool_calling = true
  else delete nextCatalog.tool_calling
  if (draft.reasoning) nextCatalog.reasoning = true
  else delete nextCatalog.reasoning

  return Object.keys(nextCatalog).length > 0 ? nextCatalog : null
}

function buildEditPayload(
  model: LlmProviderModel,
  draft: DraftState,
  labels: Labels,
): { payload: SavePayload; nextDraft: DraftState } {
  const contextWindow = normalizePositiveIntegerInput(draft.contextWindow)
  const maxOutputTokens = normalizePositiveIntegerInput(draft.maxOutputTokens)
  const defaultTemperature = normalizeFloatInput(draft.defaultTemperature)
  if (
    (contextWindow && !/^\d+$/.test(contextWindow)) ||
    (maxOutputTokens && !/^\d+$/.test(maxOutputTokens)) ||
    (defaultTemperature && !/^-?\d+(?:\.\d+)?$/.test(defaultTemperature))
  ) {
    throw new Error(labels.invalidNumber)
  }

  let providerOptions: Record<string, unknown> = {}
  try {
    const parsed = JSON.parse(draft.providerOptionsJSON.trim() || '{}') as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error(labels.invalidJson)
    }
    providerOptions = { ...(parsed as Record<string, unknown>) }
  } catch {
    throw new Error(labels.invalidJson)
  }

  if (AVAILABLE_CATALOG_ADVANCED_KEY in providerOptions) {
    delete providerOptions[AVAILABLE_CATALOG_ADVANCED_KEY]
  }
  providerOptions = stripHeaderEntriesFromAdvancedJSON(providerOptions)

  const nextType = draft.modelType.trim() || 'chat'
  const nextDraft: DraftState = {
    ...draft,
    modelType: nextType,
    embedding: nextType === 'embedding',
    contextWindow,
    maxOutputTokens,
    defaultTemperature,
  }
  const catalog = buildCatalog(model, nextDraft)
  const advancedJSON = writeHeaderEntriesToAdvancedJSON(
    mergeAvailableCatalogIntoAdvancedJson(catalog, providerOptions),
    nextDraft.headers,
  )
  const tags = nextDraft.embedding
    ? Array.from(new Set([...model.tags.filter((tag) => tag !== 'embedding'), 'embedding']))
    : model.tags.filter((tag) => tag !== 'embedding')

  return { payload: { advancedJSON, tags }, nextDraft }
}

export function ModelOptionsModal({
  open,
  mode = 'edit',
  model,
  availableModels,
  labels,
  onClose,
  onSave,
  onCreate,
}: Props) {
  const [draft, setDraft] = useState<DraftState>(() => deriveDraft(model))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const lastSavedSignatureRef = useRef('')

  const autoCatalog = useMemo(() => deriveAutoCatalog(model, availableModels), [model, availableModels])

  const handleClose = useCallback(() => { if (!saving) onClose() }, [saving, onClose])

  useEffect(() => {
    if (!open) return
    const nextDraft = deriveDraft(model)
    setDraft(nextDraft)
    setError('')
    setSaving(false)
    if (mode !== 'create' && model) {
      try {
        lastSavedSignatureRef.current = JSON.stringify(buildEditPayload(model, nextDraft, labels).payload)
      } catch {
        lastSavedSignatureRef.current = ''
      }
    } else {
      lastSavedSignatureRef.current = ''
    }
  }, [open, model, mode])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') handleClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, handleClose])

  const handleReset = () => {
    if (!model) return
    const current = deriveDraft(model)
    const restJSON = current.providerOptionsJSON
    const nextDraft: DraftState = { ...current, providerOptionsJSON: restJSON }
    if (autoCatalog) {
      const autoInput = Array.isArray(autoCatalog.input_modalities)
        ? autoCatalog.input_modalities
        : []
      const autoOutput = Array.isArray(autoCatalog.output_modalities)
        ? autoCatalog.output_modalities
        : []
      nextDraft.vision = autoInput.includes('image')
      nextDraft.imageOutput = autoOutput.includes('image')
      if (typeof autoCatalog.context_length === 'number') {
        nextDraft.contextWindow = String(autoCatalog.context_length)
      }
      if (typeof autoCatalog.max_output_tokens === 'number') {
        nextDraft.maxOutputTokens = String(autoCatalog.max_output_tokens)
      }
      nextDraft.modelType = typeof autoCatalog.type === 'string' && autoCatalog.type.trim() !== ''
        ? autoCatalog.type.trim()
        : nextDraft.modelType
      nextDraft.embedding = nextDraft.modelType === 'embedding'
      nextDraft.defaultTemperature = typeof autoCatalog.default_temperature === 'number' ? String(autoCatalog.default_temperature) : ''
      nextDraft.toolCalling = autoCatalog.tool_calling !== false
      nextDraft.reasoning = autoCatalog.reasoning === true
    }
    if (availableModels) {
      const matched = availableModels.find(
        (item) => item.id.toLowerCase() === model.model.toLowerCase(),
      )
      if (matched) {
        nextDraft.embedding = matched.type === 'embedding'
      }
    }
    setDraft(nextDraft)
    setError('')
  }

  const isCreate = mode === 'create'

  useEffect(() => {
    if (!open || isCreate || !model) return
    const id = window.setTimeout(() => {
      let built: { payload: SavePayload; nextDraft: DraftState }
      try {
        built = buildEditPayload(model, draft, labels)
      } catch (err) {
        setError(err instanceof Error ? err.message : labels.invalidJson)
        return
      }

      const signature = JSON.stringify(built.payload)
      if (signature === lastSavedSignatureRef.current) {
        setError('')
        return
      }

      setSaving(true)
      setError('')
      void onSave(built.payload)
        .then(() => {
          lastSavedSignatureRef.current = signature
          setDraft(built.nextDraft)
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : labels.invalidJson)
        })
        .finally(() => setSaving(false))
    }, 650)
    return () => window.clearTimeout(id)
  }, [draft, isCreate, labels.invalidJson, labels.invalidNumber, model, onSave, open])

  const handleSave = async () => {
    const contextWindow = normalizePositiveIntegerInput(draft.contextWindow)
    const maxOutputTokens = normalizePositiveIntegerInput(draft.maxOutputTokens)
    const defaultTemperature = normalizeFloatInput(draft.defaultTemperature)
    if (
      (contextWindow && !/^\d+$/.test(contextWindow)) ||
      (maxOutputTokens && !/^\d+$/.test(maxOutputTokens)) ||
      (defaultTemperature && !/^-?\d+(?:\.\d+)?$/.test(defaultTemperature))
    ) {
      setError(labels.invalidNumber)
      return
    }

    let providerOptions: Record<string, unknown> = {}
    try {
      const parsed = JSON.parse(draft.providerOptionsJSON.trim() || '{}') as unknown
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(labels.invalidJson)
      }
      providerOptions = { ...(parsed as Record<string, unknown>) }
    } catch {
      setError(labels.invalidJson)
      return
    }

    if (AVAILABLE_CATALOG_ADVANCED_KEY in providerOptions) {
      delete providerOptions[AVAILABLE_CATALOG_ADVANCED_KEY]
    }
    providerOptions = stripHeaderEntriesFromAdvancedJSON(providerOptions)

    const nextType = draft.modelType.trim() || 'chat'
    const nextDraft: DraftState = {
      ...draft,
      modelType: nextType,
      embedding: nextType === 'embedding',
      contextWindow,
      maxOutputTokens,
      defaultTemperature,
    }

    if (isCreate) {
      if (!draft.modelName.trim()) {
        setError(labels.modelNameLabel)
        return
      }
      const catalog: Record<string, unknown> = {
        id: draft.modelName.trim(),
        name: draft.modelName.trim(),
      }
      if (nextDraft.vision) catalog.input_modalities = ['image']
      if (nextDraft.imageOutput) catalog.output_modalities = ['image']
      if (nextDraft.contextWindow) catalog.context_length_override = Number.parseInt(nextDraft.contextWindow, 10)
      if (nextDraft.maxOutputTokens) catalog.max_output_tokens = Number.parseInt(nextDraft.maxOutputTokens, 10)
      if (nextDraft.modelType !== 'chat') catalog.type = nextDraft.modelType
      if (nextDraft.defaultTemperature) catalog.default_temperature = Number.parseFloat(nextDraft.defaultTemperature)
      if (nextDraft.toolCalling) catalog.tool_calling = true
      if (nextDraft.reasoning) catalog.reasoning = true

      const advancedJSON = writeHeaderEntriesToAdvancedJSON(
        mergeAvailableCatalogIntoAdvancedJson(catalog, providerOptions),
        nextDraft.headers,
      )
      const tags = nextDraft.embedding ? ['embedding'] : []

      setSaving(true)
      setError('')
      try {
        await onCreate?.({ model: draft.modelName.trim(), advancedJSON, tags })
      } catch (err) {
        setError(err instanceof Error ? err.message : labels.invalidJson)
        setSaving(false)
        return
      }
      setSaving(false)
    } else {
      if (!model) return
      const { payload } = buildEditPayload(model, nextDraft, labels)

      setSaving(true)
      setError('')
      try {
        await onSave(payload)
      } catch (err) {
        setError(err instanceof Error ? err.message : labels.invalidJson)
        setSaving(false)
        return
      }
      setSaving(false)
    }
  }

  if (!open) return null

  return (
    <SettingsModalFrame
      open={open}
      title={isCreate ? labels.addModelTitle : labels.modelOptionsTitle}
      onClose={handleClose}
      width={760}
    >
        {(isCreate || model) && (
          <div className="mt-6 max-h-[calc(85vh-160px)] space-y-5 overflow-y-auto pr-1">
            {isCreate ? (
              <FormField label={labels.modelNameLabel}>
                <SettingsInput
                  variant="md"
                  value={draft.modelName}
                  onChange={(e) => setDraft((prev) => ({ ...prev, modelName: e.target.value }))}
                  placeholder={labels.modelNamePlaceholder}
                  autoFocus
                />
              </FormField>
            ) : model && (
              <p className="text-sm text-[var(--c-text-secondary)]">
                {labels.modelOptionsFor}
                <span className="ml-1 rounded bg-[var(--c-bg-sub)] px-2 py-0.5 text-[var(--c-text-primary)]">{model.model}</span>
              </p>
            )}

            <section className="space-y-3">
              <div className="flex items-center justify-between gap-3">
                <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{labels.modelCapabilities}</h4>
                {!isCreate && (
                  <SettingsButton
                    onClick={handleReset}
                    disabled={saving}
                    size="modal"
                  >
                    {labels.reset}
                  </SettingsButton>
                )}
              </div>

              <FormField label={labels.modelType ?? 'Model Type'}>
                <SettingsSelect
                  value={draft.modelType}
                  options={[
                    { value: 'chat', label: labels.modelTypeChat ?? 'Chat' },
                    { value: 'embedding', label: labels.modelTypeEmbedding ?? 'Embedding' },
                    { value: 'image', label: labels.modelTypeImage ?? 'Image' },
                    { value: 'audio', label: labels.modelTypeAudio ?? 'Audio' },
                    { value: 'moderation', label: labels.modelTypeModeration ?? 'Moderation' },
                    { value: 'other', label: labels.modelTypeOther ?? 'Other' },
                  ]}
                  onChange={(next) => {
                    setDraft((prev) => ({
                      ...prev,
                      modelType: next,
                      embedding: next === 'embedding',
                    }))
                  }}
                />
              </FormField>

              <div className="grid gap-3 md:grid-cols-2">
                <CapabilityTile
                  icon={<Eye size={18} />}
                  label={labels.vision}
                  checked={draft.vision}
                  onChange={(next) => setDraft((prev) => ({ ...prev, vision: next }))}
                />
                <CapabilityTile
                  icon={<ImageIcon size={18} />}
                  label={labels.imageOutput}
                  checked={draft.imageOutput}
                  onChange={(next) => setDraft((prev) => ({ ...prev, imageOutput: next }))}
                />
                <CapabilityTile
                  icon={<Wrench size={18} />}
                  label={labels.toolCalling ?? 'Tool Calling'}
                  checked={draft.toolCalling}
                  onChange={(next) => setDraft((prev) => ({ ...prev, toolCalling: next }))}
                />
                <CapabilityTile
                  icon={<Brain size={18} />}
                  label={labels.reasoning ?? 'Reasoning'}
                  checked={draft.reasoning}
                  onChange={(next) => setDraft((prev) => ({ ...prev, reasoning: next }))}
                />
              </div>

              <div className="grid gap-3 md:grid-cols-2">
                <FormField label={labels.contextWindow}>
                  <SettingsInput
                    variant="md"
                    value={draft.contextWindow}
                    onChange={(e) => setDraft((prev) => ({ ...prev, contextWindow: e.target.value }))}
                    placeholder="e.g. 128000"
                    inputMode="numeric"
                  />
                </FormField>
                <FormField label={labels.maxOutputTokens}>
                  <SettingsInput
                    variant="md"
                    value={draft.maxOutputTokens}
                    onChange={(e) => setDraft((prev) => ({ ...prev, maxOutputTokens: e.target.value }))}
                    placeholder="e.g. 32768"
                    inputMode="numeric"
                  />
                </FormField>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <FormField label={labels.defaultTemperature ?? 'Default Temperature'}>
                  <SettingsInput
                    variant="md"
                    value={draft.defaultTemperature}
                    onChange={(e) => setDraft((prev) => ({ ...prev, defaultTemperature: e.target.value }))}
                    placeholder="e.g. 0.7"
                    inputMode="decimal"
                  />
                </FormField>
              </div>
            </section>
            <p className="text-xs text-[var(--c-text-muted)]">{labels.visionBridgeHint}</p>

            <FormField label={labels.headers ?? 'Headers'}>
              <HeadersEditor
                headers={draft.headers}
                onChange={(headers) => setDraft((prev) => ({ ...prev, headers }))}
                addLabel={labels.addHeader ?? 'Add header'}
                keyPlaceholder={labels.headerKeyPlaceholder ?? 'Header name'}
                valuePlaceholder={labels.headerValuePlaceholder ?? 'Header value'}
              />
            </FormField>

            <FormField label={labels.providerOptionsJson} error={error}>
              <AutoResizeTextarea
                rows={8}
                minRows={8}
                maxHeight={320}
                value={draft.providerOptionsJSON}
                onChange={(e) => setDraft((prev) => ({ ...prev, providerOptionsJSON: e.target.value }))}
                className={TEXTAREA_CLS}
                spellCheck={false}
              />
            </FormField>
            <p className="text-xs text-[var(--c-text-muted)]">{labels.providerOptionsHint}</p>

            {isCreate && (
            <div className="flex items-center justify-end pt-1">
              <div className="flex items-center gap-2">
                <SettingsButton
                  onClick={handleClose}
                  disabled={saving}
                  size="modal"
                >
                  {labels.cancel}
                </SettingsButton>
                <SettingsButton
                  variant="primary"
                  onClick={() => void handleSave()}
                  disabled={saving}
                  size="modal"
                >
                  <span className="relative flex items-center justify-center">
                    <span className={`flex items-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-0' : 'opacity-100'}`}>{labels.save}</span>
                    <span className={`absolute inset-0 flex items-center justify-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-100' : 'opacity-0'}`}>
                      <Loader2 size={14} className="animate-spin" />
                    </span>
                  </span>
                </SettingsButton>
              </div>
            </div>
            )}
          </div>
        )}
    </SettingsModalFrame>
  )
}

function CapabilityTile({
  icon,
  label,
  checked,
  disabled = false,
  onChange,
}: {
  icon: ReactNode
  label: string
  checked: boolean
  disabled?: boolean
  onChange: (next: boolean) => void
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={() => { if (!disabled) onChange(!checked) }}
      className="flex w-full items-center justify-between rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-3 transition-colors duration-150 hover:bg-[var(--c-bg-sub)] disabled:cursor-not-allowed disabled:opacity-50"
    >
      <div className="flex items-center gap-3 text-[var(--c-text-primary)]">
        <span className="text-[var(--c-text-secondary)]">{icon}</span>
        <span className="text-sm font-medium">{label}</span>
      </div>
      <span onClick={(e) => e.stopPropagation()}>
        <SettingsSwitch checked={checked} onChange={disabled ? () => {} : onChange} />
      </span>
    </button>
  )
}
