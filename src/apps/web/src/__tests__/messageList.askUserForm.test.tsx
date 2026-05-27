import { createRef, type ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { MessageList } from '../components/MessageList'
import type { AgentMessage } from '../agent-ui'

vi.mock('../components/MessageBubble', () => ({
  MessageBubble: ({ message, contentOverride }: { message: { content: string }; contentOverride?: string }) => (
    <div data-testid="message-bubble">{contentOverride ?? message.content}</div>
  ),
}))

vi.mock('../components/AskUserFormMessageCard', () => ({
  default: ({ content }: { content: { message: string } }) => (
    <div data-testid="ask-user-form-card">{content.message}</div>
  ),
}))

vi.mock('../components/WidgetBlock', () => ({
  WidgetBlock: () => null,
}))

vi.mock('../components/CopSegmentBlocks', () => ({
  CopSegmentBlocks: () => null,
}))

vi.mock('../components/TopLevelCopToolBlock', () => ({
  TopLevelCopToolBlock: () => null,
}))

vi.mock('../components/IncognitoDivider', () => ({
  IncognitoDivider: () => null,
}))

vi.mock('../components/MarkdownRenderer', () => ({
  MarkdownRenderer: ({ content }: { content: string }) => <div>{content}</div>,
}))

vi.mock('../components/WorkGroup', () => ({
  WorkGroup: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))

vi.mock('../components/cop-timeline/CopTimeline', () => ({
  CopTimeline: () => null,
}))

vi.mock('../components/messagebubble/AssistantMessage', () => ({
  AssistantActionBar: () => null,
}))

vi.mock('../contexts/chat-session', () => ({
  useChatSession: () => ({ threadId: 'thread-1', isSearchThread: false }),
}))

vi.mock('../contexts/run-lifecycle', () => ({
  useRunLifecycle: () => ({
    isStreaming: false,
    sending: false,
    terminalRunDisplayId: null,
    terminalRunHandoffStatus: null,
    terminalRunCoveredRunIds: [],
    activeRunId: 'run-form',
  }),
}))

vi.mock('../contexts/message-store', () => ({
  isLocalTerminalMessage: () => false,
  useMessageStore: () => ({
    messages: [],
    userEnterMessageId: null,
  }),
}))

vi.mock('../contexts/message-meta', () => ({
  useMessageMeta: () => ({
    getMeta: () => undefined,
  }),
}))

vi.mock('../contexts/stream', () => ({
  useStream: () => ({
    preserveLiveRunUi: false,
    liveAssistantTurn: null,
    topLevelCodeExecutions: [],
    topLevelSubAgents: [],
    topLevelFileOps: [],
    topLevelWebFetches: [],
    streamingArtifacts: [],
  }),
}))

vi.mock('../contexts/panels', () => ({
  useActiveCodeExecutionId: () => null,
  usePanelActions: () => ({
    closePanel: vi.fn(),
    openSourcePanel: vi.fn(),
    setShareState: vi.fn(),
  }),
  useShareModalState: () => ({
    sharingMessageId: null,
    sharedMessageId: null,
  }),
}))

vi.mock('../contexts/auth', () => ({
  useAuth: () => ({ accessToken: 'token' }),
}))

vi.mock('../contexts/thread-list', () => ({
  useThreadList: () => ({ privateThreadIds: new Set<string>() }),
}))

vi.mock('../contexts/LocaleContext', () => ({
  useLocale: () => ({
    t: {
      incognitoForkDivider: 'fork',
    },
  }),
}))

vi.mock('../lib/chat-helpers', () => ({
  turnHasCopThinkingItems: () => false,
  widgetToolCallIdsPlacedInTurn: () => new Set<string>(),
  historicWidgetsForCop: () => [],
}))

vi.mock('../components/chatSourceResolver', () => ({
  resolveMessageSourcesForRender: () => new Map(),
}))

vi.mock('../storage', () => ({
  readMessageTerminalStatus: () => null,
  readMessageWidgets: () => null,
}))

vi.mock('../api', () => ({
  createThreadShare: vi.fn(),
}))

vi.mock('@arkloop/shared/api', () => ({
  apiBaseUrl: () => '',
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useLocation: () => ({ state: null }),
  }
})

describe('MessageList ask_user_form', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('隐藏当前活跃 pending form 的历史消息行，避免 prompt 重复显示', async () => {
    const messages: AgentMessage[] = [
      {
        id: 'msg-form',
        role: 'assistant',
        content: '请补充项目地址',
        contentJson: {
          kind: 'ask_user_form',
          displayMode: 'form',
          requestId: 'req-1',
          runId: 'run-form',
          message: '请补充项目地址',
          schema: {
            properties: {
              project_url: { type: 'string', title: '项目地址' },
            },
            required: ['project_url'],
            _fieldOrder: ['project_url'],
          },
          status: 'pending',
          answers: null,
          submittedAt: null,
        },
        createdAt: '2026-03-10T00:00:01Z',
        parts: [],
        streamId: 'run-form',
      },
    ]

    await act(async () => {
      root.render(
        <MessageList
          ref={null}
          lastTurnRef={createRef<HTMLDivElement>()}
          lastUserPromptRef={createRef<HTMLDivElement>()}
          lastTurnStartIdx={0}
          handleRetryUserMessage={() => {}}
          handleEditMessage={() => {}}
          handleFork={async () => {}}
          handleArtifactAction={() => {}}
          handleAskUserFormSubmit={async () => {}}
          handleAskUserFormDismiss={async () => {}}
          openDocumentPanel={() => {}}
          openResourcePanel={() => {}}
          openCodePanel={() => {}}
          openAgentPanel={() => {}}
          showRunDetailButton={false}
          sourcePanelMessageId={null}
          setRunDetailPanelRunId={() => {}}
          currentRunCopHeaderOverride={() => undefined}
          clearUserEnterAnimation={() => {}}
          messagesOverride={messages}
        />,
      )
    })

    expect(container.querySelector('[data-testid="message-bubble"]')).toBeNull()
    expect(container.querySelector('[data-testid="ask-user-form-card"]')).toBeNull()
    expect(container.textContent).not.toContain('请补充项目地址')
  })
})
