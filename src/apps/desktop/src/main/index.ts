import { app, BrowserWindow, session } from 'electron'
import * as path from 'path'
import { loadConfig, normalizeConfig, saveConfig } from './config'
import {
  startSidecar,
  stopSidecar,
  setStatusListener,
  setRuntimeListener,
  getSidecarRuntime,
  type SidecarRuntime,
} from './sidecar'
import { createTray, registerGlobalShortcut, destroyTray } from './tray'
import { registerIpcHandlers } from './ipc'
import type { AppConfig } from './types'

let mainWindow: BrowserWindow | null = null
let activeSidecarPort: number | null = null

function getWindow(): BrowserWindow | null {
  return mainWindow
}

function mergeConfigWithRuntime(config: AppConfig, runtime: SidecarRuntime): AppConfig {
  if (config.mode !== 'local') return config
  return normalizeConfig({
    ...config,
    local: {
      ...config.local,
      port: runtime.port ?? config.local.port,
      portMode: runtime.portMode,
    },
  })
}

function syncConfigToRenderer(config: AppConfig): void {
  const win = getWindow()
  if (win) {
    win.webContents.send('arkloop:config:changed', config)
  }
}

function syncRuntimeToRenderer(runtime: SidecarRuntime): void {
  const win = getWindow()
  if (win) {
    win.webContents.send('arkloop:sidecar:runtime-changed', runtime)
  }
}

function syncActiveSidecarPort(config: AppConfig, runtime: SidecarRuntime): void {
  activeSidecarPort = config.mode === 'local'
    ? (runtime.port ?? config.local.port)
    : null
}

function handleRuntimeUpdate(runtime: SidecarRuntime): void {
  const current = loadConfig()
  const next = mergeConfigWithRuntime(current, runtime)
  syncActiveSidecarPort(next, runtime)
  if (next.local.port !== current.local.port || next.local.portMode !== current.local.portMode) {
    saveConfig(next)
    syncConfigToRenderer(next)
  }
  syncRuntimeToRenderer(runtime)
}

async function ensureLocalSidecar(config: AppConfig): Promise<AppConfig> {
  if (config.mode !== 'local') {
    activeSidecarPort = null
    return config
  }

  const runtime = await startSidecar(config.local.port, config.local.portMode)
  const next = mergeConfigWithRuntime(config, runtime)
  syncActiveSidecarPort(next, runtime)
  if (next.local.port !== config.local.port || next.local.portMode !== config.local.portMode) {
    saveConfig(next)
  }
  return next
}

async function applyConfigUpdate(config: AppConfig): Promise<AppConfig> {
  const previous = loadConfig()
  const candidate = normalizeConfig(config)
  const needsRestart = previous.mode !== candidate.mode
    || previous.local.port !== candidate.local.port
    || previous.local.portMode !== candidate.local.portMode

  if (!needsRestart) {
    saveConfig(candidate)
    syncActiveSidecarPort(candidate, getSidecarRuntime())
    syncConfigToRenderer(candidate)
    return candidate
  }

  await stopSidecar()
  try {
    const applied = await ensureLocalSidecar(candidate)
    saveConfig(applied)
    syncConfigToRenderer(applied)
    return applied
  } catch (error) {
    if (previous.mode === 'local') {
      try {
        const restored = await ensureLocalSidecar(previous)
        saveConfig(restored)
        syncConfigToRenderer(restored)
      } catch {}
    } else {
      activeSidecarPort = null
    }
    throw error
  }
}

async function restartLocalSidecar(): Promise<SidecarRuntime> {
  const config = loadConfig()
  await stopSidecar()
  const next = await ensureLocalSidecar(config)
  saveConfig(next)
  syncConfigToRenderer(next)
  return getSidecarRuntime()
}

function isActiveSidecarRequest(urlString: string): boolean {
  try {
    const parsed = new URL(urlString)
    return parsed.protocol === 'http:'
      && parsed.hostname === '127.0.0.1'
      && activeSidecarPort != null
      && parsed.port === String(activeSidecarPort)
  } catch {
    return false
  }
}

function registerSidecarSessionHooks(): void {
  session.defaultSession.webRequest.onBeforeSendHeaders(
    { urls: ['http://127.0.0.1:*/*'] },
    (details, callback) => {
      if (isActiveSidecarRequest(details.url)) {
        delete details.requestHeaders.Origin
      }
      callback({ requestHeaders: details.requestHeaders })
    },
  )

  session.defaultSession.webRequest.onHeadersReceived(
    { urls: ['http://127.0.0.1:*/*'] },
    (details, callback) => {
      if (!isActiveSidecarRequest(details.url)) {
        callback({})
        return
      }

      const headers = details.responseHeaders ?? {}
      headers['Access-Control-Allow-Origin'] = ['*']
      headers['Access-Control-Allow-Methods'] = ['GET, POST, PUT, PATCH, DELETE, OPTIONS']
      headers['Access-Control-Allow-Headers'] = ['Content-Type, Authorization, X-Trace-Id']
      callback({ responseHeaders: headers })
    },
  )
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
  setStatusListener((status) => {
    mainWindow?.webContents.send('arkloop:sidecar:status-changed', status)
  })
  setRuntimeListener((runtime) => {
    handleRuntimeUpdate(runtime)
  })

  registerIpcHandlers(getWindow, {
    applyConfigUpdate,
    restartLocalSidecar,
    getSidecarRuntime,
  })
  registerSidecarSessionHooks()

  const config = loadConfig()
  if (config.mode === 'local') {
    try {
      await ensureLocalSidecar(config)
    } catch (error) {
      console.error('[desktop] failed to start local sidecar:', error)
    }
  } else {
    activeSidecarPort = null
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
