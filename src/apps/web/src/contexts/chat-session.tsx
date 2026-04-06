import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { useParams, useLocation } from 'react-router-dom'
import { isSearchThreadId } from '../storage'

type ChatSessionContextValue = {
  threadId: string
  isSearchThread: boolean
}

const Ctx = createContext<ChatSessionContextValue | null>(null)

export function ChatSessionProvider({ children }: { children: ReactNode }) {
  const { threadId } = useParams<{ threadId: string }>()
  const location = useLocation()
  const locationState = location.state as { isSearch?: boolean } | null

  const isSearchThread =
    locationState?.isSearch === true || (threadId != null && isSearchThreadId(threadId))

  const value = useMemo(
    () => ({ threadId: threadId!, isSearchThread }),
    [threadId, isSearchThread],
  )

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useChatSession(): ChatSessionContextValue {
  const ctx = useContext(Ctx)
  if (!ctx)
    throw new Error('useChatSession must be used within ChatSessionProvider')
  return ctx
}
