import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChatPage } from '../components/ChatPage'
import { LocaleProvider } from '../contexts/LocaleContext'
import {
  listMessages,
  listThreadRuns,
} from '../api'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    listMessages: vi.fn(),
    listThreadRuns: vi.fn(),
    listRunEvents: vi.fn(),
    createMessage: vi.fn(),
    createRun: vi.fn(),
    cancelRun: vi.fn(),
    provideInput: vi.fn(),
    retryThread: vi.fn(),
    editMessage: vi.fn(),
    forkThread: vi.fn(),
    getThread: vi.fn(),
    createThreadShare: vi.fn(),
    uploadThreadAttachment: vi.fn(),
    starThread: vi.fn(),
    unstarThread: vi.fn(),
    updateThreadTitle: vi.fn(),
    deleteThread: vi.fn(),
    listStarredThreadIds: vi.fn().mockResolvedValue([]),
  }
})

vi.mock('../hooks/useSSE', () => ({
  useSSE: () => ({
    state: 'idle',
    events: [],
    connect: vi.fn(),
    disconnect: vi.fn(),
    reconnect: vi.fn(),
    clearEvents: vi.fn(),
  }),
}))

vi.mock('../runEventProcessing', () => ({
  applyCodeExecutionToolCall: vi.fn(),
  applyCodeExecutionToolResult: vi.fn(),
  buildMessageCodeExecutionsFromRunEvents: vi.fn(() => []),
  patchCodeExecutionList: vi.fn((items: unknown[]) => items),
  buildMessageThinkingFromRunEvents: vi.fn(() => null),
  findAssistantMessageForRun: vi.fn(() => null),
  selectFreshRunEvents: vi.fn(() => ({ fresh: [], nextProcessedCount: 0 })),
  shouldRefetchCompletedRunMessages: vi.fn(() => false),
  shouldReplayMessageCodeExecutions: vi.fn(() => false),
  applyBrowserToolCall: vi.fn(),
  applyBrowserToolResult: vi.fn(),
  buildMessageBrowserActionsFromRunEvents: vi.fn(() => []),
}))

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    addSearchThreadId: vi.fn(),
    isSearchThreadId: vi.fn(() => false),
    readMessageSources: vi.fn(() => null),
    writeMessageSources: vi.fn(),
    readMessageArtifacts: vi.fn(() => null),
    writeMessageArtifacts: vi.fn(),
    readMessageCodeExecutions: vi.fn(() => null),
    writeMessageCodeExecutions: vi.fn(),
    readMessageThinking: vi.fn(() => null),
    writeMessageThinking: vi.fn(),
    readMessageSearchSteps: vi.fn(() => null),
    writeMessageSearchSteps: vi.fn(),
    readMessageCopBlocks: vi.fn(() => null),
    writeMessageCopBlocks: vi.fn(),
    readMessageBrowserActions: vi.fn(() => null),
    writeMessageBrowserActions: vi.fn(),
    migrateMessageMetadata: vi.fn(),
  }
})

vi.mock('../components/ChatInput', () => ({
  ChatInput: () => <div>chat-input</div>,
}))

vi.mock('../components/MessageBubble', () => ({
  MessageBubble: ({ message }: { message: { content: string } }) => <div>{message.content}</div>,
  StreamingBubble: () => <div>streaming</div>,
}))

vi.mock('../components/ThinkingBlock', () => ({
  ThinkingBlock: () => <div />,
  CodeExecutionCard: () => <div />,
}))

vi.mock('../components/ShellExecutionBlock', () => ({
  ShellExecutionBlock: () => <div />,
}))

vi.mock('../components/SearchTimeline', () => ({
  SearchTimeline: () => <div />,
}))

vi.mock('../components/ShareModal', () => ({
  ShareModal: () => null,
}))

vi.mock('../components/ReportModal', () => ({
  ReportModal: () => null,
}))

vi.mock('../components/NotificationBell', () => ({
  NotificationBell: () => <div />,
}))

vi.mock('../components/SourcesPanel', () => ({
  SourcesPanel: () => null,
}))

vi.mock('../components/CodeExecutionPanel', () => ({
  CodeExecutionPanel: () => null,
}))

vi.mock('../components/DocumentPanel', () => ({
  DocumentPanel: () => null,
}))

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

function OutletShell({ context }: { context: Record<string, unknown> }) {
  return <Outlet context={context} />
}

describe('ChatPage loading state', () => {
  const mockedListMessages = vi.mocked(listMessages)
  const mockedListThreadRuns = vi.mocked(listThreadRuns)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    HTMLElement.prototype.scrollIntoView = vi.fn()
    mockedListMessages.mockResolvedValue([
      {
        id: 'msg-1',
        role: 'user',
        content: 'hello',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([])
  })

  afterEach(() => {
    vi.restoreAllMocks()
    HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('切换到线程页后应结束初始加载', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const outletContext = {
      accessToken: 'token',
      onLoggedOut: vi.fn(),
      onRunStarted: vi.fn(),
      onRunEnded: vi.fn(),
      onThreadCreated: vi.fn(),
      onThreadTitleUpdated: vi.fn(),
      refreshCredits: vi.fn(),
      onOpenNotifications: vi.fn(),
      notificationVersion: 0,
      creditsBalance: 0,
      isPrivateMode: false,
      onTogglePrivateMode: vi.fn(),
      privateThreadIds: new Set<string>(),
      onSetPendingIncognito: vi.fn(),
      onRightPanelChange: vi.fn(),
      threads: [],
      onThreadDeleted: vi.fn(),
    }

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MemoryRouter initialEntries={['/t/thread-1']}>
            <Routes>
              <Route element={<OutletShell context={outletContext} />}>
                <Route path="/t/:threadId" element={<ChatPage />} />
              </Route>
            </Routes>
          </MemoryRouter>
        </LocaleProvider>,
      )
    })

    await act(async () => {
      await flushMicrotasks()
    })

    expect(mockedListMessages).toHaveBeenCalledWith('token', 'thread-1')
    expect(mockedListThreadRuns).toHaveBeenCalledWith('token', 'thread-1', 1)
    expect(container.textContent).not.toContain('加载中...')
    expect(container.textContent).toContain('hello')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
