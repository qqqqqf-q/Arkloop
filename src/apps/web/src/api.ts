export const TRACE_ID_HEADER = 'X-Trace-Id'

import {
  readRefreshTokenFromStorage,
  writeRefreshTokenToStorage,
  clearRefreshTokenFromStorage,
  writeAccessTokenToStorage,
} from './storage'

export type LoginRequest = {
  login: string
  password: string
  cf_turnstile_token?: string
}

export type LoginResponse = {
  token_type: string
  access_token: string
  refresh_token: string
}

export type RegisterRequest = {
  login: string
  password: string
  email: string
  invite_code?: string
  locale?: string
  cf_turnstile_token?: string
}

export type RegisterResponse = {
  user_id: string
  token_type: string
  access_token: string
  refresh_token: string
  warning?: string
}

export type RegistrationModeResponse = {
  mode: 'invite_only' | 'open'
}

export type MeResponse = {
  id: string
  login: string
  username: string
  email?: string
  email_verified: boolean
  email_verification_required: boolean
}

type ErrorEnvelope = {
  code?: unknown
  message?: unknown
  details?: unknown
  trace_id?: unknown
}

export class ApiError extends Error {
  readonly status: number
  readonly code?: string
  readonly traceId?: string
  readonly details?: unknown

  constructor(params: {
    status: number
    message: string
    code?: string
    traceId?: string
    details?: unknown
  }) {
    super(params.message)
    this.name = 'ApiError'
    this.status = params.status
    this.code = params.code
    this.traceId = params.traceId
    this.details = params.details
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

function buildUrl(path: string): string {
  const base = apiBaseUrl()
  if (!base) return path
  if (!path.startsWith('/')) return `${base}/${path}`
  return `${base}${path}`
}

// 模块级静默刷新状态
let refreshPromise: Promise<string> | null = null
let unauthenticatedHandler: (() => void) | null = null
let accessTokenHandler: ((token: string) => void) | null = null

export function setUnauthenticatedHandler(fn: () => void): void {
  unauthenticatedHandler = fn
}

export function setAccessTokenHandler(fn: (token: string) => void): void {
  accessTokenHandler = fn
}

async function silentRefresh(): Promise<string> {
  if (refreshPromise) return refreshPromise

  refreshPromise = (async () => {
    const refreshToken = readRefreshTokenFromStorage()
    if (!refreshToken) throw new Error('no refresh token')

    const resp = await refreshAccessToken(refreshToken)
    writeAccessTokenToStorage(resp.access_token)
    writeRefreshTokenToStorage(resp.refresh_token)
    accessTokenHandler?.(resp.access_token)
    return resp.access_token
  })().finally(() => {
    refreshPromise = null
  })

  return refreshPromise
}

async function readJsonSafely(response: Response): Promise<unknown | null> {
  const text = await response.text()
  if (!text) return null
  try {
    return JSON.parse(text) as unknown
  } catch {
    return null
  }
}

export async function apiFetch<T>(
  path: string,
  init?: RequestInit & { accessToken?: string; _isRetry?: boolean },
): Promise<T> {
  const headers = new Headers(init?.headers)
  headers.set('Accept', 'application/json')

  if (init?.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  if (init?.accessToken) {
    headers.set('Authorization', `Bearer ${init.accessToken}`)
  }

  const response = await fetch(buildUrl(path), { ...init, headers })
  if (response.ok) {
    if (response.status === 204 || response.headers.get('content-length') === '0') {
      return undefined as T
    }
    return (await response.json()) as T
  }

  // 401 静默刷新（只在非重试请求上触发，防递归）
  if (response.status === 401 && !init?._isRetry) {
    try {
      const newToken = await silentRefresh()
      return await apiFetch<T>(path, { ...init, accessToken: newToken, _isRetry: true })
    } catch {
      clearRefreshTokenFromStorage()
      unauthenticatedHandler?.()
    }
  }

  const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
  const payload = await readJsonSafely(response)

  if (payload && typeof payload === 'object') {
    const env = payload as ErrorEnvelope
    const traceId =
      typeof env.trace_id === 'string' ? env.trace_id : headerTraceId
    const code = typeof env.code === 'string' ? env.code : undefined
    const message =
      typeof env.message === 'string'
        ? env.message
        : `请求失败（HTTP ${response.status}）`
    throw new ApiError({
      status: response.status,
      message,
      code,
      traceId,
      details: env.details,
    })
  }

  throw new ApiError({
    status: response.status,
    message: `请求失败（HTTP ${response.status}）`,
    traceId: headerTraceId,
  })
}

export async function login(req: LoginRequest): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function refreshAccessToken(refreshToken: string): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/refresh', {
    method: 'POST',
    body: JSON.stringify({ refresh_token: refreshToken }),
    _isRetry: true,
  })
}

