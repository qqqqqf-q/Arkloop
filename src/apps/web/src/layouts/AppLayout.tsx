import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { isDesktop, getDesktopApi } from '@arkloop/shared/desktop'
import { LoadingPage, TimeZoneProvider } from '@arkloop/shared'
import { BrowserTabPage } from '../components/BrowserTabPage'
import { Sidebar } from '../components/Sidebar'
import { DesktopTitleBar } from '../components/DesktopTitleBar'
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
  pathname: string
  onSearchClose: () => void
  onMeUpdated: (m: import('../api').MeResponse) => void
  onTrySkill: (prompt: string) => void
  browserPanelOpen: boolean
  browserFullscreen: boolean
  chatRatio: number
  containerRef: React.RefObject<HTMLDivElement | null>
  onResizeStart: (event: React.MouseEvent) => void
  onCloseBrowserPanel: () => void
  onToggleBrowserFullscreen: () => void
}

const LayoutMain = memo(function LayoutMain({
  desktop,
  isSearchOpen,
  filteredThreads,
  appMode,
  pathname,
  onSearchClose,
  onMeUpdated,
  onTrySkill,
  browserPanelOpen,
  browserFullscreen,
  chatRatio,
  containerRef,
  onResizeStart,
  onCloseBrowserPanel,
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
        <div
          ref={containerRef}
          className="relative flex min-w-0 flex-1 overflow-hidden"
          style={{ borderLeft: '0.5px solid var(--c-border-subtle)' }}
        >
          {!browserFullscreen && (
            <div
              className="flex min-w-0 flex-1 flex-col overflow-hidden"
              style={{ flex: browserPanelOpen ? `0 0 ${chatRatio}%` : '1 1 100%' }}
            >
              <MainViewport
                accessToken={accessToken}
                notificationsOpen={notificationsOpen}
                closeNotifications={closeNotifications}
                markNotificationRead={markNotificationRead}
              />
            </div>
          )}

          {desktop && browserPanelOpen && (
            <aside
              className="relative flex min-h-0 min-w-0 shrink-0 bg-(--c-bg-page)"
              style={{ flex: browserFullscreen ? '1 1 100%' : `0 0 ${100 - chatRatio}%` }}
            >
              {!browserFullscreen && (
                <div
                  className="absolute inset-y-0 left-0 cursor-col-resize"
                  style={{ width: '12px', marginLeft: '-6px', zIndex: 10 }}
                  onMouseDown={onResizeStart}
                >
                  <div
                    className="absolute inset-y-0 right-0 w-[3px] bg-transparent transition-colors hover:bg-(--c-border-subtle)"
                    style={{ right: '3px' }}
                  />
                </div>
              )}
              <div
                className="min-w-0 flex-1"
                style={{ borderLeft: browserFullscreen ? 'none' : '0.5px solid var(--c-border-subtle)' }}
              >
                <BrowserTabPage
                  browserFullscreen={browserFullscreen}
                  forcePanelOpen={browserPanelOpen}
                  onClosePanel={onCloseBrowserPanel}
                  onToggleBrowserFullscreen={onToggleBrowserFullscreen}
                />
              </div>
            </aside>
          )}
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
  useCredits()
  const { t } = useLocale()
  const navigate = useNavigate()
  const location = useLocation()
  const desktop = isDesktop()
  const { panelOpen: browserPanelOpen, toggleBrowserPanel, closeBrowserPanel } = useBrowserTabs()

  const [appUpdateState, setAppUpdateState] = useState<import('@arkloop/shared/desktop').AppUpdaterState | null>(null)
  const [productUpdateNotifications, setProductUpdateNotifications] = useState(true)
  const [browserFullscreen, setBrowserFullscreen] = useState(false)
  const [chatRatio, setChatRatio] = useState(50)
  const containerRef = useRef<HTMLDivElement>(null)
  const isDragging = useRef(false)

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

  const handleToggleBrowserFullscreen = useCallback(() => {
    setBrowserFullscreen((prev) => !prev)
  }, [])

  const handleMouseDown = useCallback((event: React.MouseEvent) => {
    event.preventDefault()
    isDragging.current = true
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'

    const handleMouseMove = (moveEvent: MouseEvent) => {
      if (!isDragging.current || !containerRef.current) return
      const rect = containerRef.current.getBoundingClientRect()
      const x = moveEvent.clientX - rect.left
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

  const handleCloseBrowserPanel = useCallback(() => {
    setBrowserFullscreen(false)
    closeBrowserPanel()
  }, [closeBrowserPanel])

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
      <div className="theme-background-root app-viewport flex flex-col overflow-hidden bg-(--c-bg-page)">
        <div className="theme-background-layer" aria-hidden="true" />
        {desktop && (
          <DesktopTitleBar
            sidebarCollapsed={sidebarCollapsed}
            onToggleSidebar={() => toggleSidebar('titlebar')}
            appMode={activeAppMode}
            onSetAppMode={setAppMode}
            availableModes={availableAppModes}
            showIncognitoToggle={activeAppMode !== 'work'}
            isPrivateMode={titleBarIncognitoActive}
            onTogglePrivateMode={handleDesktopTitleBarIncognitoClick}
            browserPanelOpen={browserPanelOpen}
            onToggleBrowserPanel={() => {
              if (browserPanelOpen) {
                handleCloseBrowserPanel()
                return
              }
              setBrowserFullscreen(false)
              toggleBrowserPanel()
            }}
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
            pathname={location.pathname}
            onSearchClose={handleCloseSearch}
            onMeUpdated={updateMe}
            onTrySkill={handleTrySkill}
            browserPanelOpen={browserPanelOpen}
            browserFullscreen={browserFullscreen}
            chatRatio={chatRatio}
            containerRef={containerRef}
            onResizeStart={handleMouseDown}
            onCloseBrowserPanel={handleCloseBrowserPanel}
            onToggleBrowserFullscreen={handleToggleBrowserFullscreen}
          />
        </div>
      </div>
    </TimeZoneProvider>
  )
}
