import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Flag, Plus, Trash2, ChevronDown, ChevronRight } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listFeatureFlags,
  createFeatureFlag,
  updateFeatureFlagDefault,
  deleteFeatureFlag,
  listFlagProjectOverrides,
  setFlagProjectOverride,
  deleteFlagProjectOverride,
  type FeatureFlag,
  type ProjectFeatureOverride,
} from '../../api/feature-flags'

export function FeatureFlagsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.featureFlags

  const [flags, setFlags] = useState<FeatureFlag[]>([])
  const [loading, setLoading] = useState(false)

  // create modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createKey, setCreateKey] = useState('')
  const [createDesc, setCreateDesc] = useState('')
  const [createDefault, setCreateDefault] = useState(false)
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  // delete dialog
  const [deleteTarget, setDeleteTarget] = useState<FeatureFlag | null>(null)
  const [deleting, setDeleting] = useState(false)

  // expanded row for project overrides
  const [expandedKey, setExpandedKey] = useState<string | null>(null)
  const [overrides, setOverrides] = useState<ProjectFeatureOverride[]>([])
  const [overridesLoading, setOverridesLoading] = useState(false)

  // add override
  const [addOverrideOpen, setAddOverrideOpen] = useState(false)
  const [overrideProjectId, setOverrideProjectId] = useState('')
  const [overrideEnabled, setOverrideEnabled] = useState(true)
  const [addingOverride, setAddingOverride] = useState(false)
  const [overrideError, setOverrideError] = useState('')

  // delete override
  const [deleteOverrideTarget, setDeleteOverrideTarget] = useState<ProjectFeatureOverride | null>(null)
  const [deletingOverride, setDeletingOverride] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listFeatureFlags(accessToken)
      setFlags(list)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void fetchAll()
  }, [fetchAll])

  const fetchOverrides = useCallback(async (flagKey: string) => {
    setOverridesLoading(true)
    try {
      const list = await listFlagProjectOverrides(flagKey, accessToken)
      setOverrides(list)
    } catch {
      setOverrides([])
    } finally {
      setOverridesLoading(false)
    }
  }, [accessToken])

  const handleToggleExpand = useCallback((flagKey: string) => {
    if (expandedKey === flagKey) {
      setExpandedKey(null)
      setOverrides([])
    } else {
      setExpandedKey(flagKey)
      void fetchOverrides(flagKey)
    }
  }, [expandedKey, fetchOverrides])

  const handleToggleDefault = useCallback(async (flag: FeatureFlag) => {
    try {
      await updateFeatureFlagDefault(flag.key, { default_value: !flag.default_value }, accessToken)
      addToast(tc.toastUpdated, 'success')
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    }
  }, [accessToken, fetchAll, addToast, tc])

  // create handlers
  const handleOpenCreate = useCallback(() => {
    setCreateKey('')
    setCreateDesc('')
    setCreateDefault(false)
    setCreateError('')
    setCreateOpen(true)
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (creating) return
    setCreateOpen(false)
  }, [creating])

  const handleCreate = useCallback(async () => {
    const key = createKey.trim()
    if (!key) {
      setCreateError(tc.errKeyRequired)
      return
    }

    setCreating(true)
    setCreateError('')
    try {
      await createFeatureFlag(
        { key, description: createDesc.trim() || null, default_value: createDefault },
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
  }, [createKey, createDesc, createDefault, accessToken, fetchAll, addToast, tc])

  // delete handlers
  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteFeatureFlag(deleteTarget.key, accessToken)
      addToast(tc.toastDeleted, 'success')
      setDeleteTarget(null)
      if (expandedKey === deleteTarget.key) {
        setExpandedKey(null)
        setOverrides([])
      }
      await fetchAll()
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, expandedKey, accessToken, fetchAll, addToast, tc])

  // override handlers
  const handleOpenAddOverride = useCallback(() => {
    setOverrideProjectId('')
    setOverrideEnabled(true)
    setOverrideError('')
    setAddOverrideOpen(true)
  }, [])

  const handleAddOverride = useCallback(async () => {
    if (!expandedKey) return
    const projectId = overrideProjectId.trim()
    if (!projectId) {
      setOverrideError(tc.errProjectIdRequired)
      return
    }
    setAddingOverride(true)
    setOverrideError('')
    try {
      await setFlagProjectOverride(expandedKey, { account_id: projectId, enabled: overrideEnabled }, accessToken)
      addToast(tc.toastOverrideSet, 'success')
      setAddOverrideOpen(false)
      void fetchOverrides(expandedKey)
    } catch (err) {
      setOverrideError(isApiError(err) ? err.message : tc.toastOverrideSetFailed)
    } finally {
      setAddingOverride(false)
    }
  }, [expandedKey, overrideProjectId, overrideEnabled, accessToken, fetchOverrides, addToast, tc])

  const handleDeleteOverride = useCallback(async () => {
    if (!deleteOverrideTarget || !expandedKey) return
    setDeletingOverride(true)
    try {
      await deleteFlagProjectOverride(expandedKey, deleteOverrideTarget.account_id, accessToken)
      addToast(tc.toastOverrideDeleted, 'success')
      setDeleteOverrideTarget(null)
      void fetchOverrides(expandedKey)
    } catch {
      addToast(tc.toastOverrideDeleteFailed, 'error')
    } finally {
      setDeletingOverride(false)
    }
  }, [deleteOverrideTarget, expandedKey, accessToken, fetchOverrides, addToast, tc])

  const columns: Column<FeatureFlag>[] = [
    {
      key: 'expand',
      header: '',
      render: (row) => (
        <button
          onClick={(e) => { e.stopPropagation(); handleToggleExpand(row.key) }}
          className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)]"
        >
          {expandedKey === row.key ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
        </button>
      ),
    },
    {
      key: 'key',
      header: tc.colKey,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-primary)]">{row.key}</span>
      ),
    },
    {
      key: 'description',
      header: tc.colDescription,
      render: (row) => (
        <span className="text-xs text-[var(--c-text-secondary)]">{row.description ?? '—'}</span>
      ),
    },
    {
      key: 'default_value',
      header: tc.colDefaultValue,
      render: (row) => (
        <button
          onClick={(e) => { e.stopPropagation(); void handleToggleDefault(row) }}
          className="cursor-pointer"
        >
          <Badge variant={row.default_value ? 'success' : 'neutral'}>
            {row.default_value ? tc.enabled : tc.disabled}
          </Badge>
        </button>
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
      render: (row) => (
        <button
          onClick={(e) => { e.stopPropagation(); setDeleteTarget(row) }}
          className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
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
      {tc.addFlag}
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
          data={flags}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<Flag size={28} />}
        />

        {/* expanded project overrides section */}
        {expandedKey && (
          <div className="border-t border-[var(--c-border)] px-6 py-4">
            <div className="flex items-center justify-between">
              <h3 className="text-xs font-medium text-[var(--c-text-secondary)]">
                {tc.projectOverrides} ({expandedKey})
              </h3>
              <button
                onClick={handleOpenAddOverride}
                className="flex items-center gap-1 rounded-lg bg-[var(--c-bg-tag)] px-2.5 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
              >
                <Plus size={11} />
                {tc.addOverride}
              </button>
            </div>

            {overridesLoading ? (
              <p className="mt-3 text-xs text-[var(--c-text-muted)]">...</p>
            ) : overrides.length === 0 ? (
              <p className="mt-3 text-xs text-[var(--c-text-muted)]">{tc.overridesEmpty}</p>
            ) : (
              <table className="mt-3 w-full text-xs">
                <thead>
                  <tr className="text-left text-[var(--c-text-muted)]">
                    <th className="pb-2 font-medium">{tc.overrideProjectId}</th>
                    <th className="pb-2 font-medium">{tc.overrideEnabled}</th>
                    <th className="pb-2 font-medium">{tc.colCreatedAt}</th>
                    <th className="pb-2" />
                  </tr>
                </thead>
                <tbody>
                  {overrides.map((o) => (
                    <tr key={o.account_id} className="border-t border-[var(--c-border)]">
                      <td className="py-2 font-mono text-[var(--c-text-primary)]">{o.account_id}</td>
                      <td className="py-2">
                        <Badge variant={o.enabled ? 'success' : 'neutral'}>
                          {o.enabled ? tc.enabled : tc.disabled}
                        </Badge>
                      </td>
                      <td className="py-2 tabular-nums">{new Date(o.created_at).toLocaleString()}</td>
                      <td className="py-2 text-right">
                        <button
                          onClick={() => setDeleteOverrideTarget(o)}
                          className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-status-error-text)]"
                        >
                          <Trash2 size={12} />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>

      {/* Create Flag Modal */}
      <Modal open={createOpen} onClose={handleCloseCreate} title={tc.modalTitleCreate} width="440px">
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldKey}>
            <input
              type="text"
              value={createKey}
              onChange={(e) => { setCreateKey(e.target.value); setCreateError('') }}
              placeholder="e.g. registration.open"
              className={inputCls}
              autoFocus
            />
          </FormField>

          <FormField label={tc.fieldDescription}>
            <input
              type="text"
              value={createDesc}
              onChange={(e) => setCreateDesc(e.target.value)}
              className={inputCls}
            />
          </FormField>

          <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={createDefault}
              onChange={(e) => setCreateDefault(e.target.checked)}
              className="accent-[var(--c-status-success-text)]"
            />
            {tc.fieldDefaultValue}
          </label>

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

      {/* Add Override Modal */}
      <Modal
        open={addOverrideOpen}
        onClose={() => { if (!addingOverride) setAddOverrideOpen(false) }}
        title={tc.addOverride}
        width="400px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.overrideProjectId}>
            <input
              type="text"
              value={overrideProjectId}
              onChange={(e) => { setOverrideProjectId(e.target.value); setOverrideError('') }}
              placeholder="project uuid"
              className={inputCls}
              autoFocus
            />
          </FormField>

          <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={overrideEnabled}
              onChange={(e) => setOverrideEnabled(e.target.checked)}
              className="accent-[var(--c-status-success-text)]"
            />
            {tc.overrideEnabled}
          </label>

          {overrideError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{overrideError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={() => setAddOverrideOpen(false)}
              disabled={addingOverride}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleAddOverride}
              disabled={addingOverride}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {addingOverride ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>

      {/* Delete Flag Confirm */}
      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => { if (!deleting) setDeleteTarget(null) }}
        onConfirm={handleDelete}
        title={tc.deleteTitle}
        message={tc.deleteMessage(deleteTarget?.key ?? '')}
        confirmLabel={tc.deleteConfirm}
        loading={deleting}
      />

      {/* Delete Override Confirm */}
      <ConfirmDialog
        open={deleteOverrideTarget !== null}
        onClose={() => { if (!deletingOverride) setDeleteOverrideTarget(null) }}
        onConfirm={handleDeleteOverride}
        title={tc.deleteOverrideTitle}
        message={tc.deleteOverrideMessage(deleteOverrideTarget?.account_id ?? '')}
        confirmLabel={tc.deleteOverrideConfirm}
        loading={deletingOverride}
      />
    </div>
  )
}
