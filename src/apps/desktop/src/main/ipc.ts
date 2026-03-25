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

  ipcMain.handle('arkloop:connectors:get', () => {
    const config = loadConfig()
    return config.connectors
  })

  ipcMain.handle('arkloop:connectors:set', async (_event, connectors: ConnectorsConfig) => {
    const config = loadConfig()
    const next: AppConfig = { ...config, connectors }
    await controller.applyConfigUpdate(next)
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

async function makeApiRequest(url: string, method: string, token: string): Promise<unknown> {
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
      let body = ''
      res.on('data', (chunk: Buffer) => { body += chunk.toString() })
      res.on('end', () => {
        try {
          resolve(JSON.parse(body))
        } catch {
          resolve({ raw: body })
        }
      })
    })
    req.on('error', reject)
    req.end()
  })
}
