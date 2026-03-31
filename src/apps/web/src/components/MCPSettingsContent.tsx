import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Loader2, Plus, RefreshCw } from 'lucide-react'
import {
  checkMCPInstall,
  createMCPInstall,
  deleteMCPInstall,
  listMCPInstalls,
  setWorkspaceMCPEnablement,
  updateMCPInstall,
  type MCPInstall,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import {
  type FormState,
  type MCPCopy,
  buildRequest,
  emptyForm,
  formFromInstall,
} from './mcp/types'
import { MCPInstallList } from './mcp/MCPInstallList'
import { MCPFormModal } from './mcp/MCPFormModal'
import { MCPScanSection } from './mcp/MCPScanSection'

type Props = {
  accessToken: string
}

export function MCPSettingsContent({ accessToken }: Props) {
  const { t, locale } = useLocale()
  const ds = t.desktopSettings

  const copy: MCPCopy = useMemo(() => {
    if (locale === 'zh') {
      return {
        add: '添加服务器',
        refresh: '刷新',
        scan: '扫描导入',
        create: '创建',
        save: '保存',
        cancel: '取消',
        delete: '删除',
        edit: '编辑',
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
      edit: 'Edit',
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
  const [saving, setSaving] = useState(false)
  const [busyID, setBusyID] = useState<string | null>(null)
  const [menuID, setMenuID] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  // close menu on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as HTMLElement)) {
        setMenuID(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

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

  useEffect(() => { void loadInstalls() }, [loadInstalls])

  const setField = useCallback(<K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }))
    setFormError('')
  }, [])

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

  const closeForm = useCallback(() => {
    if (saving) return
    setFormOpen(false)
    setEditing(null)
    setFormError('')
  }, [saving])

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
    } catch (err) {
      if (err instanceof Error) {
        const map: Record<string, string> = {
          displayName: copy.errorName,
          url: copy.errorURL,
          command: copy.errorCommand,
          timeout: copy.errorTimeout,
          envJson: copy.errorEnv,
          headersJson: copy.errorHeaders,
        }
        setFormError(map[err.message] ?? copy.toastSaveFailed)
      } else {
        setFormError(copy.toastSaveFailed)
      }
    } finally {
      setSaving(false)
    }
  }, [accessToken, copy, editing, form, loadInstalls])

  const handleDelete = useCallback(async (install: MCPInstall) => {
    if (!window.confirm(`${copy.delete} "${install.display_name}"?`)) return
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

  return (
    <div className="flex flex-col gap-4">
      {/* header */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{ds.mcpTitle}</h3>
          <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{ds.mcpDesc}</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void loadInstalls()}
            disabled={loading}
            className="flex h-9 items-center gap-1.5 rounded-lg px-3 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {loading ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
            {copy.refresh}
          </button>
          <button
            type="button"
            onClick={openCreate}
            className="flex h-9 items-center gap-1.5 rounded-lg px-3 text-sm font-medium"
            style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
          >
            <Plus size={14} />
            {copy.add}
          </button>
        </div>
      </div>

      {/* notice */}
      {notice && (
        <div
          className="rounded-xl px-4 py-3 text-sm text-[var(--c-text-secondary)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
        >
          {notice}
        </div>
      )}

      {/* install list */}
      <MCPInstallList
        installs={installs}
        loading={loading}
        busyID={busyID}
        menuID={menuID}
        setMenuID={setMenuID}
        onEdit={openEdit}
        onDelete={(i) => void handleDelete(i)}
        onToggle={(i) => void handleToggle(i)}
        onCheck={(i) => void handleCheck(i)}
        copy={copy}
        menuRef={menuRef}
      />

      {/* scan & import */}
      <MCPScanSection
        accessToken={accessToken}
        copy={copy}
        onImported={async (installId) => {
          await loadInstalls()
          // auto-check after import
          try {
            await checkMCPInstall(accessToken, installId)
            await loadInstalls()
          } catch { /* check failure is non-blocking */ }
        }}
      />

      {/* create/edit modal */}
      <MCPFormModal
        open={formOpen}
        editing={!!editing}
        form={form}
        setField={setField}
        formError={formError}
        saving={saving}
        onSave={() => void handleSave()}
        onClose={closeForm}
        copy={copy}
      />
    </div>
  )
}
