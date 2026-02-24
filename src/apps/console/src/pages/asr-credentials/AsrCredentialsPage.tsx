import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Mic, Plus, Trash2, Star } from 'lucide-react'
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
  listAsrCredentials,
  createAsrCredential,
  deleteAsrCredential,
  setDefaultAsrCredential,
  type AsrCredential,
} from '../../api/asr-credentials'

const ASR_PROVIDERS = ['groq', 'openai'] as const

const MODEL_OPTIONS: Record<string, string[]> = {
  groq: ['whisper-large-v3-turbo', 'whisper-large-v3', 'distil-whisper-large-v3-en'],
  openai: ['whisper-1'],
}

type CreateFormState = {
  name: string
  provider: string
  api_key: string
  base_url: string
  model: string
  is_default: boolean
}

function emptyForm(): CreateFormState {
  return {
    name: '',
    provider: 'groq',
    api_key: '',
    base_url: '',
    model: 'whisper-large-v3-turbo',
    is_default: false,
  }
}

type DeleteTarget = { id: string; name: string }

export function AsrCredentialsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.asrCredentials

  const [creds, setCreds] = useState<AsrCredential[]>([])
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
      setCreds(await listAsrCredentials(accessToken))
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc])

  useEffect(() => {
    void fetchCreds()
  }, [fetchCreds])

  const handleFormField = useCallback(<K extends keyof CreateFormState>(key: K, value: CreateFormState[K]) => {
    setForm((prev) => {
      const next = { ...prev, [key]: value }
      if (key === 'provider' && typeof value === 'string') {
        const opts = MODEL_OPTIONS[value] ?? []
        next.model = opts[0] ?? ''
      }
      return next
    })
    setFormError('')
  }, [])

  const handleSubmit = useCallback(async () => {
    const name = form.name.trim()
    const api_key = form.api_key.trim()
    const provider = form.provider.trim()
    const model = form.model.trim()

    if (!name || !api_key || !provider || !model) {
      setFormError(tc.errRequired)
      return
    }

    setSubmitting(true)
    setFormError('')
    try {
      await createAsrCredential(
        {
          name,
          provider,
          api_key,
          base_url: form.base_url.trim() || undefined,
          model,
          is_default: form.is_default,
        },
        accessToken,
      )
      setCreateOpen(false)
      await fetchCreds()
      addToast(tc.toastCreated, 'success')
    } catch (err) {
      if (isApiError(err)) {
        setFormError(err.code === 'database.not_configured' ? tc.errEncryptionKey : err.message)
      } else {
        setFormError(tc.errRequired)
      }
    } finally {
      setSubmitting(false)
    }
  }, [form, accessToken, fetchCreds, addToast, tc])

  const handleSetDefault = useCallback(async (id: string) => {
    try {
      await setDefaultAsrCredential(id, accessToken)
      await fetchCreds()
      addToast(tc.toastDefaultSet, 'success')
    } catch {
      addToast(tc.toastDefaultFailed, 'error')
    }
  }, [accessToken, fetchCreds, addToast, tc])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteAsrCredential(deleteTarget.id, accessToken)
      setDeleteTarget(null)
      await fetchCreds()
      addToast(tc.toastDeleted, 'success')
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, fetchCreds, addToast, tc])

  const columns: Column<AsrCredential>[] = [
    {
      key: 'name',
      header: tc.colName,
      render: (row) => (
        <div className="flex items-center gap-1.5">
          <span className="font-medium text-[var(--c-text-primary)]">{row.name}</span>
          {row.is_default && <Badge variant="success">default</Badge>}
        </div>
      ),
    },
    {
      key: 'provider',
      header: tc.colProvider,
      render: (row) => <Badge variant="neutral">{row.provider}</Badge>,
    },
    {
      key: 'model',
      header: tc.colModel,
      render: (row) => <span className="font-mono text-xs">{row.model}</span>,
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
          {!row.is_default && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                void handleSetDefault(row.id)
              }}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
              title={tc.setDefault}
            >
              <Star size={14} />
            </button>
          )}
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
      onClick={() => {
        setForm(emptyForm())
        setFormError('')
        setCreateOpen(true)
      }}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      <Plus size={13} />
      {tc.addCredential}
    </button>
  )

  const modelOptions = MODEL_OPTIONS[form.provider] ?? []

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
          emptyIcon={<Mic size={28} />}
        />
      </div>

      <Modal
        open={createOpen}
        onClose={() => { if (!submitting) setCreateOpen(false) }}
        title={tc.modalTitle}
        width="480px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={form.name}
              onChange={(e) => handleFormField('name', e.target.value)}
              placeholder="my-groq-whisper"
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
            />
          </FormField>

          <FormField label={tc.fieldProvider}>
            <select
              value={form.provider}
              onChange={(e) => handleFormField('provider', e.target.value)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
            >
              {ASR_PROVIDERS.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </FormField>

          <FormField label={tc.fieldModel}>
            <select
              value={form.model}
              onChange={(e) => handleFormField('model', e.target.value)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] focus:outline-none"
            >
              {modelOptions.map((m) => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </FormField>

          <FormField label={tc.fieldApiKey}>
            <input
              type="password"
              value={form.api_key}
              onChange={(e) => handleFormField('api_key', e.target.value)}
              placeholder="gsk_..."
              autoComplete="off"
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
            />
          </FormField>

          <FormField label={tc.fieldBaseUrl}>
            <input
              type="text"
              value={form.base_url}
              onChange={(e) => handleFormField('base_url', e.target.value)}
              placeholder="https://api.groq.com/openai/v1"
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
            />
          </FormField>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="asr-is-default"
              checked={form.is_default}
              onChange={(e) => handleFormField('is_default', e.target.checked)}
              className="h-3.5 w-3.5 rounded"
            />
            <label htmlFor="asr-is-default" className="text-xs text-[var(--c-text-secondary)]">
              {tc.fieldIsDefault}
            </label>
          </div>

          {formError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{formError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={() => { if (!submitting) setCreateOpen(false) }}
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
