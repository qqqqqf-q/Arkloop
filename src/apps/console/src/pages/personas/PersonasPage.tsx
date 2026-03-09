import { useState, useCallback, useEffect, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Plus, Trash2, ChevronLeft, Check, CheckCheck, Minus } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '../../components/useToast'
import { useLocale } from '../../contexts/LocaleContext'
import { isApiError } from '../../api'
import {
  listPersonas,
  createPersona,
  patchPersona,
  deletePersona,
  type Persona,
} from '../../api/personas'
import { listEffectiveToolCatalog, type ToolCatalogGroup, type ToolCatalogItem } from '../../api/tool-catalog'
import { notifyToolCatalogChanged, subscribeToolCatalogRefresh } from '../../lib/toolCatalogRefresh'

type DetailTab = 'overview' | 'prompt' | 'tools'
type ToolSelectionMode = 'inherit' | 'custom'

type DetailForm = {
  personaKey: string
  version: string
  displayName: string
  description: string
  model: string
  isActive: boolean
  reasoningMode: string
  promptCacheControl: string
  preferredCredential: string
  budgetsJSON: string
  executorType: string
  executorConfigJSON: string
  prompt: string
  toolSelectionMode: ToolSelectionMode
  tools: string[]
  toolDenylist: string[]
}

function personaToForm(persona: Persona): DetailForm {
  const allowlist = persona.tool_allowlist ?? []
  const denylist = persona.tool_denylist ?? []
  return {
    personaKey: persona.persona_key,
    version: persona.version,
    displayName: persona.display_name,
    description: persona.description ?? '',
    model: persona.model ?? '',
    isActive: persona.is_active,
    reasoningMode: persona.reasoning_mode || 'auto',
    promptCacheControl: persona.prompt_cache_control || 'none',
    preferredCredential: persona.preferred_credential ?? '',
    budgetsJSON: JSON.stringify(persona.budgets ?? {}, null, 2),
    executorType: persona.executor_type || 'agent.simple',
    executorConfigJSON: JSON.stringify(persona.executor_config ?? {}, null, 2),
    prompt: persona.prompt_md || '',
    toolSelectionMode: allowlist.length === 0 ? 'inherit' : 'custom',
    tools: allowlist,
    toolDenylist: denylist,
  }
}

function isHybridPersona(executorType: string): boolean {
  return executorType.trim() === 'agent.lua'
}

function tryParseJSONObject(raw: string): { ok: true; value: Record<string, unknown> } | { ok: false } {
  try {
    const parsed = JSON.parse(raw.trim() || '{}')
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return { ok: false }
    return { ok: true, value: parsed as Record<string, unknown> }
  } catch {
    return { ok: false }
  }
}

function uniqToolNames(names: string[]): string[] {
  return Array.from(new Set(names.map((n) => n.trim()).filter(Boolean)))
}

function ToolOptionCard({
  tool, checked, disabled, onToggle,
}: {
  tool: ToolCatalogItem
  checked: boolean
  disabled: boolean
  onToggle: () => void
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      disabled={disabled}
      className={[
        'flex w-full items-start gap-3 rounded-xl border px-4 py-3 text-left transition-colors',
        checked
          ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/8'
          : 'border-[var(--c-border)] bg-[var(--c-bg-sub)] hover:border-[var(--c-border-focus)]',
        disabled ? 'cursor-not-allowed opacity-60' : '',
      ].join(' ')}
    >
      <span
        className={[
          'mt-0.5 flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[5px] border transition-colors',
          checked
            ? 'border-[var(--c-accent)] bg-[var(--c-accent)] text-white'
            : 'border-[var(--c-border)] bg-[var(--c-bg-input)] text-transparent',
        ].join(' ')}
      >
        <Check size={12} strokeWidth={3} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium text-[var(--c-text-primary)]">{tool.label}</span>
        <span className="mt-0.5 block font-mono text-[10px] text-[var(--c-text-muted)]">{tool.name}</span>
        <span className="mt-1 block line-clamp-2 text-xs text-[var(--c-text-muted)]">{tool.llm_description}</span>
      </span>
    </button>
  )
}

