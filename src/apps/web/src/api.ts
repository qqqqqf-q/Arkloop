export const TRACE_ID_HEADER = 'X-Trace-Id'

export type LoginRequest = {
  login: string
  password: string
}

export type LoginResponse = {
  token_type: string
  access_token: string
}

export type RegisterRequest = {
  login: string
  password: string
  display_name: string
}

export type RegisterResponse = {
  user_id: string
  token_type: string
  access_token: string
}

export type MeResponse = {
  id: string
  display_name: string
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
  init?: RequestInit & { accessToken?: string },
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
    return (await response.json()) as T
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

export async function register(req: RegisterRequest): Promise<RegisterResponse> {
  return await apiFetch<RegisterResponse>('/v1/auth/register', {
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

export type LogoutResponse = {
  ok: boolean
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
}

export type ThreadResponse = {
  id: string
  org_id: string
  created_by_user_id: string
  title: string | null
  created_at: string
  active_run_id: string | null
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

// Runs API

export type CreateRunResponse = {
  run_id: string
  trace_id: string
}

export async function createRun(
  accessToken: string,
  threadId: string,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/runs`, {
    method: 'POST',
    accessToken,
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
