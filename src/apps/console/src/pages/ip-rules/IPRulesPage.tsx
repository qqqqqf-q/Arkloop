import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { ShieldCheck, Plus, Trash2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { formatDateTime, useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listIPRules,
  createIPRule,
  deleteIPRule,
  type IPRule,
} from '../../api/ip-rules'

export function IPRulesPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.ipRules

  const [rules, setRules] = useState<IPRule[]>([])
  const [loading, setLoading] = useState(false)

  // create modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createType, setCreateType] = useState<'allowlist' | 'blocklist'>('allowlist')
  const [createCIDR, setCreateCIDR] = useState('')
  const [createNote, setCreateNote] = useState('')
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  // delete dialog
  const [deleteTarget, setDeleteTarget] = useState<IPRule | null>(null)
  const [deleting, setDeleting] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listIPRules(accessToken)
      setRules(list)
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
    setCreateType('allowlist')
    setCreateCIDR('')
    setCreateNote('')
    setCreateError('')
    setCreateOpen(true)
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (creating) return
    setCreateOpen(false)
  }, [creating])

  const handleCreate = useCallback(async () => {
    const cidr = createCIDR.trim()
    if (!cidr) {
      setCreateError(tc.errRequired)
      return
    }

    setCreating(true)
    setCreateError('')
    try {
      await createIPRule(
        {
          type: createType,
          cidr,
          note: createNote.trim() || undefined,
        },
        accessToken,
      )
      addToast(tc.toastCreated, 'success')
      setCreateOpen(false)
      await fetchAll()
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastCreateFailed)
    } finally {
      setCreating(false)
    }
  }, [createType, createCIDR, createNote, accessToken, fetchAll, addToast, tc])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteIPRule(deleteTarget.id, accessToken)
      addToast(tc.toastDeleted, 'success')
      setDeleteTarget(null)
      await fetchAll()
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, fetchAll, addToast, tc])

  const columns: Column<IPRule>[] = [
    {
      key: 'type',
      header: tc.colType,
      render: (row) => (
        <Badge variant={row.type === 'allowlist' ? 'success' : 'error'}>
          {row.type === 'allowlist' ? tc.typeAllowlist : tc.typeBlocklist}
        </Badge>
      ),
    },
    {
      key: 'cidr',
      header: tc.colCIDR,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-primary)]">{row.cidr}</span>
      ),
    },
    {
      key: 'note',
      header: tc.colNote,
      render: (row) => (
        <span className="text-xs text-[var(--c-text-secondary)]">{row.note ?? '—'}</span>
      ),
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="tabular-nums text-xs">{formatDateTime(row.created_at, { includeZone: false })}</span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => (
        <button
          onClick={(e) => {
            e.stopPropagation()
            setDeleteTarget(row)
          }}
          className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
          title={tc.deleteTitle}
        >
          <Trash2 size={13} />
        </button>
      ),
    },
  ]

  const headerActions = (
    <button
      onClick={handleOpenCreate}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      <Plus size={13} />
      {tc.addRule}
    </button>
  )

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={headerActions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={rules}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<ShieldCheck size={28} />}
        />
      </div>

      {/* Create Modal */}
      <Modal open={createOpen} onClose={handleCloseCreate} title={tc.modalTitleCreate} width="440px">
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldType}>
            <select
              value={createType}
              onChange={(e) => setCreateType(e.target.value as 'allowlist' | 'blocklist')}
              className={inputCls}
            >
              <option value="allowlist">{tc.typeOptionAllowlist}</option>
              <option value="blocklist">{tc.typeOptionBlocklist}</option>
            </select>
          </FormField>

          <FormField label={tc.fieldCIDR}>
            <input
              type="text"
              value={createCIDR}
              onChange={(e) => {
                setCreateCIDR(e.target.value)
                setCreateError('')
              }}
              placeholder={tc.fieldCIDRPlaceholder}
              className={inputCls}
              autoFocus
            />
          </FormField>

          <FormField label={tc.fieldNote}>
            <input
              type="text"
              value={createNote}
              onChange={(e) => setCreateNote(e.target.value)}
              placeholder={tc.fieldNotePlaceholder}
              className={inputCls}
            />
          </FormField>

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

      {/* Delete Confirm Dialog */}
      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => {
          if (!deleting) setDeleteTarget(null)
        }}
        onConfirm={handleDelete}
        title={tc.deleteTitle}
        message={tc.deleteMessage(deleteTarget?.cidr ?? '')}
        confirmLabel={tc.deleteConfirm}
        loading={deleting}
      />
    </div>
  )
}
