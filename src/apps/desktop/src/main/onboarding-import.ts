import * as crypto from 'crypto'
import * as fs from 'fs/promises'
import * as http from 'http'
import * as https from 'https'
import * as os from 'os'
import * as path from 'path'
import JSON5 from 'json5'
import YAML from 'yaml'

export type ImportSourceKind = 'hermes' | 'openclaw'
export type ImportItemKey = 'identity' | 'skills' | 'mcp' | 'providers'

export type AgentImportDiscovery = {
  kind: ImportSourceKind
  name: string
  sourcePath: string
  skillsCount: number
  mcpServers: string[]
  llmProviders: string[]
}

export type OnboardingImportApplyRequest = {
  source: ImportSourceKind
  selection?: Partial<Record<ImportItemKey, boolean>>
}

export type OnboardingImportApplyResult = {
  ok: boolean
  imported: Record<ImportItemKey, number>
  errors: string[]
}

type ApiOptions = {
  apiBaseUrl: string | null
  token: string
}

type SourceDetails = AgentImportDiscovery & {
  homeDir: string
  config: Record<string, unknown>
  env: Record<string, string>
  workspaceDir?: string
  skillDirs: string[]
  providers: ProviderCandidate[]
  mcpConfigs: McpCandidate[]
}

type ProviderCandidate = {
  sourceName: string
  name: string
  provider: 'openai' | 'anthropic' | 'gemini'
  apiKey?: string
  baseUrl?: string
  openaiApiMode?: 'chat_completions' | 'responses'
  models: ModelCandidate[]
}

type ModelCandidate = {
  id: string
  name?: string
  contextWindow?: number
  maxTokens?: number
  reasoning?: boolean
  inputModalities?: string[]
}

type McpCandidate = {
  key: string
  displayName: string
  transport: 'stdio' | 'http_sse' | 'streamable_http'
  launchSpec: Record<string, unknown>
  envSecrets?: Record<string, string>
  authHeaders?: Record<string, string>
  bearerToken?: string
}

type LlmProviderResponse = {
  id: string
  provider: string
  name: string
  base_url: string | null
  openai_api_mode: string | null
  advanced_json?: Record<string, unknown> | null
  models: Array<{ model: string }>
}

type SkillPackageResponse = {
  skill_key: string
  version: string
}

type McpInstallResponse = {
  id: string
  install_key: string
}

class ApiRequestError extends Error {
  readonly status: number
  readonly code?: string

  constructor(status: number, message: string, code?: string) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
    this.code = code
  }
}

export async function detectOnboardingImportSources(): Promise<AgentImportDiscovery[]> {
  const sources = await Promise.all([loadHermesSource(), loadOpenClawSource()])
  return sources.filter((source): source is SourceDetails => source !== null).map(toDiscovery)
}

export async function applyOnboardingImport(
  request: OnboardingImportApplyRequest,
  options: ApiOptions,
): Promise<OnboardingImportApplyResult> {
  const source = request.source === 'hermes' ? await loadHermesSource() : await loadOpenClawSource()
  if (!source) {
    throw new Error('source not found')
  }

  const selection = normalizeSelection(request.selection)
  const imported = emptyImportCounts()
  const errors: string[] = []

  if (selection.identity) {
    if (!await runImportStep(errors, async () => {
      imported.identity = await importWorkspace(source)
    })) {
      return { ok: false, imported, errors }
    }
  }
  if (selection.skills) {
    if (!await runImportStep(errors, async () => {
      imported.skills = await importSkills(source, options)
    })) {
      return { ok: false, imported, errors }
    }
  }
  if (selection.providers) {
    if (!await runImportStep(errors, async () => {
      imported.providers = await importProviders(source, options)
    })) {
      return { ok: false, imported, errors }
    }
  }
  if (selection.mcp) {
    if (!await runImportStep(errors, async () => {
      imported.mcp = await importMcpServers(source, options)
    })) {
      return { ok: false, imported, errors }
    }
  }

  return { ok: errors.length === 0, imported, errors }
}

function toDiscovery(source: SourceDetails): AgentImportDiscovery {
  return {
    kind: source.kind,
    name: source.name,
    sourcePath: displayPath(source.homeDir),
    skillsCount: source.skillDirs.length,
    mcpServers: source.mcpConfigs.map((item) => item.displayName),
    llmProviders: source.providers.map((item) => item.name),
  }
}

