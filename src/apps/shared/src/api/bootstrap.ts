import { apiFetch } from './client'

export type BootstrapVerifyResponse = {
  valid: boolean
  expires_at: string
}

export type BootstrapSetupRequest = {
  token: string
  username: string
  password: string
  locale?: string
}

export type BootstrapSetupResponse = {
  user_id: string
  token_type: string
  access_token: string
}

export async function verifyBootstrapToken(token: string): Promise<BootstrapVerifyResponse> {
  return await apiFetch<BootstrapVerifyResponse>(`/v1/bootstrap/verify/${encodeURIComponent(token)}`)
}

export async function setupBootstrapAdmin(req: BootstrapSetupRequest): Promise<BootstrapSetupResponse> {
  return await apiFetch<BootstrapSetupResponse>('/v1/bootstrap/setup', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}
