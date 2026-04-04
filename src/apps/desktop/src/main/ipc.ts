import { ipcMain, BrowserWindow } from 'electron'
import os from 'os'
import { DatabaseSync } from 'node:sqlite'
import path from 'path'
import { loadConfig, saveConfig, getConfigPath } from './config'
import {
  getSidecarStatus,
  getSidecarRuntime,
  downloadSidecar,
  isSidecarAvailable,
  getDesktopAccessToken,
  getBridgeBaseUrl,
  type SidecarRuntime,
} from './sidecar'
import { checkForUpdates, applyUpdate } from './updater'
import { getAppUpdaterState, checkForAppUpdates, downloadAppUpdate, installAppUpdate } from './app-updater'
import { DEFAULT_CONFIG } from './types'
import { getDesktopLogDir, getDesktopLogPaths } from './logging'
import type { AppConfig, ApplyConfigUpdateOptions, ConnectorsConfig, MemoryConfig } from './types'

type DesktopController = {
  applyConfigUpdate: (config: AppConfig, options?: ApplyConfigUpdateOptions) => Promise<AppConfig>
  restartLocalSidecar: () => Promise<SidecarRuntime>
  getSidecarRuntime: () => Promise<SidecarRuntime>
}

type DesktopExportSection =
  | 'settings'
  | 'providers'
  | 'history'
  | 'personas'
  | 'projects'
  | 'mcp'
  | 'themes'

type DesktopThemeExportPayload = {
  customThemeId: string | null
  customThemes: Record<string, unknown>
}

type DesktopExportOptions = {
  sections?: DesktopExportSection[]
  themes?: DesktopThemeExportPayload | null
}

const DESKTOP_EXPORT_SECTION_SET = new Set<DesktopExportSection>([
  'settings',
  'providers',
  'history',
  'personas',
  'projects',
  'mcp',
  'themes',
])

const DESKTOP_EXPORT_BUNDLE_FILE_SET = new Set([
  'config.json',
  'data.sqlite',
  'themes.json',
])

type DesktopBundleFile = 'config.json' | 'data.sqlite' | 'themes.json'

type DesktopBundleManifest = {
  schema_version: number
  exported_at: string
  sections: DesktopExportSection[]
  files: DesktopBundleFile[]
}

