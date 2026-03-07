import type { LoginResponse } from './types'

export const TRACE_ID_HEADER = 'X-Trace-Id'

export type ErrorEnvelope = {
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

let refreshPromise: Promise<string> | null = null
let unauthenticatedHandler: (() => void) | null = null
let accessTokenHandler: ((token: string) => void) | null = null
let clientApp: string | null = null

export function setUnauthenticatedHandler(fn: () => void): void {
  unauthenticatedHandler = fn
}

export function setAccessTokenHandler(fn: (token: string) => void): void {
  accessTokenHandler = fn
}

export function setClientApp(app: string): void {
  clientApp = app || null
}

export function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

export function buildUrl(path: string): string {
  const base = apiBaseUrl()
  if (!base) return path
  if (!path.startsWith('/')) return `${base}/${path}`
  return `${base}${path}`
}

export async function refreshAccessToken(signal?: AbortSignal): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/refresh', {
    method: 'POST',
    _isRetry: true,
    signal,
  })
}

async function silentRefresh(): Promise<string> {
  if (refreshPromise) return refreshPromise

  refreshPromise = (async () => {
    const resp = await refreshAccessToken()
    accessTokenHandler?.(resp.access_token)
    return resp.access_token
  })().finally(() => {
    refreshPromise = null
  })

  return refreshPromise
}

export async function readJsonSafely(response: Response): Promise<unknown | null> {
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

  if (clientApp) {
    headers.set('X-Client-App', clientApp)
  }

  const credentials = init?.credentials ?? 'include'
  const response = await fetch(buildUrl(path), { ...init, headers, credentials })
  if (response.ok) {
    if (response.status === 204 || response.headers.get('content-length') === '0') {
      return undefined as T
    }
    return (await response.json()) as T
  }

  if (response.status === 401 && !init?._isRetry) {
    try {
      const newToken = await silentRefresh()
      return await apiFetch<T>(path, { ...init, accessToken: newToken, _isRetry: true })
    } catch {
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
