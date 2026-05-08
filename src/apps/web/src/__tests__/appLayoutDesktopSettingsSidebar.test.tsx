import { act, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@arkloop/shared'

import { AppLayout } from '../layouts/AppLayout'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthProvider } from '../contexts/auth'
import { ThreadListProvider } from '../contexts/thread-list'
import { AppUIProvider, useSettingsUI } from '../contexts/app-ui'
import { CreditsProvider } from '../contexts/credits'
import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { PluginRuntimeProvider } from '../plugins/runtime'
import { PluginBrowserSessionProvider } from '../plugins/browser-session'
import {
  getMe,
  getMyCredits,
  listThreads,
  streamThreadRunStateEvents,
  type MeCreditsResponse,
  type MeResponse,
} from '../api'
import {
  readGtdArchivedThreadIds,
  readGtdInboxThreadIds,
  readGtdSomedayThreadIds,
  readGtdTodoThreadIds,
  readGtdWaitingThreadIds,
  readLegacyThreadModesForMigration,
  readPinnedThreadIds,
  readThreadWorkFolder,
} from '../storage'

vi.mock('@arkloop/shared/desktop', async () => {
  const actual =
    await vi.importActual<typeof import('@arkloop/shared/desktop')>('@arkloop/shared/desktop')
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

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readLocaleFromStorage: vi.fn(() => 'zh'),
    writeLocaleToStorage: vi.fn(),
    readLegacyThreadModesForMigration: vi.fn(() => ({})),
    writeLegacyThreadModesForMigration: vi.fn(),
    readPinnedThreadIds: vi.fn(() => new Set()),
    writePinnedThreadIds: vi.fn(),
    readThreadWorkFolder: vi.fn(() => null),
    writeThreadWorkFolder: vi.fn(),
    clearThreadWorkFolder: vi.fn(),
    readGtdEnabled: vi.fn(() => false),
    readGtdInboxThreadIds: vi.fn(() => new Set()),
    writeGtdInboxThreadIds: vi.fn(),
    readGtdTodoThreadIds: vi.fn(() => new Set()),
    writeGtdTodoThreadIds: vi.fn(),
    readGtdWaitingThreadIds: vi.fn(() => new Set()),
    writeGtdWaitingThreadIds: vi.fn(),
    readGtdSomedayThreadIds: vi.fn(() => new Set()),
    writeGtdSomedayThreadIds: vi.fn(),
    readGtdArchivedThreadIds: vi.fn(() => new Set()),
    writeGtdArchivedThreadIds: vi.fn(),
  }
})

function OpenSettingsOnMount() {
  const { openSettings } = useSettingsUI()

  useEffect(() => {
    openSettings('settings')
  }, [openSettings])

  return null
}

function OutletShell() {
  return (
    <>
      <OpenSettingsOnMount />
      <Outlet />
    </>
  )
}

describe('AppLayout desktop settings sidebar replacement', () => {
  const mockedGetMe = vi.mocked(getMe)
  const mockedListThreads = vi.mocked(listThreads)
  const mockedGetMyCredits = vi.mocked(getMyCredits)
  const mockedStreamThreadRunStateEvents = vi.mocked(streamThreadRunStateEvents)
  const mockedReadLegacyThreadModesForMigration = vi.mocked(readLegacyThreadModesForMigration)
  const mockedReadPinnedThreadIds = vi.mocked(readPinnedThreadIds)
  const mockedReadThreadWorkFolder = vi.mocked(readThreadWorkFolder)
  const mockedReadGtdInboxThreadIds = vi.mocked(readGtdInboxThreadIds)
  const mockedReadGtdTodoThreadIds = vi.mocked(readGtdTodoThreadIds)
  const mockedReadGtdWaitingThreadIds = vi.mocked(readGtdWaitingThreadIds)
  const mockedReadGtdSomedayThreadIds = vi.mocked(readGtdSomedayThreadIds)
  const mockedReadGtdArchivedThreadIds = vi.mocked(readGtdArchivedThreadIds)
  const actEnvironment = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true

    mockedGetMe.mockReset()
    mockedListThreads.mockReset()
    mockedGetMyCredits.mockReset()
    mockedStreamThreadRunStateEvents.mockReset()
    mockedReadLegacyThreadModesForMigration.mockReset()
    mockedReadPinnedThreadIds.mockReset()
    mockedReadThreadWorkFolder.mockReset()
    mockedReadGtdInboxThreadIds.mockReset()
    mockedReadGtdTodoThreadIds.mockReset()
    mockedReadGtdWaitingThreadIds.mockReset()
    mockedReadGtdSomedayThreadIds.mockReset()
    mockedReadGtdArchivedThreadIds.mockReset()

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
    mockedReadLegacyThreadModesForMigration.mockReturnValue({})
    mockedReadPinnedThreadIds.mockReturnValue(new Set())
    mockedReadThreadWorkFolder.mockReturnValue(null)
    mockedReadGtdInboxThreadIds.mockReturnValue(new Set())
    mockedReadGtdTodoThreadIds.mockReturnValue(new Set())
    mockedReadGtdWaitingThreadIds.mockReturnValue(new Set())
    mockedReadGtdSomedayThreadIds.mockReturnValue(new Set())
    mockedReadGtdArchivedThreadIds.mockReturnValue(new Set())
  })

  afterEach(() => {
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  async function renderLayout(options?: { openSettingsOnMount?: boolean }) {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    const shell = options?.openSettingsOnMount ? <OutletShell /> : <Outlet />

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
                            <Routes>
                              <Route element={<AppLayout />}>
                                <Route element={shell}>
                                  <Route index element={<div>chat body</div>} />
                                </Route>
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
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    return { container, root }
  }

  it('打开桌面设置后应使用设置侧栏而不是继续显示 app 侧栏', async () => {
    const { container, root } = await renderLayout({ openSettingsOnMount: true })

    expect(container.textContent).toContain('设置')
    expect(container.textContent).toContain('通用')
    expect(container.textContent).not.toContain('无痕模式下，会话不会保存到历史记录')
    expect(
      container.querySelector('[data-testid="desktop-titlebar-sidebar-controls"]'),
    ).toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('点击顶部按钮后应完全隐藏 app 侧边栏，但保留顶部按钮组', async () => {
    const { container, root } = await renderLayout()

    expect(container.querySelector('[data-testid="app-sidebar"]')).not.toBeNull()
    const toggleButton = container.querySelector(
      '[data-testid="desktop-titlebar-toggle-sidebar"]',
    )
    expect(toggleButton).not.toBeNull()

    await act(async () => {
      toggleButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="app-sidebar"]')).toBeNull()
    expect(
      container.querySelector('[data-testid="desktop-titlebar-sidebar-controls"]'),
    ).not.toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
