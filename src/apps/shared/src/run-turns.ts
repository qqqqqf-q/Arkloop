import { redactDataUrlsInString } from './debugPayloadRedact'
import { isACPDelegateEventData } from './runEventDelegate'
import { canonicalToolName, pickLogicalToolName } from './tool-names'

export type RunEventRaw = {
  event_id: string
  run_id: string
  seq: number
  ts: string
  type: string
  data: Record<string, unknown>
  tool_name?: string
  error_class?: string
}

export type TurnSegment =
  | {
      kind: 'assistant'
      text: string
      isFinal?: boolean
    }
  | {
      kind: 'tool_call'
      toolCallId: string
      toolName: string
      argsJSON: Record<string, unknown>
    }
  | {
      kind: 'tool_result'
      toolCallId: string
      toolName: string
      resultJSON?: Record<string, unknown>
      errorClass?: string
    }

export type RequestMessageView = {
  role: string
  text: string
}

export type RequestSnapshot = {
  llmCallId: string
  messageCount?: number
  messages: RequestMessageView[]
}

export type LlmTurn = {
  llmCallId: string
  providerKind: string
  apiMode: string
  contextTokens?: number
  inputTokens?: number
  outputTokens?: number
  cachedTokens?: number
  cacheCreationTokens?: number
  payloadBytes?: number
  estimatedInputTokens?: number
  userInput?: string
  inputMeta?: Record<string, string>
  assistantText: string
  segments: TurnSegment[]
  toolCalls: Array<{
    toolCallId: string
    toolName: string
    argsJSON: Record<string, unknown>
    resultJSON?: Record<string, unknown>
    errorClass?: string
  }>
  model?: string
  systemPrompt?: string
  toolCount?: number
  toolNames?: string[]
  messageCount?: number
  temperature?: number
  maxOutputTokens?: number
  systemBytes?: number
  toolsBytes?: number
  messagesBytes?: number
  abstractRequestBytes?: number
  providerPayloadBytes?: number
  imagePartCount?: number
  base64ImageBytes?: number
  networkAttempted?: boolean
  roleBytes?: Record<string, number>
  toolSchemaBytesMap?: Record<string, number>
  stablePrefixHash?: string
  requests: RequestSnapshot[]
}

type UserInputInfo = {
  userInput?: string
  inputMeta?: Record<string, string>
  messages: Array<Record<string, unknown>>
  userMessageCount: number
}

type RequestState = {
  turn: LlmTurn
  sawVisibleAssistantDelta: boolean
}

const assistantReservedControlToken = '<end_turn>'

type TurnState = {
  turn: LlmTurn
  userMessageCount: number
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined
}

function cleanText(value: string | undefined): string | undefined {
  const trimmed = value?.trim()
  return trimmed ? trimmed : undefined
}

function stripTelegramEnvelopeBodyPrefix(text: string, meta: Record<string, string>): string {
  let cleaned = cleanText(text) ?? ''
  const title = cleanText(meta['conversation-title'])
  if (title) {
    const titlePrefix = `[Telegram in ${title}]`
    if (cleaned.startsWith(titlePrefix)) {
      cleaned = cleanText(cleaned.slice(titlePrefix.length)) ?? ''
    }
  }
  if (cleaned.startsWith('[Telegram]')) {
    cleaned = cleanText(cleaned.slice('[Telegram]'.length)) ?? ''
  }
  return cleaned
}

// 与 worker context_compact.approxTokensFromText 同阶：按 UTF-8 字节粗算 token，仅用于调试对照账单 in。
function approxContextTokensFromPayloadBytes(turn: LlmTurn): number | undefined {
  if (
    turn.systemBytes == null &&
    turn.toolsBytes == null &&
    turn.messagesBytes == null &&
    turn.providerPayloadBytes == null &&
    turn.abstractRequestBytes == null
  ) {
    return undefined
  }
  const total =
    turn.abstractRequestBytes ??
    ((turn.systemBytes ?? 0) + (turn.toolsBytes ?? 0) + (turn.messagesBytes ?? 0))
  if (total <= 0) return undefined
  return Math.floor((total + 3) / 4)
}

function refreshContextTokenEstimate(turn: LlmTurn) {
  if (turn.contextTokens != null) {
    return
  }
  const approx = approxContextTokensFromPayloadBytes(turn)
  turn.estimatedInputTokens = approx
}

