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
import { getMyCredits } from '../api'
import { useAuth } from './auth'

export interface CreditsContextValue {
  creditsBalance: number
  refreshCredits: () => void
  setCreditsBalance: (balance: number) => void
}

const CreditsContext = createContext<CreditsContextValue | null>(null)

export function CreditsProvider({ children }: { children: ReactNode }) {
  const { accessToken } = useAuth()
  const [creditsBalance, setCreditsBalance] = useState(0)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    void getMyCredits(accessToken)
      .then((resp) => {
        if (mountedRef.current) setCreditsBalance(resp.balance)
      })
      .catch(() => {})
  }, [accessToken])

  const refreshCredits = useCallback(() => {
    void getMyCredits(accessToken)
      .then((resp) => {
        if (mountedRef.current) setCreditsBalance(resp.balance)
      })
      .catch(() => {})
  }, [accessToken])

  const value = useMemo<CreditsContextValue>(() => ({
    creditsBalance, refreshCredits, setCreditsBalance,
  }), [creditsBalance, refreshCredits])

  return (
    <CreditsContext.Provider value={value}>
      {children}
    </CreditsContext.Provider>
  )
}

export function CreditsContextBridge({
  value,
  children,
}: {
  value: CreditsContextValue
  children: ReactNode
}) {
  return (
    <CreditsContext.Provider value={value}>
      {children}
    </CreditsContext.Provider>
  )
}

export function useCredits(): CreditsContextValue {
  const ctx = useContext(CreditsContext)
  if (!ctx) throw new Error('useCredits must be used within CreditsProvider')
  return ctx
}
