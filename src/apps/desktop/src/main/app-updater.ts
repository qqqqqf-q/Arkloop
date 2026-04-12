import { app } from 'electron'
import { autoUpdater } from 'electron-updater'

export type AppUpdaterPhase =
  | 'idle'
  | 'unsupported'
  | 'checking'
  | 'available'
  | 'not-available'
  | 'downloading'
  | 'downloaded'
  | 'error'

export type AppUpdaterState = {
  supported: boolean
  phase: AppUpdaterPhase
  currentVersion: string
  latestVersion: string | null
  progressPercent: number
  error: string | null
}

const baseState = (): AppUpdaterState => ({
  supported: app.isPackaged,
  phase: app.isPackaged ? 'idle' : 'unsupported',
  currentVersion: app.getVersion(),
  latestVersion: null,
  progressPercent: 0,
  error: null,
})

let state: AppUpdaterState = baseState()
let initialized = false
let getWindowRef: (() => Electron.BrowserWindow | null) | null = null

function extractVersion(value: unknown): string | null {
  if (!value || typeof value !== 'object') return null
  const maybeVersion = (value as { version?: unknown }).version
  return typeof maybeVersion === 'string' && maybeVersion.trim() ? maybeVersion : null
}

function patchState(patch: Partial<AppUpdaterState>): void {
  state = { ...state, ...patch, currentVersion: app.getVersion(), supported: app.isPackaged }
  const win = getWindowRef?.()
  if (win) {
    win.webContents.send('arkloop:app-updater:state', state)
  }
}

export function getAppUpdaterState(): AppUpdaterState {
  return { ...state, currentVersion: app.getVersion(), supported: app.isPackaged }
}

export async function checkForAppUpdates(): Promise<AppUpdaterState> {
  if (!app.isPackaged) {
    patchState({ phase: 'unsupported', latestVersion: null, progressPercent: 0, error: null })
    return getAppUpdaterState()
  }

  patchState({ phase: 'checking', progressPercent: 0, error: null })
  try {
    await autoUpdater.checkForUpdates()
    return getAppUpdaterState()
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    patchState({ phase: 'error', error: message, progressPercent: 0 })
    throw error
  }
}

export async function downloadAppUpdate(): Promise<AppUpdaterState> {
  if (!app.isPackaged) {
    patchState({ phase: 'unsupported', latestVersion: null, progressPercent: 0, error: null })
    return getAppUpdaterState()
  }

  patchState({ phase: 'downloading', progressPercent: 0, error: null })
  try {
    await autoUpdater.downloadUpdate()
    return getAppUpdaterState()
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    patchState({ phase: 'error', error: message, progressPercent: 0 })
    throw error
  }
}

export function installAppUpdate(): void {
  if (!app.isPackaged) {
    patchState({ phase: 'unsupported', latestVersion: null, progressPercent: 0, error: null })
    return
  }
  if (state.phase !== 'downloaded') {
    throw new Error('update not downloaded')
  }
  autoUpdater.quitAndInstall(false, true)
}

export function setupAppUpdater(getWindow: () => Electron.BrowserWindow | null): void {
  getWindowRef = getWindow
  state = baseState()

  if (initialized) {
    patchState({})
    return
  }

  initialized = true
  if (!app.isPackaged) {
    patchState({})
    return
  }

  autoUpdater.autoDownload = true
  autoUpdater.autoInstallOnAppQuit = true

  autoUpdater.on('checking-for-update', () => {
    patchState({ phase: 'checking', progressPercent: 0, error: null })
  })

  autoUpdater.on('update-available', (info) => {
    patchState({
      phase: 'available',
      latestVersion: extractVersion(info),
      progressPercent: 0,
      error: null,
    })
  })

  autoUpdater.on('update-not-available', (info) => {
    patchState({
      phase: 'not-available',
      latestVersion: extractVersion(info) ?? app.getVersion(),
      progressPercent: 0,
      error: null,
    })
  })

  autoUpdater.on('download-progress', (progress) => {
    patchState({
      phase: 'downloading',
      progressPercent: Math.max(0, Math.min(100, Math.round(progress.percent))),
      error: null,
    })
  })

  autoUpdater.on('update-downloaded', (info) => {
    patchState({
      phase: 'downloaded',
      latestVersion: extractVersion(info),
      progressPercent: 100,
      error: null,
    })
  })

  autoUpdater.on('error', (err) => {
    console.error('[app-updater]', err.message)
    patchState({ phase: 'error', error: err.message, progressPercent: 0 })
  })

  patchState({})
  void checkForAppUpdates().catch(() => {})
}
