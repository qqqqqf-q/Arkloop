import {
  cancelRun,
  createMessage,
  createRun,
  editMessage,
  listMessages,
  listRunEvents,
  provideInput,
  retryMessage,
  type MessageContent,
  type MessageContentPart,
  type MessageResponse,
  type RunEvent,
} from '../api'
import { buildAgentUIParts } from '../messageContent'
import { createSSEClient } from '../sse'
import type {
  AgentClient,
  AgentCreateMessageRequest,
  AgentMessage,
  AgentMessageAttachmentRef,
  AgentMessageContent,
  AgentMessageContentPart,
  AgentRun,
  AgentOpenEventStreamOptions,
  AgentUIEvent,
  AgentOpenMessageChunkStreamOptions,
  AgentUIMessageChunk,
} from './contract'
import {
  normalizeAgentEventData,
  normalizeAgentEventToolName,
  normalizeAgentEventType,
} from './event-data'

export type CreateArkloopAgentClientOptions = {
  accessToken: string
  baseUrl?: string
  refreshAccessToken?: () => Promise<string>
}

function buildStreamUrl(baseUrl: string | undefined, streamId: string): string {
  const normalizedBaseUrl = (baseUrl ?? '').replace(/\/$/, '')
  return `${normalizedBaseUrl}/v1/runs/${streamId}/events`
}

function toAgentRun(run: { run_id: string; trace_id: string }): AgentRun {
  return {
    id: run.run_id,
    traceId: run.trace_id,
  }
}

function toAgentAttachment(attachment: {
  key: string
  filename: string
  mime_type: string
  size: number
}): AgentMessageAttachmentRef {
  return {
    key: attachment.key,
    filename: attachment.filename,
    mediaType: attachment.mime_type,
    size: attachment.size,
  }
}

function toAgentContentPart(part: MessageContentPart): AgentMessageContentPart {
  if (part.type === 'text') return part
  if (part.type === 'image') {
    return { type: 'image', attachment: toAgentAttachment(part.attachment) }
  }
  return {
    type: 'file',
    attachment: toAgentAttachment(part.attachment),
    extractedText: part.extracted_text,
  }
}

function toArkloopAttachment(attachment: AgentMessageAttachmentRef): {
  key: string
  filename: string
  mime_type: string
  size: number
} {
  return {
    key: attachment.key,
    filename: attachment.filename,
    mime_type: attachment.mediaType,
    size: attachment.size,
  }
}

function toArkloopContentPart(part: AgentMessageContentPart): MessageContentPart {
  if (part.type === 'text') return part
  if (part.type === 'image') {
    return { type: 'image', attachment: toArkloopAttachment(part.attachment) }
  }
  return {
    type: 'file',
    attachment: toArkloopAttachment(part.attachment),
    extracted_text: part.extractedText,
  }
}

function toAgentContent(content: MessageContent | undefined): AgentMessageContent | undefined {
  if (!content?.parts?.length) return undefined
  return { parts: content.parts.map(toAgentContentPart) }
}

function toArkloopContent(content: AgentMessageContent | undefined): MessageContent | undefined {
  if (!content?.parts?.length) return undefined
  return { parts: content.parts.map(toArkloopContentPart) }
}

function toArkloopCreateMessageRequest(request: AgentCreateMessageRequest): {
  content?: string
  content_json?: MessageContent
  client_message_id?: string
} {
  return {
    content: request.content,
    content_json: toArkloopContent(request.contentJson),
    client_message_id: request.clientMessageId,
  }
}

function toAgentMessage(message: MessageResponse): AgentMessage {
  const contentJson = toAgentContent(message.content_json)
  const parts = buildAgentUIParts(contentJson, message.content)

  return {
    id: message.id,
    role: message.role === 'system' || message.role === 'user' || message.role === 'assistant'
      ? message.role
      : 'assistant',
    content: message.content,
    contentJson,
    createdAt: message.created_at,
    streamId: message.run_id,
    metadata: {
      createdAt: message.created_at,
      streamId: message.run_id,
      clientMessageId: message.client_message_id,
      deliveryStatus: 'sent',
    },
    parts,
  }
}

