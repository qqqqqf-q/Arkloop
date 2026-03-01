import { apiFetch } from './client'

export type OrgSetting = {
  key: string
  value: string
  updated_at: string
}

export async function listOrgSettings(orgId: string, accessToken: string): Promise<OrgSetting[]> {
  return apiFetch<OrgSetting[]>(`/v1/orgs/${orgId}/settings`, { accessToken })
}

export async function getOrgSetting(
  orgId: string,
  key: string,
  accessToken: string,
): Promise<OrgSetting> {
  return apiFetch<OrgSetting>(`/v1/orgs/${orgId}/settings/${key}`, { accessToken })
}

export async function setOrgSetting(
  orgId: string,
  key: string,
  value: string,
  accessToken: string,
): Promise<OrgSetting> {
  return apiFetch<OrgSetting>(`/v1/orgs/${orgId}/settings/${key}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
    accessToken,
  })
}

export async function deleteOrgSetting(
  orgId: string,
  key: string,
  accessToken: string,
): Promise<void> {
  return apiFetch<void>(`/v1/orgs/${orgId}/settings/${key}`, {
    method: 'DELETE',
    accessToken,
  })
}
