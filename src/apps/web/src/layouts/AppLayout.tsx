import { memo, useCallback, useEffect, useMemo, useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
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
import { writeSelectedPersonaKeyToStorage, DEFAULT_PERSONA_KEY } from '../storage'
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

  const [appUpdateState, setAppUpdateState] = useState<import('@arkloop/shared/desktop').AppUpdaterState | null>(null)

  // app updater
  useEffect(() => {
    if (!desktop) return
    const api = getDesktopApi()
    if (!api?.appUpdater) return
    void api.appUpdater.getState().then(setAppUpdateState).catch(() => {})
    return api.appUpdater.onState(setAppUpdateState)
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
    appUpdateState?.phase === 'available' ||
    appUpdateState?.phase === 'downloaded'

  return (
    <TimeZoneProvider userTimeZone={me?.timezone ?? null} accountTimeZone={me?.account_timezone ?? null}>
      <div className="flex h-screen flex-col overflow-hidden bg-[var(--c-bg-page)]">
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
          />
        </div>
      </div>
    </TimeZoneProvider>
  )
}
