import { act, useEffect, useState, forwardRef, useImperativeHandle } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Outlet, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChatPage } from '../components/ChatPage'
import { ArtifactStreamBlock, extractPartialArtifactFields, extractPartialWidgetFields } from '../components/ArtifactStreamBlock'
import { WidgetBlock } from '../components/WidgetBlock'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthContextBridge, type AuthContextValue } from '../contexts/auth'
import { ThreadListContextBridge, type ThreadListContextValue } from '../contexts/thread-list'
import { AppUIContextBridge, type AppUIContextValue } from '../contexts/app-ui'
import { CreditsContextBridge, type CreditsContextValue } from '../contexts/credits'
import { useLatest } from '../hooks/useLatest'
import {
  listMessages,
  listRunEvents,
  listStarredThreadIds,
  listThreadRuns,
  getThread,
  createMessage,
  createRun,
  cancelRun,
  continueThread,
  retryThread,
} from '../api'
import {
  readMessageTerminalStatus,
  readMessageAssistantTurn,
  readMessageCodeExecutions,
  readSelectedModelFromStorage,
  readSelectedPersonaKeyFromStorage,
  readThreadReasoningMode,
  readThreadWorkFolder,
  writeMessageAssistantTurn,
  writeMessageCodeExecutions,
  writeMessageSearchSteps,
  writeMessageTerminalStatus,
  writeMessageWidgets,
  readThreadRunHandoff,
  writeThreadRunHandoff,
  clearThreadRunHandoff,
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

const chatInputDraftStore = vi.hoisted(() => new Map<string, string>())

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
    continueThread: vi.fn(),
    provideInput: vi.fn(),
    retryThread: vi.fn(),
    editMessage: vi.fn(),
    forkThread: vi.fn(),
    getThread: vi.fn(),
    createThreadShare: vi.fn(),
    uploadStagingAttachment: vi.fn(),
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
    readSelectedPersonaKeyFromStorage: vi.fn(() => 'default'),
    readSelectedModelFromStorage: vi.fn(() => null),
    readThreadWorkFolder: vi.fn(() => null),
    readThreadReasoningMode: vi.fn(() => 'off'),
    readMessageTerminalStatus: vi.fn(() => null),
    writeMessageTerminalStatus: vi.fn(),
    readMessageAssistantTurn: vi.fn(() => null),
    writeMessageAssistantTurn: vi.fn(),
    clearMessageAssistantTurn: vi.fn(),
    readThreadRunHandoff: vi.fn(() => null),
    writeThreadRunHandoff: vi.fn(),
    clearThreadRunHandoff: vi.fn(),
    readMessageBrowserActions: vi.fn(() => null),
    writeMessageBrowserActions: vi.fn(),
    migrateMessageMetadata: vi.fn(),
  }
})

vi.mock('../components/ChatInput', () => ({
  ChatInput: forwardRef(({
    onSubmit,
    isStreaming,
    canCancel,
    onCancel,
    cancelSubmitting,
    appMode,
    searchMode,
    workThreadId,
    draftOwnerKey,
  }: {
    onSubmit: (e: { preventDefault: () => void }, personaKey: string, modelOverride?: string) => void
    isStreaming?: boolean
    canCancel?: boolean
    onCancel?: () => void
    cancelSubmitting?: boolean
    appMode?: 'chat' | 'work'
    searchMode?: boolean
    workThreadId?: string
    draftOwnerKey?: string | null
  }, ref: React.Ref<{ clear: () => void; setValue: (v: string) => void; getValue: () => string }>) => {
    const draftKey = `${draftOwnerKey ?? 'global'}:${workThreadId ?? 'welcome'}:${appMode ?? 'chat'}:${searchMode ? 'search' : 'default'}`
    const [value, setValue] = useState(() => chatInputDraftStore.get(draftKey) ?? '')
    const valueRef = useLatest(value)
    useEffect(() => {
      setValue(chatInputDraftStore.get(draftKey) ?? '')
    }, [draftKey])
    useEffect(() => {
      if (value) {
        chatInputDraftStore.set(draftKey, value)
      } else {
        chatInputDraftStore.delete(draftKey)
      }
    }, [draftKey, value])
    useImperativeHandle(ref, () => ({
      clear: () => setValue(''),
      setValue: (v: string) => setValue(v),
      getValue: () => valueRef.current,
    }))
    return (
    <form onSubmit={(event) => onSubmit(event, 'default')}>
      <input
        aria-label="chat-input"
        value={value}
          onChange={(event) => setValue(event.target.value)}
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
    )
  }),
}))

vi.mock('../components/MessageBubble', () => ({
  MessageBubble: ({
    message,
    contentOverride,
    animateUserEnter,
  }: {
    message: { content: string }
    contentOverride?: string
    animateUserEnter?: boolean
  }) => (
    <div className={animateUserEnter ? 'user-prompt-bubble-enter' : undefined}>
      {contentOverride ?? message.content}
    </div>
  ),
}))

vi.mock('../components/ExecutionCard', () => ({
  ExecutionCard: () => <div />,
}))

