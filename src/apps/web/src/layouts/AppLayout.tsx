import { memo, useEffect, useState, useCallback, useRef, useMemo } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { isDesktop, isLocalMode, getDesktopApi } from '@arkloop/shared/desktop'
import { LoadingPage } from '@arkloop/shared'
import { Sidebar } from '../components/Sidebar'
import { DesktopTitleBar } from '../components/DesktopTitleBar'
import { SettingsModal } from '../components/SettingsModal'
import { DesktopSettings } from '../components/DesktopSettings'
import type { DesktopSettingsKey } from '../components/DesktopSettings'
import { ChatsSearchModal } from '../components/ChatsSearchModal'
import { NotificationsPanel } from '../components/NotificationsPanel'
import { EmailVerificationGate } from '../components/EmailVerificationGate'
import { useLocale } from '../contexts/LocaleContext'
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
import { clearActiveThreadIdInStorage, writeSelectedPersonaKeyToStorage, DEFAULT_PERSONA_KEY, readAppModeFromStorage, writeAppModeToStorage, writeThreadMode, readThreadMode } from '../storage'
import type { AppMode } from '../storage'
import { beginPerfTrace, endPerfTrace, recordPerfDuration } from '../perfDebug'

type Props = {
  accessToken: string
  onLoggedOut: () => void
}

type LayoutMainProps = {
  desktop: boolean
  isSearchOpen: boolean
  settingsOpen: boolean
  me: MeResponse | null
  accessToken: string
  settingsInitialTab: SettingsTab
  desktopSettingsSection: DesktopSettingsKey
  onCloseSettings: () => void
  onLogout: () => void
  onCreditsChanged: (balance: number) => void
  onMeUpdated: (updated: MeResponse) => void
  onTrySkill: (prompt: string) => void
  outletContext: unknown
  notificationsOpen: boolean
  onCloseNotifications: () => void
  onMarkedRead: () => void
  filteredThreads: ThreadResponse[]
  onSearchClose: () => void
}

const LayoutMain = memo(function LayoutMain({
  desktop,
  isSearchOpen,
  settingsOpen,
  me,
  accessToken,
  settingsInitialTab,
  desktopSettingsSection,
  onCloseSettings,
  onLogout,
  onCreditsChanged,
  onMeUpdated,
  onTrySkill,
  outletContext,
  notificationsOpen,
  onCloseNotifications,
  onMarkedRead,
  filteredThreads,
  onSearchClose,
}: LayoutMainProps) {
  return (
    <>
      {settingsOpen && !desktop && (
        <SettingsModal
          me={me}
          accessToken={accessToken}
          initialTab={settingsInitialTab}
          onClose={onCloseSettings}
          onLogout={onLogout}
          onCreditsChanged={onCreditsChanged}
          onMeUpdated={onMeUpdated}
          onTrySkill={onTrySkill}
        />
      )}

      {isSearchOpen && (
        <ChatsSearchModal threads={filteredThreads} accessToken={accessToken} onClose={onSearchClose} />
      )}

      {desktop && settingsOpen ? (
        <DesktopSettings
          me={me}
          accessToken={accessToken}
          initialSection={desktopSettingsSection}
          onClose={onCloseSettings}
          onLogout={onLogout}
          onMeUpdated={onMeUpdated}
          onTrySkill={onTrySkill}
        />
      ) : (
        <main className="relative flex min-w-0 flex-1 flex-col overflow-y-auto" style={{ scrollbarGutter: 'stable' }}>
          <Outlet context={outletContext} />
          {notificationsOpen && (
            <NotificationsPanel accessToken={accessToken} onClose={onCloseNotifications} onMarkedRead={onMarkedRead} />
          )}
        </main>
      )}
    </>
  )
})

