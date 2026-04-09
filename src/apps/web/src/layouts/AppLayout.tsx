import { memo, useCallback, useEffect, useMemo } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { isDesktop } from '@arkloop/shared/desktop'
import { LoadingPage, TimeZoneProvider } from '@arkloop/shared'
import { Sidebar } from '../components/Sidebar'
import { DesktopTitleBar } from '../components/DesktopTitleBar'
import { SettingsModal } from '../components/SettingsModal'
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
  pathname: string
  onSearchClose: () => void
  onMeUpdated: (m: import('../api').MeResponse) => void
  onTrySkill: (prompt: string) => void
}

const LayoutMain = memo(function LayoutMain({
  desktop,
  isSearchOpen,
  filteredThreads,
  pathname,
  onSearchClose,
  onMeUpdated,
  onTrySkill,
}: LayoutMainProps) {
  const { me, accessToken, logout } = useAuth()
  const { setCreditsBalance } = useCredits()
  const { settingsOpen, settingsInitialTab, desktopSettingsSection, closeSettings } = useSettingsUI()
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
        <ChatsSearchModal threads={filteredThreads} accessToken={accessToken} onClose={onSearchClose} />
      )}

      {desktop && settingsOpen ? (
        <DesktopSettings
          me={me}
          accessToken={accessToken}
          initialSection={desktopSettingsSection}
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
    isPrivateMode, pendingIncognitoMode,
    privateThreadIds, removeThread,
    togglePrivateMode,
    getFilteredThreads,
  } = useThreadList()
  const { sidebarCollapsed, sidebarHiddenByWidth, toggleSidebar } = useSidebarUI()
  const { isSearchMode, searchOverlayOpen, exitSearchMode, closeSearchOverlay } = useSearchUI()
  const { appMode, availableAppModes, setAppMode } = useAppModeUI()
  const { closeSettings } = useSettingsUI()
  const { closeNotifications } = useNotificationsUI()
  const { queueSkillPrompt } = useSkillPromptUI()
  const { triggerTitleBarIncognitoClick } = useTitleBarIncognitoUI()
  useCredits()
  const { t } = useLocale()
  const navigate = useNavigate()
  const location = useLocation()
  const desktop = isDesktop()

  const pathnameSearchOpen = location.pathname.endsWith('/search')
  const isSearchOpen = searchOverlayOpen || pathnameSearchOpen
  const filteredThreads = useMemo(() => getFilteredThreads(appMode), [getFilteredThreads, appMode])

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

  const currentThreadId = location.pathname.match(/^\/t\/([^/]+)/)?.[1] ?? null
  const titleBarIncognitoActive =
    isPrivateMode || pendingIncognitoMode ||
    (currentThreadId != null && privateThreadIds.has(currentThreadId))

  return (
    <TimeZoneProvider userTimeZone={me?.timezone ?? null} accountTimeZone={me?.account_timezone ?? null}>
      <div className="flex h-screen flex-col overflow-hidden bg-[var(--c-bg-page)]">
        {desktop && (
          <DesktopTitleBar
            sidebarCollapsed={sidebarCollapsed}
            onToggleSidebar={() => toggleSidebar('titlebar')}
            appMode={appMode}
            onSetAppMode={setAppMode}
            availableModes={availableAppModes}
            showIncognitoToggle={appMode !== 'work'}
            isPrivateMode={titleBarIncognitoActive}
            onTogglePrivateMode={handleDesktopTitleBarIncognitoClick}
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
