import { memo, useCallback, useEffect, useMemo, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Bolt, Clock, Search, SquarePen } from 'lucide-react'
import { isDesktop, getDesktopApi } from '@arkloop/shared/desktop'
import { LoadingPage, TimeZoneProvider } from '@arkloop/shared'
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
  useTitleBarRightPanelUI,
} from '../contexts/app-ui'
import { useCredits } from '../contexts/credits'
import { isPerfDebugEnabled, recordPerfValue } from '../perfDebug'

const SIDEBAR_WIDTH_STORAGE_KEY = 'arkloop:web:sidebar_width'
const SIDEBAR_COLLAPSED_WIDTH = 48
const SIDEBAR_DEFAULT_WIDTH = 284
const SIDEBAR_MIN_WIDTH = 224
const SIDEBAR_MAX_WIDTH = 420
const SIDEBAR_WIDTH_TRANSITION = '280ms cubic-bezier(0.16,1,0.3,1)'

function clampSidebarWidth(width: number): number {
  return Math.min(Math.max(width, SIDEBAR_MIN_WIDTH), SIDEBAR_MAX_WIDTH)
}

function readSidebarWidth(): number {
  try {
    const raw = Number(localStorage.getItem?.(SIDEBAR_WIDTH_STORAGE_KEY))
    return Number.isFinite(raw) ? clampSidebarWidth(raw) : SIDEBAR_DEFAULT_WIDTH
  } catch {
    return SIDEBAR_DEFAULT_WIDTH
  }
}

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

function CollapsedSidebarTransitionOverlay({
  direction,
  onDone,
}: {
  direction: 'enter' | 'exit'
  onDone: () => void
}) {
  return (
    <div
      aria-hidden="true"
      className={[
        direction === 'enter' ? 'collapsed-sidebar-enter-overlay' : 'collapsed-sidebar-exit-overlay',
        'theme-surface-sidebar pointer-events-none absolute inset-y-0 left-0 z-30 flex w-[48px] flex-col bg-[var(--c-bg-sidebar)]',
      ].join(' ')}
      style={{ borderRight: '0.5px solid var(--c-border)' }}
      onAnimationEnd={onDone}
    >
      <div className="h-3 shrink-0" />
      <div className="flex flex-col gap-px px-[8px] pt-1">
        {[SquarePen, Search, Clock].map((Icon, index) => (
          <div key={index} className="flex h-[32px] w-[32px] items-center justify-center rounded-lg text-[var(--c-text-secondary)]">
            <Icon size={16} />
          </div>
        ))}
      </div>
      <div className="mt-auto px-2 pb-2 pt-1">
        <div className="mt-0.5 pl-1">
          <div className="flex h-8 w-8 items-center justify-center rounded-md text-[var(--c-text-icon)]">
            <Bolt size={18} />
          </div>
        </div>
      </div>
    </div>
  )
}

