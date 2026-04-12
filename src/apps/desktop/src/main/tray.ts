import { Tray, Menu, nativeImage, nativeTheme, app, BrowserWindow, globalShortcut } from 'electron'
import * as fs from 'fs'
import * as path from 'path'

let tray: Tray | null = null
type ShowWindow = () => void

function resolveResource(name: string): string {
  const candidates = app.isPackaged
    ? [
        path.join(process.resourcesPath, name),
        path.join(process.resourcesPath, 'app.asar', 'resources', name),
      ]
    : [
        path.join(__dirname, '..', '..', 'resources', name),
      ]
  return candidates.find((c) => fs.existsSync(c)) ?? candidates[0]
}

function getTrayIcon(): Electron.NativeImage {
  if (process.platform === 'darwin') {
    const img = nativeImage.createFromPath(resolveResource('trayTemplate.png'))
    const path2x = resolveResource('trayTemplate@2x.png')
    if (fs.existsSync(path2x)) {
      img.addRepresentation({ scaleFactor: 2.0, dataURL: nativeImage.createFromPath(path2x).toDataURL() })
    }
    img.setTemplateImage(true)
    return img
  }

  const name = nativeTheme.shouldUseDarkColors ? 'tray-icon-light.png' : 'tray-icon-dark.png'
  try {
    return nativeImage.createFromPath(resolveResource(name))
  } catch {
    return nativeImage.createEmpty()
  }
}

export function createTray(getWindow: () => BrowserWindow | null, showWindow: ShowWindow): Tray {
  tray = new Tray(getTrayIcon())
  tray.setToolTip('Arkloop')

  const contextMenu = Menu.buildFromTemplate([
    {
      label: 'Show Arkloop',
      click: () => showWindow(),
    },
    {
      label: 'Settings',
      click: () => {
        showWindow()
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
    showWindow()
  })

  if (process.platform !== 'darwin') {
    nativeTheme.on('updated', () => {
      tray?.setImage(getTrayIcon())
    })
  }

  return tray
}

export function registerGlobalShortcut(getWindow: () => BrowserWindow | null, showWindow: ShowWindow): void {
  globalShortcut.register('CommandOrControl+Shift+A', () => {
    const win = getWindow()
    if (!win) return
    if (win.isVisible() && win.isFocused()) {
      win.hide()
    } else {
      showWindow()
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
