import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { isDesktop, getDesktopApi } from '@arkloop/shared/desktop'
import { LoadingPage, TimeZoneProvider } from '@arkloop/shared'
import { Sidebar } from '../components/Sidebar'
import { DesktopTitleBar } from '../components/DesktopTitleBar'
import { DesktopTabBar } from '../components/DesktopTabBar'
import { BrowserTabPage } from '../components/BrowserTabPage'
import { SettingsModal, type SettingsTab } from '../components/SettingsModal'
import { DesktopSettings } from '../components/DesktopSettings'
import { ChatsSearchModal } from '../components/ChatsSearchModal'
import { NotificationsPanel } from '../components/NotificationsPanel'
import { EmailVerificationGate } from '../components/EmailVerificationGate'
import { useLocale } from '../contexts/LocaleContext'
import { getMe } from '../api'
import { writeActiveThreadIdToStorage, writeSelectedPersonaKeyToStorage, DEFAULT_PERSONA_KEY } from '../storage'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import {
  useAppModeUI,
  useNotificationsUI,
  useSearchUI,
  useSettingsUI,
  useSidebarUI,
  useSkillPromptUI,
  useTitleBarIncognitoUI,
} from '../contexts/app-ui'
import { useCredits } from '../contexts/credits'
import { useBrowserTabs } from '../contexts/browser-tabs'
import { isPerfDebugEnabled, recordPerfValue } from '../perfDebug'

const MainViewport = memo(function MainViewport({
  accessToken,
  notificationsOpen,
  closeNotifications,
  markNotificationRead,
}: {
  accessToken: string
  notificationsOpen: boolean
  closeNotifications: () => void
  markNotificationRead: () => void
}) {
  useEffect(() => {
    if (!isPerfDebugEnabled()) return
    recordPerfValue('layout_main_viewport_render_count', 1, 'count', {
      notificationsOpen,
    })
  })

  return (
    <main className="relative flex min-w-0 flex-1 flex-col overflow-y-auto" style={{ scrollbarGutter: 'stable' }}>
      <Outlet />
      {notificationsOpen && (
        <NotificationsPanel accessToken={accessToken} onClose={closeNotifications} onMarkedRead={markNotificationRead} />
      )}
    </main>
  )
})

type LayoutMainProps = {
  desktop: boolean
  isSearchOpen: boolean
  filteredThreads: import('../api').ThreadResponse[]
  appMode: import('../storage').AppMode
  availableModes: import('../storage').AppMode[]
  pathname: string
  onSearchClose: () => void
  onMeUpdated: (m: import('../api').MeResponse) => void
  onTrySkill: (prompt: string) => void
  onSetAppMode: (mode: import('../storage').AppMode) => void
  browserPanelOpen: boolean
  onToggleBrowserPanel: () => void
  browserFullscreen: boolean
  onToggleBrowserFullscreen: () => void
}