function extractToolName(tool: Record<string, unknown>): string {
  if (typeof tool.name === 'string') return canonicalToolName(tool.name)
  const fn = tool.function
  if (fn && typeof fn === 'object') {
    const name = (fn as Record<string, unknown>).name
    if (typeof name === 'string') return canonicalToolName(name)
  }
  return ''
}

function canonicalizeToolSchemaBytesMap(raw: unknown): Record<string, number> | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const entries = Object.entries(raw as Record<string, unknown>)
  if (entries.length === 0) return undefined

  const out: Record<string, number> = {}
  for (const [key, value] of entries) {
    if (typeof value !== 'number') continue
    const canonical = canonicalToolName(key)
    if (!canonical) continue
    out[canonical] = (out[canonical] ?? 0) + value
  }
  return Object.keys(out).length > 0 ? out : undefined
}

function readMessageText(msg: Record<string, unknown>): string {
  const content = msg.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part: unknown) => {
        if (typeof part === 'string') return part
        if (typeof part === 'object' && part !== null) {
          const record = part as Record<string, unknown>
          if (record.type === 'tool_use' || record.type === 'tool_result') return ''
          return typeof record.text === 'string' ? record.text : ''
        }
        return ''
      })
      .join('')
  }
  return JSON.stringify(content)
}

function extractMessageText(msg: Record<string, unknown>): string {
  const out = readMessageText(msg)
  return redactDataUrlsInString(normalizeChannelEnvelopeText(out))
}

function extractRawMessageText(msg: Record<string, unknown>): string {
  const out = readMessageText(msg)
  return redactDataUrlsInString(out)
}

function extractRequestMessages(messages: Array<Record<string, unknown>>): RequestMessageView[] {
  return messages
    .map((message) => ({
      role: typeof message.role === 'string' ? message.role : 'unknown',
      text: extractMessageText(message),
    }))
    .filter((message) => message.role || message.text)
}

function parseChannelEnvelope(text: string): { text: string; meta: Record<string, string> } | null {
  const normalized = text.replace(/\r\n/g, '\n')
  const match = normalized.match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/)
  if (!match) return null

  const header = match[1]
  const body = cleanText(match[2]) ?? ''
  const meta: Record<string, string> = {}

  for (const line of header.split('\n')) {
    const idx = line.indexOf(':')
    if (idx <= 0) continue
    const key = line.slice(0, idx).trim()
    const rawValue = line.slice(idx + 1).trim()
    if (!key || !rawValue) continue
    meta[key] = rawValue.replace(/^"|"$/g, '')
  }

  if (!body && Object.keys(meta).length === 0) return null
  return { text: body, meta }
}

export function normalizeChannelEnvelopeText(text: string): string {
  const parsed = parseChannelEnvelope(text)
  if (!parsed) return text

  const lines: string[] = []
  const replyToID = cleanText(parsed.meta['reply-to-message-id'])
  const replyPreview = cleanText(parsed.meta['reply-to-preview'])
  if (replyToID) {
    let replyLine = `> Reply to #${replyToID}`
    if (replyPreview) {
      replyLine += ` "${replyPreview}"`
    }
    lines.push(replyLine)
  }

  const forwardFrom = cleanText(parsed.meta['forward-from'])
  if (forwardFrom) {
    lines.push(`[Fwd: ${forwardFrom}]`)
  }

  const body = stripTelegramEnvelopeBodyPrefix(parsed.text, parsed.meta)
  if (body) {
    lines.push(body)
  }

  return lines.join('\n').trim()
}

// Anthropic 将 tool_result 包装为 role:"user" 消息，需要排除这些消息以避免 userMessageCount 膨胀。
function isToolResultOnlyMessage(message: Record<string, unknown>): boolean {
  const content = message.content
  if (!Array.isArray(content)) return false
  if (content.length === 0) return false
  return content.every((part: unknown) => {
    if (typeof part !== 'object' || part === null) return false
    const record = part as Record<string, unknown>
    return record.type === 'tool_result'
  })
}