vi.mock('../components/CopTimeline', () => ({
  CopTimeline: ({
    steps,
    codeExecutions,
    subAgents,
    fileOps,
    webFetches,
    genericTools,
    headerOverride,
    isComplete,
    live,
    preserveExpanded,
    copInlineTextRows,
    thinkingRows,
    assistantThinking,
    thinkingHint,
  }: {
    steps?: Array<{ id: string; label: string; status?: string }>
    codeExecutions?: Array<{ id: string; code: string }>
    subAgents?: Array<{ id: string; nickname?: string }>
    fileOps?: Array<{ id: string; label: string }>
    webFetches?: Array<{ id: string; url: string }>
    genericTools?: Array<{ id: string; label: string }>
    headerOverride?: string
    isComplete?: boolean
    live?: boolean
    preserveExpanded?: boolean
    copInlineTextRows?: Array<{ id: string; text: string }>
    thinkingRows?: Array<{ id: string; markdown: string }>
    assistantThinking?: { markdown: string }
    thinkingHint?: string
  }) => {
    const inlineEntries = copInlineTextRows?.map((row) => `cop-inline:${row.text}`) ?? []
    const thinkingEntries = thinkingRows?.map((row) => `thinking:${row.markdown}`) ?? []
    const entries = [
      ...(steps?.map((step) => step.label) ?? []),
      ...(codeExecutions?.map((item) => item.code) ?? []),
      ...(subAgents?.map((item) => item.nickname ?? item.id) ?? []),
      ...(fileOps?.map((item) => item.label) ?? []),
      ...(webFetches?.map((item) => item.url) ?? []),
      ...(genericTools?.map((item) => item.label) ?? []),
    ]
    const hasThinking = (thinkingRows?.length ?? 0) > 0 || !!assistantThinking
    const mixedWithThinking = hasThinking && entries.length > 0
    const autoHeader =
      headerOverride ??
      (entries.length > 0
        ? (isComplete ? `${entries.length} steps completed` : 'In process')
        : hasThinking
          ? (isComplete ? 'Thought' : 'Thinking')
          : thinkingHint
            ? `${thinkingHint}...`
        : undefined)

    return (
    <div data-preserve-expanded={preserveExpanded ? 'true' : 'false'} data-live={live ? 'true' : 'false'}>
      {autoHeader ? <span>{autoHeader}</span> : null}
      {mixedWithThinking ? <span>thought-summary</span> : null}
      {steps?.map((step) => (
        <span key={step.id}>{step.label}</span>
      ))}
      {assistantThinking ? <span>{`assistant-thinking:${assistantThinking.markdown}`}</span> : null}
      {!mixedWithThinking && thinkingEntries.map((entry, index) => (
        <span key={`${entry}-${index}`}>{entry}</span>
      ))}
      {inlineEntries.map((entry, index) => (
        <span key={`${entry}-${index}`}>{entry}</span>
      ))}
      {codeExecutions?.map((item) => (
        <span key={item.id}>{item.code}</span>
      ))}
      {subAgents?.map((item) => (
        <span key={item.id}>{item.nickname ?? item.id}</span>
      ))}
      {fileOps?.map((item) => (
        <span key={item.id}>{item.label}</span>
      ))}
      {webFetches?.map((item) => (
        <span key={item.id}>{item.url}</span>
      ))}
      {genericTools?.map((item) => (
        <span key={item.id}>{item.label}</span>
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

function flushAnimationFrame(): Promise<void> {
  return new Promise((resolve) => {
    requestAnimationFrame(() => resolve())
  })
}

async function flushAnimationFrames(count: number): Promise<void> {
  for (let i = 0; i < count; i += 1) {
    await flushAnimationFrame()
  }
}

function installReducedMotionMatchMedia() {
  const previous = window.matchMedia
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn((query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)',
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => false),
    })),
  })
  return () => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: previous,
    })
  }
}

function installDefaultMotionMatchMedia() {
  const previous = window.matchMedia
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => false),
    })),
  })
  return () => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: previous,
    })
  }
}

function countMatches(text: string, needle: string): number {
  return text.split(needle).length - 1
}

type LegacyOutletContext = {
  accessToken: string
  onLoggedOut: () => void
  onRunStarted: (threadId: string) => void
  onRunEnded: (threadId: string) => void
  onThreadCreated: (thread: ThreadListContextValue['threads'][number]) => void
  onThreadTitleUpdated: (threadId: string, title: string) => void
  refreshCredits: () => void
  onOpenNotifications: () => void
  notificationVersion: number
  creditsBalance: number
  isPrivateMode: boolean
  onTogglePrivateMode: () => void
  privateThreadIds: Set<string>
  onSetPendingIncognito: (value: boolean) => void
  onRightPanelChange: (open: boolean) => void
  threads: ThreadListContextValue['threads']
  onThreadDeleted: (threadId: string) => void
}

