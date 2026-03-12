import { apiFetch } from './client'
import type { ToolCatalogGroup } from './tool-catalog'

export type ToolProviderScope = 'org' | 'platform'

export type ConfigFieldDef = {
  key: string
  label: string
  type: 'string' | 'number' | 'select' | 'password'
  required: boolean
  default?: string
  options?: string[]
  group?: string
  placeholder?: string
}

export type ToolProviderItem = {
  group_name: string
  provider_name: string
  is_active: boolean
  key_prefix?: string
  base_url?: string
  requires_api_key: boolean
  requires_base_url: boolean
  configured: boolean
  config_json?: Record<string, unknown>
  config_fields?: ConfigFieldDef[]
}

export type ToolProviderGroup = {
  group_name: string
  providers: ToolProviderItem[]
}

export type ToolProvidersResponse = {
  groups: ToolProviderGroup[]
}

export type ToolProvidersAndCatalogResponse = {
  providerGroups: ToolProviderGroup[]
  catalogGroups: ToolCatalogGroup[]
}

export type UpdateToolProviderCredentialPayload = {
  api_key?: string
  base_url?: string
}

function withScope(path: string, scope?: ToolProviderScope): string {
  const cleaned = (scope ?? '').trim()
  if (!cleaned) return path
  return `${path}?scope=${encodeURIComponent(cleaned)}`
}

export async function listToolProviders(accessToken: string, scope?: ToolProviderScope): Promise<ToolProvidersResponse> {
  return apiFetch<ToolProvidersResponse>(withScope('/v1/tool-providers', scope), { accessToken })
}

export async function activateToolProvider(
  group: string,
  provider: string,
  accessToken: string,
  scope?: ToolProviderScope,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-providers/${group}/${provider}/activate`, scope), {
    method: 'PUT',
    accessToken,
  })
}

export async function deactivateToolProvider(
  group: string,
  provider: string,
  accessToken: string,
  scope?: ToolProviderScope,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-providers/${group}/${provider}/deactivate`, scope), {
    method: 'PUT',
    accessToken,
  })
}

export async function updateToolProviderCredential(
  group: string,
  provider: string,
  payload: UpdateToolProviderCredentialPayload,
  accessToken: string,
  scope?: ToolProviderScope,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-providers/${group}/${provider}/credential`, scope), {
    method: 'PUT',
    body: JSON.stringify(payload),
    accessToken,
  })
}

export async function clearToolProviderCredential(
  group: string,
  provider: string,
  accessToken: string,
  scope?: ToolProviderScope,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-providers/${group}/${provider}/credential`, scope), {
    method: 'DELETE',
    accessToken,
  })
}

export async function updateToolProviderConfig(
  group: string,
  provider: string,
  configJSON: Record<string, unknown>,
  accessToken: string,
  scope?: ToolProviderScope,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-providers/${group}/${provider}/config`, scope), {
    method: 'PUT',
    body: JSON.stringify(configJSON),
    accessToken,
  })
}

export async function loadToolProvidersAndCatalog(
  accessToken: string,
  scope?: ToolProviderScope,
): Promise<ToolProvidersAndCatalogResponse> {
  const [providers, catalog] = await Promise.all([
    listToolProviders(accessToken, scope),
    apiFetch<{ groups: ToolCatalogGroup[] }>(withScope('/v1/tool-catalog', scope), { accessToken }),
  ])
  return {
    providerGroups: providers.groups,
    catalogGroups: catalog.groups,
  }
}
