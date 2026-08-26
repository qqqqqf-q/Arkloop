import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

const checkUpdater = vi.fn()
const getCachedUpdater = vi.fn()
const applyUpdate = vi.fn()
const onUpdaterProgress = vi.fn(() => () => {})
const getAppUpdaterState = vi.fn()
const checkAppUpdater = vi.fn()
const downloadAppUpdate = vi.fn()
const installAppUpdate = vi.fn()
const onAppUpdaterState = vi.fn(() => () => {})
const getCliToolStatus = vi.fn()
const installCliTool = vi.fn()

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
          getCached: getCachedUpdater,
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
        cliTool: {
          getStatus: getCliToolStatus,
          install: installCliTool,
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
  getCachedUpdater.mockReset()
  applyUpdate.mockReset()
  onUpdaterProgress.mockReset()
  getAppUpdaterState.mockReset()
  checkAppUpdater.mockReset()
  downloadAppUpdate.mockReset()
  installAppUpdate.mockReset()
  onAppUpdaterState.mockReset()
  getCliToolStatus.mockReset()
  installCliTool.mockReset()

  onUpdaterProgress.mockReturnValue(() => {})
  onAppUpdaterState.mockReturnValue(() => {})
  getCachedUpdater.mockResolvedValue({
    bins: {
      rtk: { current: '1.0.0', latest: '1.0.0', available: false },
      opencli: { current: '1.0.0', latest: '1.0.0', available: false },
    },
  })
  checkUpdater.mockResolvedValue({
    bins: {
      rtk: { current: '1.0.0', latest: '1.0.0', available: false },
      opencli: { current: '1.0.0', latest: '1.0.0', available: false },
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
  getCliToolStatus.mockResolvedValue({
    available: true,
    installed: false,
    sourcePath: '/bundle/ark',
    targetPath: '/usr/local/bin/ark',
  })
  installCliTool.mockResolvedValue({
    available: true,
    installed: true,
    sourcePath: '/bundle/ark',
    targetPath: '/usr/local/bin/ark',
  })
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

  it('下载失败时显示原始报错', async () => {
    downloadAppUpdate.mockRejectedValueOnce(new Error('ZIP file not provided'))
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const downloadButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '下载')
    expect(downloadButton).toBeTruthy()

    await act(async () => {
      downloadButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(container.textContent).toContain('ZIP file not provided')
  })

  it('展示并安装 ark 命令行工具', async () => {
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('命令行')
    expect(container.textContent).toContain('ark')
    expect(container.textContent).toContain('未安装')

    const installButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '安装')
    expect(installButton).toBeTruthy()

    await act(async () => {
      installButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(installCliTool).toHaveBeenCalledTimes(1)
    expect(container.textContent).toContain('已安装')
  })

  it('检查更新失败时显示原始报错', async () => {
    checkAppUpdater.mockRejectedValueOnce(new Error('ZIP file not provided'))
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('ZIP file not provided')
  })

  it('组件更新失败时保留原始报错', async () => {
    checkUpdater.mockResolvedValue({
      bins: {
        rtk: { current: '1.0.0', latest: '1.1.0', available: true },
        opencli: { current: '1.0.0', latest: '1.0.0', available: false },
      },
    })
    applyUpdate.mockRejectedValueOnce(new Error('module apply failed'))
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const applyButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '更新到最新')
    expect(applyButton).toBeTruthy()

    await act(async () => {
      applyButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(container.textContent).toContain('module apply failed')
  })

  it('未安装组件有远端版本时不显示更新入口', async () => {
    const status = {
      bins: {
        rtk: { current: null, latest: '66.66.66', available: false },
        opencli: { current: null, latest: '55.55.55', available: false },
      },
    }
    getCachedUpdater.mockResolvedValue(status)
    checkUpdater.mockResolvedValue(status)
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('未安装')
    expect(container.textContent).not.toContain('99.99.99')
    const hasApplyButton = Array.from(container.querySelectorAll('button'))
      .some((button) => button.textContent?.trim() === '更新到最新')
    expect(hasApplyButton).toBe(false)
  })

  it('没有组件更新数据时不显示占位横杠', async () => {
    checkUpdater.mockResolvedValueOnce(null)
    const { UpdateSettingsContent, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <UpdateSettingsContent />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).not.toContain('—')
  })
})
