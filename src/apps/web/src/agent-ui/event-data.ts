import type {
  AgentContextCompactData,
  AgentInputRequestData,
  AgentRunErrorData,
  AgentSegmentDisplayData,
  AgentTodoItemData,
  AgentToolResultErrorData,
  AgentUIEventData,
  AgentUIEventType,
} from './contract'

export function normalizeAgentEventType(type: string): AgentUIEventType {
  switch (type) {
    case 'message.delta':
      return 'assistant-delta'
    case 'tool.call.delta':
      return 'tool-input-delta'
    case 'tool.call':
      return 'tool-call'
    case 'tool.result':
      return 'tool-result'
    case 'terminal.stdout_delta':
    case 'terminal.stderr_delta':
      return 'terminal-delta'
    case 'run.segment.start':
      return 'segment-start'
    case 'run.segment.end':
      return 'segment-end'
    case 'run.context_compact':
      return 'context-compact'
    case 'run.input_requested':
      return 'input-request'
    case 'run.completed':
      return 'run-completed'
    case 'run.failed':
      return 'run-failed'
    case 'run.cancelled':
      return 'run-cancelled'
    case 'run.interrupted':
      return 'run-interrupted'
    case 'thread.title.updated':
      return 'thread-title'
    case 'thread.collaboration_mode.updated':
    case 'thread.collaboration.updated':
      return 'thread-collaboration'
    case 'todo.updated':
      return 'todo-updated'
    default:
      return type
  }
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function stringField(record: Record<string, unknown> | undefined, ...keys: string[]): string | undefined {
  for (const key of keys) {
    const value = record?.[key]
    if (typeof value === 'string') return value
  }
  return undefined
}

function numberField(record: Record<string, unknown> | undefined, ...keys: string[]): number | undefined {
  for (const key of keys) {
    const value = record?.[key]
    if (typeof value === 'number' && Number.isFinite(value)) return value
  }
  return undefined
}

function objectField(record: Record<string, unknown> | undefined, ...keys: string[]): Record<string, unknown> | undefined {
  for (const key of keys) {
    const value = asRecord(record?.[key])
    if (value) return value
  }
  return undefined
}

function normalizeDetails(value: unknown): Record<string, unknown> | undefined {
  return asRecord(value)
}

function normalizeToolName(record: Record<string, unknown> | undefined, fallback?: string): string | undefined {
  return stringField(record, 'toolName', 'tool_name', 'resolved_tool_name') ?? fallback
}

function normalizeSegmentDisplay(value: unknown): AgentSegmentDisplayData | undefined {
  const record = asRecord(value)
  if (!record) return undefined
  const queriesRaw = record.queries
  const queries = Array.isArray(queriesRaw)
    ? queriesRaw.filter((item): item is string => typeof item === 'string')
    : undefined
  return {
    ...(typeof record.mode === 'string' ? { mode: record.mode } : {}),
    ...(typeof record.label === 'string' ? { label: record.label } : {}),
    ...(queries && queries.length > 0 ? { queries } : {}),
  }
}

function normalizeToolError(value: unknown, fallbackErrorCode?: string): AgentToolResultErrorData | undefined {
  const record = asRecord(value)
  const normalized = {
    ...(typeof record?.errorClass === 'string' ? { errorClass: record.errorClass } : {}),
    ...(typeof record?.error_class === 'string' ? { errorClass: record.error_class } : {}),
    ...(typeof record?.message === 'string' ? { message: record.message } : {}),
    ...(typeof record?.code === 'string' ? { code: record.code } : {}),
    ...(normalizeDetails(record?.details) ? { details: normalizeDetails(record?.details) } : {}),
  }
  if (Object.keys(normalized).length > 0) return normalized
  return fallbackErrorCode ? { errorClass: fallbackErrorCode } : undefined
}

function normalizeRunError(value: unknown, fallbackErrorCode?: string): AgentRunErrorData {
  const record = asRecord(value)
  return {
    ...(typeof record?.message === 'string' ? { message: record.message } : {}),
    ...(typeof record?.code === 'string' ? { code: record.code } : {}),
    ...(typeof record?.errorClass === 'string' ? { errorClass: record.errorClass } : {}),
    ...(typeof record?.error_class === 'string' ? { errorClass: record.error_class } : {}),
    ...(typeof record?.traceId === 'string' ? { traceId: record.traceId } : {}),
    ...(typeof record?.trace_id === 'string' ? { traceId: record.trace_id } : {}),
    ...(normalizeDetails(record?.details) ? { details: normalizeDetails(record?.details) } : {}),
    ...(fallbackErrorCode ? { errorClass: fallbackErrorCode } : {}),
  }
}

function normalizeTodos(value: unknown): AgentTodoItemData[] {
  const raw = asRecord(value)?.todos
  if (!Array.isArray(raw)) return []
  return raw.flatMap((item) => {
    const record = asRecord(item)
    if (!record) return []
    const id = stringField(record, 'id')
    const content = stringField(record, 'content')
    const status = stringField(record, 'status')
    if (!id || !content || !status) return []
    const activeForm = stringField(record, 'activeForm', 'active_form')?.trim()
    return [{
      id,
      content,
      status,
      ...(activeForm ? { activeForm } : {}),
    }]
  })
}

function normalizeContextCompact(value: unknown): AgentContextCompactData {
  const record = asRecord(value)
  return {
    ...(stringField(record, 'op') ? { op: stringField(record, 'op') } : {}),
    ...(stringField(record, 'phase') ? { phase: stringField(record, 'phase') } : {}),
    ...(numberField(record, 'droppedPrefix', 'dropped_prefix') != null
      ? { droppedPrefix: numberField(record, 'droppedPrefix', 'dropped_prefix') }
      : {}),
  }
}

function normalizeInputRequest(value: unknown): AgentInputRequestData {
  const record = asRecord(value)
  return {
    ...(stringField(record, 'requestId', 'request_id') ? { requestId: stringField(record, 'requestId', 'request_id') } : {}),
    ...(stringField(record, 'message') ? { message: stringField(record, 'message') } : {}),
    ...(record && 'requestedSchema' in record ? { requestedSchema: record.requestedSchema } : {}),
  }
}

export function normalizeAgentEventData(params: {
  type: AgentUIEventType
  rawType?: string
  eventId: string
  data: unknown
  toolName?: string
  errorCode?: string
}): AgentUIEventData {
  const { type, rawType, eventId, data, toolName, errorCode } = params
  const record = asRecord(data)

  switch (type) {
    case 'assistant-delta':
      return {
        ...(stringField(record, 'role') ? { role: stringField(record, 'role') } : {}),
        ...(stringField(record, 'channel') ? { channel: stringField(record, 'channel') } : {}),
        delta: stringField(record, 'delta', 'content_delta') ?? '',
      }
    case 'tool-input-delta':
      return {
        ...(numberField(record, 'toolCallIndex', 'tool_call_index') != null
          ? { toolCallIndex: numberField(record, 'toolCallIndex', 'tool_call_index') }
          : {}),
        ...(stringField(record, 'toolCallId', 'tool_call_id') ? { toolCallId: stringField(record, 'toolCallId', 'tool_call_id') } : {}),
        ...(normalizeToolName(record) ? { toolName: normalizeToolName(record) } : {}),
        delta: stringField(record, 'delta', 'arguments_delta') ?? '',
      }
    case 'tool-call': {
      const resolvedToolName = normalizeToolName(record, toolName) ?? 'tool'
      return {
        toolCallId: stringField(record, 'toolCallId', 'tool_call_id') ?? eventId,
        ...(numberField(record, 'toolCallIndex', 'tool_call_index') != null
          ? { toolCallIndex: numberField(record, 'toolCallIndex', 'tool_call_index') }
          : {}),
        toolName: resolvedToolName,
        input: record && 'input' in record ? record.input : record?.arguments,
        ...(stringField(record, 'displayDescription', 'display_description')
          ? { displayDescription: stringField(record, 'displayDescription', 'display_description') }
          : {}),
        ...(stringField(record, 'llmName', 'llm_name') ? { llmName: stringField(record, 'llmName', 'llm_name') } : {}),
      }
    }
    case 'tool-result':
      return {
        toolCallId: stringField(record, 'toolCallId', 'tool_call_id') ?? eventId,
        ...(normalizeToolName(record, toolName) ? { toolName: normalizeToolName(record, toolName) } : {}),
        output: record && 'output' in record ? record.output : record?.result,
        ...(normalizeToolError(record?.error, errorCode)
          ? { error: normalizeToolError(record?.error, errorCode) }
          : {}),
      }
    case 'terminal-delta': {
      const stream = rawType === 'terminal.stderr_delta'
        ? 'stderr'
        : rawType === 'terminal.stdout_delta'
          ? 'stdout'
          : stringField(record, 'stream') === 'stderr' ? 'stderr' : 'stdout'
      return {
        ...(stringField(record, 'processRef', 'process_ref') ? { processRef: stringField(record, 'processRef', 'process_ref') } : {}),
        ...(stringField(record, 'chunk') ? { chunk: stringField(record, 'chunk') } : {}),
        stream,
      }
    }
    case 'segment-start':
      return {
        segmentId: stringField(record, 'segmentId', 'segment_id') ?? '',
        kind: stringField(record, 'kind', 'type') ?? 'planning_round',
        ...(normalizeSegmentDisplay(record?.display) ? { display: normalizeSegmentDisplay(record?.display) } : {}),
      }
    case 'segment-end':
      return {
        segmentId: stringField(record, 'segmentId', 'segment_id') ?? '',
      }
    case 'context-compact':
      return normalizeContextCompact(data)
    case 'input-request':
      return normalizeInputRequest(data)
    case 'thread-title':
      return {
        ...(stringField(record, 'threadId', 'thread_id') ? { threadId: stringField(record, 'threadId', 'thread_id') } : {}),
        ...(stringField(record, 'title') ? { title: stringField(record, 'title') } : {}),
      }
    case 'thread-collaboration':
      return {
        ...(stringField(record, 'threadId', 'thread_id') ? { threadId: stringField(record, 'threadId', 'thread_id') } : {}),
        ...(stringField(record, 'collaborationMode', 'collaboration_mode')
          ? { collaborationMode: stringField(record, 'collaborationMode', 'collaboration_mode') }
          : {}),
        ...(numberField(record, 'collaborationModeRevision', 'collaboration_mode_revision') != null
          ? { collaborationModeRevision: numberField(record, 'collaborationModeRevision', 'collaboration_mode_revision') }
          : {}),
      }
    case 'todo-updated':
      return { todos: normalizeTodos(data) }
    case 'run-failed':
    case 'run-interrupted':
      return normalizeRunError(data, errorCode)
    default:
      return data as AgentUIEventData
  }
}

export function normalizeAgentEventToolName(params: {
  type: AgentUIEventType
  data: AgentUIEventData
  fallback?: string
}): string | undefined {
  if (params.type !== 'tool-call' && params.type !== 'tool-result' && params.type !== 'tool-input-delta') {
    return params.fallback
  }
  return normalizeToolName(asRecord(params.data), params.fallback)
}

export function agentEventDataRecord(data: unknown): Record<string, unknown> | undefined {
  return asRecord(data)
}

export function agentEventToolInput(data: unknown): Record<string, unknown> | undefined {
  const record = asRecord(data)
  return objectField(record, 'input', 'arguments')
}

export function agentEventToolOutput(data: unknown): unknown {
  const record = asRecord(data)
  if (!record) return undefined
  return 'output' in record ? record.output : record.result
}
