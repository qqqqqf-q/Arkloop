import { act, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppLayout } from '../layouts/AppLayout'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthProvider } from '../contexts/auth'
import { ThreadListProvider, useThreadList } from '../contexts/thread-list'
import { AppUIProvider } from '../contexts/app-ui'
import { CreditsProvider } from '../contexts/credits'
import { getMe, listThreads, getMyCredits, streamThreadRunStateEvents, updateThreadMode, updateThreadSidebarState, type ThreadResponse } from '../api'
import {
  readGtdArchivedThreadIds,
  readGtdInboxThreadIds,
  readGtdSomedayThreadIds,
  readGtdTodoThreadIds,
  readGtdWaitingThreadIds,
  readLegacyThreadModesForMigration,
  readPinnedThreadIds,
  readThreadWorkFolder,
  writeGtdArchivedThreadIds,
  writeGtdInboxThreadIds,
  writeGtdSomedayThreadIds,
  writeGtdTodoThreadIds,
  writeGtdWaitingThreadIds,
  writeLegacyThreadModesForMigration,
  writePinnedThreadIds,
} from '../storage'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    getMe: vi.fn(),
    listThreads: vi.fn(),
    getMyCredits: vi.fn(),
    streamThreadRunStateEvents: vi.fn(),
    updateThreadMode: vi.fn(),
    updateThreadSidebarState: vi.fn(),
    logout: vi.fn(),
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

function OutletShell() {
  return <Outlet />
}

function makeThread(overrides: Partial<ThreadResponse>): ThreadResponse {
  return {
    id: 'thread-1',
    account_id: 'account-1',
    created_by_user_id: 'user-1',
    mode: 'chat',
    title: 'Thread',
    project_id: 'project-1',
    created_at: '2026-01-01T00:00:00.000Z',
    active_run_id: null,
    is_private: false,
    collaboration_mode: 'default',
    collaboration_mode_revision: 0,
    learning_mode_enabled: false,
    updated_at: '2026-01-01T00:00:00.000Z',
    ...overrides,
  }
}

function ThreadProbe({ onThreads }: { onThreads: (threads: ThreadResponse[]) => void }) {
  const { threads } = useThreadList()
  onThreads(threads)
  return null
}

function UpsertProbe({ thread }: { thread: ThreadResponse }) {
  const { upsertThread } = useThreadList()
  useEffect(() => {
    upsertThread(thread)
  }, [thread, upsertThread])
  return null
}