export async function register(req: RegisterRequest): Promise<RegisterResponse> {
  return await apiFetch<RegisterResponse>('/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function getRegistrationMode(): Promise<RegistrationModeResponse> {
  return await apiFetch<RegistrationModeResponse>('/v1/auth/registration-mode', {
    method: 'GET',
  })
}

export async function getMe(accessToken: string): Promise<MeResponse> {
  return await apiFetch<MeResponse>('/v1/me', {
    method: 'GET',
    accessToken,
  })
}

export async function updateMe(accessToken: string, username: string): Promise<{ username: string }> {
  return await apiFetch<{ username: string }>('/v1/me', {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ username }),
  })
}

export async function sendEmailVerification(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/auth/email/verify/send', {
    method: 'POST',
    accessToken,
  })
}

export async function confirmEmailVerification(token: string): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>('/v1/auth/email/verify/confirm', {
    method: 'POST',
    body: JSON.stringify({ token }),
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

export async function checkUser(login: string): Promise<{ exists: boolean; masked_email?: string }> {
  return await apiFetch<{ exists: boolean; masked_email?: string }>('/v1/auth/check', {
    method: 'POST',
    body: JSON.stringify({ login }),
  })
}

export type LogoutResponse = {
  ok: boolean
}

export type CaptchaConfigResponse = {
  enabled: boolean
  site_key: string
}

export async function getCaptchaConfig(): Promise<CaptchaConfigResponse> {
  return await apiFetch<CaptchaConfigResponse>('/v1/auth/captcha-config')
}

export async function logout(accessToken: string): Promise<LogoutResponse> {
  return await apiFetch<LogoutResponse>('/v1/auth/logout', {
    method: 'POST',
    accessToken,
  })
}

// Threads API

export type CreateThreadRequest = {
  title?: string
  is_private?: boolean
}

export type ThreadResponse = {
  id: string
  org_id: string
  created_by_user_id: string
  title: string | null
  created_at: string
  active_run_id: string | null
  is_private: boolean
}

export async function createThread(
  accessToken: string,
  req?: CreateThreadRequest,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>('/v1/threads', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req ?? {}),
  })
}

export type ListThreadsRequest = {
  limit?: number
  before_created_at?: string
  before_id?: string
}

export async function listThreads(
  accessToken: string,
  req?: ListThreadsRequest,
): Promise<ThreadResponse[]> {
  const sp = new URLSearchParams()
  if (req?.limit) sp.set('limit', String(req.limit))
  if (req?.before_created_at) sp.set('before_created_at', req.before_created_at)
  if (req?.before_id) sp.set('before_id', req.before_id)
  const suffix = sp.toString() ? `?${sp.toString()}` : ''
  return await apiFetch<ThreadResponse[]>(`/v1/threads${suffix}`, {
    method: 'GET',
    accessToken,
  })
}

export async function searchThreads(
  accessToken: string,
  q: string,
  limit = 50,
): Promise<ThreadResponse[]> {
  const sp = new URLSearchParams({ q, limit: String(limit) })
  return await apiFetch<ThreadResponse[]>(`/v1/threads/search?${sp.toString()}`, {
    method: 'GET',
    accessToken,
  })
}

export async function listStarredThreadIds(accessToken: string): Promise<string[]> {
  return await apiFetch<string[]>('/v1/threads/starred', {
    method: 'GET',
    accessToken,
  })
}