const INPUT_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
const SELECT_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
const MONO_CLS =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-2 font-mono text-xs leading-relaxed text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'

export function PersonasPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.agents

  const [personas, setPersonas] = useState<Persona[]>([])
  const [catalogGroups, setCatalogGroups] = useState<ToolCatalogGroup[]>([])
  const [loading, setLoading] = useState(false)

  const [selected, setSelected] = useState<Persona | null>(null)
  const [tab, setTab] = useState<DetailTab>('overview')
  const [form, setForm] = useState<DetailForm | null>(null)
  const [saving, setSaving] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [createKey, setCreateKey] = useState('')
  const [createName, setCreateName] = useState('')
  const [creating, setCreating] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async (): Promise<Persona[]> => {
    setLoading(true)
    try {
      const [list, catalog] = await Promise.all([
        listPersonas(accessToken),
        listEffectiveToolCatalog(accessToken),
      ])
      setPersonas(list)
      setCatalogGroups(catalog.groups)
      return list
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
      return []
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void load()
    return subscribeToolCatalogRefresh(() => { void load() })
  }, [load])

  const allCatalogToolNames = useMemo(
    () => uniqToolNames(catalogGroups.flatMap((g) => g.tools.map((t) => t.name))),
    [catalogGroups],
  )

  const selectedToolCount = form
    ? (form.toolSelectionMode === 'inherit'
      ? allCatalogToolNames.filter((n) => !form.toolDenylist.includes(n)).length
      : form.tools.length)
    : 0

  const selectAgent = useCallback((persona: Persona) => {
    setSelected(persona)
    setForm(personaToForm(persona))
    setTab('overview')
  }, [])

  const goBack = useCallback(() => {
    setSelected(null)
    setForm(null)
  }, [])

  const handleCreate = useCallback(async () => {
    const key = createKey.trim()
    const name = createName.trim()
    if (!key || !name) return
    setCreating(true)
    try {
      const persona = await createPersona({
        persona_key: key,
        version: '1.0.0',
        display_name: name,
        prompt_md: name,
        tool_allowlist: [],
        tool_denylist: [],
        reasoning_mode: 'auto',
      }, accessToken)
      setCreateOpen(false)
      setCreateKey('')
      setCreateName('')
      notifyToolCatalogChanged()
      void load()
      selectAgent(persona)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setCreating(false)
    }
  }, [accessToken, addToast, createKey, createName, load, selectAgent, tc.toastSaveFailed])

  const handleSave = useCallback(async () => {
    if (!selected || !form || !form.displayName.trim()) return

    const budgets = tryParseJSONObject(form.budgetsJSON)
    if (!budgets.ok) { addToast(tc.errInvalidJSON, 'error'); return }
    const execConfig = tryParseJSONObject(form.executorConfigJSON)
    if (!execConfig.ok) { addToast(tc.errInvalidJSON, 'error'); return }

    setSaving(true)
    try {
      const payload = {
        display_name: form.displayName.trim(),
        description: form.description.trim() || undefined,
        prompt_md: form.prompt.trim(),
        model: form.model.trim() || undefined,
        tool_allowlist: form.toolSelectionMode === 'inherit' ? [] : form.tools,
        tool_denylist: form.toolSelectionMode === 'inherit' ? form.toolDenylist : [],
        budgets: budgets.value,
        is_active: form.isActive,
        preferred_credential: form.preferredCredential.trim() || undefined,
        reasoning_mode: form.reasoningMode.trim() || undefined,
        prompt_cache_control: form.promptCacheControl.trim() || undefined,
        executor_type: form.executorType.trim() || undefined,
        executor_config: execConfig.value,
      }

      const saved = selected.source === 'builtin'
        ? await createPersona({
            copy_from_repo_persona_key: selected.persona_key,
            persona_key: form.personaKey.trim(),
            version: form.version.trim(),
            ...payload,
          }, accessToken)
        : await patchPersona(selected.id, payload, accessToken)

      addToast(tc.toastUpdated, 'success')
      notifyToolCatalogChanged()
      const fresh = await load()
      const updated = fresh.find((p) => p.id === saved.id)
        ?? fresh.find((p) => p.persona_key === saved.persona_key && p.source === 'custom')
      if (updated) {
        setSelected(updated)
        setForm(personaToForm(updated))
      }
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, form, load, selected, tc])

  const handleDelete = useCallback(async () => {
    if (!selected) return
    setDeleting(true)
    try {
      await deletePersona(selected.id, accessToken)
      setDeleteOpen(false)
      goBack()
      void load()
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [accessToken, addToast, goBack, load, selected, tc.toastSaveFailed])

  const replaceTools = useCallback((tools: string[]) => {
    setForm((prev) => (prev ? { ...prev, toolSelectionMode: 'custom', tools: uniqToolNames(tools) } : prev))
  }, [])

  const replaceDeniedTools = useCallback((tools: string[]) => {
    setForm((prev) => (prev ? { ...prev, toolDenylist: uniqToolNames(tools) } : prev))
  }, [])

  const toggleTool = useCallback((key: string) => {
    setForm((prev) => (
      !prev
        ? prev
        : prev.toolSelectionMode === 'inherit'
          ? {
              ...prev,
              toolDenylist: prev.toolDenylist.includes(key)
                ? prev.toolDenylist.filter((n) => n !== key)
                : uniqToolNames([...prev.toolDenylist, key]),
            }
          : {
              ...prev,
              toolSelectionMode: 'custom',
              tools: prev.tools.includes(key)
                ? prev.tools.filter((n) => n !== key)
                : uniqToolNames([...prev.tools, key]),
            }
    ))
  }, [])

  const toggleToolGroup = useCallback((group: ToolCatalogGroup, enabled: boolean) => {
    setForm((prev) => {
      if (!prev) return prev
      const groupNames = group.tools.map((t) => t.name)
      if (prev.toolSelectionMode === 'inherit') {
        return {
          ...prev,
          toolDenylist: enabled
            ? prev.toolDenylist.filter((n) => !groupNames.includes(n))
            : uniqToolNames([...prev.toolDenylist, ...groupNames]),
        }
      }
      return {
        ...prev,
        toolSelectionMode: 'custom',
        tools: enabled
          ? uniqToolNames([...prev.tools, ...groupNames])
          : prev.tools.filter((n) => !groupNames.includes(n)),
      }
    })
  }, [])

  const setToolSelectionMode = useCallback((mode: ToolSelectionMode) => {
    setForm((prev) => {
      if (!prev || prev.toolSelectionMode === mode) return prev
      if (mode === 'inherit') return { ...prev, toolSelectionMode: mode }
      const nextTools = prev.tools.length > 0
        ? prev.tools
        : prev.toolSelectionMode === 'inherit'
          ? allCatalogToolNames.filter((n) => !prev.toolDenylist.includes(n))
          : allCatalogToolNames
      return { ...prev, toolSelectionMode: mode, tools: uniqToolNames(nextTools) }
    })
  }, [allCatalogToolNames])

  const sortedPersonas = useMemo(
    () => [...personas].sort((a, b) => {
      if (a.source !== b.source) return a.source === 'builtin' ? -1 : 1
      return a.display_name.localeCompare(b.display_name)
    }),
    [personas],
  )

  const isBuiltIn = selected?.source === 'builtin'

  // ── Detail view ──
  if (selected && form) {
    const tabs: { key: DetailTab; label: string }[] = [
      { key: 'overview', label: tc.tabOverview },
      { key: 'prompt', label: tc.tabPrompt },
      { key: 'tools', label: tc.tabTools },
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
              {selected.source === 'builtin' && (
                <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
                  {tc.builtIn}
                </span>
              )}
              {selected.is_active && (
                <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-500">
                  {tc.active}
                </span>
              )}
            </div>
          )}
          actions={(
            <div className="flex items-center gap-2">
              {!isBuiltIn && (
                <button
                  onClick={() => setDeleteOpen(true)}
                  className="flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-xs text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-red-500"
                >
                  <Trash2 size={13} />
                  {tc.cancel === '取消' ? '删除' : 'Delete'}
                </button>
              )}
              <button
                onClick={handleSave}
                disabled={saving || !form.displayName.trim()}
                className="rounded-lg bg-[var(--c-accent)] px-3.5 py-1.5 text-xs font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
              >
                {saving ? '...' : tc.save}
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
                  <div className="grid grid-cols-2 gap-4">
                    <FormField label={tc.fieldPersonaKey}>
                      <input
                        className={INPUT_CLS}
                        value={form.personaKey}
                        readOnly={!isBuiltIn && selected.source === 'custom'}
                        onChange={(e) => setForm((prev) => prev && { ...prev, personaKey: e.target.value })}
                      />
                    </FormField>
                    <FormField label={tc.fieldVersion}>
                      <input
                        className={INPUT_CLS}
                        value={form.version}
                        readOnly={!isBuiltIn && selected.source === 'custom'}
                        onChange={(e) => setForm((prev) => prev && { ...prev, version: e.target.value })}
                      />
                    </FormField>
                  </div>

                  <FormField label={`${tc.fieldDisplayName} *`}>
                    <input
                      className={INPUT_CLS}
                      value={form.displayName}
                      onChange={(e) => setForm((prev) => prev && { ...prev, displayName: e.target.value })}
                    />
                  </FormField>

                  <FormField label={tc.fieldDescription}>
                    <input
                      className={INPUT_CLS}
                      value={form.description}
                      onChange={(e) => setForm((prev) => prev && { ...prev, description: e.target.value })}
                    />
                  </FormField>

                  <FormField label={tc.fieldModel}>
                    <input
                      className={INPUT_CLS}
                      value={form.model}
                      onChange={(e) => setForm((prev) => prev && { ...prev, model: e.target.value })}
                      placeholder="provider^model"
                    />
                  </FormField>

                  <div className="grid grid-cols-2 gap-4">
                    <FormField label={tc.fieldReasoningMode}>
                      <select
                        className={SELECT_CLS}
                        value={form.reasoningMode}
                        onChange={(e) => setForm((prev) => prev && { ...prev, reasoningMode: e.target.value })}
                      >
                        {['auto', 'enabled', 'disabled', 'none'].map((v) => (
                          <option key={v} value={v}>{v}</option>
                        ))}
                      </select>
                    </FormField>
                    <FormField label={tc.fieldPromptCacheControl}>
                      <select
                        className={SELECT_CLS}
                        value={form.promptCacheControl}
                        onChange={(e) => setForm((prev) => prev && { ...prev, promptCacheControl: e.target.value })}
                      >
                        <option value="none">none</option>
                        <option value="system_prompt">system_prompt</option>
                      </select>
                    </FormField>
                  </div>

                  <div className="grid grid-cols-2 gap-4">
                    <FormField label={tc.fieldPreferredCredential}>
                      <input
                        className={INPUT_CLS}
                        value={form.preferredCredential}
                        onChange={(e) => setForm((prev) => prev && { ...prev, preferredCredential: e.target.value })}
                      />
                    </FormField>
                    <FormField label={tc.fieldExecutorType}>
                      <input
                        className={INPUT_CLS}
                        value={form.executorType}
                        onChange={(e) => setForm((prev) => prev && { ...prev, executorType: e.target.value })}
                        placeholder="agent.simple"
                      />
                    </FormField>
                  </div>

                  <label className="flex cursor-pointer select-none items-center gap-2.5 text-sm text-[var(--c-text-secondary)]">
                    <input
                      type="checkbox"
                      checked={form.isActive}
                      onChange={(e) => setForm((prev) => prev && { ...prev, isActive: e.target.checked })}
                      className="sr-only"
                    />
                    <span
                      className={[
                        'flex h-[16px] w-[16px] shrink-0 items-center justify-center rounded-[4px] border transition-colors',
                        form.isActive
                          ? 'border-[var(--c-accent)] bg-[var(--c-accent)]'
                          : 'border-[var(--c-border)] bg-[var(--c-bg-input)]',
                      ].join(' ')}
                    >
                      {form.isActive && <Check size={11} className="text-white" strokeWidth={3} />}
                    </span>
                    {tc.fieldIsActive}
                  </label>

                  <FormField label={tc.fieldBudgetsJSON}>
                    <textarea
                      className={`${MONO_CLS} min-h-[80px] resize-y`}
                      rows={3}
                      value={form.budgetsJSON}
                      onChange={(e) => setForm((prev) => prev && { ...prev, budgetsJSON: e.target.value })}
                    />
                  </FormField>

                  <FormField label={tc.fieldExecutorConfig}>
                    <textarea
                      className={`${MONO_CLS} min-h-[80px] resize-y`}
                      rows={3}
                      value={form.executorConfigJSON}
                      onChange={(e) => setForm((prev) => prev && { ...prev, executorConfigJSON: e.target.value })}
                    />
                  </FormField>
                </>
              )}

              {tab === 'prompt' && (
                <FormField label={tc.fieldPrompt}>
                  <textarea
                    className={`${MONO_CLS} min-h-[240px] resize-y`}
                    rows={10}
                    value={form.prompt}
                    onChange={(e) => setForm((prev) => prev && { ...prev, prompt: e.target.value })}
                  />
                </FormField>
              )}

              {tab === 'tools' && (
                catalogGroups.length > 0 ? (
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-3 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-3">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div>
                          <p className="text-xs font-medium uppercase tracking-wide text-[var(--c-text-muted)]">{tc.tabTools}</p>
                          <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{tc.toolsSelected(selectedToolCount, allCatalogToolNames.length)}</p>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          <button
                            type="button"
                            onClick={() => setToolSelectionMode('inherit')}
                            className={[
                              'rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors',
                              form.toolSelectionMode === 'inherit'
                                ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/10 text-[var(--c-text-primary)]'
                                : 'border-[var(--c-border)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-card)]',
                            ].join(' ')}
                          >
                            {tc.toolModeInherit}
                          </button>
                          <button
                            type="button"
                            onClick={() => setToolSelectionMode('custom')}
                            className={[
                              'rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors',
                              form.toolSelectionMode === 'custom'
                                ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/10 text-[var(--c-text-primary)]'
                                : 'border-[var(--c-border)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-card)]',
                            ].join(' ')}
                          >
                            {tc.toolModeCustom}
                          </button>
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <p className="text-sm text-[var(--c-text-secondary)]">{tc.toolModeLabel}</p>
                        <div className="flex flex-wrap gap-2">
                          <button
                            type="button"
                            onClick={() => {
                              if (form.toolSelectionMode === 'inherit') { replaceDeniedTools([]); return }
                              replaceTools(allCatalogToolNames)
                            }}
                            disabled={form.toolSelectionMode === 'inherit'
                              ? form.toolDenylist.length === 0
                              : allCatalogToolNames.length === 0 || form.tools.length === allCatalogToolNames.length}
                            className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                          >
                            <CheckCheck size={13} />
                            {tc.enableAllTools}
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              if (form.toolSelectionMode === 'inherit') { replaceDeniedTools(allCatalogToolNames); return }
                              replaceTools([])
                            }}
                            disabled={form.toolSelectionMode === 'inherit'
                              ? allCatalogToolNames.length === 0 || form.toolDenylist.length === allCatalogToolNames.length
                              : form.tools.length === 0}
                            className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                          >
                            <Minus size={13} />
                            {form.toolSelectionMode === 'inherit' ? tc.disableAllTools : tc.clearAllTools}
                          </button>
                        </div>
                      </div>
                    </div>
                    {catalogGroups.map((group) => {
                      const groupNames = group.tools.map((t) => t.name)
                      const groupSelectedCount = form.toolSelectionMode === 'inherit'
                        ? groupNames.filter((n) => !form.toolDenylist.includes(n)).length
                        : groupNames.filter((n) => form.tools.includes(n)).length
                      return (
                        <div key={group.group} className="flex flex-col gap-3 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-4">
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <div>
                              <p className="text-xs font-medium uppercase tracking-wide text-[var(--c-text-muted)]">{group.group}</p>
                              <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{tc.toolsSelected(groupSelectedCount, group.tools.length)}</p>
                            </div>
                            <div className="flex flex-wrap gap-2">
                              <button
                                type="button"
                                onClick={() => toggleToolGroup(group, true)}
                                disabled={group.tools.length === 0 || groupSelectedCount === group.tools.length}
                                className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                              >
                                {tc.groupEnableAll}
                              </button>
                              <button
                                type="button"
                                onClick={() => toggleToolGroup(group, false)}
                                disabled={groupSelectedCount === 0}
                                className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                              >
                                {form.toolSelectionMode === 'inherit' ? tc.groupDisableAll : tc.groupClearAll}
                              </button>
                            </div>
                          </div>
                          <div className="grid gap-3 md:grid-cols-2">
                            {group.tools.map((tool) => (
                              <ToolOptionCard
                                key={tool.name}
                                tool={tool}
                                checked={form.toolSelectionMode === 'inherit'
                                  ? !form.toolDenylist.includes(tool.name)
                                  : form.tools.includes(tool.name)}
                                disabled={false}
                                onToggle={() => toggleTool(tool.name)}
                              />
                            ))}
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
          message={tc.deleteConfirm}
          confirmLabel={tc.cancel === '取消' ? '删除' : 'Delete'}
          loading={deleting}
        />
      </div>
    )
  }

  // ── Card grid view ──
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={(
          <button
            onClick={() => {
              setCreateOpen(true)
              setCreateKey('')
              setCreateName('')
            }}
            className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            <Plus size={13} />
            {tc.newAgent}
          </button>
        )}
      />

      <div className="flex flex-1 flex-col gap-4 overflow-auto p-4">
        {loading && personas.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <span className="text-sm text-[var(--c-text-muted)]">...</span>
          </div>
        ) : personas.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <span className="text-sm text-[var(--c-text-muted)]">{tc.noAgents}</span>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {sortedPersonas.map((persona) => (
              <button
                key={persona.id}
                onClick={() => selectAgent(persona)}
                className="flex flex-col gap-3 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-card)] px-5 py-4 text-left transition-colors hover:border-[var(--c-border-focus)]"
              >
                <div className="flex items-start justify-between gap-2">
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                    {persona.display_name}
                  </h3>
                  <div className="flex shrink-0 items-center gap-1.5">
                    {persona.source === 'builtin' && (
                      <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
                        {tc.builtIn}
                      </span>
                    )}
                    {persona.is_active && (
                      <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-500">
                        {tc.active}
                      </span>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-1.5 text-xs text-[var(--c-text-muted)]">
                  <span className="min-w-0 flex-1 truncate" title={persona.model ?? tc.valuePlatformDefault}>
                    {persona.model?.trim() || tc.valuePlatformDefault}
                  </span>
                  {isHybridPersona(persona.executor_type) && (
                    <Badge variant="warning">{tc.labelHybrid}</Badge>
                  )}
                </div>
              </button>
            ))}
          </div>
        )}
      </div>

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title={tc.newAgent} width="420px">
        <div className="flex flex-col gap-4">
          <FormField label={`${tc.fieldPersonaKey} *`}>
            <input
              className={INPUT_CLS}
              value={createKey}
              onChange={(e) => setCreateKey(e.target.value)}
              placeholder="my_agent"
              autoFocus
            />
          </FormField>
          <FormField label={`${tc.fieldDisplayName} *`}>
            <input
              className={INPUT_CLS}
              value={createName}
              onChange={(e) => setCreateName(e.target.value)}
            />
          </FormField>
          <div className="flex justify-end gap-2 pt-2">
            <button
              onClick={() => setCreateOpen(false)}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleCreate}
              disabled={creating || !createKey.trim() || !createName.trim()}
              className="rounded-lg bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-white transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {creating ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
