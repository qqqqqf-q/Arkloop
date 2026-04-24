import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { LiteLayout } from './layouts/LiteLayout'
import { AuthPage } from './pages/AuthPage'
import { DashboardPage } from './pages/DashboardPage'
import { AgentsPage } from './pages/AgentsPage'
import { ModelsPage } from './pages/ModelsPage'
import { ToolsPage } from './pages/ToolsPage'
import { RunsPage } from './pages/RunsPage'
import { ModulesPage } from './pages/ModulesPage'
import { SettingsPage } from './pages/SettingsPage'
import { SecurityPage } from './pages/SecurityPage'
import { BootstrapPage } from './pages/BootstrapPage'
import { OperationProvider, useToast } from '@arkloop/shared'
import { bridgeClient } from './api/bridge'
import { useLocale } from './contexts/LocaleContext'
import {
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'
import { setUnauthenticatedHandler, setAccessTokenHandler, setSessionExpiredHandler, restoreAccessSession, isApiError } from './api'
import { setClientApp } from '@arkloop/shared/api'

const sessionRestoreRetries = 12
const sessionRestoreDelayMs = 1000

function App() {
  const { t } = useLocale()
  const { addToast } = useToast()
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [authChecked, setAuthChecked] = useState(false)

  useEffect(() => {
    const controller = new AbortController()

    // bootstrap token handoff from another console (via URL fragment to avoid leaking token)
    const hashParams = new URLSearchParams(window.location.hash.replace(/^#/, ''))
    const handoffToken = hashParams.get('_t')
    if (handoffToken) {
      window.history.replaceState({}, '', `${window.location.pathname}${window.location.search}`)
      writeAccessTokenToStorage(handoffToken)
      const raf = requestAnimationFrame(() => {
        setAccessToken(handoffToken)
        setAuthChecked(true)
      })
      return () => {
        controller.abort()
        cancelAnimationFrame(raf)
      }
    }

    setClientApp('console-lite')
    setUnauthenticatedHandler(() => {
      clearAccessTokenFromStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })
    setSessionExpiredHandler(() => {
      addToast(t.sessionExpired, 'warn')
    })

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
  }, [addToast, t.sessionExpired])

  const handleLoggedIn = useCallback((token: string) => {
    writeAccessTokenToStorage(token)
    setAccessToken(token)
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    setAccessToken(null)
  }, [])

  if (!authChecked) return null

  return (
    <Routes>
      <Route path="/bootstrap/:token" element={<BootstrapPage onLoggedIn={handleLoggedIn} />} />

      {!accessToken ? (
        <Route path="*" element={<AuthPage onLoggedIn={handleLoggedIn} />} />
      ) : (
        <Route
          element={
            <OperationProvider client={bridgeClient}>
              <LiteLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />
            </OperationProvider>
          }
        >
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="agents" element={<AgentsPage />} />
          <Route path="models" element={<ModelsPage />} />
          <Route path="tools" element={<ToolsPage />} />
          <Route path="memory" element={<Navigate to="/tools" replace />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="modules" element={<ModulesPage />} />
          <Route path="security" element={<SecurityPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Route>
      )}
    </Routes>
  )
}

export default App