export async function starThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:star`, {
    method: 'POST',
    accessToken,
  })
}

export async function unstarThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:star`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function forkThread(
  accessToken: string,
  threadId: string,
  messageId: string,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}:fork`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ message_id: messageId }),
  })
}

// Messages API

export type CreateMessageRequest = {
  content: string
}

export type MessageResponse = {
  id: string
  org_id: string
  thread_id: string
  created_by_user_id: string
  role: string
  content: string
  created_at: string
}

export async function createMessage(
  accessToken: string,
  threadId: string,
  req: CreateMessageRequest,
): Promise<MessageResponse> {
  return await apiFetch<MessageResponse>(`/v1/threads/${threadId}/messages`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function listMessages(
  accessToken: string,
  threadId: string,
  limit = 200,
): Promise<MessageResponse[]> {
  return await apiFetch<MessageResponse[]>(
    `/v1/threads/${threadId}/messages?limit=${limit}`,
    {
      method: 'GET',
      accessToken,
    },
  )
}

export async function editMessage(
  accessToken: string,
  threadId: string,
  messageId: string,
  content: string,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/messages/${messageId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ content }),
  })
}

// Runs API

export type CreateRunResponse = {
  run_id: string
  trace_id: string
}

export async function createRun(
  accessToken: string,
  threadId: string,
  skillId?: string,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/runs`, {
    method: 'POST',
    accessToken,
    body: skillId ? JSON.stringify({ skill_id: skillId }) : undefined,
  })
}

export type ThreadRunResponse = {
  run_id: string
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  created_at: string
}

export async function listThreadRuns(
  accessToken: string,
  threadId: string,
  limit = 50,
): Promise<ThreadRunResponse[]> {
  return await apiFetch<ThreadRunResponse[]>(
    `/v1/threads/${threadId}/runs?limit=${limit}`,
    {
      method: 'GET',
      accessToken,
    },
  )
}

export type CancelRunResponse = {
  ok: boolean
}

export async function cancelRun(
  accessToken: string,
  runId: string,
): Promise<CancelRunResponse> {
  return await apiFetch<CancelRunResponse>(`/v1/runs/${runId}:cancel`, {
    method: 'POST',
    accessToken,
  })
}

export type ProvideInputResponse = {
  ok: boolean
}

export async function provideInput(
  accessToken: string,
  runId: string,
  content: string,
): Promise<ProvideInputResponse> {
  return await apiFetch<ProvideInputResponse>(`/v1/runs/${runId}/input`, {
    method: 'POST',
    body: JSON.stringify({ content }),
    accessToken,
  })
}

export type RetryThreadResponse = {
  run_id: string
  trace_id: string
}

export async function retryThread(
  accessToken: string,
  threadId: string,
): Promise<RetryThreadResponse> {
  return await apiFetch<RetryThreadResponse>(`/v1/threads/${threadId}:retry`, {
    method: 'POST',
    accessToken,
  })
}

// Credits API

export type CreditTransaction = {
  id: string
  org_id: string
  amount: number
  type: string
  reference_type?: string
  reference_id?: string
  note?: string
  thread_title?: string
  created_at: string
}

export type MeCreditsResponse = {
  balance: number
  transactions: CreditTransaction[]
}

export async function getMyCredits(
  accessToken: string,
  from?: string,
  to?: string,
): Promise<MeCreditsResponse> {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  const qs = params.size > 0 ? `?${params.toString()}` : ''
  return await apiFetch<MeCreditsResponse>(`/v1/me/credits${qs}`, {
    method: 'GET',
    accessToken,
  })
}

export type MeUsageSummary = {
  org_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  record_count: number
}

export async function getMyUsage(
  accessToken: string,
  year: number,
  month: number,
): Promise<MeUsageSummary> {
  return await apiFetch<MeUsageSummary>(`/v1/me/usage?year=${year}&month=${month}`, {
    method: 'GET',
    accessToken,
  })
}

export type RedeemCodeResponse = {
  code: string
  type: string
  value: string
}

export async function redeemCode(
  accessToken: string,
  code: string,
): Promise<RedeemCodeResponse> {
  return await apiFetch<RedeemCodeResponse>('/v1/me/redeem', {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ code }),
  })
}

// Invite Code API

export type InviteCodeResponse = {
  id: string
  user_id: string
  code: string
  max_uses: number
  use_count: number
  is_active: boolean
  created_at: string
}