const LayoutMain = memo(function LayoutMain({
  desktop,
  isSearchOpen,
  filteredThreads,
  appMode,
  availableModes,
  pathname,
  onSearchClose,
  onMeUpdated,
  onTrySkill,
  onSetAppMode,
  browserPanelOpen,
  onToggleBrowserPanel,
  browserFullscreen,
  onToggleBrowserFullscreen,
}: LayoutMainProps) {
  const { me, accessToken, logout } = useAuth()
  const { setCreditsBalance } = useCredits()
  const {
    settingsOpen,
    settingsInitialTab,
    desktopSettingsSection,
    desktopAdvancedSection,
    desktopSettingsRequestId,
    closeSettings,
  } = useSettingsUI()
  const { notificationsOpen, closeNotifications, markNotificationRead } = useNotificationsUI()
  const [chatRatio, setChatRatio] = useState(50)
  const isDragging = useRef(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isPerfDebugEnabled()) return
    recordPerfValue('layout_main_render_count', 1, 'count', {
      desktop,
      isSearchOpen,
      settingsOpen,
      notificationsOpen,
      filteredThreadCount: filteredThreads.length,
      pathname,
    })
  })

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    isDragging.current = true
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'

    const handleMouseMove = (e: MouseEvent) => {
      if (!isDragging.current || !containerRef.current) return
      const rect = containerRef.current.getBoundingClientRect()
      const x = e.clientX - rect.left
      const ratio = Math.max(20, Math.min(80, (x / rect.width) * 100))
      setChatRatio(ratio)
    }

    const handleMouseUp = () => {
      isDragging.current = false
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
  }, [])

  return (
    <>
      {settingsOpen && !desktop && (
        <SettingsModal
          me={me}
          accessToken={accessToken}
          initialTab={settingsInitialTab}
          onClose={closeSettings}
          onLogout={logout}
          onCreditsChanged={setCreditsBalance}
          onMeUpdated={onMeUpdated}
          onTrySkill={onTrySkill}
        />
      )}

      {isSearchOpen && (
        <ChatsSearchModal threads={filteredThreads} mode={appMode} accessToken={accessToken} onClose={onSearchClose} />
      )}

      {desktop && settingsOpen ? (
        <DesktopSettings
          me={me}
          accessToken={accessToken}
          initialSection={desktopSettingsSection}
          initialAdvancedKey={desktopAdvancedSection}
          sectionRequestId={desktopSettingsRequestId}
          onClose={closeSettings}
          onLogout={logout}
          onMeUpdated={onMeUpdated}
          onTrySkill={onTrySkill}
        />
      ) : (
        <div ref={containerRef} className="relative flex min-w-0 flex-1 overflow-hidden">
          <div className="flex min-w-0 flex-1 overflow-hidden">
            {!browserFullscreen && (
              <div className="flex min-w-0 flex-col overflow-hidden" style={{ flex: browserPanelOpen ? `0 0 ${chatRatio}%` : '1 1 100%' }}>
              {desktop && (
                <DesktopTabBar
                  appMode={appMode}
                  availableModes={availableModes}
                  browserPanelOpen={browserPanelOpen}
                  onSetAppMode={onSetAppMode}
                  onToggleBrowserPanel={onToggleBrowserPanel}
                />
              )}
              <MainViewport
                accessToken={accessToken}
                notificationsOpen={notificationsOpen}
                closeNotifications={closeNotifications}
                markNotificationRead={markNotificationRead}
              />
              </div>
            )}
            {desktop && browserPanelOpen && !browserFullscreen && (
              <div
                className="w-1 shrink-0 cursor-col-resize bg-transparent hover:bg-[var(--c-border-subtle)] transition-colors"
                onMouseDown={handleMouseDown}
              />
            )}
            {desktop && browserPanelOpen && (
              <aside
                className="flex min-h-0 min-w-0 shrink-0 border-l border-[var(--c-border-subtle)] bg-[var(--c-bg-page)]"
                style={{ flex: browserFullscreen ? '1 1 100%' : `0 0 ${100 - chatRatio}%` }}
              >
                <BrowserTabPage
                  browserFullscreen={browserFullscreen}
                  onToggleBrowserFullscreen={onToggleBrowserFullscreen}
                />
              </aside>
            )}
          </div>
        </div>
      )}
    </>
  )
})