async function loadHermesSource(): Promise<SourceDetails | null> {
  const homeDir = expandHomePath(process.env.HERMES_HOME || path.join(os.homedir(), '.hermes'))
  const configPath = path.join(homeDir, 'config.yaml')
  const config = await readYamlFile(configPath)
  const hasIdentity = await existsAny([
    path.join(homeDir, 'SOUL.md'),
    path.join(homeDir, 'AGENTS.md'),
    path.join(homeDir, 'memories', 'MEMORY.md'),
    path.join(homeDir, 'memories', 'USER.md'),
  ])
  if (!config && !hasIdentity) {
    return null
  }

  const env = await readDotenv(path.join(homeDir, '.env'))
  const configRoot = config ?? {}
  const modelConfig = asRecord(configRoot.model)
  const providerMap = asRecord(modelConfig?.providers) ?? asRecord(configRoot.providers) ?? {}
  const skillsConfig = asRecord(configRoot.skills)
  const externalDirs = stringArray(skillsConfig?.external_dirs).map(expandHomePath)
  const skillDirs = await collectSkillDirs([path.join(homeDir, 'skills'), ...externalDirs])
  const mcpRoot = asRecord(configRoot.mcp_servers) ?? asRecord(asRecord(configRoot.mcp)?.servers) ?? asRecord(configRoot.mcp) ?? {}

  return {
    kind: 'hermes',
    name: 'Hermes',
    sourcePath: displayPath(homeDir),
    homeDir,
    config: configRoot,
    env,
    skillDirs,
    providers: parseProviderMap(providerMap, env),
    mcpConfigs: parseMcpMap('hermes', mcpRoot, env),
    mcpServers: [],
    llmProviders: [],
    skillsCount: 0,
  }
}

async function loadOpenClawSource(): Promise<SourceDetails | null> {
  const stateDir = await resolveOpenClawStateDir()
  const configPath = await resolveOpenClawConfigPath(stateDir)
  const config = await readJson5File(configPath)
  const agentId = resolveOpenClawAgentId(config ?? {})
  const agentDir = resolveOpenClawAgentDir(config ?? {}, stateDir, agentId)
  const workspaceDir = resolveOpenClawWorkspaceDir(config ?? {}, stateDir, agentId)
  const hasWorkspace = await pathExists(workspaceDir)
  if (!config && !hasWorkspace) {
    return null
  }

  const agentModels = await readJson5File(path.join(agentDir, 'models.json'))
  const env = await loadOpenClawEnv(stateDir, config ?? {})
  const providerMap = {
    ...(asRecord(asRecord(config?.models)?.providers) ?? {}),
    ...(asRecord(agentModels?.providers) ?? {}),
  }
  const mcpRoot =
    asRecord(asRecord(config?.mcp)?.servers) ??
    asRecord(config?.mcpServers) ??
    asRecord(config?.mcp_servers) ??
    {}
  const skillDirs = await collectSkillDirs([
    path.join(workspaceDir, 'skills'),
    path.join(stateDir, 'skills'),
  ])

  return {
    kind: 'openclaw',
    name: 'OpenClaw',
    sourcePath: displayPath(stateDir),
    homeDir: stateDir,
    config: config ?? {},
    env,
    workspaceDir,
    skillDirs,
    providers: parseProviderMap(providerMap, env),
    mcpConfigs: parseMcpMap('openclaw', mcpRoot, env),
    mcpServers: [],
    llmProviders: [],
    skillsCount: 0,
  }
}

function resolveOpenClawAgentId(config: Record<string, unknown>): string {
  const agents = asRecord(config.agents)
  const explicit = asString(agents?.default) || asString(asRecord(config.agent)?.id) || asString(config.defaultAgent)
  if (explicit) {
    return normalizeOpenClawAgentId(explicit)
  }
  const entries = openClawAgentEntries(config)
  const defaultEntry = entries.find((entry) => asBoolean(entry.default))
  const chosen = defaultEntry ?? entries[0]
  return normalizeOpenClawAgentId(asString(chosen?.id) || 'main')
}

function resolveOpenClawWorkspaceDir(config: Record<string, unknown>, stateDir: string, agentId: string): string {
  const agent = resolveOpenClawAgentConfig(config, agentId)
  const configured = asString(agent?.workspace)
  if (configured) {
    return stripNullBytes(resolveOpenClawUserPath(configured))
  }

  const defaultAgentId = resolveOpenClawAgentId(config)
  const fallback = asString(asRecord(asRecord(config.agents)?.defaults)?.workspace)
  if (agentId === defaultAgentId) {
    return stripNullBytes(fallback ? resolveOpenClawUserPath(fallback) : resolveOpenClawDefaultAgentWorkspaceDir())
  }
  if (fallback) {
    return stripNullBytes(path.join(resolveOpenClawUserPath(fallback), agentId))
  }
  return stripNullBytes(path.join(stateDir, `workspace-${agentId}`))
}

function resolveOpenClawAgentDir(config: Record<string, unknown>, stateDir: string, agentId: string): string {
  const configured = asString(resolveOpenClawAgentConfig(config, agentId)?.agentDir)
  return configured ? resolveOpenClawUserPath(configured) : path.join(stateDir, 'agents', agentId, 'agent')
}

function resolveOpenClawAgentConfig(config: Record<string, unknown>, agentId: string): Record<string, unknown> | null {
  return openClawAgentEntries(config).find((entry) => normalizeOpenClawAgentId(asString(entry.id)) === agentId) ?? null
}

function openClawAgentEntries(config: Record<string, unknown>): Record<string, unknown>[] {
  const list = asRecord(config.agents)?.list
  if (!Array.isArray(list)) {
    return []
  }
  return list.filter((entry): entry is Record<string, unknown> => asRecord(entry) !== null)
}

function normalizeOpenClawAgentId(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return 'main'
  }
  const normalized = trimmed.toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 64)
  return normalized || 'main'
}

