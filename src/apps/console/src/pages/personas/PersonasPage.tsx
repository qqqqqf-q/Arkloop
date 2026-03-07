import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Zap, Plus, Pencil } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listPersonas,
  createPersona,
  patchPersona,
  type Persona,
} from '../../api/personas'

type CreateFormState = {
  personaKey: string
  version: string
  displayName: string
  description: string
  prompt: string
  toolAllowlist: string
  budgetsJSON: string
  isActive: boolean
  executorType: string
  executorConfigJSON: string
  preferredCredential: string
}

type EditFormState = {
  displayName: string
  description: string
  prompt: string
  toolAllowlist: string
  budgetsJSON: string
  isActive: boolean
  executorType: string
  executorConfigJSON: string
  preferredCredential: string
}

function emptyCreateForm(): CreateFormState {
  return {
    personaKey: '',
    version: '1.0.0',
    displayName: '',
    description: '',
    prompt: '',
    toolAllowlist: '',
    budgetsJSON: '{}',
    isActive: true,
    executorType: 'agent.simple',
    executorConfigJSON: '{}',
    preferredCredential: '',
  }
}

function personaToEditForm(persona: Persona): EditFormState {
  return {
    displayName: persona.display_name,
    description: persona.description ?? '',
    prompt: persona.prompt_md,
    toolAllowlist: persona.tool_allowlist.join(', '),
    budgetsJSON: JSON.stringify(persona.budgets, null, 2),
    isActive: persona.is_active,
    executorType: persona.executor_type || 'agent.simple',
    executorConfigJSON: JSON.stringify(persona.executor_config ?? {}, null, 2),
    preferredCredential: persona.preferred_credential ?? '',
  }
}

function isHybridPersona(executorType: string): boolean {
  return executorType.trim() === 'agent.lua'
}

type DefaultModelValueProps = {
  persona: Pick<Persona, 'agent_config_name' | 'executor_type'>
  platformDefaultLabel: string
  hybridLabel: string
  textClassName?: string
}

function DefaultModelValue({
  persona,
  platformDefaultLabel,
  hybridLabel,
  textClassName = 'font-mono text-xs text-[var(--c-text-primary)]',
}: DefaultModelValueProps) {
  const agentConfigName = persona.agent_config_name?.trim()
  const label = agentConfigName || platformDefaultLabel
  const textTone = agentConfigName ? 'text-[var(--c-text-primary)]' : 'text-[var(--c-text-muted)]'

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className={`${textClassName} ${textTone}`}>{label}</span>
      {isHybridPersona(persona.executor_type) && <Badge variant="warning">{hybridLabel}</Badge>}
    </div>
  )
}