export function registerIpcHandlers(
  getWindow: () => BrowserWindow | null,
  controller: DesktopController,
): void {
  // preload 同步获取配置, 确保 __ARKLOOP_DESKTOP__ 在页面脚本之前注入
  ipcMain.on('arkloop:config:get-sync', (event) => {
    event.returnValue = {
      ...loadConfig(),
      desktopAccessToken: getDesktopAccessToken(),
      bridgeBaseUrl: getBridgeBaseUrl(),
    }
  })

  ipcMain.handle('arkloop:config:get', () => {
    return loadConfig()
  })

  ipcMain.handle('arkloop:config:set', async (_event, config: AppConfig) => {
    await controller.applyConfigUpdate(config)
    return { ok: true }
  })

  ipcMain.handle('arkloop:config:path', () => {
    return getConfigPath()
  })

  ipcMain.handle('arkloop:advanced:overview', async () => {
    return await buildAdvancedOverview()
  })

  ipcMain.handle('arkloop:advanced:data-folder', async () => {
    const { dialog } = require('electron') as typeof import('electron')
    const win = getWindow()
    const result = await dialog.showOpenDialog(win ?? BrowserWindow.getFocusedWindow()!, {
      properties: ['openDirectory', 'createDirectory'],
    })
    if (result.canceled || result.filePaths.length === 0) return null
    return result.filePaths[0]
  })

  ipcMain.handle('arkloop:advanced:export-data', async (_event, options?: DesktopExportOptions) => {
    const { dialog } = require('electron') as typeof import('electron')
    const win = getWindow()
    const result = await dialog.showOpenDialog(win ?? BrowserWindow.getFocusedWindow()!, {
      properties: ['openDirectory', 'createDirectory'],
    })
    if (result.canceled || result.filePaths.length === 0) {
      return { ok: false, canceled: true }
    }
    const exportPath = exportDesktopBundle(result.filePaths[0], path, options)
    return { ok: true, filePath: exportPath }
  })

  ipcMain.handle('arkloop:advanced:import-data', async () => {
    const { dialog } = require('electron') as typeof import('electron')
    const win = getWindow()
    const result = await dialog.showOpenDialog(win ?? BrowserWindow.getFocusedWindow()!, {
      properties: ['openDirectory'],
    })
    if (result.canceled || result.filePaths.length === 0) {
      return { ok: false, canceled: true }
    }
    const imported = importDesktopBundle(result.filePaths[0])
    const importedConfig = imported.config
    await controller.applyConfigUpdate(importedConfig, { forceLocalSidecarRestart: true })
    return { ok: true, importedFrom: result.filePaths[0], themes: imported.themes }
  })

  ipcMain.handle('arkloop:advanced:logs', async (_event, input?: {
    source?: 'all' | 'main' | 'sidecar'
    level?: 'all' | 'info' | 'warn' | 'error' | 'debug' | 'other'
    search?: string
    limit?: number
  }) => {
    return { entries: listDesktopLogs(input) }
  })

  ipcMain.handle('arkloop:sidecar:status', () => {
    return getSidecarStatus()
  })

  ipcMain.handle('arkloop:sidecar:runtime', async () => controller.getSidecarRuntime())

  ipcMain.handle('arkloop:sidecar:restart', async () => {
    await controller.restartLocalSidecar()
    return getSidecarStatus()
  })

  ipcMain.handle('arkloop:sidecar:download', async () => {
    await downloadSidecar((progress) => {
      const win = getWindow()
      if (win) win.webContents.send('arkloop:sidecar:download-progress', progress)
    })
    return { ok: true }
  })

  ipcMain.handle('arkloop:sidecar:is-available', () => {
    return isSidecarAvailable()
  })

  ipcMain.handle('arkloop:sidecar:check-update', async () => {
    return checkForUpdates()
  })

  ipcMain.handle('arkloop:updater:check', async () => {
    return checkForUpdates()
  })

  ipcMain.handle('arkloop:updater:apply', async (_event, { component }: { component: 'openviking' | 'sandbox_kernel' | 'sandbox_rootfs' | 'rtk' | 'opencli' }) => {
    const win = getWindow()
    await applyUpdate(component, (progress) => {
      if (win) win.webContents.send('arkloop:updater:progress', { component, ...progress })
    })
    if (component === 'sandbox_kernel' || component === 'sandbox_rootfs') {
      await controller.restartLocalSidecar()
    }
    return { ok: true }
  })

  ipcMain.handle('arkloop:app-updater:get-state', () => {
    return getAppUpdaterState()
  })

  ipcMain.handle('arkloop:app-updater:check', async () => {
    return checkForAppUpdates()
  })

  ipcMain.handle('arkloop:app-updater:download', async () => {
    return downloadAppUpdate()
  })

  ipcMain.handle('arkloop:app-updater:install', () => {
    installAppUpdate()
    return { ok: true }
  })

  ipcMain.handle('arkloop:onboarding:status', () => {
    const config = loadConfig()
    return { completed: config.onboarding_completed }
  })

  ipcMain.handle('arkloop:onboarding:complete', () => {
    const config = loadConfig()
    config.onboarding_completed = true
    saveConfig(config)
    return { ok: true }
  })

  ipcMain.handle('arkloop:connectors:get', async () => {
    const config = loadConfig()
    await migrateLegacyConnectorsIfNeeded(config)
    const providerGroups = await fetchToolProviders()
    return connectorsFromProviderGroups(providerGroups)
  })

  ipcMain.handle('arkloop:connectors:set', async (_event, connectors: ConnectorsConfig) => {
    await applyConnectorConfig(connectors)
    return { ok: true }
  })

  ipcMain.handle('arkloop:memory:get-config', () => {
    const config = loadConfig()
    return config.memory
  })

  ipcMain.handle('arkloop:memory:set-config', async (_event, memory: MemoryConfig) => {
    const config = loadConfig()
    const next: AppConfig = { ...config, memory }
    await controller.applyConfigUpdate(next, { forceLocalSidecarRestart: true })
    return { ok: true }
  })

  ipcMain.handle('arkloop:memory:list', async (_event) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { entries: [] }
    const token = getDesktopAccessToken()
    // No agent_id filter — return all memories across all agents for the settings UI.
    const url = `${apiBaseUrl}/v1/desktop/memory/entries`
    const resp = await makeApiRequest(url, 'GET', token)
    return resp
  })

  ipcMain.handle('arkloop:memory:delete', async (_event, id: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { status: 'error', message: 'sidecar not running' }
    const token = getDesktopAccessToken()
    // agent_id is resolved server-side from the entry record itself.
    const url = `${apiBaseUrl}/v1/desktop/memory/entries/${encodeURIComponent(id)}`
    const resp = await makeApiRequest(url, 'DELETE', token)
    return resp
  })

  ipcMain.handle('arkloop:memory:get-snapshot', async (_event, agentId?: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { memory_block: '', hits: [] }
    const token = getDesktopAccessToken()
    const query = typeof agentId === 'string' && agentId.trim()
      ? `?agent_id=${encodeURIComponent(agentId.trim())}`
      : ''
    const url = `${apiBaseUrl}/v1/desktop/memory/snapshot${query}`
    const resp = await makeApiRequest(url, 'GET', token)
    return resp
  })

  ipcMain.handle('arkloop:memory:rebuild-snapshot', async (_event, agentId?: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { memory_block: '', hits: [] }
    const token = getDesktopAccessToken()
    const query = typeof agentId === 'string' && agentId.trim()
      ? `?agent_id=${encodeURIComponent(agentId.trim())}`
      : ''
    const url = `${apiBaseUrl}/v1/desktop/memory/snapshot/rebuild${query}`
    const resp = await makeApiRequest(url, 'POST', token)
    return resp
  })

  ipcMain.handle('arkloop:memory:get-content', async (_event, uri: string, layer?: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { content: '' }
    const token = getDesktopAccessToken()
    const params = new URLSearchParams({ uri })
    if (layer) params.set('layer', layer)
    const url = `${apiBaseUrl}/v1/desktop/memory/content?${params.toString()}`
    const resp = await makeApiRequest(url, 'GET', token)
    return resp
  })

  ipcMain.handle('arkloop:memory:add', async (_event, content: string, category?: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { entry: null }
    const token = getDesktopAccessToken()
    const url = `${apiBaseUrl}/v1/desktop/memory/entries`
    const body = JSON.stringify({ content, category: category || undefined })
    const result = await makeApiRequestRaw(url, 'POST', token, body)
    if (!result.body) return { entry: null }
    try {
      return JSON.parse(result.body)
    } catch {
      return { entry: null }
    }
  })

  ipcMain.handle('arkloop:app:version', () => {
    const { app } = require('electron')
    return app.getVersion()
  })

  ipcMain.handle('arkloop:app:quit', () => {
    const { app } = require('electron')
    app.quit()
  })

  ipcMain.handle('arkloop:app:open-external', async (_event, url: string) => {
    const { shell } = require('electron') as typeof import('electron')
    await shell.openExternal(url)
  })

  ipcMain.handle('arkloop:app:os-username', () => {
    try {
      return os.userInfo().username
    } catch {
      return os.hostname()
    }
  })

  ipcMain.handle('arkloop:logs:dir', () => {
    return getDesktopLogDir()
  })

  ipcMain.handle('arkloop:logs:files', () => {
    return getDesktopLogPaths()
  })

  ipcMain.handle('arkloop:dialog:open-folder', async (event) => {
    const { dialog } = require('electron') as typeof import('electron')
    const win = getWindow()
    const result = await dialog.showOpenDialog(win ?? BrowserWindow.getFocusedWindow()!, {
      properties: ['openDirectory', 'createDirectory'],
    })
    if (result.canceled || result.filePaths.length === 0) return null
    return result.filePaths[0]
  })

  ipcMain.handle('arkloop:fs:list-dir', (_event, folderPath: string, subPath: string) => {
    const path = require('path') as typeof import('path')
    const fs = require('fs') as typeof import('fs')

    const normalizedSub = subPath.replace(/^[/\\]+/, '')
    const fullPath = normalizedSub ? path.join(folderPath, normalizedSub) : folderPath

    const base = path.resolve(folderPath)
    const resolved = path.resolve(fullPath)
    if (resolved !== base && !resolved.startsWith(base + path.sep)) {
      return { entries: [] }
    }

    try {
      const dirents = fs.readdirSync(fullPath, { withFileTypes: true })
      const entries = dirents
        .map((d) => {
          const entryPath = normalizedSub ? `/${normalizedSub}/${d.name}` : `/${d.name}`
          const type: 'file' | 'dir' = d.isDirectory() ? 'dir' : 'file'
          let size: number | undefined
          let mtime_unix_ms: number | undefined
          if (!d.isDirectory()) {
            try {
              const stat = fs.statSync(path.join(fullPath, d.name))
              size = stat.size
              mtime_unix_ms = stat.mtimeMs
            } catch { /* ignore */ }
          }
          return { name: d.name, path: entryPath, type, size, mtime_unix_ms }
        })
        .sort((a, b) => {
          if (a.type !== b.type) return a.type === 'dir' ? -1 : 1
          return a.name.localeCompare(b.name)
        })
      return { entries }
    } catch {
      return { entries: [] }
    }
  })

  ipcMain.handle('arkloop:fs:read-file', (_event, folderPath: string, relativePath: string) => {
    const path = require('path') as typeof import('path')
    const fs = require('fs') as typeof import('fs')

    const normalizedRel = relativePath.replace(/^[/\\]+/, '')
    if (!normalizedRel) return { error: 'forbidden' }

    const fullPath = path.join(folderPath, normalizedRel)
    const base = path.resolve(folderPath)
    const resolved = path.resolve(fullPath)
    if (!resolved.startsWith(base + path.sep)) {
      return { error: 'forbidden' }
    }

    try {
      const stat = fs.statSync(fullPath)
      if (stat.size > 5 * 1024 * 1024) return { error: 'too_large' }
      const data = fs.readFileSync(fullPath)
      return { data: data.toString('base64'), mime_type: guessMimeTypeByExt(relativePath) }
    } catch {
      return { error: 'read_failed' }
    }
  })
}