async function resolveOpenClawStateDir(): Promise<string> {
  const override = process.env.OPENCLAW_STATE_DIR?.trim()
  if (override) {
    return resolveOpenClawUserPath(override)
  }
  const home = resolveOpenClawHomeDir()
  const stateDir = path.join(home, '.openclaw')
  if (process.env.OPENCLAW_TEST_FAST === '1' || await pathExists(stateDir)) {
    return stateDir
  }
  const legacyDir = path.join(home, '.clawdbot')
  return await pathExists(legacyDir) ? legacyDir : stateDir
}

async function resolveOpenClawConfigPath(stateDir: string): Promise<string> {
  const override = process.env.OPENCLAW_CONFIG_PATH?.trim()
  if (override) {
    return resolveOpenClawUserPath(override)
  }
  const candidates = [
    path.join(stateDir, 'openclaw.json'),
    path.join(stateDir, 'clawdbot.json'),
  ]
  for (const candidate of candidates) {
    if (await pathExists(candidate)) {
      return candidate
    }
  }
  if (process.env.OPENCLAW_STATE_DIR?.trim()) {
    return candidates[0]
  }
  const home = resolveOpenClawHomeDir()
  const defaultCandidates = [
    path.join(home, '.openclaw', 'openclaw.json'),
    path.join(home, '.openclaw', 'clawdbot.json'),
    path.join(home, '.clawdbot', 'openclaw.json'),
    path.join(home, '.clawdbot', 'clawdbot.json'),
  ]
  for (const candidate of defaultCandidates) {
    if (await pathExists(candidate)) {
      return candidate
    }
  }
  return candidates[0]
}

function resolveOpenClawDefaultAgentWorkspaceDir(): string {
  const home = resolveOpenClawHomeDir()
  const profile = process.env.OPENCLAW_PROFILE?.trim()
  if (profile && profile.toLowerCase() !== 'default') {
    return path.join(home, '.openclaw', `workspace-${profile}`)
  }
  return path.join(home, '.openclaw', 'workspace')
}

async function loadOpenClawEnv(stateDir: string, config: Record<string, unknown>): Promise<Record<string, string>> {
  const env = await readDotenv(path.join(stateDir, '.env'))
  const vars = asRecord(asRecord(config.env)?.vars) ?? {}
  for (const [key, value] of Object.entries(vars)) {
    const cleanKey = key.trim()
    if (!cleanKey || env[cleanKey]) {
      continue
    }
    const resolved = resolveSecretValue(asString(value), env)
    if (resolved) {
      env[cleanKey] = resolved
    }
  }
  return env
}

function parseProviderMap(providerMap: Record<string, unknown>, env: Record<string, string>): ProviderCandidate[] {
  const providers: ProviderCandidate[] = []
  for (const [sourceName, rawValue] of Object.entries(providerMap)) {
    const raw = asRecord(rawValue)
    if (!raw) {
      continue
    }
    const api = asString(raw.api) || sourceName
    const baseUrl = normalizeProviderBaseUrl(mapArkloopProvider(api, sourceName), asString(raw.baseUrl) || asString(raw.base_url))
    const apiKey =
      resolveSecretInputValue(raw.apiKey ?? raw.api_key, env) ||
      resolveSecretValue(asString(raw.apiKeyEnv) || asString(raw.api_key_env), env)
    const mapped = mapProviderCandidate(sourceName, api, baseUrl, apiKey, parseModels(raw.models))
    if (mapped) {
      providers.push(mapped)
    }
  }
  providers.sort((a, b) => a.name.localeCompare(b.name))
  return providers
}

function mapProviderCandidate(
  sourceName: string,
  api: string,
  baseUrl: string | undefined,
  apiKey: string | undefined,
  models: ModelCandidate[],
): ProviderCandidate | null {
  const provider = mapArkloopProvider(api, sourceName)
  if (!provider) {
    return null
  }

  const openaiApiMode =
    provider === 'openai'
      ? normalizeOpenAiMode(api)
      : undefined

  return {
    sourceName,
    name: sourceName,
    provider,
    apiKey,
    baseUrl,
    openaiApiMode,
    models,
  }
}

function mapArkloopProvider(api: string, sourceName: string): ProviderCandidate['provider'] | null {
  const normalized = `${api} ${sourceName}`.toLowerCase()
  if (normalized.includes('google') || normalized.includes('gemini')) {
    return 'gemini'
  }
  if (normalized.includes('anthropic')) {
    return 'anthropic'
  }
  if (
    normalized.includes('openai') ||
    normalized.includes('openrouter') ||
    normalized.includes('groq') ||
    normalized.includes('deepseek')
  ) {
    return 'openai'
  }
  return null
}

function normalizeOpenAiMode(api: string): 'chat_completions' | 'responses' {
  return api.toLowerCase().includes('responses') ? 'responses' : 'chat_completions'
}

function normalizeProviderBaseUrl(provider: ProviderCandidate['provider'] | null, baseUrl: string): string | undefined {
  const trimmed = baseUrl.trim().replace(/\/$/, '')
  if (!trimmed) {
    return undefined
  }
  if (provider === 'openai' && trimmed === 'https://api.openai.com') {
    return 'https://api.openai.com/v1'
  }
  return trimmed
}

