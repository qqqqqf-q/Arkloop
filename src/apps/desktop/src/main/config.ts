import * as fs from 'fs'
import * as path from 'path'
import * as os from 'os'
import { DEFAULT_CONFIG } from './types'
import type {
  AppConfig,
  ConnectionMode,
  ConnectorsConfig,
  FetchConnectorConfig,
  FetchProvider,
  LocalConfig,
  LocalPortMode,
  MemoryConfig,
  MemoryProvider,
  OpenVikingDesktopConfig,
  SearchConnectorConfig,
  SearchProvider,
} from './types'

const CONFIG_DIR = path.join(os.homedir(), '.arkloop')
const CONFIG_PATH = path.join(CONFIG_DIR, 'config.json')

function normalizeConnectionMode(mode: unknown): ConnectionMode {
  return mode === 'saas' || mode === 'self-hosted' || mode === 'local'
    ? mode
    : DEFAULT_CONFIG.mode
}

function normalizePort(port: unknown): number {
  if (typeof port === 'number' && Number.isInteger(port) && port > 0 && port <= 65535) {
    return port
  }
  return DEFAULT_CONFIG.local.port
}

function normalizePortMode(mode: unknown): LocalPortMode {
  return mode === 'manual' ? 'manual' : 'auto'
}

function normalizeLocalConfig(local: unknown): LocalConfig {
  const raw = (local && typeof local === 'object') ? local as Partial<LocalConfig> : {}
  return {
    port: normalizePort(raw.port),
    portMode: normalizePortMode(raw.portMode),
  }
}

function normalizeFetchProvider(p: unknown): FetchProvider {
  return p === 'basic' || p === 'firecrawl' ? p : 'jina'
}

function normalizeSearchProvider(p: unknown): SearchProvider {
  if (p === 'browser') return 'duckduckgo'
  if (p === 'tavily' || p === 'searxng' || p === 'duckduckgo') return p
  return 'duckduckgo'
}

function normalizeFetchConnector(raw: unknown): FetchConnectorConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<FetchConnectorConfig> : {}
  return {
    provider: normalizeFetchProvider(r.provider),
    ...(typeof r.jinaApiKey === 'string' && r.jinaApiKey ? { jinaApiKey: r.jinaApiKey } : {}),
    ...(typeof r.firecrawlApiKey === 'string' && r.firecrawlApiKey ? { firecrawlApiKey: r.firecrawlApiKey } : {}),
    ...(typeof r.firecrawlBaseUrl === 'string' && r.firecrawlBaseUrl ? { firecrawlBaseUrl: r.firecrawlBaseUrl } : {}),
  }
}

function normalizeSearchConnector(raw: unknown): SearchConnectorConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<SearchConnectorConfig> : {}
  return {
    provider: normalizeSearchProvider(r.provider),
    ...(typeof r.tavilyApiKey === 'string' && r.tavilyApiKey ? { tavilyApiKey: r.tavilyApiKey } : {}),
    ...(typeof r.searxngBaseUrl === 'string' && r.searxngBaseUrl ? { searxngBaseUrl: r.searxngBaseUrl } : {}),
  }
}

function normalizeConnectors(raw: unknown): ConnectorsConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<ConnectorsConfig> : {}
  return {
    fetch: normalizeFetchConnector(r.fetch),
    search: normalizeSearchConnector(r.search),
  }
}

function normalizeMemoryProvider(p: unknown): MemoryProvider {
  return p === 'openviking' ? 'openviking' : 'local'
}

function normalizeStr(v: unknown): string | undefined {
  return typeof v === 'string' && v.trim() ? v.trim() : undefined
}

function normalizeOpenVikingConfig(raw: unknown): OpenVikingDesktopConfig | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const r = raw as Partial<OpenVikingDesktopConfig>
  const out: OpenVikingDesktopConfig = {}
  const s = normalizeStr
  if (s(r.rootApiKey)) out.rootApiKey = s(r.rootApiKey)
  if (s(r.embeddingSelector)) out.embeddingSelector = s(r.embeddingSelector)
  if (s(r.embeddingProvider)) out.embeddingProvider = s(r.embeddingProvider)
  if (s(r.embeddingModel)) out.embeddingModel = s(r.embeddingModel)
  if (s(r.embeddingApiKey)) out.embeddingApiKey = s(r.embeddingApiKey)
  if (s(r.embeddingApiBase)) out.embeddingApiBase = s(r.embeddingApiBase)
  if (typeof r.embeddingDimension === 'number' && r.embeddingDimension > 0) {
    out.embeddingDimension = r.embeddingDimension
  }
  if (s(r.vlmSelector)) out.vlmSelector = s(r.vlmSelector)
  if (s(r.vlmProvider)) out.vlmProvider = s(r.vlmProvider)
  if (s(r.vlmModel)) out.vlmModel = s(r.vlmModel)
  if (s(r.vlmApiKey)) out.vlmApiKey = s(r.vlmApiKey)
  if (s(r.vlmApiBase)) out.vlmApiBase = s(r.vlmApiBase)
  return out
}

function normalizeMemory(raw: unknown): MemoryConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<MemoryConfig> : {}
  return {
    // default true to avoid breaking existing installs without the field
    enabled: r.enabled === false ? false : true,
    provider: normalizeMemoryProvider(r.provider),
    memoryCommitEachTurn: r.memoryCommitEachTurn === false ? false : true,
    openviking: normalizeOpenVikingConfig(r.openviking),
  }
}

export function normalizeConfig(config: Partial<AppConfig> | null | undefined): AppConfig {
  const parsed = config ?? {}
  return {
    mode: normalizeConnectionMode(parsed.mode),
    saas: {
      ...DEFAULT_CONFIG.saas,
      ...(parsed.saas ?? {}),
    },
    selfHosted: {
      ...DEFAULT_CONFIG.selfHosted,
      ...(parsed.selfHosted ?? {}),
    },
    local: normalizeLocalConfig(parsed.local),
    window: {
      ...DEFAULT_CONFIG.window,
      ...(parsed.window ?? {}),
    },
    onboarding_completed: typeof parsed.onboarding_completed === 'boolean'
      ? parsed.onboarding_completed
      : DEFAULT_CONFIG.onboarding_completed,
    connectors: normalizeConnectors(parsed.connectors),
    memory: normalizeMemory(parsed.memory),
  }
}

export function loadConfig(): AppConfig {
  try {
    const raw = fs.readFileSync(CONFIG_PATH, 'utf-8')
    const parsed = JSON.parse(raw) as Partial<AppConfig>
    return normalizeConfig(parsed)
  } catch {
    return normalizeConfig(undefined)
  }
}

export function saveConfig(config: AppConfig): void {
  fs.mkdirSync(CONFIG_DIR, { recursive: true })
  fs.writeFileSync(CONFIG_PATH, JSON.stringify(normalizeConfig(config), null, 2), 'utf-8')
}

export function getConfigPath(): string {
  return CONFIG_PATH
}