const MIME_BY_EXT: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif',
  svg: 'image/svg+xml', webp: 'image/webp', bmp: 'image/bmp',
  html: 'text/html', htm: 'text/html',
  md: 'text/markdown', txt: 'text/plain', json: 'application/json', csv: 'text/csv',
  log: 'text/plain', py: 'text/x-python', ts: 'text/typescript', tsx: 'text/typescript',
  js: 'text/javascript', jsx: 'text/javascript', sh: 'text/x-shellscript',
  go: 'text/plain', rs: 'text/plain', c: 'text/plain', cpp: 'text/plain', h: 'text/plain',
  yml: 'text/yaml', yaml: 'text/yaml', xml: 'application/xml', sql: 'text/plain',
  toml: 'text/plain', ini: 'text/plain', conf: 'text/plain', css: 'text/css',
  pdf: 'application/pdf', zip: 'application/zip',
}

function guessMimeTypeByExt(filepath: string): string {
  const ext = filepath.split('.').pop()?.toLowerCase() ?? ''
  return MIME_BY_EXT[ext] ?? 'application/octet-stream'
}

function getLocalApiBaseUrl(): string | null {
  const runtime = getSidecarRuntime()
  if (runtime.status !== 'running' || !runtime.port) return null
  return `http://127.0.0.1:${runtime.port}`
}

type ToolProviderItem = {
  group_name: string
  provider_name: string
  is_active: boolean
  base_url?: string
  runtime_state?: string
  runtime_reason?: string
}

type ToolProviderGroup = {
  group_name: string
  providers: ToolProviderItem[]
}

async function fetchToolProviders(): Promise<ToolProviderGroup[]> {
  const apiBaseUrl = getLocalApiBaseUrl()
  if (!apiBaseUrl) return []
  const token = getDesktopAccessToken()
  const resp = await makeApiRequest(`${apiBaseUrl}/v1/tool-providers?scope=platform`, 'GET', token)
  if (!resp || typeof resp !== 'object' || !Array.isArray((resp as { groups?: unknown[] }).groups)) {
    return []
  }
  return ((resp as { groups: ToolProviderGroup[] }).groups ?? [])
}

