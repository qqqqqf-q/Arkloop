import { apiFetch } from './client'

export type LoginRequest = {
  login: string
  password: string
}

export type LoginResponse = {
  token_type: string
  access_token: string
}

export type MeResponse = {
  id: string
  display_name: string
  created_at: string
  org_id: string
  org_name: string
  role: string
  permissions: string[]
}

export type LogoutResponse = {
  ok: boolean
}

export async function login(req: LoginRequest): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function getMe(accessToken: string): Promise<MeResponse> {
  return await apiFetch<MeResponse>('/v1/me', {
    method: 'GET',
    accessToken,
  })
}

export async function logout(accessToken: string): Promise<LogoutResponse> {
  return await apiFetch<LogoutResponse>('/v1/auth/logout', {
    method: 'POST',
    accessToken,
  })
}
