import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { KeyRound, Plus, Trash2 } from 'lucide-react'
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
  type LlmCredential,
  type CreateLlmRouteRequest,
} from '../../api/llm-credentials'

const PROVIDERS = ['openai', 'anthropic', 'gemini', 'deepseek'] as const
const OPENAI_API_MODES = ['auto', 'responses', 'chat_completions'] as const

function providerVariant(provider: string): BadgeVariant {
  switch (provider) {
    case 'anthropic': return 'warning'
    case 'gemini': return 'success'
    default: return 'neutral'
  }
}

type RouteRow = {
  model: string
  priority: string
  is_default: boolean
  when: string
}

function emptyRoute(): RouteRow {
  return { model: '', priority: '0', is_default: false, when: '' }
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
