import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

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
          githubUrl: 'https://github.com/qqqqqf/Arkloop',
          telegramUrl: null,
          iconPath: null,
          configPath: '/tmp/config.json',
          dataDir: '/tmp/data',
          logsDir: '/tmp/logs',
          sqlitePath: '/tmp/data.db',
          links: [{ label: 'GitHub', url: 'https://github.com/qqqqqf/Arkloop' }],
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

    expect(container.textContent).toContain('高级')
    expect(container.textContent).toContain('Arkloop')
    expect(container.textContent).toContain('概览')

    const logsButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('日志'))
    expect(logsButton).toBeTruthy()

    await act(async () => {
      logsButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(container.textContent).toContain('desktop main start')
  }, 15000)
})
