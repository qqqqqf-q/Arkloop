import { act, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppUIProvider, useSettingsUI, useSidebarUI, useTitleBarRightPanelUI } from '../contexts/app-ui'
import { AuthContextBridge, type AuthContextValue } from '../contexts/auth'
import { DesktopTitleBar } from '../components/DesktopTitleBar'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { AppUpdaterState } from '@arkloop/shared/desktop'

const desktopMock = vi.hoisted(() => ({
  isDesktop: vi.fn(() => true),
  platform: vi.fn(() => 'darwin' as string | null),
}))

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: desktopMock.isDesktop,
  getDesktopPlatform: desktopMock.platform,
  getDesktopApi: () => ({}),
}))

function SidebarProbe() {
  const { sidebarCollapsed, toggleSidebar } = useSidebarUI()

  return (
    <div>
      <button type="button" onClick={() => toggleSidebar('sidebar')}>
        toggle
      </button>
      <span data-testid="collapsed">{sidebarCollapsed ? 'collapsed' : 'expanded'}</span>
    </div>
  )
}

function SettingsProbe() {
  const { settingsOpen, settingsInitialTab } = useSettingsUI()

  return (
    <div>
      <span data-testid="settings-open">{settingsOpen ? 'open' : 'closed'}</span>
      <span data-testid="settings-tab">{settingsInitialTab}</span>
    </div>
  )
}

function RightPanelShortcutProbe() {
  const { rightPanelOpen, setRightPanelOpen } = useSidebarUI()
  const { setTitleBarRightPanelClick } = useTitleBarRightPanelUI()
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    setTitleBarRightPanelClick(() => {
      setVisible((open) => !open)
    })
    return () => setTitleBarRightPanelClick(null)
  }, [setTitleBarRightPanelClick])

  useEffect(() => {
    setRightPanelOpen(visible)
  }, [visible, setRightPanelOpen])

  return (
    <div>
      <span data-testid="right-panel-visible">{visible ? 'open' : 'closed'}</span>
      <span data-testid="right-panel-icon">{rightPanelOpen ? 'open' : 'closed'}</span>
    </div>
  )
}

describe('AppUIProvider sidebar state', () => {
  const authValue: AuthContextValue = {
    me: null,
    meLoaded: true,
    accessToken: 'token',
    logout: vi.fn(),
    updateMe: vi.fn(),
  }

  const originalInnerWidth = window.innerWidth
  const originalNavigatorPlatform = Object.getOwnPropertyDescriptor(window.navigator, 'platform')
  const originalActEnvironment = (globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }).IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    desktopMock.isDesktop.mockReturnValue(true)
    vi.useFakeTimers()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(0), 0))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: 1400,
    })
    ;(globalThis as typeof globalThis & {
      IS_REACT_ACT_ENVIRONMENT?: boolean
    }).IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: originalInnerWidth,
    })
    if (originalActEnvironment === undefined) {
      delete (globalThis as typeof globalThis & {
        IS_REACT_ACT_ENVIRONMENT?: boolean
      }).IS_REACT_ACT_ENVIRONMENT
    } else {
      ;(globalThis as typeof globalThis & {
        IS_REACT_ACT_ENVIRONMENT?: boolean
      }).IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
    if (originalNavigatorPlatform) {
      Object.defineProperty(window.navigator, 'platform', originalNavigatorPlatform)
    } else {
      Reflect.deleteProperty(window.navigator, 'platform')
    }
  })

  it('保留手动折叠状态，即使跨过宽度断点后再回来', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <AuthContextBridge value={authValue}>
            <AppUIProvider>
              <SidebarProbe />
            </AppUIProvider>
          </AuthContextBridge>
        </MemoryRouter>,
      )
    })

    const toggleButton = container.querySelector('button')
    const collapsedState = container.querySelector('[data-testid="collapsed"]')
    expect(toggleButton).not.toBeNull()
    expect(collapsedState?.textContent).toBe('expanded')

    await act(async () => {
      toggleButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(collapsedState?.textContent).toBe('collapsed')

    await act(async () => {
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        writable: true,
        value: 1300,
      })
      window.dispatchEvent(new Event('resize'))
      vi.runAllTimers()
    })

    expect(collapsedState?.textContent).toBe('collapsed')

    await act(async () => {
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        writable: true,
        value: 1100,
      })
      window.dispatchEvent(new Event('resize'))
      vi.runAllTimers()
    })

    expect(collapsedState?.textContent).toBe('collapsed')

    await act(async () => {
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        writable: true,
        value: 1400,
      })
      window.dispatchEvent(new Event('resize'))
      vi.runAllTimers()
    })

    expect(collapsedState?.textContent).toBe('collapsed')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('全局设置快捷键打开 settings 页签并可再次关闭', async () => {
    desktopMock.isDesktop.mockReturnValue(false)
    Object.defineProperty(window.navigator, 'platform', {
      configurable: true,
      value: 'Win32',
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <AuthContextBridge value={authValue}>
            <AppUIProvider>
              <SettingsProbe />
            </AppUIProvider>
          </AuthContextBridge>
        </MemoryRouter>,
      )
    })

    expect(container.querySelector('[data-testid="settings-open"]')?.textContent).toBe('closed')
    expect(container.querySelector('[data-testid="settings-tab"]')?.textContent).toBe('account')

    let event = new KeyboardEvent('keydown', {
      key: ',',
      code: 'Comma',
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    })
    await act(async () => {
      window.dispatchEvent(event)
    })

    expect(event.defaultPrevented).toBe(true)
    expect(container.querySelector('[data-testid="settings-open"]')?.textContent).toBe('open')
    expect(container.querySelector('[data-testid="settings-tab"]')?.textContent).toBe('settings')

    event = new KeyboardEvent('keydown', {
      key: ',',
      code: 'Comma',
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    })
    await act(async () => {
      window.dispatchEvent(event)
    })

    expect(container.querySelector('[data-testid="settings-open"]')?.textContent).toBe('closed')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('右侧面板快捷键复用标题栏回调路径', async () => {
    desktopMock.isDesktop.mockReturnValue(false)
    Object.defineProperty(window.navigator, 'platform', {
      configurable: true,
      value: 'Win32',
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <AuthContextBridge value={authValue}>
            <AppUIProvider>
              <RightPanelShortcutProbe />
            </AppUIProvider>
          </AuthContextBridge>
        </MemoryRouter>,
      )
    })

    expect(container.querySelector('[data-testid="right-panel-visible"]')?.textContent).toBe('closed')
    expect(container.querySelector('[data-testid="right-panel-icon"]')?.textContent).toBe('closed')

    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'b',
        code: 'KeyB',
        altKey: true,
        ctrlKey: true,
        bubbles: true,
        cancelable: true,
      }))
    })

    expect(container.querySelector('[data-testid="right-panel-visible"]')?.textContent).toBe('open')
    expect(container.querySelector('[data-testid="right-panel-icon"]')?.textContent).toBe('open')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})

