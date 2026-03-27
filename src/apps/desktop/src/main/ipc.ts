import { ipcMain, BrowserWindow } from 'electron'
import os from 'os'
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
import { DEFAULT_CONFIG } from './types'
import type { AppConfig, ApplyConfigUpdateOptions, ConnectorsConfig, MemoryConfig } from './types'

type DesktopController = {
  applyConfigUpdate: (config: AppConfig, options?: ApplyConfigUpdateOptions) => Promise<AppConfig>
  restartLocalSidecar: () => Promise<SidecarRuntime>
  getSidecarRuntime: () => Promise<SidecarRuntime>
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

  ipcMain.handle('arkloop:updater:apply', async (_event, { component }: { component: 'sidecar' | 'openviking' | 'sandbox_kernel' | 'sandbox_rootfs' }) => {
    const win = getWindow()
    await applyUpdate(component, (progress) => {
      if (win) win.webContents.send('arkloop:updater:progress', { component, ...progress })
    })
    // sidecar 和 sandbox 组件需要重启 sidecar 进程才能加载新文件
    if (component === 'sidecar' || component === 'sandbox_kernel' || component === 'sandbox_rootfs') {
      await controller.restartLocalSidecar()
    }
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
    const providerGroups = await fetchEffectiveToolProviders()
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
    if (!apiBaseUrl) return { memory_block: '' }
    const token = getDesktopAccessToken()
    const url = `${apiBaseUrl}/v1/desktop/memory/snapshot?agent_id=${encodeURIComponent(agentId ?? 'default')}`
    const resp = await makeApiRequest(url, 'GET', token)
    return resp
  })

  ipcMain.handle('arkloop:app:version', () => {
    const { app } = require('electron')
    return app.getVersion()
  })

  ipcMain.handle('arkloop:app:quit', () => {
    const { app } = require('electron')
    app.quit()
  })

  ipcMain.handle('arkloop:app:os-username', () => {
    try {
      return os.userInfo().username
    } catch {
      return os.hostname()
    }
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

type ToolProviderScope = 'platform' | 'user'

async function fetchToolProviders(scope: ToolProviderScope = 'platform'): Promise<ToolProviderGroup[]> {
  const apiBaseUrl = getLocalApiBaseUrl()
  if (!apiBaseUrl) return []
  const token = getDesktopAccessToken()
  const resp = await makeApiRequest(`${apiBaseUrl}/v1/tool-providers?scope=${scope}`, 'GET', token)
  if (!resp || typeof resp !== 'object' || !Array.isArray((resp as { groups?: unknown[] }).groups)) {
    return []
  }
  return ((resp as { groups: ToolProviderGroup[] }).groups ?? [])
}

async function fetchEffectiveToolProviders(): Promise<ToolProviderGroup[]> {
  const [platformGroups, userGroups] = await Promise.all([
    fetchToolProviders('platform'),
    fetchToolProviders('user').catch(() => []),
  ])
  return mergeEffectiveToolProviderGroups(platformGroups, userGroups)
}

function mergeEffectiveToolProviderGroups(
  platformGroups: ToolProviderGroup[],
  userGroups: ToolProviderGroup[],
): ToolProviderGroup[] {
  const userByGroup = new Map(userGroups.map((group) => [group.group_name, group]))
  return platformGroups.map((platformGroup) => {
    const userGroup = userByGroup.get(platformGroup.group_name)
    const userByProvider = new Map(userGroup?.providers.map((provider) => [provider.provider_name, provider]) ?? [])
    const activeUserProvider = pickEffectiveActiveProvider(userGroup?.providers ?? [])
    const activePlatformProvider = pickEffectiveActiveProvider(platformGroup.providers)
    const effectiveActive = activeUserProvider ?? activePlatformProvider

    return {
      ...platformGroup,
      providers: platformGroup.providers.map((provider) => {
        const userProvider = userByProvider.get(provider.provider_name)
        const base = userProvider?.is_active ? userProvider : provider
        return {
          ...provider,
          is_active: effectiveActive?.provider_name === provider.provider_name,
          base_url: base.base_url ?? provider.base_url,
        }
      }),
    }
  })
}

function pickEffectiveActiveProvider(providers: ToolProviderItem[]): ToolProviderItem | undefined {
  const readyActive = providers.find((provider) => provider.is_active && provider.runtime_state === 'ready')
  if (readyActive) {
    return readyActive
  }
  return providers.find((provider) => provider.is_active)
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
  const providerGroups = await fetchEffectiveToolProviders()
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
  return (connectors.search.provider === 'tavily' && Boolean(connectors.search.tavilyApiKey))
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
  await deactivateToolProviderGroup('web_search', 'user')
  await deactivateToolProviderGroup('web_search', 'platform')
  if (search.provider === 'tavily') {
    await activateToolProvider('web_search', 'web_search.tavily', 'platform')
    await upsertToolProviderCredential('web_search', 'web_search.tavily', {
      api_key: search.tavilyApiKey ?? '',
    }, 'platform')
    return
  }
  if (search.provider === 'searxng') {
    await activateToolProvider('web_search', 'web_search.searxng', 'platform')
    await upsertToolProviderCredential('web_search', 'web_search.searxng', {
      base_url: search.searxngBaseUrl ?? '',
    }, 'platform')
    return
  }
}

async function applyFetchConnector(fetch: ConnectorsConfig['fetch']): Promise<void> {
  await deactivateToolProviderGroup('web_fetch', 'user')
  await deactivateToolProviderGroup('web_fetch', 'platform')
  if (fetch.provider === 'basic') {
    await activateToolProvider('web_fetch', 'web_fetch.basic', 'platform')
    return
  }
  if (fetch.provider === 'jina') {
    await activateToolProvider('web_fetch', 'web_fetch.jina', 'platform')
    await upsertToolProviderCredential('web_fetch', 'web_fetch.jina', {
      api_key: fetch.jinaApiKey ?? '',
    }, 'platform')
    return
  }
  if (fetch.provider === 'firecrawl') {
    await activateToolProvider('web_fetch', 'web_fetch.firecrawl', 'platform')
    await upsertToolProviderCredential('web_fetch', 'web_fetch.firecrawl', {
      api_key: fetch.firecrawlApiKey ?? '',
      base_url: fetch.firecrawlBaseUrl ?? '',
    }, 'platform')
  }
}

async function deactivateToolProviderGroup(groupName: string, scope: ToolProviderScope): Promise<void> {
  const groups = await fetchToolProviders(scope)
  const group = findProviderGroup(groups, groupName)
  if (!group) return
  for (const provider of group.providers) {
    if (!provider.is_active) continue
    await requestToolProvider(`/v1/tool-providers/${groupName}/${provider.provider_name}/deactivate`, 'PUT', undefined, scope)
  }
}

async function activateToolProvider(groupName: string, providerName: string, scope: ToolProviderScope): Promise<void> {
  await requestToolProvider(`/v1/tool-providers/${groupName}/${providerName}/activate`, 'PUT', undefined, scope)
}

async function upsertToolProviderCredential(
  groupName: string,
  providerName: string,
  payload: Record<string, string>,
  scope: ToolProviderScope,
): Promise<void> {
  const body = JSON.stringify(payload)
  await requestToolProvider(`/v1/tool-providers/${groupName}/${providerName}/credential`, 'PUT', body, scope)
}

async function requestToolProvider(pathname: string, method: string, body?: string, scope: ToolProviderScope = 'platform'): Promise<void> {
  const apiBaseUrl = getLocalApiBaseUrl()
  if (!apiBaseUrl) {
    throw new Error('sidecar not running')
  }
  const token = getDesktopAccessToken()
  const sep = pathname.includes('?') ? '&' : '?'
  const url = `${apiBaseUrl}${pathname}${sep}scope=${scope}`
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
  return new Promise((resolve, reject) => {
    const parsed = new URL(url)
    const options = {
      hostname: parsed.hostname,
      port: parseInt(parsed.port, 10) || 80,
      path: parsed.pathname + parsed.search,
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
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
    if (body) {
      req.write(body)
    }
    req.end()
  })
}
