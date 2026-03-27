import { useCallback, useEffect, useMemo, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { CheckCircle2, Pencil, Plus, RefreshCw, Server, ToggleLeft, ToggleRight, Trash2, Upload } from 'lucide-react'
import { useToast } from '@arkloop/shared'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Badge } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  checkMCPInstall,
  createMCPInstall,
  deleteMCPInstall,
  listMCPDiscoverySources,
  listMCPInstalls,
  setWorkspaceMCPEnablement,
  type CreateMCPInstallRequest,
  type MCPDiscoverySource,
  type MCPDiscoverySourceSpec,
  type MCPInstall,
  updateMCPInstall,
} from '../../api/mcp-installs'
import { notifyToolCatalogChanged } from '../../lib/toolCatalogRefresh'

type Transport = 'stdio' | 'http_sse' | 'streamable_http'
type HostRequirement = 'desktop_local' | 'desktop_sidecar' | 'cloud_worker' | 'remote_http'

type FormState = {
  displayName: string
  transport: Transport
  hostRequirement: HostRequirement
  url: string
  command: string
  args: string
  cwd: string
  envJson: string
  headersJson: string
  bearerToken: string
  timeoutMs: string
  sourceKind: string
  sourceUri: string
  syncMode: string
}

type DeleteTarget = {
  id: string
  name: string
}

function emptyForm(): FormState {
  return {
    displayName: '',
    transport: 'http_sse',
    hostRequirement: 'remote_http',
    url: '',
    command: '',
    args: '',
    cwd: '',
    envJson: '{}',
    headersJson: '{}',
    bearerToken: '',
    timeoutMs: '',
    sourceKind: 'manual_console',
    sourceUri: '',
    syncMode: 'none',
  }
}

function formatJson(value: unknown, fallback = '{}') {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return fallback
  }
  return JSON.stringify(value, null, 2)
}

function formFromInstall(install: MCPInstall): FormState {
  const launch = install.launch_spec ?? {}
  const headers = launch.headers
  return {
    displayName: install.display_name,
    transport: install.transport,
    hostRequirement: normalizeHostRequirement(install.transport, install.host_requirement),
    url: asString(launch.url),
    command: asString(launch.command),
    args: Array.isArray(launch.args) ? launch.args.map((value) => String(value)).join(', ') : '',
    cwd: asString(launch.cwd),
    envJson: formatJson(launch.env),
    headersJson: formatJson(headers),
    bearerToken: asBearerToken(headers),
    timeoutMs: typeof launch.call_timeout_ms === 'number' ? String(launch.call_timeout_ms) : '',
    sourceKind: install.source_kind,
    sourceUri: install.source_uri ?? '',
    syncMode: install.sync_mode,
  }
}

function normalizeHostRequirement(transport: Transport, value?: string): HostRequirement {
  const cleaned = (value ?? '').trim()
  if (cleaned === 'desktop_local' || cleaned === 'desktop_sidecar' || cleaned === 'cloud_worker' || cleaned === 'remote_http') {
    return cleaned
  }
  return transport === 'stdio' ? 'cloud_worker' : 'remote_http'
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function asBearerToken(headers: unknown): string {
  if (!headers || typeof headers !== 'object' || Array.isArray(headers)) {
    return ''
  }
  const authorization = (headers as Record<string, unknown>).Authorization
  if (typeof authorization !== 'string') {
    return ''
  }
  return authorization.startsWith('Bearer ') ? authorization.slice(7) : ''
}

function parseJsonMap(raw: string, fieldLabel: string): Record<string, string> {
  const trimmed = raw.trim()
  if (!trimmed) {
    return {}
  }
  const parsed = JSON.parse(trimmed)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(fieldLabel)
  }
  const output: Record<string, string> = {}
  for (const [key, value] of Object.entries(parsed)) {
    if (typeof value !== 'string') {
      throw new Error(fieldLabel)
    }
    if (key.trim()) {
      output[key.trim()] = value
    }
  }
  return output
}

