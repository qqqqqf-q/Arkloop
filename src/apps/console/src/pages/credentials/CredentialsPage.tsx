import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { KeyRound, Plus, Trash2, Pencil, Copy, Check } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge, type BadgeVariant } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listLlmCredentials,
  createLlmCredential,
  deleteLlmCredential,
  updateLlmCredential,
  updateLlmRoute,
  type LlmCredential,
  type LlmRoute,
  type CreateLlmRouteRequest,
} from '../../api/llm-credentials'

const PROVIDERS = ['openai', 'anthropic'] as const
const OPENAI_API_MODES = ['auto', 'responses', 'chat_completions'] as const

function providerVariant(provider: string): BadgeVariant {
  return provider === 'anthropic' ? 'warning' : 'neutral'
}

type RouteRow = {
  model: string
  priority: string
  is_default: boolean
  when: string
  multiplier: string
  cost_per_1k_input: string
  cost_per_1k_output: string
}

function emptyRoute(): RouteRow {
  return { model: '', priority: '0', is_default: false, when: '', multiplier: '1', cost_per_1k_input: '', cost_per_1k_output: '' }
}

type CreateFormState = {
  name: string
  provider: string
  api_key: string
  base_url: string
  openai_api_mode: string
  routes: RouteRow[]
}

function emptyForm(): CreateFormState {
  return {
    name: '',
    provider: 'openai',
    api_key: '',
    base_url: '',
    openai_api_mode: '',
    routes: [],
  }
}

type DeleteTarget = { id: string; name: string }

// 编辑路由时每行的草稿状态
type RouteEditRow = LlmRoute & {
  draftModel: string
  draftPriority: string
  draftIsDefault: boolean
  draftWhen: string
  draftMultiplier: string
  draftCostInput: string
  draftCostOutput: string
  saving: boolean
  copied: boolean
}

function routeToEditRow(route: LlmRoute): RouteEditRow {
  return {
    ...route,
    draftModel: route.model,
    draftPriority: String(route.priority),
    draftIsDefault: route.is_default,
    draftWhen: route.when && Object.keys(route.when).length > 0 ? JSON.stringify(route.when) : '',
    draftMultiplier: route.multiplier != null ? String(route.multiplier) : '1',
    draftCostInput: route.cost_per_1k_input != null ? String(route.cost_per_1k_input * 1000) : '',
    draftCostOutput: route.cost_per_1k_output != null ? String(route.cost_per_1k_output * 1000) : '',
    saving: false,
    copied: false,
  }
}

