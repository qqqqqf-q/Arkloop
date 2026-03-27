import { app, BrowserWindow, Menu } from 'electron'
import * as path from 'path'
import { loadConfig, normalizeConfig, saveConfig } from './config'
import {
  startSidecar,
  stopSidecar,
  setStatusListener,
  setRuntimeListener,
  setBridgeUrlListener,
  setMemoryConfig,
  getSidecarRuntime,
  getBridgeBaseUrl,
  stopBridgeOpenvikingIfNeeded,
  ensureOpenCLI,
  type SidecarRuntime,
} from './sidecar'
import { createTray, registerGlobalShortcut, destroyTray } from './tray'
import { registerIpcHandlers } from './ipc'
import { initVersionsFile } from './config'
import { setupAppUpdater } from './app-updater'
import type { AppConfig, ApplyConfigUpdateOptions } from './types'

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

function syncBridgeBaseUrlToRenderer(bridgeBaseUrl: string): void {
  const win = getWindow()
  if (win) {
    win.webContents.send('arkloop:bridge:url-changed', bridgeBaseUrl)
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

  setMemoryConfig(config.memory)
  void ensureOpenCLI()

  const runtime = await startSidecar(config.local.port, config.local.portMode)
  const next = mergeConfigWithRuntime(config, runtime)
  syncActiveSidecarPort(next, runtime)
  if (next.local.port !== config.local.port || next.local.portMode !== config.local.portMode) {
    saveConfig(next)
  }
  return next
}

function memoryChanged(a: AppConfig, b: AppConfig): boolean {
  return a.memory.enabled !== b.memory.enabled
    || a.memory.provider !== b.memory.provider
    || JSON.stringify(a.memory.openviking) !== JSON.stringify(b.memory.openviking)
}

async function applyConfigUpdate(
  config: AppConfig,
  options?: ApplyConfigUpdateOptions,
): Promise<AppConfig> {
  const previous = loadConfig()
  const candidate = normalizeConfig(config)
  const forceLocalReload = Boolean(options?.forceLocalSidecarRestart) && candidate.mode === 'local'
  const needsRestart = previous.mode !== candidate.mode
    || previous.local.port !== candidate.local.port
    || previous.local.portMode !== candidate.local.portMode
    || memoryChanged(previous, candidate)
    || forceLocalReload

  if (!needsRestart) {
    saveConfig(candidate)
    syncActiveSidecarPort(candidate, getSidecarRuntime())
    syncConfigToRenderer(candidate)
    return candidate
  }

  const wasOpenviking = previous.mode === 'local'
    && previous.memory.enabled
    && previous.memory.provider === 'openviking'
  const wantOpenviking = candidate.mode === 'local'
    && candidate.memory.enabled
    && candidate.memory.provider === 'openviking'
  if (wasOpenviking && !wantOpenviking) {
    await stopBridgeOpenvikingIfNeeded(previous.memory)
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

function attachRendererContextMenu(win: BrowserWindow): void {
  win.webContents.on('context-menu', (_event, params) => {
    const template: Electron.MenuItemConstructorOptions[] = []

    if (params.isEditable) {
      template.push(
        { role: 'undo' },
        { role: 'redo' },
        { type: 'separator' },
        { role: 'cut' },
        { role: 'copy' },
        { role: 'paste' },
        { type: 'separator' },
        { role: 'selectAll' },
      )
    } else {
      if (params.selectionText && params.selectionText.trim().length > 0) {
        template.push({ role: 'copy' })
      }
      if (params.mediaType === 'image') {
        template.push({
          label: '复制图片',
          click: () => {
            win.webContents.copyImageAt(Math.floor(params.x), Math.floor(params.y))
          },
        })
      }
    }

    if (template.length === 0) {
      return
    }
    Menu.buildFromTemplate(template).popup({ window: win })
  })
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

  attachRendererContextMenu(win)

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
let shutdownInProgress = false

app.whenReady().then(async () => {
  initVersionsFile()

  setStatusListener((status) => {
    mainWindow?.webContents.send('arkloop:sidecar:status-changed', status)
  })
  setRuntimeListener((runtime) => {
    handleRuntimeUpdate(runtime)
  })
  setBridgeUrlListener((bridgeBaseUrl) => {
    syncBridgeBaseUrlToRenderer(bridgeBaseUrl)
  })

  registerIpcHandlers(getWindow, {
    applyConfigUpdate,
    restartLocalSidecar,
    getSidecarRuntime: async () => getSidecarRuntime(),
  })

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
  syncBridgeBaseUrlToRenderer(getBridgeBaseUrl())

  createTray(getWindow)
  registerGlobalShortcut(getWindow)
  setupAppUpdater(getWindow)
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

app.on('before-quit', (e) => {
  if (shutdownInProgress) return
  e.preventDefault()
  shutdownInProgress = true
  isQuitting = true
  void (async () => {
    destroyTray()
    try {
      const cfg = loadConfig()
      if (cfg.mode === 'local') {
        await stopBridgeOpenvikingIfNeeded(cfg.memory)
      }
      await stopSidecar()
    } catch (err) {
      console.error('[desktop] shutdown error:', err)
    }
    app.quit()
  })()
})
