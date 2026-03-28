import { Tray, Menu, nativeImage, app, BrowserWindow, globalShortcut } from 'electron'
import * as fs from 'fs'
import * as path from 'path'

let tray: Tray | null = null

function getTrayIcon(): Electron.NativeImage {
  const candidates = app.isPackaged
    ? [
        path.join(process.resourcesPath, 'tray-icon.png'),
        path.join(process.resourcesPath, 'app.asar', 'resources', 'tray-icon.png'),
      ]
    : [
        path.join(__dirname, '..', '..', 'resources', 'tray-icon.png'),
      ]
  const iconPath = candidates.find((candidate) => fs.existsSync(candidate)) ?? candidates[0]

  try {
    const img = nativeImage.createFromPath(iconPath)
    if (process.platform === 'darwin') {
      return img.resize({ height: 18 })
    }
    return img
  } catch {
    return nativeImage.createEmpty()
  }
}

export function createTray(getWindow: () => BrowserWindow | null): Tray {
  tray = new Tray(getTrayIcon())
  tray.setToolTip('Arkloop')

  const openWindow = () => {
    const win = getWindow()
    if (win) {
      win.show()
      win.focus()
    }
  }

  const contextMenu = Menu.buildFromTemplate([
    {
      label: 'Show Arkloop',
      click: () => openWindow(),
    },
    {
      label: 'Settings',
      click: () => {
        openWindow()
        const win = getWindow()
        win?.webContents.send('arkloop:app:open-settings')
      },
    },
    { type: 'separator' },
    {
      label: 'Quit Arkloop',
      click: () => app.quit(),
    },
  ])
  tray.setContextMenu(contextMenu)

  tray.on('double-click', () => {
    openWindow()
  })

  return tray
}

export function registerGlobalShortcut(getWindow: () => BrowserWindow | null): void {
  globalShortcut.register('CommandOrControl+Shift+A', () => {
    const win = getWindow()
    if (!win) return
    if (win.isVisible() && win.isFocused()) {
      win.hide()
    } else {
      win.show()
      win.focus()
    }
  })
}

export function destroyTray(): void {
  if (tray) {
    tray.destroy()
    tray = null
  }
  globalShortcut.unregisterAll()
}