function parseModels(value: unknown): ModelCandidate[] {
  const items = Array.isArray(value) ? value : []
  const models: ModelCandidate[] = []
  for (const item of items) {
    if (typeof item === 'string') {
      if (item.trim()) {
        models.push({ id: item.trim() })
      }
      continue
    }
    const raw = asRecord(item)
    const id = asString(raw?.id) || asString(raw?.model) || asString(raw?.name)
    if (!raw || !id) {
      continue
    }
    models.push({
      id,
      name: asString(raw.name) || undefined,
      contextWindow: asNumber(raw.contextWindow) ?? asNumber(raw.context_window),
      maxTokens: asNumber(raw.maxTokens) ?? asNumber(raw.max_tokens),
      reasoning: asBoolean(raw.reasoning),
      inputModalities: stringArray(raw.input),
    })
  }
  return dedupeBy(models, (item) => item.id)
}

function parseMcpMap(
  sourceKind: ImportSourceKind,
  serverMap: Record<string, unknown>,
  env: Record<string, string>,
): McpCandidate[] {
  const servers: McpCandidate[] = []
  for (const [key, rawValue] of Object.entries(serverMap)) {
    const raw = asRecord(rawValue)
    if (!raw) {
      continue
    }
    const transport = normalizeTransport(asString(raw.transport) || asString(raw.type), raw)
    const displayName = humanizeName(key)
    const launchSpec: Record<string, unknown> = { transport }
    const envSecrets: Record<string, string> = {}

    if (transport === 'stdio') {
      const command = asString(raw.command)
      if (!command) {
        continue
      }
      launchSpec.command = command
      const args = stringArray(raw.args).map((arg) => expandHomePath(arg))
      if (args.length > 0) {
        launchSpec.args = args
      }
      const cwd = asString(raw.cwd) || asString(raw.workingDirectory) || asString(raw.working_directory)
      if (cwd) {
        launchSpec.cwd = expandHomePath(cwd)
      }
      const visibleEnv: Record<string, string> = {}
      for (const [envKey, rawEnvValue] of Object.entries(asRecord(raw.env) ?? {})) {
        const value = asString(rawEnvValue)
        if (!envKey.trim()) {
          continue
        }
        const resolved = resolveSecretInputValue(rawEnvValue, env, envAliases(envKey))
        if (resolved && isSecretEnvKey(envKey)) {
          envSecrets[envKey] = resolved
          continue
        }
        if (value.trim()) {
          visibleEnv[envKey] = value
          continue
        }
        if (resolved) {
          visibleEnv[envKey] = resolved
        }
      }
      if (Object.keys(visibleEnv).length > 0) {
        launchSpec.env = visibleEnv
      }
    } else {
      const url = asString(raw.url)
      if (!url) {
        continue
      }
      launchSpec.url = url
    }

    const headers = secretStringRecord(raw.headers, env)
    const bearerToken = resolveSecretValue(asString(raw.bearer_token) || asString(raw.bearerToken), env)
    const timeout = asNumber(raw.connectionTimeoutMs) ?? asNumber(raw.callTimeoutMs) ?? asNumber(raw.call_timeout_ms)
    if (timeout && timeout > 0) {
      launchSpec.callTimeoutMs = timeout
    }

    servers.push({
      key: `${sourceKind}_${sanitizeKey(key)}`,
      displayName,
      transport,
      launchSpec,
      envSecrets: Object.keys(envSecrets).length > 0 ? envSecrets : undefined,
      authHeaders: headers,
      bearerToken,
    })
  }
  servers.sort((a, b) => a.displayName.localeCompare(b.displayName))
  return servers
}

function normalizeTransport(value: string, raw: Record<string, unknown>): McpCandidate['transport'] {
  const normalized = value.trim().toLowerCase().replace(/-/g, '_')
  if (normalized === 'sse' || normalized === 'http_sse') {
    return 'http_sse'
  }
  if (normalized === 'streamable_http' || normalized === 'streamable') {
    return 'streamable_http'
  }
  if (asString(raw.url)) {
    return 'streamable_http'
  }
  return 'stdio'
}

async function importWorkspace(source: SourceDetails): Promise<number> {
  const targetDir = arkloopWorkDir()

  if (source.kind === 'openclaw' && source.workspaceDir) {
    if (path.resolve(source.workspaceDir) === path.resolve(targetDir)) {
      return 1
    }
    await resetArkloopWorkDir(targetDir)
    await fs.cp(source.workspaceDir, targetDir, { recursive: true, force: true })
    return 1
  }

  await resetArkloopWorkDir(targetDir)
  let count = 0
  count += await copyFileIfExists(path.join(source.homeDir, 'SOUL.md'), path.join(targetDir, 'SOUL.md'))
  count += await copyFileIfExists(path.join(source.homeDir, 'AGENTS.md'), path.join(targetDir, 'AGENTS.md'))
  count += await copyFileIfExists(path.join(source.homeDir, 'MEMORY.md'), path.join(targetDir, 'MEMORY.md'))
  count += await copyFileIfExists(path.join(source.homeDir, 'USER.md'), path.join(targetDir, 'USER.md'))
  count += await copyFileIfExists(path.join(source.homeDir, 'memories', 'MEMORY.md'), path.join(targetDir, 'MEMORY.md'))
  count += await copyFileIfExists(path.join(source.homeDir, 'memories', 'USER.md'), path.join(targetDir, 'USER.md'))
  if (await pathExists(path.join(source.homeDir, 'skills'))) {
    await fs.cp(path.join(source.homeDir, 'skills'), path.join(targetDir, 'skills'), { recursive: true, force: true })
    count += 1
  }
  return count
}