function findProviderGroup(groups: ToolProviderGroup[], groupName: string): ToolProviderGroup | undefined {
  return groups.find((group) => group.group_name === groupName)
}

function connectorsFromProviderGroups(groups: ToolProviderGroup[]): ConnectorsConfig {
  const fetchGroup = findProviderGroup(groups, 'web_fetch')
  const searchGroup = findProviderGroup(groups, 'web_search')

  const activeFetch = fetchGroup?.providers.find((provider) => provider.is_active)
  const activeSearch = searchGroup?.providers.find((provider) => provider.is_active)

  return {
    fetch: {
      provider: activeFetch
        ? providerNameToFetch(activeFetch.provider_name)
        : 'none',
      firecrawlBaseUrl: activeFetch?.provider_name === 'web_fetch.firecrawl'
        ? activeFetch.base_url ?? DEFAULT_CONFIG.connectors.fetch.firecrawlBaseUrl
        : DEFAULT_CONFIG.connectors.fetch.firecrawlBaseUrl,
    },
    search: {
      provider: activeSearch
        ? providerNameToSearch(activeSearch.provider_name)
        : 'none',
      searxngBaseUrl: activeSearch?.provider_name === 'web_search.searxng'
        ? activeSearch.base_url ?? DEFAULT_CONFIG.connectors.search.searxngBaseUrl
        : DEFAULT_CONFIG.connectors.search.searxngBaseUrl,
    },
  }
}

function providerNameToFetch(providerName: string): ConnectorsConfig['fetch']['provider'] {
  switch (providerName) {
    case 'web_fetch.basic':
      return 'basic'
    case 'web_fetch.firecrawl':
      return 'firecrawl'
    case 'web_fetch.jina':
      return 'jina'
    default:
      return 'none'
  }
}

function providerNameToSearch(providerName: string): ConnectorsConfig['search']['provider'] {
  switch (providerName) {
    case 'web_search.duckduckgo':
      return 'duckduckgo'
    case 'web_search.searxng':
      return 'searxng'
    case 'web_search.tavily':
      return 'tavily'
    default:
      return 'none'
  }
}

async function migrateLegacyConnectorsIfNeeded(config: AppConfig): Promise<void> {
  if (config.connectors_migrated) {
    return
  }
  const providerGroups = await fetchToolProviders()
  const searchGroup = findProviderGroup(providerGroups, 'web_search')
  const fetchGroup = findProviderGroup(providerGroups, 'web_fetch')

  if (!searchGroup?.providers.some((provider) => provider.is_active) && hasLegacySearchConfig(config.connectors)) {
    await applySearchConnector(config.connectors.search)
  }
  if (!fetchGroup?.providers.some((provider) => provider.is_active) && hasLegacyFetchConfig(config.connectors)) {
    await applyFetchConnector(config.connectors.fetch)
  }
  saveConfig({ ...config, connectors_migrated: true })
}

function hasLegacySearchConfig(connectors: ConnectorsConfig): boolean {
  return connectors.search.provider === 'duckduckgo'
    || (connectors.search.provider === 'tavily' && Boolean(connectors.search.tavilyApiKey))
    || (connectors.search.provider === 'searxng' && Boolean(connectors.search.searxngBaseUrl))
}

function hasLegacyFetchConfig(connectors: ConnectorsConfig): boolean {
  return connectors.fetch.provider === 'basic'
    || (connectors.fetch.provider === 'jina' && Boolean(connectors.fetch.jinaApiKey))
    || (connectors.fetch.provider === 'firecrawl' && Boolean(connectors.fetch.firecrawlBaseUrl))
}

async function applyConnectorConfig(connectors: ConnectorsConfig): Promise<void> {
  await applySearchConnector(connectors.search)
  await applyFetchConnector(connectors.fetch)
}

async function applySearchConnector(search: ConnectorsConfig['search']): Promise<void> {
  await deactivateToolProviderGroup('web_search')
  if (search.provider === 'duckduckgo') {
    await activateToolProvider('web_search', 'web_search.duckduckgo')
    return
  }
  if (search.provider === 'tavily') {
    await activateToolProvider('web_search', 'web_search.tavily')
    await upsertToolProviderCredential('web_search', 'web_search.tavily', {
      api_key: search.tavilyApiKey ?? '',
    })
    return
  }
  if (search.provider === 'searxng') {
    await activateToolProvider('web_search', 'web_search.searxng')
    await upsertToolProviderCredential('web_search', 'web_search.searxng', {
      base_url: search.searxngBaseUrl ?? '',
    })
    return
  }
}

async function applyFetchConnector(fetch: ConnectorsConfig['fetch']): Promise<void> {
  await deactivateToolProviderGroup('web_fetch')
  if (fetch.provider === 'basic') {
    await activateToolProvider('web_fetch', 'web_fetch.basic')
    return
  }
  if (fetch.provider === 'jina') {
    await activateToolProvider('web_fetch', 'web_fetch.jina')
    await upsertToolProviderCredential('web_fetch', 'web_fetch.jina', {
      api_key: fetch.jinaApiKey ?? '',
    })
    return
  }
  if (fetch.provider === 'firecrawl') {
    await activateToolProvider('web_fetch', 'web_fetch.firecrawl')
    await upsertToolProviderCredential('web_fetch', 'web_fetch.firecrawl', {
      api_key: fetch.firecrawlApiKey ?? '',
      base_url: fetch.firecrawlBaseUrl ?? '',
    })
  }
}

