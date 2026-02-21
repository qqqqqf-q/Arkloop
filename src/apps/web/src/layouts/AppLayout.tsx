import { useEffect, useState, useCallback, useRef } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { PanelLeftOpen } from 'lucide-react'
import { Sidebar } from '../components/Sidebar'
import {
  getMe,
  listThreads,
  logout,
  isApiError,
  type MeResponse,
  type ThreadResponse,
} from '../api'
import { clearActiveThreadIdInStorage } from '../storage'

type Props = {
  accessToken: string
  onLoggedOut: () => void
}

export function AppLayout({ accessToken, onLoggedOut }: Props) {
  const navigate = useNavigate()

  const [me, setMe] = useState<MeResponse | null>(null)
  const [threads, setThreads] = useState<ThreadResponse[]>([])
  const [runningThreadIds, setRunningThreadIds] = useState<Set<string>>(new Set())
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  // 加载用户信息和会话列表
  useEffect(() => {
    void (async () => {
      try {
        const [meResp, threadItems] = await Promise.all([
          getMe(accessToken),
          listThreads(accessToken, { limit: 200 }),
        ])
        if (!mountedRef.current) return
        setMe(meResp)
        setThreads(threadItems)
        setRunningThreadIds(
          new Set(threadItems.filter((t) => t.active_run_id != null).map((t) => t.id)),
        )
      } catch (err) {
        if (!mountedRef.current) return
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
        }
      }
    })()
  }, [accessToken, onLoggedOut])

  const handleLogout = useCallback(async () => {
    try {
      await logout(accessToken)
    } catch (err) {
      if (isApiError(err) && err.status !== 401) return
    }
    clearActiveThreadIdInStorage()
    onLoggedOut()
  }, [accessToken, onLoggedOut])

  const handleNewThread = useCallback(() => {
    navigate('/')
  }, [navigate])

  // 从 WelcomePage 新建的 thread 需要注入到列表
  const handleThreadCreated = useCallback((thread: ThreadResponse) => {
    setThreads((prev) => {
      if (prev.some((t) => t.id === thread.id)) return prev
      return [thread, ...prev]
    })
  }, [])

  const handleRunStarted = useCallback((threadId: string) => {
    setRunningThreadIds((prev) => new Set(prev).add(threadId))
  }, [])

  const handleRunEnded = useCallback((threadId: string) => {
    setRunningThreadIds((prev) => {
      const next = new Set(prev)
      next.delete(threadId)
      return next
    })
  }, [])

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--c-bg-page)]">
      {/* 侧边栏折叠时的展开按钮 */}
      {sidebarCollapsed && (
        <button
          onClick={() => setSidebarCollapsed(false)}
          className="fixed left-3 top-3 z-40 flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
        >
          <PanelLeftOpen size={18} />
        </button>
      )}

      <Sidebar
        me={me}
        threads={threads}
        runningThreadIds={runningThreadIds}
        onNewThread={handleNewThread}
        onLogout={handleLogout}
        collapsed={sidebarCollapsed}
        onToggleCollapse={() => setSidebarCollapsed(true)}
      />

      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={{ accessToken, onLoggedOut, me, onThreadCreated: handleThreadCreated, onRunStarted: handleRunStarted, onRunEnded: handleRunEnded }} />
      </main>
    </div>
  )
}