function OutletShell({ context }: { context: LegacyOutletContext }) {
  const navigate = useNavigate()

  const authValue: AuthContextValue = {
    me: null,
    meLoaded: true,
    accessToken: context.accessToken,
    logout: async () => { context.onLoggedOut() },
    updateMe: () => {},
  }

  const threadListValue: ThreadListContextValue = {
    threads: context.threads,
    runningThreadIds: new Set(),
    privateThreadIds: context.privateThreadIds,
    isPrivateMode: context.isPrivateMode,
    pendingIncognitoMode: false,
    addThread: context.onThreadCreated,
    removeThread: context.onThreadDeleted,
    updateTitle: context.onThreadTitleUpdated,
    markRunning: context.onRunStarted,
    markIdle: context.onRunEnded,
    togglePrivateMode: context.onTogglePrivateMode,
    setPendingIncognito: context.onSetPendingIncognito,
    getFilteredThreads: () => context.threads,
  }

  const appUIValue: AppUIContextValue = {
    sidebarCollapsed: false,
    sidebarHiddenByWidth: false,
    rightPanelOpen: false,
    isSearchMode: false,
    searchOverlayOpen: false,
    settingsOpen: false,
    settingsInitialTab: 'account',
    desktopSettingsSection: 'general',
    notificationsOpen: false,
    notificationVersion: context.notificationVersion,
    appMode: 'chat',
    availableAppModes: ['chat'],
    pendingSkillPrompt: null,
    toggleSidebar: () => {},
    setRightPanelOpen: context.onRightPanelChange,
    enterSearchMode: () => {},
    exitSearchMode: () => {},
    openSearchOverlay: () => {},
    closeSearchOverlay: () => {},
    openSettings: () => {},
    closeSettings: () => {},
    openNotifications: context.onOpenNotifications,
    closeNotifications: () => {},
    markNotificationRead: () => {},
    setAppMode: () => navigate('/'),
    queueSkillPrompt: () => {},
    consumeSkillPrompt: () => {},
    setTitleBarIncognitoClick: () => {},
    triggerTitleBarIncognitoClick: (fallback) => fallback(),
  }

  const creditsValue: CreditsContextValue = {
    creditsBalance: context.creditsBalance,
    refreshCredits: context.refreshCredits,
    setCreditsBalance: () => {},
  }

  return (
    <AuthContextBridge value={authValue}>
      <ThreadListContextBridge value={threadListValue}>
        <AppUIContextBridge value={appUIValue}>
          <CreditsContextBridge value={creditsValue}>
            <Outlet />
          </CreditsContextBridge>
        </AppUIContextBridge>
      </ThreadListContextBridge>
    </AuthContextBridge>
  )
}

