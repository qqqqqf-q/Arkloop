import { BrowserView, BrowserWindow, shell } from 'electron'

export type BrowserTabState = {
  id: string
  title: string
  url: string
  faviconUrl: string | null
  loading: boolean
  error: string | null
  canGoBack: boolean
  canGoForward: boolean
}

type BrowserTabBounds = {
  x: number
  y: number
  width: number
  height: number
}

type BrowserTabRecord = {
  state: BrowserTabState
  view: BrowserView
}

type BrowserTabsSnapshot = {
  tabs: BrowserTabState[]
}

const browserTabs = new Map<string, BrowserTabRecord>()
const FAILED_LOAD_IGNORED_CODES = new Set([-3])

let getWindowRef: (() => BrowserWindow | null) | null = null
let visibleTabId: string | null = null
let visibleBounds: BrowserTabBounds | null = null
let stateListener: ((snapshot: BrowserTabsSnapshot) => void) | null = null

function getWindow(): BrowserWindow | null {
  return getWindowRef?.() ?? null
}

function emitState(): void {
  stateListener?.(snapshotBrowserTabs())
}

function snapshotBrowserTabs(): BrowserTabsSnapshot {
  return {
    tabs: Array.from(browserTabs.values()).map((record) => ({ ...record.state })),
  }
}

function getBrowserTabRecord(tabId: string): BrowserTabRecord {
  const record = browserTabs.get(tabId)
  if (!record) {
    throw new Error(`browser tab not found: ${tabId}`)
  }
  return record
}

function parseHttpUrl(rawUrl: string): URL | null {
  try {
    const parsed = new URL(rawUrl)
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
      return parsed
    }
  } catch {}
  return null
}

