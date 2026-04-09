import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    listLlmProviders: vi.fn(),
    listSpawnProfiles: vi.fn(),
    setSpawnProfile: vi.fn(),
    deleteSpawnProfile: vi.fn(),
    resolveOpenVikingConfig: vi.fn(),
    testLlmProviderModel: vi.fn(),
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  getDesktopMode: () => 'desktop',
  isDesktop: () => true,
  isLocalMode: () => true,
  getDesktopApi: () => ({
    app: {
      getOsUsername: vi.fn().mockResolvedValue('alice'),
    },
    config: null,
  }),
}))

vi.mock('../api-bridge', () => ({
  bridgeClient: {
    getExecutionMode: vi.fn(),
    performAction: vi.fn(),
    streamOperation: vi.fn(),
  },
  checkBridgeAvailable: vi.fn().mockResolvedValue(false),
}))

vi.mock('../openExternal', () => ({
  openExternal: vi.fn(),
}))

vi.mock('../components/settings/AppearanceSettings', () => ({
  LanguageContent: () => <div data-testid="language-content" />,
  ThemeModePicker: () => <div data-testid="theme-mode-picker" />,
}))

vi.mock('../components/settings/TimeZoneSettings', () => ({
  TimeZoneSettings: () => <div data-testid="timezone-settings" />,
}))

vi.mock('../components/settings/SettingsModelDropdown', () => ({
  SettingsModelDropdown: ({
    value,
    placeholder,
    disabled,
  }: {
    value: string
    placeholder: string
    disabled: boolean
  }) => (
    <div
      data-testid="tool-model-dropdown"
      data-value={value}
      data-placeholder={placeholder}
      data-disabled={String(disabled)}
    />
  ),
}))

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve
  })
  return { promise, resolve }
}

describe('GeneralSettings', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    vi.resetModules()
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  async function loadSubject() {
    const api = await import('../api')
    const { LocaleProvider } = await import('../contexts/LocaleContext')
    const { GeneralSettings } = await import('../components/settings/GeneralSettings')
    return { api, LocaleProvider, GeneralSettings }
  }

  it('首次加载时显示加载态，而不是主对话占位', async () => {
    const { api, LocaleProvider, GeneralSettings } = await loadSubject()
    const providersRequest = deferred<Awaited<ReturnType<typeof api.listLlmProviders>>>()
    const profilesRequest = deferred<Awaited<ReturnType<typeof api.listSpawnProfiles>>>()
    vi.mocked(api.listLlmProviders).mockReturnValue(providersRequest.promise)
    vi.mocked(api.listSpawnProfiles).mockReturnValue(profilesRequest.promise)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <GeneralSettings accessToken="token" me={null} onLogout={() => {}} />
        </LocaleProvider>,
      )
    })

    const dropdown = container.querySelector('[data-testid="tool-model-dropdown"]')
    expect(dropdown?.getAttribute('data-placeholder')).toBe('加载中...')
    expect(dropdown?.getAttribute('data-disabled')).toBe('true')
  })

  it('重新打开时直接复用上次的工具模型值', async () => {
    const { api, LocaleProvider, GeneralSettings } = await loadSubject()
    vi.mocked(api.listLlmProviders).mockResolvedValue([
      {
        id: 'provider-1',
        scope: 'user',
        provider: 'openrouter',
        name: 'openrouter',
        key_prefix: null,
        base_url: 'https://openrouter.ai/api/v1',
        openai_api_mode: 'openai',
        created_at: '2026-04-09T00:00:00Z',
        models: [
          {
            id: 'model-1',
            provider_id: 'provider-1',
            model: 'google/gemma-4-26b-a4b-it',
            priority: 0,
            is_default: true,
            show_in_picker: true,
            tags: [],
            when: {},
            multiplier: 1,
          },
        ],
      },
    ])
    vi.mocked(api.listSpawnProfiles).mockResolvedValue([
      {
        profile: 'tool',
        resolved_model: 'openrouter^google/gemma-4-26b-a4b-it',
        has_override: true,
        auto_model: 'openrouter^google/gemma-4-26b-a4b-it',
      },
    ])

    await act(async () => {
      root.render(
        <LocaleProvider>
          <GeneralSettings accessToken="token" me={null} onLogout={() => {}} />
        </LocaleProvider>,
      )
    })

    await act(async () => {
      await Promise.resolve()
    })

    expect(
      container.querySelector('[data-testid="tool-model-dropdown"]')?.getAttribute('data-value'),
    ).toBe('openrouter^google/gemma-4-26b-a4b-it')

    const nextProvidersRequest = deferred<Awaited<ReturnType<typeof api.listLlmProviders>>>()
    const nextProfilesRequest = deferred<Awaited<ReturnType<typeof api.listSpawnProfiles>>>()
    vi.mocked(api.listLlmProviders).mockReturnValue(nextProvidersRequest.promise)
    vi.mocked(api.listSpawnProfiles).mockReturnValue(nextProfilesRequest.promise)

    await act(async () => {
      root.unmount()
    })

    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <GeneralSettings accessToken="token" me={null} onLogout={() => {}} />
        </LocaleProvider>,
      )
    })

    expect(
      container.querySelector('[data-testid="tool-model-dropdown"]')?.getAttribute('data-value'),
    ).toBe('openrouter^google/gemma-4-26b-a4b-it')
    expect(
      container.querySelector('[data-testid="tool-model-dropdown"]')?.getAttribute('data-disabled'),
    ).toBe('false')
  })
})
