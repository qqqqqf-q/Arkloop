import { ipcMain, BrowserWindow } from 'electron'
import { loadConfig, saveConfig, getConfigPath } from './config'
import {
  getSidecarStatus,
  getSidecarRuntime,
  downloadSidecar,
  isSidecarAvailable,
  checkSidecarVersion,
  getDesktopAccessToken,
  getBridgeBaseUrl,
  type SidecarRuntime,
} from './sidecar'
import { getRootfsStatus, isRootfsAvailable, getRootfsPath, checkRootfsVersion, downloadRootfs, deleteRootfs } from './rootfs'
import type { AppConfig, ConnectorsConfig, MemoryConfig } from './types'

type DesktopController = {
  applyConfigUpdate: (config: AppConfig) => Promise<AppConfig>
  restartLocalSidecar: () => Promise<SidecarRuntime>
  getSidecarRuntime: () => SidecarRuntime
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

  ipcMain.handle('arkloop:sidecar:runtime', () => {
    return controller.getSidecarRuntime()
  })

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
    return checkSidecarVersion()
  })

  ipcMain.handle('arkloop:rootfs:status', () => {
    return getRootfsStatus()
  })

  ipcMain.handle('arkloop:rootfs:available', () => {
    return isRootfsAvailable()
  })

  ipcMain.handle('arkloop:rootfs:path', () => {
    return getRootfsPath()
  })

  ipcMain.handle('arkloop:rootfs:check-version', async () => {
    return checkRootfsVersion()
  })

  ipcMain.handle('arkloop:rootfs:download', async () => {
    await downloadRootfs((progress) => {
      const win = getWindow()
      if (win) win.webContents.send('arkloop:rootfs:download-progress', progress)
    })
    return { ok: true }
  })

  ipcMain.handle('arkloop:rootfs:delete', async () => {
    await deleteRootfs()
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
    await controller.applyConfigUpdate(next)
    return { ok: true }
  })

  ipcMain.handle('arkloop:memory:list', async (_event, agentId?: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { entries: [] }
    const token = getDesktopAccessToken()
    const url = `${apiBaseUrl}/v1/desktop/memory/entries?agent_id=${encodeURIComponent(agentId ?? 'default')}`
    const resp = await makeApiRequest(url, 'GET', token)
    return resp
  })

  ipcMain.handle('arkloop:memory:delete', async (_event, id: string, agentId?: string) => {
    const apiBaseUrl = getLocalApiBaseUrl()
    if (!apiBaseUrl) return { status: 'error', message: 'sidecar not running' }
    const token = getDesktopAccessToken()
    const url = `${apiBaseUrl}/v1/desktop/memory/entries/${encodeURIComponent(id)}?agent_id=${encodeURIComponent(agentId ?? 'default')}`
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