function normalizeBrowserUrl(rawUrl: string): { url: string | null; error: string | null } {
  const trimmed = rawUrl.trim()
  if (!trimmed) {
    return { url: null, error: '请输入网址' }
  }
  const candidate = /^[a-z][a-z\d+\-.]*:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`
  const parsed = parseHttpUrl(candidate)
  if (!parsed) {
    return { url: null, error: '仅支持 http/https 地址' }
  }
  return { url: parsed.toString(), error: null }
}

function getFallbackFaviconUrl(rawUrl: string): string | null {
  const parsed = parseHttpUrl(rawUrl)
  if (!parsed) return null
  return new URL('/favicon.ico', parsed).toString()
}

function toBounds(bounds: BrowserTabBounds | null): Electron.Rectangle | null {
  if (!bounds) return null
  const width = Math.max(0, Math.floor(bounds.width))
  const height = Math.max(0, Math.floor(bounds.height))
  if (width <= 0 || height <= 0) return null
  return {
    x: Math.max(0, Math.floor(bounds.x)),
    y: Math.max(0, Math.floor(bounds.y)),
    width,
    height,
  }
}

function detachVisibleBrowserView(): void {
  const win = getWindow()
  if (!win || win.isDestroyed() || !visibleTabId) return
  const record = browserTabs.get(visibleTabId)
  if (!record) {
    visibleTabId = null
    return
  }
  try {
    win.removeBrowserView(record.view)
  } catch {}
}

function syncVisibleBrowserView(): void {
  const win = getWindow()
  if (!win || win.isDestroyed()) return

  const bounds = toBounds(visibleBounds)
  if (!visibleTabId || !bounds) {
    detachVisibleBrowserView()
    return
  }

  const record = browserTabs.get(visibleTabId)
  if (!record) {
    visibleTabId = null
    detachVisibleBrowserView()
    return
  }

  const browserViews = win.getBrowserViews()
  if (!browserViews.includes(record.view)) {
    for (const view of browserViews) {
      try {
        win.removeBrowserView(view)
      } catch {}
    }
    win.addBrowserView(record.view)
  }

  win.setTopBrowserView(record.view)
  record.view.setBounds(bounds)
  record.view.setAutoResize({ width: true, height: true })
}

function updateBrowserTabState(tabId: string, updater: (current: BrowserTabState) => BrowserTabState): BrowserTabState {
  const record = getBrowserTabRecord(tabId)
  record.state = updater(record.state)
  emitState()
  return { ...record.state }
}

function syncStateFromWebContents(tabId: string): void {
  const record = getBrowserTabRecord(tabId)
  const title = record.view.webContents.getTitle().trim()
  const url = record.view.webContents.getURL().trim()
  updateBrowserTabState(tabId, (current) => ({
    ...current,
    title: title || current.title,
    url: url || current.url,
    faviconUrl: current.faviconUrl ?? getFallbackFaviconUrl(url || current.url),
    loading: record.view.webContents.isLoading(),
    canGoBack: record.view.webContents.canGoBack(),
    canGoForward: record.view.webContents.canGoForward(),
    error: current.error,
  }))
}

function createBrowserView(tabId: string): BrowserView {
  const view = new BrowserView({
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  })

  const sync = () => {
    if (!browserTabs.has(tabId)) return
    syncStateFromWebContents(tabId)
  }

  view.webContents.setWindowOpenHandler(({ url }) => {
    const parsed = parseHttpUrl(url)
    if (parsed) {
      void view.webContents.loadURL(parsed.toString()).catch((error) => {
        updateBrowserTabState(tabId, (current) => ({
          ...current,
          loading: false,
          error: error instanceof Error ? error.message : String(error),
        }))
      })
      return { action: 'deny' }
    }
    void shell.openExternal(url).catch(() => {})
    return { action: 'deny' }
  })

  view.webContents.on('page-title-updated', () => sync())
  view.webContents.on('page-favicon-updated', (_event, favicons) => {
    if (!browserTabs.has(tabId)) return
    updateBrowserTabState(tabId, (current) => ({
      ...current,
      faviconUrl: favicons[0] ?? getFallbackFaviconUrl(current.url),
    }))
  })
  view.webContents.on('did-start-loading', () => sync())
  view.webContents.on('did-stop-loading', () => sync())
  view.webContents.on('did-navigate', () => sync())
  view.webContents.on('did-navigate-in-page', () => sync())
  view.webContents.on('did-redirect-navigation', () => sync())
  view.webContents.on('did-fail-load', (_event, errorCode, errorDescription, validatedURL, isMainFrame) => {
    if (!isMainFrame || FAILED_LOAD_IGNORED_CODES.has(errorCode)) return
    updateBrowserTabState(tabId, (current) => ({
      ...current,
      url: validatedURL?.trim() || current.url,
      loading: false,
      error: errorDescription || '页面加载失败',
      canGoBack: view.webContents.canGoBack(),
      canGoForward: view.webContents.canGoForward(),
    }))
  })
  view.webContents.on('will-navigate', (event, url) => {
    if (parseHttpUrl(url)) return
    event.preventDefault()
    void shell.openExternal(url).catch(() => {})
  })

  return view
}

function nextBrowserTabId(): string {
  return `browser-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

export function initializeBrowserTabs(getWindowValue: () => BrowserWindow | null): void {
  getWindowRef = getWindowValue
}

export function setBrowserTabsStateListener(listener: ((snapshot: BrowserTabsSnapshot) => void) | null): void {
  stateListener = listener
}

export function listBrowserTabs(): BrowserTabsSnapshot {
  return snapshotBrowserTabs()
}

export function createBrowserTab(): BrowserTabState {
  const id = nextBrowserTabId()
  const view = createBrowserView(id)
  const record: BrowserTabRecord = {
    view,
    state: {
      id,
      title: 'New Tab',
      url: '',
      faviconUrl: null,
      loading: false,
      error: null,
      canGoBack: false,
      canGoForward: false,
    },
  }
  browserTabs.set(id, record)
  emitState()
  return { ...record.state }
}

export async function navigateBrowserTab(tabId: string, rawUrl: string): Promise<BrowserTabState> {
  const record = getBrowserTabRecord(tabId)
  const normalized = normalizeBrowserUrl(rawUrl)
  if (!normalized.url) {
    return updateBrowserTabState(tabId, (current) => ({
      ...current,
      error: normalized.error,
      loading: false,
    }))
  }

  updateBrowserTabState(tabId, (current) => ({
    ...current,
    url: normalized.url ?? current.url,
    faviconUrl: getFallbackFaviconUrl(normalized.url ?? current.url),
    error: null,
    loading: true,
  }))

  try {
    await record.view.webContents.loadURL(normalized.url)
  } catch (error) {
    return updateBrowserTabState(tabId, (current) => ({
      ...current,
      url: normalized.url ?? current.url,
      loading: false,
      error: error instanceof Error ? error.message : String(error),
      canGoBack: record.view.webContents.canGoBack(),
      canGoForward: record.view.webContents.canGoForward(),
    }))
  }

  syncStateFromWebContents(tabId)
  return { ...getBrowserTabRecord(tabId).state }
}

export async function reloadBrowserTab(tabId: string): Promise<BrowserTabState> {
  const record = getBrowserTabRecord(tabId)
  if (!record.state.url) {
    return updateBrowserTabState(tabId, (current) => ({
      ...current,
      error: '请输入网址',
      loading: false,
    }))
  }
  updateBrowserTabState(tabId, (current) => ({
    ...current,
    error: null,
    loading: true,
  }))
  record.view.webContents.reload()
  syncStateFromWebContents(tabId)
  return { ...getBrowserTabRecord(tabId).state }
}

export function goBackBrowserTab(tabId: string): BrowserTabState {
  const record = getBrowserTabRecord(tabId)
  if (record.view.webContents.canGoBack()) {
    record.view.webContents.goBack()
  }
  syncStateFromWebContents(tabId)
  return { ...getBrowserTabRecord(tabId).state }
}

export function goForwardBrowserTab(tabId: string): BrowserTabState {
  const record = getBrowserTabRecord(tabId)
  if (record.view.webContents.canGoForward()) {
    record.view.webContents.goForward()
  }
  syncStateFromWebContents(tabId)
  return { ...getBrowserTabRecord(tabId).state }
}

export function closeBrowserTab(tabId: string): BrowserTabsSnapshot {
  const record = getBrowserTabRecord(tabId)
  if (visibleTabId === tabId) {
    hideBrowserTabView()
  }
  browserTabs.delete(tabId)
  try {
    record.view.webContents.close()
  } catch {}
  emitState()
  return snapshotBrowserTabs()
}

export function showBrowserTabView(tabId: string, bounds: BrowserTabBounds): { ok: boolean } {
  getBrowserTabRecord(tabId)
  visibleTabId = tabId
  visibleBounds = bounds
  syncVisibleBrowserView()
  return { ok: true }
}

export function hideBrowserTabView(): { ok: boolean } {
  detachVisibleBrowserView()
  visibleTabId = null
  visibleBounds = null
  return { ok: true }
}

export function syncBrowserTabViewBounds(tabId: string, bounds: BrowserTabBounds): { ok: boolean } {
  getBrowserTabRecord(tabId)
  visibleTabId = tabId
  visibleBounds = bounds
  syncVisibleBrowserView()
  return { ok: true }
}

export function closeAllBrowserTabs(): void {
  hideBrowserTabView()
  for (const tabId of Array.from(browserTabs.keys())) {
    const record = browserTabs.get(tabId)
    if (!record) continue
    browserTabs.delete(tabId)
    try {
      record.view.webContents.close()
    } catch {}
  }
  emitState()
}
