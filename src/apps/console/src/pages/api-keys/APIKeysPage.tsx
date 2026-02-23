import { useState, useCallback, useEffect, useRef } from 'react'
import { useOutletContext } from 'react-router-dom'
import { KeyRound, Plus, Ban } from 'lucide-react'
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
  listAPIKeys,
  createAPIKey,
  revokeAPIKey,
  type APIKey,
  type CreateAPIKeyResponse,
} from '../../api/api-keys'

function parseScopes(raw: string): string[] {
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

export function APIKeysPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.apiKeys

  const [keys, setKeys] = useState<APIKey[]>([])
  const [loading, setLoading] = useState(false)

  // create modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createScopes, setCreateScopes] = useState('')
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  // key reveal modal (one-time display after creation)
  const [revealKey, setRevealKey] = useState<CreateAPIKeyResponse | null>(null)
  const [copiedKey, setCopiedKey] = useState(false)
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // revoke dialog
  const [revokeTarget, setRevokeTarget] = useState<APIKey | null>(null)
  const [revoking, setRevoking] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listAPIKeys(accessToken)
      setKeys(list)
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
    setCreateName('')
    setCreateScopes('')
    setCreateError('')
    setCreateOpen(true)
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (creating) return
    setCreateOpen(false)
  }, [creating])

  const handleCreate = useCallback(async () => {
    const name = createName.trim()
    if (!name) {
      setCreateError(tc.errRequired)
      return
    }

    setCreating(true)
    setCreateError('')
    try {
      const result = await createAPIKey(
        { name, scopes: parseScopes(createScopes) },
        accessToken,
      )
      setCreateOpen(false)
      setRevealKey(result)
      setCopiedKey(false)
      await fetchAll()
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastCreateFailed)
    } finally {
      setCreating(false)
    }
  }, [createName, createScopes, accessToken, fetchAll, tc])

  const handleCopyKey = useCallback(() => {
    if (!revealKey) return
    void navigator.clipboard.writeText(revealKey.key)
    setCopiedKey(true)
    if (copyTimerRef.current) clearTimeout(copyTimerRef.current)
    copyTimerRef.current = setTimeout(() => setCopiedKey(false), 2000)
  }, [revealKey])

  const handleDoneReveal = useCallback(() => {
    setRevealKey(null)
  }, [])

  const handleRevoke = useCallback(async () => {
    if (!revokeTarget) return
    setRevoking(true)
    try {
      await revokeAPIKey(revokeTarget.id, accessToken)
      addToast(tc.toastRevoked, 'success')
      setRevokeTarget(null)
      await fetchAll()
    } catch {
      addToast(tc.toastRevokeFailed, 'error')
    } finally {
      setRevoking(false)
    }
  }, [revokeTarget, accessToken, fetchAll, addToast, tc])

  const columns: Column<APIKey>[] = [
    {
      key: 'key_prefix',
      header: tc.colKeyPrefix,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-primary)]">{row.key_prefix}</span>
      ),
    },
    {
      key: 'name',
      header: tc.colName,
      render: (row) => (
        <span className="font-medium text-[var(--c-text-primary)]">{row.name}</span>
      ),
    },
    {
      key: 'scopes',
      header: tc.colScopes,
      render: (row) => (
        <div className="flex flex-wrap gap-1">
          {row.scopes.length === 0 ? (
            <span className="text-xs text-[var(--c-text-muted)]">—</span>
          ) : (
            row.scopes.map((s) => (
              <Badge key={s} variant="neutral">
                {s}
              </Badge>
            ))
          )}
        </div>
      ),
    },
    {
      key: 'last_used_at',
      header: tc.colLastUsedAt,
      render: (row) => (
        <span className="tabular-nums text-xs text-[var(--c-text-secondary)]">
          {row.last_used_at ? new Date(row.last_used_at).toLocaleString() : '—'}
        </span>
      ),
    },
    {
      key: 'status',
      header: tc.colStatus,
      render: (row) =>
        row.revoked_at ? (
          <Badge variant="error">{tc.statusRevoked}</Badge>
        ) : (
          <Badge variant="success">{tc.statusActive}</Badge>
        ),
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="tabular-nums text-xs">{new Date(row.created_at).toLocaleString()}</span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => {
        const isRevoked = !!row.revoked_at
        return (
          <button
            onClick={(e) => {
              e.stopPropagation()
              if (!isRevoked) setRevokeTarget(row)
            }}
            disabled={isRevoked}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)] disabled:cursor-not-allowed disabled:opacity-30"
            title={tc.revokeTitle}
          >
            <Ban size={13} />
          </button>
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
      {tc.addKey}
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
          data={keys}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<KeyRound size={28} />}
        />
      </div>

      {/* Create Modal */}
      <Modal open={createOpen} onClose={handleCloseCreate} title={tc.modalTitleCreate} width="440px">
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={createName}
              onChange={(e) => {
                setCreateName(e.target.value)
                setCreateError('')
              }}
              placeholder="My Key"
              className={inputCls}
              autoFocus
            />
          </FormField>

          <FormField label={tc.fieldScopes}>
            <input
              type="text"
              value={createScopes}
              onChange={(e) => setCreateScopes(e.target.value)}
              placeholder={tc.fieldScopesPlaceholder}
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

      {/* Key Reveal Modal -- one-time display */}
      <Modal
        open={revealKey !== null}
        onClose={() => {}}
        title={tc.modalTitleKeyCreated}
        width="480px"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-[var(--c-status-warning-text)]">{tc.keyRevealNote}</p>

          <div className="flex items-center gap-2 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2">
            <code className="flex-1 break-all font-mono text-xs text-[var(--c-text-primary)]">
              {revealKey?.key}
            </code>
            <button
              onClick={handleCopyKey}
              className="shrink-0 rounded px-2 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              {copiedKey ? tc.copied : tc.copyKey}
            </button>
          </div>

          <div className="flex justify-end border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleDoneReveal}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              {tc.done}
            </button>
          </div>
        </div>
      </Modal>

      {/* Revoke Confirm Dialog */}
      <ConfirmDialog
        open={revokeTarget !== null}
        onClose={() => {
          if (!revoking) setRevokeTarget(null)
        }}
        onConfirm={handleRevoke}
        title={tc.revokeTitle}
        message={tc.revokeMessage(revokeTarget?.key_prefix ?? '')}
        confirmLabel={tc.revokeConfirm}
        loading={revoking}
      />
    </div>
  )
}
