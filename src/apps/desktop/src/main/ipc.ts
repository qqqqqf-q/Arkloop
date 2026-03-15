import { ipcMain, BrowserWindow } from 'electron'
import { loadConfig, saveConfig, getConfigPath } from './config'
import { getSidecarStatus, downloadSidecar, isSidecarAvailable, checkSidecarVersion, type SidecarRuntime } from './sidecar'
import { getRootfsStatus, isRootfsAvailable, getRootfsPath, checkRootfsVersion, downloadRootfs, deleteRootfs } from './rootfs'
import type { AppConfig } from './types'

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
    event.returnValue = loadConfig()
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

  ipcMain.handle('arkloop:app:version', () => {
    const { app } = require('electron')
    return app.getVersion()
  })

  ipcMain.handle('arkloop:app:quit', () => {
    const { app } = require('electron')
    app.quit()
  })
}
