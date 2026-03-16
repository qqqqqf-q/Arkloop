export type ConnectionMode = 'local' | 'saas' | 'self-hosted'
export type LocalPortMode = 'auto' | 'manual'

export type FetchProvider = 'jina' | 'basic' | 'firecrawl'
export type SearchProvider = 'browser' | 'tavily' | 'searxng'

export type FetchConnectorConfig = {
  provider: FetchProvider
  jinaApiKey?: string
  firecrawlApiKey?: string
  firecrawlBaseUrl?: string
}

export type SearchConnectorConfig = {
  provider: SearchProvider
  tavilyApiKey?: string
  searxngBaseUrl?: string
}

export type ConnectorsConfig = {
  fetch: FetchConnectorConfig
  search: SearchConnectorConfig
}

export type MemoryProvider = 'local' | 'openviking'

export type MemoryConfig = {
  provider: MemoryProvider
}

export type MemoryEntry = {
  id: string
  scope: string
  category: string
  key: string
  content: string
  created_at: string
}

export type DesktopConfig = {
  mode: ConnectionMode
  saas: { baseUrl: string }
  selfHosted: { baseUrl: string }
  local: { port: number; portMode: LocalPortMode }
  window: { width: number; height: number }
  onboarding_completed: boolean
  connectors: ConnectorsConfig
  memory: MemoryConfig
}

export type SidecarRuntime = {
  status: 'stopped' | 'starting' | 'running' | 'crashed'
  port: number | null
  portMode: LocalPortMode
  lastError?: string
}

type DesktopInfo = {
  apiBaseUrl?: string
  bridgeBaseUrl?: string
  accessToken?: string
  mode?: ConnectionMode
  getApiBaseUrl?: () => string
  getBridgeBaseUrl?: () => string
  getAccessToken?: () => string
  getMode?: () => ConnectionMode
}

export type ArkloopDesktopApi = {
  isDesktop: true
  config: {
    get: () => Promise<DesktopConfig>
    set: (config: DesktopConfig) => Promise<{ ok: boolean }>
    getPath: () => Promise<string>
    onChanged: (callback: (config: DesktopConfig) => void) => () => void
  }
  connectors?: {
    get: () => Promise<ConnectorsConfig>
    set: (config: ConnectorsConfig) => Promise<{ ok: boolean }>
  }
  memory?: {
    getConfig: () => Promise<MemoryConfig>
    setConfig: (config: MemoryConfig) => Promise<{ ok: boolean }>
    list: (agentId?: string) => Promise<{ entries: MemoryEntry[] }>
    delete: (id: string, agentId?: string) => Promise<{ status: string }>
    getSnapshot: (agentId?: string) => Promise<{ memory_block: string }>
  }
  sidecar: {
    getStatus: () => Promise<'stopped' | 'starting' | 'running' | 'crashed'>
    getRuntime: () => Promise<SidecarRuntime>
    restart: () => Promise<string>
    download: () => Promise<{ ok: boolean }>
    isAvailable: () => Promise<boolean>
    checkUpdate: () => Promise<{ current: string | null; latest: string | null; updateAvailable: boolean }>
    onStatusChanged: (callback: (status: string) => void) => () => void
    onRuntimeChanged: (callback: (runtime: SidecarRuntime) => void) => () => void
    onDownloadProgress: (callback: (progress: { phase: string; percent: number; bytesDownloaded: number; bytesTotal: number; error?: string }) => void) => () => void
  }
  onboarding: {
    getStatus: () => Promise<{ completed: boolean }>
    complete: () => Promise<{ ok: boolean }>
  }
  app: {
    getVersion: () => Promise<string>
    quit: () => Promise<void>
  }
  dialog?: {
    openFolder: () => Promise<string | null>
  }
}

export function isDesktop(): boolean {
  return !!(globalThis as Record<string, unknown>).arkloop
}

export function getDesktopApi(): ArkloopDesktopApi | null {
  const api = (globalThis as Record<string, unknown>).arkloop as ArkloopDesktopApi | undefined
  return api?.isDesktop ? api : null
}

function getDesktopInfo(): DesktopInfo | undefined {
  return (globalThis as Record<string, unknown>).__ARKLOOP_DESKTOP__ as DesktopInfo | undefined
}

export function getDesktopMode(): ConnectionMode | null {
  const info = getDesktopInfo()
  if (typeof info?.getMode === 'function') {
    return info.getMode() ?? null
  }
  return info?.mode ?? null
}

export function getDesktopAccessToken(): string | null {
  const info = getDesktopInfo()
  if (typeof info?.getAccessToken === 'function') {
    return info.getAccessToken() ?? null
  }
  return info?.accessToken ?? null
}

export function getDesktopBridgeBaseUrl(): string | null {
  const info = getDesktopInfo()
  if (typeof info?.getBridgeBaseUrl === 'function') {
    return info.getBridgeBaseUrl() ?? null
  }
  return info?.bridgeBaseUrl ?? null
}

export function isLocalMode(): boolean {
  return getDesktopMode() === 'local'
}
