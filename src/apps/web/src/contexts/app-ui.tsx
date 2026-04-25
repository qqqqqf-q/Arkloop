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
import { useNavigate, useLocation } from 'react-router-dom'
import { isDesktop } from '@arkloop/shared/desktop'
import type { SettingsTab } from '../components/SettingsModal'
import type { DesktopSettingsKey } from '../components/DesktopSettings'
import {
  readAppModeFromStorage,
  writeAppModeToStorage,
  type AppMode,
} from '../storage'
import {
  beginPerfTrace,
  endPerfTrace,
  isPerfDebugEnabled,
  recordPerfDuration,
  recordPerfValue,
  type PerfSample,
} from '../perfDebug'
import { useAuth } from './auth'

export interface AppUIContextValue {
  sidebarCollapsed: boolean
  sidebarHiddenByWidth: boolean
  rightPanelOpen: boolean
  isSearchMode: boolean
  searchOverlayOpen: boolean
  settingsOpen: boolean
  settingsInitialTab: SettingsTab
  desktopSettingsSection: DesktopSettingsKey
  notificationsOpen: boolean
  notificationVersion: number
  appMode: AppMode
  availableAppModes: AppMode[]
  pendingSkillPrompt: string | null

  toggleSidebar: (source?: 'sidebar' | 'titlebar') => void
  setRightPanelOpen: (open: boolean) => void
  enterSearchMode: () => void
  exitSearchMode: () => void
  openSearchOverlay: () => void
  closeSearchOverlay: () => void
  openSettings: (tab?: SettingsTab | 'voice') => void
  closeSettings: () => void
  openNotifications: () => void
  closeNotifications: () => void
  markNotificationRead: () => void
  setAppMode: (mode: AppMode) => void
  queueSkillPrompt: (prompt: string) => void
  consumeSkillPrompt: () => void
  setTitleBarIncognitoClick: (fn: (() => void) | null) => void
  triggerTitleBarIncognitoClick: (fallback: () => void) => void
}

type SidebarUIContextValue = Pick<
  AppUIContextValue,
  'sidebarCollapsed' | 'sidebarHiddenByWidth' | 'rightPanelOpen' | 'toggleSidebar' | 'setRightPanelOpen'
>

type SearchUIContextValue = Pick<
  AppUIContextValue,
  'isSearchMode' | 'searchOverlayOpen' | 'enterSearchMode' | 'exitSearchMode' | 'openSearchOverlay' | 'closeSearchOverlay'
>
type SettingsUIContextValue = Pick<AppUIContextValue, 'settingsOpen' | 'settingsInitialTab' | 'desktopSettingsSection' | 'openSettings' | 'closeSettings'>
type NotificationsUIContextValue = Pick<AppUIContextValue, 'notificationsOpen' | 'notificationVersion' | 'openNotifications' | 'closeNotifications' | 'markNotificationRead'>
type AppModeUIContextValue = Pick<AppUIContextValue, 'appMode' | 'availableAppModes' | 'setAppMode'>
type SkillPromptUIContextValue = Pick<AppUIContextValue, 'pendingSkillPrompt' | 'queueSkillPrompt' | 'consumeSkillPrompt'>
type TitleBarIncognitoUIContextValue = Pick<AppUIContextValue, 'setTitleBarIncognitoClick' | 'triggerTitleBarIncognitoClick'>

const AppUIContext = createContext<AppUIContextValue | null>(null)
const SidebarUIContext = createContext<SidebarUIContextValue | null>(null)
const SearchUIContext = createContext<SearchUIContextValue | null>(null)
const SettingsUIContext = createContext<SettingsUIContextValue | null>(null)
const NotificationsUIContext = createContext<NotificationsUIContextValue | null>(null)
const AppModeUIContext = createContext<AppModeUIContextValue | null>(null)
const SkillPromptUIContext = createContext<SkillPromptUIContextValue | null>(null)
const TitleBarIncognitoUIContext = createContext<TitleBarIncognitoUIContextValue | null>(null)

