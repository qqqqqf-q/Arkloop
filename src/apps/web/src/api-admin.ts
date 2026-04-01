/**
 * Admin-level API helpers for Desktop Settings pages.
 *
 * Ported from console-lite (src/apps/console-lite/src/api/) and adapted to
 * the web-app conventions (accessToken as the first parameter, same apiFetch
 * wrapper from @arkloop/shared/api).
 */

import { apiFetch, isApiError } from '@arkloop/shared/api'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type AgentScope = 'project' | 'platform'

function withScope(path: string, scope: AgentScope): string {
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}scope=${scope}`
}

const PLATFORM_SCOPE: AgentScope = 'platform'

function scopedPath(path: string): string {
  return withScope(path, PLATFORM_SCOPE)
}

// ---------------------------------------------------------------------------
// Lite Agent types
// ---------------------------------------------------------------------------

export type LiteAgent = {
  id: string
  scope: AgentScope
  persona_key: string
  display_name: string
  description?: string
  prompt_md: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode: string
  stream_thinking: boolean
  tool_policy: string
  tool_allowlist: string[]
  tool_denylist: string[]
  is_active: boolean
  executor_type: string
  budgets: Record<string, unknown>
  source: 'db' | 'repo'
  created_at: string
}

export type CreateLiteAgentRequest = {
  copy_from_repo_persona_key?: string
  scope?: AgentScope
  name: string
  prompt_md: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode?: string
  stream_thinking?: boolean
  tool_allowlist?: string[]
  tool_denylist?: string[]
  executor_type?: string
}

export type PatchLiteAgentRequest = {
  scope?: AgentScope
  name?: string
  prompt_md?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode?: string
  stream_thinking?: boolean
  tool_allowlist?: string[]
  tool_denylist?: string[]
  is_active?: boolean
}

// ---------------------------------------------------------------------------
// Lite Agent APIs
// ---------------------------------------------------------------------------

export async function listLiteAgents(
  accessToken: string,
  scope: AgentScope = 'platform',
): Promise<LiteAgent[]> {
  return await apiFetch<LiteAgent[]>(withScope('/v1/lite/agents', scope), {
    accessToken,
  })
}

export async function createLiteAgent(
  accessToken: string,
  req: CreateLiteAgentRequest,
): Promise<LiteAgent> {
  const scope = req.scope ?? 'platform'
  return await apiFetch<LiteAgent>(withScope('/v1/lite/agents', scope), {
    method: 'POST',
    body: JSON.stringify({ ...req, scope }),
    accessToken,
  })
}

export async function patchLiteAgent(
  accessToken: string,
  id: string,
  req: PatchLiteAgentRequest,
): Promise<LiteAgent> {
  const scope = req.scope ?? 'platform'
  const { scope: _scope, ...body } = req
  return await apiFetch<LiteAgent>(withScope(`/v1/lite/agents/${id}`, scope), {
    method: 'PATCH',
    body: JSON.stringify(body),
    accessToken,
  })
}

export async function deleteLiteAgent(
  accessToken: string,
  id: string,
  scope: AgentScope = 'platform',
): Promise<void> {
  await apiFetch<{ ok: boolean }>(withScope(`/v1/lite/agents/${id}`, scope), {
    method: 'DELETE',
    accessToken,
  })
}

// ---------------------------------------------------------------------------
// Tool Provider types
// ---------------------------------------------------------------------------

export type ToolProviderItem = {
  group_name: string
  provider_name: string
  is_active: boolean
  key_prefix?: string
  base_url?: string
  requires_api_key: boolean
  requires_base_url: boolean
  configured: boolean
  runtime_state?: string
  runtime_reason?: string
  runtime_status?: string
  runtime_source?: string
  config_status?: string
  config_reason?: string
  config_json?: Record<string, unknown>
  config_fields?: ToolProviderConfigField[]
  default_base_url?: string
}

export type ToolProviderConfigField = {
  key: string
  label: string
  type: 'string' | 'number' | 'select' | 'password'
  required: boolean
  default?: string
  options?: string[]
  group?: string
  placeholder?: string
}

export type ToolProviderGroup = {
  group_name: string
  providers: ToolProviderItem[]
}

export type ToolDescriptionSource = 'default' | 'platform' | 'project'

export type ToolCatalogItem = {
  name: string
  label: string
  llm_description: string
  has_override: boolean
  description_source: ToolDescriptionSource
  is_disabled: boolean
}

export type ToolCatalogGroup = {
  group: string
  tools: ToolCatalogItem[]
}

// ---------------------------------------------------------------------------
// Tool Provider APIs
// ---------------------------------------------------------------------------

export async function listToolProviders(
  accessToken: string,
): Promise<ToolProviderGroup[]> {
  const res = await apiFetch<{ groups: ToolProviderGroup[] }>(
    scopedPath('/v1/tool-providers'),
    { accessToken },
  )
  return res.groups
}

export async function activateToolProvider(
  accessToken: string,
  group: string,
  provider: string,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-providers/${group}/${provider}/activate`),
    { method: 'PUT', accessToken },
  )
}

