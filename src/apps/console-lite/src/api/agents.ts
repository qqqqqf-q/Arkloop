import { apiFetch } from './client'

// -- Persona types --

export type Persona = {
  id: string
  org_id: string | null
  persona_key: string
  version: string
  display_name: string
  description?: string
  prompt_md: string
  tool_allowlist: string[]
  budgets: Record<string, unknown>
  is_active: boolean
  created_at: string
  preferred_credential?: string
  executor_type: string
  executor_config: Record<string, unknown>
}

export type CreatePersonaRequest = {
  persona_key: string
  version: string
  display_name: string
  description?: string
  prompt_md: string
  tool_allowlist?: string[]
  preferred_credential?: string
  executor_type?: string
}

export type PatchPersonaRequest = {
  display_name?: string
  description?: string
  prompt_md?: string
  tool_allowlist?: string[]
  is_active?: boolean
  preferred_credential?: string
}

// -- AgentConfig types --

export type AgentConfig = {
  id: string
  org_id?: string | null
  scope: string
  name: string
  system_prompt_template_id?: string
  system_prompt_override?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  top_p?: number
  tool_policy: string
  tool_allowlist: string[]
  tool_denylist: string[]
  content_filter_level: string
  persona_id?: string
  is_default: boolean
  prompt_cache_control: string
  reasoning_mode: string
  created_at: string
}

export type CreateAgentConfigRequest = {
  scope?: string
  name: string
  system_prompt_override?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  tool_policy?: string
  tool_allowlist?: string[]
  content_filter_level?: string
  persona_id?: string
  is_default?: boolean
  prompt_cache_control?: string
  reasoning_mode?: string
}

export type UpdateAgentConfigRequest = {
  name?: string
  system_prompt_override?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  tool_policy?: string
  tool_allowlist?: string[]
  is_default?: boolean
  reasoning_mode?: string
}

// -- LITE Agent (unified view) --

export type LiteAgent = {
  id: string
  persona_key: string
  display_name: string
  description?: string
  prompt_md: string
  model?: string
  agent_config_name?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode: string
  tool_policy: string
  tool_allowlist: string[]
  is_active: boolean
  is_default: boolean
  executor_type: string
  budgets: Record<string, unknown>
  source: 'db' | 'repo'
  agent_config_id?: string
  created_at: string
}

export type CreateLiteAgentRequest = {
  name: string
  prompt_md: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode?: string
  tool_allowlist?: string[]
  executor_type?: string
}

export type PatchLiteAgentRequest = {
  name?: string
  prompt_md?: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode?: string
  tool_allowlist?: string[]
  is_active?: boolean
  is_default?: boolean
}

// -- LLM Credential (for Model dropdown) --

export type LlmCredential = {
  id: string
  name: string
  provider: string
}

// -- Tool Provider (for Tools checkbox) --

export type ToolProviderItem = {
  group_name: string
  provider_name: string
  is_active: boolean
}

export type ToolProviderGroup = {
  group_name: string
  providers: ToolProviderItem[]
}

// -- Persona API --

export async function listPersonas(accessToken: string): Promise<Persona[]> {
  return apiFetch<Persona[]>('/v1/personas', { accessToken })
}

export async function createPersona(
  req: CreatePersonaRequest,
  accessToken: string,
): Promise<Persona> {
  return apiFetch<Persona>('/v1/personas', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function patchPersona(
  id: string,
  req: PatchPersonaRequest,
  accessToken: string,
): Promise<Persona> {
  return apiFetch<Persona>(`/v1/personas/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

// -- AgentConfig API --

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

// -- LITE Agents API (unified) --

export async function listLiteAgents(accessToken: string): Promise<LiteAgent[]> {
  return apiFetch<LiteAgent[]>('/v1/lite/agents', { accessToken })
}

export async function createLiteAgent(
  req: CreateLiteAgentRequest,
  accessToken: string,
): Promise<LiteAgent> {
  return apiFetch<LiteAgent>('/v1/lite/agents', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function patchLiteAgent(
  id: string,
  req: PatchLiteAgentRequest,
  accessToken: string,
): Promise<LiteAgent> {
  return apiFetch<LiteAgent>(`/v1/lite/agents/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteLiteAgent(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/lite/agents/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

// -- LLM Credentials (read-only, for Model dropdown) --

export async function listLlmCredentials(accessToken: string): Promise<LlmCredential[]> {
  return apiFetch<LlmCredential[]>('/v1/llm-credentials', { accessToken })
}

// -- Tool Providers (read-only, for Tools checkbox) --

export async function listToolProviders(
  accessToken: string,
): Promise<{ groups: ToolProviderGroup[] }> {
  return apiFetch<{ groups: ToolProviderGroup[] }>(
    '/v1/tool-providers?scope=platform',
    { accessToken },
  )
}

// -- Tool Catalog (available tool groups for agent allowlist) --

export type ToolCatalogItem = {
  name: string
  description: string
}

export type ToolCatalogGroup = {
  group: string
  tools: ToolCatalogItem[]
}

export async function listToolCatalog(
  accessToken: string,
): Promise<{ groups: ToolCatalogGroup[] }> {
  return apiFetch<{ groups: ToolCatalogGroup[] }>(
    '/v1/tool-catalog',
    { accessToken },
  )
}
