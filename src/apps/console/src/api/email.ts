import { apiFetch } from './client'

export type EmailStatus = {
  configured: boolean
  from: string
  provider: 'smtp' | 'noop'
}

export async function getEmailStatus(accessToken: string): Promise<EmailStatus> {
  return apiFetch<EmailStatus>('/v1/admin/email/status', { accessToken })
}

export async function sendTestEmail(to: string, accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/admin/email/test', {
    method: 'POST',
    body: JSON.stringify({ to }),
    accessToken,
  })
}
