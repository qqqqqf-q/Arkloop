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

export { Turnstile } from './components/Turnstile'
export { ToastProvider } from './components/Toast'
export { ToastContext } from './components/toast-context'
export type { ToastVariant, ToastContextValue } from './components/toast-context'
export { useToast } from './components/useToast'
export { ErrorCallout } from './components/ErrorCallout'
export type { AppError } from './components/ErrorCallout'
export { AuthPage } from './components/AuthPage'
export type { AuthPageTranslations, AuthApi } from './components/AuthPage'
export { BootstrapPage } from './components/BootstrapPage'
export type { BootstrapTranslations, ConsoleTarget } from './components/BootstrapPage'

export {
  verifyBootstrapToken,
  setupBootstrapAdmin,
} from './api/bootstrap'
export type {
  BootstrapVerifyResponse,
  BootstrapSetupRequest,
  BootstrapSetupResponse,
} from './api/bootstrap'
export { SettingsModal } from './components/SettingsModal'
export type { SettingsModalTranslations } from './components/SettingsModal'
