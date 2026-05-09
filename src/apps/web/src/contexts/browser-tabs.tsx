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
import { useLocation, useNavigate } from 'react-router-dom'
import { getDesktopApi, isDesktop, type DesktopBrowserTab } from '@arkloop/shared/desktop'

type BrowserTabsContextValue = {
  initialized: boolean
  tabs: DesktopBrowserTab[]
  panelOpen: boolean
  activeBrowserTabId: string | null
  activeBrowserTab: DesktopBrowserTab | null
  getDraftUrl: (tabId: string) => string
  setDraftUrl: (tabId: string, value: string) => void
  openBrowserPanel: () => void
  closeBrowserPanel: () => void
  toggleBrowserPanel: () => void
  createBrowserTab: (options?: { openPanel?: boolean }) => Promise<string | null>
  activateBrowserTab: (tabId: string, options?: { openPanel?: boolean }) => void
  closeBrowserTab: (tabId: string) => Promise<void>
  navigateBrowserTab: (tabId: string, url?: string) => Promise<DesktopBrowserTab | null>
  reloadBrowserTab: (tabId: string) => Promise<DesktopBrowserTab | null>
  goBackBrowserTab: (tabId: string) => Promise<DesktopBrowserTab | null>
  goForwardBrowserTab: (tabId: string) => Promise<DesktopBrowserTab | null>
}

const BrowserTabsContext = createContext<BrowserTabsContextValue | null>(null)

function readBrowserTabId(pathname: string): string | null {
  const match = pathname.match(/^\/browser\/([^/]+)$/)
  return match?.[1] ? decodeURIComponent(match[1]) : null
}

