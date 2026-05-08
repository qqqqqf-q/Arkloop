import type { LoginResponse } from './types'

export const TRACE_ID_HEADER = 'X-Trace-Id'
export const AUTH_DEBUG_STORAGE_KEY = 'arkloop:web:auth_debug'

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

type AuthDebugValue = string | number | boolean | null | undefined
type AuthDebugData = Record<string, AuthDebugValue | Record<string, AuthDebugValue> | Array<AuthDebugValue>>

function authDebugEnabled(): boolean {
  try {
    const storage = (globalThis as { localStorage?: Storage }).localStorage
    return storage?.getItem(AUTH_DEBUG_STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

function currentDesktopInfo(): Record<string, unknown> | undefined {
  return (globalThis as Record<string, unknown>).__ARKLOOP_DESKTOP__ as Record<string, unknown> | undefined
}

function currentDesktopMode(): string | null {
  const desktop = currentDesktopInfo()
  const getMode = desktop?.getMode
  if (typeof getMode === 'function') {
    const mode = getMode()
    return typeof mode === 'string' ? mode : null
  }
  return typeof desktop?.mode === 'string' ? desktop.mode : null
}

function currentDesktopAccessToken(): string | null {
  const desktop = currentDesktopInfo()
  const getAccessToken = desktop?.getAccessToken
  if (typeof getAccessToken === 'function') {
    const token = getAccessToken()
    return typeof token === 'string' && token.trim() ? token.trim() : null
  }
  return typeof desktop?.accessToken === 'string' && desktop.accessToken.trim() ? desktop.accessToken.trim() : null
}

function shouldUseDesktopLocalSession(): boolean {
  return currentDesktopMode() === 'local' && currentDesktopAccessToken() != null
}

function baseAuthDebugData(): AuthDebugData {
  const desktop = currentDesktopInfo()
  return {
    is_desktop: !!desktop,
    mode: currentDesktopMode(),
    api_base_url: apiBaseUrl() || null,
  }
}

function numericClaim(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function stringClaim(value: unknown): string | null {
  return typeof value === 'string' ? value : null
}

function decodeJwtPayload(token: string): Record<string, unknown> | null {
  const parts = token.split('.')
  if (parts.length < 2) return null
  try {
    const normalized = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=')
    const raw = globalThis.atob(padded)
    return JSON.parse(raw) as Record<string, unknown>
  } catch {
    return null
  }
}

export function tokenClaimsForDebug(token?: string): Record<string, AuthDebugValue> | null {
  const trimmed = token?.trim()
  if (!trimmed || trimmed.startsWith('arkloop-desktop-') || trimmed.startsWith('ak-')) return null
  const claims = decodeJwtPayload(trimmed)
  if (!claims) return null
  return {
    typ: stringClaim(claims.typ),
    sub: stringClaim(claims.sub),
    account: stringClaim(claims.account),
    role: stringClaim(claims.role),
    iat: numericClaim(claims.iat),
    exp: numericClaim(claims.exp),
  }
}

function errorDebugData(error: unknown): AuthDebugData {
  if (error instanceof ApiError) {
    return {
      status: error.status,
      code: error.code,
      message: error.message,
      trace_id: error.traceId,
    }
  }
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
    }
  }
  return {
    message: String(error),
  }
}

export function logAuthDebug(phase: string, data: AuthDebugData = {}): void {
  if (!authDebugEnabled()) return
  const payload: AuthDebugData & { phase: string; ts: string } = {
    phase,
    ts: new Date().toISOString(),
    ...baseAuthDebugData(),
    ...data,
  }
  const summary = [
    `phase=${payload.phase}`,
    payload.status != null ? `status=${payload.status}` : null,
    payload.code != null ? `code=${payload.code}` : null,
    payload.path != null ? `path=${payload.path}` : null,
    payload.trace_id != null ? `trace_id=${payload.trace_id}` : null,
  ].filter(Boolean).join(' ')
  console.info(`[arkloop-auth] ${summary}`, payload)
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

let refreshPromise: Promise<string> | null = null
let refreshRequestPromise: Promise<LoginResponse> | null = null
let unauthenticatedHandler: (() => void) | null = null
let accessTokenHandler: ((token: string) => void) | null = null
let sessionExpiredHandler: (() => void) | null = null
let clientApp: string | null = null
let loggedOut = false
let logoutNotified = false

export type RestoreAccessSessionOptions = {
  signal?: AbortSignal
  retries?: number
  retryDelayMs?: number
}

export function setUnauthenticatedHandler(fn: () => void): void {
  unauthenticatedHandler = fn
}

export function setAccessTokenHandler(fn: (token: string) => void): void {
  accessTokenHandler = (token: string) => {
    loggedOut = false
    logoutNotified = false
    fn(token)
  }
}

export function setSessionExpiredHandler(fn: () => void): void {
  sessionExpiredHandler = fn
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
  if (shouldUseDesktopLocalSession()) {
    const desktopToken = currentDesktopAccessToken()
    logAuthDebug('refresh_start', { path: '/v1/auth/local-session', strategy: 'desktop_local_session' })
    return apiFetch<LoginResponse>('/v1/auth/local-session', {
      method: 'POST',
      accessToken: desktopToken ?? undefined,
      _isRetry: true,
    })
  }
  logAuthDebug('refresh_start', { path: '/v1/auth/refresh' })
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

export async function silentRefresh(): Promise<string> {
  if (loggedOut) throw new ApiError({ status: 401, message: 'logged out' })
  if (refreshPromise) return refreshPromise

  refreshPromise = (async () => {
    const resp = await refreshAccessToken()
    logAuthDebug('refresh_ok', {
      path: shouldUseDesktopLocalSession() ? '/v1/auth/local-session' : '/v1/auth/refresh',
      token_claims: tokenClaimsForDebug(resp.access_token) ?? undefined,
    })
    accessTokenHandler?.(resp.access_token)
    return resp.access_token
  })().catch((error: unknown) => {
    logAuthDebug('refresh_fail', errorDebugData(error))
    throw error
  }).finally(() => {
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
    if (path.startsWith('/v1/auth/')) {
      logAuthDebug('auth_api_ok', {
        path,
        method: init?.method ?? 'GET',
        status: response.status,
        is_retry: !!init?._isRetry,
        credentials,
      })
    }
    if (response.status === 204 || response.headers.get('content-length') === '0') {
      return undefined as T
    }
    return (await response.json()) as T
  }

  if (response.status === 401 && !init?._isRetry) {
    const payload = await readJsonSafely(response.clone())
    const env = payload && typeof payload === 'object' ? payload as ErrorEnvelope : null
    logAuthDebug('api_401', {
      path,
      method: init?.method ?? 'GET',
      status: response.status,
      code: typeof env?.code === 'string' ? env.code : undefined,
      message: typeof env?.message === 'string' ? env.message : undefined,
      trace_id: typeof env?.trace_id === 'string' ? env.trace_id : response.headers.get(TRACE_ID_HEADER) ?? undefined,
      is_retry: !!init?._isRetry,
      credentials,
      token_claims: tokenClaimsForDebug(init?.accessToken) ?? undefined,
    })
    try {
      const newToken = await silentRefresh()
      logAuthDebug('api_401_retry', {
        path,
        method: init?.method ?? 'GET',
        token_claims: tokenClaimsForDebug(newToken) ?? undefined,
      })
      return await apiFetch<T>(path, { ...init, accessToken: newToken, _isRetry: true })
    } catch (err) {
      if (!(err instanceof TypeError)) {
        loggedOut = true
        if (!logoutNotified) {
          logoutNotified = true
          logAuthDebug('unauthenticated_handler', errorDebugData(err))
          sessionExpiredHandler?.()
          unauthenticatedHandler?.()
        } else {
          logAuthDebug('unauthenticated_handler_skipped', errorDebugData(err))
        }
      }
      throw err
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
    if (path.startsWith('/v1/auth/') || response.status === 401 || response.status === 403) {
      logAuthDebug('api_error', {
        path,
        method: init?.method ?? 'GET',
        status: response.status,
        code,
        message,
        trace_id: traceId,
        is_retry: !!init?._isRetry,
        credentials,
      })
    }
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
