import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppUIProvider, useSidebarUI } from '../contexts/app-ui'
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

describe('AppUIProvider sidebar state', () => {
  const authValue: AuthContextValue = {
    me: null,
    meLoaded: true,
    accessToken: 'token',
    logout: vi.fn(),
    updateMe: vi.fn(),
  }

  const originalInnerWidth = window.innerWidth
  const originalActEnvironment = (globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }).IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
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
    expect(titleBar?.style.paddingLeft).toBe('8px')
    expect(container.querySelector('button[title="Minimize"]')).toBeNull()
  })

  it('Linux 桌面不渲染 Windows 自绘控制', async () => {
    desktopMock.platform.mockReturnValue('linux')

    await renderTitleBar(appUpdateState('available'), true)

    const titleBar = container.firstElementChild as HTMLElement | null
    expect(titleBar?.style.paddingLeft).toBe('8px')
    expect(container.querySelector('button[title="Minimize"]')).toBeNull()
  })

  it('只为桌面应用 available/downloaded 状态显示标题栏更新入口', async () => {
    await renderTitleBar(appUpdateState('idle'), false)
    expect(container.querySelector('button[title="发现新版本"]')).toBeNull()
    expect(container.querySelector('button[title="已可安装"]')).toBeNull()

    await renderTitleBar(appUpdateState('available'), true)
    expect(container.querySelector('button[title="发现新版本"]')).not.toBeNull()

    await renderTitleBar(appUpdateState('downloaded'), true)
    expect(container.querySelector('button[title="已可安装"]')).not.toBeNull()
  })
})
