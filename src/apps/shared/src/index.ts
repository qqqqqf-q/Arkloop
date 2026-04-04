export {
  TRACE_ID_HEADER,
  ApiError,
  isApiError,
  apiFetch,
  refreshAccessToken,
  restoreAccessSession,
  setUnauthenticatedHandler,
  setAccessTokenHandler,
  setClientApp,
  apiBaseUrl,
  buildUrl,
  readJsonSafely,
} from './api/client'
export type { ErrorEnvelope, RestoreAccessSessionOptions } from './api/client'

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
export type { AuthPageTranslations, AuthApi, ResolveIdentityResponse, RegisterRequest } from './components/AuthPage'
export { BootstrapPage } from './components/BootstrapPage'
export type { BootstrapTranslations, ConsoleTarget } from './components/BootstrapPage'
export { LoadingPage } from './components/LoadingPage'

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

export { isDesktop, getDesktopApi, getDesktopMode, isLocalMode } from './desktop'
export type { ConnectionMode, ArkloopDesktopApi } from './desktop'

export { Badge } from './components/Badge'
export type { BadgeVariant } from './components/Badge'

export { EmptyState } from './components/EmptyState'

export { Modal } from './components/Modal'

export { FormField } from './components/FormField'
export { AutoResizeTextarea } from './components/AutoResizeTextarea'

export { DataTable } from './components/DataTable'
export type { Column as DataTableColumn } from './components/DataTable'

export {
  OperationProvider,
  useOperations,
} from './components/OperationContext'
export type {
  OperationRecord,
  ModuleAction,
  BridgeOperationsClient,
} from './components/OperationContext'

export { OperationModal } from './components/OperationModal'

export { OperationHistoryModal } from './components/OperationHistoryModal'

export { ConfirmDialog } from './components/ConfirmDialog'

export { PageHeader } from './components/PageHeader'

export { PageLoading } from './components/PageLoading'

export { ModalFooter } from './components/ModalFooter'

export { SidebarNav } from './components/SidebarNav'

export { NavButton } from './components/NavButton'

export { AccessDeniedPage } from './components/AccessDeniedPage'

export { FullScreenLoading } from './components/FullScreenLoading'

export { CollapseBlock, PreText, JsonBlock } from './components/TurnViewBlocks'

export { TurnView } from './components/TurnView'

export { buildTurns, normalizeChannelEnvelopeText } from './run-turns'
export type { LlmTurn, RunEventRaw } from './run-turns'
export { ACP_DELEGATE_LAYER, isACPDelegateEventData } from './runEventDelegate'
export { buildThreadTurns, buildRequestThreadTurns, threadMessageTextContent } from './thread-turns'
export type { ThreadMessage, ThreadTurn, RequestThreadTurn } from './thread-turns'
export { redactDataUrlsInString, jsonStringifyForDebugDisplay } from './debugPayloadRedact'
export { measureTextareaHeight } from './text/measureTextareaHeight'
export { useAutoResizeTextarea } from './text/useAutoResizeTextarea'

export { Button } from './components/Button'
export type { ButtonProps } from './components/Button'

export { PillToggle } from './components/PillToggle'

export { TabBar } from './components/prompt-injection/TabBar'

export { DebugPanel } from './components/DebugPanel'
export { DebugTrigger } from './components/DebugTrigger'
export { debugBus } from './debug-bus'
export type { DebugEntry } from './debug-bus'
