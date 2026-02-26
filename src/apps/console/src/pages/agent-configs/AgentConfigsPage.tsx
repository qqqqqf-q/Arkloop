import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Bot, Plus, Pencil, Trash2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listAgentConfigs,
  createAgentConfig,
  updateAgentConfig,
  deleteAgentConfig,
  type AgentConfig,
  type CreateAgentConfigRequest,
} from '../../api/agent-configs'
import { listPromptTemplates, type PromptTemplate } from '../../api/prompt-templates'

const TOOL_POLICIES = ['allowlist', 'denylist', 'none'] as const
const PROMPT_CACHE_CONTROLS = ['none', 'system_prompt'] as const
const SCOPES = ['org', 'platform'] as const

type FormState = {
  scope: string
  name: string
  system_prompt_template_id: string
  system_prompt_override: string
  model: string
  temperature: string
  max_output_tokens: string
  top_p: string
  tool_policy: string
  tool_allowlist: string
  tool_denylist: string
  content_filter_level: string
  is_default: boolean
  prompt_cache_control: string
}

function emptyForm(): FormState {
  return {
    scope: 'org',
    name: '',
    system_prompt_template_id: '',
    system_prompt_override: '',
    model: '',
    temperature: '',
    max_output_tokens: '',
    top_p: '',
    tool_policy: 'allowlist',
    tool_allowlist: '',
    tool_denylist: '',
    content_filter_level: '',
    is_default: false,
    prompt_cache_control: 'none',
  }
}

function configToForm(ac: AgentConfig): FormState {
  return {
    scope: ac.scope ?? 'org',
    name: ac.name,
    system_prompt_template_id: ac.system_prompt_template_id ?? '',
    system_prompt_override: ac.system_prompt_override ?? '',
    model: ac.model ?? '',
    temperature: ac.temperature != null ? String(ac.temperature) : '',
    max_output_tokens: ac.max_output_tokens != null ? String(ac.max_output_tokens) : '',
    top_p: ac.top_p != null ? String(ac.top_p) : '',
    tool_policy: ac.tool_policy,
    tool_allowlist: ac.tool_allowlist.join(', '),
    tool_denylist: ac.tool_denylist.join(', '),
    content_filter_level: ac.content_filter_level,
    is_default: ac.is_default,
    prompt_cache_control: ac.prompt_cache_control ?? 'none',
  }
}

function parseCommaSeparated(value: string): string[] {
  return value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function parseOptionalFloat(value: string): number | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const n = parseFloat(trimmed)
  return isNaN(n) ? undefined : n
}

function parseOptionalInt(value: string): number | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const n = parseInt(trimmed, 10)
  return isNaN(n) ? undefined : n
}

type DeleteTarget = { id: string; name: string }

