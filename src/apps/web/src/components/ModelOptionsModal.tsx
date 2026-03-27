import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Eye, Image as ImageIcon, Database, Loader2 } from 'lucide-react'
import { FormField, Modal } from '@arkloop/shared'
import type { AvailableModel, LlmProviderModel } from '../api'
import {
  AVAILABLE_CATALOG_ADVANCED_KEY,
  getAvailableCatalogFromAdvancedJson,
  mergeAvailableCatalogIntoAdvancedJson,
  routeAdvancedJsonFromAvailableCatalog,
  stripAvailableCatalogFromAdvancedJson,
} from '@arkloop/shared/llm/available-catalog-advanced-json'
import { SettingsPillToggle } from './settings/_SettingsPillToggle'

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
}

type Props = {
  open: boolean
  model: LlmProviderModel | null
  availableModels: AvailableModel[] | null
  labels: Labels
  onClose: () => void
  onSave: (payload: {
    advancedJSON: Record<string, unknown> | null
    tags: string[]
  }) => Promise<void>
}

type DraftState = {
  vision: boolean
  imageOutput: boolean
  embedding: boolean
  contextWindow: string
  maxOutputTokens: string
  providerOptionsJSON: string
}

const TEXTAREA_CLS =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

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
  const contextLength = typeof catalog?.context_length === 'number' ? String(catalog.context_length) : ''
  const maxOutputTokens = typeof catalog?.max_output_tokens === 'number' ? String(catalog.max_output_tokens) : ''
  return {
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

  if (draft.contextWindow.trim() !== '') nextCatalog.context_length = Number.parseInt(draft.contextWindow.trim(), 10)
  else delete nextCatalog.context_length
  if (draft.maxOutputTokens.trim() !== '') nextCatalog.max_output_tokens = Number.parseInt(draft.maxOutputTokens.trim(), 10)
  else delete nextCatalog.max_output_tokens

  if (draft.embedding) nextCatalog.type = 'embedding'
  else if (nextCatalog.type === 'embedding') delete nextCatalog.type

  return Object.keys(nextCatalog).length > 0 ? nextCatalog : null
}

export function ModelOptionsModal({
  open,
  model,
  availableModels,
  labels,
  onClose,
  onSave,
}: Props) {
  const [draft, setDraft] = useState<DraftState>(() => deriveDraft(model))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const autoCatalog = useMemo(() => deriveAutoCatalog(model, availableModels), [model, availableModels])

  useEffect(() => {
    if (!open) return
    setDraft(deriveDraft(model))
    setError('')
    setSaving(false)
  }, [open, model])

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

  const handleSave = async () => {
    if (!model) return
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

    const nextDraft = {
      ...draft,
      contextWindow,
      maxOutputTokens,
    }
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

  return (
    <Modal open={open} onClose={() => { if (!saving) onClose() }} title={labels.modelOptionsTitle} width="760px">
      {!model ? null : (
        <div className="space-y-5">
          <p className="text-sm text-[var(--c-text-secondary)]">
            {labels.modelOptionsFor}
            <span className="ml-1 rounded bg-[var(--c-bg-sub)] px-2 py-0.5 text-[var(--c-text-primary)]">{model.model}</span>
          </p>

          <section className="space-y-3">
            <div>
              <h4 className="text-sm font-medium text-[var(--c-text-primary)]">{labels.modelCapabilities}</h4>
            </div>
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
                  className="w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]"
                  inputMode="numeric"
                />
              </FormField>
              <FormField label={labels.maxOutputTokens}>
                <input
                  value={draft.maxOutputTokens}
                  onChange={(e) => setDraft((prev) => ({ ...prev, maxOutputTokens: e.target.value }))}
                  placeholder="e.g. 4096"
                  className="w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]"
                  inputMode="numeric"
                />
              </FormField>
            </div>
          </section>
          <p className="text-xs text-[var(--c-text-muted)]">{labels.visionBridgeHint}</p>

          <FormField label={labels.providerOptionsJson} error={error}>
            <textarea
              rows={8}
              value={draft.providerOptionsJSON}
              onChange={(e) => setDraft((prev) => ({ ...prev, providerOptionsJSON: e.target.value }))}
              className={TEXTAREA_CLS}
              spellCheck={false}
            />
          </FormField>
          <p className="text-xs text-[var(--c-text-muted)]">{labels.providerOptionsHint}</p>

          <div className="flex items-center justify-between pt-1">
            <button
              type="button"
              onClick={handleReset}
              disabled={saving}
              className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {labels.reset}
            </button>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={onClose}
                disabled={saving}
                className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {labels.cancel}
              </button>
              <button
                type="button"
                onClick={() => void handleSave()}
                disabled={saving}
                className="inline-flex items-center gap-2 rounded-md bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {saving ? <Loader2 size={14} className="animate-spin" /> : labels.save}
              </button>
            </div>
          </div>
        </div>
      )}
    </Modal>
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
    <div className="flex items-center justify-between rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-sub)] px-4 py-3">
      <div className="flex items-center gap-3 text-[var(--c-text-primary)]">
        <span className="text-[var(--c-text-secondary)]">{icon}</span>
        <span className="text-sm font-medium">{label}</span>
      </div>
      <SettingsPillToggle checked={checked} onChange={onChange} />
    </div>
  )
}
