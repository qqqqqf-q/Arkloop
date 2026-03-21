import { useState, useCallback, useEffect, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Plus, Trash2, ChevronLeft, Check } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { Modal } from '../components/Modal'
import { FormField } from '../components/FormField'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { isApiError } from '../api'
import {
  listLiteAgents,
  createLiteAgent,
  patchLiteAgent,
  deleteLiteAgent,
  listToolCatalog,
  type AgentScope,
  type LiteAgent,
  type ToolCatalogGroup,
  type ToolCatalogItem,
} from '../api/agents'
import { listLlmProviders } from '../api/llm-providers'
import { notifyToolCatalogChanged, subscribeToolCatalogRefresh } from '../lib/toolCatalogRefresh'

type DetailTab = 'overview' | 'persona' | 'tools'
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
  coreTools: string[]
  toolDiscoveryEnabled: boolean
}

type ModelSelectorOption = {
  value: string
  label: string
}

function agentToForm(agent: LiteAgent): DetailForm {
  const allowlist = agent.tool_allowlist ?? []
  const denylist = agent.tool_denylist ?? []
  const coreTools = agent.core_tools ?? []
  return {
    name: agent.display_name,
    model: agent.model || '',
    isActive: agent.is_active,
    temperature: agent.temperature ?? 0.7,
    maxOutputTokens: agent.max_output_tokens != null ? String(agent.max_output_tokens) : '',
    reasoningMode: agent.reasoning_mode || 'auto',
    streamThinking: agent.stream_thinking !== false,
    systemPrompt: agent.prompt_md || '',
    toolSelectionMode: allowlist.length === 0 ? 'inherit' : 'custom',
    tools: allowlist,
    toolDenylist: denylist,
    coreTools,
    toolDiscoveryEnabled: coreTools.length > 0,
  }
}

function isHybridAgent(agent: Pick<LiteAgent, 'executor_type'>): boolean {
  return agent.executor_type.trim() === 'agent.lua'
}

function resolveModelLabel(agent: LiteAgent): string {
  return agent.model?.trim() || '—'
}

function buildSelectorOptions(providers: Awaited<ReturnType<typeof listLlmProviders>>): ModelSelectorOption[] {
  return providers.flatMap((provider) =>
    (provider.models ?? []).map((model) => ({
      value: `${provider.name}^${model.model}`,
      label: `${provider.name} · ${model.model}`,
    })),
  )
}

function ensureCurrentOption(
  options: ModelSelectorOption[],
  currentValue: string,
): ModelSelectorOption[] {
  if (!currentValue.trim() || options.some((item) => item.value === currentValue)) {
    return options
  }
  return [{ value: currentValue, label: currentValue }, ...options]
}

function AgentModelLine({
  agent,
  label,
  hybridLabel,
  textClassName = 'text-xs text-[var(--c-text-muted)]',
}: {
  agent: LiteAgent
  label?: string
  hybridLabel: string
  textClassName?: string
}) {
  const modelLabel = resolveModelLabel(agent)

  return (
    <div className={`flex items-center gap-1.5 ${textClassName}`}>
      {label ? <span className="shrink-0">{label}:</span> : null}
      <span className="min-w-0 flex-1 truncate" title={modelLabel}>{modelLabel}</span>
      {isHybridAgent(agent) && (
        <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
          {hybridLabel}
        </span>
      )}
    </div>
  )
}

function CheckboxField({ checked, onChange, label }: {
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
          'flex h-[16px] w-[16px] shrink-0 items-center justify-center rounded-[4px] border transition-colors',
          checked
            ? 'border-[var(--c-accent)] bg-[var(--c-accent)]'
            : 'border-[var(--c-border)] bg-[var(--c-bg-input)]',
        ].join(' ')}
      >
        {checked && <Check size={11} className="text-white" strokeWidth={3} />}
      </span>
      {label}
    </label>
  )
}

function uniqToolNames(names: string[]): string[] {
  return Array.from(new Set(names.map((name) => name.trim()).filter(Boolean)))
}

