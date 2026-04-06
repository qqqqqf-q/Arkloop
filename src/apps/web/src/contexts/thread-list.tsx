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
import { listThreads, type ThreadResponse } from '../api'
import {
  readAppModeFromStorage,
  writeThreadMode,
  readThreadMode,
  type AppMode,
} from '../storage'
import { useAuth } from './auth'

export interface ThreadListContextValue {
  threads: ThreadResponse[]
  runningThreadIds: Set<string>
  privateThreadIds: Set<string>
  isPrivateMode: boolean
  pendingIncognitoMode: boolean
  addThread: (thread: ThreadResponse) => void
  removeThread: (threadId: string) => void
  updateTitle: (threadId: string, title: string) => void
  markRunning: (threadId: string) => void
  markIdle: (threadId: string) => void
  togglePrivateMode: () => void
  setPendingIncognito: (v: boolean) => void
  getFilteredThreads: (appMode: AppMode) => ThreadResponse[]
}

const ThreadListContext = createContext<ThreadListContextValue | null>(null)

export function ThreadListProvider({ children }: { children: ReactNode }) {
  const { accessToken } = useAuth()
  const mountedRef = useRef(true)

  const [threads, setThreads] = useState<ThreadResponse[]>([])
  const [runningThreadIds, setRunningThreadIds] = useState<Set<string>>(new Set())
  const [privateThreadIds, setPrivateThreadIds] = useState<Set<string>>(new Set())
  const [isPrivateMode, setIsPrivateMode] = useState(false)
  const [pendingIncognitoMode, setPendingIncognitoMode] = useState(false)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    void (async () => {
      try {
        const items = await listThreads(accessToken, { limit: 200 })
        if (!mountedRef.current) return
        setThreads(items)
        setRunningThreadIds(
          new Set(items.filter((t) => t.active_run_id != null).map((t) => t.id)),
        )
      } catch { /* 静默：网络错误由上层处理 */ }
    })()
  }, [accessToken])

  const addThread = useCallback((thread: ThreadResponse) => {
    if (thread.is_private) {
      setPrivateThreadIds((prev) => new Set(prev).add(thread.id))
      return
    }
    const mode = readAppModeFromStorage()
    writeThreadMode(thread.id, mode)
    setThreads((prev) => {
      if (prev.some((t) => t.id === thread.id)) return prev
      return [thread, ...prev]
    })
  }, [])

  const removeThread = useCallback((threadId: string) => {
    setThreads((prev) => prev.filter((t) => t.id !== threadId))
  }, [])

  const updateTitle = useCallback((threadId: string, title: string) => {
    setThreads((prev) =>
      prev.map((t) => (t.id === threadId ? { ...t, title } : t)),
    )
  }, [])

  const markRunning = useCallback((threadId: string) => {
    setRunningThreadIds((prev) => new Set(prev).add(threadId))
    setThreads((prev) => {
      const idx = prev.findIndex((t) => t.id === threadId)
      if (idx <= 0) return prev
      const thread = prev[idx]
      return [thread, ...prev.slice(0, idx), ...prev.slice(idx + 1)]
    })
  }, [])

  const markIdle = useCallback((threadId: string) => {
    setRunningThreadIds((prev) => {
      const next = new Set(prev)
      next.delete(threadId)
      return next
    })
  }, [])

  const togglePrivateMode = useCallback(() => {
    setIsPrivateMode((prev) => !prev)
  }, [])

  const getFilteredThreads = useCallback(
    (appMode: AppMode): ThreadResponse[] =>
      threads.filter((t) => readThreadMode(t.id) === appMode),
    [threads],
  )

  const value = useMemo<ThreadListContextValue>(() => ({
    threads,
    runningThreadIds,
    privateThreadIds,
    isPrivateMode,
    pendingIncognitoMode,
    addThread,
    removeThread,
    updateTitle,
    markRunning,
    markIdle,
    togglePrivateMode,
    setPendingIncognito: setPendingIncognitoMode,
    getFilteredThreads,
  }), [
    threads,
    runningThreadIds,
    privateThreadIds,
    isPrivateMode,
    pendingIncognitoMode,
    addThread,
    removeThread,
    updateTitle,
    markRunning,
    markIdle,
    togglePrivateMode,
    getFilteredThreads,
  ])

  return (
    <ThreadListContext.Provider value={value}>
      {children}
    </ThreadListContext.Provider>
  )
}

export function ThreadListContextBridge({
  value,
  children,
}: {
  value: ThreadListContextValue
  children: ReactNode
}) {
  return (
    <ThreadListContext.Provider value={value}>
      {children}
    </ThreadListContext.Provider>
  )
}

export function useThreadList(): ThreadListContextValue {
  const ctx = useContext(ThreadListContext)
  if (!ctx) throw new Error('useThreadList must be used within ThreadListProvider')
  return ctx
}
