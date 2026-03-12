import { apiFetch } from './client'

export type FeatureFlag = {
  id: string
  key: string
  description: string | null
  default_value: boolean
  created_at: string
}

export type AccountFeatureOverride = {
  account_id: string
  flag_key: string
  enabled: boolean
  created_at: string
}

export type CreateFeatureFlagRequest = {
  key: string
  description?: string | null
  default_value: boolean
}

export type UpdateFeatureFlagRequest = {
  default_value: boolean
}

export type SetAccountOverrideRequest = {
  account_id: string
  enabled: boolean
}

export async function listFeatureFlags(accessToken: string): Promise<FeatureFlag[]> {
  return apiFetch<FeatureFlag[]>('/v1/feature-flags', { accessToken })
}

export async function getFeatureFlag(key: string, accessToken: string): Promise<FeatureFlag> {
  return apiFetch<FeatureFlag>(`/v1/feature-flags/${key}`, { accessToken })
}

export async function createFeatureFlag(
  body: CreateFeatureFlagRequest,
  accessToken: string,
): Promise<FeatureFlag> {
  return apiFetch<FeatureFlag>('/v1/feature-flags', {
    method: 'POST',
    body: JSON.stringify(body),
    accessToken,
  })
}

export async function updateFeatureFlagDefault(
  key: string,
  body: UpdateFeatureFlagRequest,
  accessToken: string,
): Promise<FeatureFlag> {
  return apiFetch<FeatureFlag>(`/v1/feature-flags/${key}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
    accessToken,
  })
}

export async function deleteFeatureFlag(
  key: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/feature-flags/${key}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function listFlagAccountOverrides(
  flagKey: string,
  accessToken: string,
): Promise<AccountFeatureOverride[]> {
  return apiFetch<AccountFeatureOverride[]>(`/v1/feature-flags/${flagKey}/account-overrides`, { accessToken })
}

export async function setFlagAccountOverride(
  flagKey: string,
  body: SetAccountOverrideRequest,
  accessToken: string,
): Promise<AccountFeatureOverride> {
  return apiFetch<AccountFeatureOverride>(`/v1/feature-flags/${flagKey}/account-overrides`, {
    method: 'POST',
    body: JSON.stringify(body),
    accessToken,
  })
}

export async function deleteFlagAccountOverride(
  flagKey: string,
  accountId: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/feature-flags/${flagKey}/account-overrides/${accountId}`, {
    method: 'DELETE',
    accessToken,
  })
}
