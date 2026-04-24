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
import { CreditsProvider } from './contexts/credits'
import { SharePage } from './components/SharePage'
import { VerifyEmailPage } from './components/VerifyEmailPage'
import { OnboardingWizard } from './components/OnboardingWizard'
import { useLocale } from './contexts/LocaleContext'
import {
  clearActiveThreadIdInStorage,
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'
import { setUnauthenticatedHandler, setAccessTokenHandler, setSessionExpiredHandler, restoreAccessSession } from './api'
import { setClientApp } from '@arkloop/shared/api'
import {
  isLocalMode,
  isDesktop,
  getDesktopApi,
  getDesktopAccessToken,
} from '@arkloop/shared/desktop'
import { isApiError } from './api'

const ScheduledJobsPage = lazy(() => import('./pages/scheduled-jobs/ScheduledJobsPage'))

const sessionRestoreRetries = 12
const sessionRestoreDelayMs = 1000

function App() {
  const { t } = useLocale()
  const { addToast } = useToast()
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [authChecked, setAuthChecked] = useState(false)
  const [onboardingDone, setOnboardingDone] = useState<boolean | null>(null)
  const [sidecarError, setSidecarError] = useState<{ title: string; message: string } | null>(null)
  const [sidecarChecked, setSidecarChecked] = useState(false)

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
      clearAccessTokenFromStorage()
      clearActiveThreadIdInStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })
    setSessionExpiredHandler(() => {
      addToast(t.sessionExpired, 'warn')
    })

    // Local 模式: Go 后端使用固定 token，跳过刷新流程
    if (isLocalMode()) {
      const desktopToken = getDesktopAccessToken() ?? ''
      writeAccessTokenToStorage(desktopToken)
      const raf = requestAnimationFrame(() => {
        setAccessToken(desktopToken)
        setAuthChecked(true)
      })
      return () => {
        controller.abort()
        cancelAnimationFrame(raf)
      }
    }

    restoreAccessSession({
      signal: controller.signal,
      retries: sessionRestoreRetries,
      retryDelayMs: sessionRestoreDelayMs,
    })
      .then((resp) => {
        if (controller.signal.aborted) return
        writeAccessTokenToStorage(resp.access_token)
        setAccessToken(resp.access_token)
      })
      .catch((err) => {
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
  }, [addToast, t.sessionExpired])

  const handleLoggedIn = useCallback((token: string) => {
    clearActiveThreadIdInStorage()
    writeAccessTokenToStorage(token)
    setAccessToken(token)
    // accessToken 变化后路由树切换，/login 自动 redirect 到 /
  }, [])

  const handleLoggedOut = useCallback(() => {
    // Local mode uses a fixed token — logout should be a no-op (button hidden, but guard here too)
    if (isLocalMode()) {
      const desktopToken = getDesktopAccessToken() ?? ''
      writeAccessTokenToStorage(desktopToken)
      setAccessToken(desktopToken)
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
      <Route path="/verify" element={<VerifyEmailPage />} />
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
                  <CreditsProvider>
                    <AppLayout />
                  </CreditsProvider>
                </AppUIProvider>
              </ThreadListProvider>
            </AuthProvider>
          }>
            <Route index element={<WelcomePage />} />
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