type LayoutMainProps = {
  desktop: boolean
  isSearchOpen: boolean
  filteredThreads: import('../api').ThreadResponse[]
  appMode: import('../storage').AppMode
  pathname: string
  onSearchClose: () => void
  onMeUpdated: (m: import('../api').MeResponse) => void
  onTrySkill: (prompt: string) => void
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
        <div className="relative flex min-w-0 flex-1 overflow-hidden">
          <MainViewport
            accessToken={accessToken}
            notificationsOpen={notificationsOpen}
            closeNotifications={closeNotifications}
            markNotificationRead={markNotificationRead}
          />
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
  const { sidebarCollapsed, sidebarHiddenByWidth, rightPanelOpen, toggleSidebar } = useSidebarUI()
  const { isSearchMode, searchOverlayOpen, exitSearchMode, closeSearchOverlay } = useSearchUI()
  const { appMode, availableAppModes, setAppMode } = useAppModeUI()
  const { openSettings, closeSettings } = useSettingsUI()
  const { closeNotifications } = useNotificationsUI()
  const { queueSkillPrompt } = useSkillPromptUI()
  const { triggerTitleBarIncognitoClick } = useTitleBarIncognitoUI()
  const { triggerTitleBarRightPanelClick } = useTitleBarRightPanelUI()
  useCredits()
  const { t } = useLocale()
  const navigate = useNavigate()
  const location = useLocation()
  const desktop = isDesktop()

  const [appUpdateState, setAppUpdateState] = useState<import('@arkloop/shared/desktop').AppUpdaterState | null>(null)
  const [productUpdateNotifications, setProductUpdateNotifications] = useState(true)
  const [sidebarWidth, setSidebarWidth] = useState(readSidebarWidth)
  const [sidebarResizing, setSidebarResizing] = useState(false)
  const [modeSwitchingCollapsedSidebar, setModeSwitchingCollapsedSidebar] = useState(false)
  const [collapsedSidebarExitVisible, setCollapsedSidebarExitVisible] = useState(false)
  const [collapsedSidebarEnterVisible, setCollapsedSidebarEnterVisible] = useState(false)

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

  const handleSidebarResizeStart = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (sidebarCollapsed) return
    event.preventDefault()
    setSidebarResizing(true)
    const startX = event.clientX
    const startWidth = sidebarWidth
    const pointerId = event.pointerId
    event.currentTarget.setPointerCapture(pointerId)

    const handlePointerMove = (moveEvent: PointerEvent) => {
      const next = clampSidebarWidth(startWidth + moveEvent.clientX - startX)
      setSidebarWidth(next)
    }

    const stopResize = () => {
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('pointerup', stopResize)
      window.removeEventListener('pointercancel', stopResize)
      setSidebarResizing(false)
    }

    window.addEventListener('pointermove', handlePointerMove)
    window.addEventListener('pointerup', stopResize)
    window.addEventListener('pointercancel', stopResize)
  }, [sidebarCollapsed, sidebarWidth])

  useEffect(() => {
    if (!sidebarCollapsed) {
      try {
        localStorage.setItem?.(SIDEBAR_WIDTH_STORAGE_KEY, String(sidebarWidth))
      } catch {
        // ignore unavailable storage
      }
    }
  }, [sidebarCollapsed, sidebarWidth])

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

  const handleSetAppMode = useCallback((mode: import('../storage').AppMode) => {
    if (desktop && sidebarCollapsed && mode !== activeAppMode) {
      if (activeAppMode === 'chat' && mode === 'work') {
        setCollapsedSidebarExitVisible(true)
      } else if (activeAppMode === 'work' && mode === 'chat') {
        setCollapsedSidebarEnterVisible(true)
      }
      setModeSwitchingCollapsedSidebar(true)
      requestAnimationFrame(() => setModeSwitchingCollapsedSidebar(false))
    }
    setAppMode(mode)
  }, [activeAppMode, desktop, setAppMode, sidebarCollapsed])

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
  const hideCollapsedWorkSidebar = desktop && activeAppMode === 'work' && sidebarCollapsed
  const mainContentAxisPaddingLeft = desktop && sidebarCollapsed && activeAppMode === 'work'
    ? `${SIDEBAR_COLLAPSED_WIDTH / 2}px`
    : '0px'
  const mainContentAxisPaddingRight = desktop && sidebarCollapsed && activeAppMode !== 'work'
    ? `${SIDEBAR_COLLAPSED_WIDTH / 2}px`
    : '0px'

  return (
    <TimeZoneProvider userTimeZone={me?.timezone ?? null} accountTimeZone={me?.account_timezone ?? null}>
      <div className="theme-background-root app-viewport flex flex-col overflow-hidden bg-[var(--c-bg-page)]">
        <div className="theme-background-layer" aria-hidden="true" />
        {desktop && (
          <DesktopTitleBar
            sidebarCollapsed={sidebarCollapsed}
            onToggleSidebar={() => toggleSidebar('titlebar')}
            onNewThread={handleNewThread}
            appMode={activeAppMode}
            onSetAppMode={handleSetAppMode}
            availableModes={availableAppModes}
            showIncognitoToggle={activeAppMode !== 'work'}
            isPrivateMode={titleBarIncognitoActive}
            onTogglePrivateMode={handleDesktopTitleBarIncognitoClick}
            rightPanelOpen={rightPanelOpen}
            onToggleRightPanel={() => triggerTitleBarRightPanelClick()}
            showTitleBarNewThread={activeAppMode === 'work' && sidebarCollapsed}
            hasAppUpdate={hasAppUpdate}
            onCheckAppUpdate={handleCheckAppUpdate}
            appUpdateState={appUpdateState}
            onDownloadApp={handleDownloadApp}
            onInstallApp={handleInstallApp}
            onOpenSettings={handleTitleBarOpenSettings}
          />
        )}

        <div
          className="relative flex min-h-0 flex-1"
          style={{
            '--main-content-axis-padding-left': mainContentAxisPaddingLeft,
            '--main-content-axis-padding-right': mainContentAxisPaddingRight,
          } as React.CSSProperties}
        >
          {collapsedSidebarExitVisible && (
            <CollapsedSidebarTransitionOverlay direction="exit" onDone={() => setCollapsedSidebarExitVisible(false)} />
          )}
          {collapsedSidebarEnterVisible && (
            <CollapsedSidebarTransitionOverlay direction="enter" onDone={() => setCollapsedSidebarEnterVisible(false)} />
          )}
          {!sidebarHiddenByWidth && (
            <div
              className="relative h-full shrink-0 overflow-hidden"
              style={{
                width: hideCollapsedWorkSidebar ? 0 : sidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : sidebarWidth,
                transition: sidebarResizing || modeSwitchingCollapsedSidebar ? undefined : `width ${SIDEBAR_WIDTH_TRANSITION}`,
                visibility: collapsedSidebarEnterVisible ? 'hidden' : undefined,
              }}
            >
              {!hideCollapsedWorkSidebar && (
                <Sidebar
                  threads={filteredThreads}
                  onNewThread={handleNewThread}
                  onThreadDeleted={handleThreadDeleted}
                  beforeNavigateToThread={handleBeforeNavigateToThread}
                />
              )}
              {!sidebarCollapsed && (
                <div
                  role="separator"
                  aria-orientation="vertical"
                  title="Resize history"
                  onPointerDown={handleSidebarResizeStart}
                  className="absolute inset-y-0 right-0 z-20 w-2 cursor-col-resize"
                />
              )}
            </div>
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
          />
        </div>
      </div>
    </TimeZoneProvider>
  )
}
