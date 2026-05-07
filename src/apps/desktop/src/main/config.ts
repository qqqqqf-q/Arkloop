import * as fs from 'fs'
import * as path from 'path'
import * as os from 'os'
import { DEFAULT_CONFIG } from './types'
import type {
  AppConfig,
  ConnectionMode,
  ConnectorsConfig,
  CloseWindowBehavior,
  DesktopPreferencesConfig,
  FetchConnectorConfig,
  FetchProvider,
  LocalConfig,
  LocalPortMode,
  MemoryConfig,
  MemoryProvider,
  NetworkConfig,
  NowledgeDesktopConfig,
  OpenVikingDesktopConfig,
  SearchConnectorConfig,
  SearchProvider,
  StartupOpenMode,
  VoiceConfig,
} from './types'

const CONFIG_DIR = path.join(os.homedir(), '.arkloop')
const CONFIG_PATH = path.join(CONFIG_DIR, 'config.json')
const VERSIONS_FILE = path.join(CONFIG_DIR, 'versions.json')
const LEGACY_SIDECAR_VERSION_FILE = path.join(CONFIG_DIR, 'bin', 'sidecar.version.json')
const LEGACY_OPENCLI_VERSION_FILE = path.join(CONFIG_DIR, 'bin', 'opencli.version.json')

export type VersionsState = {
  sidecar?: { version: string; updated_at: string }
  openviking?: { version: string; updated_at: string }
  opencli?: { version: string; updated_at: string }
  rtk?: { version: string; updated_at: string }
  update_check?: {
    checked_at: string
    openviking?: string | null
    sandbox_kernel?: string | null
    sandbox_rootfs?: string | null
    rtk?: string | null
    opencli?: string | null
  }
  sandbox?: {
    kernel?: { version: string; updated_at: string }
    rootfs?: { version: string; updated_at: string }
  }
}

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
  return p === 'none' || p === 'jina' || p === 'basic' || p === 'firecrawl'
    ? p
    : DEFAULT_CONFIG.connectors.fetch.provider
}

function normalizeSearchProvider(p: unknown): SearchProvider {
  if (p === 'browser' || p === 'duckduckgo') return 'basic'
  if (p === 'none' || p === 'basic' || p === 'tavily' || p === 'searxng') return p
  return DEFAULT_CONFIG.connectors.search.provider
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
  return p === 'openviking'
    ? 'openviking'
    : p === 'nowledge'
      ? 'nowledge'
      : 'notebook'
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
  if (s(r.rerankSelector)) out.rerankSelector = s(r.rerankSelector)
  if (s(r.rerankProvider)) out.rerankProvider = s(r.rerankProvider)
  if (s(r.rerankModel)) out.rerankModel = s(r.rerankModel)
  if (s(r.rerankApiKey)) out.rerankApiKey = s(r.rerankApiKey)
  if (s(r.rerankApiBase)) out.rerankApiBase = s(r.rerankApiBase)
  return out
}

function normalizeNowledgeConfig(raw: unknown): NowledgeDesktopConfig | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const r = raw as Partial<NowledgeDesktopConfig>
  const normalized: NowledgeDesktopConfig = {}
  if (typeof r.baseUrl === 'string' && r.baseUrl.trim()) normalized.baseUrl = r.baseUrl.trim()
  if (typeof r.apiKey === 'string' && r.apiKey.trim()) normalized.apiKey = r.apiKey.trim()
  if (typeof r.requestTimeoutMs === 'number' && Number.isFinite(r.requestTimeoutMs) && r.requestTimeoutMs > 0) {
    normalized.requestTimeoutMs = r.requestTimeoutMs
  }
  return Object.keys(normalized).length > 0 ? normalized : undefined
}

function normalizeMemory(raw: unknown): MemoryConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<MemoryConfig> : {}
  return {
    enabled: r.enabled === false ? false : true,
    provider: normalizeMemoryProvider(r.provider),
    memoryCommitEachTurn: r.memoryCommitEachTurn === false ? false : true,
    openviking: normalizeOpenVikingConfig(r.openviking),
    nowledge: normalizeNowledgeConfig(r.nowledge),
  }
}

