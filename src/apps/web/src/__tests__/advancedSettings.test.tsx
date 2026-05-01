import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
const addToast = vi.fn()

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  if (root) act(() => root!.unmount())
  root = null
  container.remove()
  vi.doUnmock('@arkloop/shared/desktop')
  vi.doUnmock('@arkloop/shared')
  vi.doUnmock('../openExternal')
  vi.doUnmock('../contexts/AppearanceContext')
  vi.doUnmock('../api')
  vi.doUnmock('../storage')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('AdvancedSettings', () => {
  it('渲染高级中心并切换到日志页', async () => {
    const desktopApi = {
      advanced: {
        getOverview: vi.fn().mockResolvedValue({
          appName: 'Arkloop',
          appVersion: '1.2.3',
          githubUrl: 'https://github.com/qqqqqf-q/Arkloop',
          telegramUrl: null,
          iconDataUrl: null,
          configPath: '/tmp/config.json',
          dataDir: '/tmp/data',
          logsDir: '/tmp/logs',
          sqlitePath: '/tmp/data.db',
          links: [{ label: 'GitHub', url: 'https://github.com/qqqqqf-q/Arkloop' }],
          status: [{ label: 'Sidecar', value: 'running', tone: 'success' }],
          usage: {
            account_id: 'acc-1',
            year: 2026,
            month: 3,
            total_input_tokens: 10,
            total_output_tokens: 20,
            total_cache_creation_tokens: 5,
            total_cache_read_tokens: 6,
            total_cached_tokens: 7,
            total_cost_usd: 0.1234,
            record_count: 2,
          },
        }),
        chooseDataFolder: vi.fn().mockResolvedValue('/tmp/export'),
        exportDataBundle: vi.fn().mockResolvedValue({ ok: true, filePath: '/tmp/export/bundle' }),
        importDataBundle: vi.fn().mockResolvedValue({ ok: true, importedFrom: '/tmp/import' }),
        listLogs: vi.fn().mockResolvedValue({
          entries: [
            {
              timestamp: '2026-03-31T00:00:00Z',
              level: 'info',
              source: 'main',
              message: 'desktop main start',
              raw: 'raw-line',
            },
          ],
        }),
      },
      config: {
        get: vi.fn().mockResolvedValue({
          network: {
            proxyEnabled: false,
            requestTimeoutMs: 30000,
            retryCount: 1,
          },
        }),
        set: vi.fn().mockResolvedValue({ ok: true }),
      },
    }

    vi.doMock('../storage', async () => {
      const actual = await vi.importActual<typeof import('../storage')>('../storage')
      return {
        ...actual,
        readLocaleFromStorage: vi.fn(() => 'zh'),
        writeLocaleToStorage: vi.fn(),
      }
    })
    vi.doMock('../api', async () => {
      const actual = await vi.importActual<typeof import('../api')>('../api')
      return {
        ...actual,
        getMyUsage: vi.fn().mockResolvedValue({
          account_id: 'acc-1',
          year: 2026,
          month: 3,
          total_input_tokens: 10,
          total_output_tokens: 20,
          total_cache_creation_tokens: 5,
          total_cache_read_tokens: 6,
          total_cached_tokens: 7,
          total_cost_usd: 0.1234,
          record_count: 2,
        }),
        getMyDailyUsage: vi.fn().mockResolvedValue([]),
        getMyHourlyUsage: vi.fn().mockResolvedValue([]),
        getMyUsageByModel: vi.fn().mockResolvedValue([]),
      }
    })
    vi.doMock('../components/settings/ConnectionSettings', () => ({
      ConnectionSettings: () => <div>connection-settings</div>,
    }))
    vi.doMock('../components/settings/ModulesSettings', () => ({
      ModulesSettings: () => <div>modules-settings</div>,
    }))
    vi.doMock('../components/settings/ExtensionsSettings', () => ({
      ExtensionsSettings: () => <div>extensions-settings</div>,
    }))
    vi.doMock('../components/settings/UpdateSettings', () => ({
      UpdateSettingsContent: () => <div>update-settings</div>,
    }))
    vi.doMock('../contexts/AppearanceContext', () => ({
      useAppearance: () => ({
        customThemeId: null,
        customThemes: {},
        saveCustomTheme: vi.fn(),
        setActiveCustomTheme: vi.fn(),
      }),
    }))
    vi.doMock('@arkloop/shared', async () => {
      const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
      return {
        ...actual,
        useToast: () => ({ addToast }),
      }
    })
    vi.doMock('@arkloop/shared/desktop', () => ({
      getDesktopApi: () => desktopApi,
    }))

    const { AdvancedSettings } = await import('../components/settings/AdvancedSettings')
    const { LocaleProvider } = await import('../contexts/LocaleContext')

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <AdvancedSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('使用量')
    expect(container.textContent).toContain('语音输入')

    const navLabels = Array.from(container.querySelectorAll('button'))
      .map((button) => button.textContent?.trim() ?? '')
      .filter((label) => ['使用量', '语音输入', '网络'].includes(label))
    expect(navLabels).toEqual(['使用量', '语音输入', '网络'])

    const logsButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('日志'))
    expect(logsButton).toBeTruthy()

    await act(async () => {
      logsButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(container.textContent).toContain('desktop main start')
  }, 15000)

  it('导出弹窗只展示批准的 recovery sections', async () => {
    const exportDataBundle = vi.fn().mockResolvedValue({ ok: true, filePath: '/tmp/export/bundle' })
    const desktopApi = {
      advanced: {
        getOverview: vi.fn().mockResolvedValue({
          appName: 'Arkloop',
          appVersion: '1.2.3',
          githubUrl: 'https://github.com/qqqqqf-q/Arkloop',
          telegramUrl: null,
          iconDataUrl: null,
          configPath: '/tmp/config.json',
          dataDir: '/tmp/data',
          logsDir: '/tmp/logs',
          sqlitePath: '/tmp/data.db',
          links: [{ label: 'GitHub', url: 'https://github.com/qqqqqf-q/Arkloop' }],
          status: [{ label: 'Sidecar', value: 'running', tone: 'success' }],
          usage: null,
        }),
        chooseDataFolder: vi.fn().mockResolvedValue('/tmp/export'),
        exportDataBundle,
        importDataBundle: vi.fn().mockResolvedValue({ ok: true, importedFrom: '/tmp/import' }),
        listLogs: vi.fn().mockResolvedValue({ entries: [] }),
      },
      config: {
        get: vi.fn().mockResolvedValue({
          network: {
            proxyEnabled: false,
            requestTimeoutMs: 30000,
            retryCount: 1,
          },
        }),
        set: vi.fn().mockResolvedValue({ ok: true }),
      },
    }

    vi.doMock('../storage', async () => {
      const actual = await vi.importActual<typeof import('../storage')>('../storage')
      return {
        ...actual,
        readLocaleFromStorage: vi.fn(() => 'zh'),
        writeLocaleToStorage: vi.fn(),
      }
    })
    vi.doMock('../api', async () => {
      const actual = await vi.importActual<typeof import('../api')>('../api')
      return {
        ...actual,
        getMyUsage: vi.fn().mockResolvedValue(null),
        getMyDailyUsage: vi.fn().mockResolvedValue([]),
        getMyHourlyUsage: vi.fn().mockResolvedValue([]),
        getMyUsageByModel: vi.fn().mockResolvedValue([]),
      }
    })
    vi.doMock('../components/settings/ConnectionSettings', () => ({
      ConnectionSettings: () => <div>connection-settings</div>,
    }))
    vi.doMock('../components/settings/ModulesSettings', () => ({
      ModulesSettings: () => <div>modules-settings</div>,
    }))
    vi.doMock('../components/settings/ExtensionsSettings', () => ({
      ExtensionsSettings: () => <div>extensions-settings</div>,
    }))
    vi.doMock('../components/settings/UpdateSettings', () => ({
      UpdateSettingsContent: () => <div>update-settings</div>,
    }))
    vi.doMock('../contexts/AppearanceContext', () => ({
      useAppearance: () => ({
        customThemeId: null,
        customThemes: {},
        saveCustomTheme: vi.fn(),
        setActiveCustomTheme: vi.fn(),
      }),
    }))
    vi.doMock('@arkloop/shared', async () => {
      const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
      return {
        ...actual,
        useToast: () => ({ addToast }),
      }
    })
    vi.doMock('@arkloop/shared/desktop', () => ({
      getDesktopApi: () => desktopApi,
    }))

    const { AdvancedSettings } = await import('../components/settings/AdvancedSettings')
    const { LocaleProvider } = await import('../contexts/LocaleContext')

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <AdvancedSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const dataButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('数据'))
    expect(dataButton).toBeTruthy()

    await act(async () => {
      dataButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    const exportButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('导出数据'))
    expect(exportButton).toBeTruthy()

    await act(async () => {
      exportButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    const optionLabels = Array.from(document.body.querySelectorAll('label')).map((label) => label.textContent?.trim() ?? '')
    expect(optionLabels).toEqual([
      '设置',
      'AI 提供商',
      '聊天历史',
      '提示词应用',
      '项目',
      'MCP 服务器',
      '自定义主题',
    ])
    expect(exportDataBundle).not.toHaveBeenCalled()
  })
})