function ToolRow({
  tool, checked, isCore, showCoreStar, onToggle, onToggleCore,
}: {
  tool: ToolCatalogItem
  checked: boolean
  isCore: boolean
  showCoreStar: boolean
  onToggle: () => void
  onToggleCore: () => void
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={[
        'flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors',
        checked
          ? 'bg-[var(--c-accent)]/6'
          : 'hover:bg-[var(--c-bg-sub)]',
      ].join(' ')}
      title={tool.llm_description}
    >
      <span
        className={[
          'flex h-[16px] w-[16px] shrink-0 items-center justify-center rounded-[4px] border transition-colors',
          checked
            ? 'border-[var(--c-accent)] bg-[var(--c-accent)]'
            : 'border-[var(--c-border)] bg-[var(--c-bg-input)]',
        ].join(' ')}
      >
        {checked && <Check size={10} className="text-white" strokeWidth={3} />}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-[var(--c-text-primary)]">
        {tool.label}
      </span>
      {showCoreStar && (
        <span
          role="button"
          onClick={(e) => { e.stopPropagation(); onToggleCore() }}
          className={[
            'shrink-0 text-sm transition-colors',
            isCore
              ? 'text-amber-500'
              : checked
                ? 'text-[var(--c-text-muted)] hover:text-amber-400'
                : 'text-[var(--c-border)]',
          ].join(' ')}
          title={isCore ? 'Core' : 'Set as core'}
        >
          {isCore ? '\u2605' : '\u2606'}
        </span>
      )}
    </button>
  )
}

const INPUT_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
const SELECT_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
const MONO_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-2 font-mono text-xs leading-relaxed text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'

