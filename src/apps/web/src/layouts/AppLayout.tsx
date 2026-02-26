import { useEffect, useState, useCallback, useRef } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { PanelLeftOpen } from 'lucide-react'
import { Sidebar } from '../components/Sidebar'
import { SettingsModal } from '../components/SettingsModal'
import { ChatsSearchModal } from '../components/ChatsSearchModal'
import { NotificationsPanel } from '../components/NotificationsPanel'
import { EmailVerificationGate } from '../components/EmailVerificationGate'
import type { SettingsTab } from '../components/SettingsModal'
import {
  getMe,
  listThreads,
  logout,
  getMyCredits,
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
  const location = useLocation()

  const isSearchOpen = location.pathname.endsWith('/search')

  const handleCloseSearch = useCallback(() => {
    const basePath = location.pathname.replace(/\/search$/, '') || '/'
    navigate(basePath)
  }, [location.pathname, navigate])

  const [me, setMe] = useState<MeResponse | null>(null)
  const [meLoaded, setMeLoaded] = useState(false)
  const [threads, setThreads] = useState<ThreadResponse[]>([])
  const [runningThreadIds, setRunningThreadIds] = useState<Set<string>>(new Set())
  const [privateThreadIds, setPrivateThreadIds] = useState<Set<string>>(new Set())
  const [isPrivateMode, setIsPrivateMode] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsInitialTab, setSettingsInitialTab] = useState<SettingsTab>('account')
  const [notificationsOpen, setNotificationsOpen] = useState(false)
  const [notificationVersion, setNotificationVersion] = useState(0)
  const [creditsBalance, setCreditsBalance] = useState(0)

  const handleNotificationMarkedRead = useCallback(() => {
    setNotificationVersion((v) => v + 1)
  }, [])
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  // 加载用户信息和会话列表
  useEffect(() => {
    void (async () => {
      try {
        const [meResp, threadItems, creditsResp] = await Promise.all([
          getMe(accessToken),
          listThreads(accessToken, { limit: 200 }),
          getMyCredits(accessToken),
        ])
        if (!mountedRef.current) return
        setMe(meResp)
        setThreads(threadItems)
        setCreditsBalance(creditsResp.balance)
        setRunningThreadIds(
          new Set(threadItems.filter((t) => t.active_run_id != null).map((t) => t.id)),
        )
        setMeLoaded(true)
      } catch (err) {
        if (!mountedRef.current) return
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
        } else {
          setMeLoaded(true)
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
    if (thread.is_private) {
      setPrivateThreadIds((prev) => new Set(prev).add(thread.id))
      return
    }
    setThreads((prev) => {
      if (prev.some((t) => t.id === thread.id)) return prev
      return [thread, ...prev]
    })
  }, [])

  const handleTogglePrivateMode = useCallback(() => {
    setIsPrivateMode((prev) => !prev)
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

  const refreshCredits = useCallback(() => {
    void getMyCredits(accessToken).then((resp) => {
      if (mountedRef.current) setCreditsBalance(resp.balance)
    }).catch(() => { /* 静默失败，不影响主流程 */ })
  }, [accessToken])

  // email 验证门控：flag 开启 + 未验证时全屏拦截
  if (!meLoaded) {
    return <div style={{ position: 'fixed', inset: 0, background: 'var(--c-bg-page)' }} />
  }

  if (me !== null && !me.email_verified && me.email_verification_required && me.email) {
    return (
      <EmailVerificationGate
        accessToken={accessToken}
        email={me.email}
        onVerified={() => {
          getMe(accessToken).then(setMe).catch(() => {})
        }}
        onPollVerified={() => {
          getMe(accessToken).then(setMe).catch(() => {})
        }}
        onLogout={handleLogout}
      />
    )
  }

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
        isPrivateMode={isPrivateMode}
        onNewThread={handleNewThread}
        onLogout={handleLogout}
        onOpenSettings={(tab = 'account') => { setSettingsInitialTab(tab); setSettingsOpen(true) }}
        collapsed={sidebarCollapsed}
        onToggleCollapse={() => setSidebarCollapsed(true)}
      />

      {settingsOpen && (
        <SettingsModal
          me={me}
          accessToken={accessToken}
          initialTab={settingsInitialTab}
          onClose={() => setSettingsOpen(false)}
          onLogout={handleLogout}
          onCreditsChanged={(balance) => setCreditsBalance(balance)}
          onMeUpdated={(updated) => setMe(updated)}
        />
      )}

      {isSearchOpen && (
        <ChatsSearchModal threads={threads} accessToken={accessToken} onClose={handleCloseSearch} />
      )}

      <main className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={{ accessToken, onLoggedOut, me, creditsBalance, onThreadCreated: handleThreadCreated, onRunStarted: handleRunStarted, onRunEnded: handleRunEnded, refreshCredits, onOpenNotifications: () => setNotificationsOpen(true), notificationVersion, isPrivateMode, onTogglePrivateMode: handleTogglePrivateMode, privateThreadIds }} />
        {notificationsOpen && (
          <NotificationsPanel accessToken={accessToken} onClose={() => setNotificationsOpen(false)} onMarkedRead={handleNotificationMarkedRead} />
        )}
      </main>
    </div>
  )
}
