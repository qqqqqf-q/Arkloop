import type { RunEventRaw } from './api/runs'

export type LlmTurn = {
  llmCallId: string
  providerKind: string
  apiMode: string
  inputTokens?: number
  outputTokens?: number
  userInput?: string
  assistantText: string
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
}

export function buildTurns(events: RunEventRaw[]): LlmTurn[] {
  const turns: LlmTurn[] = []
  let current: LlmTurn | null = null
  const assistantChunks: string[] = []
  const resultMap: Record<string, { resultJSON?: Record<string, unknown>; errorClass?: string }> = {}

  const extractToolName = (tool: Record<string, unknown>): string => {
    if (typeof tool.name === 'string') return tool.name
    const fn = tool.function
    if (fn && typeof fn === 'object') {
      const name = (fn as Record<string, unknown>).name
      if (typeof name === 'string') return name
    }
    return ''
  }

  for (const ev of events) {
    if (ev.type === 'llm.request') {
      if (current) {
        current.assistantText = assistantChunks.join('')
        assistantChunks.length = 0
      }

      const data = ev.data as Record<string, unknown>
      const payload = data.payload as Record<string, unknown> | undefined
      const messages = Array.isArray(payload?.messages)
        ? (payload.messages as Array<Record<string, unknown>>)
        : []

      let userInput: string | undefined
      for (let idx = messages.length - 1; idx >= 0; idx -= 1) {
        const message = messages[idx]
        if (message.role === 'user' || message.role === 'tool') {
          userInput = extractMessageText(message)
          break
        }
      }

      const systemMessage = messages.find((message) => message.role === 'system')
      const systemPrompt = systemMessage ? extractMessageText(systemMessage) : undefined
      const tools = Array.isArray(payload?.tools)
        ? (payload.tools as Array<Record<string, unknown>>)
        : []
      const toolNames = tools.map(extractToolName).filter(Boolean)

      current = {
        llmCallId: String(data.llm_call_id ?? ''),
        providerKind: String(data.provider_kind ?? ''),
        apiMode: String(data.api_mode ?? ''),
        userInput,
        assistantText: '',
        toolCalls: [],
        model: payload?.model != null ? String(payload.model) : undefined,
        systemPrompt,
        toolCount: tools.length > 0 ? tools.length : undefined,
        toolNames: toolNames.length > 0 ? toolNames : undefined,
        messageCount: messages.length > 0 ? messages.length : undefined,
        temperature: typeof payload?.temperature === 'number' ? payload.temperature : undefined,
        maxOutputTokens: typeof payload?.max_tokens === 'number'
          ? payload.max_tokens
          : typeof payload?.max_output_tokens === 'number'
            ? payload.max_output_tokens
            : undefined,
      }
      turns.push(current)
      continue
    }

    if (ev.type === 'message.delta' && current) {
      const data = ev.data as Record<string, unknown>
      assistantChunks.push(String(data.content_delta ?? ''))
      continue
    }

    if (ev.type === 'tool.call' && current) {
      const data = ev.data as Record<string, unknown>
      current.toolCalls.push({
        toolCallId: String(data.tool_call_id ?? ''),
        toolName: String(data.tool_name ?? ev.tool_name ?? ''),
        argsJSON: (data.arguments as Record<string, unknown>) ?? {},
      })
      continue
    }

    if (ev.type === 'tool.result') {
      const data = ev.data as Record<string, unknown>
      resultMap[String(data.tool_call_id ?? '')] = {
        resultJSON: data.result as Record<string, unknown> | undefined,
        errorClass: ev.error_class,
      }
      continue
    }

    if ((ev.type === 'run.completed' || ev.type === 'run.failed') && current) {
      current.assistantText = assistantChunks.join('')
      assistantChunks.length = 0
      const usage = (ev.data as Record<string, unknown>).usage as Record<string, unknown> | undefined
      if (usage) {
        current.inputTokens = usage.input_tokens as number | undefined
        current.outputTokens = usage.output_tokens as number | undefined
      }
      current = null
    }
  }

  if (current && assistantChunks.length > 0) {
    current.assistantText = assistantChunks.join('')
  }

  for (const turn of turns) {
    for (const toolCall of turn.toolCalls) {
      const result = resultMap[toolCall.toolCallId]
      if (!result) continue
      toolCall.resultJSON = result.resultJSON
      toolCall.errorClass = result.errorClass
    }
  }

  return turns
}

function extractMessageText(message: Record<string, unknown>): string {
  const content = message.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part: unknown) => {
        if (typeof part === 'string') return part
        if (typeof part === 'object' && part !== null) {
          const value = part as Record<string, unknown>
          return typeof value.text === 'string' ? value.text : JSON.stringify(value)
        }
        return ''
      })
      .join('')
  }
  return JSON.stringify(content)
}