async function deactivateToolProviderGroup(groupName: string): Promise<void> {
  const groups = await fetchToolProviders()
  const group = findProviderGroup(groups, groupName)
  if (!group) return
  for (const provider of group.providers) {
    if (!provider.is_active) continue
    await requestToolProvider(`/v1/tool-providers/${groupName}/${provider.provider_name}/deactivate`, 'PUT')
  }
}

async function activateToolProvider(groupName: string, providerName: string): Promise<void> {
  await requestToolProvider(`/v1/tool-providers/${groupName}/${providerName}/activate`, 'PUT')
}

async function upsertToolProviderCredential(
  groupName: string,
  providerName: string,
  payload: Record<string, string>,
): Promise<void> {
  const body = JSON.stringify(payload)
  await requestToolProvider(`/v1/tool-providers/${groupName}/${providerName}/credential`, 'PUT', body)
}

async function requestToolProvider(pathname: string, method: string, body?: string): Promise<void> {
  const apiBaseUrl = getLocalApiBaseUrl()
  if (!apiBaseUrl) {
    throw new Error('sidecar not running')
  }
  const token = getDesktopAccessToken()
  const sep = pathname.includes('?') ? '&' : '?'
  const url = `${apiBaseUrl}${pathname}${sep}scope=platform`
  await makeApiRequestRaw(url, method, token, body)
}

async function makeApiRequest(url: string, method: string, token: string): Promise<unknown> {
  const result = await makeApiRequestRaw(url, method, token)
  if (!result.body) return { raw: '' }
  try {
    return JSON.parse(result.body)
  } catch {
    return { raw: result.body }
  }
}

async function makeApiRequestRaw(url: string, method: string, token: string, body?: string): Promise<{ status: number; body: string }> {
  const config = loadConfig()
  const timeoutMs = config.network.requestTimeoutMs ?? 30000
  const maxAttempts = Math.max(1, (config.network.retryCount ?? 1) + 1)
  let attempt = 0

  const run = (): Promise<{ status: number; body: string }> => new Promise((resolve, reject) => {
    const parsed = new URL(url)
    const options = {
      hostname: parsed.hostname,
      port: parseInt(parsed.port, 10) || 80,
      path: parsed.pathname + parsed.search,
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
        ...(config.network.userAgent ? { 'User-Agent': config.network.userAgent } : {}),
      },
    }
    const http = require('http') as typeof import('http')
    const req = http.request(options, (res) => {
      let responseBody = ''
      res.on('data', (chunk: Buffer) => { responseBody += chunk.toString() })
      res.on('end', () => {
        const status = res.statusCode ?? 0
        if (status >= 400) {
          reject(new Error(responseBody || `request failed: ${status}`))
          return
        }
        resolve({ status, body: responseBody })
      })
    })
    req.on('error', reject)
    req.setTimeout(timeoutMs, () => {
      req.destroy(new Error(`request timeout after ${timeoutMs}ms`))
    })
    if (body) {
      req.write(body)
    }
    req.end()
  })

  for (;;) {
    try {
      return await run()
    } catch (error) {
      attempt += 1
      if (attempt >= maxAttempts) throw error
    }
  }
}

function desktopDataDir(): string {
  return path.dirname(getConfigPath())
}

function desktopSQLitePath(): string {
  return path.join(desktopDataDir(), 'data.db')
}

function desktopStoragePath(): string {
  return path.join(desktopDataDir(), 'storage')
}

function getDesktopIconDataUrl(): string | null {
  const fs = require('fs') as typeof import('fs')
  const { app } = require('electron') as typeof import('electron')
  const candidates = app.isPackaged
    ? (
      process.platform === 'darwin'
        ? [
            path.join(process.resourcesPath, 'icon.icns'),
            path.join(process.resourcesPath, 'app.asar', 'resources', 'icon.png'),
          ]
        : process.platform === 'win32'
          ? [
              path.join(process.resourcesPath, 'icon.ico'),
              path.join(process.resourcesPath, 'app.asar', 'resources', 'icon.ico'),
            ]
          : [
              path.join(process.resourcesPath, 'icon.png'),
              path.join(process.resourcesPath, 'app.asar', 'resources', 'icon.png'),
            ]
    )
    : [
        path.join(__dirname, '..', '..', 'resources', 'icon.png'),
        path.join(__dirname, '..', '..', 'resources', 'icon.icns'),
      ]

  for (const candidate of candidates) {
    if (!fs.existsSync(candidate)) continue
    const ext = path.extname(candidate).slice(1).toLowerCase()
    const mimeType = ext === 'icns'
      ? 'image/icns'
      : ext === 'ico'
        ? 'image/x-icon'
        : 'image/png'
    const raw = fs.readFileSync(candidate)
    return `data:${mimeType};base64,${raw.toString('base64')}`
  }
  return null
}

function readReleaseLabel(): string {
  const fs = require('fs') as typeof import('fs')
  const path = require('path') as typeof import('path')
  try {
    const metaPath = path.join(__dirname, '..', 'release-meta.json')
    const raw = JSON.parse(fs.readFileSync(metaPath, 'utf-8')) as { releaseLabel?: string }
    return raw.releaseLabel?.trim() || ''
  } catch {
    return ''
  }
}