function extractLatestUserInput(payload: Record<string, unknown> | undefined): UserInputInfo {
  const messages = Array.isArray(payload?.messages)
    ? (payload.messages as Array<Record<string, unknown>>)
    : []
  const userMessages = messages.filter(
    (message) => message.role === 'user' && !isToolResultOnlyMessage(message),
  )

  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i]
    if (message.role !== 'user') continue
    const rawText = cleanText(extractRawMessageText(message))
    if (!rawText) continue
    const parsed = parseChannelEnvelope(rawText)
    if (parsed) {
      return {
        userInput: normalizeChannelEnvelopeText(rawText),
        inputMeta: parsed.meta,
        messages,
        userMessageCount: userMessages.length,
      }
    }
    const text = cleanText(normalizeChannelEnvelopeText(rawText))
    if (!text) continue
    return {
      userInput: text,
      messages,
      userMessageCount: userMessages.length,
    }
  }

  const fallbackCandidates = [payload?.input, payload?.prompt, payload?.input_text]
  for (const candidate of fallbackCandidates) {
    if (typeof candidate !== 'string') continue
    const rawText = cleanText(redactDataUrlsInString(candidate))
    if (!rawText) continue
    const parsed = parseChannelEnvelope(rawText)
    if (parsed) {
      return {
        userInput: normalizeChannelEnvelopeText(rawText),
        inputMeta: parsed.meta,
        messages,
        userMessageCount: userMessages.length || 1,
      }
    }
    const text = cleanText(normalizeChannelEnvelopeText(rawText))
    if (!text) continue
    return {
      userInput: text,
      messages,
      userMessageCount: userMessages.length || 1,
    }
  }

  const inputRecord = asRecord(payload?.input)
  const rawInputCandidate =
    typeof inputRecord?.text === 'string'
      ? inputRecord.text
      : typeof payload?.input_text === 'string'
        ? payload.input_text
        : undefined
  const rawInputText = cleanText(
    rawInputCandidate ? redactDataUrlsInString(rawInputCandidate) : undefined,
  )
  if (rawInputText) {
    const parsed = parseChannelEnvelope(rawInputText)
    if (parsed) {
      return {
        userInput: normalizeChannelEnvelopeText(rawInputText),
        inputMeta: parsed.meta,
        messages,
        userMessageCount: userMessages.length || 1,
      }
    }
    const inputText = cleanText(normalizeChannelEnvelopeText(rawInputText))
    return {
      userInput: inputText,
      messages,
      userMessageCount: userMessages.length || 1,
    }
  }

  return {
    messages,
    userMessageCount: userMessages.length,
  }
}

function extractCompletedAssistantText(data: Record<string, unknown>): string | undefined {
  const candidates = [data.output_text, data.assistant_text, data.final_output_text, data.text]
  for (const candidate of candidates) {
    if (typeof candidate !== 'string') continue
    const text = cleanText(candidate)
    if (text) return text
  }
  return undefined
}

function uniqueStrings(values: Array<string | undefined>): string[] | undefined {
  const items = Array.from(new Set(values.map((value) => cleanText(value)).filter(Boolean) as string[]))
  return items.length > 0 ? items : undefined
}

function appendAssistantSegment(turn: LlmTurn, text: string) {
  const cleaned = cleanText(text)
  if (!cleaned) return

  const lastSegment = turn.segments[turn.segments.length - 1]
  if (lastSegment?.kind === 'assistant') {
    lastSegment.text += cleaned
    return
  }

  turn.segments.push({
    kind: 'assistant',
    text: cleaned,
  })
}

class AssistantControlTokenFilter {
  private pending = ''

  push(chunk: string): string {
    if (chunk === '') return ''
    let combined = this.pending + chunk
    this.pending = ''
    if (combined === '') return ''

    const suffix = trailingAssistantControlPrefix(combined)
    if (suffix) {
      this.pending = suffix
      combined = combined.slice(0, combined.length - suffix.length)
    }
    if (combined === '') return ''

    const cleaned = combined.split(assistantReservedControlToken).join('')
    if (cleaned.trim() === '' && combined.includes(assistantReservedControlToken)) {
      return ''
    }
    return cleaned
  }

  flush(): string {
    const tail = this.pending
    this.pending = ''
    return tail
  }
}

function trailingAssistantControlPrefix(text: string): string {
  const maxSuffix = Math.min(text.length, assistantReservedControlToken.length - 1)
  for (let size = maxSuffix; size > 0; size -= 1) {
    const suffix = text.slice(-size)
    if (assistantReservedControlToken.startsWith(suffix)) {
      return suffix
    }
  }
  return ''
}