async function resetArkloopWorkDir(targetDir: string): Promise<void> {
  if (path.resolve(targetDir) !== path.resolve(arkloopWorkDir())) {
    throw new Error('invalid work directory')
  }
  await fs.rm(targetDir, { recursive: true, force: true })
  await fs.mkdir(targetDir, { recursive: true })
}

async function importProviders(source: SourceDetails, options: ApiOptions): Promise<number> {
  if (source.providers.length === 0) {
    return 0
  }
  const api = createApiClient(options)
  const providers = await api.json<LlmProviderResponse[]>('/v1/llm-providers?scope=user', 'GET')
  let imported = 0
  for (const candidate of source.providers) {
    if (!candidate.apiKey) {
      continue
    }
    const existing = providers.find((item) => item.name === candidate.name) ?? providers.find((item) =>
      item.provider === candidate.provider &&
      normalizeNullable(item.base_url) === normalizeNullable(candidate.baseUrl) &&
      normalizeNullable(item.openai_api_mode) === normalizeNullable(candidate.openaiApiMode),
    )
    const payload = {
      scope: 'user',
      name: candidate.name,
      provider: candidate.provider,
      api_key: candidate.apiKey,
      ...(candidate.baseUrl ? { base_url: candidate.baseUrl } : {}),
      ...(candidate.openaiApiMode ? { openai_api_mode: candidate.openaiApiMode } : {}),

    }
    const saved = existing
      ? await api.json<LlmProviderResponse>(`/v1/llm-providers/${existing.id}?scope=user`, 'PATCH', payload)
      : await api.json<LlmProviderResponse>('/v1/llm-providers?scope=user', 'POST', payload)
    mergeProviderList(providers, saved)
    await importProviderModels(api, saved, candidate.models)
    imported += 1
  }
  return imported
}

async function importProviderModels(
  api: ReturnType<typeof createApiClient>,
  provider: LlmProviderResponse,
  models: ModelCandidate[],
): Promise<void> {
  const existing = new Set(provider.models.map((item) => item.model))
  for (const model of models) {
    if (existing.has(model.id)) {
      continue
    }
    try {
      await api.json(`/v1/llm-providers/${provider.id}/models?scope=user`, 'POST', {
        scope: 'user',
        model: model.id,
        is_default: existing.size === 0,
        show_in_picker: true,
        advanced_json: modelAdvancedJson(model),
      })
      existing.add(model.id)
    } catch (error) {
      if (!(error instanceof ApiRequestError) || error.status !== 409) {
        throw error
      }
    }
  }
}

function modelAdvancedJson(model: ModelCandidate): Record<string, unknown> | undefined {
  const catalog: Record<string, unknown> = {}
  if (model.name) catalog.name = model.name
  if (model.contextWindow) catalog.context_length = model.contextWindow
  if (model.maxTokens) catalog.max_output_tokens = model.maxTokens
  if (model.reasoning !== undefined) catalog.reasoning = model.reasoning
  if (model.inputModalities && model.inputModalities.length > 0) catalog.input_modalities = model.inputModalities
  return Object.keys(catalog).length > 0 ? { available_catalog: catalog } : undefined
}

async function importSkills(source: SourceDetails, options: ApiOptions): Promise<number> {
  if (source.skillDirs.length === 0) {
    return 0
  }
  const api = createApiClient(options)
  const importedSkills: SkillPackageResponse[] = []
  for (const skillDir of source.skillDirs) {
    const files = await collectUploadFiles(skillDir)
    if (files.length === 0) {
      continue
    }
    try {
      const skill = await api.multipart<SkillPackageResponse>('/v1/skill-packages/import/upload', files)
      importedSkills.push(skill)
      await installSkill(api, skill)
    } catch (error) {
      if (!(error instanceof ApiRequestError) || error.status !== 409) {
        throw error
      }
    }
  }
  if (importedSkills.length > 0) {
    await enableDefaultSkills(api, importedSkills)
  }
  return importedSkills.length
}

async function installSkill(api: ReturnType<typeof createApiClient>, skill: SkillPackageResponse): Promise<void> {
  try {
    await api.json('/v1/profiles/me/skills/install', 'POST', skill)
  } catch (error) {
    if (!(error instanceof ApiRequestError) || error.status !== 409) {
      throw error
    }
  }
}

