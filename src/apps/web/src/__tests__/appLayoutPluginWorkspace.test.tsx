import { act, useEffect, useRef } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@arkloop/shared'

import { AppLayout } from '../layouts/AppLayout'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthProvider } from '../contexts/auth'
import { ThreadListProvider } from '../contexts/thread-list'
import { AppUIProvider } from '../contexts/app-ui'
import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { PluginRuntimeProvider } from '../plugins/runtime'
import { PluginBrowserSessionProvider } from '../plugins/browser-session'
import { CreditsProvider } from '../contexts/credits'
import { PluginHostPage } from '../plugins/PluginHostPage'
import { usePluginRuntime } from '../plugins/runtime'
import {
  getMe,
  getMyCredits,
  listThreads,
  streamThreadRunStateEvents,
  type MeCreditsResponse,
  type MeResponse,
} from '../api'

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
    close: vi.fn().mockResolvedValue({ tabs: [] }),
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
    show: vi.fn().mockResolvedValue({ ok: true }),
    hide: vi.fn().mockResolvedValue({ ok: true }),
    syncBounds: vi.fn().mockResolvedValue({ ok: true }),
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

vi.mock('@arkloop/shared/desktop', async () => {
  const actual =
    await vi.importActual<typeof import('@arkloop/shared/desktop')>(
      '@arkloop/shared/desktop',
    )
  return {
    ...actual,
    isDesktop: desktopMock.isDesktop,
    getDesktopApi: desktopMock.getDesktopApi,
  }
})

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    getMe: vi.fn(),
    listThreads: vi.fn(),
    getMyCredits: vi.fn(),
    streamThreadRunStateEvents: vi.fn(),
  }
})

