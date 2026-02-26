import { apiFetch } from './client'

export type EmailStatus = {
  configured: boolean
  from?: string
  source: 'db' | 'env' | 'none'
}

export type EmailConfig = {
  from: string
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_pass_set: boolean
  smtp_tls_mode: string
}

export type UpdateEmailConfigRequest = {
  from: string
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_pass?: string
  smtp_tls_mode: string
}

export async function getEmailStatus(accessToken: string): Promise<EmailStatus> {
  return apiFetch<EmailStatus>('/v1/admin/email/status', { accessToken })
}

export async function getEmailConfig(accessToken: string): Promise<EmailConfig> {
  return apiFetch<EmailConfig>('/v1/admin/email/config', { accessToken })
}

export async function updateEmailConfig(req: UpdateEmailConfigRequest, accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/admin/email/config', {
    method: 'PUT',
    body: JSON.stringify(req),
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