export async function getMyInviteCode(
  accessToken: string,
): Promise<InviteCodeResponse> {
  return await apiFetch<InviteCodeResponse>('/v1/me/invite-code', {
    method: 'GET',
    accessToken,
  })
}

export async function resetMyInviteCode(
  accessToken: string,
): Promise<InviteCodeResponse> {
  return await apiFetch<InviteCodeResponse>('/v1/me/invite-code/reset', {
    method: 'POST',
    accessToken,
  })
}

// Notifications API

export type NotificationItem = {
  id: string
  user_id: string
  org_id: string
  type: string
  title: string
  body: string
  payload: Record<string, unknown>
  read_at?: string
  created_at: string
}

export async function listNotifications(
  accessToken: string,
  opts?: { unreadOnly?: boolean; type?: string },
): Promise<{ data: NotificationItem[] }> {
  const params = new URLSearchParams()
  if (opts?.unreadOnly) params.set('unread_only', 'true')
  if (opts?.type) params.set('type', opts.type)
  const query = params.toString()
  return await apiFetch<{ data: NotificationItem[] }>(`/v1/notifications${query ? `?${query}` : ''}`, {
    method: 'GET',
    accessToken,
  })
}

export async function markAllNotificationsRead(
  accessToken: string,
): Promise<{ ok: boolean; count: number }> {
  return await apiFetch<{ ok: boolean; count: number }>('/v1/notifications', {
    method: 'PATCH',
    accessToken,
  })
}

export async function markNotificationRead(
  accessToken: string,
  id: string,
): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>(`/v1/notifications/${id}`, {
    method: 'PATCH',
    accessToken,
  })
}

export async function transcribeAudio(
  accessToken: string,
  audioBlob: Blob,
  filename: string,
  language?: string,
): Promise<{ text: string }> {
  const form = new FormData()
  form.append('file', audioBlob, filename)
  if (language) form.append('language', language)

  const base = apiBaseUrl()
  const url = base ? `${base}/v1/asr/transcribe` : `/v1/asr/transcribe`

  const headers = new Headers()
  headers.set('Accept', 'application/json')
  headers.set('Authorization', `Bearer ${accessToken}`)

  const response = await fetch(url, { method: 'POST', body: form, headers })
  if (!response.ok) {
    const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
    const payload = await readJsonSafely(response)
    const env = payload && typeof payload === 'object' ? (payload as ErrorEnvelope) : null
    throw new ApiError({
      status: response.status,
      message: typeof env?.message === 'string' ? env.message : `转写失败（HTTP ${response.status}）`,
      traceId: headerTraceId,
    })
  }
  return response.json() as Promise<{ text: string }>
}

// Share API

export type ShareResponse = {
  token: string
  url: string
  access_type: 'public' | 'password'
  created_at: string
}

export async function createThreadShare(
  accessToken: string,
  threadId: string,
  accessType: 'public' | 'password',
  password?: string,
): Promise<ShareResponse> {
  return await apiFetch<ShareResponse>(`/v1/threads/${threadId}:share`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ access_type: accessType, password }),
  })
}

export async function getThreadShare(
  accessToken: string,
  threadId: string,
): Promise<ShareResponse> {
  return await apiFetch<ShareResponse>(`/v1/threads/${threadId}:share`, {
    method: 'GET',
    accessToken,
  })
}

export async function deleteThreadShare(
  accessToken: string,
  threadId: string,
): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:share`, {
    method: 'DELETE',
    accessToken,
  })
}

export type SharedThreadResponse = {
  requires_password: boolean
  thread?: {
    title: string | null
    created_at: string
  }
  messages?: Array<{
    id: string
    role: string
    content: string
    created_at: string
  }>
}

export async function getSharedThread(
  token: string,
  sessionToken?: string,
): Promise<SharedThreadResponse> {
  const params = new URLSearchParams()
  if (sessionToken) params.set('session_token', sessionToken)
  const qs = params.toString()
  return await apiFetch<SharedThreadResponse>(`/v1/s/${token}${qs ? `?${qs}` : ''}`)
}

export type VerifyShareResponse = {
  session_token: string
}

export async function verifySharePassword(
  token: string,
  password: string,
): Promise<VerifyShareResponse> {
  return await apiFetch<VerifyShareResponse>(`/v1/s/${token}/verify`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}