function finalizeTurnAssistant(turn: LlmTurn) {
  let lastAssistant: Extract<TurnSegment, { kind: 'assistant' }> | undefined
  for (const segment of turn.segments) {
    if (segment.kind === 'assistant') {
      segment.isFinal = false
      lastAssistant = segment
    }
  }

  if (lastAssistant) {
    lastAssistant.isFinal = true
    turn.assistantText = lastAssistant.text
  }
}

function startTurn(
  requestData: Record<string, unknown>,
  payload: Record<string, unknown> | undefined,
  input: UserInputInfo,
): TurnState {
  const tools = Array.isArray(payload?.tools)
    ? (payload.tools as Array<Record<string, unknown>>)
    : []
  const toolNames = uniqueStrings(tools.map(extractToolName))
  const systemMessage = input.messages.find((message) => message.role === 'system')

  const turn: LlmTurn = {
    llmCallId: String(requestData.llm_call_id ?? ''),
    providerKind: String(requestData.provider_kind ?? ''),
    apiMode: String(requestData.api_mode ?? ''),
    userInput: input.userInput,
    inputMeta: input.inputMeta,
    assistantText: '',
    segments: [],
    toolCalls: [],
    model: payload?.model != null ? String(payload.model) : undefined,
    systemPrompt: systemMessage ? extractMessageText(systemMessage) : undefined,
    toolCount: tools.length > 0 ? tools.length : undefined,
    toolNames,
    messageCount: input.messages.length > 0 ? input.messages.length : undefined,
    temperature: typeof payload?.temperature === 'number' ? payload.temperature : undefined,
    maxOutputTokens:
      typeof payload?.max_tokens === 'number'
        ? payload.max_tokens
        : typeof payload?.max_output_tokens === 'number'
          ? payload.max_output_tokens
          : undefined,
    systemBytes: typeof requestData.system_bytes === 'number' ? requestData.system_bytes : undefined,
    toolsBytes: typeof requestData.tools_bytes === 'number' ? requestData.tools_bytes : undefined,
    messagesBytes: typeof requestData.messages_bytes === 'number' ? requestData.messages_bytes : undefined,
    abstractRequestBytes:
      typeof requestData.abstract_request_bytes === 'number' ? requestData.abstract_request_bytes : undefined,
    providerPayloadBytes:
      typeof requestData.provider_payload_bytes === 'number' ? requestData.provider_payload_bytes : undefined,
    imagePartCount:
      typeof requestData.image_part_count === 'number' ? requestData.image_part_count : undefined,
    base64ImageBytes:
      typeof requestData.base64_image_bytes === 'number' ? requestData.base64_image_bytes : undefined,
    networkAttempted:
      typeof requestData.network_attempted === 'boolean' ? requestData.network_attempted : undefined,
    roleBytes: requestData.role_bytes as Record<string, number> | undefined,
    toolSchemaBytesMap: canonicalizeToolSchemaBytesMap(requestData.tool_schema_bytes_by_name),
    stablePrefixHash:
      typeof requestData.stable_prefix_hash === 'string' ? requestData.stable_prefix_hash : undefined,
    requests: [
      {
        llmCallId: String(requestData.llm_call_id ?? ''),
        messageCount: input.messages.length > 0 ? input.messages.length : undefined,
        messages: extractRequestMessages(input.messages),
      },
    ],
  }
  refreshContextTokenEstimate(turn)
  return { userMessageCount: input.userMessageCount, turn }
}

