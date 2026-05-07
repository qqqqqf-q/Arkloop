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
    updateMe: vi.fn(),
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  getDesktopMode: () => 'desktop',
  getDesktopAppVersion: () => '0.0.0-test',
  isDesktop: () => true,
  isLocalMode: () => true,
  getDesktopApi: () => ({
    app: {
      getOsUsername: vi.fn().mockResolvedValue('alice'),
    },
    config: null,
  }),
}))

vi.mock('@arkloop/shared', async () => {
  const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
  return {
    ...actual,
    useToast: () => ({ addToast: vi.fn() }),
  }
})

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

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readLocaleFromStorage: vi.fn(() => 'zh'),
    writeLocaleToStorage: vi.fn(),
  }
})

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

function setInputValue(input: HTMLInputElement, value: string) {
  const descriptor = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')
  descriptor?.set?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
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

  it('渲染通用页基础偏好与工具模型', async () => {
    const { api, LocaleProvider, GeneralSettings } = await loadSubject()
    vi.mocked(api.listLlmProviders).mockResolvedValue([])
    vi.mocked(api.listSpawnProfiles).mockResolvedValue([])

    await act(async () => {
      root.render(
        <LocaleProvider>
          <GeneralSettings accessToken="token" me={null} onLogout={() => {}} />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('通用')
    expect(container.textContent).toContain('语言与区域')
    expect(container.textContent).toContain('支持')
    expect(container.querySelector('[data-testid="language-content"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="timezone-settings"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="theme-mode-picker"]')).toBeNull()
    expect(container.querySelector('[data-testid="tool-model-dropdown"]')).not.toBeNull()
    expect(api.listLlmProviders).toHaveBeenCalledWith('token')
    expect(api.listSpawnProfiles).toHaveBeenCalledWith('token')
  })

  it('本地模式用户卡片内联编辑后端用户名', async () => {
    const { api, LocaleProvider, GeneralSettings } = await loadSubject()
    vi.mocked(api.listLlmProviders).mockResolvedValue([])
    vi.mocked(api.listSpawnProfiles).mockResolvedValue([])
    vi.mocked(api.updateMe).mockResolvedValue({ username: 'renamed-user', timezone: 'Asia/Singapore' })
    const onMeUpdated = vi.fn()

    await act(async () => {
      root.render(
        <LocaleProvider>
          <GeneralSettings
            accessToken="token"
            me={{
              id: 'user-1',
              username: 'desktop-user',
              email_verified: true,
              email_verification_required: false,
              work_enabled: true,
              timezone: 'Asia/Singapore',
              account_timezone: null,
            }}
            onLogout={() => {}}
            onMeUpdated={onMeUpdated}
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('desktop-user')
    expect(container.textContent).not.toContain('alice')

    const editButton = container.querySelector('button[aria-label="编辑"]') as HTMLButtonElement | null
    expect(editButton).not.toBeNull()
    expect(editButton?.className).toContain('group-hover:opacity-100')

    await act(async () => {
      editButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const input = container.querySelector('input[type="text"]') as HTMLInputElement | null
    expect(input?.value).toBe('desktop-user')

    await act(async () => {
      setInputValue(input!, 'renamed-user')
    })

    const saveButton = container.querySelector('button[aria-label="保存"]') as HTMLButtonElement | null
    expect(saveButton).not.toBeNull()

    await act(async () => {
      saveButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(api.updateMe).toHaveBeenCalledWith('token', { username: 'renamed-user' })
    expect(onMeUpdated).toHaveBeenCalledWith(expect.objectContaining({ username: 'renamed-user' }))
    expect(container.querySelector('input[type="text"]')).toBeNull()
  })
})