function AppUIProviders({
  value,
  children,
}: {
  value: AppUIContextValue
  children: ReactNode
}) {
  const sidebarValue = useMemo<SidebarUIContextValue>(
    () => ({
      sidebarCollapsed: value.sidebarCollapsed,
      sidebarHiddenByWidth: value.sidebarHiddenByWidth,
      rightPanelOpen: value.rightPanelOpen,
      toggleSidebar: value.toggleSidebar,
      setRightPanelOpen: value.setRightPanelOpen,
    }),
    [
      value.sidebarCollapsed,
      value.sidebarHiddenByWidth,
      value.rightPanelOpen,
      value.toggleSidebar,
      value.setRightPanelOpen,
    ],
  )

  const searchValue = useMemo<SearchUIContextValue>(
    () => ({
      isSearchMode: value.isSearchMode,
      searchOverlayOpen: value.searchOverlayOpen,
      enterSearchMode: value.enterSearchMode,
      exitSearchMode: value.exitSearchMode,
      openSearchOverlay: value.openSearchOverlay,
      closeSearchOverlay: value.closeSearchOverlay,
    }),
    [
      value.isSearchMode,
      value.searchOverlayOpen,
      value.enterSearchMode,
      value.exitSearchMode,
      value.openSearchOverlay,
      value.closeSearchOverlay,
    ],
  )

  const settingsValue = useMemo<SettingsUIContextValue>(
    () => ({
      settingsOpen: value.settingsOpen,
      settingsInitialTab: value.settingsInitialTab,
      desktopSettingsSection: value.desktopSettingsSection,
      openSettings: value.openSettings,
      closeSettings: value.closeSettings,
    }),
    [
      value.settingsOpen,
      value.settingsInitialTab,
      value.desktopSettingsSection,
      value.openSettings,
      value.closeSettings,
    ],
  )

  const notificationsValue = useMemo<NotificationsUIContextValue>(
    () => ({
      notificationsOpen: value.notificationsOpen,
      notificationVersion: value.notificationVersion,
      openNotifications: value.openNotifications,
      closeNotifications: value.closeNotifications,
      markNotificationRead: value.markNotificationRead,
    }),
    [
      value.notificationsOpen,
      value.notificationVersion,
      value.openNotifications,
      value.closeNotifications,
      value.markNotificationRead,
    ],
  )

  const appModeValue = useMemo<AppModeUIContextValue>(
    () => ({
      appMode: value.appMode,
      availableAppModes: value.availableAppModes,
      setAppMode: value.setAppMode,
    }),
    [value.appMode, value.availableAppModes, value.setAppMode],
  )

  const skillPromptValue = useMemo<SkillPromptUIContextValue>(
    () => ({
      pendingSkillPrompt: value.pendingSkillPrompt,
      queueSkillPrompt: value.queueSkillPrompt,
      consumeSkillPrompt: value.consumeSkillPrompt,
    }),
    [value.pendingSkillPrompt, value.queueSkillPrompt, value.consumeSkillPrompt],
  )

  const titleBarIncognitoValue = useMemo<TitleBarIncognitoUIContextValue>(
    () => ({
      setTitleBarIncognitoClick: value.setTitleBarIncognitoClick,
      triggerTitleBarIncognitoClick: value.triggerTitleBarIncognitoClick,
    }),
    [value.setTitleBarIncognitoClick, value.triggerTitleBarIncognitoClick],
  )

  return (
    <AppUIContext.Provider value={value}>
      <SidebarUIContext.Provider value={sidebarValue}>
        <SearchUIContext.Provider value={searchValue}>
          <SettingsUIContext.Provider value={settingsValue}>
            <NotificationsUIContext.Provider value={notificationsValue}>
              <AppModeUIContext.Provider value={appModeValue}>
                <SkillPromptUIContext.Provider value={skillPromptValue}>
                  <TitleBarIncognitoUIContext.Provider value={titleBarIncognitoValue}>
                    {children}
                  </TitleBarIncognitoUIContext.Provider>
                </SkillPromptUIContext.Provider>
              </AppModeUIContext.Provider>
            </NotificationsUIContext.Provider>
          </SettingsUIContext.Provider>
        </SearchUIContext.Provider>
      </SidebarUIContext.Provider>
    </AppUIContext.Provider>
  )
}