async function buildAdvancedOverview(): Promise<{
  appName: string
  appVersion: string
  githubUrl: string
  telegramUrl: string | null
  iconDataUrl: string | null
  configPath: string
  dataDir: string
  logsDir: string
  sqlitePath: string
  links: Array<{ label: string; url: string }>
  status: Array<{ label: string; value: string; tone?: 'default' | 'success' | 'warning' | 'danger' }>
  usage: unknown
}> {
  const { app } = require('electron') as typeof import('electron')
  const config = loadConfig()
  const runtime = getSidecarRuntime()
  let updater: Awaited<ReturnType<typeof getAppUpdaterState>> | null = null
  try {
    updater = await Promise.resolve(getAppUpdaterState())
  } catch {
    updater = null
  }
  const componentUpdates = await checkForUpdates().catch(() => null)
  const usage = await loadMonthlyUsage().catch(() => null)
  const releaseLabel = readReleaseLabel()
  const versionDisplay = releaseLabel ? `${app.getVersion()} ${releaseLabel}` : app.getVersion()
  return {
    appName: 'Arkloop',
    appVersion: versionDisplay,
    githubUrl: 'https://github.com/qqqqqf/Arkloop',
    telegramUrl: null,
    iconDataUrl: getDesktopIconDataUrl(),
    configPath: getConfigPath(),
    dataDir: desktopDataDir(),
    logsDir: getDesktopLogDir(),
    sqlitePath: desktopSQLitePath(),
    links: [
      { label: 'GitHub', url: 'https://github.com/qqqqqf/Arkloop' },
      { label: 'Releases', url: 'https://github.com/qqqqqf/Arkloop/releases' },
      { label: 'Follow on X', url: 'https://x.com/intent/follow?screen_name=qqqqqf_' },
    ],
    status: [
      { label: 'Connection', value: config.mode, tone: config.mode === 'local' ? 'success' : 'default' },
      { label: 'Sidecar', value: runtime.status, tone: runtime.status === 'running' ? 'success' : runtime.status === 'crashed' ? 'danger' : 'warning' },
      { label: 'App Update', value: updater?.phase ?? 'idle', tone: updater?.phase === 'available' || updater?.phase === 'downloaded' ? 'warning' : 'default' },
      { label: 'OpenViking', value: componentUpdates?.openviking.available ? 'update_available' : (componentUpdates?.openviking.current ?? 'not_installed') },
    ],
    usage,
  }
}

function normalizeDesktopExportSections(input?: DesktopExportOptions): DesktopExportSection[] {
  const raw = Array.isArray(input?.sections) ? input.sections : []
  const sections = raw.filter((section): section is DesktopExportSection => DESKTOP_EXPORT_SECTION_SET.has(section))
  return sections.length > 0 ? Array.from(new Set(sections)) : ['settings', 'providers', 'history', 'personas', 'projects', 'mcp', 'themes']
}

function exportDesktopBundle(destRoot: string, pathModule: typeof import('path'), options?: DesktopExportOptions): string {
  const fs = require('fs') as typeof import('fs')
  const stamp = new Date().toISOString().replace(/[:.]/g, '-')
  const outDir = pathModule.join(destRoot, `arkloop-export-${stamp}`)
  fs.mkdirSync(outDir, { recursive: true })

  const sections = normalizeDesktopExportSections(options)
  const exportedFiles: DesktopBundleFile[] = []
  const config = loadConfig()

  if (sections.includes('settings')) {
    fs.writeFileSync(pathModule.join(outDir, 'config.json'), JSON.stringify(config, null, 2), 'utf-8')
    exportedFiles.push('config.json')
  }

  const shouldExportDb = sections.some((section) => section !== 'settings' && section !== 'themes')
  if (shouldExportDb) {
    const exportDbPath = pathModule.join(outDir, 'data.sqlite')
    exportDesktopDatabase(exportDbPath, sections)
    exportedFiles.push('data.sqlite')
  }

  if (sections.includes('themes')) {
    fs.writeFileSync(
      pathModule.join(outDir, 'themes.json'),
      JSON.stringify(options?.themes ?? { customThemeId: null, customThemes: {} }, null, 2),
      'utf-8',
    )
    exportedFiles.push('themes.json')
  }

  fs.writeFileSync(pathModule.join(outDir, 'manifest.json'), JSON.stringify({
    schema_version: 2,
    exported_at: new Date().toISOString(),
    sections,
    files: exportedFiles,
  }, null, 2), 'utf-8')

  return outDir
}

function importDesktopBundle(srcDir: string): { config: AppConfig; themes: DesktopThemeExportPayload | null } {
  const fs = require('fs') as typeof import('fs')
  const manifest = readDesktopBundleManifest(srcDir)
  const nextConfigPath = path.join(srcDir, 'config.json')
  const bundleDbPath = path.join(srcDir, 'data.sqlite')
  const themesPath = path.join(srcDir, 'themes.json')
  const hasFile = (fileName: DesktopBundleFile) => manifest.files.includes(fileName)

  let nextConfig = loadConfig()
  if (hasFile('config.json')) {
    const raw = fs.readFileSync(nextConfigPath, 'utf-8')
    nextConfig = JSON.parse(raw) as AppConfig
    fs.copyFileSync(nextConfigPath, getConfigPath())
  }

  if (hasFile('data.sqlite')) {
    importDesktopDatabase(bundleDbPath, manifest.sections)
  }

  let themes: DesktopThemeExportPayload | null = null
  if (hasFile('themes.json')) {
    themes = JSON.parse(fs.readFileSync(themesPath, 'utf-8')) as DesktopThemeExportPayload
  }

  return { config: nextConfig, themes }
}

