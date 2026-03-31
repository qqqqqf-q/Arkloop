export type ConnectionMode = 'local' | 'saas' | 'self-hosted'
export type LocalPortMode = 'auto' | 'manual'

export type FetchProvider = 'none' | 'jina' | 'basic' | 'firecrawl'
export type SearchProvider = 'none' | 'duckduckgo' | 'tavily' | 'searxng'

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

export type OpenVikingDesktopConfig = {
  rootApiKey?: string
  embeddingSelector?: string
  embeddingProvider?: string
  embeddingModel?: string
  embeddingApiKey?: string
  embeddingApiBase?: string
  embeddingDimension?: number
  vlmSelector?: string
  vlmProvider?: string
  vlmModel?: string
  vlmApiKey?: string
  vlmApiBase?: string
}

export type MemoryConfig = {
  enabled: boolean
  provider: MemoryProvider
  memoryCommitEachTurn?: boolean
  openviking?: OpenVikingDesktopConfig
}

export type VoiceConfig = {
  enabled: boolean
  language?: string
}

export type NetworkConfig = {
  proxyEnabled: boolean
  proxyUrl?: string
  requestTimeoutMs?: number
  retryCount?: number
  userAgent?: string
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
  network: NetworkConfig
  voice?: VoiceConfig
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

export type UpdaterComponentStatus = {
  current: string | null
  latest: string | null
  available: boolean
}

export type AppUpdaterState = {
  supported: boolean
  phase: 'idle' | 'unsupported' | 'checking' | 'available' | 'not-available' | 'downloading' | 'downloaded' | 'error'
  currentVersion: string
  latestVersion: string | null
  progressPercent: number
  error: string | null
}

export type UpdaterStatus = {
  openviking: UpdaterComponentStatus
  sandbox: { kernel: UpdaterComponentStatus; rootfs: UpdaterComponentStatus }
}

export type UpdaterComponent = 'openviking' | 'sandbox_kernel' | 'sandbox_rootfs'

export type ArkloopDesktopApi = {
  isDesktop: true
  config: {
    get: () => Promise<DesktopConfig>
    set: (config: DesktopConfig) => Promise<{ ok: boolean }>
    getPath: () => Promise<string>
    onChanged: (callback: (config: DesktopConfig) => void) => () => void
  }
  advanced?: {
    getOverview: () => Promise<DesktopAdvancedOverview>
    chooseDataFolder: () => Promise<string | null>
    exportDataBundle: () => Promise<DesktopExportResult>
    importDataBundle: () => Promise<DesktopImportResult>
    listLogs: (input?: DesktopLogQuery) => Promise<{ entries: DesktopLogEntry[] }>
  }
  updater?: {
    check: () => Promise<UpdaterStatus>
    apply: (opts: { component: UpdaterComponent }) => Promise<{ ok: boolean }>
    onProgress: (callback: (data: { phase: string; percent: number; bytesDownloaded: number; bytesTotal: number; error?: string; component: UpdaterComponent }) => void) => () => void
  }
  appUpdater?: {
    getState: () => Promise<AppUpdaterState>
    check: () => Promise<AppUpdaterState>
    download: () => Promise<AppUpdaterState>
    install: () => Promise<{ ok: boolean }>
    onState: (callback: (state: AppUpdaterState) => void) => () => void
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
    getOsUsername?: () => Promise<string>
  }
  logs?: {
    getDir: () => Promise<string>
    getFiles: () => Promise<{ main: string; sidecar: string }>
  }
  dialog?: {
    openFolder: () => Promise<string | null>
  }
  fs?: {
    listDir: (folderPath: string, subPath?: string) => Promise<{ entries: LocalFileEntry[] }>
    readFile: (folderPath: string, relativePath: string) => Promise<{ data: string; mime_type: string } | { error: string }>
  }
}

export type LocalFileEntry = {
  name: string
  path: string
  type: 'file' | 'dir'
  size?: number
  mtime_unix_ms?: number
}

export type DesktopOverviewLink = {
  label: string
  url: string
}

export type DesktopUsageSummary = {
  account_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  record_count: number
}

export type DesktopOverviewItem = {
  label: string
  value: string
  tone?: 'default' | 'success' | 'warning' | 'danger'
}

export type DesktopAdvancedOverview = {
  appName: string
  appVersion: string
  githubUrl: string
  telegramUrl: string | null
  iconPath: string | null
  configPath: string
  dataDir: string
  logsDir: string
  sqlitePath: string
  links: DesktopOverviewLink[]
  status: DesktopOverviewItem[]
  usage: DesktopUsageSummary | null
}

export type DesktopExportResult = {
  ok: boolean
  filePath: string
}

export type DesktopImportResult = {
  ok: boolean
  importedFrom: string
}

export type DesktopLogLevel = 'info' | 'warn' | 'error' | 'debug' | 'other'

export type DesktopLogEntry = {
  timestamp: string
  level: DesktopLogLevel
  source: 'main' | 'sidecar'
  message: string
  raw: string
}

export type DesktopLogQuery = {
  source?: 'all' | 'main' | 'sidecar'
  level?: 'all' | DesktopLogLevel
  search?: string
  limit?: number
}

export function isDesktop(): boolean {
  // Electron preload 同时注入 arkloop API 与 __ARKLOOP_DESKTOP__；dev/hmr 下偶发只其一可用。
  const g = globalThis as Record<string, unknown>
  return !!(g.arkloop || g.__ARKLOOP_DESKTOP__)
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
