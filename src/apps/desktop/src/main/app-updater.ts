import { app } from 'electron'
import { autoUpdater } from 'electron-updater'

export function setupAppUpdater(getWindow: () => Electron.BrowserWindow | null): void {
  if (!app.isPackaged) return

  autoUpdater.autoDownload = false
  autoUpdater.autoInstallOnAppQuit = true

  autoUpdater.on('update-available', (info) => {
    const win = getWindow()
    if (win) win.webContents.send('arkloop:app:update-available', info)
  })

  autoUpdater.on('update-downloaded', (info) => {
    const win = getWindow()
    if (win) win.webContents.send('arkloop:app:update-downloaded', info)
  })

  autoUpdater.on('error', (err) => {
    console.error('[app-updater]', err.message)
  })

  void autoUpdater.checkForUpdates().catch(() => {})
}