export function AgentConfigsPage() {
  const { accessToken, me } = useOutletContext<ConsoleOutletContext>()
  const isPlatformAdmin = me?.role === 'platform_admin'
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.agentConfigs

  const [configs, setConfigs] = useState<AgentConfig[]>([])
  const [templates, setTemplates] = useState<PromptTemplate[]>([])
  const [loading, setLoading] = useState(false)

  const [modalOpen, setModalOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AgentConfig | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [formError, setFormError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const [deleting, setDeleting] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const [cfgs, tmpl] = await Promise.all([
        listAgentConfigs(accessToken),
        listPromptTemplates(accessToken),
      ])
      setConfigs(cfgs)
      setTemplates(tmpl)
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
    setEditTarget(null)
    setForm(emptyForm())
    setFormError('')
    setModalOpen(true)
  }, [])

  const handleOpenEdit = useCallback((ac: AgentConfig) => {
    setEditTarget(ac)
    setForm(configToForm(ac))
    setFormError('')
    setModalOpen(true)
  }, [])

  const handleCloseModal = useCallback(() => {
    if (submitting) return
    setModalOpen(false)
  }, [submitting])

  const handleFormField = useCallback(
    <K extends keyof FormState>(key: K, value: FormState[K]) => {
      setForm((prev) => ({ ...prev, [key]: value }))
      setFormError('')
    },
    [],
  )

  const handleSubmit = useCallback(async () => {
    const name = form.name.trim()
    if (!name) {
      setFormError(tc.errRequired)
      return
    }

    setSubmitting(true)
    setFormError('')
    try {
      if (editTarget) {
        await updateAgentConfig(
          editTarget.id,
          {
            ...(isPlatformAdmin && { scope: form.scope }),
            name,
            system_prompt_template_id: form.system_prompt_template_id,
            system_prompt_override: form.system_prompt_override,
            model: form.model.trim(),
            temperature: parseOptionalFloat(form.temperature),
            max_output_tokens: parseOptionalInt(form.max_output_tokens),
            top_p: parseOptionalFloat(form.top_p),
            tool_policy: form.tool_policy,
            tool_allowlist: parseCommaSeparated(form.tool_allowlist),
            tool_denylist: parseCommaSeparated(form.tool_denylist),
            content_filter_level: form.content_filter_level.trim(),
            is_default: form.is_default,
            prompt_cache_control: form.prompt_cache_control,
          },
          accessToken,
        )
        addToast(tc.toastUpdated, 'success')
      } else {
        const req: CreateAgentConfigRequest = {
          scope: isPlatformAdmin ? form.scope : 'org',
          name,
          system_prompt_template_id: form.system_prompt_template_id || undefined,
          system_prompt_override: form.system_prompt_override || undefined,
          model: form.model.trim() || undefined,
          temperature: parseOptionalFloat(form.temperature),
          max_output_tokens: parseOptionalInt(form.max_output_tokens),
          top_p: parseOptionalFloat(form.top_p),
          tool_policy: form.tool_policy,
          tool_allowlist: parseCommaSeparated(form.tool_allowlist),
          tool_denylist: parseCommaSeparated(form.tool_denylist),
          content_filter_level: form.content_filter_level.trim() || undefined,
          is_default: form.is_default,
          prompt_cache_control: form.prompt_cache_control || undefined,
        }
        await createAgentConfig(req, accessToken)
        addToast(tc.toastCreated, 'success')
      }
      setModalOpen(false)
      await fetchAll()
    } catch (err) {
      if (isApiError(err)) {
        setFormError(err.message)
      } else {
        setFormError(tc.toastSaveFailed)
      }
    } finally {
      setSubmitting(false)
    }
  }, [form, editTarget, accessToken, fetchAll, addToast, tc])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteAgentConfig(deleteTarget.id, accessToken)
      setDeleteTarget(null)
      await fetchAll()
      addToast(tc.toastDeleted, 'success')
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, fetchAll, addToast, tc])

  const columns: Column<AgentConfig>[] = [
    {
      key: 'name',
      header: tc.colName,
      render: (row) => (
        <span className="font-medium text-[var(--c-text-primary)]">{row.name}</span>
      ),
    },
    {
      key: 'model',
      header: tc.colModel,
      render: (row) =>
        row.model ? (
          <span className="font-mono text-xs">{row.model}</span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'temperature',
      header: tc.colTemperature,
      render: (row) =>
        row.temperature != null ? (
          <span className="tabular-nums text-xs">{row.temperature}</span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'max_output_tokens',
      header: tc.colMaxOutputTokens,
      render: (row) =>
        row.max_output_tokens != null ? (
          <span className="tabular-nums text-xs">{row.max_output_tokens}</span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'tool_policy',
      header: tc.colToolPolicy,
      render: (row) => <Badge variant="neutral">{row.tool_policy}</Badge>,
    },
    {
      key: 'is_default',
      header: tc.colIsDefault,
      render: (row) =>
        row.is_default ? (
          <Badge variant="success">default</Badge>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'scope',
      header: 'scope',
      render: (row) =>
        row.scope === 'platform' ? (
          <Badge variant="warning">platform</Badge>
        ) : (
          <span className="text-[var(--c-text-muted)]">org</span>
        ),
    },
    {
      key: 'project_id',
      header: tc.colProject,
      render: (row) =>
        row.project_id ? (
          <span className="font-mono text-xs">{row.project_id.slice(0, 8)}…</span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="tabular-nums text-xs">
          {new Date(row.created_at).toLocaleString()}
        </span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => {
        const canEdit = row.scope !== 'platform' || isPlatformAdmin
        if (!canEdit) return null
        return (
          <div className="flex items-center gap-1">
            <button
              onClick={(e) => {
                e.stopPropagation()
                handleOpenEdit(row)
              }}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
              title={tc.modalTitleEdit}
            >
              <Pencil size={13} />
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation()
                setDeleteTarget({ id: row.id, name: row.name })
              }}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
              title={tc.deleteConfirm}
            >
              <Trash2 size={13} />
            </button>
          </div>
        )
      },
    },
  ]

  const actions = (
    <button
      onClick={handleOpenCreate}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      <Plus size={13} />
      {tc.addConfig}
    </button>
  )

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={actions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={configs}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<Bot size={28} />}
        />
      </div>

      {/* Create / Edit Modal */}
      <Modal
        open={modalOpen}
        onClose={handleCloseModal}
        title={editTarget ? tc.modalTitleEdit : tc.modalTitleCreate}
        width="560px"
      >
        <div className="flex flex-col gap-4">
          {isPlatformAdmin && (
            <FormField label="scope">
              <select
                value={form.scope}
                onChange={(e) => handleFormField('scope', e.target.value)}
                className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
              >
                {SCOPES.map((s) => (
                  <option key={s} value={s}>{s}</option>
                ))}
              </select>
            </FormField>
          )}

          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={form.name}
              onChange={(e) => handleFormField('name', e.target.value)}
              placeholder="my-agent-config"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldSystemPromptTemplate}>
            <select
              value={form.system_prompt_template_id}
              onChange={(e) => handleFormField('system_prompt_template_id', e.target.value)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
            >
              <option value="">{tc.fieldSystemPromptTemplateNone}</option>
              {templates.map((tmpl) => (
                <option key={tmpl.id} value={tmpl.id}>
                  {tmpl.name}
                  {tmpl.is_default ? ' (default)' : ''}
                </option>
              ))}
            </select>
          </FormField>

          <FormField label={tc.fieldSystemPromptOverride}>
            <textarea
              value={form.system_prompt_override}
              onChange={(e) => handleFormField('system_prompt_override', e.target.value)}
              rows={3}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none resize-none"
            />
          </FormField>

          <FormField label={tc.fieldModel}>
            <input
              type="text"
              value={form.model}
              onChange={(e) => handleFormField('model', e.target.value)}
              placeholder="my-anthropic"
              className={inputCls}
            />
          </FormField>

          <div className="grid grid-cols-3 gap-3">
            <FormField label={tc.fieldTemperature}>
              <input
                type="number"
                step="0.1"
                min="0"
                max="2"
                value={form.temperature}
                onChange={(e) => handleFormField('temperature', e.target.value)}
                placeholder="0.7"
                className={inputCls}
              />
            </FormField>
            <FormField label={tc.fieldMaxOutputTokens}>
              <input
                type="number"
                min="1"
                value={form.max_output_tokens}
                onChange={(e) => handleFormField('max_output_tokens', e.target.value)}
                placeholder="4096"
                className={inputCls}
              />
            </FormField>
            <FormField label={tc.fieldTopP}>
              <input
                type="number"
                step="0.05"
                min="0"
                max="1"
                value={form.top_p}
                onChange={(e) => handleFormField('top_p', e.target.value)}
                placeholder="1.0"
                className={inputCls}
              />
            </FormField>
          </div>

          <FormField label={tc.fieldToolPolicy}>
            <select
              value={form.tool_policy}
              onChange={(e) => handleFormField('tool_policy', e.target.value)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
            >
              {TOOL_POLICIES.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </FormField>

          <FormField label={tc.fieldToolAllowlist}>
            <input
              type="text"
              value={form.tool_allowlist}
              onChange={(e) => handleFormField('tool_allowlist', e.target.value)}
              placeholder="web_fetch, code_exec"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldToolDenylist}>
            <input
              type="text"
              value={form.tool_denylist}
              onChange={(e) => handleFormField('tool_denylist', e.target.value)}
              placeholder="shell"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldContentFilterLevel}>
            <input
              type="text"
              value={form.content_filter_level}
              onChange={(e) => handleFormField('content_filter_level', e.target.value)}
              placeholder="standard"
              className={inputCls}
            />
          </FormField>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="agent-config-is-default"
              checked={form.is_default}
              onChange={(e) => handleFormField('is_default', e.target.checked)}
              className="h-3.5 w-3.5 rounded"
            />
            <label
              htmlFor="agent-config-is-default"
              className="text-sm text-[var(--c-text-secondary)]"
            >
              {tc.fieldIsDefault}
            </label>
          </div>

          <FormField label={tc.fieldPromptCacheControl}>
            <select
              value={form.prompt_cache_control}
              onChange={(e) => handleFormField('prompt_cache_control', e.target.value)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
            >
              {PROMPT_CACHE_CONTROLS.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </FormField>

          {formError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{formError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseModal}
              disabled={submitting}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleSubmit}
              disabled={submitting}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {submitting ? '...' : editTarget ? tc.save : tc.create}
            </button>
          </div>
        </div>
      </Modal>

      {/* Delete Confirm */}
      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
        title={tc.deleteTitle}
        message={tc.deleteMessage(deleteTarget?.name ?? '')}
        confirmLabel={tc.deleteConfirm}
        loading={deleting}
      />
    </div>
  )
}
