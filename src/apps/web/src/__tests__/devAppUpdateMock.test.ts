import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  applyDevAppUpdateMock,
  buildDevMockAppUpdaterState,
  mockLatestVersion,
  readDevAppUpdateMockPhase,
} from '../devAppUpdateMock'

const desktopMock = vi.hoisted(() => ({
  isDesktop: vi.fn(() => true),
}))

vi.mock('@arkloop/shared/desktop', async () => {
  const actual = await vi.importActual<typeof import('@arkloop/shared/desktop')>('@arkloop/shared/desktop')
  return {
    ...actual,
    isDesktop: desktopMock.isDesktop,
    getDesktopAppVersion: () => '26.5.15.1',
    getDesktopApi: () => ({ appUpdater: {} }),
  }
})

describe('devAppUpdateMock', () => {
  beforeEach(() => {
    desktopMock.isDesktop.mockReturnValue(true)
    vi.stubEnv('DEV', true)
    vi.stubEnv('VITE_DEV_MOCK_APP_UPDATE', 'downloaded')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('读取 Vite mock phase', () => {
    expect(readDevAppUpdateMockPhase()).toBe('downloaded')
  })

  it('构建 downloaded mock 状态', () => {
    const state = buildDevMockAppUpdaterState('downloaded')
    expect(state.supported).toBe(true)
    expect(state.phase).toBe('downloaded')
    expect(state.currentVersion).toBe('26.5.15.1')
    expect(state.latestVersion).toBe(mockLatestVersion('26.5.15.1'))
    expect(state.progressPercent).toBe(100)
  })

  it('覆盖 unsupported 状态', () => {
    vi.stubEnv('VITE_DEV_MOCK_APP_UPDATE', 'available')
    const next = applyDevAppUpdateMock({
      supported: false,
      phase: 'unsupported',
      currentVersion: '26.5.15.1',
      latestVersion: null,
      progressPercent: 0,
      error: null,
    })
    expect(next?.phase).toBe('available')
    expect(next?.supported).toBe(true)
  })
})
