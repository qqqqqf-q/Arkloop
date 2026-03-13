import { ipcMain, BrowserWindow } from 'electron'
import { loadConfig, saveConfig, getConfigPath } from './config'
import { getSidecarStatus, startSidecar, stopSidecar } from './sidecar'
import type { AppConfig } from './types'

export function registerIpcHandlers(getWindow: () => BrowserWindow | null): void {
  // preload 同步获取配置, 确保 __ARKLOOP_DESKTOP__ 在页面脚本之前注入
  ipcMain.on('arkloop:config:get-sync', (event) => {
    event.returnValue = loadConfig()
  })

  ipcMain.handle('arkloop:config:get', () => {
    return loadConfig()
  })

  ipcMain.handle('arkloop:config:set', async (_event, config: AppConfig) => {
    const prev = loadConfig()
    saveConfig(config)

    // 模式切换时重启 sidecar
    if (prev.mode !== config.mode || prev.local.port !== config.local.port) {
      await stopSidecar()
      if (config.mode === 'local') {
        await startSidecar(config.local.port)
      }
      // 通知渲染进程重新加载
      const win = getWindow()
      if (win) win.webContents.send('arkloop:config:changed', config)
    }

    return { ok: true }
  })

  ipcMain.handle('arkloop:config:path', () => {
    return getConfigPath()
  })

  ipcMain.handle('arkloop:sidecar:status', () => {
    return getSidecarStatus()
  })

  ipcMain.handle('arkloop:sidecar:restart', async () => {
    const config = loadConfig()
    await stopSidecar()
    await startSidecar(config.local.port)
    return getSidecarStatus()
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
