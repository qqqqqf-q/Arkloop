import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  checkMCPInstall,
  createMCPInstall,
  deleteMCPInstall,
  importMCPInstall,
  listMCPDiscoverySources,
  listMCPInstalls,
  setWorkspaceMCPEnablement,
  updateMCPInstall,
  type MCPDiscoveryProposal,
  type MCPDiscoverySource,
  type MCPInstall,
  type UpsertMCPInstallRequest,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'

type Props = {
  accessToken: string
}

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
  }
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function formatJson(value: unknown, fallback = '{}'): string {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return fallback
  }
  return JSON.stringify(value, null, 2)
}

function normalizeHostRequirement(transport: Transport, value?: string): HostRequirement {
  const cleaned = (value ?? '').trim()
  if (cleaned === 'desktop_local' || cleaned === 'desktop_sidecar' || cleaned === 'cloud_worker' || cleaned === 'remote_http') {
    return cleaned
  }
  return transport === 'stdio' ? 'cloud_worker' : 'remote_http'
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

function formFromInstall(install: MCPInstall): FormState {
  const launch = install.launch_spec ?? {}
  return {
    displayName: install.display_name,
    transport: install.transport,
    hostRequirement: normalizeHostRequirement(install.transport, install.host_requirement),
    url: asString(launch.url),
    command: asString(launch.command),
    args: Array.isArray(launch.args) ? launch.args.map((value) => String(value)).join(', ') : '',
    cwd: asString(launch.cwd),
    envJson: formatJson(launch.env),
    headersJson: formatJson(launch.headers),
    bearerToken: asBearerToken(launch.headers),
    timeoutMs: typeof launch.call_timeout_ms === 'number' ? String(launch.call_timeout_ms) : '',
  }
}

function parseJsonMap(raw: string, fieldKey: string): Record<string, string> {
  const trimmed = raw.trim()
  if (!trimmed) {
    return {}
  }
  const parsed = JSON.parse(trimmed)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(fieldKey)
  }
  const output: Record<string, string> = {}
  for (const [key, value] of Object.entries(parsed)) {
    if (typeof value !== 'string') {
      throw new Error(fieldKey)
    }
    if (key.trim()) {
      output[key.trim()] = value
    }
  }
  return output
}

