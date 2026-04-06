import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { getMe, logout as apiLogout, isApiError, type MeResponse } from '../api'
import { clearActiveThreadIdInStorage } from '../storage'
import { isLocalMode, getDesktopApi } from '@arkloop/shared/desktop'

export interface AuthContextValue {
  me: MeResponse | null
  meLoaded: boolean
  accessToken: string
  logout: () => Promise<void>
  updateMe: (me: MeResponse) => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

interface AuthProviderProps {
  accessToken: string
  onLoggedOut: () => void
  children: ReactNode
}

export function AuthProvider({ accessToken, onLoggedOut, children }: AuthProviderProps) {
  const [me, setMe] = useState<MeResponse | null>(null)
  const [meLoaded, setMeLoaded] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    void (async () => {
      try {
        const meResp = await getMe(accessToken)
        if (controller.signal.aborted) return

        let resolvedMe = meResp
        if (isLocalMode() && !meResp.username) {
          try {
            const fn = getDesktopApi()?.app.getOsUsername
            const osName = fn ? await fn() : ''
            if (osName) resolvedMe = { ...meResp, username: osName }
          } catch { /* local mode fallback */ }
        }

        setMe(resolvedMe)
        setMeLoaded(true)
      } catch (err) {
        if (controller.signal.aborted) return
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
        } else {
          setMeLoaded(true)
        }
      }
    })()
    return () => controller.abort()
  }, [accessToken, onLoggedOut])

  const handleLogout = useCallback(async () => {
    try { await apiLogout(accessToken) } catch { /* best-effort */ }
    clearActiveThreadIdInStorage()
    onLoggedOut()
  }, [accessToken, onLoggedOut])

  const value = useMemo<AuthContextValue>(() => ({
    me, meLoaded, accessToken, logout: handleLogout, updateMe: setMe,
  }), [me, meLoaded, accessToken, handleLogout])

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  )
}

export function AuthContextBridge({
  value,
  children,
}: {
  value: AuthContextValue
  children: ReactNode
}) {
  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