async function enableDefaultSkills(api: ReturnType<typeof createApiClient>, skills: SkillPackageResponse[]): Promise<void> {
  const current = await api.json<{ items: SkillPackageResponse[] }>('/v1/profiles/me/default-skills', 'GET')
  const keyed = new Map<string, SkillPackageResponse>()
  for (const item of current.items ?? []) {
    keyed.set(`${item.skill_key}@${item.version}`, { skill_key: item.skill_key, version: item.version })
  }
  for (const skill of skills) {
    keyed.set(`${skill.skill_key}@${skill.version}`, { skill_key: skill.skill_key, version: skill.version })
  }
  await api.json('/v1/profiles/me/default-skills', 'PUT', { skills: Array.from(keyed.values()) })
}

async function importMcpServers(source: SourceDetails, options: ApiOptions): Promise<number> {
  if (source.mcpConfigs.length === 0) {
    return 0
  }
  const api = createApiClient(options)
  const existing = await api.json<McpInstallResponse[]>('/v1/mcp-installs', 'GET')
  let imported = 0
  for (const item of source.mcpConfigs) {
    const current = existing.find((install) => install.install_key === item.key)
    const payload = {
      install_key: item.key,
      display_name: item.displayName,
      transport: item.transport,
      launch_spec: item.launchSpec,
      ...(item.envSecrets ? { env_secrets: item.envSecrets } : {}),
      ...(item.authHeaders ? { auth_headers: item.authHeaders } : {}),
      ...(item.bearerToken ? { bearer_token: item.bearerToken } : {}),
      host_requirement: item.transport === 'stdio' ? 'desktop_local' : 'remote_http',
    }
    const saved = current
      ? await api.json<McpInstallResponse>(`/v1/mcp-installs/${current.id}`, 'PATCH', payload)
      : await api.json<McpInstallResponse>('/v1/mcp-installs', 'POST', payload)
    mergeMcpList(existing, saved)
    await api.json('/v1/workspace-mcp-enablements', 'PUT', {
      install_id: saved.id,
      enabled: true,
    })
    imported += 1
  }
  return imported
}

function createApiClient(options: ApiOptions) {
  if (!options.apiBaseUrl) {
    throw new Error('sidecar not running')
  }
  return {
    json: async <T = unknown>(pathname: string, method: string, body?: unknown): Promise<T> => {
      const payload = body === undefined ? undefined : Buffer.from(JSON.stringify(body))
      const result = await requestRaw(options.apiBaseUrl!, pathname, method, options.token, payload, {
        'Content-Type': 'application/json',
      })
      return parseJsonResult<T>(result)
    },
    multipart: async <T = unknown>(pathname: string, files: UploadFile[]): Promise<T> => {
      const boundary = `arkloop-${crypto.randomBytes(12).toString('hex')}`
      const body = await buildMultipartBody(boundary, files)
      const result = await requestRaw(options.apiBaseUrl!, pathname, 'POST', options.token, body, {
        'Content-Type': `multipart/form-data; boundary=${boundary}`,
      })
      return parseJsonResult<T>(result)
    },
  }
}

async function requestRaw(
  apiBaseUrl: string,
  pathname: string,
  method: string,
  token: string,
  body: Buffer | undefined,
  headers: Record<string, string>,
): Promise<string> {
  const url = new URL(pathname, apiBaseUrl)
  const transport = url.protocol === 'https:' ? https : http
  return await new Promise((resolve, reject) => {
    const req = transport.request({
      hostname: url.hostname,
      port: url.port ? Number(url.port) : url.protocol === 'https:' ? 443 : 80,
      path: url.pathname + url.search,
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        ...headers,
        ...(body ? { 'Content-Length': String(body.length) } : {}),
      },
    }, (res) => {
      const chunks: Buffer[] = []
      res.on('data', (chunk: Buffer) => chunks.push(chunk))
      res.on('end', () => {
        const text = Buffer.concat(chunks).toString('utf8')
        const status = res.statusCode ?? 0
        if (status >= 400) {
          reject(apiErrorFromResponse(status, text))
          return
        }
        resolve(text)
      })
    })
    req.on('error', reject)
    if (body) {
      req.write(body)
    }
    req.end()
  })
}

function apiErrorFromResponse(status: number, text: string): ApiRequestError {
  try {
    const payload = JSON.parse(text) as { error?: { code?: string; message?: string }; message?: string }
    return new ApiRequestError(status, payload.error?.message || payload.message || `request failed: ${status}`, payload.error?.code)
  } catch {
    return new ApiRequestError(status, text || `request failed: ${status}`)
  }
}

function parseJsonResult<T>(text: string): T {
  if (!text.trim()) {
    return undefined as T
  }
  return JSON.parse(text) as T
}

type UploadFile = {
  relativePath: string
  data: Buffer
}

async function collectUploadFiles(root: string): Promise<UploadFile[]> {
  const files: UploadFile[] = []
  await walk(root, 10, async (filePath, entry) => {
    if (entry.isDirectory()) {
      if (shouldSkipDir(entry.name)) {
        return 'skip'
      }
      return undefined
    }
    if (!entry.isFile() || entry.name === '.DS_Store') {
      return undefined
    }
    const stat = await fs.stat(filePath)
    if (stat.size > 8 * 1024 * 1024) {
      return undefined
    }
    const relativePath = path.relative(root, filePath).split(path.sep).join('/')
    files.push({ relativePath, data: await fs.readFile(filePath) })
    return undefined
  })
  files.sort((a, b) => a.relativePath.localeCompare(b.relativePath))
  return files
}