function toAgentUIEvent(event: RunEvent): AgentUIEvent {
  const type = normalizeAgentEventType(event.type)
  const data = normalizeAgentEventData({
    type,
    rawType: event.type,
    eventId: event.event_id,
    data: event.data,
    toolName: event.tool_name,
    errorCode: event.error_class,
  })

  return {
    id: event.event_id,
    streamId: event.run_id,
    order: event.seq,
    timestamp: event.ts,
    type,
    data,
    toolName: normalizeAgentEventToolName({ type, data, fallback: event.tool_name }),
    errorCode: event.error_class,
  }
}

type MessageChunkLifecycle = {
  textStarted: boolean
  reasoningStarted: boolean
  toolInputStarted: Set<string>
  pendingToolInputDeltasByIndex: Map<number, { toolName?: string; deltas: string[] }>
}

function createMessageChunkLifecycle(): MessageChunkLifecycle {
  return {
    textStarted: false,
    reasoningStarted: false,
    toolInputStarted: new Set(),
    pendingToolInputDeltasByIndex: new Map(),
  }
}

function enqueueTextDelta(
  chunks: AgentUIMessageChunk[],
  state: MessageChunkLifecycle,
  delta: string,
): void {
  if (!state.textStarted) {
    chunks.push({ type: 'text-start', id: 'text' })
    state.textStarted = true
  }
  chunks.push({ type: 'text-delta', id: 'text', delta })
}

function enqueueReasoningDelta(
  chunks: AgentUIMessageChunk[],
  state: MessageChunkLifecycle,
  delta: string,
): void {
  if (!state.reasoningStarted) {
    chunks.push({ type: 'reasoning-start', id: 'reasoning' })
    state.reasoningStarted = true
  }
  chunks.push({ type: 'reasoning-delta', id: 'reasoning', delta })
}

function finalizeOpenContentChunks(
  chunks: AgentUIMessageChunk[],
  state: MessageChunkLifecycle,
): void {
  if (state.reasoningStarted) {
    chunks.push({ type: 'reasoning-end', id: 'reasoning' })
    state.reasoningStarted = false
  }
  if (state.textStarted) {
    chunks.push({ type: 'text-end', id: 'text' })
    state.textStarted = false
  }
}

function enqueueToolInputDelta(
  chunks: AgentUIMessageChunk[],
  state: MessageChunkLifecycle,
  params: {
    toolCallId: string
    toolName: string
    delta: string
  },
): void {
  if (!state.toolInputStarted.has(params.toolCallId)) {
    chunks.push({
      type: 'tool-input-start',
      toolCallId: params.toolCallId,
      toolName: params.toolName,
      dynamic: true,
    })
    state.toolInputStarted.add(params.toolCallId)
  }
  chunks.push({
    type: 'tool-input-delta',
    toolCallId: params.toolCallId,
    inputTextDelta: params.delta,
  })
}

function bufferToolInputDelta(
  state: MessageChunkLifecycle,
  params: {
    toolCallIndex: number
    toolName?: string
    delta: string
  },
): void {
  const pending = state.pendingToolInputDeltasByIndex.get(params.toolCallIndex) ?? { deltas: [] }
  if (params.toolName) pending.toolName = params.toolName
  pending.deltas.push(params.delta)
  state.pendingToolInputDeltasByIndex.set(params.toolCallIndex, pending)
}

function drainPendingToolInputDeltas(
  chunks: AgentUIMessageChunk[],
  state: MessageChunkLifecycle,
  params: {
    toolCallId: string
    toolName: string
    toolCallIndex?: number
  },
): void {
  let pendingIndex = params.toolCallIndex
  if (pendingIndex == null && state.pendingToolInputDeltasByIndex.size === 1) {
    pendingIndex = [...state.pendingToolInputDeltasByIndex.keys()][0]
  }
  if (pendingIndex == null) return

  const pending = state.pendingToolInputDeltasByIndex.get(pendingIndex)
  if (!pending) return
  for (const delta of pending.deltas) {
    enqueueToolInputDelta(chunks, state, {
      toolCallId: params.toolCallId,
      toolName: pending.toolName ?? params.toolName,
      delta,
    })
  }
  state.pendingToolInputDeltasByIndex.delete(pendingIndex)
}

