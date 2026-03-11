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
import { clearActiveThreadIdInStorage, writeSelectedPersonaKeyToStorage, SEARCH_PERSONA_KEY } from '../storage'

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
  const [pendingIncognitoMode, setPendingIncognitoMode] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [rightPanelOpen, setRightPanelOpen] = useState(false)
  const [isSearchMode, setIsSearchMode] = useState(false)
  // ref 用于在 popstate 回调里读取最新值，避免闭包过期
  const isSearchModeRef = useRef(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsInitialTab, setSettingsInitialTab] = useState<SettingsTab>('account')
  const [notificationsOpen, setNotificationsOpen] = useState(
    () => new URLSearchParams(location.search).has('notices'),
  )
  const [notificationVersion, setNotificationVersion] = useState(0)
  const [creditsBalance, setCreditsBalance] = useState(0)
  const [pendingSkillPrompt, setPendingSkillPrompt] = useState<string | null>(null)

  const handleNotificationMarkedRead = useCallback(() => {
    setNotificationVersion((v) => v + 1)
  }, [])

  const openNotifications = useCallback(() => {
    setNotificationsOpen(true)
    const params = new URLSearchParams(window.location.search)
    if (!params.has('notices')) {
      params.set('notices', '')
      const next = `${window.location.pathname}?${params.toString()}`
      window.history.replaceState(window.history.state, '', next)
    }
  }, [])

  const closeNotifications = useCallback(() => {
    setNotificationsOpen(false)
    const params = new URLSearchParams(window.location.search)
    if (params.has('notices')) {
      params.delete('notices')
      const qs = params.toString()
      const next = qs ? `${window.location.pathname}?${qs}` : window.location.pathname
      window.history.replaceState(window.history.state, '', next)
    }
  }, [])
  const mountedRef = useRef(true)

  // 同步 ref，使 popstate 回调始终拿到最新值
  useEffect(() => { isSearchModeRef.current = isSearchMode }, [isSearchMode])

  // 离开 / 时退出搜索模式（覆盖 popstate 和 navigate 两种场景）
  useEffect(() => {
    if (location.pathname !== '/') setIsSearchMode(false)
  }, [location.pathname])

  // 路由切换时重置右侧面板状态，避免 sidebar 宽度残留
  useEffect(() => {
    setRightPanelOpen(false)
    if (notificationsOpen) closeNotifications()
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // Mouse 5 / 浏览器返回键：退出搜索模式而非离开页面
  useEffect(() => {
    const onPopState = () => {
      if (isSearchModeRef.current) setIsSearchMode(false)
    }
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

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
    setIsSearchMode(false)
    closeNotifications()
    navigate('/')
  }, [navigate, closeNotifications])

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

  const handleSetPendingIncognito = useCallback((v: boolean) => {
    setPendingIncognitoMode(v)
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

  const handleThreadTitleUpdated = useCallback((threadId: string, title: string) => {
    setThreads((prev) =>
      prev.map((t) => (t.id === threadId ? { ...t, title } : t)),
    )
  }, [])

  const handleThreadDeleted = useCallback((deletedId: string) => {
    setThreads((prev) => prev.filter((t) => t.id !== deletedId))
    // 如果删除的是当前打开的会话，跳回首页
    if (location.pathname === `/t/${deletedId}` || location.pathname.startsWith(`/t/${deletedId}/`)) {
      navigate('/')
    }
  }, [location.pathname, navigate])

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
        isPrivateMode={(() => {
          const currentThreadId = location.pathname.match(/^\/t\/([^/]+)/)?.[1] ?? null
          return isPrivateMode || pendingIncognitoMode || (currentThreadId != null && privateThreadIds.has(currentThreadId))
        })()}
        accessToken={accessToken}
        onNewThread={handleNewThread}
        onLogout={handleLogout}
        onOpenSettings={(tab = 'account') => { setSettingsInitialTab(tab); setSettingsOpen(true) }}
        collapsed={sidebarCollapsed}
        onToggleCollapse={() => setSidebarCollapsed(true)}
        onOpenSearch={() => {
            if (location.pathname !== '/') navigate('/')
            window.history.pushState({ searchMode: true }, '', '/')
            writeSelectedPersonaKeyToStorage(SEARCH_PERSONA_KEY)
            setIsSearchMode(true)
          }}
        isSearchMode={isSearchMode}
        onThreadTitleUpdated={handleThreadTitleUpdated}
        onThreadDeleted={handleThreadDeleted}
        narrow={rightPanelOpen}
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
          onTrySkill={(prompt) => {
            setSettingsOpen(false)
            navigate('/')
            setPendingSkillPrompt(prompt)
          }}
        />
      )}

      {isSearchOpen && (
        <ChatsSearchModal threads={threads} accessToken={accessToken} onClose={handleCloseSearch} />
      )}

      <main className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={{ accessToken, onLoggedOut, me, creditsBalance, onThreadCreated: handleThreadCreated, onRunStarted: handleRunStarted, onRunEnded: handleRunEnded, onThreadTitleUpdated: handleThreadTitleUpdated, refreshCredits, onOpenNotifications: openNotifications, notificationVersion, isPrivateMode, onTogglePrivateMode: handleTogglePrivateMode, privateThreadIds, isSearchMode, onEnterSearchMode: () => { window.history.pushState({ searchMode: true }, '', '/'); setIsSearchMode(true) }, onExitSearchMode: () => setIsSearchMode(false), onSetPendingIncognito: handleSetPendingIncognito, onRightPanelChange: setRightPanelOpen, threads, onThreadDeleted: handleThreadDeleted, pendingSkillPrompt, onConsumeSkillPrompt: () => setPendingSkillPrompt(null) }} />
        {notificationsOpen && (
          <NotificationsPanel accessToken={accessToken} onClose={closeNotifications} onMarkedRead={handleNotificationMarkedRead} />
        )}
      </main>
    </div>
  )
}
