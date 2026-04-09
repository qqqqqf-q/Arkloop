import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { getMe, logout as apiLogout, isApiError, updateMe as patchMe, type MeResponse } from '../api'
import { clearActiveThreadIdInStorage } from '../storage'
import { isLocalMode, getDesktopApi } from '@arkloop/shared/desktop'
import { detectDeviceTimeZone } from '@arkloop/shared'

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
  const [localUsername, setLocalUsername] = useState<string | null>(null)
  const autoTimezoneAttemptedRef = useRef<string | null>(null)

  useEffect(() => {
    if (!isLocalMode()) return
    let cancelled = false
    void getDesktopApi()?.app.getOsUsername?.()
      .then((value) => {
        if (cancelled) return
        const next = value.trim()
        setLocalUsername(next || null)
      })
      .catch(() => {
        if (!cancelled) setLocalUsername(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void (async () => {
      try {
        const meResp = await getMe(accessToken)
        if (controller.signal.aborted) return
        setMe(meResp)
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

  useEffect(() => {
    if (!meLoaded || !me || me.timezone != null) return
    const accountTimeZone = me.account_timezone?.trim()
    if (accountTimeZone) return
    const detectedTimeZone = detectDeviceTimeZone()
    const attemptKey = `${accessToken}:${detectedTimeZone}`
    if (autoTimezoneAttemptedRef.current === attemptKey) return
    autoTimezoneAttemptedRef.current = attemptKey
    void patchMe(accessToken, { timezone: detectedTimeZone })
      .then((updated) => {
        setMe((current) => current == null
          ? current
          : { ...current, timezone: updated.timezone ?? detectedTimeZone })
      })
      .catch(() => {})
  }, [accessToken, me, meLoaded])

  const presentedMe = useMemo(() => {
    if (!me) return null
    if (!isLocalMode()) return me
    const nextUsername = localUsername?.trim()
    if (!nextUsername || nextUsername === me.username) return me
    return { ...me, username: nextUsername }
  }, [localUsername, me])

  const handleLogout = useCallback(async () => {
    try { await apiLogout(accessToken) } catch { /* best-effort */ }
    clearActiveThreadIdInStorage()
    onLoggedOut()
  }, [accessToken, onLoggedOut])

  const value = useMemo<AuthContextValue>(() => ({
    me: presentedMe, meLoaded, accessToken, logout: handleLogout, updateMe: setMe,
  }), [presentedMe, meLoaded, accessToken, handleLogout])

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
