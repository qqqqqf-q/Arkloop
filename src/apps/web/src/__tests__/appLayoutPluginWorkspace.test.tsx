import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
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
import {
  getMe,
  getMyCredits,
  listThreads,
  streamThreadRunStateEvents,
  type MeCreditsResponse,
  type MeResponse,
} from '../api'

vi.mock('@arkloop/shared/desktop', async () => {
  const actual =
    await vi.importActual<typeof import('@arkloop/shared/desktop')>(
      '@arkloop/shared/desktop',
    )
  return {
    ...actual,
    isDesktop: vi.fn(() => true),
    getDesktopApi: () => ({}),
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

  it('renders plugin workspace content while keeping the global sidebar visible', async () => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/plugins/sample-plugin']}>
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
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.textContent).toContain('Sample Plugin')
    expect(container.textContent).toContain('sample plugin page')
    expect(
      container.querySelector('[data-testid="workspace-host"]'),
    ).not.toBeNull()
  })
})