function buildRequest(form: FormState): UpsertMCPInstallRequest {
  const displayName = form.displayName.trim()
  if (!displayName) {
    throw new Error('displayName')
  }
  const launchSpec: Record<string, unknown> = {}
  if (form.transport === 'stdio') {
    if (!form.command.trim()) {
      throw new Error('command')
    }
    launchSpec.command = form.command.trim()
    const args = form.args.split(',').map((item) => item.trim()).filter(Boolean)
    if (args.length > 0) {
      launchSpec.args = args
    }
    if (form.cwd.trim()) {
      launchSpec.cwd = form.cwd.trim()
    }
    const env = parseJsonMap(form.envJson, 'envJson')
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
  const headers = parseJsonMap(form.headersJson, 'headersJson')
  const bearerToken = form.bearerToken.trim()
  if (bearerToken) {
    headers.Authorization = `Bearer ${bearerToken}`
  }
  return {
    display_name: displayName,
    transport: form.transport,
    launch_spec: launchSpec,
    auth_headers: Object.keys(headers).length > 0 ? headers : undefined,
    bearer_token: bearerToken || undefined,
    host_requirement: form.hostRequirement,
  }
}

function statusTone(status: string): string {
  switch (status) {
    case 'ready':
      return 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300'
    case 'needs_check':
    case 'configured':
      return 'bg-amber-500/12 text-amber-700 dark:text-amber-300'
    case 'install_missing':
    case 'auth_invalid':
    case 'connect_failed':
    case 'discovered_empty':
    case 'protocol_error':
      return 'bg-rose-500/12 text-rose-700 dark:text-rose-300'
    default:
      return 'bg-[var(--c-bg-deep)] text-[var(--c-text-secondary)]'
  }
}

export function MCPSettings({ accessToken }: Props) {
  const { t, locale } = useLocale()
  const ds = t.desktopSettings
  const copy = useMemo(() => {
    if (locale === 'zh') {
      return {
        add: '添加服务器',
        refresh: '刷新',
        scan: '扫描导入',
        create: '创建',
        save: '保存',
        cancel: '取消',
        delete: '删除',
        recheck: '重检',
        enable: '启用',
        disable: '禁用',
        import: '导入',
        scanning: '扫描中...',
        saving: '保存中...',
        loading: '加载中...',
        empty: '还没有 MCP 安装项。',
        sourceEmpty: '扫描结果会显示在这里。',
        formTitleCreate: '新建 MCP 服务器',
        formTitleEdit: '编辑 MCP 服务器',
        scanTitle: '从文件导入',
        fieldName: '名称',
        fieldTransport: '传输类型',
        fieldHost: '宿主要求',
        fieldURL: 'URL',
        fieldCommand: '命令',
        fieldArgs: '参数（逗号分隔）',
        fieldCwd: '工作目录',
        fieldEnv: '环境变量 JSON',
        fieldHeaders: '请求头 JSON',
        fieldToken: 'Bearer Token',
        fieldTimeout: '超时（毫秒）',
        fieldSourceKind: '来源类型',
        fieldSourceURI: '来源路径',
        fieldSyncMode: '同步模式',
        fieldFilePath: '外部文件路径',
        placeholderFilePath: '/path/to/.mcp.json',
        errorName: '名称不能为空。',
        errorURL: 'HTTP 传输必须填写 URL。',
        errorCommand: 'stdio 传输必须填写命令。',
        errorTimeout: '超时必须是正整数。',
        errorEnv: '环境变量 JSON 无效。',
        errorHeaders: '请求头 JSON 无效。',
        toastLoadFailed: '加载 MCP 服务器失败。',
        toastSaveFailed: '保存 MCP 服务器失败。',
        toastDeleteFailed: '删除 MCP 服务器失败。',
        toastCheckFailed: '检查 MCP 服务器失败。',
        toastToggleFailed: '切换工作区启用状态失败。',
        toastScanFailed: '扫描 MCP 文件失败。',
        toastImportFailed: '导入 MCP 服务器失败。',
        toastSaved: '已保存。',
        toastDeleted: '已删除。',
        toastChecked: '检查已完成。',
        toastImported: '已导入。',
      }
    }
    return {
      add: 'Add Server',
      refresh: 'Refresh',
      scan: 'Scan & Import',
      create: 'Create',
      save: 'Save',
      cancel: 'Cancel',
      delete: 'Delete',
      recheck: 'Recheck',
      enable: 'Enable',
      disable: 'Disable',
      import: 'Import',
      scanning: 'Scanning...',
      saving: 'Saving...',
      loading: 'Loading...',
      empty: 'No MCP installs yet.',
      sourceEmpty: 'Scan results will appear here.',
      formTitleCreate: 'New MCP Server',
      formTitleEdit: 'Edit MCP Server',
      scanTitle: 'Import From File',
      fieldName: 'Name',
      fieldTransport: 'Transport',
      fieldHost: 'Host Requirement',
      fieldURL: 'URL',
      fieldCommand: 'Command',
      fieldArgs: 'Args (comma-separated)',
      fieldCwd: 'Working Directory',
      fieldEnv: 'Env JSON',
      fieldHeaders: 'Headers JSON',
      fieldToken: 'Bearer Token',
      fieldTimeout: 'Timeout (ms)',
      fieldSourceKind: 'Source Kind',
      fieldSourceURI: 'Source URI',
      fieldSyncMode: 'Sync Mode',
      fieldFilePath: 'External File Path',
      placeholderFilePath: '/path/to/.mcp.json',
      errorName: 'Name is required.',
      errorURL: 'HTTP transport requires a URL.',
      errorCommand: 'stdio transport requires a command.',
      errorTimeout: 'Timeout must be a positive integer.',
      errorEnv: 'Env JSON is invalid.',
      errorHeaders: 'Headers JSON is invalid.',
      toastLoadFailed: 'Failed to load MCP servers.',
      toastSaveFailed: 'Failed to save MCP server.',
      toastDeleteFailed: 'Failed to delete MCP server.',
      toastCheckFailed: 'Failed to check MCP server.',
      toastToggleFailed: 'Failed to update workspace enablement.',
      toastScanFailed: 'Failed to scan MCP files.',
      toastImportFailed: 'Failed to import MCP server.',
      toastSaved: 'Saved.',
      toastDeleted: 'Deleted.',
      toastChecked: 'Check completed.',
      toastImported: 'Imported.',
    }
  }, [locale])

  const [installs, setInstalls] = useState<MCPInstall[]>([])
  const [loading, setLoading] = useState(false)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<MCPInstall | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [formError, setFormError] = useState('')
  const [busyID, setBusyID] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [scanOpen, setScanOpen] = useState(false)
  const [scanPath, setScanPath] = useState('')
  const [scanLoading, setScanLoading] = useState(false)
  const [scanItems, setScanItems] = useState<MCPDiscoverySource[]>([])
  const [notice, setNotice] = useState<string | null>(null)

  const loadInstalls = useCallback(async () => {
    setLoading(true)
    try {
      const items = await listMCPInstalls(accessToken)
      setInstalls(items)
      setNotice(null)
    } catch {
      setNotice(copy.toastLoadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, copy.toastLoadFailed])

  useEffect(() => {
    void loadInstalls()
  }, [loadInstalls])

  const openCreate = useCallback(() => {
    setEditing(null)
    setForm(emptyForm())
    setFormError('')
    setFormOpen(true)
  }, [])

  const openEdit = useCallback((install: MCPInstall) => {
    setEditing(install)
    setForm(formFromInstall(install))
    setFormError('')
    setFormOpen(true)
  }, [])

  const setField = useCallback(<K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }))
    setFormError('')
  }, [])

  const handleSave = useCallback(async () => {
    try {
      const req = buildRequest(form)
      setSaving(true)
      if (editing) {
        await updateMCPInstall(accessToken, editing.id, req)
      } else {
        await createMCPInstall(accessToken, req)
      }
      setNotice(copy.toastSaved)
      setFormOpen(false)
      setForm(emptyForm())
      setEditing(null)
      await loadInstalls()
    } catch (error) {
      if (error instanceof Error) {
        switch (error.message) {
          case 'displayName':
            setFormError(copy.errorName)
            break
          case 'url':
            setFormError(copy.errorURL)
            break
          case 'command':
            setFormError(copy.errorCommand)
            break
          case 'timeout':
            setFormError(copy.errorTimeout)
            break
          case 'envJson':
            setFormError(copy.errorEnv)
            break
          case 'headersJson':
            setFormError(copy.errorHeaders)
            break
          default:
            setFormError(copy.toastSaveFailed)
            break
        }
      } else {
        setFormError(copy.toastSaveFailed)
      }
    } finally {
      setSaving(false)
    }
  }, [accessToken, copy.errorCommand, copy.errorEnv, copy.errorHeaders, copy.errorName, copy.errorTimeout, copy.errorURL, copy.toastSaveFailed, copy.toastSaved, editing, form, loadInstalls])

  const handleDelete = useCallback(async (install: MCPInstall) => {
    if (!window.confirm(`${copy.delete} "${install.display_name}"?`)) {
      return
    }
    setBusyID(install.id)
    try {
      await deleteMCPInstall(accessToken, install.id)
      setNotice(copy.toastDeleted)
      await loadInstalls()
    } catch {
      setNotice(copy.toastDeleteFailed)
    } finally {
      setBusyID(null)
    }
  }, [accessToken, copy.delete, copy.toastDeleteFailed, copy.toastDeleted, loadInstalls])

  const handleToggle = useCallback(async (install: MCPInstall) => {
    setBusyID(install.id)
    try {
      await setWorkspaceMCPEnablement(accessToken, {
        install_id: install.id,
        enabled: !install.workspace_state?.enabled,
      })
      await loadInstalls()
      setNotice(null)
    } catch {
      setNotice(copy.toastToggleFailed)
    } finally {
      setBusyID(null)
    }
  }, [accessToken, copy.toastToggleFailed, loadInstalls])

  const handleCheck = useCallback(async (install: MCPInstall) => {
    setBusyID(install.id)
    try {
      await checkMCPInstall(accessToken, install.id)
      setNotice(copy.toastChecked)
      await loadInstalls()
    } catch {
      setNotice(copy.toastCheckFailed)
    } finally {
      setBusyID(null)
    }
  }, [accessToken, copy.toastCheckFailed, copy.toastChecked, loadInstalls])

  const handleScan = useCallback(async () => {
    setScanLoading(true)
    try {
      const items = await listMCPDiscoverySources(accessToken, {
        paths: scanPath.trim() ? [scanPath.trim()] : [],
      })
      setScanItems(items)
      setNotice(null)
    } catch {
      setNotice(copy.toastScanFailed)
    } finally {
      setScanLoading(false)
    }
  }, [accessToken, copy.toastScanFailed, scanPath])

  const handleImport = useCallback(async (source: MCPDiscoverySource, proposal: MCPDiscoveryProposal) => {
    setSaving(true)
    try {
      await importMCPInstall(accessToken, {
        source_uri: source.source_uri,
        install_key: proposal.install_key,
      })
      setNotice(copy.toastImported)
      await loadInstalls()
    } catch {
      setNotice(copy.toastImportFailed)
    } finally {
      setSaving(false)
    }
  }, [accessToken, copy.toastImportFailed, copy.toastImported, loadInstalls])

  const inputClass = 'w-full rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none transition focus:border-[var(--c-border-mid)]'
  const cardClass = 'rounded-2xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]'

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{ds.mcpTitle}</h3>
          <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{ds.mcpDesc}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)]" onClick={() => void loadInstalls()}>
            {copy.refresh}
          </button>
          <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)]" onClick={() => setScanOpen((current) => !current)}>
            {copy.scan}
          </button>
          <button className="rounded-xl bg-[var(--c-bg-deep)] px-3 py-2 text-sm font-medium text-[var(--c-text-heading)] transition hover:opacity-90" onClick={openCreate}>
            {copy.add}
          </button>
        </div>
      </div>

      {notice ? (
        <div className="rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-3 text-sm text-[var(--c-text-secondary)]">
          {notice}
        </div>
      ) : null}

      {formOpen ? (
        <div className={`${cardClass} p-4`}>
          <div className="mb-4 flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">
              {editing ? copy.formTitleEdit : copy.formTitleCreate}
            </h4>
            <button className="text-sm text-[var(--c-text-secondary)] transition hover:text-[var(--c-text-primary)]" onClick={() => { setFormOpen(false); setEditing(null); setFormError('') }}>
              {copy.cancel}
            </button>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
              {copy.fieldName}
              <input className={inputClass} value={form.displayName} onChange={(event) => setField('displayName', event.target.value)} />
            </label>
            <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
              {copy.fieldTransport}
              <select className={inputClass} value={form.transport} onChange={(event) => {
                const transport = event.target.value as Transport
                setField('transport', transport)
                setField('hostRequirement', normalizeHostRequirement(transport))
              }}>
                <option value="http_sse">http_sse</option>
                <option value="streamable_http">streamable_http</option>
                <option value="stdio">stdio</option>
              </select>
            </label>
            <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
              {copy.fieldHost}
              <select className={inputClass} value={form.hostRequirement} onChange={(event) => setField('hostRequirement', event.target.value as HostRequirement)}>
                <option value="remote_http">remote_http</option>
                <option value="cloud_worker">cloud_worker</option>
                <option value="desktop_local">desktop_local</option>
                <option value="desktop_sidecar">desktop_sidecar</option>
              </select>
            </label>
            <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
              {copy.fieldTimeout}
              <input className={inputClass} value={form.timeoutMs} onChange={(event) => setField('timeoutMs', event.target.value)} />
            </label>
            {form.transport === 'stdio' ? (
              <>
                <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)] md:col-span-2">
                  {copy.fieldCommand}
                  <input className={inputClass} value={form.command} onChange={(event) => setField('command', event.target.value)} />
                </label>
                <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
                  {copy.fieldArgs}
                  <input className={inputClass} value={form.args} onChange={(event) => setField('args', event.target.value)} />
                </label>
                <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
                  {copy.fieldCwd}
                  <input className={inputClass} value={form.cwd} onChange={(event) => setField('cwd', event.target.value)} />
                </label>
                <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)] md:col-span-2">
                  {copy.fieldEnv}
                  <textarea className={`${inputClass} min-h-24`} value={form.envJson} onChange={(event) => setField('envJson', event.target.value)} />
                </label>
              </>
            ) : (
              <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)] md:col-span-2">
                {copy.fieldURL}
                <input className={inputClass} value={form.url} onChange={(event) => setField('url', event.target.value)} />
              </label>
            )}
            <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)] md:col-span-2">
              {copy.fieldHeaders}
              <textarea className={`${inputClass} min-h-24`} value={form.headersJson} onChange={(event) => setField('headersJson', event.target.value)} />
            </label>
            <label className="flex flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
              {copy.fieldToken}
              <input className={inputClass} value={form.bearerToken} onChange={(event) => setField('bearerToken', event.target.value)} />
            </label>
          </div>
          {formError ? <p className="mt-3 text-sm text-rose-600">{formError}</p> : null}
          <div className="mt-4 flex justify-end gap-2">
            <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)]" onClick={() => { setFormOpen(false); setEditing(null); setFormError('') }}>
              {copy.cancel}
            </button>
            <button className="rounded-xl bg-[var(--c-bg-deep)] px-3 py-2 text-sm font-medium text-[var(--c-text-heading)] transition hover:opacity-90 disabled:opacity-60" disabled={saving} onClick={() => void handleSave()}>
              {saving ? copy.saving : editing ? copy.save : copy.create}
            </button>
          </div>
        </div>
      ) : null}

      {scanOpen ? (
        <div className={`${cardClass} p-4`}>
          <div className="mb-4 flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{copy.scanTitle}</h4>
            <button className="text-sm text-[var(--c-text-secondary)] transition hover:text-[var(--c-text-primary)]" onClick={() => setScanOpen(false)}>
              {copy.cancel}
            </button>
          </div>
          <div className="flex flex-col gap-3 md:flex-row">
            <label className="flex min-w-0 flex-1 flex-col gap-1 text-sm text-[var(--c-text-secondary)]">
              {copy.fieldFilePath}
              <input className={inputClass} value={scanPath} onChange={(event) => setScanPath(event.target.value)} placeholder={copy.placeholderFilePath} />
            </label>
            <div className="flex items-end">
              <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)] disabled:opacity-60" disabled={scanLoading} onClick={() => void handleScan()}>
                {scanLoading ? copy.scanning : copy.scan}
              </button>
            </div>
          </div>
          <div className="mt-4 flex flex-col gap-3">
            {scanItems.length === 0 ? (
              <div className="rounded-xl border border-dashed border-[var(--c-border)] px-4 py-6 text-sm text-[var(--c-text-muted)]">
                {copy.sourceEmpty}
              </div>
            ) : (
              scanItems.map((source) => (
                <div key={`${source.source_kind}:${source.source_uri}`} className="rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-page)] p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={`rounded-full px-2 py-1 text-xs font-medium ${source.installable ? 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300' : 'bg-amber-500/12 text-amber-700 dark:text-amber-300'}`}>
                      {source.source_kind}
                    </span>
                    <span className="text-sm text-[var(--c-text-secondary)]">{source.source_uri}</span>
                  </div>
                  {source.validation_errors.length > 0 ? (
                    <p className="mt-2 text-sm text-rose-600">{source.validation_errors.join(' | ')}</p>
                  ) : null}
                  {source.host_warnings.length > 0 ? (
                    <p className="mt-2 text-sm text-[var(--c-text-secondary)]">{source.host_warnings.join(' | ')}</p>
                  ) : null}
                  <div className="mt-3 flex flex-col gap-2">
                    {source.proposed_installs.map((proposal) => (
                      <div key={proposal.install_key} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--c-border-subtle)] px-3 py-2">
                        <div>
                          <div className="text-sm font-medium text-[var(--c-text-primary)]">{proposal.display_name}</div>
                          <div className="text-xs text-[var(--c-text-muted)]">{proposal.transport} · {proposal.host_requirement}</div>
                        </div>
                        <button className="rounded-xl bg-[var(--c-bg-deep)] px-3 py-2 text-sm font-medium text-[var(--c-text-heading)] transition hover:opacity-90 disabled:opacity-60" disabled={!source.installable || saving} onClick={() => void handleImport(source, proposal)}>
                          {copy.import}
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      ) : null}

      <div className="flex flex-col gap-3">
        {loading ? (
          <div className={`${cardClass} px-4 py-6 text-sm text-[var(--c-text-muted)]`}>{copy.loading}</div>
        ) : installs.length === 0 ? (
          <div className={`${cardClass} px-4 py-6 text-sm text-[var(--c-text-muted)]`}>{copy.empty}</div>
        ) : installs.map((install) => (
          <div key={install.id} className={`${cardClass} p-4`}>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-semibold text-[var(--c-text-heading)]">{install.display_name}</span>
                  <span className={`rounded-full px-2 py-1 text-xs font-medium ${statusTone(install.discovery_status)}`}>
                    {install.discovery_status}
                  </span>
                  <span className="rounded-full bg-[var(--c-bg-deep)] px-2 py-1 text-xs text-[var(--c-text-secondary)]">
                    {install.transport}
                  </span>
                </div>
                <div className="mt-2 flex flex-wrap gap-3 text-xs text-[var(--c-text-muted)]">
                  <span>{install.install_key}</span>
                  <span>{install.source_kind}</span>
                  <span>{install.workspace_state?.enabled ? copy.enable : copy.disable}</span>
                </div>
                {install.last_error_message ? (
                  <p className="mt-2 text-sm text-rose-600">{install.last_error_message}</p>
                ) : null}
              </div>
              <div className="flex flex-wrap gap-2">
                <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)] disabled:opacity-60" disabled={busyID === install.id} onClick={() => openEdit(install)}>
                  {copy.save}
                </button>
                <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)] disabled:opacity-60" disabled={busyID === install.id} onClick={() => void handleCheck(install)}>
                  {copy.recheck}
                </button>
                <button className="rounded-xl border border-[var(--c-border)] px-3 py-2 text-sm text-[var(--c-text-secondary)] transition hover:bg-[var(--c-bg-deep)] disabled:opacity-60" disabled={busyID === install.id} onClick={() => void handleToggle(install)}>
                  {install.workspace_state?.enabled ? copy.disable : copy.enable}
                </button>
                <button className="rounded-xl border border-rose-300 px-3 py-2 text-sm text-rose-600 transition hover:bg-rose-50 disabled:opacity-60 dark:border-rose-900/60 dark:hover:bg-rose-950/20" disabled={busyID === install.id} onClick={() => void handleDelete(install)}>
                  {copy.delete}
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