function buildRequest(form: FormState): CreateMCPInstallRequest {
  const displayName = form.displayName.trim()
  if (!displayName) {
    throw new Error('display_name')
  }
  const launchSpec: Record<string, unknown> = {}
  if (form.transport === 'stdio') {
    if (!form.command.trim()) {
      throw new Error('command')
    }
    launchSpec.command = form.command.trim()
    const args = form.args
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
    if (args.length > 0) {
      launchSpec.args = args
    }
    if (form.cwd.trim()) {
      launchSpec.cwd = form.cwd.trim()
    }
    const env = parseJsonMap(form.envJson, 'env_json')
    if (Object.keys(env).length > 0) {
      launchSpec.env = env
    }
  } else {
    if (!form.url.trim()) {
      throw new Error('url')
    }
    launchSpec.url = form.url.trim()
  }
  if (form.timeoutMs.trim()) {
    const timeout = Number.parseInt(form.timeoutMs.trim(), 10)
    if (!Number.isFinite(timeout) || timeout <= 0) {
      throw new Error('timeout')
    }
    launchSpec.call_timeout_ms = timeout
  }
  const headers = parseJsonMap(form.headersJson, 'headers_json')
  const bearerToken = form.bearerToken.trim()
  if (bearerToken) {
    headers.Authorization = `Bearer ${bearerToken}`
  }
  return {
    display_name: displayName,
    source_kind: form.sourceKind.trim() || 'manual_console',
    source_uri: form.sourceUri.trim() || undefined,
    sync_mode: form.syncMode.trim() || 'none',
    transport: form.transport,
    launch_spec: launchSpec,
    auth_headers: Object.keys(headers).length > 0 ? headers : undefined,
    bearer_token: bearerToken || undefined,
    host_requirement: form.hostRequirement,
  }
}

function splitAuthFromLaunchSpec(launchSpec: Record<string, unknown>) {
  const next = { ...launchSpec }
  let bearerToken = ''
  const authHeaders: Record<string, string> = {}
  if (next.headers && typeof next.headers === 'object' && !Array.isArray(next.headers)) {
    for (const [key, value] of Object.entries(next.headers as Record<string, unknown>)) {
      if (typeof value === 'string' && key.trim()) {
        authHeaders[key.trim()] = value
      }
    }
    delete next.headers
  }
  const authorization = authHeaders.Authorization
  if (typeof authorization === 'string' && authorization.startsWith('Bearer ')) {
    bearerToken = authorization.slice(7)
    delete authHeaders.Authorization
  }
  if (typeof next.bearer_token === 'string' && next.bearer_token.trim()) {
    bearerToken = next.bearer_token.trim()
    delete next.bearer_token
  }
  return { launchSpec: next, authHeaders, bearerToken }
}

function statusVariant(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  switch (status) {
    case 'ready':
      return 'success'
    case 'needs_check':
    case 'configured':
      return 'warning'
    case 'install_missing':
    case 'auth_invalid':
    case 'connect_failed':
    case 'discovered_empty':
    case 'protocol_error':
      return 'error'
    default:
      return 'neutral'
  }
}

