import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { LocaleProvider } from '../contexts/LocaleContext'
import { BrowserTabsProvider, useBrowserTabs } from '../contexts/browser-tabs'
import { BrowserTabPage } from '../components/BrowserTabPage'

const desktopMock = vi.hoisted(() => {
  const list = vi.fn()
  let stateChangedListener: ((snapshot: { tabs: Array<{
    id: string
    title: string
    url: string
    loading: boolean
    canGoBack: boolean
    canGoForward: boolean
    faviconUrl: string | null
    error: string | null
  }> }) => void) | null = null
  const onStateChanged = vi.fn((listener) => {
    stateChangedListener = listener
    return () => {
      stateChangedListener = null
    }
  })
  const create = vi.fn()
  const navigate = vi.fn()
  const close = vi.fn()
  const reload = vi.fn()
  const goBack = vi.fn()
  const goForward = vi.fn()

  return {
    isDesktop: vi.fn(() => true),
    getDesktopApi: vi.fn(() => ({
      browserTabs: {
        list,
        onStateChanged,
        create,
        navigate,
        close,
        reload,
        goBack,
        goForward,
        show: vi.fn(() => Promise.resolve()),
        hide: vi.fn(() => Promise.resolve()),
        syncBounds: vi.fn(() => Promise.resolve()),
      },
    })),
    list,
    onStateChanged,
    create,
    navigate,
    close,
    reload,
    goBack,
    goForward,
    emitStateChanged: (snapshot: { tabs: Array<{
      id: string
      title: string
      url: string
      loading: boolean
      canGoBack: boolean
      canGoForward: boolean
      faviconUrl: string | null
      error: string | null
    }> }) => {
      stateChangedListener?.(snapshot)
    },
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: desktopMock.isDesktop,
  getDesktopApi: desktopMock.getDesktopApi,
}))

function BrowserTabsProbe() {
  const { panelOpen, activeBrowserTabId, openBrowserPanel } = useBrowserTabs()

  return (
    <div>
      <button type="button" onClick={() => openBrowserPanel()}>
        open-panel
      </button>
      <span data-testid="panel-state">{panelOpen ? 'open' : 'closed'}</span>
      <span data-testid="active-tab">{activeBrowserTabId ?? 'none'}</span>
    </div>
  )
}

describe('browser panel foundation', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)

    root = createRoot(container)
    desktopMock.list.mockResolvedValue({
      tabs: [{
        id: 'tab-1',
        title: 'Arkloop',
        url: 'https://arkloop.ai',
        loading: false,
        canGoBack: false,
        canGoForward: false,
        faviconUrl: null,
        error: null,
      }],
    })
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

  it('opens the browser panel and activates the created tab', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <BrowserTabsProvider>
            <BrowserTabsProbe />
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    await act(async () => {
      container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="panel-state"]')?.textContent).toBe('open')
    expect(container.querySelector('[data-testid="active-tab"]')?.textContent).toBe('tab-1')
  })

  it('renders the desktop-only fallback when desktop browser tabs are unavailable', async () => {
    desktopMock.isDesktop.mockReturnValue(false)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MemoryRouter initialEntries={['/']}>
            <BrowserTabsProvider>
              <BrowserTabPage />
            </BrowserTabsProvider>
          </MemoryRouter>
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('桌面端')
  })
})
