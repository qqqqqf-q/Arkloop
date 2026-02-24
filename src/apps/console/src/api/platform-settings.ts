import { apiFetch } from './client'

export type PlatformSetting = {
  key: string
  value: string
  updated_at: string
}

export async function listPlatformSettings(accessToken: string): Promise<PlatformSetting[]> {
  return apiFetch<PlatformSetting[]>('/v1/admin/platform-settings', { accessToken })
}

export async function getPlatformSetting(key: string, accessToken: string): Promise<PlatformSetting> {
  return apiFetch<PlatformSetting>(`/v1/admin/platform-settings/${key}`, { accessToken })
}

export async function setPlatformSetting(
  key: string,
  value: string,
  accessToken: string,
): Promise<PlatformSetting> {
  return apiFetch<PlatformSetting>(`/v1/admin/platform-settings/${key}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
    accessToken,
  })
}