export function CredentialsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.credentials

  const [creds, setCreds] = useState<LlmCredential[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState<CreateFormState>(emptyForm)
  const [formError, setFormError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const [deleting, setDeleting] = useState(false)

  // 编辑 modal
  const [editCred, setEditCred] = useState<LlmCredential | null>(null)
  const [editRows, setEditRows] = useState<RouteEditRow[]>([])
  // 凭证元数据草稿
  const [editCredName, setEditCredName] = useState('')
  const [editCredBaseUrl, setEditCredBaseUrl] = useState('')
  const [editCredApiMode, setEditCredApiMode] = useState('')
  const [editCredApiKey, setEditCredApiKey] = useState('')
  const [savingCred, setSavingCred] = useState(false)

  const fetchCreds = useCallback(async () => {
    setLoading(true)
    try {
      const data = await listLlmCredentials(accessToken)
      setCreds(data)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast])

  useEffect(() => {
    void fetchCreds()
  }, [fetchCreds])

  const handleOpenCreate = useCallback(() => {
    setForm(emptyForm())
    setFormError('')
    setCreateOpen(true)
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (submitting) return
    setCreateOpen(false)
  }, [submitting])

  const handleFormField = useCallback(
    <K extends keyof CreateFormState>(key: K, value: CreateFormState[K]) => {
      setForm((prev) => ({ ...prev, [key]: value }))
      setFormError('')
    },
    [],
  )

  const handleRouteField = useCallback(
    <K extends keyof RouteRow>(idx: number, key: K, value: RouteRow[K]) => {
      setForm((prev) => {
        const routes = prev.routes.map((r, i) => (i === idx ? { ...r, [key]: value } : r))
        return { ...prev, routes }
      })
    },
    [],
  )

  const handleAddRoute = useCallback(() => {
    setForm((prev) => ({ ...prev, routes: [...prev.routes, emptyRoute()] }))
  }, [])

  const handleRemoveRoute = useCallback((idx: number) => {
    setForm((prev) => ({ ...prev, routes: prev.routes.filter((_, i) => i !== idx) }))
  }, [])

  const handleSubmit = useCallback(async () => {
    const name = form.name.trim()
    const api_key = form.api_key.trim()
    const provider = form.provider.trim()

    if (!name || !api_key || !provider) {
      setFormError(tc.errRequired)
      return
    }

    const routes: CreateLlmRouteRequest[] = []
    for (const r of form.routes) {
      const model = r.model.trim()
      if (!model) continue

      let when: Record<string, unknown> = {}
      const whenStr = r.when.trim()
      if (whenStr) {
        try {
          when = JSON.parse(whenStr) as Record<string, unknown>
        } catch {
          setFormError(tc.errInvalidJson(model))
          return
        }
      }

      routes.push({
        model,
        priority: parseInt(r.priority, 10) || 0,
        is_default: r.is_default,
        when,
        multiplier: parseFloat(r.multiplier) > 0 ? parseFloat(r.multiplier) : undefined,
        cost_per_1k_input: r.cost_per_1k_input !== '' ? parseFloat(r.cost_per_1k_input) / 1000 : undefined,
        cost_per_1k_output: r.cost_per_1k_output !== '' ? parseFloat(r.cost_per_1k_output) / 1000 : undefined,
      })
    }

    setSubmitting(true)
    setFormError('')
    try {
      await createLlmCredential(
        {
          name,
          provider,
          api_key,
          base_url: form.base_url.trim() || undefined,
          openai_api_mode: (provider === 'openai' && form.openai_api_mode) ? form.openai_api_mode : undefined,
          routes,
        },
        accessToken,
      )
      setCreateOpen(false)
      await fetchCreds()
      addToast(tc.toastCreated, 'success')
    } catch (err) {
      if (isApiError(err)) {
        if (err.code === 'database.not_configured') {
          setFormError(tc.errEncryptionKey)
        } else {
          setFormError(err.message)
        }
      } else {
        setFormError(tc.errRequired)
      }
    } finally {
      setSubmitting(false)
    }
  }, [form, accessToken, fetchCreds, addToast, tc])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteLlmCredential(deleteTarget.id, accessToken)
      setDeleteTarget(null)
      await fetchCreds()
      addToast(tc.toastDeleted, 'success')
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, fetchCreds, addToast])

  const handleOpenEdit = useCallback((cred: LlmCredential) => {
    setEditCred(cred)
    setEditRows(cred.routes.map(routeToEditRow))
    setEditCredName(cred.name)
    setEditCredBaseUrl(cred.base_url ?? '')
    setEditCredApiMode(cred.openai_api_mode ?? '')
    setEditCredApiKey('')
  }, [])

  const handleCloseEdit = useCallback(() => {
    setEditCred(null)
    setEditRows([])
  }, [])

  const handleEditRowField = useCallback(
    <K extends keyof RouteEditRow>(idx: number, key: K, value: RouteEditRow[K]) => {
      setEditRows((prev) => prev.map((r, i) => (i === idx ? { ...r, [key]: value } : r)))
    },
    [],
  )

  const handleSaveRoute = useCallback(
    async (idx: number) => {
      if (!editCred) return
      const row = editRows[idx]

      let when: Record<string, unknown> = {}
      const whenStr = row.draftWhen.trim()
      if (whenStr) {
        try {
          when = JSON.parse(whenStr) as Record<string, unknown>
        } catch {
          addToast(tc.errInvalidJson(row.draftModel), 'error')
          return
        }
      }

      setEditRows((prev) => prev.map((r, i) => (i === idx ? { ...r, saving: true } : r)))
      try {
        const updated = await updateLlmRoute(
          editCred.id,
          row.id,
          {
            model: row.draftModel.trim(),
            priority: parseInt(row.draftPriority, 10) || 0,
            is_default: row.draftIsDefault,
            when,
            multiplier: parseFloat(row.draftMultiplier) > 0 ? parseFloat(row.draftMultiplier) : undefined,
            cost_per_1k_input: row.draftCostInput !== '' ? parseFloat(row.draftCostInput) / 1000 : undefined,
            cost_per_1k_output: row.draftCostOutput !== '' ? parseFloat(row.draftCostOutput) / 1000 : undefined,
          },
          accessToken,
        )
        setEditRows((prev) =>
          prev.map((r, i) =>
            i === idx ? { ...routeToEditRow(updated), saving: false } : r,
          ),
        )
        // 同步更新主列表
        setCreds((prev) =>
          prev.map((c) =>
            c.id === editCred.id
              ? { ...c, routes: c.routes.map((rt) => (rt.id === updated.id ? updated : rt)) }
              : c,
          ),
        )
        addToast(tc.toastRouteUpdated, 'success')
      } catch {
        setEditRows((prev) => prev.map((r, i) => (i === idx ? { ...r, saving: false } : r)))
        addToast(tc.toastRouteUpdateFailed, 'error')
      }
    },
    [editCred, editRows, accessToken, addToast, tc],
  )

  const handleCopyModel = useCallback((idx: number) => {
    const row = editRows[idx]
    void navigator.clipboard.writeText(row.model)
    setEditRows((prev) => prev.map((r, i) => (i === idx ? { ...r, copied: true } : r)))
    setTimeout(() => {
      setEditRows((prev) => prev.map((r, i) => (i === idx ? { ...r, copied: false } : r)))
    }, 1500)
  }, [editRows])

  const handleSaveCred = useCallback(async () => {
    if (!editCred) return
    const name = editCredName.trim()
    if (!name) return
    setSavingCred(true)
    try {
      const updated = await updateLlmCredential(
        editCred.id,
        {
          name,
          base_url: editCredBaseUrl.trim() || null,
          openai_api_mode: editCredApiMode || null,
          ...(editCredApiKey.trim() ? { api_key: editCredApiKey.trim() } : {}),
        },
        accessToken,
      )
      setCreds((prev) => prev.map((c) => (c.id === updated.id ? { ...updated, routes: c.routes } : c)))
      setEditCred((prev) => prev ? { ...prev, name: updated.name, base_url: updated.base_url, openai_api_mode: updated.openai_api_mode } : null)
      setEditCredApiKey('')
      addToast(tc.toastCredUpdated, 'success')
    } catch {
      addToast(tc.toastCredUpdateFailed, 'error')
    } finally {
      setSavingCred(false)
    }
  }, [editCred, editCredName, editCredBaseUrl, editCredApiMode, editCredApiKey, accessToken, addToast, tc])

  const columns: Column<LlmCredential>[] = [
    {
      key: 'name',
      header: tc.colName,
      render: (row) => <span className="font-medium text-[var(--c-text-primary)]">{row.name}</span>,
    },
    {
      key: 'provider',
      header: tc.colProvider,
      render: (row) => <Badge variant={providerVariant(row.provider)}>{row.provider}</Badge>,
    },
    {
      key: 'key_prefix',
      header: tc.colKeyPrefix,
      render: (row) =>
        row.key_prefix ? (
          <span className="font-mono text-xs">{row.key_prefix}…</span>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'base_url',
      header: tc.colBaseUrl,
      render: (row) => (
        <span className="text-xs">{row.base_url ?? <span className="text-[var(--c-text-muted)]">--</span>}</span>
      ),
    },
    {
      key: 'openai_api_mode',
      header: tc.colApiMode,
      render: (row) =>
        row.openai_api_mode ? (
          <Badge variant="neutral">{row.openai_api_mode}</Badge>
        ) : (
          <span className="text-[var(--c-text-muted)]">--</span>
        ),
    },
    {
      key: 'routes_count',
      header: tc.colRoutes,
      render: (row) => <span className="tabular-nums">{row.routes.length}</span>,
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="text-xs tabular-nums">
          {new Date(row.created_at).toLocaleString()}
        </span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => (
        <div className="flex items-center gap-1">
          <button
            onClick={(e) => {
              e.stopPropagation()
              handleOpenEdit(row)
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            title={tc.editRoutesTitle}
          >
            <Pencil size={14} />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              setDeleteTarget({ id: row.id, name: row.name })
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
            title={tc.deleteConfirm}
          >
            <Trash2 size={14} />
          </button>
        </div>
      ),
    },
  ]

  const actions = (
    <button
      onClick={handleOpenCreate}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      <Plus size={13} />
      {tc.addCredential}
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={actions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={creds}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<KeyRound size={28} />}
        />
      </div>

      {/* Create Modal */}
      <Modal open={createOpen} onClose={handleCloseCreate} title={tc.modalTitle} width="560px">
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={form.name}
              onChange={(e) => handleFormField('name', e.target.value)}
              placeholder="my-openai"
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
            />
          </FormField>

          <FormField label={tc.fieldProvider}>
            <select
              value={form.provider}
              onChange={(e) => handleFormField('provider', e.target.value)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
            >
              {PROVIDERS.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </FormField>

          <FormField label={tc.fieldApiKey}>
            <input
              type="password"
              value={form.api_key}
              onChange={(e) => handleFormField('api_key', e.target.value)}
              placeholder="sk-..."
              autoComplete="off"
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
            />
          </FormField>

          <FormField label={tc.fieldBaseUrl}>
            <input
              type="text"
              value={form.base_url}
              onChange={(e) => handleFormField('base_url', e.target.value)}
              placeholder="https://api.openai.com/v1"
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
            />
          </FormField>

          {form.provider === 'openai' && (
            <FormField label={tc.fieldApiMode}>
              <select
                value={form.openai_api_mode}
                onChange={(e) => handleFormField('openai_api_mode', e.target.value)}
                className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
              >
                <option value="">-- none --</option>
                {OPENAI_API_MODES.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </FormField>
          )}

          {/* Routes */}
          <div className="flex flex-col gap-2">
            <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{tc.fieldRoutes}</span>
            {form.routes.map((route, idx) => (
              <div key={idx} className="flex items-start gap-2 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-3">
                <div className="flex flex-1 flex-col gap-2">
                  <div className="flex gap-2">
                    <div className="flex flex-1 flex-col gap-1">
                      <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeModel}</span>
                      <input
                        type="text"
                        value={route.model}
                        onChange={(e) => handleRouteField(idx, 'model', e.target.value)}
                        placeholder="gpt-4o"
                        className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
                      />
                    </div>
                    <div className="flex w-20 flex-col gap-1">
                      <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routePriority}</span>
                      <input
                        type="number"
                        value={route.priority}
                        onChange={(e) => handleRouteField(idx, 'priority', e.target.value)}
                        className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id={`route-default-${idx}`}
                      checked={route.is_default}
                      onChange={(e) => handleRouteField(idx, 'is_default', e.target.checked)}
                      className="h-3.5 w-3.5 rounded"
                    />
                    <label htmlFor={`route-default-${idx}`} className="text-xs text-[var(--c-text-secondary)]">
                      {tc.routeDefault}
                    </label>
                  </div>
                  <div className="flex flex-col gap-1">
                    <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeWhen}</span>
                    <input
                      type="text"
                      value={route.when}
                      onChange={(e) => handleRouteField(idx, 'when', e.target.value)}
                      placeholder="{}"
                      className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 font-mono text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
                    />
                  </div>
                  <div className="flex gap-2">
                    <div className="flex w-24 flex-col gap-1">
                      <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeMultiplier}</span>
                      <input
                        type="number"
                        step="0.1"
                        min="0"
                        value={route.multiplier}
                        onChange={(e) => handleRouteField(idx, 'multiplier', e.target.value)}
                        placeholder="1"
                        className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                      />
                    </div>
                    <div className="flex flex-1 flex-col gap-1">
                      <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeCostInput}</span>
                      <input
                        type="number"
                        step="0.001"
                        min="0"
                        value={route.cost_per_1k_input}
                        onChange={(e) => handleRouteField(idx, 'cost_per_1k_input', e.target.value)}
                        placeholder="0.00"
                        className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                      />
                    </div>
                    <div className="flex flex-1 flex-col gap-1">
                      <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeCostOutput}</span>
                      <input
                        type="number"
                        step="0.001"
                        min="0"
                        value={route.cost_per_1k_output}
                        onChange={(e) => handleRouteField(idx, 'cost_per_1k_output', e.target.value)}
                        placeholder="0.00"
                        className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                      />
                    </div>
                  </div>
                </div>
                <button
                  onClick={() => handleRemoveRoute(idx)}
                  className="mt-5 flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-status-error-text)]"
                >
                  <Trash2 size={13} />
                </button>
              </div>
            ))}
            <button
              onClick={handleAddRoute}
              type="button"
              className="flex items-center gap-1.5 self-start rounded border border-dashed border-[var(--c-border)] px-2.5 py-1.5 text-xs text-[var(--c-text-muted)] transition-colors hover:border-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]"
            >
              <Plus size={12} />
              {tc.addRoute}
            </button>
          </div>

          {formError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{formError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseCreate}
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
              {submitting ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>

      {/* Edit Credential Modal */}
      <Modal
        open={editCred !== null}
        onClose={handleCloseEdit}
        title={`${tc.editRoutesTitle} — ${editCred?.name ?? ''}`}
        width="560px"
      >
        <div className="flex flex-col gap-4">
          {/* 凭证元数据编辑 */}
          <div className="flex flex-col gap-3 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-3">
            <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{tc.editCredTitle}</span>
            <FormField label={tc.fieldName}>
              <input
                type="text"
                value={editCredName}
                onChange={(e) => setEditCredName(e.target.value)}
                className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] focus:outline-none"
              />
            </FormField>
            <FormField label={tc.fieldBaseUrl}>
              <input
                type="text"
                value={editCredBaseUrl}
                onChange={(e) => setEditCredBaseUrl(e.target.value)}
                placeholder="https://openrouter.ai/api/v1"
                className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
              />
            </FormField>
            {editCred?.provider === 'openai' && (
              <FormField label={tc.fieldApiMode}>
                <select
                  value={editCredApiMode}
                  onChange={(e) => setEditCredApiMode(e.target.value)}
                  className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
                >
                  <option value="">-- none --</option>
                  {OPENAI_API_MODES.map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))}
                </select>
              </FormField>
            )}
            <FormField label={tc.fieldApiKeyOptional}>
              <input
                type="password"
                value={editCredApiKey}
                onChange={(e) => setEditCredApiKey(e.target.value)}
                autoComplete="new-password"
                className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] focus:outline-none"
              />
            </FormField>
            <div className="flex justify-end">
              <button
                onClick={() => void handleSaveCred()}
                disabled={savingCred}
                className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {savingCred ? '...' : tc.editCredSave}
              </button>
            </div>
          </div>

          {/* 路由编辑 */}
          <div className="flex flex-col gap-2">
            <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{tc.fieldRoutes}</span>
            {editRows.length === 0 && (
              <p className="text-xs text-[var(--c-text-muted)]">{tc.empty}</p>
            )}
            {editRows.map((row, idx) => (
              <div key={row.id} className="flex flex-col gap-2 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-3">
                <div className="flex gap-2">
                  <div className="flex flex-1 flex-col gap-1">
                    <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeModel}</span>
                    <div className="flex items-center gap-1">
                      <input
                        type="text"
                        value={row.draftModel}
                        onChange={(e) => handleEditRowField(idx, 'draftModel', e.target.value)}
                        className="flex-1 rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                      />
                      <button
                        onClick={() => handleCopyModel(idx)}
                        className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
                        title="Copy model ID"
                      >
                        {row.copied ? <Check size={13} className="text-[var(--c-status-success-text)]" /> : <Copy size={13} />}
                      </button>
                    </div>
                  </div>
                  <div className="flex w-20 flex-col gap-1">
                    <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routePriority}</span>
                    <input
                      type="number"
                      value={row.draftPriority}
                      onChange={(e) => handleEditRowField(idx, 'draftPriority', e.target.value)}
                      className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                    />
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id={`edit-route-default-${idx}`}
                    checked={row.draftIsDefault}
                    onChange={(e) => handleEditRowField(idx, 'draftIsDefault', e.target.checked)}
                    className="h-3.5 w-3.5 rounded"
                  />
                  <label htmlFor={`edit-route-default-${idx}`} className="text-xs text-[var(--c-text-secondary)]">
                    {tc.routeDefault}
                  </label>
                </div>
                <div className="flex flex-col gap-1">
                  <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeWhen}</span>
                  <input
                    type="text"
                    value={row.draftWhen}
                    onChange={(e) => handleEditRowField(idx, 'draftWhen', e.target.value)}
                    placeholder="{}"
                    className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 font-mono text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
                  />
                </div>
                <div className="flex gap-2">
                  <div className="flex w-24 flex-col gap-1">
                    <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeMultiplier}</span>
                    <input
                      type="number"
                      step="0.1"
                      min="0"
                      value={row.draftMultiplier}
                      onChange={(e) => handleEditRowField(idx, 'draftMultiplier', e.target.value)}
                      placeholder="1"
                      className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                    />
                  </div>
                  <div className="flex flex-1 flex-col gap-1">
                    <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeCostInput}</span>
                    <input
                      type="number"
                      step="0.001"
                      min="0"
                      value={row.draftCostInput}
                      onChange={(e) => handleEditRowField(idx, 'draftCostInput', e.target.value)}
                      placeholder="0.00"
                      className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                    />
                  </div>
                  <div className="flex flex-1 flex-col gap-1">
                    <span className="text-[10px] text-[var(--c-text-muted)]">{tc.routeCostOutput}</span>
                    <input
                      type="number"
                      step="0.001"
                      min="0"
                      value={row.draftCostOutput}
                      onChange={(e) => handleEditRowField(idx, 'draftCostOutput', e.target.value)}
                      placeholder="0.00"
                      className="rounded border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1 text-xs text-[var(--c-text-primary)] focus:outline-none"
                    />
                  </div>
                </div>
                <div className="flex justify-end">
                  <button
                    onClick={() => void handleSaveRoute(idx)}
                    disabled={row.saving}
                    className="rounded-lg bg-[var(--c-bg-tag)] px-3 py-1 text-xs font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  >
                    {row.saving ? '...' : tc.editRoutesSave}
                  </button>
                </div>
              </div>
            ))}
          </div>

          <div className="flex justify-end border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseEdit}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              {tc.cancel}
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
