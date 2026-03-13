import { app, BrowserWindow, session } from 'electron'
import * as path from 'path'
import { loadConfig, saveConfig } from './config'
import { startSidecar, stopSidecar, setStatusListener } from './sidecar'
import { createTray, registerGlobalShortcut, destroyTray } from './tray'
import { registerIpcHandlers } from './ipc'

let mainWindow: BrowserWindow | null = null

function getWindow(): BrowserWindow | null {
  return mainWindow
}

function createWindow(): BrowserWindow {
  const config = loadConfig()

  const win = new BrowserWindow({
    width: config.window.width,
    height: config.window.height,
    minWidth: 900,
    minHeight: 600,
    title: 'Arkloop',
    show: false,
    webPreferences: {
      preload: path.join(__dirname, '..', 'preload', 'index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    trafficLightPosition: { x: 12, y: 12 },
  })

  // 窗口大小变化时持久化
  win.on('resize', () => {
    if (win.isMaximized()) return
    const [width, height] = win.getSize()
    const cfg = loadConfig()
    cfg.window = { width, height }
    saveConfig(cfg)
  })

  // 关闭时最小化到托盘而非退出
  win.on('close', (e) => {
    if (!isQuitting) {
      e.preventDefault()
      win.hide()
    }
  })

  win.once('ready-to-show', () => {
    win.show()
  })

  return win
}

function loadContent(win: BrowserWindow): void {
  if (process.env.ELECTRON_DEV === 'true') {
    // 开发模式: 加载 Vite dev server
    const devUrl = process.env.VITE_DEV_URL || 'http://localhost:5173'
    win.loadURL(devUrl)
    win.webContents.openDevTools({ mode: 'detach' })
  } else if (app.isPackaged) {
    // 生产打包模式
    const rendererPath = path.join(process.resourcesPath, 'renderer', 'index.html')
    win.loadFile(rendererPath)
  } else {
    // 开发模式但非 ELECTRON_DEV（直接 build 后测试）
    const webDist = path.resolve(__dirname, '..', '..', '..', 'web', 'dist', 'index.html')
    win.loadFile(webDist)
  }
}

let isQuitting = false

app.on('before-quit', () => {
  isQuitting = true
})

app.whenReady().then(async () => {
  const config = loadConfig()

  registerIpcHandlers(getWindow)

  // 为 sidecar 请求注入 CORS 头，生产模式下 file:// -> http://localhost 需要
  if (config.mode === 'local') {
    const sidecarOrigin = `http://127.0.0.1:${config.local.port}`
    session.defaultSession.webRequest.onBeforeSendHeaders(
      { urls: [`${sidecarOrigin}/*`] },
      (details, callback) => {
        delete details.requestHeaders['Origin']
        callback({ requestHeaders: details.requestHeaders })
      },
    )
    session.defaultSession.webRequest.onHeadersReceived(
      { urls: [`${sidecarOrigin}/*`] },
      (details, callback) => {
        const headers = details.responseHeaders ?? {}
        headers['Access-Control-Allow-Origin'] = ['*']
        headers['Access-Control-Allow-Methods'] = ['GET, POST, PUT, PATCH, DELETE, OPTIONS']
        headers['Access-Control-Allow-Headers'] = ['Content-Type, Authorization, X-Trace-Id']
        callback({ responseHeaders: headers })
      },
    )
  }

  // Local 模式下启动 sidecar
  if (config.mode === 'local') {
    setStatusListener((s) => {
      mainWindow?.webContents.send('arkloop:sidecar:status-changed', s)
    })
    await startSidecar(config.local.port)
  }

  mainWindow = createWindow()
  loadContent(mainWindow)

  createTray(getWindow)
  registerGlobalShortcut(getWindow)
})

app.on('window-all-closed', () => {
  // macOS: 保持运行直到用户显式退出
  if (process.platform !== 'darwin') {
    app.quit()
  }
})

app.on('activate', () => {
  if (mainWindow) {
    mainWindow.show()
    mainWindow.focus()
  }
})

app.on('will-quit', async () => {
  destroyTray()
  await stopSidecar()
})
