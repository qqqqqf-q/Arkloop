export type AgentMessageRole = 'system' | 'user' | 'assistant'

export type AgentMessageAttachmentRef = {
  key: string
  filename: string
  mediaType: string
  size: number
}

export type AgentMessageContentPart =
  | { type: 'text'; text: string }
  | { type: 'image'; attachment: AgentMessageAttachmentRef }
  | { type: 'file'; attachment: AgentMessageAttachmentRef; extractedText: string }

export type AgentMessageContent = {
  parts: AgentMessageContentPart[]
}

export type AgentProviderMetadata = Record<string, unknown>
export type AgentUIDataTypes = Record<string, unknown>

export type AgentUITool = {
  input: unknown
  output: unknown | undefined
}

export type AgentUITools = Record<string, AgentUITool>

export type AgentTextUIPart = {
  type: 'text'
  text: string
  state?: 'streaming' | 'done'
  providerMetadata?: AgentProviderMetadata
}

export type AgentReasoningUIPart = {
  type: 'reasoning'
  text: string
  state?: 'streaming' | 'done'
  providerMetadata?: AgentProviderMetadata
}

export type AgentCustomContentUIPart = {
  type: 'custom'
  kind: `${string}.${string}`
  providerMetadata?: AgentProviderMetadata
}

export type AgentFileUIPart = {
  type: 'file'
  mediaType: string
  filename?: string
  url: string
  providerMetadata?: AgentProviderMetadata
}

export type AgentReasoningFileUIPart = {
  type: 'reasoning-file'
  mediaType: string
  url: string
  providerMetadata?: AgentProviderMetadata
}

export type AgentSourceUrlUIPart = {
  type: 'source-url'
  sourceId: string
  url: string
  title?: string
  providerMetadata?: AgentProviderMetadata
}

export type AgentSourceDocumentUIPart = {
  type: 'source-document'
  sourceId: string
  mediaType: string
  title: string
  filename?: string
  providerMetadata?: AgentProviderMetadata
}

export type AgentDataUIPart<DATA_TYPES extends AgentUIDataTypes = AgentUIDataTypes> = {
  [NAME in keyof DATA_TYPES & string]: {
    type: `data-${NAME}`
    id?: string
    data: DATA_TYPES[NAME]
  }
}[keyof DATA_TYPES & string]

export type AgentToolUIPart = {
  type: 'dynamic-tool'
  toolName: string
  toolCallId: string
  title?: string
  providerExecuted?: boolean
} & (
  | {
      state: 'input-streaming'
      input: unknown | undefined
      output?: never
      errorText?: never
      callProviderMetadata?: AgentProviderMetadata
      approval?: never
    }
  | {
      state: 'input-available'
      input: unknown
      output?: never
      errorText?: never
      callProviderMetadata?: AgentProviderMetadata
      approval?: never
    }
  | {
      state: 'approval-requested'
      input: unknown
      output?: never
      errorText?: never
      callProviderMetadata?: AgentProviderMetadata
      approval: {
        id: string
        approved?: never
        reason?: never
        isAutomatic?: boolean
      }
    }
  | {
      state: 'approval-responded'
      input: unknown
      output?: never
      errorText?: never
      callProviderMetadata?: AgentProviderMetadata
      approval: {
        id: string
        approved: boolean
        reason?: string
        isAutomatic?: boolean
      }
    }
  | {
      state: 'output-available'
      input: unknown
      output: unknown
      errorText?: never
      callProviderMetadata?: AgentProviderMetadata
      resultProviderMetadata?: AgentProviderMetadata
      preliminary?: boolean
      approval?: {
        id: string
        approved: true
        reason?: string
        isAutomatic?: boolean
      }
    }
  | {
      state: 'output-error'
      input: unknown | undefined
      rawInput?: unknown
      output?: never
      errorText: string
      callProviderMetadata?: AgentProviderMetadata
      resultProviderMetadata?: AgentProviderMetadata
      approval?: {
        id: string
        approved: true
        reason?: string
        isAutomatic?: boolean
      }
    }
  | {
      state: 'output-denied'
      input: unknown
      output?: never
      errorText?: never
      callProviderMetadata?: AgentProviderMetadata
      approval: {
        id: string
        approved: false
        reason?: string
        isAutomatic?: boolean
      }
    }
)

export type AgentUIMessagePart<DATA_TYPES extends AgentUIDataTypes = AgentUIDataTypes> =
  | AgentTextUIPart
  | AgentCustomContentUIPart
  | AgentReasoningUIPart
  | AgentFileUIPart
  | AgentReasoningFileUIPart
  | AgentSourceUrlUIPart
  | AgentSourceDocumentUIPart
  | AgentDataUIPart<DATA_TYPES>
  | AgentToolUIPart
  | { type: 'step-start' }

export type AgentUIMessage<
  METADATA = unknown,
  DATA_TYPES extends AgentUIDataTypes = AgentUIDataTypes,