export function MCPConfigsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.mcpConfigs

  const [installs, setInstalls] = useState<MCPInstall[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<MCPInstall | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [checkingID, setCheckingID] = useState<string | null>(null)
  const [toggleID, setToggleID] = useState<string | null>(null)
  const [scanOpen, setScanOpen] = useState(false)
  const [scanPath, setScanPath] = useState('')
  const [scanLoading, setScanLoading] = useState(false)
  const [scanItems, setScanItems] = useState<MCPDiscoverySource[]>([])

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setInstalls(await listMCPInstalls(accessToken))
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const setField = useCallback(<K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }))
    setFormError('')
  }, [])

  const openCreate = useCallback(() => {
    setForm(emptyForm())
    setFormError('')
    setCreateOpen(true)
  }, [])

  const openEdit = useCallback((install: MCPInstall) => {
    setEditTarget(install)
    setForm(formFromInstall(install))
    setFormError('')
  }, [])

  const closeForm = useCallback(() => {
    if (saving) {
      return
    }
    setCreateOpen(false)
    setEditTarget(null)
    setFormError('')
  }, [saving])

  const handleSave = useCallback(async () => {
    setSaving(true)
    setFormError('')
    try {
      const req = buildRequest(form)
      if (editTarget) {
        await updateMCPInstall(editTarget.id, req, accessToken)
        addToast(tc.toastUpdated, 'success')
      } else {
        await createMCPInstall(req, accessToken)
        addToast(tc.toastCreated, 'success')
      }
      notifyToolCatalogChanged()
      setCreateOpen(false)
      setEditTarget(null)
      await refresh()
    } catch (error) {
      if (isApiError(error)) {
        setFormError(error.message)
      } else if (error instanceof Error) {
        setFormError(
          error.message === 'display_name'
            ? tc.errRequired
            : error.message === 'url'
              ? tc.errUrlRequired
              : error.message === 'command'
                ? tc.errCommandRequired
                : error.message === 'timeout'
                  ? tc.errTimeoutInvalid
                  : error.message === 'env_json'
                    ? tc.errEnvJsonInvalid
                    : error.message === 'headers_json'
                      ? tc.errHeadersInvalid
                      : tc.toastSaveFailed,
        )
      } else {
        setFormError(tc.toastSaveFailed)
      }
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, editTarget, form, refresh, tc])

  const handleCheck = useCallback(async (install: MCPInstall) => {
    setCheckingID(install.id)
    try {
      await checkMCPInstall(install.id, accessToken)
      notifyToolCatalogChanged()
      await refresh()
      addToast(tc.toastChecked, 'success')
    } catch {
      addToast(tc.toastCheckFailed, 'error')
    } finally {
      setCheckingID(null)
    }
  }, [accessToken, addToast, refresh, tc.toastChecked, tc.toastCheckFailed])

  const handleToggle = useCallback(async (install: MCPInstall) => {
    setToggleID(install.id)
    try {
      await setWorkspaceMCPEnablement({
        workspace_ref: install.workspace_state?.workspace_ref,
        install_key: install.install_key,
        enabled: !install.workspace_state?.enabled,
      }, accessToken)
      notifyToolCatalogChanged()
      await refresh()
    } catch {
      addToast(tc.toastToggleFailed, 'error')
    } finally {
      setToggleID(null)
    }
  }, [accessToken, addToast, refresh, tc.toastToggleFailed])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) {
      return
    }
    setSaving(true)
    try {
      await deleteMCPInstall(deleteTarget.id, accessToken)
      notifyToolCatalogChanged()
      setDeleteTarget(null)
      await refresh()
      addToast(tc.toastDeleted, 'success')
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, deleteTarget, refresh, tc.toastDeleteFailed, tc.toastDeleted])

  const handleScan = useCallback(async () => {
    setScanLoading(true)
    try {
      const items = await listMCPDiscoverySources(accessToken, {
        paths: scanPath.trim() ? [scanPath.trim()] : [],
      })
      setScanItems(items)
    } catch {
      addToast(tc.toastDiscoveryFailed, 'error')
    } finally {
      setScanLoading(false)
    }
  }, [accessToken, addToast, scanPath, tc.toastDiscoveryFailed])

  const handleImport = useCallback(async (source: MCPDiscoverySource, install: MCPDiscoverySourceSpec) => {
    setSaving(true)
    try {
      const split = splitAuthFromLaunchSpec(install.launch_spec)
      await createMCPInstall({
        display_name: install.display_name,
        install_key: install.install_key,
        source_kind: source.source_kind,
        source_uri: source.source_uri,
        sync_mode: source.source_kind === 'desktop_file' ? 'desktop_file_bidirectional' : 'none',
        transport: install.transport,
        launch_spec: split.launchSpec,
        auth_headers: Object.keys(split.authHeaders).length > 0 ? split.authHeaders : undefined,
        bearer_token: split.bearerToken || undefined,
        host_requirement: install.host_requirement,
      }, accessToken)
      notifyToolCatalogChanged()
      await refresh()
      addToast(tc.toastImported, 'success')
    } catch {
      addToast(tc.toastImportFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, refresh, tc.toastImportFailed, tc.toastImported])

  const columns = useMemo<Column<MCPInstall>[]>(() => [
    {
      key: 'display_name',
      header: tc.colName,
      render: (row) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium text-[var(--c-text-primary)]">{row.display_name}</span>
          <span className="text-xs text-[var(--c-text-muted)]">{row.install_key}</span>
        </div>
      ),
    },
    {
      key: 'transport',
      header: tc.colTransport,
      render: (row) => <Badge variant="neutral">{row.transport}</Badge>,
    },
    {
      key: 'discovery_status',
      header: tc.colStatus,
      render: (row) => <Badge variant={statusVariant(row.discovery_status)}>{row.discovery_status}</Badge>,
    },
    {
      key: 'enabled',
      header: tc.colWorkspace,
      render: (row) => row.workspace_state?.enabled ? <Badge variant="success">{tc.enabledLabel}</Badge> : <Badge variant="neutral">{tc.disabledLabel}</Badge>,
    },
    {
      key: 'source_kind',
      header: tc.colSource,
      render: (row) => <Badge variant="warning">{row.source_kind}</Badge>,
    },
    {
      key: 'updated_at',
      header: tc.colCreatedAt,
      render: (row) => <span className="tabular-nums text-xs">{new Date(row.updated_at).toLocaleString()}</span>,
    },
    {
      key: 'actions',
      header: '',
      render: (row) => (
        <div className="flex items-center gap-1">
          <button
            onClick={(event) => {
              event.stopPropagation()
              void handleToggle(row)
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            title={row.workspace_state?.enabled ? tc.actionDisable : tc.actionEnable}
          >
            {row.workspace_state?.enabled ? <ToggleRight size={15} /> : <ToggleLeft size={15} />}
          </button>
          <button
            onClick={(event) => {
              event.stopPropagation()
              void handleCheck(row)
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            title={tc.actionCheck}
          >
            <RefreshCw size={14} className={checkingID === row.id ? 'animate-spin' : ''} />
          </button>
          <button
            onClick={(event) => {
              event.stopPropagation()
              openEdit(row)
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            title={tc.modalTitleEdit}
          >
            <Pencil size={13} />
          </button>
          <button
            onClick={(event) => {
              event.stopPropagation()
              setDeleteTarget({ id: row.id, name: row.display_name })
            }}
            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
            title={tc.deleteConfirm}
          >
            <Trash2 size={13} />
          </button>
        </div>
      ),
    },
  ], [checkingID, handleCheck, handleToggle, openEdit, tc])

  const inputCls = 'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'
  const textareaCls = `${inputCls} min-h-[92px] font-mono text-xs`
  const isFormOpen = createOpen || editTarget !== null
  const isHTTP = form.transport !== 'stdio'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={(
          <div className="flex items-center gap-2">
            <button
              onClick={() => setScanOpen(true)}
              className="flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <Upload size={13} />
              {tc.discoveryTitle}
            </button>
            <button
              onClick={openCreate}
              className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <Plus size={13} />
              {tc.addConfig}
            </button>
          </div>
        )}
      />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={installs}
          rowKey={(row) => row.id}
          loading={loading || toggleID !== null}
          emptyMessage={tc.empty}
          emptyIcon={<Server size={28} />}
        />
      </div>

      <Modal
        open={isFormOpen}
        onClose={closeForm}
        title={editTarget ? tc.modalTitleEdit : tc.modalTitleCreate}
        width="720px"
      >
        <div className="grid grid-cols-2 gap-4">
          <FormField label={tc.fieldName}>
            <input className={inputCls} value={form.displayName} onChange={(event) => setField('displayName', event.target.value)} placeholder="Context7" />
          </FormField>
          <FormField label={tc.fieldTransport}>
            <select
              className={inputCls}
              value={form.transport}
              onChange={(event) => {
                const transport = event.target.value as Transport
                setField('transport', transport)
                setField('hostRequirement', normalizeHostRequirement(transport))
              }}
            >
              <option value="http_sse">http_sse</option>
              <option value="streamable_http">streamable_http</option>
              <option value="stdio">stdio</option>
            </select>
          </FormField>
          <FormField label={tc.fieldHostRequirement}>
            <select className={inputCls} value={form.hostRequirement} onChange={(event) => setField('hostRequirement', event.target.value as HostRequirement)}>
              <option value="remote_http">remote_http</option>
              <option value="cloud_worker">cloud_worker</option>
              <option value="desktop_local">desktop_local</option>
              <option value="desktop_sidecar">desktop_sidecar</option>
            </select>
          </FormField>
          <FormField label={tc.fieldTimeout}>
            <input className={inputCls} value={form.timeoutMs} onChange={(event) => setField('timeoutMs', event.target.value)} placeholder="10000" />
          </FormField>

          {isHTTP ? (
            <FormField label={tc.fieldUrl}>
              <input className={inputCls} value={form.url} onChange={(event) => setField('url', event.target.value)} placeholder="https://example.com/mcp" />
            </FormField>
          ) : (
            <FormField label={tc.fieldCommand}>
              <input className={inputCls} value={form.command} onChange={(event) => setField('command', event.target.value)} placeholder="npx" />
            </FormField>
          )}
          {!isHTTP ? (
            <FormField label={tc.fieldArgs}>
              <input className={inputCls} value={form.args} onChange={(event) => setField('args', event.target.value)} placeholder="-y, @modelcontextprotocol/server-filesystem, ." />
            </FormField>
          ) : (
            <FormField label={tc.fieldBearerToken}>
              <input className={inputCls} type="password" value={form.bearerToken} onChange={(event) => setField('bearerToken', event.target.value)} placeholder={tc.fieldBearerTokenPlaceholder} />
            </FormField>
          )}
          {!isHTTP && (
            <>
              <FormField label={tc.fieldCwd}>
                <input className={inputCls} value={form.cwd} onChange={(event) => setField('cwd', event.target.value)} placeholder="/workspace" />
              </FormField>
              <FormField label={tc.fieldEnvJson}>
                <textarea className={textareaCls} value={form.envJson} onChange={(event) => setField('envJson', event.target.value)} />
              </FormField>
            </>
          )}
          <FormField label={tc.fieldHeaders}>
            <textarea className={textareaCls} value={form.headersJson} onChange={(event) => setField('headersJson', event.target.value)} />
          </FormField>
          <FormField label={tc.fieldSourceKind}>
            <input className={inputCls} value={form.sourceKind} onChange={(event) => setField('sourceKind', event.target.value)} />
          </FormField>
          <FormField label={tc.fieldSourceUri}>
            <input className={inputCls} value={form.sourceUri} onChange={(event) => setField('sourceUri', event.target.value)} />
          </FormField>
          <FormField label={tc.fieldSyncMode}>
            <select className={inputCls} value={form.syncMode} onChange={(event) => setField('syncMode', event.target.value)}>
              <option value="none">none</option>
              <option value="desktop_file_bidirectional">desktop_file_bidirectional</option>
            </select>
          </FormField>
        </div>
        {formError && <p className="mt-3 text-xs text-[var(--c-status-error-text)]">{formError}</p>}
        {editTarget?.last_error_message && (
          <p className="mt-2 text-xs text-[var(--c-text-muted)]">{editTarget.last_error_message}</p>
        )}
        <div className="mt-4 flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
          <button onClick={closeForm} disabled={saving} className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50">
            {tc.cancel}
          </button>
          <button onClick={() => void handleSave()} disabled={saving} className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50">
            {saving ? '...' : editTarget ? tc.save : tc.create}
          </button>
        </div>
      </Modal>

      <Modal open={scanOpen} onClose={() => setScanOpen(false)} title={tc.discoveryTitle} width="760px">
        <div className="flex items-end gap-3">
          <FormField label={tc.discoveryPath}>
            <input className={inputCls} value={scanPath} onChange={(event) => setScanPath(event.target.value)} placeholder={tc.discoveryPathPlaceholder} />
          </FormField>
          <button onClick={() => void handleScan()} disabled={scanLoading} className="mb-0.5 rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50">
            {scanLoading ? tc.discoveryScanning : tc.discoveryScan}
          </button>
        </div>
        <div className="mt-4 flex max-h-[420px] flex-col gap-3 overflow-auto">
          {scanItems.length === 0 ? (
            <div className="rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] px-4 py-6 text-sm text-[var(--c-text-muted)]">
              {tc.discoveryEmpty}
            </div>
          ) : scanItems.map((source) => (
            <div key={`${source.source_kind}:${source.source_uri}`} className="rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-sub)] p-4">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Badge variant={source.installable ? 'success' : 'warning'}>{source.installable ? tc.discoveryInstallable : tc.discoveryNeedsAttention}</Badge>
                    <span className="truncate text-sm font-medium text-[var(--c-text-primary)]">{source.source_uri}</span>
                  </div>
                  {source.validation_errors.length > 0 && (
                    <p className="mt-2 text-xs text-[var(--c-status-error-text)]">{source.validation_errors.join(' | ')}</p>
                  )}
                  {source.host_warnings.length > 0 && (
                    <p className="mt-2 text-xs text-[var(--c-text-muted)]">{source.host_warnings.join(' | ')}</p>
                  )}
                </div>
              </div>
              <div className="mt-3 flex flex-col gap-2">
                {source.proposed_installs.map((install) => (
                  <div key={install.install_key} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-page)] px-3 py-2">
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-[var(--c-text-primary)]">{install.display_name}</div>
                      <div className="text-xs text-[var(--c-text-muted)]">{install.transport} · {install.host_requirement}</div>
                    </div>
                    <button
                      onClick={() => void handleImport(source, install)}
                      disabled={!source.installable || saving}
                      className="flex shrink-0 items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                    >
                      <CheckCircle2 size={13} />
                      {tc.discoveryImport}
                    </button>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </Modal>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
        title={tc.deleteTitle}
        message={tc.deleteMessage(deleteTarget?.name ?? '')}
        confirmLabel={tc.deleteConfirm}
        loading={saving}
      />
    </div>
  )
}