function mergeRequestMetadata(
  turn: LlmTurn,
  requestData: Record<string, unknown>,
  payload: Record<string, unknown> | undefined,
  input: UserInputInfo,
) {
  turn.providerKind = String(requestData.provider_kind ?? turn.providerKind)
  turn.apiMode = String(requestData.api_mode ?? turn.apiMode)
  if (payload?.model != null) turn.model = String(payload.model)
  if (turn.systemPrompt == null) {
    const systemMessage = input.messages.find((message) => message.role === 'system')
    if (systemMessage) turn.systemPrompt = extractMessageText(systemMessage)
  }

  const tools = Array.isArray(payload?.tools)
    ? (payload.tools as Array<Record<string, unknown>>)
    : []
  const mergedToolNames = uniqueStrings([...(turn.toolNames ?? []), ...tools.map(extractToolName)])
  turn.toolNames = mergedToolNames
  turn.toolCount = Math.max(turn.toolCount ?? 0, tools.length || 0) || undefined
  turn.messageCount = Math.max(turn.messageCount ?? 0, input.messages.length || 0) || undefined
  turn.temperature =
    typeof payload?.temperature === 'number'
      ? payload.temperature
      : turn.temperature
  turn.maxOutputTokens =
    typeof payload?.max_tokens === 'number'
      ? payload.max_tokens
      : typeof payload?.max_output_tokens === 'number'
        ? payload.max_output_tokens
        : turn.maxOutputTokens
  if (typeof requestData.system_bytes === 'number') turn.systemBytes = requestData.system_bytes || undefined
  if (typeof requestData.tools_bytes === 'number') turn.toolsBytes = requestData.tools_bytes || undefined
  if (typeof requestData.messages_bytes === 'number') turn.messagesBytes = requestData.messages_bytes || undefined
  if (typeof requestData.abstract_request_bytes === 'number') {
    turn.abstractRequestBytes = requestData.abstract_request_bytes || undefined
  }
  if (typeof requestData.provider_payload_bytes === 'number') {
    turn.providerPayloadBytes = requestData.provider_payload_bytes || undefined
  }
  if (typeof requestData.image_part_count === 'number') {
    turn.imagePartCount = requestData.image_part_count || undefined
  }
  if (typeof requestData.base64_image_bytes === 'number') {
    turn.base64ImageBytes = requestData.base64_image_bytes || undefined
  }
  if (typeof requestData.network_attempted === 'boolean') {
    turn.networkAttempted = requestData.network_attempted
  }
  if (requestData.role_bytes && typeof requestData.role_bytes === 'object') {
    turn.roleBytes = requestData.role_bytes as Record<string, number>
  }
  const canonicalToolSchemaBytesMap = canonicalizeToolSchemaBytesMap(requestData.tool_schema_bytes_by_name)
  if (canonicalToolSchemaBytesMap) {
    turn.toolSchemaBytesMap = canonicalToolSchemaBytesMap
  }
  if (typeof requestData.stable_prefix_hash === 'string') {
    turn.stablePrefixHash = requestData.stable_prefix_hash
  }
  const llmCallId = String(requestData.llm_call_id ?? '')
  if (llmCallId) {
    const nextSnapshot: RequestSnapshot = {
      llmCallId,
      messageCount: input.messages.length > 0 ? input.messages.length : undefined,
      messages: extractRequestMessages(input.messages),
    }
    const existingIndex = turn.requests.findIndex((request) => request.llmCallId === llmCallId)
    if (existingIndex >= 0) turn.requests[existingIndex] = nextSnapshot
    else turn.requests.push(nextSnapshot)
  }
  refreshContextTokenEstimate(turn)
}