export function AgentsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const ta = t.agents

  const [agents, setAgents] = useState<LiteAgent[]>([])
  const [scope, setScope] = useState<AgentScope>('platform')
  const [modelOptions, setModelOptions] = useState<ModelSelectorOption[]>([])
  const [catalogGroups, setCatalogGroups] = useState<ToolCatalogGroup[]>([])
  const [loading, setLoading] = useState(false)

  const [selected, setSelected] = useState<LiteAgent | null>(null)
  const [tab, setTab] = useState<DetailTab>('overview')
  const [form, setForm] = useState<DetailForm | null>(null)
  const [saving, setSaving] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createModel, setCreateModel] = useState('')
  const [creating, setCreating] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async (): Promise<LiteAgent[]> => {
    setLoading(true)
    try {
      const [liteAgents, providers, catalogResp] = await Promise.all([
        listLiteAgents(accessToken, scope),
        listLlmProviders(accessToken, scope),
        listToolCatalog(accessToken),
      ])

      setAgents(liteAgents)
      setModelOptions(buildSelectorOptions(providers))
      setCatalogGroups(catalogResp.groups)
      return liteAgents
    } catch (err) {
      addToast(isApiError(err) ? err.message : t.requestFailed, 'error')
      return []
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, scope, t.requestFailed])

  useEffect(() => {
    void load()
    return subscribeToolCatalogRefresh(() => {
      void load()
    })
  }, [load])

  const selectAgent = useCallback((agent: LiteAgent) => {
    setSelected(agent)
    setForm(agentToForm(agent))
    setTab('overview')
  }, [])

  const goBack = useCallback(() => {
    setSelected(null)
    setForm(null)
  }, [])

  const allCatalogToolNames = useMemo(
    () => uniqToolNames(catalogGroups.flatMap((group) => group.tools.map((tool) => tool.name))),
    [catalogGroups],
  )
  const selectedToolCount = form
    ? (form.toolSelectionMode === 'inherit'
        ? allCatalogToolNames.filter((toolName) => !form.toolDenylist.includes(toolName)).length
        : form.tools.length)
    : 0

  const handleCreate = useCallback(async () => {
    if (!createName.trim() || !createModel.trim()) return
    setCreating(true)
    try {
      const agent = await createLiteAgent({
        scope,
        name: createName.trim(),
        prompt_md: createName.trim(),
        model: createModel.trim(),
        tool_allowlist: [],
        tool_denylist: [],
        reasoning_mode: 'auto',
      }, accessToken)

      setCreateOpen(false)
      setCreateName('')
      setCreateModel('')
      notifyToolCatalogChanged()
      void load()
      selectAgent(agent)
    } catch (err) {
      addToast(isApiError(err) ? err.message : t.requestFailed, 'error')
    } finally {
      setCreating(false)
    }
  }, [accessToken, addToast, createModel, createName, load, scope, selectAgent, t.requestFailed])

  const handleSave = useCallback(async () => {
    if (!selected || !form || !form.name.trim()) return
    setSaving(true)
    try {
      const saved = selected.source === 'repo'
        ? await createLiteAgent({
          copy_from_repo_persona_key: selected.persona_key,
          scope,
          name: form.name.trim(),
          prompt_md: form.systemPrompt.trim(),
          model: form.model.trim() || undefined,
          temperature: form.temperature,
          max_output_tokens: form.maxOutputTokens ? Number(form.maxOutputTokens) : undefined,
          reasoning_mode: form.reasoningMode,
          stream_thinking: form.streamThinking,
          tool_allowlist: form.toolSelectionMode === 'inherit' ? [] : form.tools,
          tool_denylist: form.toolSelectionMode === 'inherit' ? form.toolDenylist : [],
          core_tools: form.toolDiscoveryEnabled ? form.coreTools : [],
          executor_type: selected.executor_type,
        }, accessToken)
        : await patchLiteAgent(selected.id, {
          scope,
          name: form.name.trim(),
          prompt_md: form.systemPrompt.trim() || undefined,
          model: form.model.trim() || undefined,
          temperature: form.temperature,
          max_output_tokens: form.maxOutputTokens ? Number(form.maxOutputTokens) : undefined,
          reasoning_mode: form.reasoningMode,
          stream_thinking: form.streamThinking,
          tool_allowlist: form.toolSelectionMode === 'inherit' ? [] : form.tools,
          tool_denylist: form.toolSelectionMode === 'inherit' ? form.toolDenylist : [],
          core_tools: form.toolDiscoveryEnabled ? form.coreTools : [],
          is_active: form.isActive,
        }, accessToken)

      notifyToolCatalogChanged()
      addToast(t.saved, 'success')
      const fresh = await load()
      const updated = fresh.find((item) => item.id === saved.id)
        ?? fresh.find((item) => item.persona_key === saved.persona_key && item.source === 'db')
      if (updated) {
        setSelected(updated)
        setForm(agentToForm(updated))
      }
    } catch (err) {
      addToast(isApiError(err) ? err.message : t.requestFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, form, load, scope, selected, t.requestFailed, t.saved])

  const handleDelete = useCallback(async () => {
    if (!selected) return
    setDeleting(true)
    try {
      await deleteLiteAgent(selected.id, scope, accessToken)
      setDeleteOpen(false)
      goBack()
      void load()
    } catch (err) {
      addToast(isApiError(err) ? err.message : t.requestFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [accessToken, addToast, goBack, load, scope, selected, t.requestFailed])

  const toggleTool = useCallback((key: string) => {
    setForm((prev) => (
      !prev
        ? prev
        : prev.toolSelectionMode === 'inherit'
          ? {
              ...prev,
              toolDenylist: prev.toolDenylist.includes(key)
                ? prev.toolDenylist.filter((item) => item !== key)
                : uniqToolNames([...prev.toolDenylist, key]),
            }
          : {
              ...prev,
              toolSelectionMode: 'custom',
              tools: prev.tools.includes(key)
                ? prev.tools.filter((item) => item !== key)
                : uniqToolNames([...prev.tools, key]),
            }
    ))
  }, [])

  const toggleToolGroup = useCallback((group: ToolCatalogGroup, enabled: boolean) => {
    setForm((prev) => {
      if (!prev) return prev
      const groupNames = group.tools.map((tool) => tool.name)
      if (prev.toolSelectionMode === 'inherit') {
        return {
          ...prev,
          toolDenylist: enabled
            ? prev.toolDenylist.filter((toolName) => !groupNames.includes(toolName))
            : uniqToolNames([...prev.toolDenylist, ...groupNames]),
        }
      }
      return {
        ...prev,
        toolSelectionMode: 'custom',
        tools: enabled
          ? uniqToolNames([...prev.tools, ...groupNames])
          : prev.tools.filter((toolName) => !groupNames.includes(toolName)),
      }
    })
  }, [])

  const toggleCoreTool = useCallback((key: string) => {
    setForm((prev) => {
      if (!prev) return prev
      const has = prev.coreTools.includes(key)
      return {
        ...prev,
        coreTools: has
          ? prev.coreTools.filter((t) => t !== key)
          : uniqToolNames([...prev.coreTools, key]),
      }
    })
  }, [])

  const setToolDiscoveryEnabled = useCallback((enabled: boolean) => {
    setForm((prev) => {
      if (!prev) return prev
      return {
        ...prev,
        toolDiscoveryEnabled: enabled,
        coreTools: enabled ? prev.coreTools : [],
      }
    })
  }, [])

  const setToolSelectionMode = useCallback((mode: ToolSelectionMode) => {
    setForm((prev) => {
      if (!prev || prev.toolSelectionMode === mode) return prev
      if (mode === 'inherit') {
        return { ...prev, toolSelectionMode: mode }
      }
      const nextTools = prev.tools.length > 0
        ? prev.tools
        : prev.toolSelectionMode === 'inherit'
          ? allCatalogToolNames.filter((toolName) => !prev.toolDenylist.includes(toolName))
          : allCatalogToolNames
      return { ...prev, toolSelectionMode: mode, tools: uniqToolNames(nextTools) }
    })
  }, [allCatalogToolNames])

  const sortedAgents = useMemo(
    () => [...agents].sort((a, b) => {
      if (a.source !== b.source) return a.source === 'repo' ? -1 : 1
      return a.display_name.localeCompare(b.display_name)
    }),
    [agents],
  )

  const isRepoAgent = selected?.source === 'repo'
  const selectedModelOptions = useMemo(
    () => ensureCurrentOption(modelOptions, form?.model ?? ''),
    [form?.model, modelOptions],
  )
  const createModelOptions = useMemo(
    () => ensureCurrentOption(modelOptions, createModel),
    [createModel, modelOptions],
  )

  if (selected && form) {
    const tabs: { key: DetailTab; label: string }[] = [
      { key: 'overview', label: ta.overview },
      { key: 'persona', label: ta.persona },
      { key: 'tools', label: ta.tools },
    ]

    return (
      <div className="flex h-full flex-col overflow-hidden">
        <PageHeader
          title={(
            <div className="flex items-center gap-2">
              <button
                onClick={goBack}
                className="flex items-center text-[var(--c-text-tertiary)] transition-colors hover:text-[var(--c-text-secondary)]"
              >
                <ChevronLeft size={16} />
              </button>
              <span>{selected.display_name}</span>
              {selected.source === 'repo' && (
                <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
                  {ta.builtIn}
                </span>
              )}
              {selected.is_active && (
                <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-500">
                  {ta.active}
                </span>
              )}
            </div>
          )}
          actions={(
            <div className="flex items-center gap-2">
              {!isRepoAgent && (
                <button
                  onClick={() => setDeleteOpen(true)}
                  className="flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-xs text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-red-500"
                >
                  <Trash2 size={13} />
                  {t.common.delete}
                </button>
              )}
              <button
                onClick={handleSave}
                disabled={saving || !form.name.trim()}
                className="rounded-lg bg-[var(--c-accent)] px-3.5 py-1.5 text-xs font-medium text-[var(--c-accent-text)] transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {saving ? '...' : t.common.save}
              </button>
            </div>
          )}
        />

        <div className="flex flex-1 overflow-hidden">
          <nav className="w-[160px] shrink-0 overflow-y-auto border-r border-[var(--c-border-console)] p-2">
            <div className="flex flex-col gap-[3px]">
              {tabs.map((item) => (
                <button
                  key={item.key}
                  onClick={() => setTab(item.key)}
                  className={[
                    'w-full rounded-[5px] px-3 py-[7px] text-left text-sm font-medium transition-colors',
                    tab === item.key
                      ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                      : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                  ].join(' ')}
                >
                  {item.label}
                </button>
              ))}
            </div>
          </nav>

          <div className="flex-1 overflow-auto p-6">
            <div className="flex max-w-[640px] flex-col gap-5">
              {tab === 'overview' && (
                <>
                  <FormField label={`${ta.name} *`}>
                    <input
                      className={INPUT_CLS}
                      value={form.name}
                      onChange={(e) => setForm((prev) => prev && { ...prev, name: e.target.value })}
                    />
                  </FormField>

                  <FormField label={ta.model}>
                    <select
                      className={SELECT_CLS}
                      value={form.model}
                      onChange={(e) => setForm((prev) => prev && { ...prev, model: e.target.value })}
                    >
                      <option value="" />
                      {selectedModelOptions.map((option) => (
                        <option key={option.value} value={option.value}>{option.label}</option>
                      ))}
                    </select>
                  </FormField>

                  <CheckboxField
                    checked={form.isActive}
                    onChange={(value) => setForm((prev) => prev && { ...prev, isActive: value })}
                    label={ta.active}
                  />

                  <FormField label={ta.temperature}>
                    <div className="flex items-center gap-3">
                      <input
                        type="range"
                        min={0}
                        max={2}
                        step={0.1}
                        value={form.temperature}
                        onChange={(e) => setForm((prev) => prev && { ...prev, temperature: Number(e.target.value) })}
                        className="flex-1"
                      />
                      <span className="w-8 text-right text-xs tabular-nums text-[var(--c-text-muted)]">
                        {form.temperature.toFixed(1)}
                      </span>
                    </div>
                  </FormField>

                  <FormField label={ta.maxOutputTokens}>
                    <input
                      type="number"
                      className={INPUT_CLS}
                      value={form.maxOutputTokens}
                      onChange={(e) => setForm((prev) => prev && { ...prev, maxOutputTokens: e.target.value })}
                    />
                  </FormField>

                  <FormField label={ta.reasoningMode}>
                    <select
                      className={SELECT_CLS}
                      value={form.reasoningMode}
                      onChange={(e) => setForm((prev) => prev && { ...prev, reasoningMode: e.target.value })}
                    >
                      {['auto', 'enabled', 'disabled', 'none'].map((value) => (
                        <option key={value} value={value}>{value}</option>
                      ))}
                    </select>
                  </FormField>

                  <CheckboxField
                    checked={form.streamThinking}
                    onChange={(v) => setForm((prev) => prev && { ...prev, streamThinking: v })}
                    label={ta.streamThinking}
                  />
                </>
              )}

              {tab === 'persona' && (
                <FormField label="prompt.md">
                  <textarea
                    className={`${MONO_CLS} min-h-[240px] resize-y`}
                    rows={10}
                    value={form.systemPrompt}
                    onChange={(e) => setForm((prev) => prev && { ...prev, systemPrompt: e.target.value })}
                  />
                </FormField>
              )}

              {tab === 'tools' && (
                catalogGroups.length > 0 ? (
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-3">
                      <div className="flex items-center gap-3">
                        <span className="text-xs font-medium text-[var(--c-text-muted)]">{ta.toolModeLabel}</span>
                        <div className="flex gap-1.5">
                          <button
                            type="button"
                            onClick={() => setToolSelectionMode('inherit')}
                            className={[
                              'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
                              form.toolSelectionMode === 'inherit'
                                ? 'bg-[var(--c-accent)]/12 text-[var(--c-text-primary)]'
                                : 'text-[var(--c-text-tertiary)] hover:text-[var(--c-text-secondary)]',
                            ].join(' ')}
                          >
                            {ta.toolModeInherit}
                          </button>
                          <button
                            type="button"
                            onClick={() => setToolSelectionMode('custom')}
                            className={[
                              'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
                              form.toolSelectionMode === 'custom'
                                ? 'bg-[var(--c-accent)]/12 text-[var(--c-text-primary)]'
                                : 'text-[var(--c-text-tertiary)] hover:text-[var(--c-text-secondary)]',
                            ].join(' ')}
                          >
                            {ta.toolModeCustom}
                          </button>
                        </div>
                      </div>
                      <span className="text-xs tabular-nums text-[var(--c-text-muted)]">
                        {ta.toolsSelected(selectedToolCount, allCatalogToolNames.length)}
                      </span>
                    </div>

                    <label className="flex items-center justify-between gap-3 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-3">
                      <div>
                        <p className="text-sm font-medium text-[var(--c-text-primary)]">{ta.toolDiscovery}</p>
                        <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{ta.toolDiscoveryDesc}</p>
                      </div>
                      <input
                        type="checkbox"
                        checked={form.toolDiscoveryEnabled}
                        onChange={(e) => setToolDiscoveryEnabled(e.target.checked)}
                        className="h-4 w-4 rounded border-[var(--c-border)] accent-[var(--c-accent)]"
                      />
                    </label>

                    {catalogGroups.map((group) => {
                      const groupNames = group.tools.map((tool) => tool.name)
                      const groupSelectedCount = form.toolSelectionMode === 'inherit'
                        ? groupNames.filter((n) => !form.toolDenylist.includes(n)).length
                        : groupNames.filter((n) => form.tools.includes(n)).length
                      return (
                        <div key={group.group}>
                          <div className="mb-1 flex items-center gap-2 px-1">
                            <span className="text-xs font-medium uppercase tracking-wide text-[var(--c-text-muted)]">
                              {group.group}
                            </span>
                            <span className="text-[10px] text-[var(--c-text-muted)]">
                              {groupSelectedCount}/{group.tools.length}
                            </span>
                            <button
                              type="button"
                              onClick={() => toggleToolGroup(group, true)}
                              disabled={groupSelectedCount === group.tools.length}
                              className="text-[10px] text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)] disabled:opacity-40"
                            >
                              {ta.groupEnableAll}
                            </button>
                            <button
                              type="button"
                              onClick={() => toggleToolGroup(group, false)}
                              disabled={groupSelectedCount === 0}
                              className="text-[10px] text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)] disabled:opacity-40"
                            >
                              {form.toolSelectionMode === 'inherit' ? ta.groupDisableAll : ta.groupClearAll}
                            </button>
                          </div>
                          <div className="grid gap-1 md:grid-cols-2">
                            {group.tools.map((tool) => {
                              const checked = form.toolSelectionMode === 'inherit'
                                ? !form.toolDenylist.includes(tool.name)
                                : form.tools.includes(tool.name)
                              return (
                                <ToolRow
                                  key={tool.name}
                                  tool={tool}
                                  checked={checked}
                                  isCore={form.coreTools.includes(tool.name)}
                                  showCoreStar={form.toolDiscoveryEnabled}
                                  onToggle={() => toggleTool(tool.name)}
                                  onToggleCore={() => toggleCoreTool(tool.name)}
                                />
                              )
                            })}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  <p className="text-sm text-[var(--c-text-muted)]">--</p>
                )
              )}
            </div>
          </div>
        </div>

        <ConfirmDialog
          open={deleteOpen}
          onClose={() => setDeleteOpen(false)}
          onConfirm={handleDelete}
          message={ta.deleteConfirm}
          confirmLabel={t.common.delete}
          loading={deleting}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={ta.title}
        actions={(
          <div className="flex items-center justify-end gap-2 whitespace-nowrap">
            <label className="shrink-0 text-xs text-[var(--c-text-muted)]">{ta.fieldScope}</label>
            <select
              value={scope}
              onChange={(e) => setScope(e.target.value as AgentScope)}
              className="w-[112px] rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1 text-xs text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]"
            >
              <option value="platform">{ta.scopePlatform}</option>
              <option value="user">{ta.scopeAccount}</option>
            </select>
            <button
              onClick={() => {
                setCreateOpen(true)
                setCreateName('')
                setCreateModel('')
              }}
              className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <Plus size={13} />
              {ta.newAgent}
            </button>
          </div>
        )}
      />

      <div className="flex flex-1 flex-col gap-4 overflow-auto p-4">
        {loading && agents.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <span className="text-sm text-[var(--c-text-muted)]">{t.common.loading}</span>
          </div>
        ) : agents.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <span className="text-sm text-[var(--c-text-muted)]">{ta.noAgents}</span>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {sortedAgents.map((agent) => (
              <button
                key={agent.id}
                onClick={() => selectAgent(agent)}
                className="flex flex-col gap-3 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-5 py-4 text-left transition-colors hover:border-[var(--c-border-focus)]"
              >
                <div className="flex items-start justify-between gap-2">
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                    {agent.display_name}
                  </h3>
                  <div className="flex shrink-0 items-center gap-1.5">
                    {agent.source === 'repo' && (
                      <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
                        {ta.builtIn}
                      </span>
                    )}
                    {agent.is_active && (
                      <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-500">
                        {ta.active}
                      </span>
                    )}
                  </div>
                </div>
                <AgentModelLine agent={agent} label={ta.model} hybridLabel={ta.hybrid} />
              </button>
            ))}
          </div>
        )}
      </div>

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title={ta.newAgent} width="420px">
        <div className="flex flex-col gap-4">
          <FormField label={`${ta.name} *`}>
            <input
              className={INPUT_CLS}
              value={createName}
              onChange={(e) => setCreateName(e.target.value)}
              autoFocus
            />
          </FormField>
          <FormField label={`${ta.model} *`}>
            <select
              className={SELECT_CLS}
              value={createModel}
              onChange={(e) => setCreateModel(e.target.value)}
            >
              <option value="" />
              {createModelOptions.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </FormField>
          <div className="flex justify-end gap-2 pt-2">
            <button
              onClick={() => setCreateOpen(false)}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              {t.common.cancel}
            </button>
            <button
              onClick={handleCreate}
              disabled={creating || !createName.trim() || !createModel.trim()}
              className="rounded-lg bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-accent-text)] transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {creating ? '...' : t.common.save}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
