import { apiFetch } from './client'

export type MCPConfig = {
  id: string
  account_id: string
  name: string
  transport: string
  url?: string
  has_auth: boolean
  command?: string
  args: string[]
  cwd?: string
  inherit_parent_env: boolean
  call_timeout_ms: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export type CreateMCPConfigRequest = {
  name: string
  transport: string
  url?: string
  bearer_token?: string
  command?: string
  args?: string[]
  cwd?: string
  env?: Record<string, string>
  inherit_parent_env?: boolean
  call_timeout_ms?: number
}

export type PatchMCPConfigRequest = {
  name?: string
  url?: string
  bearer_token?: string
  call_timeout_ms?: number
  is_active?: boolean
}

export async function listMCPConfigs(accessToken: string): Promise<MCPConfig[]> {
  return apiFetch<MCPConfig[]>('/v1/mcp-configs', { accessToken })
}

export async function createMCPConfig(
  req: CreateMCPConfigRequest,
  accessToken: string,
): Promise<MCPConfig> {
  return apiFetch<MCPConfig>('/v1/mcp-configs', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function patchMCPConfig(
  id: string,
  req: PatchMCPConfigRequest,
  accessToken: string,
): Promise<MCPConfig> {
  return apiFetch<MCPConfig>(`/v1/mcp-configs/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteMCPConfig(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/mcp-configs/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}