function agentEventToMessageChunks(uiEvent: AgentUIEvent, state: MessageChunkLifecycle): AgentUIMessageChunk[] {
  const chunks: AgentUIMessageChunk[] = []

  if (uiEvent.type === 'assistant-delta') {
    const data = uiEvent.data as { delta?: unknown; role?: unknown; channel?: unknown }
    const delta = typeof data.delta === 'string' ? data.delta : ''
    const role = typeof data.role === 'string' ? data.role : undefined
    const channel = typeof data.channel === 'string' ? data.channel : undefined
    if (!delta || (role && role !== 'assistant')) return chunks
    if (channel === 'thinking') {
      enqueueReasoningDelta(chunks, state, delta)
    } else {
      enqueueTextDelta(chunks, state, delta)
    }
    return chunks
  }

  if (uiEvent.type === 'tool-input-delta') {
    const data = uiEvent.data as {
      toolCallIndex?: number
      toolCallId?: string
      toolName?: string
      delta?: string
    }
    if (!data.delta) return chunks
    if (!data.toolCallId) {
      if (data.toolCallIndex != null) {
        bufferToolInputDelta(state, {
          toolCallIndex: data.toolCallIndex,
          toolName: data.toolName,
          delta: data.delta,
        })
      }
      return chunks
    }
    drainPendingToolInputDeltas(chunks, state, {
      toolCallId: data.toolCallId,
      toolName: data.toolName ?? 'tool',
      toolCallIndex: data.toolCallIndex,
    })
    enqueueToolInputDelta(chunks, state, {
      toolCallId: data.toolCallId,
      toolName: data.toolName ?? 'tool',
      delta: data.delta,
    })
    return chunks
  }

  if (uiEvent.type === 'tool-call') {
    const data = uiEvent.data as {
      toolCallId: string
      toolName: string
      input: unknown
      displayDescription?: string
      toolCallIndex?: number
    }
    const toolCallId = data.toolCallId
    const toolName = data.toolName || uiEvent.toolName || 'tool'
    drainPendingToolInputDeltas(chunks, state, {
      toolCallId,
      toolName,
      toolCallIndex: data.toolCallIndex,
    })
    chunks.push({
      type: 'tool-input-available',
      toolCallId,
      toolName,
      input: data.input,
    })
    state.toolInputStarted.delete(toolCallId)
    return chunks
  }

  if (uiEvent.type === 'tool-result') {
    const data = uiEvent.data as {
      toolCallId: string
      output: unknown
      error?: { message?: string; errorClass?: string; code?: string }
    }
    if (data.error || uiEvent.errorCode) {
      chunks.push({
        type: 'tool-output-error',
        toolCallId: data.toolCallId,
        errorText: data.error?.message ?? data.error?.errorClass ?? data.error?.code ?? uiEvent.errorCode ?? 'tool error',
      })
      return chunks
    }
    chunks.push({ type: 'tool-output-available', toolCallId: data.toolCallId, output: data.output })
    return chunks
  }

  if (uiEvent.type === 'run-completed') {
    finalizeOpenContentChunks(chunks, state)
    chunks.push({ type: 'finish', finishReason: 'stop' })
    return chunks
  }
  if (uiEvent.type === 'run-failed') {
    finalizeOpenContentChunks(chunks, state)
    chunks.push({ type: 'finish', finishReason: 'error' })
    return chunks
  }
  if (uiEvent.type === 'run-cancelled' || uiEvent.type === 'run-interrupted') {
    finalizeOpenContentChunks(chunks, state)
    chunks.push({ type: 'abort', reason: uiEvent.type })
    return chunks
  }

  return chunks
}

function enqueueStreamStart(controller: ReadableStreamDefaultController<AgentUIMessageChunk>): void {
  controller.enqueue({ type: 'start' })
}

function enqueueStreamEnd(
  controller: ReadableStreamDefaultController<AgentUIMessageChunk>,
  state: MessageChunkLifecycle,
): void {
  const chunks: AgentUIMessageChunk[] = []
  finalizeOpenContentChunks(chunks, state)
  for (const chunk of chunks) controller.enqueue(chunk)
}

function runEventsToEventStream(eventsPromise: Promise<RunEvent[]>): ReadableStream<AgentUIEvent> {
  return new ReadableStream<AgentUIEvent>({
    async start(controller) {
      try {
        const events = await eventsPromise
        for (const event of events) {
          controller.enqueue(toAgentUIEvent(event))
        }
        controller.close()
      } catch (error) {
        controller.error(error)
      }
    },
  })
}

