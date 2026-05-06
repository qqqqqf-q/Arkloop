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
  createBrowserTab: () => Promise<string | null>
  activateBrowserTab: (tabId: string) => void
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
  const legacyBrowserTabId = readBrowserTabId(location.pathname)
  const [initialized, setInitialized] = useState(!desktop)
  const [tabs, setTabs] = useState<DesktopBrowserTab[]>([])
  const [panelOpen, setPanelOpen] = useState(false)
  const [activeBrowserTabId, setActiveBrowserTabId] = useState<string | null>(null)
  const [draftUrls, setDraftUrls] = useState<Record<string, string>>({})
  const recentTabIdsRef = useRef<string[]>([])

  useEffect(() => {
    if (!desktop) {
      setInitialized(true)
      setTabs([])
      setPanelOpen(false)
      setActiveBrowserTabId(null)
      return
    }
    const api = getDesktopApi()
    if (!api?.browserTabs) {
      setInitialized(true)
      setTabs([])
      return
    }

    let cancelled = false
    api.browserTabs.list()
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

    const unsubscribe = api.browserTabs.onStateChanged((snapshot) => {
      if (cancelled) return
      setTabs(snapshot.tabs)
    })

    return () => {
      cancelled = true
      unsubscribe?.()
    }
  }, [desktop])

  useEffect(() => {
    setDraftUrls((current) => {
      const next: Record<string, string> = {}
      for (const tab of tabs) {
        next[tab.id] = current[tab.id] ?? tab.url
      }
      return next
    })
  }, [tabs])

  useEffect(() => {
    if (!activeBrowserTabId) return
    recentTabIdsRef.current = [
      activeBrowserTabId,
      ...recentTabIdsRef.current.filter((tabId) => tabId !== activeBrowserTabId),
    ]
  }, [activeBrowserTabId])

  useEffect(() => {
    if (!initialized) return
    if (!legacyBrowserTabId) return
    setPanelOpen(true)
    setActiveBrowserTabId(legacyBrowserTabId)
    navigate('/', { replace: true })
  }, [initialized, legacyBrowserTabId, navigate])

  useEffect(() => {
    if (!initialized) return
    if (!activeBrowserTabId) return
    if (tabs.some((tab) => tab.id === activeBrowserTabId)) return
    const nextRecentTabId = recentTabIdsRef.current.find((id) => tabs.some((tab) => tab.id === id))
    setActiveBrowserTabId(nextRecentTabId ?? tabs[0]?.id ?? null)
    if (tabs.length === 0) {
      setPanelOpen(false)
    }
  }, [activeBrowserTabId, initialized, tabs])

  const getDraftUrl = useCallback((tabId: string) => draftUrls[tabId] ?? '', [draftUrls])

  const setDraftUrl = useCallback((tabId: string, value: string) => {
    setDraftUrls((current) => ({ ...current, [tabId]: value }))
  }, [])

  const openBrowserPanel = useCallback(() => {
    setPanelOpen(true)
    setActiveBrowserTabId((current) => current ?? recentTabIdsRef.current[0] ?? tabs[0]?.id ?? null)
  }, [tabs])

  const closeBrowserPanel = useCallback(() => {
    setPanelOpen(false)
  }, [])

  const toggleBrowserPanel = useCallback(() => {
    setPanelOpen((current) => {
      const next = !current
      if (next) {
        setActiveBrowserTabId((active) => active ?? recentTabIdsRef.current[0] ?? tabs[0]?.id ?? null)
      }
      return next
    })
  }, [tabs])

  const createBrowserTab = useCallback(async () => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    const tab = await api.browserTabs.create()
    setDraftUrls((current) => ({ ...current, [tab.id]: tab.url }))
    setPanelOpen(true)
    setActiveBrowserTabId(tab.id)
    return tab.id
  }, [])

  const activateBrowserTab = useCallback((tabId: string) => {
    setPanelOpen(true)
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
    recentTabIdsRef.current = recentTabIdsRef.current.filter((id) => id !== tabId)
    setTabs(snapshot.tabs)
    if (activeBrowserTabId === tabId) {
      const nextRecentTabId = recentTabIdsRef.current.find((id) => snapshot.tabs.some((tab) => tab.id === id))
      const fallbackTabId = nextRecentTabId ?? snapshot.tabs[0]?.id ?? null
      setActiveBrowserTabId(fallbackTabId)
      if (!fallbackTabId) {
        setPanelOpen(false)
      }
    }
  }, [activeBrowserTabId])

  const navigateBrowserTab = useCallback(async (tabId: string, url?: string) => {
    const api = getDesktopApi()
    if (!api?.browserTabs) return null
    const targetUrl = (url ?? draftUrls[tabId] ?? '').trim()
    setDraftUrls((current) => ({ ...current, [tabId]: targetUrl }))
    return await api.browserTabs.navigate(tabId, targetUrl)
  }, [draftUrls])

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
    () => activeBrowserTabId ? tabs.find((tab) => tab.id === activeBrowserTabId) ?? null : null,
    [activeBrowserTabId, tabs],
  )

  const value = useMemo<BrowserTabsContextValue>(() => ({
    initialized,
    tabs,
    panelOpen,
    activeBrowserTabId,
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
    activeBrowserTabId,
    activateBrowserTab,
    closeBrowserTab,
    createBrowserTab,
    getDraftUrl,
    goBackBrowserTab,
    goForwardBrowserTab,
    initialized,
    panelOpen,
    navigateBrowserTab,
    openBrowserPanel,
    reloadBrowserTab,
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
