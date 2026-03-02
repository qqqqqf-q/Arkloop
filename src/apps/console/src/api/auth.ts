import { apiFetch } from './client'

export type LoginRequest = {
  login: string
  password: string
  cf_turnstile_token?: string
}

export type CaptchaConfigResponse = {
  enabled: boolean
  site_key: string
}

export type LoginResponse = {
  token_type: string
  access_token: string
  refresh_token: string
}

export type MeResponse = {
  id: string
  username: string
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

export async function getCaptchaConfig(): Promise<CaptchaConfigResponse> {
  return await apiFetch<CaptchaConfigResponse>('/v1/auth/captcha-config')
}

export async function checkUser(login: string): Promise<{ exists: boolean; masked_email?: string }> {
  return await apiFetch<{ exists: boolean; masked_email?: string }>('/v1/auth/check', {
    method: 'POST',
    body: JSON.stringify({ login }),
  })
}

export async function sendEmailOTP(email: string, cfTurnstileToken?: string): Promise<void> {
  await apiFetch<void>('/v1/auth/email/otp/send', {
    method: 'POST',
    body: JSON.stringify({ email, cf_turnstile_token: cfTurnstileToken }),
  })
}

export async function verifyEmailOTP(email: string, code: string): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/email/otp/verify', {
    method: 'POST',
    body: JSON.stringify({ email, code }),
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