export function buildTurns(events: RunEventRaw[]): LlmTurn[] {
  const orderedEvents = [...events].sort((left, right) => left.seq - right.seq)
  const turns: LlmTurn[] = []
  const requestStates = new Map<string, RequestState>()
  const toolNameByCallID = new Map<string, string>()
  let currentState: TurnState | null = null
  let activeRequestCallID = ''
  const assistantChunks: string[] = []
  const assistantControlFilter = new AssistantControlTokenFilter()

  const flushAssistant = () => {
    if (!currentState) return
    const tail = assistantControlFilter.flush()
    if (tail) assistantChunks.push(tail)
    const merged = cleanText(assistantChunks.join(''))
    assistantChunks.length = 0
    if (!merged) return
    appendAssistantSegment(currentState.turn, merged)
  }

  for (const event of orderedEvents) {
    if (event.type === 'llm.request') {
      flushAssistant()

      const data = event.data as Record<string, unknown>
      const payload = data.payload as Record<string, unknown> | undefined
      const input = extractLatestUserInput(payload)
      const shouldStartNewTurn =
        currentState == null ||
        (input.userMessageCount > 0 && input.userMessageCount > currentState.userMessageCount) ||
        (
          input.userMessageCount > 0 &&
          input.userMessageCount === currentState.userMessageCount &&
          cleanText(input.userInput) !== cleanText(currentState.turn.userInput)
        )

      if (shouldStartNewTurn) {
        currentState = startTurn(data, payload, input)
        turns.push(currentState.turn)
      } else if (currentState) {
        mergeRequestMetadata(currentState.turn, data, payload, input)
      }

      activeRequestCallID = String(data.llm_call_id ?? '')
      if (currentState && activeRequestCallID) {
        requestStates.set(activeRequestCallID, {
          turn: currentState.turn,
          sawVisibleAssistantDelta: false,
        })
      }
      continue
    }

    if (!currentState) continue

    if (event.type === 'message.delta') {
      if (isACPDelegateEventData(event.data)) continue
      const data = event.data as Record<string, unknown>
      if (data.channel === 'thinking') continue
      const delta = String(data.content_delta ?? '')
      if (!delta) continue
      const cleaned = assistantControlFilter.push(delta)
      if (!cleaned) continue
      assistantChunks.push(cleaned)
      const requestState = requestStates.get(activeRequestCallID)
      if (requestState) requestState.sawVisibleAssistantDelta = true
      continue
    }

    if (event.type === 'tool.call') {
      if (isACPDelegateEventData(event.data)) continue
      flushAssistant()
      const data = event.data as Record<string, unknown>
      const toolCallId = String(data.tool_call_id ?? '')
      const toolName = pickLogicalToolName(data, event.tool_name)
      const argsJSON = (data.arguments as Record<string, unknown>) ?? {}
      currentState.turn.toolCalls.push({
        toolCallId,
        toolName,
        argsJSON,
      })
      currentState.turn.segments.push({
        kind: 'tool_call',
        toolCallId,
        toolName,
        argsJSON,
      })
      if (toolCallId) toolNameByCallID.set(toolCallId, toolName)
      continue
    }

    if (event.type === 'tool.result') {
      if (isACPDelegateEventData(event.data)) continue
      flushAssistant()
      const data = event.data as Record<string, unknown>
      const toolCallId = String(data.tool_call_id ?? '')
      const toolName = pickLogicalToolName(data, event.tool_name ?? toolNameByCallID.get(toolCallId) ?? '')
      const resultJSON = data.result as Record<string, unknown> | undefined
      const existing = currentState.turn.toolCalls.find((toolCall) => toolCall.toolCallId === toolCallId)
      if (existing) {
        existing.resultJSON = resultJSON
        existing.errorClass = event.error_class
      }
      currentState.turn.segments.push({
        kind: 'tool_result',
        toolCallId,
        toolName,
        resultJSON,
        errorClass: event.error_class,
      })
      continue
    }

    if (event.type === 'llm.turn.completed') {
      const data = event.data as Record<string, unknown>
      const llmCallId = String(data.llm_call_id ?? '')
      const requestState = requestStates.get(llmCallId)
      if (!requestState) continue

      const usage = data.usage as Record<string, unknown> | undefined
      if (usage) {
        const inputTokens = Number(usage.input_tokens ?? 0)
        const cacheReadTokens = Number(usage.cache_read_input_tokens ?? 0)
        const cacheCreationTokens = Number(usage.cache_creation_input_tokens ?? 0)
        const cachedRead = Number(usage.cached_tokens ?? usage.cache_read_input_tokens ?? 0)
        // 单次请求的窗口上下文：取最后一次 completion（多轮工具往返共用同一 LlmTurn）
        requestState.turn.inputTokens = inputTokens
        requestState.turn.cachedTokens = cachedRead
        requestState.turn.cacheCreationTokens = cacheCreationTokens
        requestState.turn.contextTokens = inputTokens + cacheReadTokens + cacheCreationTokens
        requestState.turn.outputTokens = (requestState.turn.outputTokens ?? 0) + Number(usage.output_tokens ?? 0)
      }
      if (typeof data.last_request_context_estimate_tokens === 'number' && data.last_request_context_estimate_tokens >= 0) {
        requestState.turn.estimatedInputTokens = data.last_request_context_estimate_tokens
      }

      if (llmCallId === activeRequestCallID) {
        const hadPendingText = assistantChunks.length > 0
        flushAssistant()
        if (!hadPendingText && !requestState.sawVisibleAssistantDelta) {
          const fallbackText = extractCompletedAssistantText(data)
          if (fallbackText) appendAssistantSegment(requestState.turn, fallbackText)
        }
      }
      continue
    }

    if (event.type === 'run.completed' || event.type === 'run.failed' || event.type === 'run.cancelled') {
      if (!isACPDelegateEventData(event.data)) {
        flushAssistant()
        currentState = null
        activeRequestCallID = ''
      }
      continue
    }
  }

  flushAssistant()
  turns.forEach(finalizeTurnAssistant)
  turns.forEach(refreshContextTokenEstimate)
  return turns
}
