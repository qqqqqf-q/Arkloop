export {
  TRACE_ID_HEADER,
  ApiError,
  isApiError,
  apiFetch,
  refreshAccessToken,
  setUnauthenticatedHandler,
  setAccessTokenHandler,
  setClientApp,
  apiBaseUrl,
  buildUrl,
  readJsonSafely,
} from './api/client'
export type { ErrorEnvelope } from './api/client'

export type { LoginRequest, LoginResponse } from './api/types'

export {
  readAccessToken,
  writeAccessToken,
  clearAccessToken,
  canUseStorage,
} from './storage/tokens'

export { ThemeProvider, useTheme } from './contexts/ThemeContext'
export type { Theme } from './contexts/ThemeContext'

export { createLocaleContext } from './contexts/LocaleContext'
export type { Locale } from './contexts/LocaleContext'