function normalizeVoice(raw: unknown): VoiceConfig | undefined {
  const r = (raw && typeof raw === 'object') ? raw as Partial<VoiceConfig> : {}
  if (typeof r.enabled !== 'boolean') return undefined
  return { enabled: r.enabled, language: typeof r.language === 'string' ? r.language : undefined }
}

function normalizeTimeoutMs(value: unknown): number {
  if (typeof value === 'number' && Number.isInteger(value) && value >= 1000 && value <= 300000) {
    return value
  }
  return DEFAULT_CONFIG.network.requestTimeoutMs ?? 30000
}

function normalizeRetryCount(value: unknown): number {
  if (typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 10) {
    return value
  }
  return DEFAULT_CONFIG.network.retryCount ?? 1
}

function normalizeNetwork(raw: unknown): NetworkConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<NetworkConfig> : {}
  return {
    proxyEnabled: r.proxyEnabled === true,
    ...(typeof r.proxyUrl === 'string' && r.proxyUrl.trim() ? { proxyUrl: r.proxyUrl.trim() } : {}),
    requestTimeoutMs: normalizeTimeoutMs(r.requestTimeoutMs),
    retryCount: normalizeRetryCount(r.retryCount),
    ...(typeof r.userAgent === 'string' && r.userAgent.trim() ? { userAgent: r.userAgent.trim() } : {}),
  }
}

function normalizeStartupOpenMode(value: unknown): StartupOpenMode {
  return value === 'home' ? 'home' : 'last-workspace'
}

function normalizeCloseBehavior(value: unknown): CloseWindowBehavior {
  return value === 'quit' ? 'quit' : 'keep-in-background'
}

function normalizeDesktopPreferences(raw: unknown): DesktopPreferencesConfig {
  const r = (raw && typeof raw === 'object') ? raw as Partial<DesktopPreferencesConfig> : {}
  return {
    startupOpen: normalizeStartupOpenMode(r.startupOpen),
    closeBehavior: normalizeCloseBehavior(r.closeBehavior),
    launchAtLogin: r.launchAtLogin === true,
    desktopNotifications: r.desktopNotifications === false ? false : true,
    productUpdateNotifications: r.productUpdateNotifications === false ? false : true,
    keepScreenAwake: r.keepScreenAwake === true,
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
    connectors_migrated: typeof parsed.connectors_migrated === 'boolean'
      ? parsed.connectors_migrated
      : DEFAULT_CONFIG.connectors_migrated,
    connectors: normalizeConnectors(parsed.connectors),
    memory: normalizeMemory(parsed.memory),
    network: normalizeNetwork(parsed.network),
    desktop: normalizeDesktopPreferences(parsed.desktop),
    voice: normalizeVoice(parsed.voice),
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

type LegacyVersionState = {
  version?: string
  downloadedAt?: string
}

function readLegacyVersionState(filePath: string): { version: string; updated_at: string } | null {
  try {
    const raw = fs.readFileSync(filePath, 'utf-8')
    const parsed = JSON.parse(raw) as LegacyVersionState
    if (typeof parsed.version !== 'string' || !parsed.version.trim()) return null
    return {
      version: parsed.version.trim(),
      updated_at: parsed.downloadedAt ?? new Date().toISOString(),
    }
  } catch {
    return null
  }
}

export function loadVersionsFile(): VersionsState {
  try {
    const raw = fs.readFileSync(VERSIONS_FILE, 'utf-8')
    return JSON.parse(raw) as VersionsState
  } catch {
    return {}
  }
}

export function saveVersionsFile(versions: VersionsState): void {
  fs.mkdirSync(CONFIG_DIR, { recursive: true })
  fs.writeFileSync(VERSIONS_FILE, JSON.stringify(versions, null, 2), 'utf-8')
}

export function initVersionsFile(): void {
  const next = loadVersionsFile()
  let changed = false

  if (!next.sidecar) {
    const sidecarVersion = readLegacyVersionState(LEGACY_SIDECAR_VERSION_FILE)
    if (sidecarVersion) {
      next.sidecar = sidecarVersion
      changed = true
    }
  }

  if (!next.opencli) {
    const opencliVersion = readLegacyVersionState(LEGACY_OPENCLI_VERSION_FILE)
    if (opencliVersion) {
      next.opencli = opencliVersion
      changed = true
    }
  }

  if (changed) {
    saveVersionsFile(next)
  }
}
