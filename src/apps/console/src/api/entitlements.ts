import { apiFetch } from './client'

export type EntitlementOverride = {
  id: string
  account_id: string
  key: string
  value: string
  value_type: 'int' | 'bool' | 'string'
  reason?: string | null
  expires_at?: string | null
  created_by_user_id?: string | null
  created_at: string
}

export type CreateEntitlementOverrideRequest = {
  account_id: string
  key: string
  value: string
  value_type: 'int' | 'bool' | 'string'
  reason?: string
  expires_at?: string
}

export async function listEntitlementOverrides(
  projectId: string,
  accessToken: string,
): Promise<EntitlementOverride[]> {
  const qs = new URLSearchParams({ account_id: projectId })
  return apiFetch<EntitlementOverride[]>(`/v1/entitlement-overrides?${qs.toString()}`, { accessToken })
}

export async function upsertEntitlementOverride(
  req: CreateEntitlementOverrideRequest,
  accessToken: string,
): Promise<EntitlementOverride> {
  return apiFetch<EntitlementOverride>('/v1/entitlement-overrides', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteEntitlementOverride(
  id: string,
  projectId: string,
  accessToken: string,
): Promise<void> {
  const qs = new URLSearchParams({ account_id: projectId })
  await apiFetch<void>(`/v1/entitlement-overrides/${id}?${qs.toString()}`, {
    method: 'DELETE',
    accessToken,
  })
}
