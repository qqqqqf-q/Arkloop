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

export type OpenVikingDesktopConfig = {
  rootApiKey?: string
  embeddingProvider?: string
  embeddingModel?: string
  embeddingApiKey?: string
  embeddingApiBase?: string
  embeddingDimension?: number
  vlmProvider?: string
  vlmModel?: string
  vlmApiKey?: string
  vlmApiBase?: string
}

export type MemoryConfig = {
  enabled: boolean
  provider: MemoryProvider
  openviking?: OpenVikingDesktopConfig
}

export type LocalConfig = {
  port: number
  portMode: LocalPortMode
}

export type AppConfig = {
  mode: ConnectionMode
  saas: { baseUrl: string }
  selfHosted: { baseUrl: string }
  local: LocalConfig
  window: { width: number; height: number }
  onboarding_completed: boolean
  connectors: ConnectorsConfig
  memory: MemoryConfig
}

export const DEFAULT_CONFIG: AppConfig = {
  mode: 'local',
  saas: { baseUrl: 'https://api.arkloop.com' },
  selfHosted: { baseUrl: '' },
  local: { port: 19001, portMode: 'auto' },
  window: { width: 1280, height: 800 },
  onboarding_completed: false,
  connectors: {
    fetch: { provider: 'jina' },
    search: { provider: 'browser' },
  },
  memory: { enabled: true, provider: 'local' },
}
