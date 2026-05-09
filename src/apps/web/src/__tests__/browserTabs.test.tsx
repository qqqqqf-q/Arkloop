import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BrowserTabsProvider, useBrowserTabs } from '../contexts/browser-tabs'

const desktopMock = vi.hoisted(() => {
  const browserTabsApi = {
    list: vi.fn(),
    create: vi.fn(),
    close: vi.fn(),
    navigate: vi.fn(),
    reload: vi.fn(),
    goBack: vi.fn(),
    goForward: vi.fn(),
    show: vi.fn(),
    hide: vi.fn(),
    syncBounds: vi.fn(),
    onStateChanged: vi.fn(),
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

function BrowserTabsProbe() {
  const { tabs, panelOpen, activeBrowserTabId, openBrowserPanel, activateBrowserTab, closeBrowserTab } = useBrowserTabs()

  return (
    <div>
      <div data-testid="panel-open">{panelOpen ? 'open' : 'closed'}</div>
      <div data-testid="active">{activeBrowserTabId ?? 'none'}</div>
      <div data-testid="tabs">{tabs.map((tab) => tab.id).join(',')}</div>
      <button type="button" onClick={openBrowserPanel}>
        open-panel
      </button>
      <button type="button" onClick={() => activateBrowserTab('browser-b')}>
        activate-b
      </button>
      <button type="button" onClick={() => void closeBrowserTab(activeBrowserTabId ?? '')}>
        close-active
      </button>
    </div>
  )
}

function BrowserTabsDraftProbe() {
  const {
    tabs,
    panelOpen,
    activeBrowserTabId,
    createBrowserTab,
    activateBrowserTab,
    getDraftUrl,
    setDraftUrl,
  } = useBrowserTabs()

  return (
    <div>
      <div data-testid="panel-open">{panelOpen ? 'open' : 'closed'}</div>
      <div data-testid="active">{activeBrowserTabId ?? 'none'}</div>
      <div data-testid="tabs">{tabs.map((tab) => tab.id).join(',')}</div>
      <div data-testid="draft">{activeBrowserTabId ? getDraftUrl(activeBrowserTabId) : ''}</div>
      <button type="button" onClick={() => void createBrowserTab()}>
        create
      </button>
      <button type="button" onClick={() => activeBrowserTabId && setDraftUrl(activeBrowserTabId, 'draft-a.test')}>
        set-draft-a
      </button>
      <button type="button" onClick={() => activeBrowserTabId && setDraftUrl(activeBrowserTabId, 'draft-b.test')}>
        set-draft-b
      </button>
      <button type="button" onClick={() => activateBrowserTab('browser-a')}>
        activate-a
      </button>
      <button type="button" onClick={() => activateBrowserTab('browser-b')}>
        activate-b
      </button>
    </div>
  )
}

describe('BrowserTabsProvider', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot> | null
  let stateChangedHandler: ((snapshot: { tabs: Array<{
    id: string
    title: string
    url: string
    loading: boolean
    error: string | null
    canGoBack: boolean
    canGoForward: boolean
  }> }) => void) | null
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    desktopMock.isDesktop.mockReturnValue(true)
    desktopMock.browserTabsApi.list.mockResolvedValue({
      tabs: [
        { id: 'browser-a', title: 'A', url: 'https://a.test', faviconUrl: null, loading: false, error: null, canGoBack: false, canGoForward: false },
        { id: 'browser-b', title: 'B', url: 'https://b.test', faviconUrl: null, loading: false, error: null, canGoBack: false, canGoForward: false },
      ],
    })
    stateChangedHandler = null
    desktopMock.browserTabsApi.onStateChanged.mockImplementation((callback) => {
      stateChangedHandler = callback
      return () => {
        stateChangedHandler = null
      }
    })
    desktopMock.browserTabsApi.close.mockResolvedValue({
      tabs: [
        { id: 'browser-a', title: 'A', url: 'https://a.test', faviconUrl: null, loading: false, error: null, canGoBack: false, canGoForward: false },
      ],
    })
  })

  afterEach(() => {
    if (root) {
      act(() => root!.unmount())
    }
    container.remove()
    root = null
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('关闭当前浏览页签后回退到最近可用页签', async () => {
    await act(async () => {
      root!.render(
        <MemoryRouter initialEntries={['/']}>
          <BrowserTabsProvider>
            <BrowserTabsProbe />
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    await act(async () => {
      const buttons = container.querySelectorAll('button')
      buttons[0]?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="panel-open"]')?.textContent).toBe('open')
    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('browser-a')
    expect(container.querySelector('[data-testid="tabs"]')?.textContent).toBe('browser-a,browser-b')

    await act(async () => {
      const buttons = container.querySelectorAll('button')
      buttons[1]?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('browser-b')

    await act(async () => {
      const buttons = container.querySelectorAll('button')
      buttons[2]?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(desktopMock.browserTabsApi.close).toHaveBeenCalledWith('browser-b')
    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('browser-a')
  })

  it('在右侧浏览器容器中新建页签后自动激活并展开面板', async () => {
    desktopMock.browserTabsApi.list.mockResolvedValueOnce({ tabs: [] })
    desktopMock.browserTabsApi.create.mockImplementation(async () => {
      const tab = {
        id: 'browser-new',
        title: 'New Tab',
        url: '',
        faviconUrl: null,
        loading: false,
        error: null,
        canGoBack: false,
        canGoForward: false,
      }
      stateChangedHandler?.({ tabs: [tab] })
      return tab
    })

    await act(async () => {
      root!.render(
        <MemoryRouter initialEntries={['/']}>
          <BrowserTabsProvider>
            <BrowserTabsDraftProbe />
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    await act(async () => {
      container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(desktopMock.browserTabsApi.create).toHaveBeenCalledTimes(1)
    expect(container.querySelector('[data-testid="panel-open"]')?.textContent).toBe('open')
    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('browser-new')
    expect(container.querySelector('[data-testid="tabs"]')?.textContent).toBe('browser-new')
  })

  it('在多个浏览页签之间切换时保留各自独立的地址草稿', async () => {
    await act(async () => {
      root!.render(
        <MemoryRouter initialEntries={['/']}>
          <BrowserTabsProvider>
            <BrowserTabsDraftProbe />
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    const buttons = container.querySelectorAll('button')
    const createButton = buttons[0]
    const setDraftAButton = buttons[1]
    const setDraftBButton = buttons[2]
    const activateAButton = buttons[3]
    const activateBButton = buttons[4]
    expect(createButton?.textContent).toBe('create')

    await act(async () => {
      activateAButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    expect(container.querySelector('[data-testid="panel-open"]')?.textContent).toBe('open')

    await act(async () => {
      setDraftAButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(container.querySelector('[data-testid="draft"]')?.textContent).toBe('draft-a.test')

    await act(async () => {
      activateBButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('browser-b')
    expect(container.querySelector('[data-testid="draft"]')?.textContent).toBe('https://b.test')

    await act(async () => {
      setDraftBButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(container.querySelector('[data-testid="draft"]')?.textContent).toBe('draft-b.test')

    await act(async () => {
      activateAButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('browser-a')
    expect(container.querySelector('[data-testid="draft"]')?.textContent).toBe('draft-a.test')
  })
})
