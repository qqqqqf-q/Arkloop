import { apiFetch } from './client'
import type { ToolCatalogGroup } from './tool-providers'

export type { ToolCatalogGroup, ToolCatalogItem } from './tool-providers'

export type AgentScope = 'user' | 'platform'

function withScope(path: string, scope: AgentScope): string {
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}scope=${scope}`
}

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
  core_tools: string[]
  is_active: boolean
  executor_type: string
  budgets: Record<string, unknown>
  source: 'db' | 'repo'
  created_at: string
}

type RawLiteAgent = Omit<LiteAgent, 'scope'> & { scope: string }

function normalizeScope(scope: string): AgentScope {
  return scope === 'platform' ? 'platform' : 'user'
}

function normalizeLiteAgent(agent: RawLiteAgent): LiteAgent {
  return {
    ...agent,
    scope: normalizeScope(agent.scope),
  }
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
  core_tools?: string[]
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
  core_tools?: string[]
  is_active?: boolean
}

export async function listLiteAgents(accessToken: string, scope: AgentScope): Promise<LiteAgent[]> {
  const agents = await apiFetch<RawLiteAgent[]>(withScope('/v1/lite/agents', scope), { accessToken })
  return agents.map(normalizeLiteAgent)
}

export async function createLiteAgent(
  req: CreateLiteAgentRequest,
  accessToken: string,
): Promise<LiteAgent> {
  const scope = req.scope ?? 'platform'
  const agent = await apiFetch<RawLiteAgent>(withScope('/v1/lite/agents', scope), {
    method: 'POST',
    body: JSON.stringify({ ...req, scope }),
    accessToken,
  })
  return normalizeLiteAgent(agent)
}

export async function patchLiteAgent(
  id: string,
  req: PatchLiteAgentRequest,
  accessToken: string,
): Promise<LiteAgent> {
  const scope = req.scope ?? 'platform'
  const { scope: _scope, ...body } = req
  const agent = await apiFetch<RawLiteAgent>(withScope(`/v1/lite/agents/${id}`, scope), {
    method: 'PATCH',
    body: JSON.stringify(body),
    accessToken,
  })
  return normalizeLiteAgent(agent)
}

export async function deleteLiteAgent(
  id: string,
  scope: AgentScope,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(withScope(`/v1/lite/agents/${id}`, scope), {
    method: 'DELETE',
    accessToken,
  })
}

export async function listToolCatalog(
  accessToken: string,
): Promise<{ groups: ToolCatalogGroup[] }> {
  return apiFetch<{ groups: ToolCatalogGroup[] }>('/v1/tool-catalog/effective', { accessToken })
}
