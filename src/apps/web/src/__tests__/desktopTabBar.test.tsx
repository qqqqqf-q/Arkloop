import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: vi.fn(() => true),
}))

vi.mock('../contexts/auth', () => ({
  useAuth: () => ({
    accessToken: 'test-token',
  }),
}))

vi.mock('../contexts/thread-list', () => ({
  useThreadList: () => ({
    updateTitle: vi.fn(),
    removeThread: vi.fn(),
  }),
}))

vi.mock('../api', () => ({
  starThread: vi.fn(() => Promise.resolve()),
  unstarThread: vi.fn(() => Promise.resolve()),
  updateThreadTitle: vi.fn(() => Promise.resolve()),
  deleteThread: vi.fn(() => Promise.resolve()),
  listStarredThreadIds: vi.fn(() => Promise.resolve([])),
}))

import { DesktopTabBar } from '../components/DesktopTabBar'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { ThreadResponse } from '../api'

const currentThread: ThreadResponse = {
  id: 'thread-1',
  account_id: 'account-1',
  created_by_user_id: 'user-1',
  mode: 'chat',
  title: '当前会话',
  project_id: 'project-1',
  created_at: '2026-05-08T00:00:00Z',
  active_run_id: null,
  is_private: false,
  collaboration_mode: 'manual',
  collaboration_mode_revision: 1,
  learning_mode_enabled: false,
}

describe('DesktopTabBar', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot> | null
  let originalLocalStorage: Storage | undefined
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
  })

  afterEach(() => {
    if (root) {
      act(() => root!.unmount())
    }
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

  it('在主体 Bar 中展示 chat/work，并把右侧按钮作为浏览器容器展开入口', async () => {
    const handleToggleBrowserPanel = vi.fn()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopTabBar
            appMode="chat"
            availableModes={['chat', 'work']}
            browserPanelOpen={false}
            onSetAppMode={() => {}}
            onToggleBrowserPanel={handleToggleBrowserPanel}
            currentThread={currentThread}
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('当前会话')
    expect(container.querySelector('button[title="新建网页 Tab"]')).toBeNull()
    const expandButton = (
      container.querySelector('button[title="展开右侧浏览器"]') ??
      container.querySelector('button[title="Open right browser panel"]')
    ) as HTMLButtonElement | null
    expect(expandButton).not.toBeNull()

    await act(async () => {
      expandButton?.click()
    })
    expect(handleToggleBrowserPanel).toHaveBeenCalledTimes(1)
  })

  it('浏览器容器展开后，主体 Bar 不再显示展开按钮', async () => {
    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopTabBar
            appMode="chat"
            availableModes={['chat', 'work']}
            browserPanelOpen
            onSetAppMode={() => {}}
            onToggleBrowserPanel={() => {}}
            currentThread={currentThread}
          />
        </LocaleProvider>,
      )
    })

    expect(
      container.querySelector('button[title="展开右侧浏览器"]') ??
      container.querySelector('button[title="Open right browser panel"]'),
    ).toBeNull()
    expect(container.textContent).toContain('当前会话')
  })

  it('主体 Bar 不渲染底部边线', async () => {
    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopTabBar
            appMode="chat"
            availableModes={['chat', 'work']}
            browserPanelOpen={false}
            onSetAppMode={() => {}}
            onToggleBrowserPanel={() => {}}
            currentThread={currentThread}
          />
        </LocaleProvider>,
      )
    })

    const bar = container.firstElementChild as HTMLDivElement | null
    expect(bar?.style.borderBottom).toBe('')
  })
})
