import { useState, useCallback, useEffect, useMemo } from 'react'
import {
  Plus,
  Trash2,
  Bot,
  Check,
  Search,
  ChevronLeft,
  CheckCheck,
  Minus,
} from 'lucide-react'
import { AutoResizeTextarea, Modal, ConfirmDialog } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { isApiError } from '../../api'
import { listLlmProviders, type LlmProvider } from '../../api'
import { SettingsModelDropdown } from './SettingsModelDropdown'
import { SettingsSelect } from './_SettingsSelect'
import {
  type LiteAgent,
  type ToolCatalogGroup,
  type ToolCatalogItem,
  listLiteAgents,
  createLiteAgent,
  patchLiteAgent,
  deleteLiteAgent,
  listToolCatalog,
} from '../../api-admin'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Props = { accessToken: string }
type DetailTab = 'overview' | 'prompt' | 'tools'
type ToolSelectionMode = 'inherit' | 'custom'

type DetailForm = {
  name: string
  model: string
  isActive: boolean
  temperature: number
  maxOutputTokens: string
  reasoningMode: string
  streamThinking: boolean
  systemPrompt: string
  toolSelectionMode: ToolSelectionMode
  tools: string[]
  toolDenylist: string[]
}

type ModelOption = { value: string; label: string }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function agentToForm(agent: LiteAgent): DetailForm {
  const allowlist = agent.tool_allowlist ?? []
  const denylist = agent.tool_denylist ?? []
  return {
    name: agent.display_name,
    model: agent.model || '',
    isActive: agent.is_active,
    temperature: agent.temperature ?? 0.7,
    maxOutputTokens:
      agent.max_output_tokens != null ? String(agent.max_output_tokens) : '',
    reasoningMode: agent.reasoning_mode || 'auto',
    streamThinking: agent.stream_thinking !== false,
    systemPrompt: agent.prompt_md || '',
    toolSelectionMode: allowlist.length === 0 ? 'inherit' : 'custom',
    tools: allowlist,
    toolDenylist: denylist,
  }
}

function uniq(names: string[]): string[] {
  return Array.from(new Set(names.map((n) => n.trim()).filter(Boolean)))
}

function buildModelOptions(providers: LlmProvider[]): ModelOption[] {
  return providers.flatMap((p) =>
    (p.models ?? []).map((m) => ({
      value: `${p.name}^${m.model}`,
      label: `${p.name} · ${m.model}`,
    })),
  )
}

function ensureCurrentOption(
  options: ModelOption[],
  current: string,
): ModelOption[] {
  if (!current.trim() || options.some((o) => o.value === current))
    return options
  return [{ value: current, label: current }, ...options]
}

// ---------------------------------------------------------------------------
// Shared styles
// ---------------------------------------------------------------------------

const INPUT_CLS =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'
const MONO_CLS =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 font-mono text-xs leading-relaxed text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'

// ---------------------------------------------------------------------------
// Small sub-components
// ---------------------------------------------------------------------------

function CheckboxField({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <label className="flex cursor-pointer select-none items-center gap-2.5 text-sm text-[var(--c-text-secondary)]">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="sr-only"
      />
      <span
        className={[
          'flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors',
          checked
            ? 'border-[var(--c-accent)] bg-[var(--c-accent)]'
            : 'border-[var(--c-border-subtle)] bg-[var(--c-bg-input)]',
        ].join(' ')}
      >
        {checked && <Check size={11} className="text-white" strokeWidth={3} />}
      </span>
      {label}
    </label>
  )
}

