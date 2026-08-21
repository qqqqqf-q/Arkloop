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

const DEV_MOCK_PHASES = new Set<AppUpdaterPhase>([
  'checking',
  'available',
  'not-available',
  'downloading',
  'downloaded',
  'error',
])

function readDevMockPhase(): AppUpdaterPhase | null {
  if (app.isPackaged) return null
  const raw = process.env.ARKLOOP_DEV_MOCK_APP_UPDATE?.trim()
  if (!raw || !DEV_MOCK_PHASES.has(raw as AppUpdaterPhase)) return null
  return raw as AppUpdaterPhase
}

function isUpdaterDisabled(): boolean {
  return process.env.ARKLOOP_DISABLE_APP_UPDATER === '1'
}

function isUpdaterSupported(): boolean {
  if (isUpdaterDisabled()) return false
  return app.isPackaged || readDevMockPhase() !== null
}

function mockLatestVersion(currentVersion: string): string {
  const segments = currentVersion.split('.')
  const last = Number(segments[segments.length - 1])
  if (Number.isFinite(last)) {
    segments[segments.length - 1] = String(last + 4)
    return segments.join('.')
  }
  return `${currentVersion}.mock`
}

function buildDevMockState(phase: AppUpdaterPhase): AppUpdaterState {
  const currentVersion = app.getVersion()
  const latestVersion = mockLatestVersion(currentVersion)
  return {
    supported: true,
    phase,
    currentVersion,
    latestVersion: phase === 'not-available' ? currentVersion : latestVersion,
    progressPercent: phase === 'downloading' ? 42 : phase === 'downloaded' ? 100 : 0,
    error: phase === 'error' ? 'mock update error' : null,
  }
}

const baseState = (): AppUpdaterState => {
  const devMockPhase = readDevMockPhase()
  if (devMockPhase) return buildDevMockState(devMockPhase)
  if (isUpdaterDisabled()) {
    return {
      supported: false,
      phase: 'unsupported',
      currentVersion: app.getVersion(),
      latestVersion: null,
      progressPercent: 0,
      error: null,
    }
  }
  return {
    supported: app.isPackaged,
    phase: app.isPackaged ? 'idle' : 'unsupported',
    currentVersion: app.getVersion(),
    latestVersion: null,
    progressPercent: 0,
    error: null,
  }
}

let state: AppUpdaterState = baseState()
let initialized = false
let getWindowRef: (() => Electron.BrowserWindow | null) | null = null
let updateInstallQuit = false

export function isUpdateInstallQuit(): boolean {
  return updateInstallQuit
}

function extractVersion(value: unknown): string | null {
  if (!value || typeof value !== 'object') return null
  const maybeVersion = (value as { version?: unknown }).version
  return typeof maybeVersion === 'string' && maybeVersion.trim() ? maybeVersion : null
}

function patchState(patch: Partial<AppUpdaterState>): void {
  state = { ...state, ...patch, currentVersion: app.getVersion(), supported: isUpdaterSupported() }
  const win = getWindowRef?.()
  if (win) {
    win.webContents.send('arkloop:app-updater:state', state)
  }
}

export function getAppUpdaterState(): AppUpdaterState {
  return { ...state, currentVersion: app.getVersion(), supported: isUpdaterSupported() }
}

export async function checkForAppUpdates(): Promise<AppUpdaterState> {
  const devMockPhase = readDevMockPhase()
  if (devMockPhase) {
    const nextPhase = devMockPhase === 'downloaded' ? 'downloaded' : 'available'
    patchState(buildDevMockState(nextPhase))
    return getAppUpdaterState()
  }

  if (isUpdaterDisabled() || !app.isPackaged) {
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
  if (readDevMockPhase()) {
    patchState(buildDevMockState('downloading'))
    await new Promise((resolve) => setTimeout(resolve, 300))
    patchState(buildDevMockState('downloaded'))
    return getAppUpdaterState()
  }

  if (isUpdaterDisabled() || !app.isPackaged) {
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
  if (readDevMockPhase()) {
    if (state.phase !== 'downloaded') {
      throw new Error('update not downloaded')
    }
    return
  }

  if (isUpdaterDisabled() || !app.isPackaged) {
    patchState({ phase: 'unsupported', latestVersion: null, progressPercent: 0, error: null })
    return
  }
  if (state.phase !== 'downloaded') {
    throw new Error('update not downloaded')
  }
  updateInstallQuit = true
  autoUpdater.quitAndInstall(false, true)
}

export function setupAppUpdater(
  getWindow: () => Electron.BrowserWindow | null,
  options: { autoCheck: boolean } = { autoCheck: true },
): void {
  getWindowRef = getWindow
  state = baseState()

  if (initialized) {
    patchState({})
    return
  }

  initialized = true
  if (isUpdaterDisabled() || !app.isPackaged) {
    patchState({})
    if (readDevMockPhase() && options.autoCheck) {
      void checkForAppUpdates().catch(() => {})
    }
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
  if (options.autoCheck) {
    void checkForAppUpdates().catch(() => {})
  }
}