export function AppLayout({ accessToken, onLoggedOut }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useLocale()

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
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => window.innerWidth < 1200)
  const [sidebarHiddenByWidth, setSidebarHiddenByWidth] = useState(() => window.innerWidth < 900)
  const collapsedByWidthRef = useRef(window.innerWidth < 1200)
  const [rightPanelOpen, setRightPanelOpen] = useState(false)
  const [isSearchMode, setIsSearchMode] = useState(false)
  // ref 用于在 popstate 回调里读取最新值，避免闭包过期
  const isSearchModeRef = useRef(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsInitialTab, setSettingsInitialTab] = useState<SettingsTab>('account')
  const [desktopSettingsSection, setDesktopSettingsSection] = useState<DesktopSettingsKey>('general')
  const [notificationsOpen, setNotificationsOpen] = useState(
    () => new URLSearchParams(location.search).has('notices'),
  )
  const [notificationVersion, setNotificationVersion] = useState(0)
  const [creditsBalance, setCreditsBalance] = useState(0)
  const [pendingSkillPrompt, setPendingSkillPrompt] = useState<string | null>(null)
  const [appMode, setAppMode] = useState<AppMode>(readAppModeFromStorage)
  const desktop = isDesktop()
  const usesHashRouting = window.location.protocol === 'file:'
  const settingsOpenTraceRef = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const filteredThreads = useMemo(
    () => threads.filter((t) => readThreadMode(t.id) === appMode),
    [threads, appMode],
  )

  const replaceQueryState = useCallback((params: URLSearchParams) => {
    const qs = params.toString()
    const basePath = window.location.pathname
    const hash = usesHashRouting ? window.location.hash : ''
    const next = `${basePath}${qs ? `?${qs}` : ''}${hash}`
    window.history.replaceState(window.history.state, '', next)
  }, [usesHashRouting])

  const pushSearchModeState = useCallback(() => {
    const basePath = window.location.pathname
    const next = usesHashRouting ? `${basePath}${window.location.search}${window.location.hash}` : '/'
    window.history.pushState({ searchMode: true }, '', next)
  }, [usesHashRouting])

  const availableAppModes: AppMode[] = (desktop || me?.work_enabled !== false) ? ['chat', 'work'] : ['chat']

  const handleSetAppMode = useCallback((mode: AppMode) => {
    writeAppModeToStorage(mode)
    setAppMode(mode)
    // 切换模式时，如果当前在某个会话内，跳回新建对话页
    if (/^\/t\//.test(location.pathname)) {
      navigate('/')
    }
  }, [location.pathname, navigate])

  useEffect(() => {
    let raf = 0
    const handler = () => {
      cancelAnimationFrame(raf)
      raf = requestAnimationFrame(() => {
        const w = window.innerWidth
        const hidden = w < 900
        setSidebarHiddenByWidth((prev) => (prev === hidden ? prev : hidden))
        const narrow = w < 1200
        if (narrow && !collapsedByWidthRef.current) {
          collapsedByWidthRef.current = true
          setSidebarCollapsed(true)
        } else if (!narrow && collapsedByWidthRef.current) {
          collapsedByWidthRef.current = false
          setSidebarCollapsed(false)
        }
      })
    }
    window.addEventListener('resize', handler)
    return () => {
      window.removeEventListener('resize', handler)
      cancelAnimationFrame(raf)
    }
  }, [])

  const handleNotificationMarkedRead = useCallback(() => {
    setNotificationVersion((v) => v + 1)
  }, [])

  const openNotifications = useCallback(() => {
    setNotificationsOpen(true)
    const params = new URLSearchParams(window.location.search)
    if (!params.has('notices')) {
      params.set('notices', '')
      replaceQueryState(params)
    }
  }, [replaceQueryState])

  const closeNotifications = useCallback(() => {
    setNotificationsOpen(false)
    const params = new URLSearchParams(window.location.search)
    if (params.has('notices')) {
      params.delete('notices')
      replaceQueryState(params)
    }
  }, [replaceQueryState])
  const mountedRef = useRef(true)

  // 同步 ref，使 popstate 回调始终拿到最新值
  useEffect(() => { isSearchModeRef.current = isSearchMode }, [isSearchMode])

  // 离开 / 时退出搜索模式（覆盖 popstate 和 navigate 两种场景）
  useEffect(() => {
    if (location.pathname === '/') return
    const id = requestAnimationFrame(() => setIsSearchMode(false))
    return () => cancelAnimationFrame(id)
  }, [location.pathname])

  // 路由切换时重置右侧面板状态，避免 sidebar 宽度残留
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      setRightPanelOpen(false)
      if (notificationsOpen) closeNotifications()
    })
    return () => cancelAnimationFrame(id)
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // Desktop 模式下，点击历史记录跳转到会话时关闭设置界面
  useEffect(() => {
    if (!(desktop && settingsOpen && /^\/t\//.test(location.pathname))) return
    const id = requestAnimationFrame(() => {
      setSettingsOpen(false)
    })
    return () => cancelAnimationFrame(id)
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!desktop) return
    const openDesktopSettings = () => {
      settingsOpenTraceRef.current = beginPerfTrace('desktop_settings_open', {
        source: 'window-event',
        requestedSection: 'general',
        pathname: location.pathname,
        threadCount: threads.length,
      })
      setDesktopSettingsSection('general')
      setSettingsOpen(true)
    }
    window.addEventListener('arkloop:app:open-settings', openDesktopSettings as EventListener)
    return () => {
      window.removeEventListener('arkloop:app:open-settings', openDesktopSettings as EventListener)
    }
  }, [desktop, location.pathname, threads.length])

  useEffect(() => {
    if (!(desktop && settingsOpen)) return
    endPerfTrace(settingsOpenTraceRef.current, {
      phase: 'visible',
      section: desktopSettingsSection,
      pathname: location.pathname,
      threadCount: threads.length,
    })
    settingsOpenTraceRef.current = null
  }, [desktop, settingsOpen, desktopSettingsSection, location.pathname, threads.length])

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
        // local 模式没有 user_credentials，用 OS 用户名填充
        let resolvedMe = meResp
        if (isLocalMode() && !meResp.username) {
          try {
            const fn = getDesktopApi()?.app.getOsUsername
            const osName = fn ? await fn() : ''
            if (osName) resolvedMe = { ...meResp, username: osName }
          } catch {
            // ignore，降级无名字问候
          }
        }
        setMe(resolvedMe)
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
    } catch {
      // Best-effort server revocation — always proceed with local cleanup
    }
    clearActiveThreadIdInStorage()
    onLoggedOut()
  }, [accessToken, onLoggedOut])

  const handleNewThread = useCallback(() => {
    // Leaving search/retrieval mode — reset persona back to default
    if (isSearchModeRef.current) {
      writeSelectedPersonaKeyToStorage(DEFAULT_PERSONA_KEY)
    }
    setIsSearchMode(false)
    closeNotifications()
    // In desktop mode, close settings so the chat view becomes visible
    if (desktop) {
      setSettingsOpen(false)
    }
    navigate('/')
  }, [navigate, closeNotifications, desktop])

  // 从 WelcomePage 新建的 thread 需要注入到列表，并标记所属模式
  const handleThreadCreated = useCallback((thread: ThreadResponse) => {
    if (thread.is_private) {
      setPrivateThreadIds((prev) => new Set(prev).add(thread.id))
      return
    }
    writeThreadMode(thread.id, appMode)
    setThreads((prev) => {
      if (prev.some((t) => t.id === thread.id)) return prev
      return [thread, ...prev]
    })
  }, [appMode])

  const handleTogglePrivateMode = useCallback(() => {
    setIsPrivateMode((prev) => !prev)
  }, [])

  const handleSetPendingIncognito = useCallback((v: boolean) => {
    setPendingIncognitoMode(v)
  }, [])

  const titleBarIncognitoRef = useRef<(() => void) | null>(null)
  const setTitleBarIncognitoClick = useCallback((fn: (() => void) | null) => {
    titleBarIncognitoRef.current = fn
  }, [])

  const handleDesktopTitleBarIncognitoClick = useCallback(() => {
    const fn = titleBarIncognitoRef.current
    if (fn) fn()
    else handleTogglePrivateMode()
  }, [handleTogglePrivateMode])

  const handleRunStarted = useCallback((threadId: string) => {
    setRunningThreadIds((prev) => new Set(prev).add(threadId))
    setThreads((prev) => {
      const idx = prev.findIndex((t) => t.id === threadId)
      if (idx <= 0) return prev
      const thread = prev[idx]
      return [thread, ...prev.slice(0, idx), ...prev.slice(idx + 1)]
    })
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

  const handleSettingsClose = useCallback(() => {
    setSettingsOpen(false)
  }, [])

  const handleCreditsChanged = useCallback((balance: number) => {
    setCreditsBalance(balance)
  }, [])

  const handleMeUpdated = useCallback((updated: MeResponse) => {
    setMe(updated)
  }, [])

  const handleTrySkill = useCallback((prompt: string) => {
    setSettingsOpen(false)
    navigate('/')
    setPendingSkillPrompt(prompt)
  }, [navigate])

  const handleBeforeNavigateToThread = useCallback(() => {
    setSettingsOpen(false)
  }, [])

  const handleOpenSettings = useCallback((tab: SettingsTab | 'voice' = 'account') => {
    if (desktop) {
      const keyMap: Record<string, DesktopSettingsKey> = {
        account: 'general',
        settings: 'general',
        skills: 'skills',
        models: 'providers',
        agents: 'personas',
        channels: 'channels',
        connection: 'advanced',
        voice: 'advanced',
      }
      const section = keyMap[tab] ?? 'general'
      recordPerfDuration('desktop_settings_open_request', 0, {
        source: 'sidebar',
        requestedTab: tab,
        section,
        pathname: location.pathname,
        threadCount: threads.length,
        alreadyOpen: settingsOpen,
      })
      settingsOpenTraceRef.current = beginPerfTrace('desktop_settings_open', {
        source: 'sidebar',
        requestedTab: tab,
        section,
        pathname: location.pathname,
        threadCount: threads.length,
      })
      setDesktopSettingsSection(section)
      setSettingsOpen(true)
      return
    }
    setSettingsInitialTab(tab as SettingsTab)
    setSettingsOpen(true)
  }, [desktop, location.pathname, settingsOpen, threads.length])

  const handleEnterSearchMode = useCallback(() => {
    pushSearchModeState()
    setIsSearchMode(true)
  }, [pushSearchModeState])

  const handleConsumePendingSkillPrompt = useCallback(() => {
    setPendingSkillPrompt(null)
  }, [])

  const outletContext = useMemo(() => ({
    accessToken,
    onLoggedOut,
    me,
    creditsBalance,
    onThreadCreated: handleThreadCreated,
    onRunStarted: handleRunStarted,
    onRunEnded: handleRunEnded,
    onThreadTitleUpdated: handleThreadTitleUpdated,
    refreshCredits,
    onOpenNotifications: openNotifications,
    notificationVersion,
    isPrivateMode,
    onTogglePrivateMode: handleTogglePrivateMode,
    privateThreadIds,
    isSearchMode,
    onEnterSearchMode: handleEnterSearchMode,
    onExitSearchMode: () => setIsSearchMode(false),
    onSetPendingIncognito: handleSetPendingIncognito,
    setTitleBarIncognitoClick,
    onRightPanelChange: setRightPanelOpen,
    threads,
    onThreadDeleted: handleThreadDeleted,
    pendingSkillPrompt,
    onConsumeSkillPrompt: handleConsumePendingSkillPrompt,
    onOpenSettings: handleOpenSettings,
    appMode,
    availableAppModes,
    onSetAppMode: handleSetAppMode,
  }), [
    accessToken,
    onLoggedOut,
    me,
    creditsBalance,
    handleThreadCreated,
    handleRunStarted,
    handleRunEnded,
    handleThreadTitleUpdated,
    refreshCredits,
    openNotifications,
    notificationVersion,
    isPrivateMode,
    handleTogglePrivateMode,
    privateThreadIds,
    isSearchMode,
    handleEnterSearchMode,
    handleSetPendingIncognito,
    setTitleBarIncognitoClick,
    threads,
    handleThreadDeleted,
    pendingSkillPrompt,
    handleConsumePendingSkillPrompt,
    handleOpenSettings,
    appMode,
    availableAppModes,
    handleSetAppMode,
  ])

  // email 验证门控：flag 开启 + 未验证时全屏拦截
  if (!meLoaded) {
    return <LoadingPage label={t.loading} />
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

  const currentThreadId = location.pathname.match(/^\/t\/([^/]+)/)?.[1] ?? null
  const titleBarIncognitoActive =
    isPrivateMode ||
    pendingIncognitoMode ||
    (currentThreadId != null && privateThreadIds.has(currentThreadId))

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-[var(--c-bg-page)]">
      {desktop && (
        <DesktopTitleBar
          sidebarCollapsed={sidebarCollapsed}
          onToggleSidebar={() => setSidebarCollapsed((v) => !v)}
          appMode={appMode}
          onSetAppMode={handleSetAppMode}
          availableModes={availableAppModes}
          showIncognitoToggle={appMode !== 'work'}
          isPrivateMode={titleBarIncognitoActive}
          onTogglePrivateMode={handleDesktopTitleBarIncognitoClick}
        />
      )}

      <div className="flex min-h-0 flex-1">
        {!sidebarHiddenByWidth && <Sidebar
          me={me}
          threads={filteredThreads}
          runningThreadIds={runningThreadIds}
          // 侧栏「小黑屋」仅跟全局开关与 pending fork；当前是否 private thread 不改变侧栏可导航性
          isPrivateMode={isPrivateMode || pendingIncognitoMode}
          accessToken={accessToken}
          onNewThread={handleNewThread}
          onLogout={handleLogout}
          onOpenSettings={handleOpenSettings}
          collapsed={sidebarCollapsed}
          onToggleCollapse={() => {
            collapsedByWidthRef.current = !sidebarCollapsed
            setSidebarCollapsed(v => !v)
          }}
          onThreadTitleUpdated={handleThreadTitleUpdated}
          onThreadDeleted={handleThreadDeleted}
          narrow={rightPanelOpen}
          desktopMode={desktop}
          appMode={appMode}
          suppressActiveThreadHighlight={settingsOpen}
          beforeNavigateToThread={handleBeforeNavigateToThread}
        />}

        <LayoutMain
          desktop={desktop}
          isSearchOpen={isSearchOpen}
          settingsOpen={settingsOpen}
          me={me}
          accessToken={accessToken}
          settingsInitialTab={settingsInitialTab}
          desktopSettingsSection={desktopSettingsSection}
          onCloseSettings={handleSettingsClose}
          onLogout={handleLogout}
          onCreditsChanged={handleCreditsChanged}
          onMeUpdated={handleMeUpdated}
          onTrySkill={handleTrySkill}
          outletContext={outletContext}
          notificationsOpen={notificationsOpen}
          onCloseNotifications={closeNotifications}
          onMarkedRead={handleNotificationMarkedRead}
          filteredThreads={filteredThreads}
          onSearchClose={handleCloseSearch}
        />
      </div>
    </div>
  )
}
