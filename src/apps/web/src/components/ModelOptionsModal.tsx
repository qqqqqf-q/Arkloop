import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Eye, Image as ImageIcon, Database, Loader2, X } from 'lucide-react'
import { AutoResizeTextarea, FormField } from '@arkloop/shared'
import type { AvailableModel, LlmProviderModel } from '../api'
import {
  AVAILABLE_CATALOG_ADVANCED_KEY,
  getAvailableCatalogFromAdvancedJson,
  mergeAvailableCatalogIntoAdvancedJson,
  routeAdvancedJsonFromAvailableCatalog,
  stripAvailableCatalogFromAdvancedJson,
} from '@arkloop/shared/llm/available-catalog-advanced-json'
import { PillToggle } from '@arkloop/shared'

type Labels = {
  modelOptionsTitle: string
  modelOptionsFor: string
  modelCapabilities: string
  vision: string
  imageOutput: string
  embedding: string
  contextWindow: string
  maxOutputTokens: string
  providerOptionsJson: string
  providerOptionsHint: string
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
  vision: boolean
  imageOutput: boolean
  embedding: boolean
  contextWindow: string
  maxOutputTokens: string
  providerOptionsJSON: string
}

const TEXTAREA_CLS =
  'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] transition-colors duration-150 focus:border-[var(--c-border)]'