export function AppUIProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const { me } = useAuth()
  const desktop = isDesktop()
  const usesHashRouting = window.location.protocol === 'file:'

  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => window.innerWidth < 1200)
  const [sidebarHiddenByWidth, setSidebarHiddenByWidth] = useState(() => window.innerWidth < 560)
  const autoCollapsedByWidthRef = useRef(window.innerWidth < 1200)
  const manualSidebarCollapsedRef = useRef<boolean | null>(null)
  const [rightPanelOpen, setRightPanelOpen] = useState(false)
  const [isSearchMode, setIsSearchMode] = useState(false)
  const [searchOverlayOpen, setSearchOverlayOpen] = useState(false)
  const isSearchModeRef = useRef(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsInitialTab, setSettingsInitialTab] = useState<SettingsTab>('account')
  const [desktopSettingsSection, setDesktopSettingsSection] = useState<DesktopSettingsKey>('general')
  const [notificationsOpen, setNotificationsOpen] = useState(
    () => new URLSearchParams(location.search).has('notices'),
  )
  const [notificationVersion, setNotificationVersion] = useState(0)
  const [appMode, setAppModeState] = useState<AppMode>(readAppModeFromStorage)
  const [pendingSkillPrompt, setPendingSkillPrompt] = useState<string | null>(null)

  const settingsOpenTraceRef = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const settingsLifecycleRef = useRef<{ startedAt: number; sample: PerfSample } | null>(null)
  const sidebarToggleTraceRef = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const sidebarLifecycleRef = useRef<{ startedAt: number; sample: PerfSample } | null>(null)
  const titleBarIncognitoRef = useRef<(() => void) | null>(null)

  const availableAppModes: AppMode[] = useMemo(
    () => (desktop || me?.work_enabled !== false) ? ['chat', 'work'] : ['chat'],
    [desktop, me?.work_enabled],
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

  const toggleSidebar = useCallback((source: 'sidebar' | 'titlebar' = 'sidebar') => {
    const nextCollapsed = !sidebarCollapsed
    if (isPerfDebugEnabled() && typeof performance !== 'undefined') {
      const sample = {
        source,
        collapsed: sidebarCollapsed,
        nextCollapsed,
        appMode,
        pathname: location.pathname,
      }
      sidebarLifecycleRef.current = {
        startedAt: performance.now(),
        sample,
      }
      sidebarToggleTraceRef.current = beginPerfTrace('desktop_sidebar_toggle', sample)
      window.dispatchEvent(new CustomEvent('arkloop:sidebar-toggle-started', {
        detail: {
          startedAt: sidebarLifecycleRef.current.startedAt,
          sample,
        },
      }))
    }
    manualSidebarCollapsedRef.current = nextCollapsed
    setSidebarCollapsed(nextCollapsed)
  }, [appMode, location.pathname, sidebarCollapsed])

  const enterSearchMode = useCallback(() => {
    pushSearchModeState()
    setIsSearchMode(true)
  }, [pushSearchModeState])

  const exitSearchMode = useCallback(() => {
    setIsSearchMode(false)
  }, [])

  const openSearchOverlay = useCallback(() => {
    setSearchOverlayOpen(true)
  }, [])

  const closeSearchOverlay = useCallback(() => {
    setSearchOverlayOpen(false)
  }, [])

  const openSettings = useCallback((tab: SettingsTab | 'voice' = 'account') => {
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
        updates: 'advanced',
      }
      const section = keyMap[tab] ?? 'general'
      const sample = {
        source: 'sidebar',
        requestedTab: tab,
        section,
        pathname: location.pathname,
      }
      recordPerfDuration('desktop_settings_open_request', 0, sample)
      settingsOpenTraceRef.current = beginPerfTrace('desktop_settings_open', sample)
      if (isPerfDebugEnabled() && typeof performance !== 'undefined') {
        settingsLifecycleRef.current = {
          startedAt: performance.now(),
          sample,
        }
      }
      setDesktopSettingsSection(section)
      setSettingsOpen(true)
      return
    }
    setSettingsInitialTab(tab as SettingsTab)
    setSettingsOpen(true)
  }, [desktop, location.pathname])

  const closeSettings = useCallback(() => {
    setSettingsOpen(false)
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

  const markNotificationRead = useCallback(() => {
    setNotificationVersion((v) => v + 1)
  }, [])

  const handleSetAppMode = useCallback((mode: AppMode) => {
    writeAppModeToStorage(mode)
    setAppModeState(mode)
    if (/^\/t\//.test(location.pathname)) {
      navigate('/')
    }
  }, [location.pathname, navigate])

  const queueSkillPrompt = useCallback((prompt: string) => {
    setPendingSkillPrompt(prompt)
  }, [])

  const consumeSkillPrompt = useCallback(() => {
    setPendingSkillPrompt(null)
  }, [])

  const setTitleBarIncognitoClick = useCallback((fn: (() => void) | null) => {
    titleBarIncognitoRef.current = fn
  }, [])

  const triggerTitleBarIncognitoClick = useCallback((fallback: () => void) => {
    const fn = titleBarIncognitoRef.current
    if (fn) fn()
    else fallback()
  }, [])

  useEffect(() => {
    isSearchModeRef.current = isSearchMode
  }, [isSearchMode])

  useEffect(() => {
    const lifecycle = sidebarLifecycleRef.current
    if (!lifecycle || typeof performance === 'undefined') return
    const commitDuration = performance.now() - lifecycle.startedAt
    recordPerfDuration('desktop_sidebar_toggle_commit', commitDuration, {
      ...lifecycle.sample,
      phase: 'commit',
    })
    const frameId = requestAnimationFrame(() => {
      if (typeof performance === 'undefined') return
      recordPerfDuration('desktop_sidebar_toggle_first_frame', performance.now() - lifecycle.startedAt, {
        ...lifecycle.sample,
        phase: 'frame',
      })
      endPerfTrace(sidebarToggleTraceRef.current, {
        ...lifecycle.sample,
        phase: 'visible',
      })
      sidebarToggleTraceRef.current = null
      sidebarLifecycleRef.current = null
    })
    return () => cancelAnimationFrame(frameId)
  }, [sidebarCollapsed])

  useEffect(() => {
    let raf = 0
    const handler = () => {
      cancelAnimationFrame(raf)
      raf = requestAnimationFrame(() => {
        const width = window.innerWidth
        const hidden = width < 560
        setSidebarHiddenByWidth((prev) => (prev === hidden ? prev : hidden))
        const narrow = width < 1200
        if (manualSidebarCollapsedRef.current !== null) {
          autoCollapsedByWidthRef.current = narrow
          return
        }
        if (narrow !== autoCollapsedByWidthRef.current) {
          autoCollapsedByWidthRef.current = narrow
          setSidebarCollapsed(narrow)
        }
      })
    }
    window.addEventListener('resize', handler)
    return () => {
      window.removeEventListener('resize', handler)
      cancelAnimationFrame(raf)
    }
  }, [])

  useEffect(() => {
    if (location.pathname === '/') return
    const id = requestAnimationFrame(() => setIsSearchMode(false))
    return () => cancelAnimationFrame(id)
  }, [location.pathname])

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      setRightPanelOpen(false)
      if (notificationsOpen) closeNotifications()
    })
    return () => cancelAnimationFrame(id)
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!(desktop && settingsOpen && /^\/t\//.test(location.pathname))) return
    const id = requestAnimationFrame(() => setSettingsOpen(false))
    return () => cancelAnimationFrame(id)
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!desktop) return
    const handler = () => {
      const sample = {
        source: 'window-event',
        requestedSection: 'general',
        pathname: location.pathname,
      }
      settingsOpenTraceRef.current = beginPerfTrace('desktop_settings_open', sample)
      if (isPerfDebugEnabled() && typeof performance !== 'undefined') {
        settingsLifecycleRef.current = {
          startedAt: performance.now(),
          sample,
        }
      }
      setDesktopSettingsSection('general')
      setSettingsOpen(true)
    }
    window.addEventListener('arkloop:app:open-settings', handler as EventListener)
    return () => window.removeEventListener('arkloop:app:open-settings', handler as EventListener)
  }, [desktop, location.pathname])

  useEffect(() => {
    if (!(desktop && settingsOpen)) return
    const lifecycle = settingsLifecycleRef.current
    if (lifecycle && typeof performance !== 'undefined') {
      recordPerfDuration('desktop_settings_state_commit', performance.now() - lifecycle.startedAt, {
        ...lifecycle.sample,
        section: desktopSettingsSection,
        pathname: location.pathname,
        phase: 'commit',
      })
    }
    const frameId = requestAnimationFrame(() => {
      if (lifecycle && typeof performance !== 'undefined') {
        recordPerfDuration('desktop_settings_first_frame', performance.now() - lifecycle.startedAt, {
          ...lifecycle.sample,
          section: desktopSettingsSection,
          pathname: location.pathname,
          phase: 'frame',
        })
      }
      endPerfTrace(settingsOpenTraceRef.current, {
        phase: 'visible',
        section: desktopSettingsSection,
        pathname: location.pathname,
      })
      settingsOpenTraceRef.current = null
      settingsLifecycleRef.current = null
    })
    return () => cancelAnimationFrame(frameId)
  }, [desktop, settingsOpen, desktopSettingsSection, location.pathname])

  useEffect(() => {
    recordPerfValue('app_ui_render_count', 1, 'count', {
      sidebarCollapsed,
      settingsOpen,
      searchOverlayOpen,
      notificationsOpen,
      isSearchMode,
      appMode,
      pathname: location.pathname,
    })
  })

  useEffect(() => {
    const onPopState = () => {
      if (isSearchModeRef.current) setIsSearchMode(false)
    }
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const value = useMemo<AppUIContextValue>(() => ({
    sidebarCollapsed,
    sidebarHiddenByWidth,
    rightPanelOpen,
    isSearchMode,
    searchOverlayOpen,
    settingsOpen,
    settingsInitialTab,
    desktopSettingsSection,
    notificationsOpen,
    notificationVersion,
    appMode,
    availableAppModes,
    pendingSkillPrompt,
    toggleSidebar,
    setRightPanelOpen,
    enterSearchMode,
    exitSearchMode,
    openSearchOverlay,
    closeSearchOverlay,
    openSettings,
    closeSettings,
    openNotifications,
    closeNotifications,
    markNotificationRead,
    setAppMode: handleSetAppMode,
    queueSkillPrompt,
    consumeSkillPrompt,
    setTitleBarIncognitoClick,
    triggerTitleBarIncognitoClick,
  }), [
    sidebarCollapsed,
    sidebarHiddenByWidth,
    rightPanelOpen,
    isSearchMode,
    searchOverlayOpen,
    settingsOpen,
    settingsInitialTab,
    desktopSettingsSection,
    notificationsOpen,
    notificationVersion,
    appMode,
    availableAppModes,
    pendingSkillPrompt,
    toggleSidebar,
    setRightPanelOpen,
    enterSearchMode,
    exitSearchMode,
    openSearchOverlay,
    closeSearchOverlay,
    openSettings,
    closeSettings,
    openNotifications,
    closeNotifications,
    markNotificationRead,
    handleSetAppMode,
    queueSkillPrompt,
    consumeSkillPrompt,
    setTitleBarIncognitoClick,
    triggerTitleBarIncognitoClick,
  ])

  return <AppUIProviders value={value}>{children}</AppUIProviders>
}

export function AppUIContextBridge({
  value,
  children,
}: {
  value: AppUIContextValue
  children: ReactNode
}) {
  return <AppUIProviders value={value}>{children}</AppUIProviders>
}

export function useAppUI(): AppUIContextValue {
  const ctx = useContext(AppUIContext)
  if (!ctx) throw new Error('useAppUI must be used within AppUIProvider')
  return ctx
}

export function useSidebarUI(): SidebarUIContextValue {
  const ctx = useContext(SidebarUIContext)
  if (!ctx) throw new Error('useSidebarUI must be used within AppUIProvider')
  return ctx
}

export function useSearchUI(): SearchUIContextValue {
  const ctx = useContext(SearchUIContext)
  if (!ctx) throw new Error('useSearchUI must be used within AppUIProvider')
  return ctx
}

export function useSettingsUI(): SettingsUIContextValue {
  const ctx = useContext(SettingsUIContext)
  if (!ctx) throw new Error('useSettingsUI must be used within AppUIProvider')
  return ctx
}

export function useNotificationsUI(): NotificationsUIContextValue {
  const ctx = useContext(NotificationsUIContext)
  if (!ctx) throw new Error('useNotificationsUI must be used within AppUIProvider')
  return ctx
}

export function useAppModeUI(): AppModeUIContextValue {
  const ctx = useContext(AppModeUIContext)
  if (!ctx) throw new Error('useAppModeUI must be used within AppUIProvider')
  return ctx
}

export function useSkillPromptUI(): SkillPromptUIContextValue {
  const ctx = useContext(SkillPromptUIContext)
  if (!ctx) throw new Error('useSkillPromptUI must be used within AppUIProvider')
  return ctx
}

export function useTitleBarIncognitoUI(): TitleBarIncognitoUIContextValue {
  const ctx = useContext(TitleBarIncognitoUIContext)
  if (!ctx) throw new Error('useTitleBarIncognitoUI must be used within AppUIProvider')
  return ctx
}
