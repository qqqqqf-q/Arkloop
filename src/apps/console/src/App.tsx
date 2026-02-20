import { useCallback, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { ConsoleLayout } from './layouts/ConsoleLayout'
import { AuthPage } from './pages/AuthPage'
import { AuditPage } from './pages/AuditPage'
import { RunsPage } from './pages/RunsPage'
import { ProvidersPage } from './pages/ProvidersPage'
import { OrgsPage } from './pages/OrgsPage'
import {
  readAccessTokenFromStorage,
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'

function App() {
  const [accessToken, setAccessToken] = useState<string | null>(() => readAccessTokenFromStorage())

  const handleLoggedIn = useCallback((token: string) => {
    writeAccessTokenToStorage(token)
    setAccessToken(token)
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    setAccessToken(null)
  }, [])

  if (!accessToken) {
    return <AuthPage onLoggedIn={handleLoggedIn} />
  }

  return (
    <Routes>
      <Route
        element={<ConsoleLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />}
      >
        <Route index element={<Navigate to="/audit" replace />} />
        <Route path="audit" element={<AuditPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="providers" element={<ProvidersPage />} />
        <Route path="orgs" element={<OrgsPage />} />
        <Route path="*" element={<Navigate to="/audit" replace />} />
      </Route>
    </Routes>
  )
}

export default App