function parseToolAllowlist(raw: string): string[] {
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function tryParseJSON(raw: string): { ok: true; value: Record<string, unknown> } | { ok: false } {
  try {
    const parsed = JSON.parse(raw.trim() || '{}')
    if (typeof parsed !== 'object' || Array.isArray(parsed) || parsed === null) return { ok: false }
    return { ok: true, value: parsed as Record<string, unknown> }
  } catch {
    return { ok: false }
  }
}

export function PersonasPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.personas

  const [personas, setPersonas] = useState<Persona[]>([])
  const [loading, setLoading] = useState(false)

  // create modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateFormState>(emptyCreateForm)
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  // edit modal
  const [editTarget, setEditTarget] = useState<Persona | null>(null)
  const [editForm, setEditForm] = useState<EditFormState>({
    displayName: '',
    description: '',
    prompt: '',
    toolAllowlist: '',
    budgetsJSON: '{}',
    isActive: true,
    executorType: 'agent.simple',
    executorConfigJSON: '{}',
    preferredCredential: '',
  })
  const [editError, setEditError] = useState('')
  const [saving, setSaving] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listPersonas(accessToken)
      setPersonas(list)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void fetchAll()
  }, [fetchAll])

  const handleOpenCreate = useCallback(() => {
    setCreateForm(emptyCreateForm())
    setCreateError('')
    setCreateOpen(true)
  }, [])

  const handleOpenEdit = useCallback((persona: Persona) => {
    setEditTarget(persona)
    setEditForm(personaToEditForm(persona))
    setEditError('')
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (creating) return
    setCreateOpen(false)
  }, [creating])

  const handleCloseEdit = useCallback(() => {
    if (saving) return
    setEditTarget(null)
  }, [saving])

  const setCreateField = useCallback(
    <K extends keyof CreateFormState>(key: K, value: CreateFormState[K]) => {
      setCreateForm((prev) => ({ ...prev, [key]: value }))
      setCreateError('')
    },
    [],
  )

  const setEditField = useCallback(
    <K extends keyof EditFormState>(key: K, value: EditFormState[K]) => {
      setEditForm((prev) => ({ ...prev, [key]: value }))
      setEditError('')
    },
    [],
  )

  const handleCreate = useCallback(async () => {
    const personaKey = createForm.personaKey.trim()
    const version = createForm.version.trim()
    const displayName = createForm.displayName.trim()
    const prompt = createForm.prompt.trim()

    if (!personaKey || !version || !displayName || !prompt) {
      setCreateError(tc.errRequired)
      return
    }

    const budgetsParsed = tryParseJSON(createForm.budgetsJSON)
    if (!budgetsParsed.ok) {
      setCreateError(tc.errInvalidJSON)
      return
    }

    const executorConfigParsed = tryParseJSON(createForm.executorConfigJSON)
    if (!executorConfigParsed.ok) {
      setCreateError(tc.errInvalidJSON)
      return
    }

    setCreating(true)
    setCreateError('')
    try {
      await createPersona(
        {
          persona_key: personaKey,
          version,
          display_name: displayName,
          description: createForm.description.trim() || undefined,
          prompt_md: prompt,
          tool_allowlist: parseToolAllowlist(createForm.toolAllowlist),
          budgets: budgetsParsed.value,
          is_active: createForm.isActive,
          executor_type: createForm.executorType.trim() || undefined,
          executor_config: executorConfigParsed.value,
          preferred_credential: createForm.preferredCredential.trim() || undefined,
        },
        accessToken,
      )
      addToast(tc.toastCreated, 'success')
      setCreateOpen(false)
      await fetchAll()
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastSaveFailed)
    } finally {
      setCreating(false)
    }
  }, [createForm, accessToken, fetchAll, addToast, tc])

  const handleSave = useCallback(async () => {
    if (!editTarget) return

    const displayName = editForm.displayName.trim()
    if (!displayName) {
      setEditError(tc.errRequired)
      return
    }

    const budgetsParsed = tryParseJSON(editForm.budgetsJSON)
    if (!budgetsParsed.ok) {
      setEditError(tc.errInvalidJSON)
      return
    }

    const executorConfigParsed = tryParseJSON(editForm.executorConfigJSON)
    if (!executorConfigParsed.ok) {
      setEditError(tc.errInvalidJSON)
      return
    }

    setSaving(true)
    setEditError('')
    try {
      await patchPersona(
        editTarget.id,
        {
          display_name: displayName,
          description: editForm.description.trim() || undefined,
          prompt_md: editForm.prompt.trim() || undefined,
          tool_allowlist: parseToolAllowlist(editForm.toolAllowlist),
          budgets: budgetsParsed.value,
          is_active: editForm.isActive,
          executor_type: editForm.executorType.trim() || undefined,
          executor_config: executorConfigParsed.value,
          preferred_credential: editForm.preferredCredential.trim() || undefined,
        },
        accessToken,
      )
      addToast(tc.toastUpdated, 'success')
      setEditTarget(null)
      await fetchAll()
    } catch (err) {
      setEditError(isApiError(err) ? err.message : tc.toastSaveFailed)
    } finally {
      setSaving(false)
    }
  }, [editForm, editTarget, accessToken, fetchAll, addToast, tc])

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
      key: 'agent_config_name',
      header: tc.colDefaultModel,
      render: (row) => (
        <DefaultModelValue
          persona={row}
          platformDefaultLabel={tc.valuePlatformDefault}
          hybridLabel={tc.labelHybrid}
        />
      ),
    },
    {
      key: 'is_active',
      header: tc.colActive,
      render: (row) =>
        row.is_active ? (
          <Badge variant="success">on</Badge>
        ) : (
          <Badge variant="neutral">off</Badge>
        ),
    },
    {
      key: 'user_selectable',
      header: tc.colSelectable,
      render: (row) =>
        row.user_selectable ? (
          <Badge variant="success">on</Badge>
        ) : (
          <Badge variant="neutral">off</Badge>
        ),
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="tabular-nums text-xs">{row.created_at ? new Date(row.created_at).toLocaleString() : '-'}</span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => {
        const isGlobal = row.source === 'builtin'
        return (
          <div className="flex items-center gap-1">
            {isGlobal && (
              <span className="mr-1 rounded px-1.5 py-0.5 text-[10px] text-[var(--c-text-muted)] ring-1 ring-[var(--c-border)]">
                {tc.labelGlobal}
              </span>
            )}
            <button
              onClick={(e) => {
                e.stopPropagation()
                if (!isGlobal) handleOpenEdit(row)
              }}
              disabled={isGlobal}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:cursor-not-allowed disabled:opacity-30"
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
      onClick={handleOpenCreate}
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

      {/* Create Modal */}
      <Modal
        open={createOpen}
        onClose={handleCloseCreate}
        title={tc.modalTitleCreate}
        width="560px"
      >
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-3">
            <FormField label={tc.fieldPersonaKey}>
              <input
                type="text"
                value={createForm.personaKey}
                onChange={(e) => setCreateField('personaKey', e.target.value)}
                placeholder="my_persona"
                className={inputCls}
              />
            </FormField>
            <FormField label={tc.fieldVersion}>
              <input
                type="text"
                value={createForm.version}
                onChange={(e) => setCreateField('version', e.target.value)}
                placeholder="1.0.0"
                className={inputCls}
              />
            </FormField>
          </div>

          <FormField label={tc.fieldDisplayName}>
            <input
              type="text"
              value={createForm.displayName}
              onChange={(e) => setCreateField('displayName', e.target.value)}
              placeholder="My Persona"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldDescription}>
            <input
              type="text"
              value={createForm.description}
              onChange={(e) => setCreateField('description', e.target.value)}
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldPrompt}>
            <textarea
              value={createForm.prompt}
              onChange={(e) => setCreateField('prompt', e.target.value)}
              rows={5}
              className={textareaCls}
            />
          </FormField>

          <FormField label={tc.fieldToolAllowlist}>
            <input
              type="text"
              value={createForm.toolAllowlist}
              onChange={(e) => setCreateField('toolAllowlist', e.target.value)}
              placeholder={tc.fieldToolAllowlistPlaceholder}
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldBudgetsJSON}>
            <textarea
              value={createForm.budgetsJSON}
              onChange={(e) => setCreateField('budgetsJSON', e.target.value)}
              rows={3}
              className={textareaCls}
            />
          </FormField>

          <FormField label={tc.fieldExecutorType}>
            <input
              type="text"
              value={createForm.executorType}
              onChange={(e) => setCreateField('executorType', e.target.value)}
              placeholder="agent.simple"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldExecutorConfig}>
            <textarea
              value={createForm.executorConfigJSON}
              onChange={(e) => setCreateField('executorConfigJSON', e.target.value)}
              rows={3}
              className={textareaCls}
            />
          </FormField>

          <FormField label={tc.fieldPreferredCredential}>
            <input
              type="text"
              value={createForm.preferredCredential}
              onChange={(e) => setCreateField('preferredCredential', e.target.value)}
              className={inputCls}
            />
          </FormField>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="persona-create-is-active"
              checked={createForm.isActive}
              onChange={(e) => setCreateField('isActive', e.target.checked)}
              className="h-3.5 w-3.5 rounded"
            />
            <label
              htmlFor="persona-create-is-active"
              className="text-sm text-[var(--c-text-secondary)]"
            >
              {tc.fieldIsActive}
            </label>
          </div>

          {createError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{createError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseCreate}
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

      {/* Edit Modal */}
      <Modal
        open={editTarget !== null}
        onClose={handleCloseEdit}
        title={tc.modalTitleEdit}
        width="560px"
      >
        <div className="flex flex-col gap-4">
          {/* persona_key / version — read-only */}
          <div className="grid grid-cols-2 gap-3">
            <FormField label={tc.fieldPersonaKey}>
              <div className="flex items-center px-3 py-1.5 text-sm font-mono text-[var(--c-text-muted)]">
                {editTarget?.persona_key}
              </div>
            </FormField>
            <FormField label={tc.fieldVersion}>
              <div className="flex items-center px-3 py-1.5 text-sm text-[var(--c-text-muted)]">
                {editTarget?.version}
              </div>
            </FormField>
          </div>

          <FormField label={tc.fieldDisplayName}>
            <input
              type="text"
              value={editForm.displayName}
              onChange={(e) => setEditField('displayName', e.target.value)}
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldDescription}>
            <input
              type="text"
              value={editForm.description}
              onChange={(e) => setEditField('description', e.target.value)}
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldPrompt}>
            <textarea
              value={editForm.prompt}
              onChange={(e) => setEditField('prompt', e.target.value)}
              rows={5}
              className={textareaCls}
            />
          </FormField>

          <FormField label={tc.fieldToolAllowlist}>
            <input
              type="text"
              value={editForm.toolAllowlist}
              onChange={(e) => setEditField('toolAllowlist', e.target.value)}
              placeholder={tc.fieldToolAllowlistPlaceholder}
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldBudgetsJSON}>
            <textarea
              value={editForm.budgetsJSON}
              onChange={(e) => setEditField('budgetsJSON', e.target.value)}
              rows={3}
              className={textareaCls}
            />
          </FormField>

          <FormField label={tc.fieldExecutorType}>
            <input
              type="text"
              value={editForm.executorType}
              onChange={(e) => setEditField('executorType', e.target.value)}
              placeholder="agent.simple"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldDefaultModel}>
            {editTarget ? (
              <div className="flex min-h-9 items-center rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5">
                <DefaultModelValue
                  persona={editTarget}
                  platformDefaultLabel={tc.valuePlatformDefault}
                  hybridLabel={tc.labelHybrid}
                  textClassName="font-mono text-sm"
                />
              </div>
            ) : null}
          </FormField>

          <FormField label={tc.fieldExecutorConfig}>
            <textarea
              value={editForm.executorConfigJSON}
              onChange={(e) => setEditField('executorConfigJSON', e.target.value)}
              rows={3}
              className={textareaCls}
            />
          </FormField>

          <FormField label={tc.fieldPreferredCredential}>
            <input
              type="text"
              value={editForm.preferredCredential}
              onChange={(e) => setEditField('preferredCredential', e.target.value)}
              className={inputCls}
            />
          </FormField>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="persona-edit-is-active"
              checked={editForm.isActive}
              onChange={(e) => setEditField('isActive', e.target.checked)}
              className="h-3.5 w-3.5 rounded"
            />
            <label
              htmlFor="persona-edit-is-active"
              className="text-sm text-[var(--c-text-secondary)]"
            >
              {tc.fieldIsActive}
            </label>
          </div>

          {editError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{editError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseEdit}
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
