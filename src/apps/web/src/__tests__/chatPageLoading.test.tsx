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
  createRun,
  cancelRun,
} from '../api'
import {
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
  MessageBubble: ({ message }: { message: { content: string } }) => <div>{message.content}</div>,
}))

vi.mock('../components/ExecutionCard', () => ({
  ExecutionCard: () => <div />,
}))

vi.mock('../components/CopTimeline', () => ({
  CopTimeline: ({ steps }: { steps?: Array<{ id: string; label: string; status?: string }> }) => (
    <div>
      {steps?.map((step) => (
        <span key={step.id}>{step.label}</span>
      ))}
    </div>
  ),
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
  const mockedListRunEvents = vi.mocked(listRunEvents)
  const mockedListStarredThreadIds = vi.mocked(listStarredThreadIds)
  const mockedListThreadRuns = vi.mocked(listThreadRuns)
  const mockedCreateRun = vi.mocked(createRun)
  const mockedCancelRun = vi.mocked(cancelRun)
  const mockedWriteMessageWidgets = vi.mocked(writeMessageWidgets)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView

  beforeEach(() => {
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
    mockedCreateRun.mockResolvedValue({ run_id: 'run-created', trace_id: 'trace-1' })
    mockedCancelRun.mockResolvedValue({ ok: true })
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

    expect(container.textContent).toContain('resume after cancel')

    await act(async () => {
      cancelButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(mockedCancelRun).toHaveBeenCalledWith('token', 'run-cancel')
    expect(container.textContent).toContain('streaming')
    expect(container.textContent).toContain('canceling')
    expect(container.textContent).toContain('resume after cancel')

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-cancelled',
        run_id: 'run-cancel',
        seq: 1,
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
    expect(container.textContent).toContain('已停止生成')

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