describe('AppLayout loading state', () => {
  const mockedGetMe = vi.mocked(getMe)
  const mockedListThreads = vi.mocked(listThreads)
  const mockedGetMyCredits = vi.mocked(getMyCredits)
  const mockedStreamThreadRunStateEvents = vi.mocked(streamThreadRunStateEvents)
  const mockedUpdateThreadMode = vi.mocked(updateThreadMode)
  const mockedUpdateThreadSidebarState = vi.mocked(updateThreadSidebarState)
  const mockedReadLegacyThreadModesForMigration = vi.mocked(readLegacyThreadModesForMigration)
  const mockedWriteLegacyThreadModesForMigration = vi.mocked(writeLegacyThreadModesForMigration)
  const mockedReadPinnedThreadIds = vi.mocked(readPinnedThreadIds)
  const mockedReadThreadWorkFolder = vi.mocked(readThreadWorkFolder)
  const mockedReadGtdInboxThreadIds = vi.mocked(readGtdInboxThreadIds)
  const mockedReadGtdTodoThreadIds = vi.mocked(readGtdTodoThreadIds)
  const mockedReadGtdWaitingThreadIds = vi.mocked(readGtdWaitingThreadIds)
  const mockedReadGtdSomedayThreadIds = vi.mocked(readGtdSomedayThreadIds)
  const mockedReadGtdArchivedThreadIds = vi.mocked(readGtdArchivedThreadIds)
  const mockedWritePinnedThreadIds = vi.mocked(writePinnedThreadIds)
  const mockedWriteGtdInboxThreadIds = vi.mocked(writeGtdInboxThreadIds)
  const mockedWriteGtdTodoThreadIds = vi.mocked(writeGtdTodoThreadIds)
  const mockedWriteGtdWaitingThreadIds = vi.mocked(writeGtdWaitingThreadIds)
  const mockedWriteGtdSomedayThreadIds = vi.mocked(writeGtdSomedayThreadIds)
  const mockedWriteGtdArchivedThreadIds = vi.mocked(writeGtdArchivedThreadIds)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    mockedGetMe.mockReset()
    mockedListThreads.mockReset()
    mockedGetMyCredits.mockReset()
    mockedStreamThreadRunStateEvents.mockReset()
    mockedUpdateThreadMode.mockReset()
    mockedUpdateThreadSidebarState.mockReset()
    mockedReadLegacyThreadModesForMigration.mockReset()
    mockedWriteLegacyThreadModesForMigration.mockReset()
    mockedReadPinnedThreadIds.mockReset()
    mockedReadThreadWorkFolder.mockReset()
    mockedReadGtdInboxThreadIds.mockReset()
    mockedReadGtdTodoThreadIds.mockReset()
    mockedReadGtdWaitingThreadIds.mockReset()
    mockedReadGtdSomedayThreadIds.mockReset()
    mockedReadGtdArchivedThreadIds.mockReset()
    mockedWritePinnedThreadIds.mockReset()
    mockedWriteGtdInboxThreadIds.mockReset()
    mockedWriteGtdTodoThreadIds.mockReset()
    mockedWriteGtdWaitingThreadIds.mockReset()
    mockedWriteGtdSomedayThreadIds.mockReset()
    mockedWriteGtdArchivedThreadIds.mockReset()

    mockedGetMe.mockReturnValue(new Promise(() => {}))
    mockedListThreads.mockReturnValue(new Promise(() => {}))
    mockedGetMyCredits.mockReturnValue(new Promise(() => {}))
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
    expect(mockedListThreads).toHaveBeenCalledWith('token', { limit: 200, mode: 'chat' })
    expect(mockedListThreads).toHaveBeenCalledWith('token', { limit: 200, mode: 'work' })
    expect(mockedGetMyCredits).toHaveBeenCalledWith('token')
    expect(container.textContent).toContain('Arkloop')
    expect(container.textContent).toContain('加载中...')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('将旧版本地 Work 线程标记迁移到服务端 mode', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const legacyThread = makeThread({ id: 'legacy-work-thread', mode: 'chat' })
    const migratedThread = { ...legacyThread, mode: 'work' as const }
    const observedThreads: ThreadResponse[][] = []

    mockedReadLegacyThreadModesForMigration.mockReturnValue({
      'legacy-work-thread': 'work',
      'legacy-chat-thread': 'chat',
    })
    mockedListThreads.mockImplementation(async (_token, req) => (req?.mode === 'chat' ? [legacyThread] : []))
    mockedUpdateThreadMode.mockResolvedValue(migratedThread)

    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <ThreadListProvider>
            <ThreadProbe onThreads={(threads) => observedThreads.push(threads)} />
          </ThreadListProvider>
        </AuthProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(mockedUpdateThreadMode).toHaveBeenCalledWith('token', 'legacy-work-thread', 'work')
    expect(observedThreads.at(-1)?.map((thread) => thread.id)).toEqual(['legacy-work-thread'])
    expect(mockedWriteLegacyThreadModesForMigration).toHaveBeenCalledWith({})

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('将旧版侧栏文件夹、置顶和 GTD 状态迁移到服务端', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const workThread = makeThread({ id: 'work-thread', mode: 'work' })
    const chatThread = makeThread({ id: 'chat-thread', mode: 'chat' })
    const migratedWorkThread = {
      ...workThread,
      sidebar_work_folder: '/workspace/arkloop',
      sidebar_pinned_at: '2026-01-01T00:00:00.000Z',
    }
    const migratedChatThread = {
      ...chatThread,
      sidebar_gtd_bucket: 'todo' as const,
    }
    const observedThreads: ThreadResponse[][] = []

    mockedListThreads.mockImplementation(async (_token, req) => {
      if (req?.mode === 'work') return [workThread]
      if (req?.mode === 'chat') return [chatThread]
      return []
    })
    mockedReadPinnedThreadIds.mockReturnValue(new Set(['work-thread']))
    mockedReadThreadWorkFolder.mockImplementation((threadId) => (threadId === 'work-thread' ? '/workspace/arkloop' : null))
    mockedReadGtdTodoThreadIds.mockReturnValue(new Set(['chat-thread']))
    mockedUpdateThreadSidebarState.mockImplementation(async (_token, threadId, req) => {
      if (threadId === 'work-thread') {
        expect(req).toEqual({
          sidebar_work_folder: '/workspace/arkloop',
          sidebar_pinned: true,
        })
        return migratedWorkThread
      }
      if (threadId === 'chat-thread') {
        expect(req).toEqual({ sidebar_gtd_bucket: 'todo' })
        return migratedChatThread
      }
      throw new Error('unexpected thread')
    })

    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <ThreadListProvider>
            <ThreadProbe onThreads={(threads) => observedThreads.push(threads)} />
          </ThreadListProvider>
        </AuthProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(mockedUpdateThreadSidebarState).toHaveBeenCalledWith('token', 'work-thread', {
      sidebar_work_folder: '/workspace/arkloop',
      sidebar_pinned: true,
    })
    expect(mockedUpdateThreadSidebarState).toHaveBeenCalledWith('token', 'chat-thread', {
      sidebar_gtd_bucket: 'todo',
    })
    expect(observedThreads.at(-1)?.find((thread) => thread.id === 'work-thread')?.sidebar_work_folder).toBe('/workspace/arkloop')
    expect(observedThreads.at(-1)?.find((thread) => thread.id === 'chat-thread')?.sidebar_gtd_bucket).toBe('todo')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('保留未加载线程的旧版侧栏状态', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const visibleThread = makeThread({ id: 'visible-chat-thread', mode: 'chat' })

    mockedListThreads.mockImplementation(async (_token, req) => (req?.mode === 'chat' ? [visibleThread] : []))
    mockedReadPinnedThreadIds.mockReturnValue(new Set(['old-pinned-thread']))
    mockedReadGtdTodoThreadIds.mockReturnValue(new Set(['old-todo-thread']))

    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <ThreadListProvider>
            <ThreadProbe onThreads={() => {}} />
          </ThreadListProvider>
        </AuthProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect([...mockedWritePinnedThreadIds.mock.calls.at(-1)![0]]).toEqual(['old-pinned-thread'])
    expect([...mockedWriteGtdTodoThreadIds.mock.calls.at(-1)![0]]).toEqual(['old-todo-thread'])

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('迁移旧版侧栏状态时不覆盖已有服务端字段', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const workThread = makeThread({
      id: 'server-work-thread',
      mode: 'work',
      sidebar_work_folder: '/server/workspace',
      sidebar_pinned_at: '2026-01-01T00:00:00.000Z',
    })
    const chatThread = makeThread({
      id: 'server-chat-thread',
      mode: 'chat',
      sidebar_gtd_bucket: 'waiting',
    })

    mockedListThreads.mockImplementation(async (_token, req) => {
      if (req?.mode === 'work') return [workThread]
      if (req?.mode === 'chat') return [chatThread]
      return []
    })
    mockedReadPinnedThreadIds.mockReturnValue(new Set(['server-work-thread']))
    mockedReadThreadWorkFolder.mockImplementation((threadId) => (
      threadId === 'server-work-thread' ? '/local/workspace' : null
    ))
    mockedReadGtdTodoThreadIds.mockReturnValue(new Set(['server-chat-thread']))

    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <ThreadListProvider>
            <ThreadProbe onThreads={() => {}} />
          </ThreadListProvider>
        </AuthProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(mockedUpdateThreadSidebarState).not.toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('直接打开历史线程时迁移旧版侧栏状态', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const openedThread = makeThread({ id: 'opened-work-thread', mode: 'work' })
    const migratedThread = {
      ...openedThread,
      sidebar_work_folder: '/opened/workspace',
      sidebar_pinned_at: '2026-01-01T00:00:00.000Z',
    }

    mockedReadPinnedThreadIds.mockReturnValue(new Set(['opened-work-thread']))
    mockedReadThreadWorkFolder.mockImplementation((threadId) => (
      threadId === 'opened-work-thread' ? '/opened/workspace' : null
    ))
    mockedUpdateThreadSidebarState.mockResolvedValue(migratedThread)

    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <ThreadListProvider>
            <UpsertProbe thread={openedThread} />
          </ThreadListProvider>
        </AuthProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(mockedUpdateThreadSidebarState).toHaveBeenCalledWith('token', 'opened-work-thread', {
      sidebar_work_folder: '/opened/workspace',
      sidebar_pinned: true,
    })

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