function exportDesktopDatabase(destPath: string, sections: DesktopExportSection[]): void {
  const source = new DatabaseSync(desktopSQLitePath(), { readOnly: true })
  const dest = new DatabaseSync(destPath)
  try {
    source.exec('PRAGMA foreign_keys = OFF')
    dest.exec('PRAGMA foreign_keys = OFF')
    replicateTableSchema(source, dest, '_sequences')
    for (const tableName of tablesForDesktopExport(sections)) {
      replicateTableSchema(source, dest, tableName)
      copyTableRows(source, dest, tableName)
    }
  } finally {
    dest.close()
    source.close()
  }
}

function importDesktopDatabase(bundlePath: string, sections: DesktopExportSection[]): void {
  const source = new DatabaseSync(bundlePath, { readOnly: true })
  const dest = new DatabaseSync(desktopSQLitePath())
  try {
    source.exec('PRAGMA foreign_keys = OFF')
    dest.exec('PRAGMA foreign_keys = OFF')
    const tableNames = tablesForDesktopExport(sections)
    for (const tableName of tableNames) {
      if (!tableExists(dest, tableName) && tableExists(source, tableName)) {
        replicateTableSchema(source, dest, tableName)
      }
      if (tableExists(dest, tableName)) {
        dest.exec(`DELETE FROM "${tableName}"`)
      }
    }
    for (const tableName of tableNames) {
      copyTableRows(source, dest, tableName)
    }
  } finally {
    dest.close()
    source.close()
  }
}

function tablesForDesktopExport(sections: DesktopExportSection[]): string[] {
  const tables = new Set<string>()
  if (sections.includes('settings')) {
    addTables(tables, ['platform_settings'])
  }
  if (sections.includes('providers')) {
    addTables(tables, ['llm_credentials', 'llm_routes', 'secrets', 'asr_credentials', 'tool_provider_configs'])
  }
  if (sections.includes('history')) {
    addTables(tables, [
      'threads',
      'messages',
      'runs',
      'run_events',
      'thread_stars',
      'thread_shares',
      'thread_reports',
      'channel_message_ledger',
      'scheduled_triggers',
      'channel_group_threads',
      'channel_message_deliveries',
      'channel_dm_threads',
      'channel_message_receipts',
      'channel_identities',
      'channel_identity_bind_codes',
      'channel_identity_links',
    ])
  }
  if (sections.includes('personas')) {
    addTables(tables, ['personas'])
  }
  if (sections.includes('projects')) {
    addTables(tables, [
      'projects',
      'profile_registries',
      'workspace_registries',
      'browser_state_registries',
      'default_workspace_bindings',
      'shell_sessions',
      'profile_skill_installs',
      'workspace_skill_enablements',
      'skill_packages',
    ])
  }
  if (sections.includes('mcp')) {
    addTables(tables, ['mcp_configs', 'profile_mcp_installs', 'workspace_mcp_enablements'])
  }
  addTables(tables, ['accounts', 'users', 'account_memberships'])
  return Array.from(tables)
}

function bundleFilesForSections(sections: DesktopExportSection[]): DesktopBundleFile[] {
  const files: DesktopBundleFile[] = []
  if (sections.includes('settings')) {
    files.push('config.json')
  }
  if (sections.some((section) => section !== 'settings' && section !== 'themes')) {
    files.push('data.sqlite')
  }
  if (sections.includes('themes')) {
    files.push('themes.json')
  }
  return files
}

function readDesktopBundleManifest(srcDir: string): DesktopBundleManifest {
  const fs = require('fs') as typeof import('fs')
  const manifestPath = path.join(srcDir, 'manifest.json')
  if (!fs.existsSync(manifestPath)) {
    throw new Error('manifest.json not found in import bundle')
  }
  const raw = JSON.parse(fs.readFileSync(manifestPath, 'utf-8')) as Record<string, unknown>
  if (raw.schema_version !== 2) {
    throw new Error('unsupported import bundle schema')
  }

  const rawSections = raw.sections
  if (!Array.isArray(rawSections) || rawSections.length === 0) {
    throw new Error('manifest.json is missing export sections')
  }
  const sections = Array.from(new Set(rawSections))
  if (!sections.every((section): section is DesktopExportSection => typeof section === 'string' && DESKTOP_EXPORT_SECTION_SET.has(section as DesktopExportSection))) {
    throw new Error('manifest.json contains unsupported export sections')
  }

  const rawFiles = raw.files
  if (!Array.isArray(rawFiles)) {
    throw new Error('manifest.json is missing bundle files')
  }
  const files = Array.from(new Set(rawFiles))
  if (!files.every((file): file is DesktopBundleFile => typeof file === 'string' && DESKTOP_EXPORT_BUNDLE_FILE_SET.has(file))) {
    throw new Error('manifest.json contains unsupported bundle files')
  }

  const expectedFiles = bundleFilesForSections(sections)
  if (files.length !== expectedFiles.length || expectedFiles.some((file) => !files.includes(file))) {
    throw new Error('manifest.json files do not match export sections')
  }
  for (const file of files) {
    if (!fs.existsSync(path.join(srcDir, file))) {
      throw new Error(`${file} not found in import bundle`)
    }
  }

  return {
    schema_version: 2,
    exported_at: typeof raw.exported_at === 'string' ? raw.exported_at : '',
    sections,
    files,
  }
}

function addTables(set: Set<string>, names: string[]): void {
  for (const name of names) set.add(name)
}

