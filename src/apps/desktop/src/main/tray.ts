import { Tray, Menu, nativeImage, app, BrowserWindow, globalShortcut } from 'electron'
import * as path from 'path'

let tray: Tray | null = null

function getTrayIcon(): Electron.NativeImage {
  const iconName = process.platform === 'darwin' ? 'tray-icon.png' : 'tray-icon.png'
  const iconPath = app.isPackaged
    ? path.join(process.resourcesPath, iconName)
    : path.join(__dirname, '..', '..', 'resources', iconName)

  try {
    const img = nativeImage.createFromPath(iconPath)
    if (process.platform === 'darwin') img.setTemplateImage(true)
    return img
  } catch {
    return nativeImage.createEmpty()
  }
}

export function createTray(getWindow: () => BrowserWindow | null): Tray {
  tray = new Tray(getTrayIcon())
  tray.setToolTip('Arkloop')

  const contextMenu = Menu.buildFromTemplate([
    {
      label: 'Show',
      click: () => {
        const win = getWindow()
        if (win) {
          win.show()
          win.focus()
        }
      },
    },
    { type: 'separator' },
    {
      label: 'Quit',
      click: () => app.quit(),
    },
  ])
  tray.setContextMenu(contextMenu)

  tray.on('double-click', () => {
    const win = getWindow()
    if (win) {
      win.show()
      win.focus()
    }
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
