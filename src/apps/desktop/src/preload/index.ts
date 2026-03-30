import { contextBridge, ipcRenderer } from 'electron'

export type ConnectionMode = 'local' | 'saas' | 'self-hosted'
export type LocalPortMode = 'auto' | 'manual'

export type FetchProvider = 'none' | 'jina' | 'basic' | 'firecrawl'
export type SearchProvider = 'none' | 'duckduckgo' | 'tavily' | 'searxng'

export type ConnectorsConfig = {
  fetch: {
    provider: FetchProvider
    jinaApiKey?: string
    firecrawlApiKey?: string
    firecrawlBaseUrl?: string
  }
  search: {
    provider: SearchProvider
    tavilyApiKey?: string
    searxngBaseUrl?: string
  }
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

export type MemoryEntry = {
  id: string
  scope: string
  category: string
  key: string
  content: string
  created_at: string
}

export type AppConfig = {
  mode: ConnectionMode
  saas: { baseUrl: string }
  selfHosted: { baseUrl: string }
  local: { port: number; portMode: LocalPortMode }
  window: { width: number; height: number }
  onboarding_completed: boolean
  connectors: ConnectorsConfig
  memory: MemoryConfig
  voice?: VoiceConfig
}

export type SidecarStatus = 'stopped' | 'starting' | 'running' | 'crashed'
export type SidecarRuntime = {
  status: SidecarStatus
  port: number | null
  portMode: LocalPortMode
  lastError?: string
}

export type DownloadProgress = {
  phase: 'connecting' | 'downloading' | 'verifying' | 'done' | 'error'
  percent: number
  bytesDownloaded: number
  bytesTotal: number
  error?: string
}

export type SidecarVersionInfo = {
  current: string | null
  latest: string | null
  updateAvailable: boolean
}

export type LocalFileEntry = {
  name: string
  path: string
  type: 'file' | 'dir'
  size?: number
  mtime_unix_ms?: number
}

export type LocalDirResult = { entries: LocalFileEntry[] }
export type LocalFileResult = { data: string; mime_type: string } | { error: string }

export type UpdaterComponentStatus = {
  current: string | null
  latest: string | null
  available: boolean
}

export type UpdaterStatus = {
  sidecar: UpdaterComponentStatus
  openviking: UpdaterComponentStatus
  sandbox: { kernel: UpdaterComponentStatus; rootfs: UpdaterComponentStatus }
}

export type UpdaterComponent = 'sidecar' | 'openviking' | 'sandbox_kernel' | 'sandbox_rootfs'

export type ArkloopDesktopApi = {
  isDesktop: true
  config: {
    get: () => Promise<AppConfig>
    set: (config: AppConfig) => Promise<{ ok: boolean }>
    getPath: () => Promise<string>
    onChanged: (callback: (config: AppConfig) => void) => () => void
  }
  updater: {
    check: () => Promise<UpdaterStatus>
    apply: (opts: { component: UpdaterComponent }) => Promise<{ ok: boolean }>
    onProgress: (callback: (data: DownloadProgress & { component: UpdaterComponent }) => void) => () => void
  }
  dialog: {
    openFolder: () => Promise<string | null>
  }
  sidecar: {
    getStatus: () => Promise<SidecarStatus>
    getRuntime: () => Promise<SidecarRuntime>
    restart: () => Promise<SidecarStatus>
    download: () => Promise<{ ok: boolean }>
    isAvailable: () => Promise<boolean>
    checkUpdate: () => Promise<SidecarVersionInfo>
    onStatusChanged: (callback: (status: SidecarStatus) => void) => () => void
    onRuntimeChanged: (callback: (runtime: SidecarRuntime) => void) => () => void
    onDownloadProgress: (callback: (progress: DownloadProgress) => void) => () => void
  }
  onboarding: {
    getStatus: () => Promise<{ completed: boolean }>
    complete: () => Promise<{ ok: boolean }>
  }
  connectors: {
    get: () => Promise<ConnectorsConfig>
    set: (config: ConnectorsConfig) => Promise<{ ok: boolean }>
  }
  memory: {
    getConfig: () => Promise<MemoryConfig>
    setConfig: (config: MemoryConfig) => Promise<{ ok: boolean }>
    list: (agentId?: string) => Promise<{ entries: MemoryEntry[] }>
    delete: (id: string, agentId?: string) => Promise<{ status: string }>
    getSnapshot: (agentId?: string) => Promise<{ memory_block: string }>
  }
  app: {
    getVersion: () => Promise<string>
    quit: () => Promise<void>
    getOsUsername: () => Promise<string>
    openExternal: (url: string) => Promise<void>
  }
  logs: {
    getDir: () => Promise<string>
    getFiles: () => Promise<{ main: string; sidecar: string }>
  }
  fs: {
    listDir: (folderPath: string, subPath?: string) => Promise<LocalDirResult>
    readFile: (folderPath: string, relativePath: string) => Promise<LocalFileResult>
  }
}

// 同步注入 __ARKLOOP_DESKTOP__, 必须在页面脚本执行前完成
const config = ipcRenderer.sendSync('arkloop:config:get-sync') as {
  mode: string
  saas: { baseUrl: string }
  selfHosted: { baseUrl: string }
  local: { port: number; portMode: LocalPortMode }
  desktopAccessToken?: string
  bridgeBaseUrl?: string
}

let configSnapshot: AppConfig = config as AppConfig
let sidecarRuntimeSnapshot: SidecarRuntime = {
  status: 'stopped',
  port: config.local.port,
  portMode: config.local.portMode,
}
let bridgeBaseUrlSnapshot = config.bridgeBaseUrl ?? 'http://127.0.0.1:19003'

function computeApiBaseUrl(nextConfig: AppConfig, runtime: SidecarRuntime): string {
  if (nextConfig.mode === 'local') {
    const port = runtime.port ?? nextConfig.local.port
    return `http://127.0.0.1:${port}`
  }
  if (nextConfig.mode === 'saas') {
    return nextConfig.saas.baseUrl
  }
  if (nextConfig.mode === 'self-hosted') {
    return nextConfig.selfHosted.baseUrl
  }
  return ''
}

function getCurrentApiBaseUrl(): string {
  return computeApiBaseUrl(configSnapshot, sidecarRuntimeSnapshot)
}

contextBridge.exposeInMainWorld('__ARKLOOP_DESKTOP__', {
  apiBaseUrl: getCurrentApiBaseUrl(),
  bridgeBaseUrl: bridgeBaseUrlSnapshot,
  accessToken: config.desktopAccessToken ?? 'arkloop-desktop-local-token',
  mode: configSnapshot.mode,
  getApiBaseUrl: () => getCurrentApiBaseUrl(),
  getBridgeBaseUrl: () => bridgeBaseUrlSnapshot,
  getAccessToken: () => config.desktopAccessToken ?? 'arkloop-desktop-local-token',
  getMode: () => configSnapshot.mode,
})

ipcRenderer.on('arkloop:config:changed', (_event: Electron.IpcRendererEvent, nextConfig: AppConfig) => {
  configSnapshot = nextConfig
})

ipcRenderer.on('arkloop:sidecar:runtime-changed', (_event: Electron.IpcRendererEvent, runtime: SidecarRuntime) => {
  sidecarRuntimeSnapshot = runtime
})

ipcRenderer.on('arkloop:bridge:url-changed', (_event: Electron.IpcRendererEvent, bridgeBaseUrl: string) => {
  bridgeBaseUrlSnapshot = bridgeBaseUrl
})

ipcRenderer.on('arkloop:app:open-settings', () => {
  window.dispatchEvent(new CustomEvent('arkloop:app:open-settings'))
})

const api: ArkloopDesktopApi = {
  isDesktop: true,

  config: {
    get: () => ipcRenderer.invoke('arkloop:config:get'),
    set: (config) => ipcRenderer.invoke('arkloop:config:set', config),
    getPath: () => ipcRenderer.invoke('arkloop:config:path'),
    onChanged: (callback) => {
      const handler = (_event: Electron.IpcRendererEvent, config: AppConfig) => callback(config)
      ipcRenderer.on('arkloop:config:changed', handler)
      return () => ipcRenderer.removeListener('arkloop:config:changed', handler)
    },
  },

  updater: {
    check: () => ipcRenderer.invoke('arkloop:updater:check'),
    apply: (opts) => ipcRenderer.invoke('arkloop:updater:apply', opts),
    onProgress: (callback) => {
      const handler = (_event: Electron.IpcRendererEvent, data: DownloadProgress & { component: UpdaterComponent }) => callback(data)
      ipcRenderer.on('arkloop:updater:progress', handler)
      return () => ipcRenderer.removeListener('arkloop:updater:progress', handler)
    },
  },

  sidecar: {
    getStatus: () => ipcRenderer.invoke('arkloop:sidecar:status'),
    getRuntime: () => ipcRenderer.invoke('arkloop:sidecar:runtime'),
    restart: () => ipcRenderer.invoke('arkloop:sidecar:restart'),
    download: () => ipcRenderer.invoke('arkloop:sidecar:download'),
    isAvailable: () => ipcRenderer.invoke('arkloop:sidecar:is-available'),
    checkUpdate: () => ipcRenderer.invoke('arkloop:sidecar:check-update'),
    onStatusChanged: (callback) => {
      const handler = (_event: Electron.IpcRendererEvent, status: SidecarStatus) => callback(status)
      ipcRenderer.on('arkloop:sidecar:status-changed', handler)
      return () => ipcRenderer.removeListener('arkloop:sidecar:status-changed', handler)
    },
    onRuntimeChanged: (callback) => {
      const handler = (_event: Electron.IpcRendererEvent, runtime: SidecarRuntime) => callback(runtime)
      ipcRenderer.on('arkloop:sidecar:runtime-changed', handler)
      return () => ipcRenderer.removeListener('arkloop:sidecar:runtime-changed', handler)
    },
    onDownloadProgress: (callback) => {
      const handler = (_event: Electron.IpcRendererEvent, progress: DownloadProgress) => callback(progress)
      ipcRenderer.on('arkloop:sidecar:download-progress', handler)
      return () => ipcRenderer.removeListener('arkloop:sidecar:download-progress', handler)
    },
  },

  onboarding: {
    getStatus: () => ipcRenderer.invoke('arkloop:onboarding:status'),
    complete: () => ipcRenderer.invoke('arkloop:onboarding:complete'),
  },

  connectors: {
    get: () => ipcRenderer.invoke('arkloop:connectors:get'),
    set: (config: ConnectorsConfig) => ipcRenderer.invoke('arkloop:connectors:set', config),
  },

  memory: {
    getConfig: () => ipcRenderer.invoke('arkloop:memory:get-config'),
    setConfig: (config: MemoryConfig) => ipcRenderer.invoke('arkloop:memory:set-config', config),
    list: (agentId?: string) => ipcRenderer.invoke('arkloop:memory:list', agentId),
    delete: (id: string, agentId?: string) => ipcRenderer.invoke('arkloop:memory:delete', id, agentId),
    getSnapshot: (agentId?: string) => ipcRenderer.invoke('arkloop:memory:get-snapshot', agentId),
  },

  app: {
    getVersion: () => ipcRenderer.invoke('arkloop:app:version'),
    quit: () => ipcRenderer.invoke('arkloop:app:quit'),
    getOsUsername: () => ipcRenderer.invoke('arkloop:app:os-username'),
    openExternal: (url: string) => ipcRenderer.invoke('arkloop:app:open-external', url),
  },

  logs: {
    getDir: () => ipcRenderer.invoke('arkloop:logs:dir'),
    getFiles: () => ipcRenderer.invoke('arkloop:logs:files'),
  },

  dialog: {
    openFolder: () => ipcRenderer.invoke('arkloop:dialog:open-folder'),
  },

  fs: {
    listDir: (folderPath: string, subPath = '/') => ipcRenderer.invoke('arkloop:fs:list-dir', folderPath, subPath),
    readFile: (folderPath: string, relativePath: string) => ipcRenderer.invoke('arkloop:fs:read-file', folderPath, relativePath),
  },
}

contextBridge.exposeInMainWorld('arkloop', api)
