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

export type UpdateLlmCredentialRequest = {
  name: string
  provider?: string
  base_url?: string | null
  openai_api_mode?: string | null
  advanced_json?: Record<string, unknown>
  api_key?: string
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

type LlmProviderModel = {
  id: string
  provider_id: string
  model: string
  priority: number
  is_default: boolean
  tags?: string[]
  when: Record<string, unknown>
  multiplier: number
  cost_per_1k_input?: number | null
  cost_per_1k_output?: number | null
  cost_per_1k_cache_write?: number | null
  cost_per_1k_cache_read?: number | null
}

type LlmProvider = {
  id: string
  org_id: string
  provider: string
  name: string
  key_prefix: string | null
  base_url: string | null
  openai_api_mode: string | null
  advanced_json?: Record<string, unknown> | null
  created_at: string
  models: LlmProviderModel[]
}

function mapProviderModelToRoute(model: LlmProviderModel): LlmRoute {
  return {
    id: model.id,
    credential_id: model.provider_id,
    model: model.model,
    priority: model.priority,
    is_default: model.is_default,
    when: model.when ?? {},
    multiplier: model.multiplier,
    cost_per_1k_input: model.cost_per_1k_input ?? null,
    cost_per_1k_output: model.cost_per_1k_output ?? null,
    cost_per_1k_cache_write: model.cost_per_1k_cache_write ?? null,
    cost_per_1k_cache_read: model.cost_per_1k_cache_read ?? null,
  }
}

function mapProviderToCredential(provider: LlmProvider): LlmCredential {
  return {
    id: provider.id,
    org_id: provider.org_id,
    provider: provider.provider,
    name: provider.name,
    key_prefix: provider.key_prefix,
    base_url: provider.base_url,
    openai_api_mode: provider.openai_api_mode,
    advanced_json: provider.advanced_json ?? null,
    created_at: provider.created_at,
    routes: (provider.models ?? []).map(mapProviderModelToRoute),
  }
}

async function listLlmProviders(accessToken: string): Promise<LlmProvider[]> {
  return apiFetch<LlmProvider[]>('/v1/llm-providers', { accessToken })
}

async function getLlmCredentialByID(id: string, accessToken: string): Promise<LlmCredential> {
  const providers = await listLlmProviders(accessToken)
  const provider = providers.find((item) => item.id === id)
  if (!provider) {
    throw new Error(`provider ${id} not found`)
  }
  return mapProviderToCredential(provider)
}

async function createLlmProviderModel(
  providerId: string,
  req: CreateLlmRouteRequest,
  accessToken: string,
): Promise<LlmRoute> {
  const model = await apiFetch<LlmProviderModel>(`/v1/llm-providers/${providerId}/models`, {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
  return mapProviderModelToRoute(model)
}

export async function listLlmCredentials(accessToken: string): Promise<LlmCredential[]> {
  const providers = await listLlmProviders(accessToken)
  return providers.map(mapProviderToCredential)
}

export async function createLlmCredential(
  req: CreateLlmCredentialRequest,
  accessToken: string,
): Promise<LlmCredential> {
  const provider = await apiFetch<LlmProvider>('/v1/llm-providers', {
    method: 'POST',
    body: JSON.stringify({
      name: req.name,
      provider: req.provider,
      api_key: req.api_key,
      base_url: req.base_url,
      openai_api_mode: req.openai_api_mode,
      advanced_json: req.advanced_json,
    }),
    accessToken,
  })

  try {
    for (const route of req.routes) {
      await createLlmProviderModel(provider.id, route, accessToken)
    }
  } catch (error) {
    try {
      await apiFetch<{ ok: boolean }>(`/v1/llm-providers/${provider.id}`, {
        method: 'DELETE',
        accessToken,
      })
    } catch {
      // 忽略补偿失败，保留原始错误
    }
    throw error
  }

  return getLlmCredentialByID(provider.id, accessToken)
}

export async function updateLlmCredential(
  id: string,
  req: UpdateLlmCredentialRequest,
  accessToken: string,
): Promise<LlmCredential> {
  const provider = await apiFetch<LlmProvider>(`/v1/llm-providers/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
  return mapProviderToCredential(provider)
}

export async function updateLlmRoute(
  credentialId: string,
  routeId: string,
  req: UpdateLlmRouteRequest,
  accessToken: string,
): Promise<LlmRoute> {
  const model = await apiFetch<LlmProviderModel>(`/v1/llm-providers/${credentialId}/models/${routeId}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
  return mapProviderModelToRoute(model)
}

export async function deleteLlmCredential(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/llm-providers/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}