function replicateTableSchema(source: DatabaseSync, dest: DatabaseSync, tableName: string): void {
  const createSQL = source.prepare(`
    SELECT sql
    FROM sqlite_master
    WHERE type = 'table' AND name = ?
  `).get(tableName) as { sql?: string } | undefined
  if (!createSQL?.sql) return
  dest.exec(createSQL.sql)
  const indexes = source.prepare(`
    SELECT sql
    FROM sqlite_master
    WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL
  `).all(tableName) as Array<{ sql?: string }>
  for (const index of indexes) {
    if (index.sql) dest.exec(index.sql)
  }
}

function copyTableRows(source: DatabaseSync, dest: DatabaseSync, tableName: string): void {
  if (!tableExists(source, tableName)) return
  const rows = source.prepare(`SELECT * FROM "${tableName}"`).all() as Array<Record<string, unknown>>
  if (rows.length === 0) return
  const columns = Object.keys(rows[0])
  const placeholders = columns.map(() => '?').join(', ')
  const insert = dest.prepare(
    `INSERT INTO "${tableName}" (${columns.map((column) => `"${column}"`).join(', ')}) VALUES (${placeholders})`,
  )
  for (const row of rows) {
    insert.run(...columns.map((column) => {
      const value = row[column]
      return value === undefined ? null : (value as string | number | bigint | Uint8Array | null)
    }))
  }
}

function tableExists(db: DatabaseSync, tableName: string): boolean {
  const row = db.prepare(`
    SELECT 1
    FROM sqlite_master
    WHERE type = 'table' AND name = ?
  `).get(tableName)
  return Boolean(row)
}

function listDesktopLogs(input?: {
  source?: 'all' | 'main' | 'sidecar'
  level?: 'all' | 'info' | 'warn' | 'error' | 'debug' | 'other'
  search?: string
  limit?: number
}): Array<{ timestamp: string; level: 'info' | 'warn' | 'error' | 'debug' | 'other'; source: 'main' | 'sidecar'; message: string; raw: string }> {
  const fs = require('fs') as typeof import('fs')
  const files = getDesktopLogPaths()
  const sources = input?.source === 'main'
    ? [{ source: 'main' as const, file: files.main }]
    : input?.source === 'sidecar'
      ? [{ source: 'sidecar' as const, file: files.sidecar }]
      : [
          { source: 'main' as const, file: files.main },
          { source: 'sidecar' as const, file: files.sidecar },
        ]
  const search = input?.search?.trim().toLowerCase() ?? ''
  const limit = Math.min(Math.max(input?.limit ?? 200, 1), 1000)
  const entries = sources.flatMap(({ source, file }) => {
    if (!fs.existsSync(file)) return []
    const lines = readLogTailLines(file, Math.max(limit * 5, 200))
    return lines.map((line) => parseDesktopLogLine(source, line))
  })

  return entries
    .filter((entry) => input?.level && input.level !== 'all' ? entry.level === input.level : true)
    .filter((entry) => search ? entry.raw.toLowerCase().includes(search) || entry.message.toLowerCase().includes(search) : true)
    .sort((a, b) => b.timestamp.localeCompare(a.timestamp))
    .slice(0, limit)
}

function readLogTailLines(filePath: string, maxLines: number): string[] {
  const fs = require('fs') as typeof import('fs')
  const fd = fs.openSync(filePath, 'r')
  try {
    const stat = fs.fstatSync(fd)
    if (stat.size === 0) return []
    const chunkSize = 64 * 1024
    let position = stat.size
    let pending = ''
    const lines: string[] = []

    while (position > 0 && lines.length <= maxLines) {
      const start = Math.max(0, position - chunkSize)
      const size = position - start
      const buffer = Buffer.alloc(size)
      fs.readSync(fd, buffer, 0, size, start)
      const text = buffer.toString('utf-8') + pending
      const parts = text.split(/\r?\n/)
      pending = parts.shift() ?? ''
      for (let i = parts.length - 1; i >= 0 && lines.length <= maxLines; i -= 1) {
        const line = parts[i]?.trim()
        if (line) lines.push(line)
      }
      position = start
    }

    if (pending.trim() && lines.length <= maxLines) {
      lines.push(pending.trim())
    }

    return lines.reverse()
  } finally {
    fs.closeSync(fd)
  }
}

function parseDesktopLogLine(source: 'main' | 'sidecar', line: string): {
  timestamp: string
  level: 'info' | 'warn' | 'error' | 'debug' | 'other'
  source: 'main' | 'sidecar'
  message: string
  raw: string
} {
  const match = line.match(/^\[(.+?)\]\s+\[(.+?)\]\s+(.*)$/)
  const timestamp = match?.[1] ?? new Date(0).toISOString()
  const tag = (match?.[2] ?? '').toLowerCase()
  const message = match?.[3] ?? line
  const level = tag.includes('error')
    ? 'error'
    : tag.includes('warn')
      ? 'warn'
      : tag.includes('debug')
        ? 'debug'
        : tag.includes('info') || tag.includes('log') || tag.includes('session') || tag.includes('stdout')
          ? 'info'
          : 'other'
  return { timestamp, level, source, message, raw: line }
}

async function loadMonthlyUsage(): Promise<unknown> {
  const apiBaseUrl = getLocalApiBaseUrl()
  if (!apiBaseUrl) return null
  const now = new Date()
  const token = getDesktopAccessToken()
  return await makeApiRequest(`${apiBaseUrl}/v1/me/usage?year=${now.getUTCFullYear()}&month=${now.getUTCMonth() + 1}`, 'GET', token)
}