describe('AppLayout plugin workspace takeover', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const mockedGetMe = vi.mocked(getMe)
  const mockedListThreads = vi.mocked(listThreads)
  const mockedGetMyCredits = vi.mocked(getMyCredits)
  const mockedStreamThreadRunStateEvents = vi.mocked(streamThreadRunStateEvents)
  const actEnvironment = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    mockedGetMe.mockResolvedValue({
      id: 'user-1',
      username: 'Test User',
      email: 'test@example.com',
      email_verified: true,
      email_verification_required: false,
      work_enabled: true,
      timezone: 'Asia/Shanghai',
      account_timezone: 'Asia/Shanghai',
    } satisfies MeResponse)
    mockedListThreads.mockResolvedValue([])
    mockedGetMyCredits.mockResolvedValue({
      balance: 0,
      transactions: [],
    } satisfies MeCreditsResponse)
    mockedStreamThreadRunStateEvents.mockReturnValue(new Promise(() => {}))
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

  function PluginOpener({ pluginId }: { pluginId: string }) {
    const { openPlugin } = usePluginRuntime()
    const openedRef = useRef(false)

    useEffect(() => {
      if (openedRef.current) return
      openedRef.current = true
      void openPlugin(pluginId)
    }, [openPlugin, pluginId])

    return null
  }

  function LocationProbe() {
    const location = useLocation()
    return <div data-testid="path">{location.pathname}</div>
  }

  async function flushPluginBrowserEffects() {
    await Promise.resolve()
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await Promise.resolve()
  }

  it('renders plugin workspace content while keeping the global sidebar visible', async () => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/plugins/sample-hybrid-plugin']}>
              <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
                <ThreadListProvider>
                  <AppUIProvider>
                    <BrowserTabsProvider>
                      <PluginRuntimeProvider>
                        <PluginBrowserSessionProvider>
                          <CreditsProvider>
                            <Routes>
                              <Route element={<AppLayout />}>
                                <Route
                                  path="/plugins/:pluginId"
                                  element={<PluginHostPage />}
                                />
                              </Route>
                            </Routes>
                          </CreditsProvider>
                        </PluginBrowserSessionProvider>
                      </PluginRuntimeProvider>
                    </BrowserTabsProvider>
                  </AppUIProvider>
                </ThreadListProvider>
              </AuthProvider>
            </MemoryRouter>
          </ToastProvider>
        </LocaleProvider>,
      )
      await flushPluginBrowserEffects()
    })

    expect(container.textContent).toContain('Sample Hybrid Plugin')
    expect(container.textContent).toContain('sample hybrid plugin page')
    expect(
      container.querySelector('[data-testid="workspace-host"]'),
    ).not.toBeNull()
  })

  it('keeps the main workspace visible while opening the browser panel for a hybrid plugin', async () => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/']}>
              <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
                <ThreadListProvider>
                  <AppUIProvider>
                    <BrowserTabsProvider>
                      <PluginRuntimeProvider>
                        <PluginBrowserSessionProvider>
                          <CreditsProvider>
                            <PluginOpener pluginId="sample-hybrid-plugin" />
                            <LocationProbe />
                            <Routes>
                              <Route element={<AppLayout />}>
                                <Route path="/" element={<div data-testid="chat-view">Chat view</div>} />
                                <Route
                                  path="/plugins/:pluginId"
                                  element={<PluginHostPage />}
                                />
                              </Route>
                            </Routes>
                          </CreditsProvider>
                        </PluginBrowserSessionProvider>
                      </PluginRuntimeProvider>
                    </BrowserTabsProvider>
                  </AppUIProvider>
                </ThreadListProvider>
              </AuthProvider>
            </MemoryRouter>
          </ToastProvider>
        </LocaleProvider>,
      )
      await flushPluginBrowserEffects()
    })

    expect(container.querySelector('[data-testid="path"]')?.textContent).toBe('/')
    expect(container.querySelector('[data-testid="workspace-host"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="chat-view"]')?.textContent).toContain('Chat view')
    expect(container.querySelectorAll('[data-testid="browser-tab-page"]')).toHaveLength(1)
  })

  it('hides the main workspace while opening the browser panel fullscreen for a browser plugin', async () => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/']}>
              <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
                <ThreadListProvider>
                  <AppUIProvider>
                    <BrowserTabsProvider>
                      <PluginRuntimeProvider>
                        <PluginBrowserSessionProvider>
                          <CreditsProvider>
                            <PluginOpener pluginId="sample-browser-plugin" />
                            <LocationProbe />
                            <Routes>
                              <Route element={<AppLayout />}>
                                <Route path="/" element={<div data-testid="chat-view">Chat view</div>} />
                                <Route
                                  path="/plugins/:pluginId"
                                  element={<PluginHostPage />}
                                />
                              </Route>
                            </Routes>
                          </CreditsProvider>
                        </PluginBrowserSessionProvider>
                      </PluginRuntimeProvider>
                    </BrowserTabsProvider>
                  </AppUIProvider>
                </ThreadListProvider>
              </AuthProvider>
            </MemoryRouter>
          </ToastProvider>
        </LocaleProvider>,
      )
      await flushPluginBrowserEffects()
    })

    expect(container.querySelector('[data-testid="path"]')?.textContent).toBe('/')
    expect(container.querySelector('[data-testid="workspace-host"]')).toBeNull()
    expect(container.querySelector('[data-testid="chat-view"]')).toBeNull()
    expect(container.querySelectorAll('[data-testid="browser-tab-page"]')).toHaveLength(1)
  })

  it('exits browser plugin takeover when collapsing the browser panel', async () => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/']}>
              <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
                <ThreadListProvider>
                  <AppUIProvider>
                    <BrowserTabsProvider>
                      <PluginRuntimeProvider>
                        <PluginBrowserSessionProvider>
                          <CreditsProvider>
                            <PluginOpener pluginId="sample-browser-plugin" />
                            <LocationProbe />
                            <Routes>
                              <Route element={<AppLayout />}>
                                <Route path="/" element={<div data-testid="chat-view">Chat view</div>} />
                                <Route
                                  path="/plugins/:pluginId"
                                  element={<PluginHostPage />}
                                />
                              </Route>
                            </Routes>
                          </CreditsProvider>
                        </PluginBrowserSessionProvider>
                      </PluginRuntimeProvider>
                    </BrowserTabsProvider>
                  </AppUIProvider>
                </ThreadListProvider>
              </AuthProvider>
            </MemoryRouter>
          </ToastProvider>
        </LocaleProvider>,
      )
      await flushPluginBrowserEffects()
    })

    expect(container.querySelector('[data-testid="workspace-host"]')).toBeNull()
    expect(container.querySelector('[data-testid="chat-view"]')).toBeNull()

    await act(async () => {
      container
        .querySelector('button[title="收起右侧浏览器"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushPluginBrowserEffects()
    })

    expect(container.querySelector('[data-testid="path"]')?.textContent).toBe('/')
    expect(container.querySelector('[data-testid="workspace-host"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="chat-view"]')?.textContent).toContain('Chat view')
  })
})
