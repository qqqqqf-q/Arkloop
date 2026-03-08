import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Server, Plus, Pencil, Trash2 } from 'lucide-react'
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
  listMCPConfigs,
  createMCPConfig,
  patchMCPConfig,
  deleteMCPConfig,
  type MCPConfig,
} from '../../api/mcp-configs'
import { notifyToolCatalogChanged } from '../../lib/toolCatalogRefresh'

type Transport = 'stdio' | 'http_sse' | 'streamable_http'

type CreateFormState = {
  name: string
  transport: Transport
  url: string
  bearerToken: string
  command: string
  args: string
  isActive: boolean
}

type EditFormState = {
  name: string
  url: string
  bearerToken: string
  isActive: boolean
}

function emptyCreateForm(): CreateFormState {
  return { name: '', transport: 'http_sse', url: '', bearerToken: '', command: '', args: '', isActive: true }
}

function configToEditForm(cfg: MCPConfig): EditFormState {
  return { name: cfg.name, url: cfg.url ?? '', bearerToken: '', isActive: cfg.is_active }
}

function transportBadge(transport: string) {
  switch (transport) {
    case 'http_sse':
      return <Badge variant="neutral">{transport}</Badge>
    case 'streamable_http':
      return <Badge variant="warning">{transport}</Badge>
    case 'stdio':
      return <Badge variant="neutral">{transport}</Badge>
    default:
      return <Badge variant="neutral">{transport}</Badge>
  }
}

type DeleteTarget = { id: string; name: string }

