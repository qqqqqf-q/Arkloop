import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChatPage } from '../components/ChatPage'
import { extractPartialArtifactFields } from '../components/ArtifactStreamBlock'
import { LocaleProvider } from '../contexts/LocaleContext'
import {
  listMessages,
  listRunEvents,
  listStarredThreadIds,
  listThreadRuns,
  getThread,
  createMessage,
  createRun,
  cancelRun,
} from '../api'
import {
  readMessageTerminalStatus,
  readMessageAssistantTurn,
  readMessageCodeExecutions,
  writeMessageAssistantTurn,
  writeMessageTerminalStatus,
  writeMessageWidgets,
} from '../storage'

const sseMock = vi.hoisted(() => ({
  state: 'idle',
  events: [] as unknown[],
  lastSeq: 0,
  error: null as Error | null,
  connect: vi.fn(),
  disconnect: vi.fn(),
  reconnect: vi.fn(),
  clearEvents: vi.fn(),
  reset: vi.fn(),
}))

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
  useSSE: () => sseMock,
}))

vi.mock('../runEventProcessing', async () => await vi.importActual<typeof import('../runEventProcessing')>('../runEventProcessing'))

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
    readMessageWidgets: vi.fn(() => null),
    writeMessageWidgets: vi.fn(),
    readMessageCodeExecutions: vi.fn(() => null),
    writeMessageCodeExecutions: vi.fn(),
    readMessageThinking: vi.fn(() => null),
    writeMessageThinking: vi.fn(),
    readMessageSearchSteps: vi.fn(() => null),
    writeMessageSearchSteps: vi.fn(),
    readMessageTerminalStatus: vi.fn(() => null),
    writeMessageTerminalStatus: vi.fn(),
    readMessageAssistantTurn: vi.fn(() => null),
    writeMessageAssistantTurn: vi.fn(),
    clearMessageAssistantTurn: vi.fn(),
    readMessageBrowserActions: vi.fn(() => null),
    writeMessageBrowserActions: vi.fn(),
    migrateMessageMetadata: vi.fn(),
  }
})

vi.mock('../components/ChatInput', () => ({
  ChatInput: ({
    value,
    onChange,
    onSubmit,
    isStreaming,
    canCancel,
    onCancel,
    cancelSubmitting,
  }: {
    value: string
    onChange: (value: string) => void
    onSubmit: (e: { preventDefault: () => void }, personaKey: string, modelOverride?: string) => void
    isStreaming?: boolean
    canCancel?: boolean
    onCancel?: () => void
    cancelSubmitting?: boolean
  }) => (
    <form onSubmit={(event) => onSubmit(event, 'default')}>
      <input
        aria-label="chat-input"
        value={value}
          onChange={(event) => onChange(event.target.value)}
        />
        <button type="submit">send</button>
        {isStreaming && canCancel && (
          <button type="button" aria-label="cancel-button" onClick={onCancel}>
            cancel
          </button>
        )}
        <div>{isStreaming ? 'streaming' : 'idle'}</div>
        <div>{cancelSubmitting ? 'canceling' : 'ready'}</div>
      </form>
    ),
}))

vi.mock('../components/MessageBubble', () => ({
  MessageBubble: ({ message, contentOverride }: { message: { content: string }; contentOverride?: string }) => (
    <div>{contentOverride ?? message.content}</div>
  ),
}))

vi.mock('../components/ExecutionCard', () => ({
  ExecutionCard: () => <div />,
}))

