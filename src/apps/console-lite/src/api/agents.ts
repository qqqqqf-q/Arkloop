import { apiFetch } from './client'
import type { ToolCatalogGroup } from './tool-providers'

export type { ToolCatalogGroup, ToolCatalogItem } from './tool-providers'

export type LiteAgent = {
  id: string
  persona_key: string
  display_name: string
  description?: string
  prompt_md: string
  model?: string
  temperature?: number
  max_output_tokens?: number
  reasoning_mode: string
  tool_policy: string
  tool_allowlist: string[]
  is_active: boolean
  executor_type: string
  budgets: Record<string, unknown>
  source: 'db' | 'repo'
  created_at: string
}

export type CreateLiteAgentRequest = {
  copy_from_repo_persona_key?: string
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
}

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

export async function listToolCatalog(
  accessToken: string,
): Promise<{ groups: ToolCatalogGroup[] }> {
  return apiFetch<{ groups: ToolCatalogGroup[] }>('/v1/tool-catalog/effective', { accessToken })
}
