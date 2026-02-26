import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { AppLayout } from './layouts/AppLayout'
import { AuthPage } from './components/AuthPage'
import { WelcomePage } from './components/WelcomePage'
import { ChatPage } from './components/ChatPage'
import { VerifyEmailPage } from './components/VerifyEmailPage'
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
    // accessToken 变化后路由树切换，/login 自动 redirect 到 /
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    clearRefreshTokenFromStorage()
    clearActiveThreadIdInStorage()
    setAccessToken(null)
  }, [])

  return (
    <Routes>
      <Route path="/verify" element={<VerifyEmailPage />} />
      {!accessToken ? (
        <>
          <Route path="/login" element={<AuthPage mode="login" onLoggedIn={handleLoggedIn} />} />
          <Route path="/register" element={<AuthPage mode="register" onLoggedIn={handleLoggedIn} />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </>
      ) : (
        <>
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="/register" element={<Navigate to="/" replace />} />
          <Route element={<AppLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />}>
            <Route index element={<WelcomePage />} />
            <Route path="search" element={<WelcomePage />} />
            <Route path="t/:threadId" element={<ChatPage />} />
            <Route path="t/:threadId/search" element={<ChatPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </>
      )}
    </Routes>
  )
}

export default App
