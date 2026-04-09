import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { LoadingPage } from '@arkloop/shared'
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
import { setUnauthenticatedHandler, setAccessTokenHandler, restoreAccessSession } from './api'
import { setClientApp } from '@arkloop/shared/api'
import {
  isLocalMode,
  isDesktop,
  getDesktopApi,
  getDesktopAccessToken,
} from '@arkloop/shared/desktop'
import { isApiError } from './api'

const sessionRestoreRetries = 12
const sessionRestoreDelayMs = 1000

function App() {
  const { t } = useLocale()
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [authChecked, setAuthChecked] = useState(false)
  const [onboardingDone, setOnboardingDone] = useState<boolean | null>(null)

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
  }, [])

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

    // Local 模式: Go 后端使用固定 token，跳过刷新流程
    if (isLocalMode()) {
      const desktopToken = getDesktopAccessToken() ?? 'arkloop-desktop-local-token'
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
        console.error('session restore failed', err)
      })
      .finally(() => {
        if (controller.signal.aborted) return
        setAuthChecked(true)
      })

    return () => {
      controller.abort()
    }
  }, [])

  const handleLoggedIn = useCallback((token: string) => {
    clearActiveThreadIdInStorage()
    writeAccessTokenToStorage(token)
    setAccessToken(token)
    // accessToken 变化后路由树切换，/login 自动 redirect 到 /
  }, [])

  const handleLoggedOut = useCallback(() => {
    // Local mode uses a fixed token — logout should be a no-op (button hidden, but guard here too)
    if (isLocalMode()) {
      const desktopToken = getDesktopAccessToken() ?? 'arkloop-desktop-local-token'
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
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </>
      )}
    </Routes>
  )
}

export default App
