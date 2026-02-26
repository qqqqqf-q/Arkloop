import { apiFetch } from './client'

export type AgentConfig = {
  id: string
  org_id?: string | null // undefined for platform scope
  scope: string          // "org" | "platform"
  name: string
  system_prompt_template_id?: string
  system_prompt_override?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  top_p?: number
  context_window_limit?: number
  tool_policy: string
  tool_allowlist: string[]
  tool_denylist: string[]
  content_filter_level: string
  safety_rules_json: Record<string, unknown>
  project_id?: string
  skill_id?: string
  is_default: boolean
  prompt_cache_control: string
  created_at: string
}

export type CreateAgentConfigRequest = {
  scope?: string // "org" | "platform"; default "org"
  name: string
  system_prompt_template_id?: string
  system_prompt_override?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  top_p?: number
  context_window_limit?: number
  tool_policy?: string
  tool_allowlist?: string[]
  tool_denylist?: string[]
  content_filter_level?: string
  is_default?: boolean
  prompt_cache_control?: string
}

export type UpdateAgentConfigRequest = {
  scope?: string // "org" | "platform"; platform_admin only
  name?: string
  system_prompt_template_id?: string
  system_prompt_override?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  top_p?: number
  context_window_limit?: number
  tool_policy?: string
  tool_allowlist?: string[]
  tool_denylist?: string[]
  content_filter_level?: string
  is_default?: boolean
  prompt_cache_control?: string
}

export async function listAgentConfigs(accessToken: string): Promise<AgentConfig[]> {
  return apiFetch<AgentConfig[]>('/v1/agent-configs', { accessToken })
}

export async function createAgentConfig(
  req: CreateAgentConfigRequest,
  accessToken: string,
): Promise<AgentConfig> {
  return apiFetch<AgentConfig>('/v1/agent-configs', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function updateAgentConfig(
  id: string,
  req: UpdateAgentConfigRequest,
  accessToken: string,
): Promise<AgentConfig> {
  return apiFetch<AgentConfig>(`/v1/agent-configs/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteAgentConfig(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/agent-configs/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}
