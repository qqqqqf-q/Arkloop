import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

const checkUpdater = vi.fn()
const applyUpdate = vi.fn()
const onUpdaterProgress = vi.fn(() => () => {})
const getAppUpdaterState = vi.fn()
const checkAppUpdater = vi.fn()
const downloadAppUpdate = vi.fn()
const installAppUpdate = vi.fn()
const onAppUpdaterState = vi.fn(() => () => {})

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function loadSubject() {
  vi.resetModules()
  vi.doMock('../storage', async () => {
    const actual = await vi.importActual<typeof import('../storage')>('../storage')
    return {
      ...actual,
      readLocaleFromStorage: vi.fn(() => 'zh'),
      writeLocaleToStorage: vi.fn(),
    }
  })
  vi.doMock('@arkloop/shared/desktop', async () => {
    const actual = await vi.importActual<typeof import('@arkloop/shared/desktop')>('@arkloop/shared/desktop')
    return {
      ...actual,
      getDesktopApi: () => ({
        updater: {
          check: checkUpdater,
          apply: applyUpdate,
          onProgress: onUpdaterProgress,
        },
        appUpdater: {
          getState: getAppUpdaterState,
          check: checkAppUpdater,
          download: downloadAppUpdate,
          install: installAppUpdate,
          onState: onAppUpdaterState,
        },
      }),
    }
  })

  const { UpdateSettingsContent } = await import('../components/settings/UpdateSettings')
  const { LocaleProvider } = await import('../contexts/LocaleContext')
  return { UpdateSettingsContent, LocaleProvider }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)

  checkUpdater.mockReset()
  applyUpdate.mockReset()
  onUpdaterProgress.mockReset()
  getAppUpdaterState.mockReset()
  checkAppUpdater.mockReset()
  downloadAppUpdate.mockReset()
  installAppUpdate.mockReset()
  onAppUpdaterState.mockReset()

  onUpdaterProgress.mockReturnValue(() => {})
  onAppUpdaterState.mockReturnValue(() => {})
  checkUpdater.mockResolvedValue({
    openviking: { current: '1.0.0', latest: '1.0.0', available: false },
    sandbox: {
      kernel: { current: '1.0.0', latest: '1.0.0', available: false },
      rootfs: { current: '1.0.0', latest: '1.0.0', available: false },
    },
  })
  getAppUpdaterState.mockResolvedValue({
    supported: true,
    phase: 'available',
    currentVersion: '1.0.0',
    latestVersion: '1.0.1',
    progressPercent: 0,
    error: null,
  })
  checkAppUpdater.mockResolvedValue({
    supported: true,
    phase: 'available',
    currentVersion: '1.0.0',
    latestVersion: '1.0.1',
    progressPercent: 0,
    error: null,
  })
  downloadAppUpdate.mockResolvedValue({
    supported: true,
    phase: 'downloaded',
    currentVersion: '1.0.0',
    latestVersion: '1.0.1',
    progressPercent: 100,
    error: null,
  })
  installAppUpdate.mockResolvedValue({ ok: true })
})

afterEach(() => {
  if (root) {
    act(() => root!.unmount())
  }
  container.remove()
  root = null
  vi.doUnmock('../storage')
  vi.doUnmock('@arkloop/shared/desktop')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('UpdateSettingsContent', () => {
  it('分开展示桌面应用更新和组件更新，并支持下载桌面更新', async () => {
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('桌面应用')
    expect(container.textContent).toContain('组件')
    expect(container.textContent).toContain('1.0.0')
    expect(container.textContent).toContain('1.0.1')

    const downloadButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '下载')
    expect(downloadButton).toBeTruthy()

    await act(async () => {
      downloadButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(downloadAppUpdate).toHaveBeenCalledTimes(1)
  })

  it('忽略未发布 release 的原始错误', async () => {
    checkUpdater.mockRejectedValueOnce(new Error('failed to fetch release info: 404'))
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).not.toContain('failed to fetch release info: 404')
  })
})