describe('custom background surface CSS', () => {
  it('keeps chat gradient transparent outside color-mix support', () => {
    const css = readFileSync('src/index.css', 'utf-8')

    expect(css).toContain(':root[data-background-image="custom"]')
    expect(css).toContain('--c-chat-bg-gradient-stop: transparent;')
    expect(css).toContain('.theme-chat-surface')
    expect(css).toContain('background-color: color-mix(in srgb, var(--c-bg-page) 42%, transparent);')
  })
})

describe('DesktopTitleBar update entry', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot> | null

  const actEnvironment = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalNavigatorPlatform = Object.getOwnPropertyDescriptor(window.navigator, 'platform')

  const appUpdateState = (phase: AppUpdaterState['phase']): AppUpdaterState => ({
    supported: true,
    phase,
    currentVersion: '1.0.0',
    latestVersion: phase === 'available' || phase === 'downloaded' ? '1.0.1' : null,
    progressPercent: phase === 'downloaded' ? 100 : 0,
    error: null,
  })

  beforeEach(() => {
    desktopMock.isDesktop.mockReturnValue(true)
    desktopMock.platform.mockReturnValue('darwin')
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
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
    if (originalNavigatorPlatform) {
      Object.defineProperty(window.navigator, 'platform', originalNavigatorPlatform)
    } else {
      Reflect.deleteProperty(window.navigator, 'platform')
    }
  })

  async function renderTitleBar(state: AppUpdaterState, hasAppUpdate: boolean) {
    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopTitleBar
            sidebarCollapsed={false}
            onToggleSidebar={() => {}}
            appMode="chat"
            onSetAppMode={() => {}}
            availableModes={['chat', 'work']}
            showIncognitoToggle={false}
            hasAppUpdate={hasAppUpdate}
            appUpdateState={state}
            onCheckAppUpdate={() => {}}
            onDownloadApp={() => {}}
            onInstallApp={() => {}}
          />
        </LocaleProvider>,
      )
    })
  }

  it('普通浏览器模式不渲染标题栏', async () => {
    desktopMock.isDesktop.mockReturnValue(false)

    await renderTitleBar(appUpdateState('available'), true)

    expect(container.firstElementChild).toBeNull()
  })

  it('平台缺失时不使用浏览器平台推断 macOS 标题栏留白', async () => {
    desktopMock.platform.mockReturnValue(null)
    Object.defineProperty(window.navigator, 'platform', {
      configurable: true,
      value: 'MacIntel',
    })

    await renderTitleBar(appUpdateState('available'), true)

    const titleBar = container.firstElementChild as HTMLElement | null
    expect(titleBar?.style.paddingLeft).toBe('12px')
    expect(container.querySelector('button[title="Minimize"]')).toBeNull()
  })

  it('Linux 桌面不渲染 Windows 自绘控制', async () => {
    desktopMock.platform.mockReturnValue('linux')

    await renderTitleBar(appUpdateState('available'), true)

    const titleBar = container.firstElementChild as HTMLElement | null
    expect(titleBar?.style.paddingLeft).toBe('12px')
    expect(container.querySelector('button[title="Minimize"]')).toBeNull()
  })

  it('只为桌面应用 available/downloaded 状态显示标题栏更新入口', async () => {
    await renderTitleBar(appUpdateState('idle'), false)
    expect(container.querySelector('button[title="发现新版本"], button[title="Update available"]')).toBeNull()
    expect(container.querySelector('button[title="已可安装"], button[title="Ready to install"]')).toBeNull()

    await renderTitleBar(appUpdateState('available'), true)
    expect(container.querySelector('button[title="发现新版本"], button[title="Update available"]')).not.toBeNull()

    await renderTitleBar(appUpdateState('downloaded'), true)
    expect(container.querySelector('button[title="已可安装"], button[title="Ready to install"]')).not.toBeNull()
  })
})
