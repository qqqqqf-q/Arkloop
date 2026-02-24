export const TRACE_ID_HEADER = 'X-Trace-Id'

import {
  readRefreshTokenFromStorage,
  writeRefreshTokenToStorage,
  clearRefreshTokenFromStorage,
  writeAccessTokenToStorage,
} from '../storage'

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

type RefreshResponse = {
  token_type: string
  access_token: string
  refresh_token: string
}

export async function refreshAccessToken(refreshToken: string): Promise<RefreshResponse> {
  return await apiFetch<RefreshResponse>('/v1/auth/refresh', {
    method: 'POST',
    body: JSON.stringify({ refresh_token: refreshToken }),
    _isRetry: true,
  })
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
