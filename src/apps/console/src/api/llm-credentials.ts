import { apiFetch } from './client'

export type LlmRoute = {
  id: string
  credential_id: string
  model: string
  priority: number
  is_default: boolean
  when: Record<string, unknown>
  multiplier: number
  cost_per_1k_input?: number | null
  cost_per_1k_output?: number | null
  cost_per_1k_cache_write?: number | null
  cost_per_1k_cache_read?: number | null
}

export type LlmCredential = {
  id: string
  org_id: string
  provider: string
  name: string
  key_prefix: string | null
  base_url: string | null
  openai_api_mode: string | null
  advanced_json?: Record<string, unknown> | null
  created_at: string
  routes: LlmRoute[]
}

export type CreateLlmRouteRequest = {
  model: string
  priority: number
  is_default: boolean
  when: Record<string, unknown>
  multiplier?: number
  cost_per_1k_input?: number
  cost_per_1k_output?: number
  cost_per_1k_cache_write?: number
  cost_per_1k_cache_read?: number
}

export type CreateLlmCredentialRequest = {
  name: string
  provider: string
  api_key: string
  base_url?: string
  openai_api_mode?: string
  advanced_json?: Record<string, unknown>
  routes: CreateLlmRouteRequest[]
}

export async function listLlmCredentials(accessToken: string): Promise<LlmCredential[]> {
  return apiFetch<LlmCredential[]>('/v1/llm-credentials', { accessToken })
}

export async function createLlmCredential(
  req: CreateLlmCredentialRequest,
  accessToken: string,
): Promise<LlmCredential> {
  return apiFetch<LlmCredential>('/v1/llm-credentials', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export type UpdateLlmCredentialRequest = {
  name: string
  provider?: string
  base_url?: string | null
  openai_api_mode?: string | null
  advanced_json?: Record<string, unknown>
  api_key?: string
}

export async function updateLlmCredential(
  id: string,
  req: UpdateLlmCredentialRequest,
  accessToken: string,
): Promise<LlmCredential> {
  return apiFetch<LlmCredential>(`/v1/llm-credentials/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export type UpdateLlmRouteRequest = {
  model: string
  priority: number
  is_default: boolean
  when: Record<string, unknown>
  multiplier?: number
  cost_per_1k_input?: number
  cost_per_1k_output?: number
  cost_per_1k_cache_write?: number
  cost_per_1k_cache_read?: number
}

export async function updateLlmRoute(
  credentialId: string,
  routeId: string,
  req: UpdateLlmRouteRequest,
  accessToken: string,
): Promise<LlmRoute> {
  return apiFetch<LlmRoute>(`/v1/llm-credentials/${credentialId}/routes/${routeId}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteLlmCredential(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/llm-credentials/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}
