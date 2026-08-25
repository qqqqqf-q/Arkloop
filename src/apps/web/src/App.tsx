import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { LoadingPage, useToast } from '@arkloop/shared'
import { AppLayout } from './layouts/AppLayout'
import { AuthPage } from './components/AuthPage'
import { WelcomePage } from './components/WelcomePage'
import { ChatShell } from './components/ChatShell'
import { AuthProvider } from './contexts/auth'
import { ThreadListProvider } from './contexts/thread-list'
import { AppUIProvider } from './contexts/app-ui'
import { SharePage } from './components/SharePage'
import { OnboardingWizard } from './components/OnboardingWizard'
import { useLocale } from './contexts/LocaleContext'
import { shouldDelayLocalSession } from './appAuthStartup'
import {
  clearActiveThreadIdInStorage,
  readActiveThreadIdFromStorage,
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'
import {
  createLocalSession,
  isApiError,
  setUnauthenticatedHandler,
  setAccessTokenHandler,
  setSessionExpiredHandler,
  restoreAccessSession,
  logAuthDebug,
  tokenClaimsForDebug,
} from './api'
import { setClientApp } from '@arkloop/shared/api'
import {
  isLocalMode,
  isDesktop,
  getDesktopApi,
  getDesktopAccessToken,
} from '@arkloop/shared/desktop'

const ScheduledJobsPage = lazy(() => import('./pages/scheduled-jobs/ScheduledJobsPage'))

const sessionRestoreRetries = 12
const sessionRestoreDelayMs = 1000
let startupRestoreAttempted = false

function StartupRoute() {
  const [targetThreadId, setTargetThreadId] = useState<string | null>(null)
  const shouldRestoreStartupThread = isDesktop() && !startupRestoreAttempted
  const [checked, setChecked] = useState(!shouldRestoreStartupThread)

  useEffect(() => {
    if (!shouldRestoreStartupThread) return
    startupRestoreAttempted = true
    let active = true
    const api = getDesktopApi()
    if (!api?.config) {
      setChecked(true)
      return
    }
    void api.config.get().then((config) => {
      if (!active) return
      if ((config.desktop?.startupOpen ?? 'last-workspace') === 'last-workspace') {
        setTargetThreadId(readActiveThreadIdFromStorage())
      }
      setChecked(true)
    }).catch(() => {
      if (active) setChecked(true)
    })
    return () => {
      active = false
    }
  }, [shouldRestoreStartupThread])

  if (!checked) return null
  if (targetThreadId) return <Navigate to={`/t/${targetThreadId}`} replace />
  return <WelcomePage />
}

function App() {
  const { t } = useLocale()
  const { addToast } = useToast()
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [authChecked, setAuthChecked] = useState(false)
  const [onboardingDone, setOnboardingDone] = useState<boolean | null>(() => (isDesktop() ? null : true))
  const [sidecarError, setSidecarError] = useState<{ title: string; message: string } | null>(null)
  const [sidecarChecked, setSidecarChecked] = useState(() => !isDesktop())

  // Desktop: 检查 onboarding 状态
  useEffect(() => {
    if (!isDesktop()) {
      const id = requestAnimationFrame(() => setOnboardingDone(true))
      return () => cancelAnimationFrame(id)
    }
    const api = getDesktopApi()
    if (!api) {
      const id = requestAnimationFrame(() => setOnboardingDone(true))
      return () => cancelAnimationFrame(id)
    }
    api.onboarding
      .getStatus()
      .then((s) => setOnboardingDone(s.completed))
      .catch(() => setOnboardingDone(true))
  }, [addToast, t.sessionExpired])

  // Desktop: 检查 sidecar 启动错误
  useEffect(() => {
    if (!isDesktop()) {
      setSidecarChecked(true)
      return
    }

    // 无 Electron preload（如 Chrome dev 环境），跳过 sidecar 检查
    if (!getDesktopApi()) {
      setSidecarChecked(true)
      return
    }

    let cancelled = false
    let cleanupRuntimeListener: (() => void) | null = null

    const check = async () => {
      const api = getDesktopApi()
      if (!api) {
        // preload not injected yet, retry shortly
        setTimeout(check, 100)
        return
      }

      try {
        // Subscribe to runtime changes for continuous updates
        if (api.sidecar.onRuntimeChanged) {
          cleanupRuntimeListener = api.sidecar.onRuntimeChanged((runtime) => {
            if (cancelled) return
            if (runtime.lastError) {
              setSidecarError({
                title: t.connectionFailed,
                message: runtime.lastError,
              })
              setSidecarChecked(true)
            } else if (runtime.status === 'running') {
              setSidecarError(null)
              setSidecarChecked(true)
            }
            // other statuses (starting, stopped without error): wait for next event
          })
        }

        const runtime = await api.sidecar.getRuntime()
        if (cancelled) return
        if (runtime.lastError) {
          setSidecarError({
            title: t.connectionFailed,
            message: runtime.lastError,
          })
          setSidecarChecked(true)
        } else if (runtime.status === 'running') {
          setSidecarChecked(true)
        }
        // stopped/starting without error: don't set sidecarChecked, wait for onRuntimeChanged
      } catch (err) {
        if (cancelled) return
        setSidecarError({
          title: t.connectionFailed,
          message: err instanceof Error ? err.message : 'Sidecar process is not responding',
        })
        setSidecarChecked(true)
      }
    }

    check()

    return () => {
      cancelled = true
      if (cleanupRuntimeListener) cleanupRuntimeListener()
    }
  }, [t.connectionFailed])

  useEffect(() => {
    const controller = new AbortController()

    setClientApp('web')
    setUnauthenticatedHandler(() => {
      logAuthDebug('app_unauthenticated_handler')
      clearAccessTokenFromStorage()
      clearActiveThreadIdInStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      logAuthDebug('app_access_token_handler', {
        has_access_token_after: !!token,
      })
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })
    setSessionExpiredHandler(() => {
      logAuthDebug('app_session_expired_handler')
      addToast(t.sessionExpired, 'warn')
    })

    const localMode = isLocalMode()
    logAuthDebug('app_auth_effect', {
      local_mode: localMode,
      sidecar_checked: sidecarChecked,
      onboarding_done: onboardingDone,
    })
    if (!sidecarChecked || onboardingDone === null) {
      setAuthChecked(false)
      return () => {
        controller.abort()
      }
    }
    if (shouldDelayLocalSession(localMode, sidecarChecked, onboardingDone)) {
      setAuthChecked(false)
      return () => {
        controller.abort()
      }
    }

    // Local 模式: local trust 只用于换取正常 session，业务 API 继续使用 JWT。
    if (localMode) {
      const desktopToken = getDesktopAccessToken()?.trim()
      logAuthDebug('local_session_prepare', {
        has_desktop_token: !!desktopToken,
      })
      if (!desktopToken) {
        clearAccessTokenFromStorage()
        setAccessToken(null)
        setAuthChecked(true)
        return () => {
          controller.abort()
        }
      }

      createLocalSession(desktopToken, controller.signal)
        .then((resp) => {
          if (controller.signal.aborted) return
          logAuthDebug('local_session_ok', {
            has_access_token_after: !!resp.access_token,
            token_claims: tokenClaimsForDebug(resp.access_token) ?? undefined,
          })
          writeAccessTokenToStorage(resp.access_token)
          setAccessToken(resp.access_token)
        })
        .catch((err) => {
          if (controller.signal.aborted) return
          if (err instanceof Error && err.name === 'AbortError') return
          logAuthDebug('local_session_fail', {
            error_name: err instanceof Error ? err.name : 'unknown',
            error_message: err instanceof Error ? err.message : String(err),
            error_status: isApiError(err) ? err.status : undefined,
            error_code: isApiError(err) ? err.code : undefined,
            trace_id: isApiError(err) ? err.traceId : undefined,
          })
          clearAccessTokenFromStorage()
          setAccessToken(null)
        })
        .finally(() => {
          if (controller.signal.aborted) return
          setAuthChecked(true)
        })
      return () => {
        controller.abort()
      }
    }

    restoreAccessSession({
      signal: controller.signal,
      retries: sessionRestoreRetries,
      retryDelayMs: sessionRestoreDelayMs,
    })
      .then((resp) => {
        if (controller.signal.aborted) return
        logAuthDebug('startup_restore_ok', {
          has_access_token_after: !!resp.access_token,
          token_claims: tokenClaimsForDebug(resp.access_token) ?? undefined,
        })
        writeAccessTokenToStorage(resp.access_token)
        setAccessToken(resp.access_token)
      })
      .catch((err) => {
        logAuthDebug('startup_restore_fail', {
          error_name: err instanceof Error ? err.name : 'unknown',
          error_message: err instanceof Error ? err.message : String(err),
          error_status: isApiError(err) ? err.status : undefined,
          error_code: isApiError(err) ? err.code : undefined,
          trace_id: isApiError(err) ? err.traceId : undefined,
        })
        if (isApiError(err) && (err.status === 401 || err.status === 403)) return
        if (err instanceof Error && err.name === 'AbortError') return
      })
      .finally(() => {
        if (controller.signal.aborted) return
        setAuthChecked(true)
      })

    return () => {
      controller.abort()
    }
  }, [addToast, onboardingDone, sidecarChecked, t.sessionExpired])

  const handleLoggedIn = useCallback((token: string) => {
    clearActiveThreadIdInStorage()
    writeAccessTokenToStorage(token)
    setAccessToken(token)
    // accessToken 变化后路由树切换，/login 自动 redirect 到 /
  }, [])

  const handleLoggedOut = useCallback(() => {
    if (isLocalMode()) {
      return
    }
    clearAccessTokenFromStorage()
    clearActiveThreadIdInStorage()
    setAccessToken(null)
  }, [])

  const handleOnboardingComplete = useCallback(() => {
    // config.mode 在 onboarding 中可能已变更，需要 reload 使 preload 重新注入 __ARKLOOP_DESKTOP__
    window.location.reload()
  }, [])

  const handleRetrySidecar = useCallback(async () => {
    setSidecarError(null)
    setSidecarChecked(false)
    const api = getDesktopApi()
    if (!api) return
    try {
      await api.sidecar.restart()
      setTimeout(() => {
        api.sidecar.getRuntime().then((runtime) => {
          if (runtime.lastError) {
            setSidecarError({
              title: t.connectionFailed,
              message: runtime.lastError,
            })
          }
          setSidecarChecked(true)
        }).catch(() => {
          setSidecarChecked(true)
        })
      }, 2000)
    } catch (err) {
      setSidecarError({
        title: t.connectionFailed,
        message: err instanceof Error ? err.message : String(err),
      })
      setSidecarChecked(true)
    }
  }, [t.connectionFailed])

  if (!sidecarChecked) {
    if (isDesktop()) return <LoadingPage label={t.loading} />
    return null
  }

  if (sidecarError) {
    return <LoadingPage label={t.loading} error={sidecarError} onRetry={handleRetrySidecar} retryLabel={t.retryConnection} />
  }

  if (onboardingDone === null) {
    if (isDesktop()) return <LoadingPage label={t.loading} />
    return null
  }
  if (onboardingDone === false) return <OnboardingWizard onComplete={handleOnboardingComplete} />

  return (
    <Routes>
      <Route path="/s/:token" element={<SharePage />} />
      {!authChecked ? (
        <Route path="*" element={<LoadingPage label={t.loading} />} />
      ) : !accessToken ? (
        <>
          <Route path="/login" element={<AuthPage onLoggedIn={handleLoggedIn} />} />
          <Route path="/register" element={<Navigate to="/login" replace />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </>
      ) : (
        <>
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="/register" element={<Navigate to="/" replace />} />
          <Route element={
            <AuthProvider accessToken={accessToken} onLoggedOut={handleLoggedOut}>
              <ThreadListProvider>
                <AppUIProvider>
                    <AppLayout />
                </AppUIProvider>
              </ThreadListProvider>
            </AuthProvider>
          }>
            <Route index element={<StartupRoute />} />
            <Route path="search" element={<WelcomePage />} />
            <Route path="t/:threadId" element={<ChatShell />} />
            <Route path="t/:threadId/search" element={<ChatShell />} />
            <Route path="scheduled-jobs" element={<Suspense fallback={<LoadingPage label={t.loading} />}><ScheduledJobsPage /></Suspense>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </>
      )}
    </Routes>
  )
}

export default App
