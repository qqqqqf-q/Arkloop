import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Plus, Pencil, Zap, Check, CheckCheck, Minus } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { useToast } from '../../components/useToast'
import { useLocale } from '../../contexts/LocaleContext'
import { isApiError } from '../../api'
import {
  listPersonas,
  createPersona,
  patchPersona,
  type Persona,
} from '../../api/personas'
import { listEffectiveToolCatalog, type ToolCatalogGroup, type ToolCatalogItem } from '../../api/tool-catalog'
import { notifyToolCatalogChanged, subscribeToolCatalogRefresh } from '../../lib/toolCatalogRefresh'

type ToolSelectionMode = 'inherit' | 'custom'

type PersonaFormState = {
  personaKey: string
  version: string
  displayName: string
  description: string
  prompt: string
  model: string
  toolAllowlistMode: ToolSelectionMode
  toolAllowlist: string
  toolDenylist: string
  budgetsJSON: string
  isActive: boolean
  executorType: string
  executorConfigJSON: string
  preferredCredential: string
  reasoningMode: string
  promptCacheControl: string
}

function emptyPersonaForm(): PersonaFormState {
  return {
    personaKey: '',
    version: '1.0.0',
    displayName: '',
    description: '',
    prompt: '',
    model: '',
    toolAllowlistMode: 'inherit',
    toolAllowlist: '',
    toolDenylist: '',
    budgetsJSON: '{}',
    isActive: true,
    executorType: 'agent.simple',
    executorConfigJSON: '{}',
    preferredCredential: '',
    reasoningMode: 'auto',
    promptCacheControl: 'none',
  }
}

function personaToForm(persona: Persona): PersonaFormState {
  const allowlist = persona.tool_allowlist ?? []
  return {
    personaKey: persona.persona_key,
    version: persona.version,
    displayName: persona.display_name,
    description: persona.description ?? '',
    prompt: persona.prompt_md,
    model: persona.model ?? '',
    toolAllowlistMode: allowlist.length === 0 ? 'inherit' : 'custom',
    toolAllowlist: allowlist.join(', '),
    toolDenylist: persona.tool_denylist.join(', '),
    budgetsJSON: JSON.stringify(persona.budgets ?? {}, null, 2),
    isActive: persona.is_active,
    executorType: persona.executor_type || 'agent.simple',
    executorConfigJSON: JSON.stringify(persona.executor_config ?? {}, null, 2),
    preferredCredential: persona.preferred_credential ?? '',
    reasoningMode: persona.reasoning_mode || 'auto',
    promptCacheControl: persona.prompt_cache_control || 'none',
  }
}