vi.mock('../components/CopTimeline', () => ({
  CopTimeline: ({
    steps,
    codeExecutions,
    headerOverride,
    isComplete,
    copInlineTextRows,
    thinkingRows,
    assistantThinking,
  }: {
    steps?: Array<{ id: string; label: string; status?: string }>
    codeExecutions?: Array<{ id: string; code: string }>
    headerOverride?: string
    isComplete?: boolean
    copInlineTextRows?: Array<{ id: string; text: string }>
    thinkingRows?: Array<{ id: string; markdown: string }>
    assistantThinking?: { markdown: string }
  }) => {
    const inlineEntries = copInlineTextRows?.map((row) => `cop-inline:${row.text}`) ?? []
    const thinkingEntries = thinkingRows?.map((row) => `thinking:${row.markdown}`) ?? []
    const entries = [
      ...(steps?.map((step) => step.label) ?? []),
      ...(codeExecutions?.map((item) => item.code) ?? []),
    ]
    const hasThinking = (thinkingRows?.length ?? 0) > 0 || !!assistantThinking
    const autoHeader =
      headerOverride ??
      (entries.length > 0
        ? (isComplete ? `${entries.length} steps completed` : 'In process')
        : hasThinking
          ? (isComplete ? 'Thought' : 'Thinking')
        : undefined)

    return (
    <div>
      {autoHeader ? <span>{autoHeader}</span> : null}
      {steps?.map((step) => (
        <span key={step.id}>{step.label}</span>
      ))}
      {assistantThinking ? <span>{`assistant-thinking:${assistantThinking.markdown}`}</span> : null}
      {thinkingEntries.map((entry, index) => (
        <span key={`${entry}-${index}`}>{entry}</span>
      ))}
      {inlineEntries.map((entry, index) => (
        <span key={`${entry}-${index}`}>{entry}</span>
      ))}
      {codeExecutions?.map((item) => (
        <span key={item.id}>{item.code}</span>
      ))}
    </div>
    )
  },
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

function countMatches(text: string, needle: string): number {
  return text.split(needle).length - 1
}

function OutletShell({ context }: { context: Record<string, unknown> }) {
  return <Outlet context={context} />
}

describe('ChatPage loading state', () => {
  const mockedListMessages = vi.mocked(listMessages)
  const mockedListRunEvents = vi.mocked(listRunEvents)
  const mockedListStarredThreadIds = vi.mocked(listStarredThreadIds)
  const mockedListThreadRuns = vi.mocked(listThreadRuns)
  const mockedGetThread = vi.mocked(getThread)
  const mockedCreateMessage = vi.mocked(createMessage)
  const mockedCreateRun = vi.mocked(createRun)
  const mockedCancelRun = vi.mocked(cancelRun)
  const mockedWriteMessageAssistantTurn = vi.mocked(writeMessageAssistantTurn)
const mockedReadMessageTerminalStatus = vi.mocked(readMessageTerminalStatus)
const mockedReadMessageAssistantTurn = vi.mocked(readMessageAssistantTurn)
const mockedReadMessageCodeExecutions = vi.mocked(readMessageCodeExecutions)
const mockedWriteMessageTerminalStatus = vi.mocked(writeMessageTerminalStatus)
  const mockedWriteMessageWidgets = vi.mocked(writeMessageWidgets)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView

  beforeEach(() => {
    mockedReadMessageAssistantTurn.mockReturnValue(null)
    mockedReadMessageTerminalStatus.mockReturnValue(null)
    mockedReadMessageCodeExecutions.mockReturnValue(null)
    vi.clearAllMocks()
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    HTMLElement.prototype.scrollIntoView = vi.fn()
    sseMock.state = 'idle'
    sseMock.events = []
    sseMock.lastSeq = 0
    sseMock.error = null
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
    mockedListRunEvents.mockResolvedValue([])
    mockedListStarredThreadIds.mockResolvedValue([])
    mockedListThreadRuns.mockResolvedValue([])
    mockedGetThread.mockResolvedValue({
      id: 'thread-1',
      title: 'hello',
      account_id: 'acc-1',
      created_by_user_id: 'user-1',
      mode: 'chat',
      project_id: 'proj-1',
      active_run_id: null,
      is_private: false,
      title_locked: false,
      hidden: false,
      created_at: '2026-03-10T00:00:00Z',
      updated_at: '2026-03-10T00:00:00Z',
    })
    mockedCreateMessage.mockResolvedValue({
      id: 'msg-created',
      role: 'user',
      content: 'created',
      account_id: 'acc-1',
      thread_id: 'thread-1',
      created_by_user_id: 'user-1',
      created_at: '2026-03-10T00:00:02Z',
    })
    mockedCreateRun.mockResolvedValue({ run_id: 'run-created', trace_id: 'trace-1' })
    mockedCancelRun.mockResolvedValue({ ok: true })
    mockedReadMessageTerminalStatus.mockReturnValue(null)
  })

  afterEach(() => {
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

  it('重新进入 thread 时若最新 run 为 interrupted 应显示中断提示', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-1',
        status: 'interrupted',
        created_at: '2026-03-10T00:00:05Z',
      },
    ])

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

    expect(container.textContent).toMatch(/Run interrupted|运行已中断/)

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.interrupted 后应把排队输入还原到输入框', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-1',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const input = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    const form = container.querySelector('form')
    if (!input || !form) {
      throw new Error('chat input mock not rendered')
    }

    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(input, 'continue from here')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-1',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          arguments: { query: 'resume me' },
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-1',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.interrupted',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const restoredInput = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    if (!restoredInput) {
      throw new Error('restored chat input not rendered')
    }
    expect(restoredInput.value).toBe('continue from here')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('manual cancel 应等待终态并保留排队输入', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancel',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    sseMock.state = 'connected'

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const input = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    const form = container.querySelector('form')
    const cancelButton = container.querySelector('button[aria-label="cancel-button"]')
    if (!input || !form || !cancelButton) {
      throw new Error('chat input controls not rendered')
    }

    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(input, 'resume after cancel')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-delta',
        run_id: 'run-cancel',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '正在查看 mirrorflow',
        },
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('resume after cancel')

    await act(async () => {
      cancelButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(mockedCancelRun).toHaveBeenCalledWith('token', 'run-cancel', 1)
    expect(container.textContent).toContain('streaming')
    expect(container.textContent).toContain('canceling')
    expect(container.textContent).toContain('resume after cancel')

    sseMock.events = [
      {
        event_id: 'evt-delta',
        run_id: 'run-cancel',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '正在查看 mirrorflow',
        },
      },
      {
        event_id: 'evt-cancelled',
        run_id: 'run-cancel',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.cancelled',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const restoredInput = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    if (!restoredInput) {
      throw new Error('restored input not rendered')
    }
    expect(restoredInput.value).toBe('resume after cancel')
    expect(container.textContent).not.toContain('已停止生成')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('发送后在首个 SSE 事件前应立即显示 pending thinking 外壳', async () => {
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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const input = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    const form = container.querySelector('form')
    if (!input || !form) {
      throw new Error('chat input mock not rendered')
    }

    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(input, 'look now')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('assistant-thinking:')
    expect(text).toContain('Thinking')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.cancelled 后会尝试刷新消息列表', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-1',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    mockedListMessages.mockClear()

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-cancelled',
        run_id: 'run-1',
        seq: 3,
        ts: '2026-03-10T00:00:02Z',
        type: 'run.cancelled',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedListMessages).toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.completed 后应把显示权交回历史消息，同时保留展开结构', async () => {
    mockedListMessages
      .mockResolvedValueOnce([
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
      .mockResolvedValueOnce([
        {
          id: 'msg-1',
          role: 'user',
          content: 'hello',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '我要先再继续',
          run_id: 'run-completed-structure',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-completed-structure',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onThreadTitleUpdated = vi.fn()
    const outletContext = {
      accessToken: 'token',
      onLoggedOut: vi.fn(),
      onRunStarted: vi.fn(),
      onRunEnded: vi.fn(),
      onThreadCreated: vi.fn(),
      onThreadTitleUpdated,
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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })
    const scrollIntoViewMock = vi.mocked(HTMLElement.prototype.scrollIntoView)
    scrollIntoViewMock.mockClear()

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-completed-structure',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我要先',
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-completed-structure',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-pwd',
          arguments: {
            command: 'pwd',
          },
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-completed-structure',
        seq: 3,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-pwd',
          result: {
            output: '/workspace',
          },
        },
      },
      {
        event_id: 'evt-4',
        run_id: 'run-completed-structure',
        seq: 4,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '再继续',
        },
      },
      {
        event_id: 'evt-5',
        run_id: 'run-completed-structure',
        seq: 5,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          arguments: {
            command: 'ls',
          },
        },
      },
      {
        event_id: 'evt-6',
        run_id: 'run-completed-structure',
        seq: 6,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          result: {
            output: 'a\nb',
          },
        },
      },
      {
        event_id: 'evt-7',
        run_id: 'run-completed-structure',
        seq: 7,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.completed',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('我要先')
    expect(text).toContain('再继续')
    expect(container.querySelector('[data-testid="current-run-handoff"]')).toBeNull()
    expect(countMatches(text, '1 steps completed')).toBe(1)
    expect(text.indexOf('我要先')).toBeGreaterThanOrEqual(0)
    expect(text.indexOf('pwd')).toBeGreaterThanOrEqual(0)
    expect(text.indexOf('再继续')).toBeGreaterThan(text.indexOf('pwd'))
    expect(scrollIntoViewMock.mock.calls.some(([opts]) => (opts as { behavior?: string } | undefined)?.behavior === 'smooth')).toBe(false)
    expect(scrollIntoViewMock.mock.calls.some(([opts]) => (opts as { behavior?: string } | undefined)?.behavior === 'instant')).toBe(true)
    expect(mockedGetThread).not.toHaveBeenCalled()

    sseMock.events = [
      ...sseMock.events,
      {
        event_id: 'evt-8',
        run_id: 'run-completed-structure',
        seq: 8,
        ts: '2026-03-10T00:00:02Z',
        type: 'thread.title.updated',
        data: {
          thread_id: 'thread-1',
          title: '查看文件夹内容',
        },
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
    })

    expect(onThreadTitleUpdated).toHaveBeenCalledWith('thread-1', '查看文件夹内容')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.interrupted 后当前 run 应继续保留 handoff 结构而不是落入 compact summary', async () => {
    mockedListMessages
      .mockResolvedValueOnce([
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
      .mockResolvedValueOnce([
        {
          id: 'msg-1',
          role: 'user',
          content: 'hello',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '我要先再继续',
          run_id: 'run-interrupt-structure',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-interrupt-structure',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-interrupt-structure',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我要先',
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-interrupt-structure',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-pwd',
          arguments: {
            command: 'pwd',
          },
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-interrupt-structure',
        seq: 3,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-pwd',
          result: {
            output: '/workspace',
          },
        },
      },
      {
        event_id: 'evt-4',
        run_id: 'run-interrupt-structure',
        seq: 4,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '再继续',
        },
      },
      {
        event_id: 'evt-5',
        run_id: 'run-interrupt-structure',
        seq: 5,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          arguments: {
            command: 'ls',
          },
        },
      },
      {
        event_id: 'evt-6',
        run_id: 'run-interrupt-structure',
        seq: 6,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          result: {
            output: 'a\nb',
          },
        },
      },
      {
        event_id: 'evt-7',
        run_id: 'run-interrupt-structure',
        seq: 7,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.interrupted',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('我要先')
    expect(text).toContain('再继续')
    expect(container.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()
    expect(text).not.toContain('2 steps completed')
    expect(text.indexOf('我要先')).toBeGreaterThanOrEqual(0)
    expect(text.indexOf('pwd')).toBeGreaterThan(text.indexOf('我要先'))
    expect(text.indexOf('再继续')).toBeGreaterThan(text.indexOf('pwd'))
    expect(text.indexOf('ls')).toBeGreaterThan(text.indexOf('再继续'))

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('assistant 前导文本应保持独立正文段，不再并入紧邻 exec cop', async () => {
    const runId = 'run-inline-intro'
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
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: runId,
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: runId,
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我来帮你看看这个文件夹的内容。',
        },
      },
      {
        event_id: 'evt-2',
        run_id: runId,
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          arguments: {
            command: 'ls -la ~/Documents/mirrorflow',
          },
        },
      },
      {
        event_id: 'evt-3',
        run_id: runId,
        seq: 3,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          result: {
            output: 'README.md',
          },
        },
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('我来帮你看看这个文件夹的内容。')
    expect(text).toContain('ls -la ~/Documents/mirrorflow')
    expect(text).not.toContain('cop-inline:我来帮你看看这个文件夹的内容。')
    expect(countMatches(text, '我来帮你看看这个文件夹的内容。')).toBe(1)
    expect(text.indexOf('我来帮你看看这个文件夹的内容。')).toBeLessThan(text.indexOf('ls -la ~/Documents/mirrorflow'))

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.cancelled 后应保留 handoff 的展开态与 thinking，并写入 assistant turn 持久化', async () => {
    mockedListMessages
      .mockResolvedValueOnce([
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
      .mockResolvedValueOnce([
        {
          id: 'msg-1',
          role: 'user',
          content: 'hello',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '我要先再继续',
          run_id: 'run-cancel-closed',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancel-closed',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-cancel-closed',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          channel: 'thinking',
          content_delta: '先想一下',
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-cancel-closed',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我要先',
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-cancel-closed',
        seq: 3,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-pwd',
          arguments: {
            command: 'pwd',
          },
        },
      },
      {
        event_id: 'evt-4',
        run_id: 'run-cancel-closed',
        seq: 4,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-pwd',
          result: {
            output: '/workspace',
          },
        },
      },
      {
        event_id: 'evt-5',
        run_id: 'run-cancel-closed',
        seq: 5,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '再继续',
        },
      },
      {
        event_id: 'evt-6',
        run_id: 'run-cancel-closed',
        seq: 6,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          arguments: {
            command: 'ls',
          },
        },
      },
      {
        event_id: 'evt-7',
        run_id: 'run-cancel-closed',
        seq: 7,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-ls',
          result: {
            output: 'a\\nb',
          },
        },
      },
      {
        event_id: 'evt-8',
        run_id: 'run-cancel-closed',
        seq: 8,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.cancelled',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(container.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()
    expect(text).toContain('thinking:先想一下')
    expect(text).toContain('我要先')
    expect(text).toContain('再继续')
    expect(text).not.toContain('In process')
    expect(mockedWriteMessageAssistantTurn).toHaveBeenCalledWith(
      'msg-2',
      expect.objectContaining({
        segments: expect.any(Array),
      }),
    )
    expect(mockedWriteMessageTerminalStatus).toHaveBeenCalledWith('msg-2', 'cancelled')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('刷新后历史 cancelled reply 仍保留 stopped 语义', async () => {
    mockedReadMessageTerminalStatus.mockImplementation((messageId: string) => (
      messageId === 'msg-2' ? 'cancelled' : null
    ))
    mockedReadMessageAssistantTurn.mockImplementation((messageId: string) => (
      messageId === 'msg-2'
        ? {
            segments: [
              {
                type: 'cop',
                title: null,
                items: [{ kind: 'thinking', content: '先想一下', seq: 1 }],
              },
            ],
          }
        : null
    ))
    mockedReadMessageCodeExecutions.mockImplementation((messageId: string) => (
      messageId === 'msg-2'
        ? [{ id: 'exec-1', language: 'shell', code: 'pwd', status: 'failed' }]
        : null
    ))
    mockedListMessages.mockResolvedValueOnce([
      {
        id: 'msg-1',
        role: 'user',
        content: 'hello',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:00Z',
      },
      {
        id: 'msg-2',
        role: 'assistant',
        content: '',
        run_id: 'run-cancel-persisted',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(container.textContent ?? '').toMatch(/Stopped|已停止/)

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.cancelled 且仅有 thinking 时，当前页标题应回落为 Thought 而不是继续卡在 Thinking', async () => {
    mockedListMessages
      .mockResolvedValueOnce([
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
      .mockResolvedValueOnce([
        {
          id: 'msg-1',
          role: 'user',
          content: 'hello',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '',
          run_id: 'run-cancel-thinking-only',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancel-thinking-only',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-cancel-thinking-only',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          channel: 'thinking',
          content_delta: '先想一下',
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-cancel-thinking-only',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.cancelled',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('Thought')
    expect(text).toContain('thinking:先想一下')
    expect(text).not.toContain('Thinkingthinking:先想一下')
    expect(text).not.toContain('Stoppedthinking:先想一下')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.cancelled 的 handoff 只会在下一次真正发送后整体收起', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancel-next',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    mockedCreateMessage.mockResolvedValueOnce({
      id: 'msg-next-user',
      role: 'user',
      content: 'resume after cancel',
      account_id: 'acc-1',
      thread_id: 'thread-1',
      created_by_user_id: 'user-1',
      created_at: '2026-03-10T00:00:03Z',
    })
    mockedCreateRun.mockResolvedValueOnce({ run_id: 'run-next', trace_id: 'trace-next' })
    sseMock.state = 'connected'

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const input = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    const form = container.querySelector('form')
    const cancelButton = container.querySelector('button[aria-label="cancel-button"]')
    if (!input || !form || !cancelButton) {
      throw new Error('chat input controls not rendered')
    }

    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(input, 'resume after cancel')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await flushMicrotasks()
    })

    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-cancel-next',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          channel: 'thinking',
          content_delta: '先想一下',
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-cancel-next',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.cancelled',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(container.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()

    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedCreateMessage).toHaveBeenCalled()
    expect(mockedCreateRun).toHaveBeenCalledWith('token', 'thread-1', 'default', undefined, undefined)
    expect(container.querySelector('[data-testid="current-run-handoff"]')).toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.completed 后应把 show_widget 写入消息缓存', async () => {
    mockedListMessages
      .mockResolvedValueOnce([
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
      .mockResolvedValueOnce([
        {
          id: 'msg-1',
          role: 'user',
          content: 'hello',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '图表已创建',
          run_id: 'run-1',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-1',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-1',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call.delta',
        data: {
          tool_call_index: 0,
          tool_call_id: 'call-widget',
          tool_name: 'show_widget',
          arguments_delta: '{"title":"销售图表","widget_code":"<div>图表</div>"}',
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-1',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call-widget',
          arguments: {
            title: '销售图表',
            widget_code: '<div>图表</div>',
          },
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-1',
        seq: 3,
        ts: '2026-03-10T00:00:00Z',
        type: 'run.completed',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedWriteMessageWidgets).toHaveBeenCalledWith('msg-2', [
      {
        id: 'call-widget',
        title: '销售图表',
        html: '<div>图表</div>',
      },
    ])

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('仅靠 replay 也能恢复 search steps', async () => {
    mockedListMessages.mockResolvedValueOnce([
      {
        id: 'msg-1',
        role: 'user',
        content: 'hello',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:00Z',
      },
      {
        id: 'msg-2',
        role: 'assistant',
        content: '',
        run_id: 'run-replay-search',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-replay-search',
        status: 'completed',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    mockedListRunEvents.mockResolvedValue([
      {
        event_id: 'evt-1',
        run_id: 'run-replay-search',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          arguments: { query: 'arkloop' },
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-replay-search',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'tool.result',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          result: { results: [{ title: 'Arkloop', url: 'https://arkloop.test' }] },
        },
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('Searching')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('没有 tool.call.delta 时也应在 run.completed 后写入 show_widget', async () => {
    mockedListMessages
      .mockResolvedValueOnce([
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
      .mockResolvedValueOnce([
        {
          id: 'msg-1',
          role: 'user',
          content: 'hello',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '图表已创建',
          run_id: 'run-plain-call',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-plain-call',
        status: 'running',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

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

    const renderTree = () => (
      <LocaleProvider>
        <MemoryRouter initialEntries={['/t/thread-1']}>
          <Routes>
            <Route element={<OutletShell context={outletContext} />}>
              <Route path="/t/:threadId" element={<ChatPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    )

    await act(async () => {
      root.render(renderTree())
    })
    await act(async () => {
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-plain-1',
        run_id: 'run-plain-call',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call-widget-plain',
          arguments: {
            title: '数学绘图',
            widget_code: '<div>plain call</div>',
          },
        },
      },
      {
        event_id: 'evt-plain-2',
        run_id: 'run-plain-call',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'run.completed',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedWriteMessageWidgets).toHaveBeenCalledWith('msg-2', [
      {
        id: 'call-widget-plain',
        title: '数学绘图',
        html: '<div>plain call</div>',
      },
    ])

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('加载 completed run 时应从 run events 回放 widgets', async () => {
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
      {
        id: 'msg-2',
        role: 'assistant',
        content: '图表已创建',
        run_id: 'run-2',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-2',
        status: 'completed',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListRunEvents.mockResolvedValue([
      {
        event_id: 'evt-1',
        run_id: 'run-2',
        seq: 1,
        ts: '2026-03-10T00:00:01Z',
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call-widget',
          arguments: {
            title: '系统架构图',
            widget_code: '<svg><text>ok</text></svg>',
          },
        },
      },
    ])

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
      await flushMicrotasks()
    })

    expect(mockedListRunEvents).toHaveBeenCalledWith('token', 'run-2', { follow: false })
    expect(mockedWriteMessageWidgets).toHaveBeenCalledWith('msg-2', [
      {
        id: 'call-widget',
        title: '系统架构图',
        html: '<svg><text>ok</text></svg>',
      },
    ])

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})

describe('extractPartialArtifactFields', () => {
  it('应解析带空格的 widget_code 增量并保留未闭合内容', () => {
    const result = extractPartialArtifactFields(`{
      "title": "interactive_neural_network",
      "widget_code": "<style>.node{opacity:.8}</style><div>streaming
    `)

    expect(result.title).toBe('interactive_neural_network')
    expect(result.content).toBe('<style>.node{opacity:.8}</style><div>streaming\n    ')
  })

  it('应在流式阶段正确解码转义字符', () => {
    const result = extractPartialArtifactFields('{"widget_code":"<div class=\\"chip\\">line 1\\nline 2<\\/div>"}')

    expect(result.content).toBe('<div class="chip">line 1\nline 2</div>')
  })

  it('应解析流式 loading_messages 已完整项并忽略未闭合字符串', () => {
    const partial = '{"loading_messages":["a","b'
    expect(extractPartialArtifactFields(partial).loadingMessages).toEqual(['a'])

    const partial2 = '{"loading_messages":["first", "sec'
    expect(extractPartialArtifactFields(partial2).loadingMessages).toEqual(['first'])
  })

  it('应解析完整 loading_messages 与转义', () => {
    const result = extractPartialArtifactFields(
      '{"loading_messages":["x","line \\"quote\\""],"widget_code":"<div />"}',
    )
    expect(result.loadingMessages).toEqual(['x', 'line "quote"'])
  })

  it('loading_messages 空数组应返回空数组', () => {
    expect(extractPartialArtifactFields('{"loading_messages":[]').loadingMessages).toEqual([])
  })
})