export function AppLayout() {
  const { me, meLoaded, accessToken, logout, updateMe } = useAuth()
  const {
    threads,
    isPrivateMode, pendingIncognitoMode,
    privateThreadIds, removeThread,
    togglePrivateMode,
    getFilteredThreads,
  } = useThreadList()
  const { sidebarCollapsed, sidebarHiddenByWidth, toggleSidebar } = useSidebarUI()
  const { isSearchMode, searchOverlayOpen, exitSearchMode, closeSearchOverlay } = useSearchUI()
  const { appMode, availableAppModes, setAppMode } = useAppModeUI()
  const { openSettings, closeSettings } = useSettingsUI()
  const { closeNotifications } = useNotificationsUI()
  const { queueSkillPrompt } = useSkillPromptUI()
  const { triggerTitleBarIncognitoClick } = useTitleBarIncognitoUI()
  const { panelOpen: browserPanelOpen, toggleBrowserPanel } = useBrowserTabs()
  useCredits()
  const { t } = useLocale()
  const navigate = useNavigate()
  const location = useLocation()
  const desktop = isDesktop()

  const [appUpdateState, setAppUpdateState] = useState<import('@arkloop/shared/desktop').AppUpdaterState | null>(null)
  const [productUpdateNotifications, setProductUpdateNotifications] = useState(true)
  const [browserFullscreen, setBrowserFullscreen] = useState(false)

  const handleToggleBrowserFullscreen = useCallback(() => {
    setBrowserFullscreen(prev => !prev)
  }, [])

  // app updater
  useEffect(() => {
    if (!desktop) return
    const api = getDesktopApi()
    if (!api?.appUpdater) return
    void api.appUpdater.getState().then(setAppUpdateState).catch(() => {})
    return api.appUpdater.onState(setAppUpdateState)
  }, [desktop])

  useEffect(() => {
    if (!desktop) return
    const api = getDesktopApi()
    if (!api?.config) return
    void api.config.get()
      .then((config) => setProductUpdateNotifications(config.desktop?.productUpdateNotifications ?? true))
      .catch(() => {})
    return api.config.onChanged((config) => {
      setProductUpdateNotifications(config.desktop?.productUpdateNotifications ?? true)
    })
  }, [desktop])

  const handleCheckAppUpdate = useCallback(() => {
    const api = getDesktopApi()
    void api?.appUpdater?.check().then(setAppUpdateState).catch(() => {})
  }, [])

  const handleDownloadApp = useCallback(() => {
    const api = getDesktopApi()
    void api?.appUpdater?.download().then(setAppUpdateState).catch(() => {})
  }, [])

  const handleInstallApp = useCallback(() => {
    const api = getDesktopApi()
    void api?.appUpdater?.install().catch(() => {})
  }, [])

  const handleTitleBarOpenSettings = useCallback((tab?: SettingsTab | 'voice') => {
    openSettings(tab)
  }, [openSettings])

  const pathnameSearchOpen = location.pathname.endsWith('/search')
  const isSearchOpen = searchOverlayOpen || pathnameSearchOpen
  const currentThreadId = location.pathname.match(/^\/t\/([^/]+)/)?.[1] ?? null

  useEffect(() => {
    if (!currentThreadId) return
    writeActiveThreadIdToStorage(currentThreadId)
  }, [currentThreadId])

  const currentThread = useMemo(
    () => currentThreadId ? threads.find((thread) => thread.id === currentThreadId) ?? null : null,
    [currentThreadId, threads],
  )
  const activeAppMode = currentThread?.mode === 'work' ? 'work' : currentThread?.mode === 'chat' ? 'chat' : appMode
  const filteredThreads = useMemo(() => getFilteredThreads(activeAppMode), [getFilteredThreads, activeAppMode])

  const handleDesktopTitleBarIncognitoClick = useCallback(() => {
    triggerTitleBarIncognitoClick(togglePrivateMode)
  }, [triggerTitleBarIncognitoClick, togglePrivateMode])

  const handleTitleBarSetAppMode = useCallback((mode: import('../storage').AppMode) => {
    setAppMode(mode)
  }, [setAppMode])

  const handleNewThread = useCallback(() => {
    if (isSearchMode) writeSelectedPersonaKeyToStorage(DEFAULT_PERSONA_KEY)
    exitSearchMode()
    closeNotifications()
    if (desktop) closeSettings()
    navigate('/')
  }, [isSearchMode, exitSearchMode, closeNotifications, desktop, closeSettings, navigate])

  const handleCloseSearch = useCallback(() => {
    closeSearchOverlay()
    if (!location.pathname.endsWith('/search')) return
    const basePath = location.pathname.replace(/\/search$/, '') || '/'
    navigate(basePath)
  }, [closeSearchOverlay, location.pathname, navigate])

  const handleTrySkill = useCallback((prompt: string) => {
    closeSettings()
    navigate('/')
    queueSkillPrompt(prompt)
  }, [closeSettings, navigate, queueSkillPrompt])

  const handleThreadDeleted = useCallback((deletedId: string) => {
    removeThread(deletedId)
    if (location.pathname === `/t/${deletedId}` || location.pathname.startsWith(`/t/${deletedId}/`)) {
      navigate('/')
    }
  }, [removeThread, location.pathname, navigate])

  const handleBeforeNavigateToThread = useCallback(() => {
    closeSettings()
  }, [closeSettings])

  if (!meLoaded) return <LoadingPage label={t.loading} />

  if (me !== null && !me.email_verified && me.email_verification_required && me.email) {
    return (
      <EmailVerificationGate
        accessToken={accessToken}
        email={me.email}
        onVerified={() => { getMe(accessToken).then(updateMe).catch(() => {}) }}
        onPollVerified={() => { getMe(accessToken).then(updateMe).catch(() => {}) }}
        onLogout={logout}
      />
    )
  }

  const titleBarIncognitoActive =
    isPrivateMode || pendingIncognitoMode ||
    (currentThreadId != null && privateThreadIds.has(currentThreadId))
  const hasAppUpdate =
    productUpdateNotifications &&
    (appUpdateState?.phase === 'available' ||
      appUpdateState?.phase === 'downloaded')

  return (
    <TimeZoneProvider userTimeZone={me?.timezone ?? null} accountTimeZone={me?.account_timezone ?? null}>
      <div className="theme-background-root app-viewport flex flex-col overflow-hidden bg-[var(--c-bg-page)]">
        <div className="theme-background-layer" aria-hidden="true" />
        {desktop && (
          <DesktopTitleBar
            sidebarCollapsed={sidebarCollapsed}
            onToggleSidebar={() => toggleSidebar('titlebar')}
            appMode={activeAppMode}
            showIncognitoToggle={activeAppMode !== 'work'}
            isPrivateMode={titleBarIncognitoActive}
            onTogglePrivateMode={handleDesktopTitleBarIncognitoClick}
            hasAppUpdate={hasAppUpdate}
            onCheckAppUpdate={handleCheckAppUpdate}
            appUpdateState={appUpdateState}
            onDownloadApp={handleDownloadApp}
            onInstallApp={handleInstallApp}
            onOpenSettings={handleTitleBarOpenSettings}
          />
        )}

        <div className="flex min-h-0 flex-1">
          {!sidebarHiddenByWidth && (
            <Sidebar
              threads={filteredThreads}
              onNewThread={handleNewThread}
              onThreadDeleted={handleThreadDeleted}
              beforeNavigateToThread={handleBeforeNavigateToThread}
            />
          )}

          <LayoutMain
            desktop={desktop}
            isSearchOpen={isSearchOpen}
            filteredThreads={filteredThreads}
            appMode={activeAppMode}
            availableModes={availableAppModes}
            pathname={location.pathname}
            onSearchClose={handleCloseSearch}
            onMeUpdated={updateMe}
            onTrySkill={handleTrySkill}
            onSetAppMode={handleTitleBarSetAppMode}
            browserPanelOpen={browserPanelOpen}
            onToggleBrowserPanel={toggleBrowserPanel}
            browserFullscreen={browserFullscreen}
            onToggleBrowserFullscreen={handleToggleBrowserFullscreen}
          />
        </div>
      </div>
    </TimeZoneProvider>
  )
}