export function MCPConfigsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.mcpConfigs

  const [configs, setConfigs] = useState<MCPConfig[]>([])
  const [loading, setLoading] = useState(false)

  // create modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateFormState>(emptyCreateForm)
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  // edit modal
  const [editTarget, setEditTarget] = useState<MCPConfig | null>(null)
  const [editForm, setEditForm] = useState<EditFormState>({ name: '', url: '', bearerToken: '', isActive: true })
  const [editError, setEditError] = useState('')
  const [saving, setSaving] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const [deleting, setDeleting] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listMCPConfigs(accessToken)
      setConfigs(list)
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

  const handleOpenEdit = useCallback((cfg: MCPConfig) => {
    setEditTarget(cfg)
    setEditForm(configToEditForm(cfg))
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
    const name = createForm.name.trim()
    if (!name) {
      setCreateError(tc.errRequired)
      return
    }
    const isHttp = createForm.transport !== 'stdio'
    if (isHttp && !createForm.url.trim()) {
      setCreateError(tc.errUrlRequired)
      return
    }
    if (!isHttp && !createForm.command.trim()) {
      setCreateError(tc.errCommandRequired)
      return
    }

    setCreating(true)
    setCreateError('')
    try {
      const req = isHttp
        ? {
            name,
            transport: createForm.transport,
            url: createForm.url.trim(),
            bearer_token: createForm.bearerToken.trim() || undefined,
            is_active: createForm.isActive,
          }
        : {
            name,
            transport: createForm.transport,
            command: createForm.command.trim(),
            args: createForm.args
              .split(',')
              .map((s) => s.trim())
              .filter(Boolean),
            is_active: createForm.isActive,
          }
      await createMCPConfig(req, accessToken)
      addToast(tc.toastCreated, 'success')
      setCreateOpen(false)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastSaveFailed)
    } finally {
      setCreating(false)
    }
  }, [createForm, accessToken, fetchAll, addToast, tc])

  const handleSave = useCallback(async () => {
    if (!editTarget) return
    const name = editForm.name.trim()
    if (!name) {
      setEditError(tc.errRequired)
      return
    }

    setSaving(true)
    setEditError('')
    try {
      const req: Record<string, unknown> = { name, is_active: editForm.isActive }
      if (editTarget.transport !== 'stdio') {
        if (editForm.url.trim()) req.url = editForm.url.trim()
        if (editForm.bearerToken.trim()) req.bearer_token = editForm.bearerToken.trim()
      }
      await patchMCPConfig(editTarget.id, req, accessToken)
      addToast(tc.toastUpdated, 'success')
      setEditTarget(null)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch (err) {
      setEditError(isApiError(err) ? err.message : tc.toastSaveFailed)
    } finally {
      setSaving(false)
    }
  }, [editForm, editTarget, accessToken, fetchAll, addToast, tc])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteMCPConfig(deleteTarget.id, accessToken)
      setDeleteTarget(null)
      notifyToolCatalogChanged()
      await fetchAll()
      addToast(tc.toastDeleted, 'success')
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, fetchAll, addToast, tc])

  const columns: Column<MCPConfig>[] = [
    {
      key: 'name',
      header: tc.colName,
      render: (row) => (
        <span className="font-medium text-[var(--c-text-primary)]">{row.name}</span>
      ),
    },
    {
      key: 'transport',
      header: tc.colTransport,
      render: (row) => transportBadge(row.transport),
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
      render: (row) => (
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
      ),
    },
  ]

  const headerActions = (
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

  const isHttpTransport = createForm.transport !== 'stdio'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={headerActions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={configs}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<Server size={28} />}
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
          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={createForm.name}
              onChange={(e) => setCreateField('name', e.target.value)}
              placeholder="my-mcp-server"
              className={inputCls}
            />
          </FormField>

          <FormField label={tc.fieldTransport}>
            <select
              value={createForm.transport}
              onChange={(e) => setCreateField('transport', e.target.value as Transport)}
              className={inputCls}
            >
              <option value="http_sse">http_sse</option>
              <option value="streamable_http">streamable_http</option>
              <option value="stdio">stdio</option>
            </select>
          </FormField>

          {isHttpTransport && (
            <>
              <FormField label={tc.fieldUrl}>
                <input
                  type="text"
                  value={createForm.url}
                  onChange={(e) => setCreateField('url', e.target.value)}
                  placeholder="https://example.com/mcp"
                  className={inputCls}
                />
              </FormField>
              <FormField label={tc.fieldBearerToken}>
                <input
                  type="password"
                  value={createForm.bearerToken}
                  onChange={(e) => setCreateField('bearerToken', e.target.value)}
                  placeholder={tc.fieldBearerTokenPlaceholder}
                  className={inputCls}
                />
              </FormField>
            </>
          )}

          {!isHttpTransport && (
            <>
              <FormField label={tc.fieldCommand}>
                <input
                  type="text"
                  value={createForm.command}
                  onChange={(e) => setCreateField('command', e.target.value)}
                  placeholder="/usr/local/bin/my-server"
                  className={inputCls}
                />
              </FormField>
              <FormField label={tc.fieldArgs}>
                <input
                  type="text"
                  value={createForm.args}
                  onChange={(e) => setCreateField('args', e.target.value)}
                  placeholder="--port, 8080"
                  className={inputCls}
                />
              </FormField>
            </>
          )}

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="mcp-create-is-active"
              checked={createForm.isActive}
              onChange={(e) => setCreateField('isActive', e.target.checked)}
              className="h-3.5 w-3.5 rounded"
            />
            <label
              htmlFor="mcp-create-is-active"
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
          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={editForm.name}
              onChange={(e) => setEditField('name', e.target.value)}
              className={inputCls}
            />
          </FormField>

          {/* transport 不可修改，只读展示 */}
          <FormField label={tc.fieldTransport}>
            <div className="flex items-center">
              {editTarget && transportBadge(editTarget.transport)}
            </div>
          </FormField>

          {editTarget && editTarget.transport !== 'stdio' && (
            <>
              <FormField label={tc.fieldUrl}>
                <input
                  type="text"
                  value={editForm.url}
                  onChange={(e) => setEditField('url', e.target.value)}
                  className={inputCls}
                />
              </FormField>
              <FormField label={tc.fieldBearerToken}>
                {editTarget.has_auth && (
                  <p className="mb-1 text-xs text-[var(--c-text-muted)]">
                    {tc.fieldBearerTokenSet}
                  </p>
                )}
                <input
                  type="password"
                  value={editForm.bearerToken}
                  onChange={(e) => setEditField('bearerToken', e.target.value)}
                  placeholder={tc.fieldBearerTokenPlaceholder}
                  className={inputCls}
                />
              </FormField>
            </>
          )}

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="mcp-edit-is-active"
              checked={editForm.isActive}
              onChange={(e) => setEditField('isActive', e.target.checked)}
              className="h-3.5 w-3.5 rounded"
            />
            <label
              htmlFor="mcp-edit-is-active"
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