async function buildMultipartBody(boundary: string, files: UploadFile[]): Promise<Buffer> {
  const chunks: Buffer[] = []
  for (const file of files) {
    chunks.push(Buffer.from(`--${boundary}\r\n`))
    chunks.push(Buffer.from(`Content-Disposition: form-data; name="files"; filename="${escapeMultipartValue(file.relativePath)}"\r\n`))
    chunks.push(Buffer.from('Content-Type: application/octet-stream\r\n\r\n'))
    chunks.push(file.data)
    chunks.push(Buffer.from('\r\n'))
    chunks.push(Buffer.from(`--${boundary}\r\n`))
    chunks.push(Buffer.from('Content-Disposition: form-data; name="relative_paths"\r\n\r\n'))
    chunks.push(Buffer.from(file.relativePath))
    chunks.push(Buffer.from('\r\n'))
  }
  chunks.push(Buffer.from(`--${boundary}--\r\n`))
  return Buffer.concat(chunks)
}

function escapeMultipartValue(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

async function collectSkillDirs(roots: string[]): Promise<string[]> {
  const dirs = new Set<string>()
  for (const root of roots) {
    if (!(await pathExists(root))) {
      continue
    }
    await walk(root, 5, async (filePath, entry) => {
      if (entry.isDirectory()) {
        if (shouldSkipDir(entry.name)) {
          return 'skip'
        }
        return undefined
      }
      if (entry.name === 'SKILL.md') {
        dirs.add(path.dirname(filePath))
      }
      return undefined
    })
  }
  return Array.from(dirs).sort((a, b) => a.localeCompare(b))
}

async function walk(
  root: string,
  maxDepth: number,
  visit: (filePath: string, entry: import('fs').Dirent) => Promise<'skip' | undefined>,
  depth = 0,
): Promise<void> {
  if (depth > maxDepth) {
    return
  }
  let entries: import('fs').Dirent[]
  try {
    entries = await fs.readdir(root, { withFileTypes: true })
  } catch {
    return
  }
  for (const entry of entries) {
    const filePath = path.join(root, entry.name)
    const action = await visit(filePath, entry)
    if (entry.isDirectory() && action !== 'skip') {
      await walk(filePath, maxDepth, visit, depth + 1)
    }
  }
}

function shouldSkipDir(name: string): boolean {
  return name === '.git' || name === 'node_modules' || name === '__pycache__'
}

async function readYamlFile(filePath: string): Promise<Record<string, unknown> | null> {
  const content = await readTextFile(filePath)
  if (!content) {
    return null
  }
  try {
    const parsed = YAML.parse(content) as unknown
    return asRecord(parsed)
  } catch {
    return null
  }
}

async function readJson5File(filePath: string): Promise<Record<string, unknown> | null> {
  const content = await readTextFile(filePath)
  if (!content) {
    return null
  }
  try {
    const parsed = JSON5.parse(content) as unknown
    return asRecord(parsed)
  } catch {
    return null
  }
}

async function readTextFile(filePath: string): Promise<string | null> {
  try {
    return await fs.readFile(filePath, 'utf8')
  } catch {
    return null
  }
}

async function readDotenv(filePath: string): Promise<Record<string, string>> {
  const content = await readTextFile(filePath)
  if (!content) {
    return {}
  }
  const env: Record<string, string> = {}
  for (const line of content.split(/\r?\n/)) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) {
      continue
    }
    const index = trimmed.indexOf('=')
    if (index <= 0) {
      continue
    }
    const key = trimmed.slice(0, index).trim()
    let value = trimmed.slice(index + 1).trim()
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1)
    }
    if (key && !key.toUpperCase().includes('TELEGRAM')) {
      env[key] = value
    }
  }
  return env
}

function resolveSecretValue(raw: string, env: Record<string, string>, aliases: string[] = []): string | undefined {
  const value = raw.trim()
  if (!value || value === '__REPLACE_ME__') {
    return undefined
  }
  const ref = envRefName(value)
  const names = [ref || value, ...aliases].filter(Boolean)
  for (const name of names) {
    const fromEnv = env[name] || process.env[name]
    if (fromEnv && fromEnv !== '__REPLACE_ME__') {
      return fromEnv
    }
  }
  if (!ref && !/^[A-Z0-9_]+$/.test(value)) {
    return value
  }
  return undefined
}

function resolveSecretInputValue(raw: unknown, env: Record<string, string>, aliases: string[] = []): string | undefined {
  const direct = asString(raw)
  if (direct) {
    return resolveSecretValue(direct, env, aliases)
  }
  const ref = asRecord(raw)
  if (!ref || asString(ref.source) !== 'env') {
    return undefined
  }
  return resolveSecretValue(asString(ref.id), env, aliases)
}

function envRefName(value: string): string | null {
  const trimmed = value.trim()
  const braced = trimmed.match(/^\$\{([A-Z0-9_]+)\}$/i)
  if (braced) {
    return braced[1]
  }
  const simple = trimmed.match(/^\$([A-Z0-9_]+)$/i)
  return simple ? simple[1] : null
}