function eventStreamToMessageChunkStream(
  eventStream: ReadableStream<AgentUIEvent>,
): ReadableStream<AgentUIMessageChunk> {
  let reader: ReadableStreamDefaultReader<AgentUIEvent> | null = null

  return new ReadableStream<AgentUIMessageChunk>({
    async start(controller) {
      const lifecycle = createMessageChunkLifecycle()
      reader = eventStream.getReader()
      enqueueStreamStart(controller)
      try {
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          const chunks: AgentUIMessageChunk[] = [{
            type: 'data-agent-event',
            id: value.id,
            data: value,
            transient: true,
          }]
          const projected = agentEventToMessageChunks(value, lifecycle)
          for (const chunk of [...chunks, ...projected]) {
            controller.enqueue(chunk)
          }
        }
        enqueueStreamEnd(controller, lifecycle)
        controller.close()
      } catch (error) {
        controller.enqueue({
          type: 'error',
          errorText: error instanceof Error ? error.message : String(error),
        })
        controller.close()
      } finally {
        reader?.releaseLock()
        reader = null
      }
    },
    cancel() {
      void reader?.cancel().catch(() => {})
    },
  })
}

export function createArkloopAgentClient({
  accessToken,
  baseUrl,
  refreshAccessToken,
}: CreateArkloopAgentClientOptions): AgentClient {
  const openEventStream = (
    streamId: string,
    options?: AgentOpenEventStreamOptions,
  ): ReadableStream<AgentUIEvent> => {
    if (options?.live === false) {
      return runEventsToEventStream(listRunEvents(accessToken, streamId, {
        afterSeq: options.cursor,
        follow: false,
      }))
    }

    let client: ReturnType<typeof createSSEClient> | null = null
    let closed = false
    const abort = () => {
      closed = true
      client?.close()
    }

    return new ReadableStream<AgentUIEvent>({
      start(controller) {
        const signal = options?.signal
        if (signal?.aborted) {
          abort()
          controller.close()
          return
        }
        signal?.addEventListener('abort', abort, { once: true })

        client = createSSEClient({
          url: buildStreamUrl(baseUrl, streamId),
          accessToken,
          afterSeq: options?.cursor,
          follow: options?.live ?? true,
          maxRetries: options?.maxRetries,
          retryDelayMs: options?.retryDelayMs,
          maxRetryDelayMs: options?.maxRetryDelayMs,
          readTimeoutMs: options?.readTimeoutMs,
          maxAuthRetries: options?.maxAuthRetries,
          onTokenRefresh: refreshAccessToken,
          onStateChange: options?.onStateChange,
          onError: (error) => {
            options?.onError?.(error)
            if (!closed) {
              controller.error(error)
            }
          },
          onEvent: (event) => {
            if (closed) return
            controller.enqueue(toAgentUIEvent(event))
          },
        })

        void client.connect()
          .catch((error: unknown) => {
            if (closed) return
            const err = error instanceof Error ? error : new Error(String(error))
            options?.onError?.(err)
            controller.error(err)
          })
          .finally(() => {
            signal?.removeEventListener('abort', abort)
            if (closed) return
            closed = true
            controller.close()
          })
      },
      cancel() {
        abort()
      },
    })
  }

  return {
    listMessages: async (threadId, limit) => (
      await (limit == null
        ? listMessages(accessToken, threadId)
        : listMessages(accessToken, threadId, limit))
    ).map(toAgentMessage),
    createMessage: async (input) => toAgentMessage(await createMessage(
      accessToken,
      input.threadId,
      toArkloopCreateMessageRequest(input.request),
    )),
    createRun: async (input) => toAgentRun(await createRun(
      accessToken,
      input.threadId,
      input.personaId,
      input.modelOverride,
      input.workDir,
      input.reasoningMode,
      input.options,
    )),
    editMessage: async (input) => toAgentRun(await editMessage(
      accessToken,
      input.threadId,
      input.messageId,
      input.content,
      toArkloopContent(input.contentJson),
      input.personaId,
      input.modelOverride,
      input.workDir,
      input.reasoningMode,
    )),
    retryMessage: async (input) => toAgentRun(await retryMessage(
      accessToken,
      input.threadId,
      input.messageId,
      input.personaId,
      input.modelOverride,
      input.workDir,
      input.reasoningMode,
    )),
    cancelRun: async (streamId, lastSeenSequence) => {
      await cancelRun(accessToken, streamId, lastSeenSequence)
    },
    provideInput: async (streamId, value) => {
      await provideInput(accessToken, streamId, value)
    },
    openEventStream,
    openMessageChunkStream: (
      streamId: string,
      options?: AgentOpenMessageChunkStreamOptions,
    ) => eventStreamToMessageChunkStream(openEventStream(streamId, options)),
  }
}