function ToolCard({
  tool,
  checked,
  onToggle,
}: {
  tool: ToolCatalogItem
  checked: boolean
  onToggle: () => void
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={[
        'flex w-full items-start gap-3 rounded-xl border px-4 py-3 text-left transition-colors',
        checked
          ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/8'
          : 'border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] hover:border-[var(--c-border)]',
      ].join(' ')}
    >
      <span
        className={[
          'mt-0.5 flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded border transition-colors',
          checked
            ? 'border-[var(--c-accent)] bg-[var(--c-accent)] text-white'
            : 'border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] text-transparent',
        ].join(' ')}
      >
        <Check size={12} strokeWidth={3} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium text-[var(--c-text-primary)]">
          {tool.label || tool.name}
        </span>
        <span className="mt-0.5 block font-mono text-[10px] text-[var(--c-text-muted)]">
          {tool.name}
        </span>
        {tool.llm_description && (
          <span className="mt-1 block line-clamp-2 text-xs text-[var(--c-text-muted)]">
            {tool.llm_description}
          </span>
        )}
      </span>
    </button>
  )
}


// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function PersonasSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const a = t.adminAgents

  // Data
  const [agents, setAgents] = useState<LiteAgent[]>([])
  const [modelOptions, setModelOptions] = useState<ModelOption[]>([])
  const [catalogGroups, setCatalogGroups] = useState<ToolCatalogGroup[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Detail view
  const [selected, setSelected] = useState<LiteAgent | null>(null)
  const [detailTab, setDetailTab] = useState<DetailTab>('overview')
  const [form, setForm] = useState<DetailForm | null>(null)
  const [saving, setSaving] = useState(false)

  // Create modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createModel, setCreateModel] = useState('')
  const [creating, setCreating] = useState(false)

  // Delete confirmation
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

  // Tool search
  const [toolSearch, setToolSearch] = useState('')

  // ------ data loading ------
  const load = useCallback(async (): Promise<LiteAgent[]> => {
    setLoading(true)
    setError(null)
    try {
      const [liteAgents, providers, catalog] = await Promise.all([
        listLiteAgents(accessToken),
        listLlmProviders(accessToken),
        listToolCatalog(accessToken),
      ])
      setAgents(liteAgents)
      setModelOptions(buildModelOptions(providers))
      setCatalogGroups(catalog)
      return liteAgents
    } catch (err) {
      const msg = isApiError(err) ? err.message : t.requestFailed
      setError(msg)
      return []
    } finally {
      setLoading(false)
    }
  }, [accessToken, t.requestFailed])

  useEffect(() => {
    void load()
  }, [load])

  // ------ selection ------
  const selectAgent = useCallback((agent: LiteAgent) => {
    setSelected(agent)
    setForm(agentToForm(agent))
    setDetailTab('overview')
    setToolSearch('')
  }, [])

  const goBack = useCallback(() => {
    setSelected(null)
    setForm(null)
  }, [])

  // ------ tool helpers ------
  const allCatalogToolNames = useMemo(
    () => uniq(catalogGroups.flatMap((g) => g.tools.map((t) => t.name))),
    [catalogGroups],
  )

  const selectedToolCount = form
    ? form.toolSelectionMode === 'inherit'
      ? allCatalogToolNames.filter((n) => !form.toolDenylist.includes(n)).length
      : form.tools.length
    : 0

  const replaceTools = useCallback((tools: string[]) => {
    setForm((prev) =>
      prev
        ? { ...prev, toolSelectionMode: 'custom' as const, tools: uniq(tools) }
        : prev,
    )
  }, [])

  const replaceDeniedTools = useCallback((tools: string[]) => {
    setForm((prev) =>
      prev ? { ...prev, toolDenylist: uniq(tools) } : prev,
    )
  }, [])

  const toggleTool = useCallback((key: string) => {
    setForm((prev) => {
      if (!prev) return prev
      if (prev.toolSelectionMode === 'inherit') {
        return {
          ...prev,
          toolDenylist: prev.toolDenylist.includes(key)
            ? prev.toolDenylist.filter((n) => n !== key)
            : uniq([...prev.toolDenylist, key]),
        }
      }
      return {
        ...prev,
        tools: prev.tools.includes(key)
          ? prev.tools.filter((n) => n !== key)
          : uniq([...prev.tools, key]),
      }
    })
  }, [])

  const toggleToolGroup = useCallback(
    (group: ToolCatalogGroup, enable: boolean) => {
      setForm((prev) => {
        if (!prev) return prev
        const names = group.tools.map((t) => t.name)
        if (prev.toolSelectionMode === 'inherit') {
          return {
            ...prev,
            toolDenylist: enable
              ? prev.toolDenylist.filter((n) => !names.includes(n))
              : uniq([...prev.toolDenylist, ...names]),
          }
        }
        return {
          ...prev,
          tools: enable
            ? uniq([...prev.tools, ...names])
            : prev.tools.filter((n) => !names.includes(n)),
        }
      })
    },
    [],
  )

  const setToolMode = useCallback(
    (mode: ToolSelectionMode) => {
      setForm((prev) => {
        if (!prev || prev.toolSelectionMode === mode) return prev
        if (mode === 'inherit') return { ...prev, toolSelectionMode: mode }
        const next =
          prev.tools.length > 0
            ? prev.tools
            : prev.toolSelectionMode === 'inherit'
              ? allCatalogToolNames.filter(
                  (n) => !prev.toolDenylist.includes(n),
                )
              : allCatalogToolNames
        return { ...prev, toolSelectionMode: mode, tools: uniq(next) }
      })
    },
    [allCatalogToolNames],
  )

  // ------ CRUD ------
  const handleCreate = useCallback(async () => {
    if (!createName.trim() || !createModel.trim()) return
    setCreating(true)
    try {
      const agent = await createLiteAgent(accessToken, {
        scope: 'platform',
        name: createName.trim(),
        prompt_md: createName.trim(),
        model: createModel.trim(),
        tool_allowlist: [],
        tool_denylist: [],
        reasoning_mode: 'auto',
      })
      setCreateOpen(false)
      setCreateName('')
      setCreateModel('')
      void load()
      selectAgent(agent)
    } catch (err) {
      setError(isApiError(err) ? err.message : t.requestFailed)
    } finally {
      setCreating(false)
    }
  }, [accessToken, createModel, createName, load, selectAgent, t.requestFailed])

  const handleSave = useCallback(async () => {
    if (!selected || !form || !form.name.trim()) return
    setSaving(true)
    try {
      const payload = {
        scope: 'platform' as const,
        name: form.name.trim(),
        prompt_md: form.systemPrompt.trim(),
        model: form.model.trim() || undefined,
        temperature: form.temperature,
        max_output_tokens: form.maxOutputTokens
          ? Number(form.maxOutputTokens)
          : undefined,
        reasoning_mode: form.reasoningMode,
        stream_thinking: form.streamThinking,
        tool_allowlist:
          form.toolSelectionMode === 'inherit' ? [] : form.tools,
        tool_denylist:
          form.toolSelectionMode === 'inherit' ? form.toolDenylist : [],
        is_active: form.isActive,
      }

      const saved =
        selected.source === 'repo'
          ? await createLiteAgent(accessToken, {
              copy_from_repo_persona_key: selected.persona_key,
              ...payload,
              executor_type: selected.executor_type,
            })
          : await patchLiteAgent(accessToken, selected.id, payload)

      const fresh = await load()
      const updated =
        fresh.find((a) => a.id === saved.id) ??
        fresh.find(
          (a) => a.persona_key === saved.persona_key && a.source === 'db',
        )
      if (updated) {
        setSelected(updated)
        setForm(agentToForm(updated))
      }
    } catch (err) {
      setError(isApiError(err) ? err.message : a.saveFailed)
    } finally {
      setSaving(false)
    }
  }, [accessToken, form, load, selected, a.saveFailed])

  const handleDelete = useCallback(async () => {
    if (!selected) return
    setDeleting(true)
    try {
      await deleteLiteAgent(accessToken, selected.id)
      setDeleteOpen(false)
      goBack()
      void load()
    } catch (err) {
      setError(isApiError(err) ? err.message : t.requestFailed)
    } finally {
      setDeleting(false)
    }
  }, [accessToken, goBack, load, selected, t.requestFailed])

  // ------ derived ------
  const sortedAgents = useMemo(
    () =>
      [...agents].sort((a, b) => {
        if (a.source !== b.source) return a.source === 'repo' ? -1 : 1
        return a.display_name.localeCompare(b.display_name)
      }),
    [agents],
  )

  const selectedModelOptions = useMemo(
    () => ensureCurrentOption(modelOptions, form?.model ?? ''),
    [form?.model, modelOptions],
  )
  const createModelOptions = useMemo(
    () => ensureCurrentOption(modelOptions, createModel),
    [createModel, modelOptions],
  )

  const filteredCatalog = useMemo(() => {
    if (!toolSearch.trim()) return catalogGroups
    const q = toolSearch.toLowerCase()
    return catalogGroups
      .map((g) => ({
        ...g,
        tools: g.tools.filter(
          (t) =>
            t.name.toLowerCase().includes(q) ||
            (t.label && t.label.toLowerCase().includes(q)) ||
            (t.llm_description && t.llm_description.toLowerCase().includes(q)),
        ),
      }))
      .filter((g) => g.tools.length > 0)
  }, [catalogGroups, toolSearch])

  const isRepoAgent = selected?.source === 'repo'

  // =====================================================================
  // DETAIL VIEW (agent selected)
  // =====================================================================
  if (selected && form) {
    const tabs: { key: DetailTab; label: string }[] = [
      { key: 'overview', label: a.overviewTab },
      { key: 'prompt', label: a.promptTab },
      { key: 'tools', label: a.toolsTab },
    ]

    return (
      <div className="flex flex-col gap-4">
        {/* Header bar */}
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <button
              onClick={goBack}
              className="flex items-center text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
            >
              <ChevronLeft size={16} />
            </button>
            <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
              {selected.display_name}
            </h3>
            {selected.source === 'repo' && (
              <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
                {a.builtIn}
              </span>
            )}
            {selected.is_active ? (
              <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-500">
                {a.active}
              </span>
            ) : (
              <span className="rounded bg-neutral-500/10 px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
                {a.inactive}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {!isRepoAgent && (
              <button
                onClick={() => setDeleteOpen(true)}
                className="flex items-center gap-1 rounded-md px-2.5 py-1.5 text-xs text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-menu)] hover:text-red-500"
              >
                <Trash2 size={13} />
                {a.deleteAgent}
              </button>
            )}
            <button
              onClick={handleSave}
              disabled={saving || !form.name.trim()}
              className="rounded-md bg-[var(--c-accent)] px-3.5 py-1.5 text-xs font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {saving ? '…' : a.editAgent.replace(/^Edit/, 'Save')}
            </button>
          </div>
        </div>

        {error && (
          <div className="rounded-md bg-red-500/10 px-3 py-2 text-xs text-red-500">
            {error}
            <button
              className="ml-2 underline"
              onClick={() => setError(null)}
            >
              ✕
            </button>
          </div>
        )}

        {/* Tabs */}
        <div className="flex gap-1 border-b border-[var(--c-border-subtle)]">
          {tabs.map((item) => (
            <button
              key={item.key}
              onClick={() => setDetailTab(item.key)}
              className={[
                'px-3 py-2 text-sm font-medium transition-colors',
                detailTab === item.key
                  ? 'border-b-2 border-[var(--c-accent)] text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]',
              ].join(' ')}
            >
              {item.label}
            </button>
          ))}
        </div>

        {/* Tab content */}
        <div className="flex max-w-[640px] flex-col gap-5">
          {/* ---- Overview ---- */}
          {detailTab === 'overview' && (
            <>
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--c-text-muted)]">
                  {a.displayName} *
                </label>
                <input
                  className={INPUT_CLS}
                  placeholder={a.displayNamePlaceholder}
                  value={form.name}
                  onChange={(e) =>
                    setForm((p) => p && { ...p, name: e.target.value })
                  }
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--c-text-muted)]">
                  {a.model}
                </label>
                <SettingsModelDropdown
                  value={form.model}
                  options={selectedModelOptions}
                  placeholder={a.noModel}
                  disabled={saving}
                  onChange={(value) =>
                    setForm((p) => p && { ...p, model: value })
                  }
                  showEmpty
                />
              </div>

              <CheckboxField
                checked={form.isActive}
                onChange={(v) =>
                  setForm((p) => p && { ...p, isActive: v })
                }
                label={a.active}
              />

              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--c-text-muted)]">
                  {a.reasoningMode}
                </label>
                <SettingsSelect
                  value={form.reasoningMode}
                  onChange={(value) =>
                    setForm((p) =>
                      p && { ...p, reasoningMode: value },
                    )
                  }
                  options={[
                    { value: 'auto', label: a.reasoningDefault },
                    { value: 'enabled', label: a.reasoningEnabled },
                    { value: 'disabled', label: a.reasoningDisabled },
                  ]}
                />
              </div>

              <CheckboxField
                checked={form.streamThinking}
                onChange={(v) =>
                  setForm((p) => p && { ...p, streamThinking: v })
                }
                label={a.streamThinking}
              />
            </>
          )}

          {/* ---- Prompt ---- */}
          {detailTab === 'prompt' && (
            <>
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--c-text-muted)]">
                  {a.prompt}
                </label>
                <AutoResizeTextarea
                  className={`${MONO_CLS} min-h-[240px]`}
                  rows={12}
                  minRows={12}
                  maxHeight={480}
                  placeholder={a.promptPlaceholder}
                  value={form.systemPrompt}
                  onChange={(e) =>
                    setForm((p) =>
                      p && { ...p, systemPrompt: e.target.value },
                    )
                  }
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--c-text-muted)]">
                  {a.temperature}
                </label>
                <div className="flex items-center gap-3">
                  <input
                    type="range"
                    min={0}
                    max={2}
                    step={0.1}
                    value={form.temperature}
                    onChange={(e) =>
                      setForm((p) =>
                        p && { ...p, temperature: Number(e.target.value) },
                      )
                    }
                    className="flex-1"
                  />
                  <span className="w-8 text-right text-xs tabular-nums text-[var(--c-text-muted)]">
                    {form.temperature.toFixed(1)}
                  </span>
                </div>
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--c-text-muted)]">
                  {a.maxOutputTokens}
                </label>
                <input
                  type="number"
                  className={INPUT_CLS}
                  value={form.maxOutputTokens}
                  onChange={(e) =>
                    setForm((p) =>
                      p && { ...p, maxOutputTokens: e.target.value },
                    )
                  }
                />
              </div>
            </>
          )}

          {/* ---- Tools ---- */}
          {detailTab === 'tools' && (
            <>
              {catalogGroups.length > 0 ? (
                <div className="flex flex-col gap-4">
                  {/* Mode selector + search */}
                  <div className="flex flex-col gap-3 rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-3">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <p className="text-sm text-[var(--c-text-secondary)]">
                        {selectedToolCount} / {allCatalogToolNames.length} tools
                      </p>
                      <div className="flex gap-2">
                        <button
                          type="button"
                          onClick={() => setToolMode('inherit')}
                          className={[
                            'rounded-md border px-3 py-1.5 text-xs font-medium transition-colors',
                            form.toolSelectionMode === 'inherit'
                              ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/10 text-[var(--c-text-primary)]'
                              : 'border-[var(--c-border-subtle)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-input)]',
                          ].join(' ')}
                        >
                          {a.toolSelectionInherit}
                        </button>
                        <button
                          type="button"
                          onClick={() => setToolMode('custom')}
                          className={[
                            'rounded-md border px-3 py-1.5 text-xs font-medium transition-colors',
                            form.toolSelectionMode === 'custom'
                              ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/10 text-[var(--c-text-primary)]'
                              : 'border-[var(--c-border-subtle)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-input)]',
                          ].join(' ')}
                        >
                          {a.toolSelectionCustom}
                        </button>
                      </div>
                    </div>
                    <p className="text-xs text-[var(--c-text-muted)]">
                      {form.toolSelectionMode === 'inherit'
                        ? a.toolsInheritDesc
                        : a.toolsCustomDesc}
                    </p>
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div className="relative flex-1">
                        <Search
                          size={14}
                          className="absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
                        />
                        <input
                          className={`${INPUT_CLS} pl-8`}
                          placeholder={a.toolSearch}
                          value={toolSearch}
                          onChange={(e) => setToolSearch(e.target.value)}
                        />
                      </div>
                      <div className="flex gap-2">
                        <button
                          type="button"
                          onClick={() => {
                            if (form.toolSelectionMode === 'inherit') {
                              replaceDeniedTools([])
                            } else {
                              replaceTools(allCatalogToolNames)
                            }
                          }}
                          disabled={
                            form.toolSelectionMode === 'inherit'
                              ? form.toolDenylist.length === 0
                              : form.tools.length ===
                                allCatalogToolNames.length
                          }
                          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-input)] disabled:opacity-50"
                        >
                          <CheckCheck size={13} />
                          {a.enableAll}
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            if (form.toolSelectionMode === 'inherit') {
                              replaceDeniedTools(allCatalogToolNames)
                            } else {
                              replaceTools([])
                            }
                          }}
                          disabled={
                            form.toolSelectionMode === 'inherit'
                              ? form.toolDenylist.length ===
                                allCatalogToolNames.length
                              : form.tools.length === 0
                          }
                          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-input)] disabled:opacity-50"
                        >
                          <Minus size={13} />
                          {a.disableAll}
                        </button>
                      </div>
                    </div>
                  </div>

                  {/* Tool groups */}
                  {filteredCatalog.map((group) => {
                    const groupNames = group.tools.map((t) => t.name)
                    const groupSelected =
                      form.toolSelectionMode === 'inherit'
                        ? groupNames.filter(
                            (n) => !form.toolDenylist.includes(n),
                          ).length
                        : groupNames.filter((n) =>
                            form.tools.includes(n),
                          ).length

                    return (
                      <div
                        key={group.group}
                        className="flex flex-col gap-3 rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] p-4"
                      >
                        <div className="flex flex-wrap items-center justify-between gap-3">
                          <div>
                            <p className="text-xs font-medium uppercase tracking-wide text-[var(--c-text-muted)]">
                              {group.group}
                            </p>
                            <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
                              {groupSelected} / {group.tools.length}
                            </p>
                          </div>
                          <div className="flex gap-2">
                            <button
                              type="button"
                              onClick={() => toggleToolGroup(group, true)}
                              disabled={
                                group.tools.length === 0 ||
                                groupSelected === group.tools.length
                              }
                              className="rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-input)] disabled:opacity-50"
                            >
                              {a.enableAll}
                            </button>
                            <button
                              type="button"
                              onClick={() => toggleToolGroup(group, false)}
                              disabled={groupSelected === 0}
                              className="rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-input)] disabled:opacity-50"
                            >
                              {a.disableAll}
                            </button>
                          </div>
                        </div>
                        <div className="grid gap-3 md:grid-cols-2">
                          {group.tools.map((tool) => (
                            <ToolCard
                              key={tool.name}
                              tool={tool}
                              checked={
                                form.toolSelectionMode === 'inherit'
                                  ? !form.toolDenylist.includes(tool.name)
                                  : form.tools.includes(tool.name)
                              }
                              onToggle={() => toggleTool(tool.name)}
                            />
                          ))}
                        </div>
                      </div>
                    )
                  })}

                  {filteredCatalog.length === 0 && toolSearch.trim() && (
                    <p className="py-8 text-center text-sm text-[var(--c-text-muted)]">
                      {a.noTools}
                    </p>
                  )}
                </div>
              ) : (
                <p className="text-sm text-[var(--c-text-muted)]">
                  {a.noTools}
                </p>
              )}
            </>
          )}
        </div>

        {/* Delete confirmation */}
        <ConfirmDialog
          open={deleteOpen}
          onClose={() => setDeleteOpen(false)}
          onConfirm={handleDelete}
          message={a.deleteAgentConfirm}
          confirmLabel={deleting ? '…' : a.deleteAgent}
          loading={deleting}
        />
      </div>
    )
  }

  // =====================================================================
  // LIST VIEW (no agent selected)
  // =====================================================================
  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between gap-4">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
            {a.title}
          </h3>
          <p className="mt-1 text-xs text-[var(--c-text-muted)]">
            {a.subtitle}
          </p>
        </div>
        <button
          onClick={() => {
            setCreateOpen(true)
            setCreateName('')
            setCreateModel('')
          }}
          className="flex items-center gap-1.5 rounded-md bg-[var(--c-accent)] px-3 py-1.5 text-xs font-medium text-white transition-colors hover:opacity-90"
        >
          <Plus size={13} />
          {a.addAgent}
        </button>
      </div>

      {error && (
        <div className="rounded-md bg-red-500/10 px-3 py-2 text-xs text-red-500">
          {error}
          <button className="ml-2 underline" onClick={() => setError(null)}>
            ✕
          </button>
        </div>
      )}

      {/* Content */}
      {loading && agents.length === 0 ? (
        <div className="flex items-center justify-center py-16">
          <span className="text-sm text-[var(--c-text-muted)]">
            {t.loading}
          </span>
        </div>
      ) : agents.length === 0 ? (
        <div
          className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <Bot size={32} className="mb-3 text-[var(--c-text-muted)]" />
          <p className="text-sm text-[var(--c-text-muted)]">{a.noAgents}</p>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {sortedAgents.map((agent) => (
            <button
              key={agent.id}
              onClick={() => selectAgent(agent)}
              className="flex flex-col gap-3 rounded-xl bg-[var(--c-bg-menu)] px-5 py-4 text-left transition-colors hover:ring-1 hover:ring-[var(--c-border)]"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <div className="flex items-start justify-between gap-2">
                <h4 className="text-sm font-medium text-[var(--c-text-primary)]">
                  {agent.display_name}
                </h4>
                <div className="flex shrink-0 items-center gap-1.5">
                  {agent.source === 'repo' ? (
                    <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
                      {a.builtIn}
                    </span>
                  ) : (
                    <span className="rounded bg-purple-500/10 px-1.5 py-0.5 text-[10px] font-medium text-purple-500">
                      {a.custom}
                    </span>
                  )}
                  {agent.is_active ? (
                    <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-500">
                      {a.active}
                    </span>
                  ) : (
                    <span className="rounded bg-neutral-500/10 px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
                      {a.inactive}
                    </span>
                  )}
                </div>
              </div>
              <div className="text-xs text-[var(--c-text-muted)] truncate">
                {agent.model?.trim() || a.noModel}
              </div>
            </button>
          ))}
        </div>
      )}

      {/* Create modal */}
      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title={a.addAgent} width="420px">
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-[var(--c-text-muted)]">
              {a.displayName} *
            </label>
            <input
              className={INPUT_CLS}
              placeholder={a.displayNamePlaceholder}
              value={createName}
              onChange={(e) => setCreateName(e.target.value)}
              autoFocus
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-[var(--c-text-muted)]">
              {a.model} *
            </label>
            <SettingsModelDropdown
              value={createModel}
              options={createModelOptions}
              placeholder={a.modelPlaceholder}
              disabled={creating}
              onChange={setCreateModel}
              showEmpty
            />
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              onClick={() => setCreateOpen(false)}
              className="rounded-md border border-[var(--c-border-subtle)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-menu)]"
            >
              Cancel
            </button>
            <button
              onClick={handleCreate}
              disabled={creating || !createName.trim() || !createModel.trim()}
              className="rounded-md bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {creating ? '…' : a.addAgent}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
