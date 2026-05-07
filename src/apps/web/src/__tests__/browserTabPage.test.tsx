import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BrowserTabPage } from '../components/BrowserTabPage'
import { LocaleProvider } from '../contexts/LocaleContext'
import { useBrowserTabs } from '../contexts/browser-tabs'

const desktopMock = vi.hoisted(() => {
  const browserTabsApi = {
    show: vi.fn(() => Promise.resolve({ ok: true })),
    hide: vi.fn(() => Promise.resolve({ ok: true })),
    syncBounds: vi.fn(() => Promise.resolve({ ok: true })),
  }

  return {
    isDesktop: vi.fn(() => true),
    getDesktopApi: vi.fn(() => ({ browserTabs: browserTabsApi })),
    browserTabsApi,
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: desktopMock.isDesktop,
  getDesktopApi: desktopMock.getDesktopApi,
}))

vi.mock('../contexts/browser-tabs', () => ({
  useBrowserTabs: vi.fn(),
}))

describe('BrowserTabPage', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot> | null
  let originalLocalStorage: Storage | undefined
  const mockedUseBrowserTabs = vi.mocked(useBrowserTabs)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    originalLocalStorage = window.localStorage
    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: vi.fn(() => null),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn(),
        key: vi.fn(() => null),
        length: 0,
      } satisfies Storage,
      configurable: true,
    })
    window.localStorage.setItem('arkloop:web:locale', 'zh')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      disconnect() {}
    })
  })

  afterEach(() => {
    if (root) {
      act(() => root!.unmount())
    }
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    window.localStorage.removeItem('arkloop:web:locale')
    Object.defineProperty(window, 'localStorage', {
      value: originalLocalStorage,
      configurable: true,
    })
    container.remove()
    root = null
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('显示页面加载失败提示，并允许在当前页签重试', async () => {
    const navigateBrowserTab = vi.fn(() => Promise.resolve(null))

    mockedUseBrowserTabs.mockReturnValue({
      initialized: true,
      tabs: [{
        id: 'browser-a',
        title: 'Broken Page',
        url: 'https://bad.test',
        faviconUrl: null,
        loading: false,
        error: 'net::ERR_NAME_NOT_RESOLVED',
        canGoBack: false,
        canGoForward: false,
      }],
      panelOpen: true,
      activeBrowserTabId: 'browser-a',
      activeBrowserTab: {
        id: 'browser-a',
        title: 'Broken Page',
        url: 'https://bad.test',
        faviconUrl: null,
        loading: false,
        error: 'net::ERR_NAME_NOT_RESOLVED',
        canGoBack: false,
        canGoForward: false,
      },
      getDraftUrl: vi.fn(() => 'bad.test'),
      setDraftUrl: vi.fn(),
      openBrowserPanel: vi.fn(),
      closeBrowserPanel: vi.fn(),
      toggleBrowserPanel: vi.fn(),
      createBrowserTab: vi.fn(() => Promise.resolve(null)),
      activateBrowserTab: vi.fn(),
      closeBrowserTab: vi.fn(() => Promise.resolve()),
      navigateBrowserTab,
      reloadBrowserTab: vi.fn(() => Promise.resolve(null)),
      goBackBrowserTab: vi.fn(() => Promise.resolve(null)),
      goForwardBrowserTab: vi.fn(() => Promise.resolve(null)),
    })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <BrowserTabPage />
        </LocaleProvider>,
      )
      await Promise.resolve()
    })

    expect(container.textContent).toContain('Failed to load page')
    expect(container.textContent).toContain('net::ERR_NAME_NOT_RESOLVED')
    expect(container.textContent).toContain('Retry')
    expect(desktopMock.browserTabsApi.show).toHaveBeenCalledTimes(1)

    const retryButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('Retry'))
    expect(retryButton).not.toBeUndefined()

    await act(async () => {
      retryButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(navigateBrowserTab).toHaveBeenCalledWith('browser-a', 'bad.test')
  })

  it('右侧浏览器容器在没有页签时显示空状态提示', async () => {
    mockedUseBrowserTabs.mockReturnValue({
      initialized: true,
      tabs: [],
      panelOpen: true,
      activeBrowserTabId: null,
      activeBrowserTab: null,
      getDraftUrl: vi.fn(() => ''),
      setDraftUrl: vi.fn(),
      openBrowserPanel: vi.fn(),
      closeBrowserPanel: vi.fn(),
      toggleBrowserPanel: vi.fn(),
      createBrowserTab: vi.fn(() => Promise.resolve(null)),
      activateBrowserTab: vi.fn(),
      closeBrowserTab: vi.fn(() => Promise.resolve()),
      navigateBrowserTab: vi.fn(() => Promise.resolve(null)),
      reloadBrowserTab: vi.fn(() => Promise.resolve(null)),
      goBackBrowserTab: vi.fn(() => Promise.resolve(null)),
      goForwardBrowserTab: vi.fn(() => Promise.resolve(null)),
    })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <BrowserTabPage />
        </LocaleProvider>,
      )
      await Promise.resolve()
    })

    expect(container.textContent).toContain('Click the + in the top-left corner to create a browser tab.')
    expect(container.querySelector('button[title="New Browser Tab"]')).not.toBeNull()
  })

  it('有 favicon 时优先显示站点图标', async () => {
    mockedUseBrowserTabs.mockReturnValue({
      initialized: true,
      tabs: [{
        id: 'browser-a',
        title: 'Arkloop',
        url: 'https://arkloop.test',
        faviconUrl: 'https://arkloop.test/favicon.ico',
        loading: false,
        error: null,
        canGoBack: false,
        canGoForward: false,
      }],
      panelOpen: true,
      activeBrowserTabId: 'browser-a',
      activeBrowserTab: {
        id: 'browser-a',
        title: 'Arkloop',
        url: 'https://arkloop.test',
        faviconUrl: 'https://arkloop.test/favicon.ico',
        loading: false,
        error: null,
        canGoBack: false,
        canGoForward: false,
      },
      getDraftUrl: vi.fn(() => 'arkloop.test'),
      setDraftUrl: vi.fn(),
      openBrowserPanel: vi.fn(),
      closeBrowserPanel: vi.fn(),
      toggleBrowserPanel: vi.fn(),
      createBrowserTab: vi.fn(() => Promise.resolve(null)),
      activateBrowserTab: vi.fn(),
      closeBrowserTab: vi.fn(() => Promise.resolve()),
      navigateBrowserTab: vi.fn(() => Promise.resolve(null)),
      reloadBrowserTab: vi.fn(() => Promise.resolve(null)),
      goBackBrowserTab: vi.fn(() => Promise.resolve(null)),
      goForwardBrowserTab: vi.fn(() => Promise.resolve(null)),
    })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <BrowserTabPage />
        </LocaleProvider>,
      )
      await Promise.resolve()
    })

    expect(container.querySelector('img[src="https://arkloop.test/favicon.ico"]')).not.toBeNull()
  })

  it('没有显式 favicon 时按页面地址回退到 /favicon.ico', async () => {
    mockedUseBrowserTabs.mockReturnValue({
      initialized: true,
      tabs: [{
        id: 'browser-a',
        title: '163',
        url: 'https://www.163.com/',
        faviconUrl: null,
        loading: false,
        error: null,
        canGoBack: false,
        canGoForward: false,
      }],
      panelOpen: true,
      activeBrowserTabId: 'browser-a',
      activeBrowserTab: {
        id: 'browser-a',
        title: '163',
        url: 'https://www.163.com/',
        faviconUrl: null,
        loading: false,
        error: null,
        canGoBack: false,
        canGoForward: false,
      },
      getDraftUrl: vi.fn(() => 'www.163.com'),
      setDraftUrl: vi.fn(),
      openBrowserPanel: vi.fn(),
      closeBrowserPanel: vi.fn(),
      toggleBrowserPanel: vi.fn(),
      createBrowserTab: vi.fn(() => Promise.resolve(null)),
      activateBrowserTab: vi.fn(),
      closeBrowserTab: vi.fn(() => Promise.resolve()),
      navigateBrowserTab: vi.fn(() => Promise.resolve(null)),
      reloadBrowserTab: vi.fn(() => Promise.resolve(null)),
      goBackBrowserTab: vi.fn(() => Promise.resolve(null)),
      goForwardBrowserTab: vi.fn(() => Promise.resolve(null)),
    })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <BrowserTabPage />
        </LocaleProvider>,
      )
      await Promise.resolve()
    })

    expect(container.querySelector('img[src="https://www.163.com/favicon.ico"]')).not.toBeNull()
  })
})
