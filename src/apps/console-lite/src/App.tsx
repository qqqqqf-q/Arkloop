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
import {
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'
import { setUnauthenticatedHandler, setAccessTokenHandler, refreshAccessToken } from './api'
import { setClientApp } from '@arkloop/shared/api'

function App() {
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [authChecked, setAuthChecked] = useState(false)

  useEffect(() => {
    setClientApp('console-lite')
    setUnauthenticatedHandler(() => {
      clearAccessTokenFromStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })

    refreshAccessToken()
      .then((resp) => {
        writeAccessTokenToStorage(resp.access_token)
        setAccessToken(resp.access_token)
      })
      .catch(() => {})
      .finally(() => {
        setAuthChecked(true)
      })
  }, [])

  const handleLoggedIn = useCallback((token: string) => {
    writeAccessTokenToStorage(token)
    setAccessToken(token)
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    setAccessToken(null)
  }, [])

  if (!authChecked) return null

  if (!accessToken) {
    return <AuthPage onLoggedIn={handleLoggedIn} />
  }

  return (
    <Routes>
      <Route
        element={<LiteLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />}
      >
        <Route index element={<Navigate to="/dashboard" replace />} />
        <Route path="dashboard" element={<DashboardPage />} />
        <Route path="agents" element={<AgentsPage />} />
        <Route path="models" element={<ModelsPage />} />
        <Route path="tools" element={<ToolsPage />} />
        <Route path="memory" element={<Navigate to="/tools" replace />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="modules" element={<ModulesPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/dashboard" replace />} />
      </Route>
    </Routes>
  )
}

export default App
