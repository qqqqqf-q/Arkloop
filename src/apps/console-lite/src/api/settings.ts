import { apiFetch } from './client'

export type PlatformSetting = {
  key: string
  value: string
  updated_at: string
}

export type SmtpProvider = {
  id: string
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: number
  smtp_user: string
  pass_set: boolean
  tls_mode: string
  is_default: boolean
  created_at: string
  updated_at: string
}

export type CreateSmtpProviderRequest = {
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: number
  smtp_user: string
  smtp_pass: string
  tls_mode: string
}

export type UpdateSmtpProviderRequest = {
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: number
  smtp_user: string
  smtp_pass: string
  tls_mode: string
}

export async function listPlatformSettings(accessToken: string): Promise<PlatformSetting[]> {
  return apiFetch<PlatformSetting[]>('/v1/admin/platform-settings', { accessToken })
}

export async function updatePlatformSetting(
  key: string,
  value: string,
  accessToken: string,
): Promise<PlatformSetting> {
  return apiFetch<PlatformSetting>(`/v1/admin/platform-settings/${encodeURIComponent(key)}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
    accessToken,
  })
}

export async function listSmtpProviders(accessToken: string): Promise<SmtpProvider[]> {
  return apiFetch<SmtpProvider[]>('/v1/admin/smtp-providers', { accessToken })
}

export async function createSmtpProvider(
  req: CreateSmtpProviderRequest,
  accessToken: string,
): Promise<SmtpProvider> {
  return apiFetch<SmtpProvider>('/v1/admin/smtp-providers', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function updateSmtpProvider(
  id: string,
  req: UpdateSmtpProviderRequest,
  accessToken: string,
): Promise<SmtpProvider> {
  return apiFetch<SmtpProvider>(`/v1/admin/smtp-providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteSmtpProvider(id: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/smtp-providers/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function setDefaultSmtpProvider(id: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/smtp-providers/${id}/default`, {
    method: 'PUT',
    accessToken,
  })
}

export async function testSmtpProvider(
  id: string,
  to: string,
  accessToken: string,
): Promise<void> {
  await apiFetch<void>(`/v1/admin/smtp-providers/${id}/test`, {
    method: 'POST',
    body: JSON.stringify({ to }),
    accessToken,
  })
}
