import { Suspense, act } from 'react'
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

function setInputValue(input: HTMLInputElement, value: string) {
  const descriptor = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')
  descriptor?.set?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

async function loadDesktopSettingsSubject() {
  vi.resetModules()
  vi.doMock('../api-admin', () => ({
    listPlatformSettings: vi.fn().mockResolvedValue([]),
  }))
  vi.doMock('../api-bridge', () => ({
    bridgeClient: {
      getExecutionMode: vi.fn().mockResolvedValue('local'),
    },
  }))
  vi.doMock('@arkloop/shared/desktop', () => ({
    getDesktopApi: () => ({
      config: {
        get: vi.fn().mockResolvedValue({
          mode: 'local',
          saas: { baseUrl: '' },
          selfHosted: { baseUrl: '' },
          local: { port: 19001, portMode: 'auto' },
          window: { width: 1280, height: 800 },
          onboarding_completed: true,
          connectors: {
            fetch: { provider: 'basic' },
            search: { provider: 'duckduckgo' },
          },
          memory: { enabled: true, provider: 'notebook' },
          network: { proxyEnabled: false, requestTimeoutMs: 30000, retryCount: 1 },
        }),
        onChanged: vi.fn(() => () => {}),
      },
    }),
  }))
  vi.doMock('../storage', async () => {
    const actual = await vi.importActual<typeof import('../storage')>('../storage')
    return {
      ...actual,
      readLocaleFromStorage: vi.fn(() => 'zh'),
      writeLocaleToStorage: vi.fn(),
    }
  })
  vi.doMock('../components/settings', () => ({
    GeneralSettings: () => <div>general</div>,
    DesktopAppearanceSettings: () => <div>appearance</div>,
    ProvidersSettings: () => <div>providers</div>,
    RoutingSettings: () => <div>routing</div>,
    PersonasSettings: () => <div>personas</div>,
    DesktopChannelsSettings: () => <div>integrations</div>,
    SkillsSettings: () => <div>skills</div>,
    MCPSettings: () => <div>mcp</div>,
    AdvancedSettings: () => <div>advanced</div>,
    SearchFetchSettings: () => <div>search fetch</div>,
    MemorySettings: () => <div>memory</div>,
    ConnectionSettings: () => <div>connection</div>,
    ChatSettings: () => <div>chat</div>,
    ExtensionsSettings: () => <div>extensions</div>,
    ModulesSettings: () => <div>modules</div>,
    DeveloperSettings: () => <div>developer</div>,
    DesktopPromptInjectionSettings: () => <div>prompt injection</div>,
    ConnectorsSettings: () => <div>connectors</div>,
    VoiceSettings: () => <div>voice</div>,
  }))
  vi.doMock('../components/settings/GeneralSettings', () => ({ GeneralSettings: () => <div>general</div> }))
  vi.doMock('../components/settings/DesktopAppearanceSettings', () => ({ DesktopAppearanceSettings: () => <div>appearance</div> }))
  vi.doMock('../components/settings/ProvidersSettings', () => ({ ProvidersSettings: () => <div>providers</div> }))
  vi.doMock('../components/settings/RoutingSettings', () => ({ RoutingSettings: () => <div>routing</div> }))
  vi.doMock('../components/settings/PersonasSettings', () => ({ PersonasSettings: () => <div>personas</div> }))
  vi.doMock('../components/settings/DesktopChannelsSettings', () => ({ DesktopChannelsSettings: () => <div>integrations</div> }))
  vi.doMock('../components/settings/SkillsSettings', () => ({ SkillsSettings: () => <div>skills</div> }))
  vi.doMock('../components/settings/MCPSettings', () => ({ MCPSettings: () => <div>mcp</div> }))
  vi.doMock('../components/settings/AdvancedSettings', () => ({ AdvancedSettings: () => <div>advanced</div> }))
  vi.doMock('../components/settings/MemorySettings', () => ({ MemorySettings: () => <div>memory</div> }))
  vi.doMock('../components/settings/ConnectionSettings', () => ({ ConnectionSettings: () => <div>connection</div> }))
  vi.doMock('../components/settings/ChatSettings', () => ({ ChatSettings: () => <div>chat</div> }))
  vi.doMock('../components/settings/ExtensionsSettings', () => ({ ExtensionsSettings: () => <div>extensions</div> }))
  vi.doMock('../components/settings/ModulesSettings', () => ({ ModulesSettings: () => <div>modules</div> }))
  vi.doMock('../components/settings/DeveloperSettings', () => ({ DeveloperSettings: () => <div>developer</div> }))
  vi.doMock('../components/settings/DesktopPromptInjectionSettings', () => ({ DesktopPromptInjectionSettings: () => <div>prompt injection</div> }))
  vi.doMock('../components/settings/VoiceSettings', () => ({ VoiceSettings: () => <div>voice</div> }))
  vi.doMock('../components/settings/DesignTokensSettings', () => ({ DesignTokensSettings: () => <div>design tokens</div> }))

  const { DesktopSettings } = await import('../components/DesktopSettings')
  const { LocaleProvider } = await import('../contexts/LocaleContext')
  return { DesktopSettings, LocaleProvider }
}

async function loadChannelsSubject() {
  vi.resetModules()
  vi.doMock('../api', async () => {
    const actual = await vi.importActual<typeof import('../api')>('../api')
    return {
      ...actual,
      listChannels: vi.fn(),
      listMyChannelIdentities: vi.fn(),
      listChannelPersonas: vi.fn(),
      listLlmProviders: vi.fn(),
      createChannel: vi.fn(),
      updateChannel: vi.fn(),
      verifyChannel: vi.fn(),
      createChannelBindCode: vi.fn(),
      unbindChannelIdentity: vi.fn(),
      isApiError: vi.fn(() => false),
    }
  })
  vi.doMock('../storage', async () => {
    const actual = await vi.importActual<typeof import('../storage')>('../storage')
    return {
      ...actual,
      readLocaleFromStorage: vi.fn(() => 'zh'),
      writeLocaleToStorage: vi.fn(),
    }
  })

  const api = await import('../api')
  const { DesktopChannelsSettings } = await import('../components/settings/DesktopChannelsSettings')
  const { LocaleProvider } = await import('../contexts/LocaleContext')
  return { api, DesktopChannelsSettings, LocaleProvider }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  if (root) {
    act(() => root!.unmount())
  }
  container.remove()
  root = null
  vi.doUnmock('../api')
  vi.doUnmock('../storage')
  vi.doUnmock('../api-admin')
  vi.doUnmock('../api-bridge')
  vi.doUnmock('@arkloop/shared/desktop')
  vi.doUnmock('../components/settings')
  vi.doUnmock('../components/settings/GeneralSettings')
  vi.doUnmock('../components/settings/DesktopAppearanceSettings')
  vi.doUnmock('../components/settings/ProvidersSettings')
  vi.doUnmock('../components/settings/RoutingSettings')
  vi.doUnmock('../components/settings/PersonasSettings')
  vi.doUnmock('../components/settings/DesktopChannelsSettings')
  vi.doUnmock('../components/settings/SkillsSettings')
  vi.doUnmock('../components/settings/MCPSettings')
  vi.doUnmock('../components/settings/AdvancedSettings')
  vi.doUnmock('../components/settings/MemorySettings')
  vi.doUnmock('../components/settings/ConnectionSettings')
  vi.doUnmock('../components/settings/ChatSettings')
  vi.doUnmock('../components/settings/ExtensionsSettings')
  vi.doUnmock('../components/settings/ModulesSettings')
  vi.doUnmock('../components/settings/DeveloperSettings')
  vi.doUnmock('../components/settings/DesktopPromptInjectionSettings')
  vi.doUnmock('../components/settings/VoiceSettings')
  vi.doUnmock('../components/settings/DesignTokensSettings')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('DesktopSettings', () => {
  it('侧边栏将 channels 文案显示为第三方接入并包含高级入口', async () => {
    const { DesktopSettings, LocaleProvider } = await loadDesktopSettingsSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <Suspense fallback={<div>loading</div>}>
            <DesktopSettings
              me={null}
              accessToken="token"
              initialSection="channels"
              onClose={() => {}}
              onLogout={() => {}}
            />
          </Suspense>
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('接入')
    expect(container.textContent).toContain('高级')
  })

  it('侧边栏恢复记忆入口并能展示记忆页内容', async () => {
    const { DesktopSettings, LocaleProvider } = await loadDesktopSettingsSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <Suspense fallback={<div>loading</div>}>
            <DesktopSettings
              me={null}
              accessToken="token"
              initialSection="memory"
              onClose={() => {}}
              onLogout={() => {}}
            />
          </Suspense>
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('Memory')
    expect(container.textContent).toContain('memory')
  })
})

describe('DesktopChannelsSettings', () => {
  it('父页支持 Telegram / Discord 切换并显示 Discord 访问控制字段', async () => {
    const { api, DesktopChannelsSettings, LocaleProvider } = await loadChannelsSubject()
    vi.mocked(api.listChannels).mockResolvedValue([
      {
        id: 'tg-1',
        account_id: 'acc-1',
        channel_type: 'telegram',
        persona_id: 'persona-1',
        webhook_url: null,
        is_active: true,
        config_json: { allowed_user_ids: ['10001'], default_model: 'provider^model-a' },
        has_credentials: true,
        created_at: '2026-03-26T00:00:00Z',
        updated_at: '2026-03-26T00:00:00Z',
      },
      {
        id: 'dc-1',
        account_id: 'acc-1',
        channel_type: 'discord',
        persona_id: 'persona-1',
        webhook_url: null,
        is_active: false,
        config_json: { allowed_server_ids: ['20001'], allowed_channel_ids: ['30001'] },
        has_credentials: true,
        created_at: '2026-03-26T00:00:00Z',
        updated_at: '2026-03-26T00:00:00Z',
      },
    ])
    vi.mocked(api.listMyChannelIdentities).mockResolvedValue([])
    vi.mocked(api.listChannelPersonas).mockResolvedValue([
      {
        id: 'persona-1',
        persona_key: 'normal',
        version: '1',
        display_name: 'Normal',
        source: 'project',
      } as never,
    ])
    vi.mocked(api.listLlmProviders).mockResolvedValue([])

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopChannelsSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('Telegram')
    expect(container.textContent).toContain('Discord')
    expect(container.textContent).toContain('私聊访问控制')

    const discordTab = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('Discord'))
    expect(discordTab).toBeTruthy()

    await act(async () => {
      discordTab!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.textContent).toContain('访问控制')
    expect(container.textContent).toContain('允许的 Server ID')
    expect(container.textContent).toContain('允许的 Channel ID')
  })

  it('可以创建 Discord 配置并生成 Discord 绑定码', async () => {
    const { api, DesktopChannelsSettings, LocaleProvider } = await loadChannelsSubject()
    vi.mocked(api.listChannels).mockResolvedValue([])
    vi.mocked(api.listMyChannelIdentities).mockResolvedValue([])
    vi.mocked(api.listChannelPersonas).mockResolvedValue([
      {
        id: 'persona-1',
        persona_key: 'normal',
        version: '1',
        display_name: 'Normal',
        source: 'project',
      } as never,
    ])
    vi.mocked(api.listLlmProviders).mockResolvedValue([])
    vi.mocked(api.createChannel).mockResolvedValue({
      id: 'dc-created',
      account_id: 'acc-1',
      channel_type: 'discord',
      persona_id: 'persona-1',
      webhook_url: null,
      is_active: false,
      config_json: {},
      has_credentials: true,
      created_at: '2026-03-26T00:00:00Z',
      updated_at: '2026-03-26T00:00:00Z',
    })
    vi.mocked(api.createChannelBindCode).mockResolvedValue({
      id: 'bind-1',
      token: 'ABC123',
      channel_type: 'discord',
      expires_at: '2026-03-27T00:00:00Z',
      created_at: '2026-03-26T00:00:00Z',
    })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopChannelsSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const discordTab = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('Discord'))
    await act(async () => {
      discordTab!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const inputs = Array.from(container.querySelectorAll('input'))
    const tokenInput = inputs.find((input) => input.getAttribute('placeholder')?.includes('Bot Token')) as HTMLInputElement
    const serverInput = inputs.find((input) => input.getAttribute('placeholder')?.includes('Server ID')) as HTMLInputElement
    const channelInput = inputs.find((input) => input.getAttribute('placeholder')?.includes('Channel ID')) as HTMLInputElement

    await act(async () => {
      setInputValue(tokenInput, 'discord-token')
      setInputValue(serverInput, '20001')
      setInputValue(channelInput, '30001')
    })
    await flushEffects()

    const addButtons = Array.from(container.querySelectorAll('button')).filter((button) => button.textContent?.includes('添加'))
    await act(async () => {
      addButtons[0]!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      addButtons[1]!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const saveButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '保存')
    await act(async () => {
      saveButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(api.createChannel).toHaveBeenCalledWith('token', {
      channel_type: 'discord',
      bot_token: 'discord-token',
      persona_id: 'persona-1',
      config_json: {
        allowed_server_ids: ['20001'],
        allowed_channel_ids: ['30001'],
      },
    })

    const bindButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('生成绑定码'))
    await act(async () => {
      bindButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(api.createChannelBindCode).toHaveBeenCalledWith('token', 'discord')
    expect(container.textContent).toContain('/bind ABC123')
  })

  it('Discord verify 成功时同时显示 app 和 bot 信息', async () => {
    const { api, DesktopChannelsSettings, LocaleProvider } = await loadChannelsSubject()
    vi.mocked(api.listChannels).mockResolvedValue([
      {
        id: 'dc-1',
        account_id: 'acc-1',
        channel_type: 'discord',
        persona_id: 'persona-1',
        webhook_url: null,
        is_active: true,
        config_json: {},
        has_credentials: true,
        created_at: '2026-03-26T00:00:00Z',
        updated_at: '2026-03-26T00:00:00Z',
      },
    ])
    vi.mocked(api.listMyChannelIdentities).mockResolvedValue([])
    vi.mocked(api.listChannelPersonas).mockResolvedValue([
      {
        id: 'persona-1',
        persona_key: 'normal',
        version: '1',
        display_name: 'Normal',
        source: 'project',
      } as never,
    ])
    vi.mocked(api.listLlmProviders).mockResolvedValue([])
    vi.mocked(api.verifyChannel).mockResolvedValue({
      ok: true,
      application_name: 'Arkloop DM',
      application_id: 'app-123',
      bot_username: 'arkloop_bot',
    })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopChannelsSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const discordTab = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('Discord'))
    await act(async () => {
      discordTab!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const verifyButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('验证连接'))
    expect(verifyButton).toBeTruthy()

    await act(async () => {
      verifyButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(api.verifyChannel).toHaveBeenCalledWith('token', 'dc-1')
    expect(container.textContent).toContain('Arkloop DM')
    expect(container.textContent).toContain('@arkloop_bot')
    expect(container.textContent).toContain('app-123')
  })
})
