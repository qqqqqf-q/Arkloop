import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate, useNavigate } from 'react-router-dom'
import { AppLayout } from './layouts/AppLayout'
import { AuthPage } from './components/AuthPage'
import { WelcomePage } from './components/WelcomePage'
import { ChatPage } from './components/ChatPage'
import {
  clearActiveThreadIdInStorage,
  readAccessTokenFromStorage,
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
  writeRefreshTokenToStorage,
  clearRefreshTokenFromStorage,
} from './storage'
import { setUnauthenticatedHandler, setAccessTokenHandler } from './api'

function App() {
  const [accessToken, setAccessToken] = useState<string | null>(() => readAccessTokenFromStorage())
  const navigate = useNavigate()

  useEffect(() => {
    setUnauthenticatedHandler(() => {
      clearAccessTokenFromStorage()
      clearRefreshTokenFromStorage()
      clearActiveThreadIdInStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })
  }, [])

  const handleLoggedIn = useCallback((token: string, refreshToken: string) => {
    clearActiveThreadIdInStorage()
    writeAccessTokenToStorage(token)
    writeRefreshTokenToStorage(refreshToken)
    setAccessToken(token)
    navigate('/', { replace: true })
  }, [navigate])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    clearRefreshTokenFromStorage()
    clearActiveThreadIdInStorage()
    setAccessToken(null)
  }, [])

  if (!accessToken) {
    return <AuthPage onLoggedIn={handleLoggedIn} />
  }

  return (
    <Routes>
      <Route
        element={<AppLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />}
      >
        <Route index element={<WelcomePage />} />
        <Route path="search" element={<WelcomePage />} />
        <Route path="t/:threadId" element={<ChatPage />} />
        <Route path="t/:threadId/search" element={<ChatPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export default App
