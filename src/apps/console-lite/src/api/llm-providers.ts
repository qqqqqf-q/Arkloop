import { apiFetch } from './client'

export type LlmProviderScope = 'org' | 'platform'

function withScope(path: string, scope: LlmProviderScope): string {
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}scope=${scope}`
}

export type LlmProviderModel = {
  id: string
  provider_id: string
  model: string
  priority: number
  is_default: boolean
  tags: string[]
  when: Record<string, unknown>
  advanced_json?: Record<string, unknown> | null
  multiplier: number
  cost_per_1k_input?: number | null
  cost_per_1k_output?: number | null
  cost_per_1k_cache_write?: number | null
  cost_per_1k_cache_read?: number | null
}

export type LlmProvider = {
  id: string
  account_id?: string | null
  scope: LlmProviderScope
  provider: string
  name: string
  key_prefix: string | null
  base_url: string | null
  openai_api_mode: string | null
  advanced_json?: Record<string, unknown> | null
  created_at: string
  models: LlmProviderModel[]
}

export type CreateLlmProviderRequest = {
  scope?: LlmProviderScope
  name: string
  provider: string
  api_key: string
  base_url?: string
  openai_api_mode?: string
  advanced_json?: Record<string, unknown>
}

export type UpdateLlmProviderRequest = {
  scope?: LlmProviderScope
  name?: string
  provider?: string
  api_key?: string
  base_url?: string | null
  openai_api_mode?: string | null
  advanced_json?: Record<string, unknown> | null
}

export type CreateModelRequest = {
  scope?: LlmProviderScope
  model: string
  priority?: number
  is_default?: boolean
  tags?: string[]
  when?: Record<string, unknown>
  advanced_json?: Record<string, unknown> | null
  multiplier?: number
}

export type UpdateModelRequest = {
  scope?: LlmProviderScope
  model?: string
  priority?: number
  is_default?: boolean
  tags?: string[]
  when?: Record<string, unknown>
  advanced_json?: Record<string, unknown> | null
  multiplier?: number
}

export type AvailableModel = {
  id: string
  name: string
  configured: boolean
}

export async function listLlmProviders(accessToken: string, scope: LlmProviderScope): Promise<LlmProvider[]> {
  return apiFetch<LlmProvider[]>(withScope('/v1/llm-providers', scope), { accessToken })
}

export async function createLlmProvider(
  req: CreateLlmProviderRequest,
  accessToken: string,
): Promise<LlmProvider> {
  const scope = req.scope ?? 'platform'
  return apiFetch<LlmProvider>(withScope('/v1/llm-providers', scope), {
    method: 'POST',
    body: JSON.stringify({ ...req, scope }),
    accessToken,
  })
}

export async function updateLlmProvider(
  id: string,
  req: UpdateLlmProviderRequest,
  accessToken: string,
): Promise<LlmProvider> {
  const scope = req.scope ?? 'platform'
  return apiFetch<LlmProvider>(withScope(`/v1/llm-providers/${id}`, scope), {
    method: 'PATCH',
    body: JSON.stringify({ ...req, scope }),
    accessToken,
  })
}

export async function deleteLlmProvider(
  id: string,
  scope: LlmProviderScope,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(withScope(`/v1/llm-providers/${id}`, scope), {
    method: 'DELETE',
    accessToken,
  })
}

export async function createProviderModel(
  providerId: string,
  req: CreateModelRequest,
  accessToken: string,
): Promise<LlmProviderModel> {
  const scope = req.scope ?? 'platform'
  return apiFetch<LlmProviderModel>(withScope(`/v1/llm-providers/${providerId}/models`, scope), {
    method: 'POST',
    body: JSON.stringify({ ...req, scope }),
    accessToken,
  })
}

export async function updateProviderModel(
  providerId: string,
  modelId: string,
  req: UpdateModelRequest,
  accessToken: string,
): Promise<LlmProviderModel> {
  const scope = req.scope ?? 'platform'
  return apiFetch<LlmProviderModel>(withScope(`/v1/llm-providers/${providerId}/models/${modelId}`, scope), {
    method: 'PATCH',
    body: JSON.stringify({ ...req, scope }),
    accessToken,
  })
}

export async function deleteProviderModel(
  providerId: string,
  modelId: string,
  scope: LlmProviderScope,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(withScope(`/v1/llm-providers/${providerId}/models/${modelId}`, scope), {
    method: 'DELETE',
    accessToken,
  })
}

export async function listAvailableModels(
  providerId: string,
  scope: LlmProviderScope,
  accessToken: string,
): Promise<{ models: AvailableModel[] }> {
  return apiFetch<{ models: AvailableModel[] }>(withScope(`/v1/llm-providers/${providerId}/available-models`, scope), {
    accessToken,
  })
}
