import { apiFetch } from './client'

// --- Platform Settings (title_summarizer) ---

export type PlatformSetting = {
  key: string
  value: string
}

export async function getPlatformSetting(key: string, accessToken: string): Promise<PlatformSetting | null> {
  try {
    return await apiFetch<PlatformSetting>(`/v1/admin/platform-settings/${key}`, { accessToken })
  } catch {
    return null
  }
}

export async function setPlatformSetting(key: string, value: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/platform-settings/${key}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
    accessToken,
  })
}

export async function deletePlatformSetting(key: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/platform-settings/${key}`, {
    method: 'DELETE',
    accessToken,
  })
}

// --- Credits Mode ---

export type CreditsModeResponse = {
  enabled: boolean
}

export async function getCreditsMode(accessToken: string): Promise<CreditsModeResponse> {
  return apiFetch<CreditsModeResponse>('/v1/admin/credits/mode', { accessToken })
}

export async function setCreditsMode(enabled: boolean, accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/admin/credits/mode', {
    method: 'PUT',
    body: JSON.stringify({ enabled }),
    accessToken,
  })
}

// --- Email Configs ---

export type EmailConfig = {
  id: string
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_pass_set: boolean
  smtp_tls_mode: string
  is_default: boolean
}

export type CreateEmailConfigRequest = {
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_pass: string
  smtp_tls_mode: string
  is_default: boolean
}

export type PatchEmailConfigRequest = {
  name?: string
  from_addr?: string
  smtp_host?: string
  smtp_port?: string
  smtp_user?: string
  smtp_pass?: string
  smtp_tls_mode?: string
}

export async function listEmailConfigs(accessToken: string): Promise<EmailConfig[]> {
  return apiFetch<EmailConfig[]>('/v1/admin/email/configs', { accessToken })
}

export async function createEmailConfig(req: CreateEmailConfigRequest, accessToken: string): Promise<EmailConfig> {
  return apiFetch<EmailConfig>('/v1/admin/email/configs', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function updateEmailConfig(id: string, req: PatchEmailConfigRequest, accessToken: string): Promise<EmailConfig> {
  return apiFetch<EmailConfig>(`/v1/admin/email/configs/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteEmailConfig(id: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/email/configs/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function setDefaultEmailConfig(id: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/email/configs/${id}/set-default`, {
    method: 'POST',
    accessToken,
  })
}

export async function sendTestEmail(to: string, accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/admin/email/test', {
    method: 'POST',
    body: JSON.stringify({ to }),
    accessToken,
  })
}
