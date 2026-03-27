export type ConnectionMode = 'local' | 'saas' | 'self-hosted'
export type LocalPortMode = 'auto' | 'manual'

export type FetchProvider = 'jina' | 'basic' | 'firecrawl'
export type SearchProvider = 'duckduckgo' | 'tavily' | 'searxng'

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

export type LocalConfig = {
  port: number
  portMode: LocalPortMode
}

/** applyConfigUpdate 可选行为（仅 Electron 主进程使用） */
export type ApplyConfigUpdateOptions = {
  /** 本地模式：无论记忆字段是否变化都重启 sidecar，使 Worker 重读 ARKLOOP_MEMORY_* / OPENVIKING 等环境 */
  forceLocalSidecarRestart?: boolean
}

export type AppConfig = {
  mode: ConnectionMode
  saas: { baseUrl: string }
  selfHosted: { baseUrl: string }
  local: LocalConfig
  window: { width: number; height: number }
  onboarding_completed: boolean
  connectors_migrated: boolean
  connectors: ConnectorsConfig
  memory: MemoryConfig
  voice?: VoiceConfig
}

export const DEFAULT_CONFIG: AppConfig = {
  mode: 'local',
  saas: { baseUrl: 'https://api.arkloop.com' },
  selfHosted: { baseUrl: '' },
  local: { port: 19001, portMode: 'auto' },
  window: { width: 1280, height: 800 },
  onboarding_completed: false,
  connectors_migrated: false,
  connectors: {
    fetch: { provider: 'jina' },
    search: { provider: 'duckduckgo' },
  },
  memory: { enabled: true, provider: 'local', memoryCommitEachTurn: true },
}