> = {
  id: string
  role: AgentMessageRole
  metadata?: METADATA
  parts: AgentUIMessagePart<DATA_TYPES>[]
}

export type AgentMessageMetadata = {
  createdAt: string
  streamId?: string
  clientMessageId?: string
  deliveryStatus?: 'pending' | 'sent' | 'failed'
}

export type AgentMessage = AgentUIMessage<AgentMessageMetadata> & {
  content: string
  contentJson?: AgentMessageContent
  createdAt: string
  streamId?: string
}

export type AgentCreateMessageRequest = {
  content?: string
  contentJson?: AgentMessageContent
  clientMessageId?: string
}

export type AgentRun = {
  id: string
  traceId: string
}

export type AgentUIEventType =
  | 'assistant-delta'
  | 'tool-input-delta'
  | 'tool-call'
  | 'tool-result'
  | 'terminal-delta'
  | 'segment-start'
  | 'segment-end'
  | 'context-compact'
  | 'input-request'
  | 'run-completed'
  | 'run-failed'
  | 'run-cancelled'
  | 'run-interrupted'
  | 'security-block'
  | 'thread-title'
  | 'thread-collaboration'
  | 'todo-updated'
  | string

export type AgentAssistantDeltaData = {
  role?: string
  channel?: string
  delta: string
}

export type AgentToolInputDeltaData = {
  toolCallIndex?: number
  toolCallId?: string
  toolName?: string
  delta: string
}

export type AgentToolCallData = {
  toolCallId: string
  toolCallIndex?: number
  toolName: string
  input: unknown
  displayDescription?: string
  llmName?: string
}

export type AgentToolResultErrorData = {
  errorClass?: string
  message?: string
  code?: string
  details?: Record<string, unknown>
}

export type AgentToolResultData = {
  toolCallId: string
  toolName?: string
  output: unknown
  error?: AgentToolResultErrorData
}

export type AgentTerminalDeltaData = {
  processRef?: string
  chunk?: string
  stream: 'stdout' | 'stderr'
}

export type AgentSegmentDisplayData = {
  mode?: string
  label?: string
  queries?: string[]
}

export type AgentSegmentStartData = {
  segmentId: string
  kind: string
  display?: AgentSegmentDisplayData
}

export type AgentSegmentEndData = {
  segmentId: string
}

export type AgentContextCompactData = {
  op?: string
  phase?: string
  droppedPrefix?: number
}

export type AgentInputRequestData = {
  requestId?: string
  message?: string
  requestedSchema?: unknown
}

export type AgentSecurityBlockData = {
  message?: string
}

export type AgentThreadTitleData = {
  threadId?: string
  title?: string
}

export type AgentThreadCollaborationData = {
  threadId?: string
  collaborationMode?: string
  collaborationModeRevision?: number
}

export type AgentTodoItemData = {
  id: string
  content: string
  status: string
  activeForm?: string
}

export type AgentTodoUpdatedData = {
  todos: AgentTodoItemData[]
}

export type AgentRunErrorData = {
  message?: string
  code?: string
  errorClass?: string
  traceId?: string
  details?: Record<string, unknown>
}

export type AgentUIEventData =
  | AgentAssistantDeltaData
  | AgentToolInputDeltaData
  | AgentToolCallData
  | AgentToolResultData
  | AgentTerminalDeltaData
  | AgentSegmentStartData
  | AgentSegmentEndData
  | AgentContextCompactData
  | AgentInputRequestData
  | AgentSecurityBlockData
  | AgentThreadTitleData
  | AgentThreadCollaborationData
  | AgentTodoUpdatedData
  | AgentRunErrorData
  | Record<string, unknown>
  | string
  | null
  | undefined

export type AgentUIEvent = {
  id: string
  streamId: string
  order: number
  timestamp: string
  type: AgentUIEventType
  data: AgentUIEventData
  toolName?: string
  errorCode?: string
}

export type AgentFinishReason =
  | 'stop'
  | 'length'
  | 'content-filter'
  | 'tool-calls'
  | 'error'
  | 'other'