function normalizePositiveIntegerInput(value: string): string {
  const trimmed = value.trim()
  if (trimmed === '') return ''
  if (!/^\d+$/.test(trimmed)) return trimmed
  const parsed = Number.parseInt(trimmed, 10)
  return Number.isFinite(parsed) && parsed > 0 ? String(parsed) : trimmed
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
      vision: false,
      imageOutput: false,
      embedding: false,
      contextWindow: '',
      maxOutputTokens: '',
      providerOptionsJSON: '{}',
    }
  }
  const catalog = getAvailableCatalogFromAdvancedJson(model.advanced_json)
  const rest = stripAvailableCatalogFromAdvancedJson(model.advanced_json)
  const inputModalities = Array.isArray(catalog?.input_modalities) ? catalog?.input_modalities : []
  const outputModalities = Array.isArray(catalog?.output_modalities) ? catalog?.output_modalities : []
  const overrideVal = catalog?.context_length_override
  const catalogVal = catalog?.context_length
  const contextLength = typeof overrideVal === 'number' ? String(overrideVal)
    : typeof catalogVal === 'number' ? String(catalogVal)
    : ''
  const maxOutputTokens = typeof catalog?.max_output_tokens === 'number' ? String(catalog.max_output_tokens) : ''
  return {
    modelName: '',
    vision: inputModalities.includes('image'),
    imageOutput: outputModalities.includes('image'),
    embedding: model.tags.includes('embedding'),
    contextWindow: contextLength,
    maxOutputTokens,
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
      ? currentCatalog.input_modalities.filter((item): item is string => typeof item === 'string').map((item) => item.trim()).filter(Boolean)
      : [],
  )
  const outputModalities = new Set<string>(
    Array.isArray(currentCatalog.output_modalities)
      ? currentCatalog.output_modalities.filter((item): item is string => typeof item === 'string').map((item) => item.trim()).filter(Boolean)
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
  if (draft.maxOutputTokens.trim() !== '') nextCatalog.max_output_tokens = Number.parseInt(draft.maxOutputTokens.trim(), 10)
  else delete nextCatalog.max_output_tokens

  if (draft.embedding) nextCatalog.type = 'embedding'
  else if (nextCatalog.type === 'embedding') delete nextCatalog.type

  return Object.keys(nextCatalog).length > 0 ? nextCatalog : null
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
  const overlayRef = useRef<HTMLDivElement>(null)

  const autoCatalog = useMemo(() => deriveAutoCatalog(model, availableModels), [model, availableModels])

  const handleClose = useCallback(() => { if (!saving) onClose() }, [saving, onClose])

  useEffect(() => {
    if (!open) return
    setDraft(deriveDraft(model))
    setError('')
    setSaving(false)
  }, [open, model])

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

  const handleSave = async () => {
    const contextWindow = normalizePositiveIntegerInput(draft.contextWindow)
    const maxOutputTokens = normalizePositiveIntegerInput(draft.maxOutputTokens)
    if ((contextWindow && !/^\d+$/.test(contextWindow)) || (maxOutputTokens && !/^\d+$/.test(maxOutputTokens))) {
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

    const nextDraft = { ...draft, contextWindow, maxOutputTokens }

    if (isCreate) {
      if (!draft.modelName.trim()) {
        setError(labels.modelNameLabel)
        return
      }
      // build catalog from scratch for create mode
      const catalog: Record<string, unknown> = {
        id: draft.modelName.trim(),
        name: draft.modelName.trim(),
      }
      if (nextDraft.vision) catalog.input_modalities = ['image']
      if (nextDraft.imageOutput) catalog.output_modalities = ['image']
      if (nextDraft.contextWindow) catalog.context_length_override = Number.parseInt(nextDraft.contextWindow, 10)
      if (nextDraft.maxOutputTokens) catalog.max_output_tokens = Number.parseInt(nextDraft.maxOutputTokens, 10)
      if (nextDraft.embedding) catalog.type = 'embedding'

      const advancedJSON = mergeAvailableCatalogIntoAdvancedJson(catalog, providerOptions)
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
      const catalog = buildCatalog(model, nextDraft)
      const advancedJSON = mergeAvailableCatalogIntoAdvancedJson(catalog, providerOptions)
      const nextTags = draft.embedding
        ? Array.from(new Set([...model.tags.filter((tag) => tag !== 'embedding'), 'embedding']))
        : model.tags.filter((tag) => tag !== 'embedding')

      setSaving(true)
      setError('')
      try {
        await onSave({ advancedJSON, tags: nextTags })
      } catch (err) {
        setError(err instanceof Error ? err.message : labels.invalidJson)
        setSaving(false)
        return
      }
      setSaving(false)
    }
  }

  if (!open) return null

  return createPortal(
    <div
      ref={overlayRef}
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'var(--c-overlay)' }}
      onClick={(e) => { if (e.target === overlayRef.current) handleClose() }}
    >
      <div
        className="modal-enter flex w-full max-w-[760px] flex-col gap-5 rounded-[14px] p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)', maxHeight: '85vh', margin: '0 20px', overflowY: 'auto' }}
      >
        {/* Header */}
        <div className="flex items-center justify-between">
          <h3 className="text-[15px] font-semibold text-[var(--c-text-heading)]">{isCreate ? labels.addModelTitle : labels.modelOptionsTitle}</h3>
          <button
            type="button"
            onClick={handleClose}
            disabled={saving}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:opacity-50"
          >
            <X size={14} />
          </button>
        </div>

        {(isCreate || model) && (
          <div className="space-y-5">
            {isCreate ? (
              <FormField label={labels.modelNameLabel}>
                <input
                  value={draft.modelName}
                  onChange={(e) => setDraft((prev) => ({ ...prev, modelName: e.target.value }))}
                  placeholder={labels.modelNamePlaceholder}
                  className="w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] transition-colors duration-150 focus:border-[var(--c-border)]"
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
              <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{labels.modelCapabilities}</h4>
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
                  icon={<Database size={18} />}
                  label={labels.embedding}
                  checked={draft.embedding}
                  onChange={(next) => setDraft((prev) => ({ ...prev, embedding: next }))}
                />
              </div>

              <div className="grid gap-3 md:grid-cols-2">
                <FormField label={labels.contextWindow}>
                  <input
                    value={draft.contextWindow}
                    onChange={(e) => setDraft((prev) => ({ ...prev, contextWindow: e.target.value }))}
                    placeholder="e.g. 128000"
                    className="w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] transition-colors duration-150 focus:border-[var(--c-border)]"
                    inputMode="numeric"
                  />
                </FormField>
                <FormField label={labels.maxOutputTokens}>
                  <input
                    value={draft.maxOutputTokens}
                    onChange={(e) => setDraft((prev) => ({ ...prev, maxOutputTokens: e.target.value }))}
                    placeholder="e.g. 32768"
                    className="w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] transition-colors duration-150 focus:border-[var(--c-border)]"
                    inputMode="numeric"
                  />
                </FormField>
              </div>
            </section>
            <p className="text-xs text-[var(--c-text-muted)]">{labels.visionBridgeHint}</p>

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

            <div className="flex items-center justify-between pt-1">
              {!isCreate ? (
                <button
                  type="button"
                  onClick={handleReset}
                  disabled={saving}
                  className="rounded-lg bg-[var(--c-bg-page)] px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                >
                  {labels.reset}
                </button>
              ) : <div />}
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleClose}
                  disabled={saving}
                  className="rounded-lg bg-[var(--c-bg-page)] px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                >
                  {labels.cancel}
                </button>
                <button
                  type="button"
                  onClick={() => void handleSave()}
                  disabled={saving}
                  className="inline-flex items-center justify-center rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)] disabled:opacity-50"
                  style={{ background: 'var(--c-btn-bg)' }}
                >
                  <span className="relative flex items-center justify-center">
                    <span className={`flex items-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-0' : 'opacity-100'}`}>{labels.save}</span>
                    <span className={`absolute inset-0 flex items-center justify-center gap-1.5 transition-opacity duration-150 ${saving ? 'opacity-100' : 'opacity-0'}`}>
                      <Loader2 size={14} className="animate-spin" />
                    </span>
                  </span>
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>,
    document.body,
  )
}

function CapabilityTile({
  icon,
  label,
  checked,
  onChange,
}: {
  icon: ReactNode
  label: string
  checked: boolean
  onChange: (next: boolean) => void
}) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className="flex w-full cursor-pointer items-center justify-between rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-3 transition-colors duration-150 hover:bg-[var(--c-bg-sub)]"
    >
      <div className="flex items-center gap-3 text-[var(--c-text-primary)]">
        <span className="text-[var(--c-text-secondary)]">{icon}</span>
        <span className="text-sm font-medium">{label}</span>
      </div>
      <span onClick={(e) => e.stopPropagation()}>
        <PillToggle checked={checked} onChange={onChange} />
      </span>
    </button>
  )
}
