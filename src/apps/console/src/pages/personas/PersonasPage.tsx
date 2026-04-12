import { useState, useCallback, useEffect, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Plus, Trash2, ChevronLeft, Check } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { AutoResizeTextarea, useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { formatDateTime as formatDateTimeWithZone } from '@arkloop/shared'
import { isApiError } from '../../api'
import {
  listPersonas,
  createPersona,
  patchPersona,
  deletePersona,
  type Persona,
  type PersonaScope,
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
  luaScript: string
  toolSelectionMode: ToolSelectionMode
  tools: string[]
  toolDenylist: string[]
  coreTools: string[]
  toolDiscoveryEnabled: boolean
}

function stripLuaScript(config: Record<string, unknown>): Record<string, unknown> {
  const next = { ...config }
  delete next.script
  delete next.script_file
  return next
}

function readLuaScript(config: Record<string, unknown>): string {
  const raw = config.script
  return typeof raw === 'string' ? raw : ''
}

function buildExecutorConfig(form: DetailForm, config: Record<string, unknown>): Record<string, unknown> {
  if (!isHybridPersona(form.executorType)) {
    return config
  }
  const next = stripLuaScript(config)
  next.script = form.luaScript
  return next
}

function formatSyncMode(persona: Persona, tc: ReturnType<typeof useLocale>['t']['pages']['agents']): string {
  switch (persona.sync_mode) {
    case 'platform_file_mirror':
      return tc.syncModePlatformFileMirror
    case 'none':
      return tc.syncModeNone
    default:
      return tc.valueNotSet
  }
}

function formatDateTime(value?: string): string {
  if (!value) return '--'
  return formatDateTimeWithZone(value)
}

function personaToForm(persona: Persona): DetailForm {
  const allowlist = persona.tool_allowlist ?? []
  const denylist = persona.tool_denylist ?? []
  const coreTools = persona.core_tools ?? []
  const executorConfig = persona.executor_config ?? {}
  return {
    personaKey: persona.persona_key,
    version: persona.version,
    displayName: persona.display_name,
    description: persona.description ?? '',
    model: persona.model ?? '',
    isActive: persona.is_active,
    reasoningMode: persona.reasoning_mode || 'auto',
    promptCacheControl: persona.prompt_cache_control || 'system_prompt',
    preferredCredential: persona.preferred_credential ?? '',
    budgetsJSON: JSON.stringify(persona.budgets ?? {}, null, 2),
    executorType: persona.executor_type || 'agent.simple',
    executorConfigJSON: JSON.stringify(stripLuaScript(executorConfig), null, 2),
    prompt: persona.prompt_md || '',
    luaScript: readLuaScript(executorConfig),
    toolSelectionMode: allowlist.length === 0 ? 'inherit' : 'custom',
    tools: allowlist,
    toolDenylist: denylist,
    coreTools,
    toolDiscoveryEnabled: coreTools.length > 0,
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

export function PersonasPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.agents

  const [personas, setPersonas] = useState<Persona[]>([])
  const [scope, setScope] = useState<PersonaScope>('platform')
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
        listPersonas(accessToken, scope),
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
  }, [accessToken, addToast, scope, tc.toastLoadFailed])

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
        scope,
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
  }, [accessToken, addToast, createKey, createName, load, scope, selectAgent, tc.toastSaveFailed])

  const handleSave = useCallback(async () => {
    if (!selected || !form || !form.displayName.trim()) return

    const budgets = tryParseJSONObject(form.budgetsJSON)
    if (!budgets.ok) { addToast(tc.errInvalidJSON, 'error'); return }
    const execConfig = tryParseJSONObject(form.executorConfigJSON)
    if (!execConfig.ok) { addToast(tc.errInvalidJSON, 'error'); return }
    const executorConfig = buildExecutorConfig(form, execConfig.value)
    const originalLuaScript = typeof selected.executor_config?.script === 'string' ? selected.executor_config.script : ''
    const shouldSendExecutorConfig = !(
      selected.source === 'builtin'
      && isHybridPersona(form.executorType)
      && !form.luaScript.trim()
      && !originalLuaScript.trim()
    )

    setSaving(true)
    try {
      const payload = {
        display_name: form.displayName.trim(),
        description: form.description.trim() || undefined,
        prompt_md: form.prompt.trim(),
        model: form.model.trim() || undefined,
        tool_allowlist: form.toolSelectionMode === 'inherit' ? [] : form.tools,
        tool_denylist: form.toolSelectionMode === 'inherit' ? form.toolDenylist : [],
        core_tools: form.toolDiscoveryEnabled ? form.coreTools : [],
        budgets: budgets.value,
        is_active: form.isActive,
        preferred_credential: form.preferredCredential.trim() || undefined,
        reasoning_mode: form.reasoningMode.trim() || undefined,
        prompt_cache_control: form.promptCacheControl.trim() || undefined,
        executor_type: form.executorType.trim() || undefined,
        executor_config: shouldSendExecutorConfig ? executorConfig : undefined,
      }

      const saved = selected.source === 'builtin'
        ? await createPersona({
            copy_from_repo_persona_key: selected.persona_key,
            scope,
            persona_key: form.personaKey.trim(),
            version: form.version.trim(),
            ...payload,
          }, accessToken)
        : await patchPersona(selected.id, { ...payload, scope }, accessToken)

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
  }, [accessToken, addToast, form, load, scope, selected, tc])

  const handleDelete = useCallback(async () => {
    if (!selected) return
    setDeleting(true)
    try {
      await deletePersona(selected.id, scope, accessToken)
      setDeleteOpen(false)
      goBack()
      void load()
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [accessToken, addToast, goBack, load, scope, selected, tc.toastSaveFailed])

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
                className="rounded-lg bg-[var(--c-accent)] px-3.5 py-1.5 text-xs font-medium text-[var(--c-accent-text)] transition-colors hover:opacity-90 disabled:opacity-50"
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
                      {form.isActive && <Check size={11} className="text-[var(--c-accent-text)]" strokeWidth={3} />}
                    </span>
                    {tc.fieldIsActive}
                  </label>

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

                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <FormField label={tc.fieldSyncMode}>
                      <input
                        className={INPUT_CLS}
                        value={formatSyncMode(selected, tc)}
                        readOnly
                      />
                    </FormField>
                    <FormField label={tc.fieldLastSyncedAt}>
                      <input
                        className={INPUT_CLS}
                        value={formatDateTime(selected.last_synced_at)}
                        readOnly
                      />
                    </FormField>
                  </div>

                  <FormField label={tc.fieldMirroredFilePath}>
                    <input
                      className={INPUT_CLS}
                      value={selected.mirrored_file_path || tc.valueNotSet}
                      readOnly
                    />
                  </FormField>


                  <FormField label={tc.fieldBudgetsJSON}>
                    <AutoResizeTextarea
                      className={`${MONO_CLS} min-h-[80px]`}
                      rows={3}
                      minRows={3}
                      maxHeight={260}
                      value={form.budgetsJSON}
                      onChange={(e) => setForm((prev) => prev && { ...prev, budgetsJSON: e.target.value })}
                    />
                  </FormField>

                  <FormField label={tc.fieldExecutorConfig}>
                    <AutoResizeTextarea
                      className={`${MONO_CLS} min-h-[80px]`}
                      rows={3}
                      minRows={3}
                      maxHeight={260}
                      value={form.executorConfigJSON}
                      onChange={(e) => setForm((prev) => prev && { ...prev, executorConfigJSON: e.target.value })}
                    />
                  </FormField>
                </>
              )}

              {tab === 'prompt' && (
                <div className="flex flex-col gap-5">
                  <FormField label={tc.fieldPrompt}>
                    <AutoResizeTextarea
                      className={`${MONO_CLS} min-h-[240px]`}
                      rows={10}
                      minRows={10}
                      maxHeight={520}
                      value={form.prompt}
                      onChange={(e) => setForm((prev) => prev && { ...prev, prompt: e.target.value })}
                    />
                  </FormField>
                  {isHybridPersona(form.executorType) && (
                    <FormField label={tc.fieldLuaScript}>
                      <AutoResizeTextarea
                        className={`${MONO_CLS} min-h-[240px]`}
                        rows={10}
                        minRows={10}
                        maxHeight={520}
                        value={form.luaScript}
                        onChange={(e) => setForm((prev) => prev && { ...prev, luaScript: e.target.value })}
                      />
                    </FormField>
                  )}
                </div>
              )}

              {tab === 'tools' && (
                catalogGroups.length > 0 ? (
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-3">
                      <div className="flex items-center gap-3">
                        <span className="text-xs font-medium text-[var(--c-text-muted)]">{tc.toolModeLabel}</span>
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
                            {tc.toolModeInherit}
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
                            {tc.toolModeCustom}
                          </button>
                        </div>
                      </div>
                      <span className="text-xs tabular-nums text-[var(--c-text-muted)]">
                        {tc.toolsSelected(selectedToolCount, allCatalogToolNames.length)}
                      </span>
                    </div>

                    <label className="flex items-center justify-between gap-3 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-3">
                      <div>
                        <p className="text-sm font-medium text-[var(--c-text-primary)]">{tc.toolDiscovery}</p>
                        <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{tc.toolDiscoveryDesc}</p>
                      </div>
                      <input
                        type="checkbox"
                        checked={form.toolDiscoveryEnabled}
                        onChange={(e) => setToolDiscoveryEnabled(e.target.checked)}
                        className="h-4 w-4 rounded border-[var(--c-border)] accent-[var(--c-accent)]"
                      />
                    </label>

                    {catalogGroups.map((group) => {
                      const groupNames = group.tools.map((t) => t.name)
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
                              {tc.groupEnableAll}
                            </button>
                            <button
                              type="button"
                              onClick={() => toggleToolGroup(group, false)}
                              disabled={groupSelectedCount === 0}
                              className="text-[10px] text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)] disabled:opacity-40"
                            >
                              {form.toolSelectionMode === 'inherit' ? tc.groupDisableAll : tc.groupClearAll}
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
          <div className="flex items-center justify-end gap-2 whitespace-nowrap">
            <label className="shrink-0 text-xs text-[var(--c-text-muted)]">{tc.fieldScope}</label>
            <select
              value={scope}
              onChange={(e) => setScope(e.target.value as PersonaScope)}
              className="w-[112px] rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1 text-xs text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]"
            >
              <option value="platform">{tc.scopePlatform}</option>
              <option value="user">{tc.scopeAccount}</option>
            </select>
            <button
              onClick={() => {
                setCreateOpen(true)
                setCreateKey('')
                setCreateName('')
              }}
              className="flex shrink-0 items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <Plus size={13} />
              {tc.newAgent}
            </button>
          </div>
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
                <div className="grid gap-1 text-[11px] text-[var(--c-text-muted)]">
                  <div className="flex items-center justify-between gap-2">
                    <span>{tc.fieldSyncMode}</span>
                    <span className="truncate text-right">{formatSyncMode(persona, tc)}</span>
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <span>{tc.fieldMirroredFilePath}</span>
                    <span className="truncate text-right" title={persona.mirrored_file_path ?? tc.valueNotSet}>
                      {persona.mirrored_file_path ?? tc.valueNotSet}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <span>{tc.fieldLastSyncedAt}</span>
                    <span className="truncate text-right">{formatDateTime(persona.last_synced_at)}</span>
                  </div>
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
              className="rounded-lg bg-[var(--c-accent)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-accent-text)] transition-colors hover:opacity-90 disabled:opacity-50"
            >
              {creating ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