export type AgentUIMessageChunk<METADATA = unknown, DATA_TYPES extends AgentUIDataTypes = AgentUIDataTypes> =
  | { type: 'start'; messageId?: string; messageMetadata?: METADATA }
  | { type: 'finish'; finishReason?: AgentFinishReason; messageMetadata?: METADATA }
  | { type: 'message-metadata'; messageMetadata: METADATA }
  | { type: 'text-start'; id: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'text-delta'; id: string; delta: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'text-end'; id: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'reasoning-start'; id: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'reasoning-delta'; id: string; delta: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'reasoning-end'; id: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'custom'; kind: `${string}.${string}`; providerMetadata?: AgentProviderMetadata }
  | { type: 'tool-input-start'; toolCallId: string; toolName: string; title?: string; providerExecuted?: boolean; providerMetadata?: AgentProviderMetadata; dynamic?: boolean }
  | { type: 'tool-input-delta'; toolCallId: string; inputTextDelta: string }
  | { type: 'tool-input-available'; toolCallId: string; toolName: string; input: unknown; title?: string; providerExecuted?: boolean; providerMetadata?: AgentProviderMetadata; dynamic?: boolean }
  | { type: 'tool-input-error'; toolCallId: string; toolName: string; input: unknown; errorText: string; title?: string; providerExecuted?: boolean; providerMetadata?: AgentProviderMetadata; dynamic?: boolean }
  | { type: 'tool-approval-request'; approvalId: string; toolCallId: string; isAutomatic?: boolean }
  | { type: 'tool-approval-response'; approvalId: string; approved: boolean; reason?: string; providerExecuted?: boolean; providerMetadata?: AgentProviderMetadata }
  | { type: 'tool-output-available'; toolCallId: string; output: unknown; preliminary?: boolean; providerExecuted?: boolean; providerMetadata?: AgentProviderMetadata; dynamic?: boolean }
  | { type: 'tool-output-error'; toolCallId: string; errorText: string; providerExecuted?: boolean; providerMetadata?: AgentProviderMetadata; dynamic?: boolean }
  | { type: 'tool-output-denied'; toolCallId: string }
  | { type: 'source-url'; sourceId: string; url: string; title?: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'source-document'; sourceId: string; mediaType: string; title: string; filename?: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'file'; url: string; mediaType: string; providerMetadata?: AgentProviderMetadata }
  | { type: 'reasoning-file'; url: string; mediaType: string; providerMetadata?: AgentProviderMetadata }
  | ({ type: `data-${string}`; id?: string; data: DATA_TYPES[keyof DATA_TYPES & string]; transient?: boolean })
  | { type: 'start-step' }
  | { type: 'finish-step' }
  | { type: 'abort'; reason?: string }
  | { type: 'error'; errorText: string }

export type AgentRunReasoningMode =
  | 'auto'
  | 'enabled'
  | 'disabled'
  | 'none'
  | 'off'
  | 'minimal'
  | 'low'
  | 'medium'
  | 'high'
  | 'max'
  | 'xhigh'

export type AgentCreateRunOptions = {
  resumeFromRunId?: string | null
}

export type AgentCreateMessageInput = {
  threadId: string
  request: AgentCreateMessageRequest
}

export type AgentCreateRunInput = {
  threadId: string
  personaId?: string
  modelOverride?: string
  workDir?: string
  reasoningMode?: AgentRunReasoningMode
  options?: AgentCreateRunOptions
}

export type AgentEditMessageInput = AgentCreateRunInput & {
  messageId: string
  content: string
  contentJson?: AgentMessageContent
}

export type AgentRetryMessageInput = AgentCreateRunInput & {
  messageId: string
}

export type AgentStreamState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'closed' | 'error'

export type AgentOpenMessageChunkStreamOptions = {
  cursor?: number
  live?: boolean
  onStateChange?: (state: AgentStreamState) => void
  onError?: (error: Error) => void
  signal?: AbortSignal
  maxRetries?: number
  retryDelayMs?: number
  maxRetryDelayMs?: number
  readTimeoutMs?: number
  maxAuthRetries?: number
}

export type AgentOpenEventStreamOptions = AgentOpenMessageChunkStreamOptions

export type AgentChatRequestOptions = {
  headers?: HeadersInit
  body?: Record<string, unknown>
  metadata?: unknown
}

export type AgentTransport<UI_MESSAGE extends AgentUIMessage = AgentUIMessage> = {
  sendMessages: (options: {
    trigger: 'submit-message' | 'regenerate-message'
    chatId: string
    messageId?: string
    messages: UI_MESSAGE[]
    abortSignal?: AbortSignal
  } & AgentChatRequestOptions) => Promise<ReadableStream<AgentUIMessageChunk>>
  reconnectToStream: (options: {
    chatId: string
    streamId?: string
  } & AgentChatRequestOptions) => Promise<ReadableStream<AgentUIMessageChunk> | null>
}

export type AgentClient = {
  listMessages: (threadId: string, limit?: number) => Promise<AgentMessage[]>
  createMessage: (input: AgentCreateMessageInput) => Promise<AgentMessage>
  createRun: (input: AgentCreateRunInput) => Promise<AgentRun>
  editMessage: (input: AgentEditMessageInput) => Promise<AgentRun>
  retryMessage: (input: AgentRetryMessageInput) => Promise<AgentRun>
  cancelRun: (streamId: string, lastSeenSequence?: number) => Promise<void>
  provideInput: (streamId: string, value: string) => Promise<void>
  openEventStream: (
    streamId: string,
    options?: AgentOpenEventStreamOptions,
  ) => ReadableStream<AgentUIEvent>
  openMessageChunkStream: (
    streamId: string,
    options?: AgentOpenMessageChunkStreamOptions,
  ) => ReadableStream<AgentUIMessageChunk>
}

export type AgentBackendAdapter = AgentClient