export function BrowserTabsProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const desktop = isDesktop()
  const browserApi = desktop ? getDesktopApi()?.browserTabs ?? null : null
  const legacyBrowserTabId = readBrowserTabId(location.pathname)
  const [initialized, setInitialized] = useState(!desktop || !browserApi)
  const [tabs, setTabs] = useState<DesktopBrowserTab[]>([])
  const [panelOpen, setPanelOpen] = useState(Boolean(legacyBrowserTabId))
  const [activeBrowserTabId, setActiveBrowserTabId] = useState<string | null>(legacyBrowserTabId)
  const [recentTabIds, setRecentTabIds] = useState<string[]>(legacyBrowserTabId ? [legacyBrowserTabId] : [])
  const [draftUrls, setDraftUrls] = useState<Record<string, string>>({})
  const draftUrlsRef = useRef<Record<string, string>>({})

  const mergedDraftUrls = useMemo(() => {
    const next: Record<string, string> = {}
    for (const tab of tabs) {
      next[tab.id] = draftUrls[tab.id] ?? tab.url
    }
    return next
  }, [draftUrls, tabs])

  const resolvedActiveBrowserTabId = useMemo(() => {
    if (!initialized) return activeBrowserTabId
    if (!activeBrowserTabId) return activeBrowserTabId
    if (tabs.some((tab) => tab.id === activeBrowserTabId)) return activeBrowserTabId
    const nextRecentTabId = recentTabIds.find((id) => tabs.some((tab) => tab.id === id))
    if (nextRecentTabId) return nextRecentTabId
    return tabs[0]?.id ?? null
  }, [activeBrowserTabId, initialized, recentTabIds, tabs])

  const resolvedPanelOpen = useMemo(() => {
    if (!initialized) return panelOpen
    if (!panelOpen) return false
    if (tabs.length > 0) return true
    return activeBrowserTabId == null
  }, [activeBrowserTabId, initialized, panelOpen, tabs.length])

  useEffect(() => {
    draftUrlsRef.current = mergedDraftUrls
  }, [mergedDraftUrls])

  useEffect(() => {
    if (!desktop) {
      return
    }
    if (!browserApi) {
      return
    }

    let cancelled = false
    browserApi.list()
      .then((snapshot) => {
        if (cancelled) return
        setTabs(snapshot.tabs)
        setInitialized(true)
      })
      .catch(() => {
        if (cancelled) return
        setTabs([])
        setInitialized(true)
      })

    const unsubscribe = browserApi.onStateChanged((snapshot) => {
      if (cancelled) return
      setTabs(snapshot.tabs)
    })

    return () => {
      cancelled = true
      unsubscribe?.()
    }
  }, [browserApi, desktop])

  useEffect(() => {
    if (!initialized) return
    if (!legacyBrowserTabId) return
    navigate('/', { replace: true })
  }, [initialized, legacyBrowserTabId, navigate])

  const getDraftUrl = useCallback((tabId: string) => mergedDraftUrls[tabId] ?? '', [mergedDraftUrls])

  const setDraftUrl = useCallback((tabId: string, value: string) => {
    setDraftUrls((current) => ({ ...current, [tabId]: value }))
  }, [])

  const openBrowserPanel = useCallback(() => {
    setPanelOpen(true)
    setActiveBrowserTabId((current) => current ?? recentTabIds[0] ?? tabs[0]?.id ?? null)
  }, [recentTabIds, tabs])

  const closeBrowserPanel = useCallback(() => {
    setPanelOpen(false)
  }, [])

  const toggleBrowserPanel = useCallback(() => {
    setPanelOpen((current) => {
      const next = !current
      if (next) {
        setActiveBrowserTabId((active) => active ?? recentTabIds[0] ?? tabs[0]?.id ?? null)
      }
      return next
    })
  }, [recentTabIds, tabs])

  const createBrowserTab = useCallback(async (options?: { openPanel?: boolean }) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    const tab = await api.browserTabs.create()
    setDraftUrls((current) => ({ ...current, [tab.id]: tab.url }))
    setRecentTabIds((current) => [tab.id, ...current.filter((existingId) => existingId !== tab.id)])
    setPanelOpen(options?.openPanel ?? true)
    setActiveBrowserTabId(tab.id)
    return tab.id
  }, [])

  const activateBrowserTab = useCallback((tabId: string, options?: { openPanel?: boolean }) => {
    setPanelOpen((current) => (options?.openPanel ?? true) ? true : current)
    setRecentTabIds((current) => [tabId, ...current.filter((existingId) => existingId !== tabId)])
    setActiveBrowserTabId(tabId)
  }, [])

  const closeBrowserTab = useCallback(async (tabId: string) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return
    const snapshot = await api.browserTabs.close(tabId)
    setDraftUrls((current) => {
      const next = { ...current }
      delete next[tabId]
      return next
    })
    const remainingRecentTabIds = recentTabIds.filter((id) => id !== tabId)
    setRecentTabIds(remainingRecentTabIds)
    const nextRecentTabId = remainingRecentTabIds.find((id) => snapshot.tabs.some((tab) => tab.id === id))
    setTabs(snapshot.tabs)
    if (activeBrowserTabId === tabId) {
      const fallbackTabId = nextRecentTabId ?? snapshot.tabs[0]?.id ?? null
      setActiveBrowserTabId(fallbackTabId)
      if (!fallbackTabId) {
        setPanelOpen(false)
      }
    }
  }, [activeBrowserTabId, recentTabIds])

  const navigateBrowserTab = useCallback(async (tabId: string, url?: string) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    const targetUrl = (url ?? draftUrlsRef.current[tabId] ?? '').trim()
    setDraftUrls((current) => ({ ...current, [tabId]: targetUrl }))
    return await api.browserTabs.navigate(tabId, targetUrl)
  }, [])

  const reloadBrowserTab = useCallback(async (tabId: string) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    return await api.browserTabs.reload(tabId)
  }, [])

  const goBackBrowserTab = useCallback(async (tabId: string) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    return await api.browserTabs.goBack(tabId)
  }, [])

  const goForwardBrowserTab = useCallback(async (tabId: string) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    return await api.browserTabs.goForward(tabId)
  }, [])

  const activeBrowserTab = useMemo(
    () => resolvedActiveBrowserTabId ? tabs.find((tab) => tab.id === resolvedActiveBrowserTabId) ?? null : null,
    [resolvedActiveBrowserTabId, tabs],
  )

  const value = useMemo<BrowserTabsContextValue>(() => ({
    initialized,
    tabs,
    panelOpen: resolvedPanelOpen,
    activeBrowserTabId: resolvedActiveBrowserTabId,
    activeBrowserTab,
    getDraftUrl,
    setDraftUrl,
    openBrowserPanel,
    closeBrowserPanel,
    toggleBrowserPanel,
    createBrowserTab,
    activateBrowserTab,
    closeBrowserTab,
    navigateBrowserTab,
    reloadBrowserTab,
    goBackBrowserTab,
    goForwardBrowserTab,
  }), [
    activeBrowserTab,
    activateBrowserTab,
    closeBrowserTab,
    createBrowserTab,
    getDraftUrl,
    goBackBrowserTab,
    goForwardBrowserTab,
    initialized,
    navigateBrowserTab,
    openBrowserPanel,
    reloadBrowserTab,
    resolvedActiveBrowserTabId,
    resolvedPanelOpen,
    setDraftUrl,
    closeBrowserPanel,
    tabs,
    toggleBrowserPanel,
  ])

  return <BrowserTabsContext.Provider value={value}>{children}</BrowserTabsContext.Provider>
}

export function useBrowserTabs(): BrowserTabsContextValue {
  const context = useContext(BrowserTabsContext)
  if (!context) {
    throw new Error('useBrowserTabs must be used within BrowserTabsProvider')
  }
  return context
}
