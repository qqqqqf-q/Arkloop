import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { PluginBrowserSessionProvider } from '../plugins/browser-session'
import { PluginWorkspaceShell } from '../plugins/PluginWorkspaceShell'
import { PluginRuntimeProvider } from '../plugins/runtime'
import type { PluginDefinition } from '../plugins/types'

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
      url: 'https://example.com/hybrid',
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

function SamplePluginBody() {
  return <div data-testid="sample-plugin-body">sample plugin page</div>
}

const hybridPlugin: PluginDefinition = {
  id: 'sample-plugin',
  title: 'Sample Plugin',
  desktopOnly: true,
  nav: { section: 'workspace', order: 100 },
  shell: { mode: 'plugin-workspace' },
  presentation: {
    default: 'hybrid',
    supported: ['route', 'embedded-browser', 'hybrid'],
  },
  surfaces: {
    mount: SamplePluginBody,
    resolveBrowserUrl: () => 'https://example.com/hybrid',
    browserPlacement: 'sidecar',
  },
}

describe('PluginWorkspaceShell', () => {
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
    act(() => root.unmount())
    container.remove()
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('opens a browser session and navigates it to the plugin url in hybrid mode', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/plugins/sample-plugin']}>
          <BrowserTabsProvider>
            <PluginRuntimeProvider>
              <PluginBrowserSessionProvider>
                <PluginWorkspaceShell plugin={hybridPlugin} presentation="hybrid" />
              </PluginBrowserSessionProvider>
            </PluginRuntimeProvider>
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="sample-plugin-body"]')).not.toBeNull()
    expect(desktopMock.browserTabsApi.create).toHaveBeenCalledTimes(1)
    expect(desktopMock.browserTabsApi.navigate).toHaveBeenCalledWith(
      'browser-plugin',
      'https://example.com/hybrid',
    )
  })
})
