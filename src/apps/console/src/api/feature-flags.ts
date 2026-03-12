import { apiFetch } from './client'

export type FeatureFlag = {
  id: string
  key: string
  description: string | null
  default_value: boolean
  supports_org_overrides: boolean
  created_at: string
}

export type OrgFeatureOverride = {
  org_id: string
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

export type SetOrgOverrideRequest = {
  org_id: string
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

export async function listFlagOrgOverrides(
  flagKey: string,
  accessToken: string,
): Promise<OrgFeatureOverride[]> {
  return apiFetch<OrgFeatureOverride[]>(`/v1/feature-flags/${flagKey}/org-overrides`, { accessToken })
}

export async function setFlagOrgOverride(
  flagKey: string,
  body: SetOrgOverrideRequest,
  accessToken: string,
): Promise<OrgFeatureOverride> {
  return apiFetch<OrgFeatureOverride>(`/v1/feature-flags/${flagKey}/org-overrides`, {
    method: 'POST',
    body: JSON.stringify(body),
    accessToken,
  })
}

export async function deleteFlagOrgOverride(
  flagKey: string,
  orgId: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/feature-flags/${flagKey}/org-overrides/${orgId}`, {
    method: 'DELETE',
    accessToken,
  })
}
