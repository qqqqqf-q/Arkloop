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
let refreshRequestPromise: Promise<LoginResponse> | null = null
let unauthenticatedHandler: (() => void) | null = null
let accessTokenHandler: ((token: string) => void) | null = null
let clientApp: string | null = null

export type RestoreAccessSessionOptions = {
  signal?: AbortSignal
  retries?: number
  retryDelayMs?: number
}

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
  // 桌面模式: 优先使用 Electron 注入的 API 地址
  const desktop = (globalThis as Record<string, unknown>).__ARKLOOP_DESKTOP__ as
    | { apiBaseUrl?: string; getApiBaseUrl?: () => string }
    | undefined
  if (typeof desktop?.getApiBaseUrl === 'function') {
    const current = desktop.getApiBaseUrl()
    if (current) return current.replace(/\/$/, '')
  }
  if (desktop?.apiBaseUrl) return desktop.apiBaseUrl.replace(/\/$/, '')

  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

export function buildUrl(path: string): string {
  const base = apiBaseUrl()
  if (!base) return path
  if (!path.startsWith('/')) return `${base}/${path}`
  return `${base}${path}`
}

function makeAbortError(): Error {
  if (typeof DOMException !== 'undefined') {
    return new DOMException('The operation was aborted.', 'AbortError')
  }
  const error = new Error('The operation was aborted.')
  error.name = 'AbortError'
  return error
}

function withAbort<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise
  if (signal.aborted) return Promise.reject(makeAbortError())

  return new Promise<T>((resolve, reject) => {
    const onAbort = () => {
      signal.removeEventListener('abort', onAbort)
      reject(makeAbortError())
    }

    signal.addEventListener('abort', onAbort, { once: true })

    promise
      .then((value) => {
        signal.removeEventListener('abort', onAbort)
        resolve(value)
      })
      .catch((error: unknown) => {
        signal.removeEventListener('abort', onAbort)
        reject(error)
      })
  })
}

function isAbortError(error: unknown): boolean {
  return !!error && typeof error === 'object' && 'name' in error && error.name === 'AbortError'
}

function shouldRetryRestore(error: unknown): boolean {
  if (isAbortError(error)) return false
  if (error instanceof TypeError) return true
  if (error instanceof ApiError) {
    return error.status === 429 || error.status >= 500
  }
  return false
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  if (ms <= 0) {
    return withAbort(Promise.resolve(), signal)
  }

  return new Promise<void>((resolve, reject) => {
    const timer = globalThis.setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)

    const onAbort = () => {
      globalThis.clearTimeout(timer)
      signal?.removeEventListener('abort', onAbort)
      reject(makeAbortError())
    }

    if (signal) {
      if (signal.aborted) {
        globalThis.clearTimeout(timer)
        reject(makeAbortError())
        return
      }
      signal.addEventListener('abort', onAbort, { once: true })
    }
  })
}

function requestRefreshAccessToken(): Promise<LoginResponse> {
  return apiFetch<LoginResponse>('/v1/auth/refresh', {
    method: 'POST',
    _isRetry: true,
  })
}

export async function refreshAccessToken(signal?: AbortSignal): Promise<LoginResponse> {
  if (signal?.aborted) {
    throw makeAbortError()
  }
  if (!refreshRequestPromise) {
    refreshRequestPromise = requestRefreshAccessToken().finally(() => {
      refreshRequestPromise = null
    })
  }
  return await withAbort(refreshRequestPromise, signal)
}

export async function restoreAccessSession(options: RestoreAccessSessionOptions = {}): Promise<LoginResponse> {
  const retries = Math.max(0, options.retries ?? 0)
  const retryDelayMs = Math.max(0, options.retryDelayMs ?? 1000)

  for (let attempt = 0; ; attempt += 1) {
    try {
      return await refreshAccessToken(options.signal)
    } catch (error) {
      if (!shouldRetryRestore(error) || attempt >= retries) {
        throw error
      }
      await delay(retryDelayMs, options.signal)
    }
  }
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

  const isFormData = typeof FormData !== 'undefined' && init?.body instanceof FormData
  if (init?.body && !isFormData && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  if (init?.accessToken) {
    headers.set('Authorization', `Bearer ${init.accessToken}`)
  }

  if (clientApp) {
    headers.set('X-Client-App', clientApp)
  }

  const credentials = init?.credentials ?? 'include'
  const signal = init?.signal ?? AbortSignal.timeout(30_000)
  const response = await fetch(buildUrl(path), { ...init, headers, credentials, signal })
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
    } catch (err) {
      // 仅在认证失败时登出，网络错误不触发
      if (!(err instanceof TypeError)) {
        unauthenticatedHandler?.()
      }
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
