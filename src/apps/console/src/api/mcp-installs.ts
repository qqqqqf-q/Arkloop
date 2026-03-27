import { apiFetch } from './client'

export type MCPWorkspaceState = {
  workspace_ref: string
  enabled: boolean
  enabled_at?: string
}

export type MCPInstall = {
  id: string
  install_key: string
  account_id: string
  profile_ref: string
  display_name: string
  source_kind: string
  source_uri?: string
  sync_mode: string
  transport: 'stdio' | 'http_sse' | 'streamable_http'
  launch_spec: Record<string, unknown>
  has_auth: boolean
  host_requirement: string
  discovery_status: string
  last_error_code?: string
  last_error_message?: string
  last_checked_at?: string
  created_at: string
  updated_at: string
  workspace_state?: MCPWorkspaceState
}

export type CreateMCPInstallRequest = {
  install_key?: string
  display_name: string
  transport: 'stdio' | 'http_sse' | 'streamable_http'
  launch_spec: Record<string, unknown>
  auth_headers?: Record<string, string>
  bearer_token?: string
  host_requirement?: string
  clear_auth?: boolean
}

export type UpdateMCPInstallRequest = Partial<CreateMCPInstallRequest>

export type WorkspaceMCPEnablement = {
  workspace_ref: string
  account_id: string
  install_id: string
  install_key: string
  enabled_by_user_id: string
  enabled: boolean
  enabled_at?: string
  created_at: string
  updated_at: string
  display_name: string
  profile_ref: string
  source_kind: string
  source_uri?: string
  sync_mode: string
  transport: string
  host_requirement: string
  discovery_status: string
  last_error_code?: string
  last_error_message?: string
  last_checked_at?: string
  launch_spec_json: unknown
}

export type MCPDiscoverySourceSpec = {
  install_key: string
  display_name: string
  transport: 'stdio' | 'http_sse' | 'streamable_http'
  launch_spec: Record<string, unknown>
  host_requirement: string
  has_auth?: boolean
}

export type MCPDiscoverySource = {
  source_uri: string
  source_kind: string
  installable: boolean
  validation_errors: string[]
  host_warnings: string[]
  proposed_installs: MCPDiscoverySourceSpec[]
}

export async function listMCPInstalls(accessToken: string): Promise<MCPInstall[]> {
  return apiFetch<MCPInstall[]>('/v1/mcp-installs', { accessToken })
}

export async function createMCPInstall(
  req: CreateMCPInstallRequest,
  accessToken: string,
): Promise<MCPInstall> {
  return apiFetch<MCPInstall>('/v1/mcp-installs', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function updateMCPInstall(
  id: string,
  req: UpdateMCPInstallRequest,
  accessToken: string,
): Promise<MCPInstall> {
  return apiFetch<MCPInstall>(`/v1/mcp-installs/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteMCPInstall(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/mcp-installs/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function checkMCPInstall(id: string, accessToken: string): Promise<MCPInstall> {
  return apiFetch<MCPInstall>(`/v1/mcp-installs/${id}:check`, {
    method: 'POST',
    accessToken,
  })
}

export async function listWorkspaceMCPEnablements(
  accessToken: string,
  workspaceRef?: string,
): Promise<WorkspaceMCPEnablement[]> {
  const query = workspaceRef ? `?workspace_ref=${encodeURIComponent(workspaceRef)}` : ''
  const response = await apiFetch<{ items: WorkspaceMCPEnablement[] }>(`/v1/workspace-mcp-enablements${query}`, {
    accessToken,
  })
  return response.items ?? []
}

export async function setWorkspaceMCPEnablement(
  req: { workspace_ref?: string; install_id: string; enabled: boolean },
  accessToken: string,
): Promise<WorkspaceMCPEnablement[]> {
  const response = await apiFetch<{ items: WorkspaceMCPEnablement[] }>('/v1/workspace-mcp-enablements', {
    method: 'PUT',
    body: JSON.stringify(req),
    accessToken,
  })
  return response.items ?? []
}

export async function importMCPInstall(
  req: { workspace_ref?: string; source_uri: string; install_key: string },
  accessToken: string,
): Promise<MCPInstall> {
  return apiFetch<MCPInstall>('/v1/mcp-installs/import', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function listMCPDiscoverySources(
  accessToken: string,
  options?: { workspaceRoot?: string; paths?: string[] },
): Promise<MCPDiscoverySource[]> {
  const params = new URLSearchParams()
  if (options?.workspaceRoot?.trim()) {
    params.set('workspace_root', options.workspaceRoot.trim())
  }
  for (const path of options?.paths ?? []) {
    if (path.trim()) {
      params.append('path', path.trim())
    }
  }
  const suffix = params.toString() ? `?${params.toString()}` : ''
  const response = await apiFetch<{ items: MCPDiscoverySource[] }>(`/v1/mcp-discovery-sources${suffix}`, {
    accessToken,
  })
  return response.items ?? []
}
