import type { AppUpdaterState } from '@arkloop/shared/desktop'
import { getDesktopApi, getDesktopAppVersion, isDesktop } from '@arkloop/shared/desktop'

const STORAGE_KEY = 'arkloop:dev:mock-app-update'

export type DevAppUpdateMockPhase = Exclude<
  AppUpdaterState['phase'],
  'idle' | 'unsupported'
>

const VALID_PHASES = new Set<DevAppUpdateMockPhase>([
  'checking',
  'available',
  'not-available',
  'downloading',
  'downloaded',
  'error',
])

type AppUpdaterApi = NonNullable<ReturnType<typeof getDesktopApi>>['appUpdater']

function parsePhase(raw: string | null | undefined): DevAppUpdateMockPhase | null {
  const value = raw?.trim()
  if (!value || !VALID_PHASES.has(value as DevAppUpdateMockPhase)) return null
  return value as DevAppUpdateMockPhase
}

export function readDevAppUpdateMockPhase(): DevAppUpdateMockPhase | null {
  if (!import.meta.env.DEV || !isDesktop()) return null

  try {
    const stored = parsePhase(localStorage.getItem(STORAGE_KEY))
    if (stored) return stored
  } catch {
    // storage unavailable
  }

  return parsePhase(import.meta.env.VITE_DEV_MOCK_APP_UPDATE)
}

export function isDevAppUpdateMockEnabled(): boolean {
  return readDevAppUpdateMockPhase() !== null
}

function resolveCurrentVersion(state: AppUpdaterState | null | undefined): string {
  return state?.currentVersion?.trim()
    || getDesktopAppVersion()?.trim()
    || '26.5.15.1'
}

export function mockLatestVersion(currentVersion: string): string {
  const segments = currentVersion.split('.')
  const last = Number(segments[segments.length - 1])
  if (Number.isFinite(last)) {
    segments[segments.length - 1] = String(last + 4)
    return segments.join('.')
  }
  return `${currentVersion}.mock`
}

export function buildDevMockAppUpdaterState(
  phase: DevAppUpdateMockPhase,
  base?: AppUpdaterState | null,
): AppUpdaterState {
  const currentVersion = resolveCurrentVersion(base)
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

export function applyDevAppUpdateMock(state: AppUpdaterState | null | undefined): AppUpdaterState | null {
  const phase = readDevAppUpdateMockPhase()
  if (!phase) return state ?? null
  return buildDevMockAppUpdaterState(phase, state)
}

function transitionMockPhase(current: AppUpdaterState, next: DevAppUpdateMockPhase): AppUpdaterState {
  return buildDevMockAppUpdaterState(next, current)
}

export function createDevMockAppUpdaterApi(
  real?: AppUpdaterApi,
  listeners = new Set<(state: AppUpdaterState) => void>(),
): NonNullable<AppUpdaterApi> {
  let mockState = buildDevMockAppUpdaterState(readDevAppUpdateMockPhase() ?? 'downloaded')

  const emit = (state: AppUpdaterState) => {
    mockState = state
    listeners.forEach((listener) => listener(state))
  }

  return {
    getState: async () => {
      const realState = real ? await real.getState().catch(() => null) : null
      const next = buildDevMockAppUpdaterState(readDevAppUpdateMockPhase() ?? mockState.phase as DevAppUpdateMockPhase, realState ?? mockState)
      mockState = next
      return next
    },
    check: async () => {
      const phase = readDevAppUpdateMockPhase() ?? 'available'
      const next = transitionMockPhase(mockState, phase === 'downloaded' ? 'downloaded' : 'available')
      emit(next)
      return next
    },
    download: async () => {
      const next = transitionMockPhase(mockState, 'downloading')
      emit(next)
      await new Promise((resolve) => window.setTimeout(resolve, 300))
      const downloaded = transitionMockPhase(next, 'downloaded')
      emit(downloaded)
      return downloaded
    },
    install: async () => {
      if (mockState.phase !== 'downloaded') {
        throw new Error('update not downloaded')
      }
      return { ok: true }
    },
    onState: (callback) => {
      listeners.add(callback)
      callback(mockState)
      const unsubscribeReal = real?.onState((state) => {
        const next = applyDevAppUpdateMock(state)
        if (!next) return
        mockState = next
        callback(next)
      })
      return () => {
        listeners.delete(callback)
        unsubscribeReal?.()
      }
    },
  }
}

let devMockApi: NonNullable<AppUpdaterApi> | null = null
const devMockListeners = new Set<(state: AppUpdaterState) => void>()

export function resolveAppUpdaterApi(): AppUpdaterApi | undefined {
  const real = getDesktopApi()?.appUpdater
  if (!isDevAppUpdateMockEnabled()) {
    devMockApi = null
    return real
  }
  if (!devMockApi) {
    devMockApi = createDevMockAppUpdaterApi(real, devMockListeners)
  }
  return devMockApi
}
