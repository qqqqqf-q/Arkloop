import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppLayout } from '../layouts/AppLayout'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthProvider } from '../contexts/auth'
import { ThreadListProvider } from '../contexts/thread-list'
import { AppUIProvider } from '../contexts/app-ui'
import { CreditsProvider } from '../contexts/credits'
import { getMe, listThreads, getMyCredits } from '../api'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    getMe: vi.fn(),
    listThreads: vi.fn(),
    getMyCredits: vi.fn(),
    logout: vi.fn(),
  }
})

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readLocaleFromStorage: vi.fn(() => 'zh'),
    writeLocaleToStorage: vi.fn(),
  }
})

function OutletShell() {
  return <Outlet />
}

describe('AppLayout loading state', () => {
  const mockedGetMe = vi.mocked(getMe)
  const mockedListThreads = vi.mocked(listThreads)
  const mockedGetMyCredits = vi.mocked(getMyCredits)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    mockedGetMe.mockReset()
    mockedListThreads.mockReset()
    mockedGetMyCredits.mockReset()

    mockedGetMe.mockReturnValue(new Promise(() => {}))
    mockedListThreads.mockReturnValue(new Promise(() => {}))
    mockedGetMyCredits.mockReturnValue(new Promise(() => {}))
  })

  afterEach(() => {
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('等待用户上下文时应显示全屏加载页而不是空白', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MemoryRouter initialEntries={['/']}>
            <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
              <ThreadListProvider>
                <AppUIProvider>
                  <CreditsProvider>
                    <Routes>
                      <Route
                        element={<AppLayout />}
                      >
                        <Route element={<OutletShell />}>
                          <Route index element={<div>home</div>} />
                        </Route>
                      </Route>
                    </Routes>
                  </CreditsProvider>
                </AppUIProvider>
              </ThreadListProvider>
            </AuthProvider>
          </MemoryRouter>
        </LocaleProvider>,
      )
    })

    expect(mockedGetMe).toHaveBeenCalledWith('token')
    expect(mockedListThreads).toHaveBeenCalledWith('token', { limit: 200 })
    expect(mockedGetMyCredits).toHaveBeenCalledWith('token')
    expect(container.textContent).toContain('Arkloop')
    expect(container.textContent).toContain('加载中...')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
