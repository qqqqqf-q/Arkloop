import type { MCPInstall, UpsertMCPInstallRequest } from '../../api'

export type Transport = 'stdio' | 'http_sse' | 'streamable_http'
export type HostRequirement = 'desktop_local' | 'desktop_sidecar' | 'cloud_worker' | 'remote_http'

export type FormState = {
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

export function emptyForm(): FormState {
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
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fallback
  return JSON.stringify(value, null, 2)
}

function asBearerToken(headers: unknown): string {
  if (!headers || typeof headers !== 'object' || Array.isArray(headers)) return ''
  const auth = (headers as Record<string, unknown>).Authorization
  if (typeof auth !== 'string') return ''
  return auth.startsWith('Bearer ') ? auth.slice(7) : ''
}

export function normalizeHostRequirement(transport: Transport, value?: string): HostRequirement {
  const cleaned = (value ?? '').trim()
  if (cleaned === 'desktop_local' || cleaned === 'desktop_sidecar' || cleaned === 'cloud_worker' || cleaned === 'remote_http') {
    return cleaned
  }
  return transport === 'stdio' ? 'cloud_worker' : 'remote_http'
}

export function formFromInstall(install: MCPInstall): FormState {
  const launch = install.launch_spec ?? {}
  return {
    displayName: install.display_name,
    transport: install.transport,
    hostRequirement: normalizeHostRequirement(install.transport, install.host_requirement),
    url: asString(launch.url),
    command: asString(launch.command),
    args: Array.isArray(launch.args) ? launch.args.map((v) => String(v)).join(', ') : '',
    cwd: asString(launch.cwd),
    envJson: formatJson(launch.env),
    headersJson: formatJson(launch.headers),
    bearerToken: asBearerToken(launch.headers),
    timeoutMs: typeof launch.call_timeout_ms === 'number' ? String(launch.call_timeout_ms) : '',
  }
}

function parseJsonMap(raw: string, fieldKey: string): Record<string, string> {
  const trimmed = raw.trim()
  if (!trimmed) return {}
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    throw new Error(fieldKey)
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(fieldKey)
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(parsed)) {
    if (typeof v !== 'string') throw new Error(fieldKey)
    if (k.trim()) out[k.trim()] = v
  }
  return out
}

export function buildRequest(form: FormState): UpsertMCPInstallRequest {
  const displayName = form.displayName.trim()
  if (!displayName) throw new Error('displayName')

  const launchSpec: Record<string, unknown> = {}
  if (form.transport === 'stdio') {
    if (!form.command.trim()) throw new Error('command')
    launchSpec.command = form.command.trim()
    const args = form.args.split(',').map((s) => s.trim()).filter(Boolean)
    if (args.length > 0) launchSpec.args = args
    if (form.cwd.trim()) launchSpec.cwd = form.cwd.trim()
    const env = parseJsonMap(form.envJson, 'envJson')
    if (Object.keys(env).length > 0) launchSpec.env = env
  } else {
    if (!form.url.trim()) throw new Error('url')
    launchSpec.url = form.url.trim()
  }

  if (form.timeoutMs.trim()) {
    const timeout = Number.parseInt(form.timeoutMs.trim(), 10)
    if (!Number.isFinite(timeout) || timeout <= 0) throw new Error('timeout')
    launchSpec.call_timeout_ms = timeout
  }

  const headers = parseJsonMap(form.headersJson, 'headersJson')
  const bearerToken = form.bearerToken.trim()
  if (bearerToken) headers.Authorization = `Bearer ${bearerToken}`

  return {
    display_name: displayName,
    transport: form.transport,
    launch_spec: launchSpec,
    auth_headers: Object.keys(headers).length > 0 ? headers : undefined,
    bearer_token: bearerToken || undefined,
    host_requirement: form.hostRequirement,
  }
}

type StatusBadgeVariant = 'success' | 'warning' | 'error' | 'neutral'

export function statusVariant(status: string): StatusBadgeVariant {
  switch (status) {
    case 'ready':
      return 'neutral'
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

export function statusLabel(status: string, labels: MCPStatusCopy): string {
  switch (status) {
    case 'ready': return labels.checked
    case 'needs_check': return labels.pending
    case 'configured': return labels.configured
    case 'connect_failed': return labels.failed
    case 'auth_invalid': return labels.authError
    case 'protocol_error': return labels.error
    case 'install_missing': return labels.missing
    default: return status
  }
}

export type MCPStatusCopy = {
  checked: string
  pending: string
  configured: string
  failed: string
  authError: string
  error: string
  missing: string
}

export type MCPCopy = {
  add: string
  refresh: string
  scan: string
  create: string
  save: string
  cancel: string
  delete: string
  edit: string
  recheck: string
  enable: string
  disable: string
  import: string
  scanning: string
  saving: string
  loading: string
  empty: string
  sourceEmpty: string
  formTitleCreate: string
  formTitleEdit: string
  scanTitle: string
  externalTitle: string
  fieldName: string
  fieldTransport: string
  fieldHost: string
  fieldURL: string
  fieldCommand: string
  fieldArgs: string
  fieldCwd: string
  fieldEnv: string
  fieldHeaders: string
  fieldToken: string
  fieldTimeout: string
  fieldFilePath: string
  placeholderFilePath: string
  errorName: string
  errorURL: string
  errorCommand: string
  errorTimeout: string
  errorEnv: string
  errorHeaders: string
  toastLoadFailed: string
  toastSaveFailed: string
  toastDeleteFailed: string
  toastCheckFailed: string
  toastToggleFailed: string
  toastScanFailed: string
  toastImportFailed: string
  toastSaved: string
  toastDeleted: string
  toastChecked: string
  toastImported: string
  status: MCPStatusCopy
}