export async function deactivateToolProvider(
  accessToken: string,
  group: string,
  provider: string,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-providers/${group}/${provider}/deactivate`),
    { method: 'PUT', accessToken },
  )
}

export async function updateToolProviderCredential(
  accessToken: string,
  group: string,
  provider: string,
  payload: Record<string, string>,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-providers/${group}/${provider}/credential`),
    { method: 'PUT', body: JSON.stringify(payload), accessToken },
  )
}

export async function clearToolProviderCredential(
  accessToken: string,
  group: string,
  provider: string,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-providers/${group}/${provider}/credential`),
    { method: 'DELETE', accessToken },
  )
}

export async function updateToolProviderConfig(
  accessToken: string,
  group: string,
  provider: string,
  configJSON: Record<string, unknown>,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-providers/${group}/${provider}/config`),
    { method: 'PUT', body: JSON.stringify(configJSON), accessToken },
  )
}

// ---------------------------------------------------------------------------
// Tool Catalog APIs
// ---------------------------------------------------------------------------

export async function listToolCatalog(
  accessToken: string,
): Promise<ToolCatalogGroup[]> {
  const res = await apiFetch<{ groups: ToolCatalogGroup[] }>(
    scopedPath('/v1/tool-catalog'),
    { accessToken },
  )
  return res.groups
}

export async function updateToolDescription(
  accessToken: string,
  toolName: string,
  description: string,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-catalog/${toolName}/description`),
    { method: 'PUT', body: JSON.stringify({ description }), accessToken },
  )
}

export async function deleteToolDescription(
  accessToken: string,
  toolName: string,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-catalog/${toolName}/description`),
    { method: 'DELETE', accessToken },
  )
}

export async function updateToolDisabled(
  accessToken: string,
  toolName: string,
  disabled: boolean,
): Promise<void> {
  await apiFetch<void>(
    scopedPath(`/v1/tool-catalog/${toolName}/disabled`),
    { method: 'PUT', body: JSON.stringify({ disabled }), accessToken },
  )
}

// ---------------------------------------------------------------------------
// Platform Settings types
// ---------------------------------------------------------------------------

export type PlatformSetting = {
  key: string
  value: string
  updated_at: string
}

// ---------------------------------------------------------------------------
// Platform Settings APIs
// ---------------------------------------------------------------------------

export async function listPlatformSettings(
  accessToken: string,
): Promise<PlatformSetting[]> {
  return await apiFetch<PlatformSetting[]>('/v1/admin/platform-settings', {
    accessToken,
  })
}

export async function getPlatformSetting(
  accessToken: string,
  key: string,
): Promise<PlatformSetting | null> {
  try {
    return await apiFetch<PlatformSetting>(
      `/v1/admin/platform-settings/${encodeURIComponent(key)}`,
      { accessToken },
    )
  } catch (err) {
    if (isApiError(err) && err.status === 404) {
      return null
    }
    throw err
  }
}

export async function updatePlatformSetting(
  accessToken: string,
  key: string,
  value: string,
): Promise<PlatformSetting> {
  return await apiFetch<PlatformSetting>(
    `/v1/admin/platform-settings/${encodeURIComponent(key)}`,
    { method: 'PUT', body: JSON.stringify({ value }), accessToken },
  )
}

export async function deletePlatformSetting(
  accessToken: string,
  key: string,
): Promise<void> {
  await apiFetch<void>(
    `/v1/admin/platform-settings/${encodeURIComponent(key)}`,
    { method: 'DELETE', accessToken },
  )
}

// ---------------------------------------------------------------------------
// Audit logs (injection detection events)
// ---------------------------------------------------------------------------

export type AuditLogEntry = {
  id: string
  account_id?: string
  actor_user_id?: string
  action: string
  target_type?: string
  target_id?: string
  trace_id: string
  metadata: Record<string, unknown>
  ip_address?: string
  user_agent?: string
  created_at: string
}

export type ListAuditLogsResponse = {
  data: AuditLogEntry[]
  total: number
}

export async function listAuditLogs(
  accessToken: string,
  params: { action?: string; limit?: number; offset?: number },
): Promise<ListAuditLogsResponse> {
  const qs = new URLSearchParams()
  if (params.action) qs.set('action', params.action)
  if (params.limit != null) qs.set('limit', String(params.limit))
  if (params.offset != null) qs.set('offset', String(params.offset))
  const query = qs.toString()
  return apiFetch<ListAuditLogsResponse>(
    `/v1/audit-logs${query ? `?${query}` : ''}`,
    { accessToken },
  )
}
