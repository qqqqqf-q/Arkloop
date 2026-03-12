import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Wrench, Pencil, Trash2, CheckCircle2, Ban } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { isApiError } from '../../api'
import {
  listToolProviders,
  activateToolProvider,
  deactivateToolProvider,
  updateToolProviderCredential,
  clearToolProviderCredential,
  type ToolProviderScope,
  type ToolProviderGroup,
  type ToolProviderItem,
} from '../../api/tool-providers'

type EditTarget = {
  group: string
  provider: ToolProviderItem
}

function displayOrDash(value?: string): string {
  const cleaned = (value ?? '').trim()
  return cleaned ? cleaned : '--'
}

export function ToolProvidersPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.toolProviders

  const [scope, setScope] = useState<ToolProviderScope>('platform')
  const [groups, setGroups] = useState<ToolProviderGroup[]>([])
  const [loading, setLoading] = useState(false)
  const [mutating, setMutating] = useState(false)

  const [editTarget, setEditTarget] = useState<EditTarget | null>(null)
  const [apiKey, setApiKey] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [editError, setEditError] = useState('')
  const [saving, setSaving] = useState(false)

  const [clearTarget, setClearTarget] = useState<EditTarget | null>(null)
  const [clearing, setClearing] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await listToolProviders(accessToken, scope)
      setGroups(resp.groups)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, scope, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void fetchAll()
  }, [fetchAll])

  const openEdit = useCallback((group: ToolProviderGroup, provider: ToolProviderItem) => {
    setEditTarget({ group: group.group_name, provider })
    setApiKey('')
    setBaseURL(provider.base_url ?? '')
    setEditError('')
  }, [])

  const closeEdit = useCallback(() => {
    if (saving) return
    setEditTarget(null)
  }, [saving])

  const handleActivate = useCallback(async (groupName: string, providerName: string) => {
    if (mutating) return
    setMutating(true)
    try {
      await activateToolProvider(groupName, providerName, accessToken, scope)
      addToast(tc.toastUpdated, 'success')
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [mutating, accessToken, scope, fetchAll, addToast, tc])

  const handleDeactivate = useCallback(async (groupName: string, providerName: string) => {
    if (mutating) return
    setMutating(true)
    try {
      await deactivateToolProvider(groupName, providerName, accessToken, scope)
      addToast(tc.toastUpdated, 'success')
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [mutating, accessToken, scope, fetchAll, addToast, tc])

  const handleSave = useCallback(async () => {
    if (!editTarget) return

    const trimmedKey = apiKey.trim()
    const trimmedBase = baseURL.trim()

    if (editTarget.provider.requires_api_key && !trimmedKey && !editTarget.provider.key_prefix) {
      setEditError(tc.errApiKeyRequired)
      return
    }
    if (editTarget.provider.requires_base_url && !trimmedBase) {
      setEditError(tc.errBaseUrlRequired)
      return
    }

    const payload: Record<string, string> = {}
    if (trimmedKey) payload.api_key = trimmedKey
    if (trimmedBase) payload.base_url = trimmedBase

    if (Object.keys(payload).length === 0) {
      setEditTarget(null)
      return
    }

    setSaving(true)
    setEditError('')
    try {
      await updateToolProviderCredential(editTarget.group, editTarget.provider.provider_name, payload, accessToken, scope)
      addToast(tc.toastUpdated, 'success')
      setEditTarget(null)
      await fetchAll()
    } catch (err) {
      setEditError(isApiError(err) ? err.message : tc.toastUpdateFailed)
    } finally {
      setSaving(false)
    }
  }, [editTarget, apiKey, baseURL, accessToken, scope, fetchAll, addToast, tc])

  const handleClear = useCallback(async () => {
    if (!clearTarget) return
    setClearing(true)
    try {
      await clearToolProviderCredential(clearTarget.group, clearTarget.provider.provider_name, accessToken, scope)
      addToast(tc.toastUpdated, 'success')
      setClearTarget(null)
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setClearing(false)
    }
  }, [clearTarget, accessToken, scope, fetchAll, addToast, tc])

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  const columns: Column<ToolProviderItem>[] = [
    {
      key: 'provider_name',
      header: tc.colProvider,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-primary)]">{row.provider_name}</span>
      ),
    },
    {
      key: 'status',
      header: tc.colStatus,
      render: (row) => (
        <div className="flex items-center gap-2">
          {row.is_active ? (
            <Badge variant="success">{tc.statusActive}</Badge>
          ) : (
            <Badge variant="neutral">{tc.statusInactive}</Badge>
          )}
          {row.is_active && !row.configured && (
            <Badge variant="warning">{tc.statusUnconfigured}</Badge>
          )}
        </div>
      ),
    },
    {
      key: 'key_prefix',
      header: tc.colKeyPrefix,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-secondary)]">
          {displayOrDash(row.key_prefix)}
        </span>
      ),
    },
    {
      key: 'base_url',
      header: tc.colBaseUrl,
      render: (row) => (
        <span className="font-mono text-xs text-[var(--c-text-secondary)]">
          {displayOrDash(row.base_url)}
        </span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => (
        <div className="flex items-center justify-end gap-1.5">
          {!row.is_active ? (
            <button
              disabled={mutating}
              onClick={() => handleActivate(row.group_name, row.provider_name)}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-success-text)] disabled:cursor-not-allowed disabled:opacity-40"
              title={tc.activate}
            >
              <CheckCircle2 size={14} />
            </button>
          ) : (
            <button
              disabled={mutating}
              onClick={() => handleDeactivate(row.group_name, row.provider_name)}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:cursor-not-allowed disabled:opacity-40"
              title={tc.deactivate}
            >
              <Ban size={14} />
            </button>
          )}

          <button
            disabled={mutating}
            onClick={() => {
              const groupObj = groups.find((g) => g.group_name === row.group_name)
              if (!groupObj) return
              openEdit(groupObj, row)
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:cursor-not-allowed disabled:opacity-40"
            title={tc.configure}
          >
            <Pencil size={14} />
          </button>

          <button
            disabled={mutating}
            onClick={() => {
              const groupObj = groups.find((g) => g.group_name === row.group_name)
              if (!groupObj) return
              setClearTarget({ group: groupObj.group_name, provider: row })
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)] disabled:cursor-not-allowed disabled:opacity-40"
            title={tc.clearCredential}
          >
            <Trash2 size={14} />
          </button>
        </div>
      ),
    },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={(
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--c-text-muted)]">{tc.fieldScope}</span>
            <select
              value={scope}
              onChange={(e) => setScope(e.target.value as ToolProviderScope)}
              className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] focus:outline-none"
            >
              <option value="platform">platform</option>
              <option value="project">account</option>
            </select>
          </div>
        )}
      />

      <div className="flex flex-1 flex-col gap-5 overflow-auto p-4">
        {groups.map((g) => (
          <div key={g.group_name} className="rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-card)]">
            <div className="flex items-center justify-between border-b border-[var(--c-border)] px-4 py-3">
              <div className="flex items-center gap-2">
                <Wrench size={16} className="text-[var(--c-text-muted)]" />
                <span className="text-sm font-semibold text-[var(--c-text-primary)]">{g.group_name}</span>
              </div>
            </div>
            <DataTable
              columns={columns}
              data={g.providers}
              rowKey={(row) => row.provider_name}
              loading={loading}
              emptyMessage="--"
            />
          </div>
        ))}
      </div>

      <Modal
        open={!!editTarget}
        onClose={closeEdit}
        title={editTarget ? `${tc.modalTitle}: ${editTarget.provider.provider_name}` : tc.modalTitle}
      >
        {editTarget && (
          <div className="flex flex-col gap-4">
            {editTarget.provider.requires_api_key && (
              <div className="flex flex-col gap-2">
                <FormField label={tc.fieldApiKey} error={editError}>
                  <input
                    type="password"
                    value={apiKey}
                    onChange={(e) => {
                      setApiKey(e.target.value)
                      setEditError('')
                    }}
                    className={inputCls}
                    placeholder={editTarget.provider.key_prefix ? editTarget.provider.key_prefix : ''}
                  />
                </FormField>
                {editTarget.provider.key_prefix && (
                  <p className="text-xs text-[var(--c-text-muted)]">
                    {tc.currentKeyPrefix}: {editTarget.provider.key_prefix}
                  </p>
                )}
              </div>
            )}

            {editTarget.provider.requires_base_url && (
              <FormField label={tc.fieldBaseUrl} error={editError}>
                <input
                  type="text"
                  value={baseURL}
                  onChange={(e) => {
                    setBaseURL(e.target.value)
                    setEditError('')
                  }}
                  className={inputCls}
                />
              </FormField>
            )}

            {!editTarget.provider.requires_base_url && (
              <FormField label={tc.fieldBaseUrlOptional} error={editError}>
                <input
                  type="text"
                  value={baseURL}
                  onChange={(e) => {
                    setBaseURL(e.target.value)
                    setEditError('')
                  }}
                  className={inputCls}
                  placeholder={editTarget.provider.base_url ?? ''}
                />
              </FormField>
            )}

            {editError && (
              <p className="text-xs text-[var(--c-status-error-text)]">{editError}</p>
            )}

            <div className="mt-2 flex justify-end gap-2">
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
                className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {saving ? '...' : tc.save}
              </button>
            </div>
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={!!clearTarget}
        onClose={() => setClearTarget(null)}
        onConfirm={handleClear}
        title={tc.clearTitle}
        message={clearTarget ? tc.clearMessage(clearTarget.provider.provider_name) : ''}
        confirmLabel={tc.clearConfirm}
        loading={clearing}
      />
    </div>
  )
}