function envAliases(key: string): string[] {
  if (key === 'GITHUB_PERSONAL_ACCESS_TOKEN') {
    return ['GITHUB_TOKEN']
  }
  return []
}

function isSecretEnvKey(key: string): boolean {
  return /KEY|TOKEN|SECRET|PASSWORD|PAT|AUTH/i.test(key)
}

function secretStringRecord(value: unknown, env: Record<string, string>): Record<string, string> | undefined {
  const raw = asRecord(value)
  if (!raw) {
    return undefined
  }
  const out: Record<string, string> = {}
  for (const [key, item] of Object.entries(raw)) {
    const text = resolveSecretInputValue(item, env) || asString(item)
    if (key.trim() && text.trim()) {
      out[key.trim()] = text
    }
  }
  return Object.keys(out).length > 0 ? out : undefined
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value
  }
  return undefined
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.map(asString).filter(Boolean)
}

function dedupeBy<T>(items: T[], keyOf: (item: T) => string): T[] {
  const seen = new Set<string>()
  const out: T[] = []
  for (const item of items) {
    const key = keyOf(item)
    if (!key || seen.has(key)) {
      continue
    }
    seen.add(key)
    out.push(item)
  }
  return out
}

function normalizeSelection(selection?: Partial<Record<ImportItemKey, boolean>>): Record<ImportItemKey, boolean> {
  return {
    identity: selection?.identity ?? true,
    skills: selection?.skills ?? true,
    mcp: selection?.mcp ?? true,
    providers: selection?.providers ?? true,
  }
}

function emptyImportCounts(): Record<ImportItemKey, number> {
  return { identity: 0, skills: 0, mcp: 0, providers: 0 }
}

async function runImportStep(errors: string[], run: () => Promise<void>): Promise<boolean> {
  try {
    await run()
    return true
  } catch (error) {
    errors.push(error instanceof Error ? error.message : String(error))
    return false
  }
}

function mergeProviderList(items: LlmProviderResponse[], item: LlmProviderResponse): void {
  const index = items.findIndex((current) => current.id === item.id)
  if (index >= 0) {
    items[index] = item
  } else {
    items.push(item)
  }
}

function mergeMcpList(items: McpInstallResponse[], item: McpInstallResponse): void {
  const index = items.findIndex((current) => current.id === item.id)
  if (index >= 0) {
    items[index] = item
  } else {
    items.push(item)
  }
}

async function copyFileIfExists(source: string, target: string): Promise<number> {
  if (!(await pathExists(source))) {
    return 0
  }
  await fs.mkdir(path.dirname(target), { recursive: true })
  await fs.copyFile(source, target)
  return 1
}

async function existsAny(paths: string[]): Promise<boolean> {
  for (const item of paths) {
    if (await pathExists(item)) {
      return true
    }
  }
  return false
}

async function pathExists(filePath: string): Promise<boolean> {
  try {
    await fs.access(filePath)
    return true
  } catch {
    return false
  }
}

function resolveOpenClawUserPath(value: string): string {
  const expanded = expandHomePathWithHome(value.trim(), resolveOpenClawHomeDir())
  return expanded ? path.resolve(expanded) : expanded
}

function stripNullBytes(value: string): string {
  return value.replace(/\0/g, '')
}

function expandHomePath(value: string): string {
  return expandHomePathWithHome(value, os.homedir())
}

function expandHomePathWithHome(value: string, home: string): string {
  if (value === '~') {
    return home
  }
  if (value.startsWith('~/') || value.startsWith('~\\')) {
    return path.join(home, value.slice(2))
  }
  return value
}

function resolveOpenClawHomeDir(): string {
  const explicitHome = normalizeHomeEnv(process.env.OPENCLAW_HOME)
  if (explicitHome) {
    return path.resolve(expandHomePathWithHome(explicitHome, resolveOpenClawOsHomeDir()))
  }
  return resolveOpenClawOsHomeDir()
}

function resolveOpenClawOsHomeDir(): string {
  return path.resolve(
    normalizeHomeEnv(process.env.HOME) ||
    normalizeHomeEnv(process.env.USERPROFILE) ||
    os.homedir() ||
    process.cwd(),
  )
}

function normalizeHomeEnv(value: string | undefined): string | undefined {
  const trimmed = value?.trim()
  if (!trimmed || trimmed === 'undefined' || trimmed === 'null') {
    return undefined
  }
  return trimmed
}

function displayPath(value: string): string {
  const home = os.homedir()
  return value.startsWith(home) ? `~${value.slice(home.length)}` : value
}

function arkloopWorkDir(): string {
  return path.join(os.homedir(), '.arkloop', 'home')
}

function sanitizeKey(value: string): string {
  const key = value.toLowerCase().trim().replace(/[^a-z0-9_]+/g, '_').replace(/^_+|_+$/g, '')
  return key || 'imported'
}

function humanizeName(value: string): string {
  return value
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function normalizeNullable(value: string | null | undefined): string {
  return (value ?? '').trim().replace(/\/$/, '')
}
