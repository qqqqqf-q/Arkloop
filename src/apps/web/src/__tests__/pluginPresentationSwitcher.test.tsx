import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { PluginBrowserSessionProvider } from '../plugins/browser-session'
import { PluginHostPage } from '../plugins/PluginHostPage'
import { PluginRuntimeProvider } from '../plugins/runtime'

const desktopMock = vi.hoisted(() => {
  let stateChangedHandler:
    | ((snapshot: {
        tabs: Array<{
          id: string
          title: string
          url: string
          faviconUrl: string | null
          loading: boolean
          error: string | null
          canGoBack: boolean
          canGoForward: boolean
        }>
      }) => void)
    | null = null

  const browserTabsApi = {
    list: vi.fn().mockResolvedValue({ tabs: [] }),
    create: vi.fn().mockImplementation(async () => {
      const tab = {
        id: 'browser-plugin',
        title: 'Plugin Tab',
        url: 'https://example.com/',
        faviconUrl: null,
        loading: false,
        error: null,
        canGoBack: false,
        canGoForward: false,
      }
      stateChangedHandler?.({ tabs: [tab] })
      return tab
    }),
    close: vi.fn(),
    navigate: vi.fn().mockResolvedValue({
      id: 'browser-plugin',
      title: 'Plugin Tab',
      url: 'https://example.com/',
      faviconUrl: null,
      loading: false,
      error: null,
      canGoBack: false,
      canGoForward: false,
    }),
    reload: vi.fn(),
    goBack: vi.fn(),
    goForward: vi.fn(),
    show: vi.fn(),
    hide: vi.fn(),
    syncBounds: vi.fn(),
    onStateChanged: vi.fn((callback) => {
      stateChangedHandler = callback
      return () => {
        stateChangedHandler = null
      }
    }),
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

const storageMock = vi.hoisted(() => ({
  readPluginRuntimeState: vi.fn(() => ({
    lastPluginId: null,
    presentationByPluginId: {},
  })),
  writePluginRuntimeState: vi.fn(),
}))

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readPluginRuntimeState: storageMock.readPluginRuntimeState,
    writePluginRuntimeState: storageMock.writePluginRuntimeState,
  }
})

describe('Plugin presentation switcher', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const actEnvironment = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    if (root) {
      act(() => root.unmount())
    }
    if (container) {
      container.remove()
    }
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('switches the plugin presentation from hybrid to embedded browser', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/plugins/sample-plugin']}>
          <BrowserTabsProvider>
            <PluginRuntimeProvider>
              <PluginBrowserSessionProvider>
                <Routes>
                  <Route path="/plugins/:pluginId" element={<PluginHostPage />} />
                </Routes>
              </PluginBrowserSessionProvider>
            </PluginRuntimeProvider>
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="plugin-presentation-value"]')?.textContent).toBe(
      'hybrid',
    )

    await act(async () => {
      container
        .querySelector('[data-testid="plugin-presentation-button-embedded-browser"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="plugin-presentation-value"]')?.textContent).toBe(
      'embedded-browser',
    )
    expect(desktopMock.browserTabsApi.navigate).toHaveBeenCalledWith(
      'browser-plugin',
      'https://example.com/',
    )
  })
})