function NavigateButton({ to, label }: { to: string; label: string }) {
  const navigate = useNavigate()
  return (
    <button type="button" onClick={() => navigate(to)}>
      {label}
    </button>
  )
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
  const mockedContinueThread = vi.mocked(continueThread)
  const mockedRetryThread = vi.mocked(retryThread)
  const mockedWriteMessageAssistantTurn = vi.mocked(writeMessageAssistantTurn)
  const mockedWriteMessageCodeExecutions = vi.mocked(writeMessageCodeExecutions)
  const mockedWriteMessageSearchSteps = vi.mocked(writeMessageSearchSteps)
  const mockedReadMessageTerminalStatus = vi.mocked(readMessageTerminalStatus)
  const mockedReadMessageAssistantTurn = vi.mocked(readMessageAssistantTurn)
  const mockedReadMessageCodeExecutions = vi.mocked(readMessageCodeExecutions)
  const mockedReadSelectedPersonaKeyFromStorage = vi.mocked(readSelectedPersonaKeyFromStorage)
  const mockedReadSelectedModelFromStorage = vi.mocked(readSelectedModelFromStorage)
  const mockedReadThreadWorkFolder = vi.mocked(readThreadWorkFolder)
  const mockedReadThreadThinkingEnabled = vi.mocked(readThreadReasoningMode)
  const mockedWriteMessageTerminalStatus = vi.mocked(writeMessageTerminalStatus)
  const mockedWriteMessageWidgets = vi.mocked(writeMessageWidgets)
  const mockedReadThreadRunHandoff = vi.mocked(readThreadRunHandoff)
  const mockedWriteThreadRunHandoff = vi.mocked(writeThreadRunHandoff)
  const mockedClearThreadRunHandoff = vi.mocked(clearThreadRunHandoff)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView

  beforeEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
    chatInputDraftStore.clear()
    mockedReadMessageAssistantTurn.mockReturnValue(null)
    mockedReadMessageTerminalStatus.mockReturnValue(null)
    mockedReadMessageCodeExecutions.mockReturnValue(null)
    mockedReadSelectedPersonaKeyFromStorage.mockReturnValue('default')
    mockedReadSelectedModelFromStorage.mockReturnValue(null)
    mockedReadThreadWorkFolder.mockReturnValue(null)
    mockedReadThreadThinkingEnabled.mockReturnValue('off')
    mockedReadThreadRunHandoff.mockReturnValue(null)
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
    mockedContinueThread.mockResolvedValue({ run_id: 'run-continued', trace_id: 'trace-continue' })
    mockedRetryThread.mockResolvedValue({ run_id: 'run-retried', trace_id: 'trace-retry' })
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

  it('welcome 首条消息跳转时不应先显示骨架屏', async () => {
    let releaseMessages: ((value: Awaited<ReturnType<typeof listMessages>>) => void) | null = null
    mockedListMessages.mockImplementationOnce(() => new Promise((resolve) => {
      releaseMessages = resolve
    }))

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
    const welcomeMessage = {
      id: 'msg-1',
      role: 'user' as const,
      content: 'hello',
      account_id: 'acc-1',
      thread_id: 'thread-1',
      created_by_user_id: 'user-1',
      created_at: '2026-03-10T00:00:00Z',
    }

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MemoryRouter initialEntries={[{
            pathname: '/t/thread-1',
            state: {
              initialRunId: 'run-1',
              userEnterMessageId: 'msg-1',
              welcomeUserMessage: welcomeMessage,
            },
          }]}>
            <Routes>
              <Route element={<OutletShell context={outletContext} />}>
                <Route path="/t/:threadId" element={<ChatPage />} />
              </Route>
            </Routes>
          </MemoryRouter>
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('hello')
    expect(container.querySelector('.user-prompt-bubble-enter')).toBeTruthy()

    await act(async () => {
      releaseMessages?.([welcomeMessage])
      await flushMicrotasks()
    })

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

    expect(container.textContent).toContain('hello')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('重新进入 thread 时若最新 run 为 failed 应恢复失败原因', async () => {
    vi.mocked(listMessages).mockResolvedValue([
      { id: 'm1', role: 'assistant', content: 'bad output', created_at: '', run_id: 'r1' },
    ] as never)
    vi.mocked(listThreadRuns).mockResolvedValue([
      {
        run_id: 'r1',
        status: 'failed',
        created_at: '',
      },
    ] as never)
    vi.mocked(listRunEvents).mockResolvedValue([
      {
        event_id: 'e1',
        run_id: 'r1',
        seq: 1,
        ts: '',
        type: 'run.failed',
        data: {
          message: 'upstream exploded',
          error_class: 'provider.non_retryable',
          details: { status: 500 },
        },
      },
    ] as never)

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

    expect(container.textContent).toContain('出了点问题，本次运行未能完成')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('重新进入 thread 时若 failed run 尚未落库 assistant 消息也应恢复 handoff 并显示继续', async () => {
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
        run_id: 'run-failed-handoff',
        status: 'failed',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListRunEvents.mockResolvedValue([
      {
        event_id: 'evt-1',
        run_id: 'run-failed-handoff',
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
        run_id: 'run-failed-handoff',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.failed',
        data: {
          message: 'upstream exploded',
          error_class: 'provider.non_retryable',
        },
      },
    ] as never)

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

    expect(container.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()
    expect(container.textContent).toContain('先想一下')
    expect(container.querySelector('.failed-run-retry-button')?.textContent).toBe('继续')

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
      await flushAnimationFrames(12)
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

  it('切换 thread 时应按线程隔离草稿，并在切回时恢复', async () => {
    mockedListMessages.mockImplementation(async (_accessToken, threadId) => [
      {
        id: `msg-${threadId}`,
        role: 'user',
        content: `hello ${threadId}`,
        account_id: 'acc-1',
        thread_id: threadId,
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    mockedGetThread.mockImplementation(async (_accessToken, threadId) => ({
      id: threadId,
      title: `thread ${threadId}`,
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
    }))

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
              <Route
                element={(
                  <>
                    <NavigateButton to="/t/thread-1" label="go-thread-1" />
                    <NavigateButton to="/t/thread-2" label="go-thread-2" />
                    <OutletShell context={outletContext} />
                  </>
                )}
              >
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

    const input = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    const navigateButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'go-thread-2',
    )
    if (!input || !navigateButton) {
      throw new Error('thread navigation controls not rendered')
    }

    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(input, 'stale draft')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(input.value).toBe('stale draft')

    await act(async () => {
      navigateButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    const nextInput = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    expect(nextInput?.value).toBe('')

    const backButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'go-thread-1',
    )
    if (!backButton) {
      throw new Error('thread back navigation control not rendered')
    }

    await act(async () => {
      backButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    const restoredInput = container.querySelector('input[aria-label="chat-input"]') as HTMLInputElement | null
    expect(restoredInput?.value).toBe('stale draft')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('发送后在首个 SSE 事件前不应暴露 raw thinking 标签', async () => {
    const mathRandomSpy = vi.spyOn(Math, 'random').mockReturnValue(0)
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
      await flushAnimationFrames(12)
    })

    const text = container.textContent ?? ''
    expect(text).not.toContain('assistant-thinking:')
    expect(text).not.toContain('Think')
    expect(mockedCreateMessage).toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
    mathRandomSpy.mockRestore()
  })

  it('首个 SSE 为正文时应保持 pending thinking 的清爽过渡', async () => {
    const mathRandomSpy = vi.spyOn(Math, 'random').mockReturnValue(0)
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
      await flushAnimationFrames(12)
    })

    const firstText = container.textContent ?? ''
    expect(firstText).not.toContain('assistant-thinking:')
    expect(firstText).not.toContain('Think')

    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-created',
        type: 'message.delta',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        data: {
          role: 'assistant',
          content_delta: '我先看一下代码路径',
        },
      },
    ]
    sseMock.lastSeq = 1

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = container.textContent ?? ''
    expect(text).not.toContain('assistant-thinking:')
    expect(text).not.toContain('Finding the right words')
    expect(text).not.toContain('Thinking')

    act(() => {
      root.unmount()
    })
    container.remove()
    mathRandomSpy.mockRestore()
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

  it('加载到 cancelling run 时应继续保持 streaming 状态', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancelling',
        status: 'cancelling',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onRunStarted = vi.fn()
    const onRunEnded = vi.fn()
    const outletContext = {
      accessToken: 'token',
      onLoggedOut: vi.fn(),
      onRunStarted,
      onRunEnded,
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

    expect(container.textContent).toContain('streaming')
    expect(onRunStarted).toHaveBeenCalledWith('thread-1')
    expect(onRunEnded).not.toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('cancelled 且没有 assistant 消息时不应展示 retry 按钮', async () => {
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancelled',
        status: 'cancelled',
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

    expect(container.querySelector('.failed-run-retry-button')).toBeNull()
    expect(mockedRetryThread).not.toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('cancelled 且有 assistant 消息时应显示继续并调用 continue 接口', async () => {
    const mathRandomSpy = vi.spyOn(Math, 'random').mockReturnValue(0)
    mockedReadMessageTerminalStatus.mockReturnValue('cancelled')
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
        content: '半截输出',
        run_id: 'run-cancelled-output',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancelled-output',
        status: 'cancelled',
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

    const actionButton = container.querySelector('.failed-run-retry-button')
    expect(actionButton?.textContent).toBe('继续')

    await act(async () => {
      actionButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('Finding the right words')
    expect(mockedContinueThread).toHaveBeenCalledWith('token', 'thread-1', 'run-cancelled-output')
    expect(mockedRetryThread).not.toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
    mathRandomSpy.mockRestore()
  })

  it('failed 且有可恢复输出时应显示继续并调用 continue 接口', async () => {
    const mathRandomSpy = vi.spyOn(Math, 'random').mockReturnValue(0)
    mockedReadMessageTerminalStatus.mockReturnValue('failed')
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
        content: '半截输出',
        run_id: 'run-failed-output',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedReadSelectedPersonaKeyFromStorage.mockReturnValue('search')
    mockedReadSelectedModelFromStorage.mockReturnValue('openai^gpt-5')
    mockedReadThreadWorkFolder.mockReturnValue('/workspace/demo')
    mockedReadThreadThinkingEnabled.mockReturnValue('high')

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

    const actionButton = container.querySelector('.failed-run-retry-button')
    expect(actionButton?.textContent).toBe('继续')

    await act(async () => {
      actionButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('Finding the right words')
    expect(mockedContinueThread).toHaveBeenCalledWith(
      'token',
      'thread-1',
      'run-failed-output',
    )
    expect(mockedRetryThread).not.toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()
    mathRandomSpy.mockRestore()
  })

  it('failed 且无可恢复输出时重试应带上当前选择的 model', async () => {
    const mathRandomSpy = vi.spyOn(Math, 'random').mockReturnValue(0)
    mockedReadMessageTerminalStatus.mockReturnValue('failed')
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
        content: '',
        run_id: 'run-failed-empty',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedReadSelectedPersonaKeyFromStorage.mockReturnValue('search')
    mockedReadSelectedModelFromStorage.mockReturnValue('openai^gpt-5')
    mockedReadThreadWorkFolder.mockReturnValue('/workspace/demo')
    mockedReadThreadThinkingEnabled.mockReturnValue('high')

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

    const actionButton = container.querySelector('.failed-run-retry-button')
    expect(actionButton?.textContent).toBe('重试')

    await act(async () => {
      actionButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(mockedRetryThread).toHaveBeenCalledWith(
      'token',
      'thread-1',
      'openai^gpt-5',
    )

    act(() => {
      root.unmount()
    })
    container.remove()
    mathRandomSpy.mockRestore()
  })

  it('run.completed 后应把显示权交回历史消息，同时保留完成态的折叠结构', async () => {
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
    expect(countMatches(text, '1 steps completed')).toBe(2)
    expect(container.querySelector('[data-preserve-expanded="true"]')).toBeNull()
    expect(text.indexOf('我要先')).toBeGreaterThanOrEqual(0)
    expect(scrollIntoViewMock.mock.calls.some(([opts]) => (opts as { behavior?: string } | undefined)?.behavior === 'smooth')).toBe(false)
    expect(scrollIntoViewMock.mock.calls.some(([opts]) => (opts as { behavior?: string } | undefined)?.behavior === 'instant')).toBe(false)
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
        type: 'tool.call',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          arguments: {
            query: 'Claude Desktop 更新',
          },
        },
      },
      {
        event_id: 'evt-9',
        run_id: 'run-cancel-closed',
        seq: 9,
        ts: '2026-03-10T00:00:01Z',
        type: 'tool.result',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          result: {
            results: [
              {
                title: 'Release notes',
                url: 'https://claude.ai/release-notes',
              },
            ],
          },
        },
      },
      {
        event_id: 'evt-10',
        run_id: 'run-cancel-closed',
        seq: 10,
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
    expect(text).toContain('Thought')
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
    expect(mockedWriteMessageSearchSteps).toHaveBeenCalledWith(
      'msg-2',
      expect.arrayContaining([
        expect.objectContaining({
          id: 'search-1',
          kind: 'searching',
          status: 'done',
          queries: ['Claude Desktop 更新'],
          sources: [{ title: 'Release notes', url: 'https://claude.ai/release-notes' }],
        }),
      ]),
    )
    expect(mockedWriteMessageTerminalStatus).toHaveBeenCalledWith('msg-2', 'cancelled')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('cancel 后在消息刷新前重开 thread 仍应恢复 handoff 与重试入口', async () => {
    let resolveSecondMessages: ((value: Awaited<ReturnType<typeof listMessages>>) => void) | null = null
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
      .mockImplementationOnce(() => new Promise((resolve) => {
        resolveSecondMessages = resolve
      }))
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

    mockedListThreadRuns
      .mockResolvedValueOnce([
        {
          run_id: 'run-cancel-restore',
          status: 'running',
          created_at: '2026-03-10T00:00:00Z',
        },
      ])
      .mockResolvedValueOnce([])

    const handoff: NonNullable<ReturnType<typeof readThreadRunHandoff>> = {
      runId: 'run-cancel-restore',
      status: 'cancelled' as const,
      assistantTurn: {
        segments: [
          {
            type: 'cop' as const,
            title: null,
            items: [{ kind: 'thinking' as const, content: '先想一下', seq: 1 }],
          },
          {
            type: 'text' as const,
            content: '半截回复',
          },
        ],
      },
      sources: [],
      artifacts: [],
      widgets: [],
      codeExecutions: [],
      browserActions: [],
      subAgents: [],
      fileOps: [],
      webFetches: [],
      searchSteps: [],
    }

    mockedReadThreadRunHandoff
      .mockReturnValueOnce(null)
      .mockImplementation((threadId: string) => (
        threadId === 'thread-1' ? handoff : null
      ))

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

    const firstContainer = document.createElement('div')
    document.body.appendChild(firstContainer)
    const firstRoot = createRoot(firstContainer)

    await act(async () => {
      firstRoot.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    sseMock.state = 'connected'
    sseMock.events = [
      {
        event_id: 'evt-1',
        run_id: 'run-cancel-restore',
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
        run_id: 'run-cancel-restore',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '半截回复',
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-cancel-restore',
        seq: 3,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.cancelled',
        data: {},
      },
    ]

    await act(async () => {
      firstRoot.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedWriteThreadRunHandoff).toHaveBeenCalledWith('thread-1', expect.objectContaining({
      runId: 'run-cancel-restore',
      status: 'cancelled',
    }))

    act(() => {
      firstRoot.unmount()
    })
    firstContainer.remove()

    const secondContainer = document.createElement('div')
    document.body.appendChild(secondContainer)
    const secondRoot = createRoot(secondContainer)

    sseMock.state = 'idle'
    sseMock.events = []

    await act(async () => {
      secondRoot.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    const text = secondContainer.textContent ?? ''
    expect(secondContainer.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()
    expect(text).toContain('半截回复')
    expect(text).toMatch(/Stopped|已停止|运行已取消|Run was cancelled/)

    if (!resolveSecondMessages) {
      throw new Error('second listMessages resolver missing')
    }
    const resolvePendingMessages: (value: Awaited<ReturnType<typeof listMessages>>) => void = resolveSecondMessages
    resolvePendingMessages([
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
        content: '半截回复',
        run_id: 'run-cancel-restore',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])

    await act(async () => {
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedWriteMessageTerminalStatus).toHaveBeenCalledWith('msg-2', 'cancelled')
    expect(mockedClearThreadRunHandoff).toHaveBeenCalledWith('thread-1')

    act(() => {
      secondRoot.unmount()
    })
    secondContainer.remove()
  })

  it('run.cancelled 落入历史消息后不应默认保留展开态', async () => {
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
          run_id: 'run-cancel-history-collapsed',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancel-history-collapsed',
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
        run_id: 'run-cancel-history-collapsed',
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
        run_id: 'run-cancel-history-collapsed',
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
    expect(container.querySelector('[data-preserve-expanded="true"]')).not.toBeNull()

    sseMock.state = 'idle'
    sseMock.events = []

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(container.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()
    const preserved = Array.from(container.querySelectorAll('[data-preserve-expanded="true"]'))
    expect(preserved).toHaveLength(1)
    expect((preserved[0] as HTMLElement).closest('[data-testid="current-run-handoff"]')).not.toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.cancelled 且仅有 thinking 时应继续保留 think 正文', async () => {
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
          run_id: 'run-cancel-thinking-visible',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancel-thinking-visible',
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
        run_id: 'run-cancel-thinking-visible',
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
        run_id: 'run-cancel-thinking-visible',
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
    expect(text).toContain('thinking:先想一下')
    expect(text).not.toContain('thought-summary')

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

  it('run.cancelled 的 handoff 会在下一次 run 创建后立即清掉', async () => {
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
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(input, 'resume again')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })

    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedCreateMessage).toHaveBeenCalled()
    expect(container.querySelector('[data-testid="current-run-handoff"]')).toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.completed handoff 不应继续把最后一个 cop 当作 live', async () => {
    let resolveRefresh: ((value: Awaited<ReturnType<typeof listMessages>>) => void) | null = null
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
      .mockImplementationOnce(() => new Promise((resolve) => {
        resolveRefresh = resolve
      }))
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-completed-live-flag',
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
        run_id: 'run-completed-live-flag',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-1',
          arguments: { command: 'pwd' },
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-completed-live-flag',
        seq: 2,
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

    expect(container.querySelector('[data-testid="current-run-handoff"]')).not.toBeNull()
    expect(container.querySelector('[data-live="true"]')).toBeNull()
    expect(container.querySelector('[data-live="false"]')).not.toBeNull()

    await act(async () => {
      resolveRefresh?.([
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
          content: 'done',
          run_id: 'run-completed-live-flag',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
      await flushMicrotasks()
      await flushMicrotasks()
    })

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('run.completed 后应按顺序写入阶段文字与后续 cop，而不是把它们并到末尾', async () => {
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
          content: '阶段完成',
          run_id: 'run-segment-order',
          account_id: 'acc-1',
          thread_id: 'thread-1',
          created_by_user_id: 'user-1',
          created_at: '2026-03-10T00:00:01Z',
        },
      ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-segment-order',
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
        run_id: 'run-segment-order',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-before',
          arguments: { command: 'pwd' },
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-segment-order',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-before',
          result: { output: '/workspace' },
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-segment-order',
        seq: 3,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.segment.start',
        data: {
          segment_id: 'seg-visible',
          kind: 'planning_round',
          display: { mode: 'collapsed', label: 'Stage' },
        },
      },
      {
        event_id: 'evt-4',
        run_id: 'run-segment-order',
        seq: 4,
        ts: '2026-03-10T00:00:01Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '先整理数据，再生成图表。',
        },
      },
      {
        event_id: 'evt-5',
        run_id: 'run-segment-order',
        seq: 5,
        ts: '2026-03-10T00:00:02Z',
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call-after',
          arguments: {
            title: '图表',
            widget_code: '<div>chart</div>',
          },
        },
      },
      {
        event_id: 'evt-6',
        run_id: 'run-segment-order',
        seq: 6,
        ts: '2026-03-10T00:00:03Z',
        type: 'run.completed',
        data: {},
      },
    ]

    await act(async () => {
      root.render(renderTree())
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(mockedWriteMessageAssistantTurn).toHaveBeenCalledWith(
      'msg-2',
      expect.objectContaining({
        segments: [
          {
            type: 'cop',
            title: null,
            items: [
              expect.objectContaining({
                kind: 'call',
                call: expect.objectContaining({ toolCallId: 'call-before' }),
              }),
            ],
          },
          {
            type: 'text',
            content: '先整理数据，再生成图表。',
          },
          {
            type: 'cop',
            title: null,
            items: [
              expect.objectContaining({
                kind: 'call',
                call: expect.objectContaining({ toolCallId: 'call-after' }),
              }),
            ],
          },
        ],
      }),
    )

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

  it('show_widget 流式 delta 只有 loading_messages 时也应显示 loading 文案', async () => {
    const restoreMatchMedia = installReducedMotionMatchMedia()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <WidgetBlock
          html=""
          title="交互表"
          complete={false}
          loadingMessages={['正在生成表格', '正在填充数据']}
        />,
      )
      await flushAnimationFrames(12)
    })

    expect(container.textContent ?? '').toContain('正在生成表格')

    act(() => {
      root.unmount()
    })
    container.remove()
    restoreMatchMedia()
  })

  it('show_widget 完成后应显示 title 而不是 loading_messages', async () => {
    const restoreMatchMedia = installReducedMotionMatchMedia()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <WidgetBlock
          html="<div>ok</div>"
          title="交互表"
          complete
          loadingMessages={['正在生成表格']}
        />,
      )
      await flushAnimationFrame()
      await flushAnimationFrame()
    })

    const text = container.textContent ?? ''
    expect(text).toContain('交互表')
    expect(text).not.toContain('正在生成表格')

    act(() => {
      root.unmount()
    })
    container.remove()
    restoreMatchMedia()
  })

  it('show_widget loading message 应使用打字机，并在切换后从头重新打', async () => {
    const restoreMatchMedia = installDefaultMotionMatchMedia()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <WidgetBlock
          html=""
          title="交互表"
          complete={false}
          loadingMessages={['正在生成表格']}
        />,
      )
      await flushAnimationFrame()
    })

    expect(container.textContent ?? '').not.toBe('正在生成表格')

    await act(async () => {
      await flushAnimationFrames(16)
    })
    expect(container.textContent ?? '').toContain('正在生成表格')

    await act(async () => {
      root.render(
        <WidgetBlock
          html=""
          title="交互表"
          complete={false}
          loadingMessages={['正在填充数据']}
        />,
      )
      await flushAnimationFrame()
    })

    expect(container.textContent ?? '').not.toBe('正在填充数据')

    await act(async () => {
      await flushAnimationFrames(16)
    })
    expect(container.textContent ?? '').toContain('正在填充数据')

    act(() => {
      root.unmount()
    })
    container.remove()
    restoreMatchMedia()
  })

  it('紧凑 WidgetBlock 应收紧与 timeline 相邻时的上下间距', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <WidgetBlock
          html="<div>ok</div>"
          title="交互表"
          complete
          compact
        />,
      )
      await flushAnimationFrame()
      await flushAnimationFrame()
    })

    const block = container.firstElementChild as HTMLElement | null
    const header = block?.firstElementChild as HTMLElement | null
    expect(block?.style.margin).toBe('0px 0px 2px 0px')
    expect(header?.style.marginBottom).toBe('2px')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('紧凑 ArtifactStreamBlock 应收紧与 timeline 相邻时的上下间距', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <ArtifactStreamBlock
          compact
          entry={{
            toolCallIndex: 1,
            toolCallId: 'art-1',
            toolName: 'create_artifact',
            argumentsBuffer: '',
            title: '对比图表',
            display: 'inline',
            content: '<div>chart</div>',
            complete: false,
          }}
        />,
      )
      await flushAnimationFrame()
      await flushAnimationFrame()
    })

    const block = container.firstElementChild as HTMLElement | null
    const header = block?.firstElementChild as HTMLElement | null
    expect(block?.style.margin).toBe('0px 0px 2px 0px')
    expect(header?.style.marginBottom).toBe('2px')

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
    expect(text).toContain('Search completed')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('cancelled 历史消息缺 search steps 时也应通过 replay 恢复', async () => {
    mockedReadMessageAssistantTurn.mockImplementation((messageId: string) => (
      messageId === 'msg-2'
        ? {
            segments: [
              {
                type: 'cop',
                title: null,
                items: [
                  {
                    kind: 'assistant_text',
                    content: '让我看看官方 release notes。',
                    seq: 1,
                  },
                ],
              },
            ],
          }
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
        run_id: 'run-cancelled-replay-search',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-cancelled-replay-search',
        status: 'cancelled',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    mockedListRunEvents.mockResolvedValue([
      {
        event_id: 'evt-1',
        run_id: 'run-cancelled-replay-search',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          arguments: { query: 'Claude Desktop release notes' },
        },
      },
      {
        event_id: 'evt-2',
        run_id: 'run-cancelled-replay-search',
        seq: 2,
        ts: '2026-03-10T00:00:01Z',
        type: 'tool.result',
        data: {
          tool_name: 'web_search',
          tool_call_id: 'search-1',
          result: { results: [{ title: 'Release notes', url: 'https://claude.ai/release-notes' }] },
        },
      },
      {
        event_id: 'evt-3',
        run_id: 'run-cancelled-replay-search',
        seq: 3,
        ts: '2026-03-10T00:00:02Z',
        type: 'run.cancelled',
        data: {},
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

    expect(mockedWriteMessageSearchSteps).toHaveBeenCalledWith(
      'msg-2',
      expect.arrayContaining([
        expect.objectContaining({
          id: 'search-1',
          kind: 'searching',
          status: 'done',
          queries: ['Claude Desktop release notes'],
        }),
      ]),
    )

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('widgets、assistant turn、code exec 同时 miss 时也应仅靠 replay 恢复', async () => {
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
        run_id: 'run-replay-rich',
        account_id: 'acc-1',
        thread_id: 'thread-1',
        created_by_user_id: 'user-1',
        created_at: '2026-03-10T00:00:01Z',
      },
    ])
    mockedListThreadRuns.mockResolvedValue([
      {
        run_id: 'run-replay-rich',
        status: 'completed',
        created_at: '2026-03-10T00:00:00Z',
      },
    ])
    mockedListRunEvents.mockResolvedValue([
      {
        event_id: 'evt-rich-1',
        run_id: 'run-replay-rich',
        seq: 1,
        ts: '2026-03-10T00:00:00Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我先确认一下环境。',
        },
      },
      {
        event_id: 'evt-rich-2',
        run_id: 'run-replay-rich',
        seq: 2,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call-widget-rich',
          arguments: {
            title: '状态卡片',
            widget_code: '<div>ready</div>',
          },
        },
      },
      {
        event_id: 'evt-rich-3',
        run_id: 'run-replay-rich',
        seq: 3,
        ts: '2026-03-10T00:00:00Z',
        type: 'tool.call',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-exec-rich',
          arguments: {
            command: 'pwd',
          },
        },
      },
      {
        event_id: 'evt-rich-4',
        run_id: 'run-replay-rich',
        seq: 4,
        ts: '2026-03-10T00:00:01Z',
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call-exec-rich',
          result: {
            status: 'exited',
            stdout: '/workspace/demo',
            exit_code: 0,
          },
        },
      },
      {
        event_id: 'evt-rich-5',
        run_id: 'run-replay-rich',
        seq: 5,
        ts: '2026-03-10T00:00:01Z',
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '已经拿到结果。',
        },
      },
      {
        event_id: 'evt-rich-6',
        run_id: 'run-replay-rich',
        seq: 6,
        ts: '2026-03-10T00:00:01Z',
        type: 'run.completed',
        data: {},
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

    expect(mockedListRunEvents).toHaveBeenCalledWith('token', 'run-replay-rich', { follow: false })
    expect(mockedWriteMessageWidgets).toHaveBeenCalledWith('msg-2', [
      {
        id: 'call-widget-rich',
        title: '状态卡片',
        html: '<div>ready</div>',
      },
    ])
    expect(mockedWriteMessageCodeExecutions).toHaveBeenCalledWith('msg-2', [
      expect.objectContaining({
        id: 'call-exec-rich',
        code: 'pwd',
        output: '/workspace/demo',
        status: 'success',
      }),
    ])
    expect(mockedWriteMessageAssistantTurn).toHaveBeenCalledWith(
      'msg-2',
      expect.objectContaining({
        segments: expect.arrayContaining([
          expect.objectContaining({
            type: 'text',
          }),
        ]),
      }),
    )

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

describe('extractPartialWidgetFields', () => {
  it('应单独提取 show_widget 的 widget_code 与 loading_messages', () => {
    const result = extractPartialWidgetFields(
      '{"title":"stream_test","loading_messages":["first"],"widget_code":"<div>row 1\\nrow 2</div>","content":"ignore me"}',
    )

    expect(result.title).toBe('stream_test')
    expect(result.widgetCode).toBe('<div>row 1\nrow 2</div>')
    expect(result.loadingMessages).toEqual(['first'])
  })
})