function parseToolList(raw: string): string[] {
  return raw
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function uniqToolNames(names: string[]): string[] {
  return Array.from(new Set(names.map((item) => item.trim()).filter(Boolean)))
}

function formatToolList(names: string[]): string {
  return uniqToolNames(names).join(', ')
}

function ToolOptionCard({
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
          : 'border-[var(--c-border)] bg-[var(--c-bg-sub)] hover:border-[var(--c-border-focus)]',
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

function tryParseJSONObject(raw: string): { ok: true; value: Record<string, unknown> } | { ok: false } {
  try {
    const parsed = JSON.parse(raw.trim() || '{}')
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      return { ok: false }
    }
    return { ok: true, value: parsed as Record<string, unknown> }
  } catch {
    return { ok: false }
  }
}

function isHybridPersona(executorType: string): boolean {
  return executorType.trim() === 'agent.lua'
}

function ModelValue({
  persona,
  platformDefaultLabel,
  hybridLabel,
  textClassName = 'font-mono text-xs text-[var(--c-text-primary)]',
}: {
  persona: Pick<Persona, 'model' | 'executor_type'>
  platformDefaultLabel: string
  hybridLabel: string
  textClassName?: string
}) {
  const model = persona.model?.trim() || platformDefaultLabel
  const textTone = persona.model?.trim() ? 'text-[var(--c-text-primary)]' : 'text-[var(--c-text-muted)]'

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className={`${textClassName} ${textTone}`}>{model}</span>
      {isHybridPersona(persona.executor_type) && <Badge variant="warning">{hybridLabel}</Badge>}
    </div>
  )
}

export function PersonasPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.personas

  const [personas, setPersonas] = useState<Persona[]>([])
  const [catalogGroups, setCatalogGroups] = useState<ToolCatalogGroup[]>([])
  const [loading, setLoading] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<PersonaFormState>(emptyPersonaForm)
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  const [editTarget, setEditTarget] = useState<Persona | null>(null)
  const [editForm, setEditForm] = useState<PersonaFormState>(emptyPersonaForm)
  const [editError, setEditError] = useState('')
  const [saving, setSaving] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const [list, catalog] = await Promise.all([
        listPersonas(accessToken),
        listEffectiveToolCatalog(accessToken),
      ])
      setPersonas(list)
      setCatalogGroups(catalog.groups)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void fetchAll()
    return subscribeToolCatalogRefresh(() => {
      void fetchAll()
    })
  }, [fetchAll])

  const setCreateField = useCallback(
    <K extends keyof PersonaFormState>(key: K, value: PersonaFormState[K]) => {
      setCreateForm((prev) => ({ ...prev, [key]: value }))
      setCreateError('')
    },
    [],
  )

  const setEditField = useCallback(
    <K extends keyof PersonaFormState>(key: K, value: PersonaFormState[K]) => {
      setEditForm((prev) => ({ ...prev, [key]: value }))
      setEditError('')
    },
    [],
  )

  const openCreate = useCallback(() => {
    setCreateForm(emptyPersonaForm())
    setCreateError('')
    setCreateOpen(true)
  }, [])

  const openEdit = useCallback((persona: Persona) => {
    setEditTarget(persona)
    setEditForm(personaToForm(persona))
    setEditError('')
  }, [])

  const closeCreate = useCallback(() => {
    if (creating) return
    setCreateOpen(false)
  }, [creating])

  const closeEdit = useCallback(() => {
    if (saving) return
    setEditTarget(null)
  }, [saving])

  const buildPayload = useCallback((form: PersonaFormState) => {
    const budgetsParsed = tryParseJSONObject(form.budgetsJSON)
    if (!budgetsParsed.ok) {
      return { ok: false as const, message: tc.errInvalidJSON }
    }

    const executorConfigParsed = tryParseJSONObject(form.executorConfigJSON)
    if (!executorConfigParsed.ok) {
      return { ok: false as const, message: tc.errInvalidJSON }
    }

    return {
      ok: true as const,
      value: {
        display_name: form.displayName.trim(),
        description: form.description.trim() || undefined,
        prompt_md: form.prompt.trim(),
        model: form.model.trim() || undefined,
        tool_allowlist: form.toolAllowlistMode === 'inherit' ? [] : parseToolList(form.toolAllowlist),
        tool_denylist: parseToolList(form.toolDenylist),
        budgets: budgetsParsed.value,
        is_active: form.isActive,
        preferred_credential: form.preferredCredential.trim() || undefined,
        reasoning_mode: form.reasoningMode.trim() || undefined,
        prompt_cache_control: form.promptCacheControl.trim() || undefined,
        executor_type: form.executorType.trim() || undefined,
        executor_config: executorConfigParsed.value,
      },
    }
  }, [tc.errInvalidJSON])

  const handleCreate = useCallback(async () => {
    const personaKey = createForm.personaKey.trim()
    const version = createForm.version.trim()
    const displayName = createForm.displayName.trim()
    const prompt = createForm.prompt.trim()

    if (!personaKey || !version || !displayName || !prompt) {
      setCreateError(tc.errRequired)
      return
    }

    const payload = buildPayload(createForm)
    if (!payload.ok) {
      setCreateError(payload.message)
      return
    }

    setCreating(true)
    setCreateError('')
    try {
      await createPersona(
        {
          persona_key: personaKey,
          version,
          ...payload.value,
        },
        accessToken,
      )
      addToast(tc.toastCreated, 'success')
      setCreateOpen(false)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastSaveFailed)
    } finally {
      setCreating(false)
    }
  }, [accessToken, addToast, buildPayload, createForm, fetchAll, tc])

  const handleSave = useCallback(async () => {
    if (!editTarget) return
    if (!editForm.displayName.trim()) {
      setEditError(tc.errRequired)
      return
    }

    const payload = buildPayload(editForm)
    if (!payload.ok) {
      setEditError(payload.message)
      return
    }

    setSaving(true)
    setEditError('')
    try {
      if (editTarget.source === 'builtin') {
        await createPersona({
          copy_from_repo_persona_key: editTarget.persona_key,
          persona_key: editForm.personaKey.trim(),
          version: editForm.version.trim(),
          ...payload.value,
        }, accessToken)
      } else {
        await patchPersona(editTarget.id, payload.value, accessToken)
      }
      addToast(tc.toastUpdated, 'success')
      setEditTarget(null)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch (err) {
      setEditError(isApiError(err) ? err.message : tc.toastSaveFailed)
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, buildPayload, editForm, editTarget, fetchAll, tc])

  const columns: Column<Persona>[] = [
    {
      key: 'persona_key',
      header: tc.colPersonaKey,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-primary)]">{row.persona_key}</span>
      ),
    },
    {
      key: 'display_name',
      header: tc.colDisplayName,
      render: (row) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium text-[var(--c-text-primary)]">{row.display_name}</span>
          {row.source === 'builtin' && row.user_selectable && row.selector_name && typeof row.selector_order === 'number' && (
            <span className="text-xs text-[var(--c-text-muted)]">
              {tc.selectorMeta(row.selector_name, row.selector_order)}
            </span>
          )}
        </div>
      ),
    },
    {
      key: 'version',
      header: tc.colVersion,
      render: (row) => (
        <span className="tabular-nums text-xs text-[var(--c-text-secondary)]">{row.version}</span>
      ),
    },
    {
      key: 'model',
      header: tc.colModel,
      render: (row) => (
        <ModelValue
          persona={row}
          platformDefaultLabel={tc.valuePlatformDefault}
          hybridLabel={tc.labelHybrid}
        />
      ),
    },
    {
      key: 'is_active',
      header: tc.colActive,
      render: (row) => row.is_active ? <Badge variant="success">on</Badge> : <Badge variant="neutral">off</Badge>,
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="tabular-nums text-xs text-[var(--c-text-secondary)]">
          {row.created_at ? new Date(row.created_at).toLocaleString() : '-'}
        </span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => {
        const isBuiltin = row.source === 'builtin'
        return (
          <div className="flex items-center gap-1">
            {isBuiltin && (
              <span className="mr-1 rounded px-1.5 py-0.5 text-[10px] text-[var(--c-text-muted)] ring-1 ring-[var(--c-border)]">
                {tc.labelGlobal}
              </span>
            )}
            <button
              onClick={(event) => {
                event.stopPropagation()
                openEdit(row)
              }}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
              title={tc.modalTitleEdit}
            >
              <Pencil size={13} />
            </button>
          </div>
        )
      },
    },
  ]

  const headerActions = (
    <button
      onClick={openCreate}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      <Plus size={13} />
      {tc.addPersona}
    </button>
  )

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'
  const textareaCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none resize-y font-mono'
  const selectCls = `${inputCls} w-full`

  const renderForm = (
    form: PersonaFormState,
    setField: <K extends keyof PersonaFormState>(key: K, value: PersonaFormState[K]) => void,
    readOnlyIdentity: boolean,
  ) => {
    const renderToolSelector = (fieldKey: 'toolAllowlist' | 'toolDenylist', label: string) => {
      const allowInherit = fieldKey === 'toolAllowlist'
      const selectedNames = uniqToolNames(parseToolList(form[fieldKey]))
      const totalToolNames = uniqToolNames(catalogGroups.flatMap((group) => group.tools.map((tool) => tool.name)))
      const totalToolCount = totalToolNames.length
      const isInherit = allowInherit && form.toolAllowlistMode === 'inherit'
      const visibleSelectedCount = isInherit ? totalToolCount : selectedNames.length
      const replaceTools = (next: string[]) => {
        if (allowInherit) {
          setField('toolAllowlistMode', 'custom')
        }
        setField(fieldKey, formatToolList(next) as PersonaFormState[typeof fieldKey])
      }
      const setAllowlistMode = (mode: ToolSelectionMode) => {
        if (!allowInherit) return
        setField('toolAllowlistMode', mode)
        if (mode === 'custom' && selectedNames.length === 0) {
          setField('toolAllowlist', formatToolList(totalToolNames))
        }
      }
      const toggleTool = (toolName: string) => {
        replaceTools(
          selectedNames.includes(toolName)
            ? selectedNames.filter((item) => item !== toolName)
            : [...selectedNames, toolName],
        )
      }
      const toggleGroup = (group: ToolCatalogGroup, enabled: boolean) => {
        const groupNames = group.tools.map((tool) => tool.name)
        replaceTools(enabled ? [...selectedNames, ...groupNames] : selectedNames.filter((name) => !groupNames.includes(name)))
      }

      return (
        <FormField label={label}>
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-3 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-sm text-[var(--c-text-secondary)]">{tc.toolsSelected(visibleSelectedCount, totalToolCount)}</p>
                {allowInherit ? (
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={() => setAllowlistMode('inherit')}
                      className={[
                        'rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors',
                        form.toolAllowlistMode === 'inherit'
                          ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/10 text-[var(--c-text-primary)]'
                          : 'border-[var(--c-border)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-card)]',
                      ].join(' ')}
                    >
                      {tc.toolModeInherit}
                    </button>
                    <button
                      type="button"
                      onClick={() => setAllowlistMode('custom')}
                      className={[
                        'rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors',
                        form.toolAllowlistMode === 'custom'
                          ? 'border-[var(--c-accent)] bg-[var(--c-accent)]/10 text-[var(--c-text-primary)]'
                          : 'border-[var(--c-border)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-card)]',
                      ].join(' ')}
                    >
                      {tc.toolModeCustom}
                    </button>
                  </div>
                ) : null}
              </div>
              {allowInherit ? <p className="text-sm text-[var(--c-text-secondary)]">{tc.toolModeLabel}</p> : null}
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => replaceTools(totalToolNames)}
                  disabled={isInherit}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                >
                  <CheckCheck size={13} />
                  {tc.enableAllTools}
                </button>
                <button
                  type="button"
                  onClick={() => replaceTools([])}
                  disabled={isInherit}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                >
                  <Minus size={13} />
                  {tc.clearAllTools}
                </button>
              </div>
            </div>
            {catalogGroups.map((group) => {
              const groupNames = group.tools.map((tool) => tool.name)
              const groupSelectedCount = isInherit
                ? group.tools.length
                : groupNames.filter((toolName) => selectedNames.includes(toolName)).length
              return (
                <div key={`${fieldKey}-${group.group}`} className="flex flex-col gap-3 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-4">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <p className="text-xs font-medium uppercase tracking-wide text-[var(--c-text-muted)]">{group.group}</p>
                      <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{tc.toolsSelected(groupSelectedCount, group.tools.length)}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        onClick={() => toggleGroup(group, true)}
                        disabled={isInherit}
                        className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                      >
                        {tc.groupEnableAll}
                      </button>
                      <button
                        type="button"
                        onClick={() => toggleGroup(group, false)}
                        disabled={isInherit}
                        className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-card)] disabled:opacity-50"
                      >
                        {tc.groupClearAll}
                      </button>
                    </div>
                  </div>
                  <div className="grid gap-3 md:grid-cols-2">
                    {group.tools.map((tool) => (
                      <ToolOptionCard
                        key={`${fieldKey}-${tool.name}`}
                        tool={tool}
                        checked={isInherit || selectedNames.includes(tool.name)}
                        onToggle={() => { if (!isInherit) toggleTool(tool.name) }}
                      />
                    ))}
                  </div>
                </div>
              )
            })}
          </div>
        </FormField>
      )
    }

    return (
      <>
        <div className="grid grid-cols-2 gap-3">
          <FormField label={tc.fieldPersonaKey}>
            {readOnlyIdentity ? (
              <div className="flex items-center px-3 py-1.5 text-sm font-mono text-[var(--c-text-muted)]">{form.personaKey}</div>
            ) : (
              <input
                type="text"
                value={form.personaKey}
                onChange={(e) => setField('personaKey', e.target.value)}
                placeholder="my_persona"
                className={inputCls}
              />
            )}
          </FormField>
          <FormField label={tc.fieldVersion}>
            {readOnlyIdentity ? (
              <div className="flex items-center px-3 py-1.5 text-sm text-[var(--c-text-muted)]">{form.version}</div>
            ) : (
              <input
                type="text"
                value={form.version}
                onChange={(e) => setField('version', e.target.value)}
                placeholder="1.0.0"
                className={inputCls}
              />
            )}
          </FormField>
        </div>

        <FormField label={tc.fieldDisplayName}>
          <input
            type="text"
            value={form.displayName}
            onChange={(e) => setField('displayName', e.target.value)}
            className={inputCls}
          />
        </FormField>

        <FormField label={tc.fieldDescription}>
          <input
            type="text"
            value={form.description}
            onChange={(e) => setField('description', e.target.value)}
            className={inputCls}
          />
        </FormField>

        <FormField label={tc.fieldPrompt}>
          <textarea
            value={form.prompt}
            onChange={(e) => setField('prompt', e.target.value)}
            rows={5}
            className={textareaCls}
          />
        </FormField>

        <div className="grid grid-cols-2 gap-3">
          <FormField label={tc.fieldModel}>
            <input
              type="text"
              value={form.model}
              onChange={(e) => setField('model', e.target.value)}
              placeholder="provider^model"
              className={inputCls}
            />
          </FormField>
          <FormField label={tc.fieldPreferredCredential}>
            <input
              type="text"
              value={form.preferredCredential}
              onChange={(e) => setField('preferredCredential', e.target.value)}
              className={inputCls}
            />
          </FormField>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <FormField label={tc.fieldReasoningMode}>
            <select
              value={form.reasoningMode}
              onChange={(e) => setField('reasoningMode', e.target.value)}
              className={selectCls}
            >
              <option value="auto">auto</option>
              <option value="enabled">enabled</option>
              <option value="disabled">disabled</option>
              <option value="none">none</option>
            </select>
          </FormField>
          <FormField label={tc.fieldPromptCacheControl}>
            <select
              value={form.promptCacheControl}
              onChange={(e) => setField('promptCacheControl', e.target.value)}
              className={selectCls}
            >
              <option value="none">none</option>
              <option value="system_prompt">system_prompt</option>
            </select>
          </FormField>
        </div>

        {renderToolSelector('toolAllowlist', tc.fieldToolAllowlist)}
        {renderToolSelector('toolDenylist', tc.fieldToolDenylist)}

        <FormField label={tc.fieldBudgetsJSON}>
          <textarea
            value={form.budgetsJSON}
            onChange={(e) => setField('budgetsJSON', e.target.value)}
            rows={4}
            className={textareaCls}
          />
        </FormField>

        <FormField label={tc.fieldExecutorType}>
          <input
            type="text"
            value={form.executorType}
            onChange={(e) => setField('executorType', e.target.value)}
            placeholder="agent.simple"
            className={inputCls}
          />
        </FormField>

        <FormField label={tc.fieldExecutorConfig}>
          <textarea
            value={form.executorConfigJSON}
            onChange={(e) => setField('executorConfigJSON', e.target.value)}
            rows={4}
            className={textareaCls}
          />
        </FormField>

        <div className="flex items-center gap-2">
          <input
            id={readOnlyIdentity ? 'persona-edit-is-active' : 'persona-create-is-active'}
            type="checkbox"
            checked={form.isActive}
            onChange={(e) => setField('isActive', e.target.checked)}
            className="h-3.5 w-3.5 rounded"
          />
          <label
            htmlFor={readOnlyIdentity ? 'persona-edit-is-active' : 'persona-create-is-active'}
            className="text-sm text-[var(--c-text-secondary)]"
          >
            {tc.fieldIsActive}
          </label>
        </div>
      </>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={headerActions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={personas}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<Zap size={28} />}
        />
      </div>

      <Modal open={createOpen} onClose={closeCreate} title={tc.modalTitleCreate} width="640px">
        <div className="flex flex-col gap-4">
          {renderForm(createForm, setCreateField, false)}
          {createError && <p className="text-xs text-[var(--c-status-error-text)]">{createError}</p>}
          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={closeCreate}
              disabled={creating}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleCreate}
              disabled={creating}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {creating ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>

      <Modal open={editTarget !== null} onClose={closeEdit} title={tc.modalTitleEdit} width="640px">
        <div className="flex flex-col gap-4">
          {renderForm(editForm, setEditField, true)}
          {editError && <p className="text-xs text-[var(--c-status-error-text)]">{editError}</p>}
          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={closeEdit}
              disabled={saving}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleSave}
              disabled={saving}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {saving ? '...' : tc.save}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
