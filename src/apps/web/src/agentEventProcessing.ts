import { canonicalToolName, pickLogicalToolName } from '@arkloop/shared'
import type { ThreadRunResponse } from './api'
import type { AgentMessage, AgentUIEvent } from './agent-ui'
import {
  agentEventDataRecord,
  agentEventToolInput,
  agentEventToolOutput,
} from './agent-ui/event-data'
import type { ArtifactRef, BrowserActionRef, CodeExecutionRef, FileOpRef, MessageThinkingRef, SubAgentRef, WebFetchRef, WidgetRef } from './storage'
import { basename, presentationForTool, truncate } from './toolPresentation'
import { contentText } from './timelineText'
import { FILE_OP_TOOL_NAMES } from './copSubSegment'

const CODE_EXECUTION_CALL_TOOL_NAMES = new Set(['python_execute', 'exec_command'])
const CODE_EXECUTION_RESULT_TOOL_NAMES = new Set(['python_execute', 'exec_command', 'continue_process', 'terminate_process'])
const TERMINAL_CONTROL_SEQUENCE_PATTERN = new RegExp(String.raw`\u001b\[[0-9;?]*[ -/]*[@-~]`, 'g')

type CodeExecutionToolCallPatch = {
  nextExecutions: CodeExecutionRef[]
  appended?: CodeExecutionRef
}

type CodeExecutionToolResultPatch = {
  nextExecutions: CodeExecutionRef[]
  updated?: CodeExecutionRef
  appended?: CodeExecutionRef
}

type CodeExecutionListPatch = {
  next: CodeExecutionRef[]
  matched: boolean
}

type CodeExecutionErrorDetails = {
  errorClass?: string
  errorMessage?: string
}

type ToolOutputFormat = {
  output?: string
  emptyLabel?: string
  errorMessage?: string
}

type CodeExecutionDeltaPatch = {
  nextExecutions: CodeExecutionRef[]
  updated?: CodeExecutionRef
}

function pickToolName(data: unknown, fallbackToolName?: string): string {
  const record = data && typeof data === 'object' && !Array.isArray(data)
    ? data as Record<string, unknown>
    : undefined
  const stableToolName = typeof record?.toolName === 'string' ? record.toolName : undefined
  return canonicalToolName(stableToolName ?? pickLogicalToolName(data, fallbackToolName))
}

function pickToolCallId(event: AgentUIEvent): string {
  const raw = agentEventDataRecord(event.data)?.toolCallId
  return typeof raw === 'string' && raw.trim() !== '' ? raw : event.id
}

export function extractArtifacts(result: unknown): ArtifactRef[] {
  if (!result || typeof result !== 'object') return []
  const artifacts = (result as { artifacts?: unknown[] }).artifacts
  if (!Array.isArray(artifacts)) return []
  return artifacts
    .filter((item): item is Record<string, unknown> => item != null && typeof item === 'object')
    .filter((item) => typeof item.key === 'string' && typeof item.filename === 'string')
    .map((item) => ({
      key: item.key as string,
      filename: item.filename as string,
      size: typeof item.size === 'number' ? item.size : 0,
      mime_type: typeof item.mime_type === 'string' ? item.mime_type : '',
      title: typeof item.title === 'string' ? item.title : undefined,
      display: item.display === 'inline' || item.display === 'panel' ? item.display as 'inline' | 'panel' : undefined,
    }))
}

export function buildMessageArtifactsFromAgentEvents(events: AgentUIEvent[]): ArtifactRef[] {
  const artifacts: ArtifactRef[] = []
  const seen = new Set<string>()
  for (const event of events) {
    if (event.type !== 'tool-result') continue
    for (const artifact of extractArtifacts(agentEventToolOutput(event.data))) {
      if (seen.has(artifact.key)) continue
      seen.add(artifact.key)
      artifacts.push(artifact)
    }
  }
  return artifacts
}

export function isWebFetchToolName(toolName: string): boolean {
  const t = canonicalToolName(toolName)
  if (!t) return false
  const n = t.toLowerCase().replace(/-/g, '_')
  if (n === 'web_fetch' || n === 'webfetch') return true
  return n.startsWith('web_fetch.')
}

function pickProcessRef(result: unknown): string | undefined {
  if (!result || typeof result !== 'object') return undefined
  const raw = (result as { process_ref?: unknown }).process_ref
  return typeof raw === 'string' && raw.trim() !== '' ? raw : undefined
}

function detectCodeExecutionLanguage(toolName: string): CodeExecutionRef['language'] | null {
  if (toolName === 'python_execute') return 'python'
  if (toolName === 'exec_command' || toolName === 'continue_process' || toolName === 'terminate_process') return 'shell'
  return null
}

function sanitizeTerminalOutput(value: string): string {
  return value.replace(TERMINAL_CONTROL_SEQUENCE_PATTERN, '')
}

function truncateForToolPreview(value: string, max = 1600): string {
  if (value.length <= max) return value
  return `${value.slice(0, max)}\n… truncated ${value.length - max} chars`
}

function pickStringField(record: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'string' && value.trim()) return value
  }
  return ''
}

function pickCommandMode(args: Record<string, unknown> | undefined): CodeExecutionRef['mode'] {
  const raw = typeof args?.mode === 'string' ? args.mode.trim() : ''
  switch (raw) {
    case 'follow':
    case 'stdin':
    case 'pty':
      return raw
    default:
      return 'buffered'
  }
}

function formatCodeExecutionItems(items: unknown): string {
  if (!Array.isArray(items)) return ''
  const ordered = items
    .filter((item): item is Record<string, unknown> => !!item && typeof item === 'object')
    .sort((left, right) => {
      const leftSeq = typeof left.seq === 'number' ? left.seq : Number.MAX_SAFE_INTEGER
      const rightSeq = typeof right.seq === 'number' ? right.seq : Number.MAX_SAFE_INTEGER
      return leftSeq - rightSeq
    })
  const segments: string[] = []
  let currentTaggedStream: 'stderr' | 'system' | null = null
  for (const item of ordered) {
    if (!item || typeof item !== 'object') continue
    const text = typeof (item as { text?: unknown }).text === 'string'
      ? sanitizeTerminalOutput((item as { text: string }).text)
      : ''
    if (!text) continue
    const stream = typeof item.stream === 'string' ? item.stream : 'stdout'
    if (stream === 'stderr' || stream === 'system') {
      if (currentTaggedStream !== stream) {
        segments.push(stream === 'stderr' ? '[stderr]\n' : '[system]\n')
        currentTaggedStream = stream
      }
    } else {
      currentTaggedStream = null
    }
    segments.push(text)
  }
  return segments.join('')
}

function extractCodeExecutionOutput(result: unknown): { output?: string; exitCode?: number } {
  if (!result || typeof result !== 'object') return {}
  const typed = result as {
    stdout?: unknown
    stderr?: unknown
    output?: unknown
    exit_code?: unknown
    items?: unknown
  }
  const exitCode = typeof typed.exit_code === 'number' ? typed.exit_code : undefined
  const stdout = typeof typed.stdout === 'string' ? sanitizeTerminalOutput(typed.stdout) : ''
  const stderr = typeof typed.stderr === 'string' ? sanitizeTerminalOutput(typed.stderr) : ''
  const fallbackOutput = typeof typed.output === 'string' ? sanitizeTerminalOutput(typed.output) : ''
  const itemOutput = formatCodeExecutionItems(typed.items)
  const rawOutput = exitCode != null && exitCode !== 0
    ? (stderr || itemOutput || stdout || fallbackOutput)
    : (stdout || itemOutput || stderr || fallbackOutput)

  return {
    output: rawOutput || undefined,
    exitCode,
  }
}

function codeExecutionEmptyLabel(language: CodeExecutionRef['language'] | null, toolName: string, status: CodeExecutionRef['status']): string | undefined {
  if (status === 'running') return undefined
  if (status === 'failed') return undefined
  if (language === 'python' || toolName === 'python_execute') return 'Execution completed with no output'
  if (language === 'shell' || toolName === 'exec_command' || toolName === 'continue_process' || toolName === 'terminate_process') {
    return 'Command completed with no stdout/stderr'
  }
  return 'Completed with no output'
}

function extractEventError(event: AgentUIEvent): CodeExecutionErrorDetails {
  const data = agentEventDataRecord(event.data)
  if (!data) {
    return {
      errorClass: typeof event.errorCode === 'string' ? event.errorCode : undefined,
      errorMessage: typeof event.errorCode === 'string' && event.errorCode === 'process.cursor_expired'
        ? 'cursor 已过期'
        : undefined,
    }
  }
  const rawError = data.error
  if (!rawError || typeof rawError !== 'object') {
    return {
      errorClass: typeof event.errorCode === 'string' ? event.errorCode : undefined,
      errorMessage: typeof event.errorCode === 'string' && event.errorCode === 'process.cursor_expired'
        ? 'cursor 已过期'
        : undefined,
    }
  }
  const typed = rawError as { errorClass?: unknown; message?: unknown }
  const errorClass = typeof typed.errorClass === 'string'
    ? typed.errorClass
    : typeof event.errorCode === 'string' ? event.errorCode : undefined
  const errorMessage = typeof typed.message === 'string'
    ? typed.message
    : errorClass === 'process.cursor_expired' ? 'cursor 已过期' : undefined
  return {
    errorClass,
    errorMessage,
  }
}

function pickExecutionRunning(result: unknown): boolean {
  if (!result || typeof result !== 'object') return false
  return (result as { running?: unknown }).running === true
}

function resolveCodeExecutionStatus(params: {
  event: AgentUIEvent
  result: unknown
  exitCode?: number
}): CodeExecutionRef['status'] {
  const { event, result, exitCode } = params
  const error = extractEventError(event)
  if (error.errorClass || error.errorMessage) {
    return 'failed'
  }
  if (result && typeof result === 'object') {
    const processStatus = (result as { status?: unknown }).status
    if (processStatus === 'terminated' || processStatus === 'timed_out' || processStatus === 'cancelled') {
      return 'failed'
    }
    if (processStatus === 'exited') {
      return exitCode === 0 ? 'success' : 'failed'
    }
  }
  // exit_code 表示会话已结束；部分后端在终态仍带 running=true，必须先认 exit_code
  if (exitCode != null) {
    return exitCode === 0 ? 'success' : 'failed'
  }
  if (pickExecutionRunning(result)) {
    return 'running'
  }
  return 'completed'
}

function mergeExecutionOutput(previous: string | undefined, incoming: string | undefined): string | undefined {
  if (!previous) return incoming
  if (!incoming) return previous
  if (previous === incoming) return previous
  if (incoming.includes(previous)) return incoming
  if (previous.includes(incoming)) return previous

  const maxOverlap = Math.min(previous.length, incoming.length)
  for (let size = maxOverlap; size > 0; size--) {
    if (previous.slice(-size) === incoming.slice(0, size)) {
      return previous + incoming.slice(size)
    }
  }
  return previous + incoming
}

function findExecutionIndex(
  executions: CodeExecutionRef[],
  params: { toolCallId?: string; processRef?: string; preferProcess: boolean },
): number {
  const { toolCallId, processRef, preferProcess } = params
  const findByProcess = () => processRef ? executions.findIndex((item) => item.processRef === processRef) : -1
  const findByCallId = () => toolCallId ? executions.findIndex((item) => item.id === toolCallId) : -1

  const primary = preferProcess ? findByProcess() : findByCallId()
  if (primary >= 0) return primary
  const secondary = preferProcess ? findByCallId() : findByProcess()
  if (secondary >= 0) return secondary

  return -1
}

function patchExecution(
  execution: CodeExecutionRef,
  params: {
    processRef?: string
    cursor?: string
    nextCursor?: string
    processStatus?: CodeExecutionRef['processStatus']
    output?: string
    emptyLabel?: string
    exitCode?: number
    status: CodeExecutionRef['status']
    errorClass?: string
    errorMessage?: string
  },
): CodeExecutionRef {
  const next: CodeExecutionRef = { ...execution }
  if (params.processRef) {
    next.processRef = params.processRef
  }
  if (params.cursor) {
    next.cursor = params.cursor
  }
  if (params.nextCursor) {
    next.nextCursor = params.nextCursor
  }
  if (params.processStatus) {
    next.processStatus = params.processStatus
  }
  const mergedOutput = mergeExecutionOutput(execution.output, params.output)
  if (mergedOutput) {
    next.output = mergedOutput
  }
  if (params.emptyLabel) {
    next.emptyLabel = params.emptyLabel
  }
  if (params.exitCode != null) {
    next.exitCode = params.exitCode
  }
  next.status = params.status
  next.errorClass = params.errorClass
  next.errorMessage = params.errorMessage
  return next
}

// applyTerminalDelta applies a terminal output delta event to update running executions.
export function applyTerminalDelta(
  executions: CodeExecutionRef[],
  event: AgentUIEvent,
): CodeExecutionDeltaPatch {
  const eventType = event.type
  if (eventType !== 'terminal-delta') {
    return { nextExecutions: executions }
  }

  const data = agentEventDataRecord(event.data)
  const processRef = typeof data?.processRef === 'string' ? data.processRef : undefined
  const chunk = typeof data?.chunk === 'string' ? data.chunk : undefined
  if (!processRef || !chunk) {
    return { nextExecutions: executions }
  }

  const targetIndex = findExecutionIndex(executions, {
    processRef,
    preferProcess: true,
  })
  if (targetIndex < 0) {
    return { nextExecutions: executions }
  }

  const target = executions[targetIndex]
  // Only update if still running (don't append output to completed executions)
  if (target.status !== 'running') {
    return { nextExecutions: executions }
  }

  const sanitizedChunk = sanitizeTerminalOutput(chunk)
  const mergedOutput = mergeExecutionOutput(target.output, sanitizedChunk)
  if (!mergedOutput || mergedOutput === target.output) {
    return { nextExecutions: executions }
  }

  const updated = patchExecution(target, {
    output: mergedOutput,
    status: 'running',
  })
  return {
    updated,
    nextExecutions: executions.map((item, index) => index === targetIndex ? updated : item),
  }
}

export function applyCodeExecutionToolCall(
  executions: CodeExecutionRef[],
  event: AgentUIEvent,
): CodeExecutionToolCallPatch {
  if (event.type !== 'tool-call') {
    return { nextExecutions: executions }
  }

  const toolName = pickToolName(event.data, event.toolName)
  if (!CODE_EXECUTION_CALL_TOOL_NAMES.has(toolName)) {
    return { nextExecutions: executions }
  }

  const language = detectCodeExecutionLanguage(toolName)
  if (!language) {
    return { nextExecutions: executions }
  }

  const args = agentEventToolInput(event.data)
  const code = typeof args?.code === 'string' ? args.code
    : typeof args?.command === 'string' ? args.command
    : undefined
  const displayDescriptionRaw = agentEventDataRecord(event.data)?.displayDescription
  const displayDescription = typeof displayDescriptionRaw === 'string' && displayDescriptionRaw.trim() !== ''
    ? displayDescriptionRaw.trim()
    : undefined
  const appended: CodeExecutionRef = {
    id: pickToolCallId(event),
    language,
    mode: toolName === 'exec_command' ? pickCommandMode(args) : undefined,
    code,
    displayDescription,
    status: 'running',
    seq: event.order,
  }
  return {
    appended,
    nextExecutions: [...executions, appended],
  }
}

export function applyCodeExecutionToolResult(
  executions: CodeExecutionRef[],
  event: AgentUIEvent,
): CodeExecutionToolResultPatch {
  if (event.type !== 'tool-result') {
    return { nextExecutions: executions }
  }

  const toolName = pickToolName(event.data, event.toolName)
  if (!CODE_EXECUTION_RESULT_TOOL_NAMES.has(toolName)) {
    return { nextExecutions: executions }
  }

  const result = agentEventToolOutput(event.data)
  const processRef = pickProcessRef(result)
  const toolCallId = pickToolCallId(event)
  const outputPatch = extractCodeExecutionOutput(result)
  const error = extractEventError(event)
  const cursor = result && typeof result === 'object' && typeof (result as { cursor?: unknown }).cursor === 'string'
    ? (result as { cursor: string }).cursor
    : undefined
  const nextCursor = result && typeof result === 'object' && typeof (result as { next_cursor?: unknown }).next_cursor === 'string'
    ? (result as { next_cursor: string }).next_cursor
    : undefined
  const processStatus = result && typeof result === 'object'
    ? ((result as { status?: unknown }).status as CodeExecutionRef['processStatus'] | undefined)
    : undefined
  const status = resolveCodeExecutionStatus({
    event,
    result,
    exitCode: outputPatch.exitCode,
  })

  const targetIndex = findExecutionIndex(executions, {
    toolCallId,
    processRef,
    preferProcess: toolName === 'continue_process' || toolName === 'terminate_process',
  })

  if (targetIndex >= 0) {
    const language = executions[targetIndex].language
    const updated = patchExecution(executions[targetIndex], {
      processRef,
      cursor,
      nextCursor,
      processStatus,
      output: outputPatch.output,
      emptyLabel: outputPatch.output ? undefined : codeExecutionEmptyLabel(language, toolName, status),
      exitCode: outputPatch.exitCode,
      status,
      errorClass: error.errorClass,
      errorMessage: error.errorMessage,
    })
    const current = executions[targetIndex]
    if (
      current.output === updated.output &&
      current.exitCode === updated.exitCode &&
      current.processRef === updated.processRef &&
      current.cursor === updated.cursor &&
      current.nextCursor === updated.nextCursor &&
      current.processStatus === updated.processStatus &&
      current.status === updated.status &&
      current.errorClass === updated.errorClass &&
      current.errorMessage === updated.errorMessage
    ) {
      return { nextExecutions: executions }
    }

    return {
      updated,
      nextExecutions: executions.map((item, index) => index === targetIndex ? updated : item),
    }
  }

  if (toolName !== 'continue_process') {
    return { nextExecutions: executions }
  }

  const language = detectCodeExecutionLanguage(toolName)
  if (!language) {
    return { nextExecutions: executions }
  }

  const appended: CodeExecutionRef = {
    id: toolCallId,
    language,
    processRef,
    cursor,
    nextCursor,
    processStatus,
    output: outputPatch.output,
    emptyLabel: outputPatch.output ? undefined : codeExecutionEmptyLabel(language, toolName, status),
    exitCode: outputPatch.exitCode,
    status,
    errorClass: error.errorClass,
    errorMessage: error.errorMessage,
  }
  return {
    appended,
    updated: appended,
    nextExecutions: [...executions, appended],
  }
}

export function buildMessageCodeExecutionsFromAgentEvents(events: AgentUIEvent[]): CodeExecutionRef[] {
  let executions: CodeExecutionRef[] = []
  for (const event of events) {
    if (event.type === 'tool-call') {
      executions = applyCodeExecutionToolCall(executions, event).nextExecutions
      continue
    }
    if (event.type === 'tool-result') {
      executions = applyCodeExecutionToolResult(executions, event).nextExecutions
      continue
    }
    if (event.type === 'terminal-delta') {
      executions = applyTerminalDelta(executions, event).nextExecutions
    }
  }
  return executions
}

export function firstVisibleCodeExecutionToolCallIndex(events: AgentUIEvent[]): number {
  return events.findIndex((event, index) => {
    if (index >= events.length - 1) return false
    if (event.type !== 'tool-call') return false
    return CODE_EXECUTION_CALL_TOOL_NAMES.has(pickToolName(event.data, event.toolName))
  })
}

export function patchCodeExecutionList(
  executions: CodeExecutionRef[],
  target: CodeExecutionRef,
): CodeExecutionListPatch {
  let matched = false
  const next = executions.map((execution) => {
    if (execution.id !== target.id) return execution
    matched = true
    return { ...execution, ...target }
  })
  return { next, matched }
}

export function shouldReplayMessageCodeExecutions(executions: CodeExecutionRef[] | null | undefined): boolean {
  if (executions == null) return true
  if (executions.length === 0) return false
  return executions.some((item) => item.language === 'shell' && item.mode !== 'buffered' && !item.processRef)
}

export function filterUnprocessedAgentEvents(params: {
  events: AgentUIEvent[]
  activeRunId: string
  processedCount: number
}): { fresh: AgentUIEvent[]; nextProcessedCount: number } {
  const { events, activeRunId } = params
  const normalizedProcessedCount = params.processedCount > events.length ? 0 : params.processedCount

  if (events.length <= normalizedProcessedCount) {
    return { fresh: [], nextProcessedCount: normalizedProcessedCount }
  }

  const slice = events.slice(normalizedProcessedCount)
  return {
    fresh: slice
      .filter((event) => event.streamId === activeRunId)
      .sort((left, right) => left.order - right.order || left.timestamp.localeCompare(right.timestamp)),
    nextProcessedCount: events.length,
  }
}

/** 首包「处理中」占位：仅此类事件表示用户可见的助手正文或工具链路（segment / thinking delta 等不算）。 */
export function shouldClearAssistantPlaceholder(event: AgentUIEvent): boolean {
  switch (event.type) {
    case 'assistant-delta': {
      const obj = agentEventDataRecord(event.data) ?? {}
      if (obj.role != null && obj.role !== 'assistant') return false
      if (obj.channel === 'thinking') return false
      return typeof obj.delta === 'string' && obj.delta.length > 0
    }
    case 'tool-call':
    case 'tool-input-delta':
    case 'tool-result':
      return true
    default:
      return false
  }
}

export function findAssistantMessageForRun(
  messages: AgentMessage[],
  runId: string | null | undefined,
): AgentMessage | undefined {
  const normalizedRunId = typeof runId === 'string' ? runId.trim() : ''
  if (!normalizedRunId) return undefined

  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index]
    if (message.role !== 'assistant') continue
    if (typeof message.streamId === 'string' && message.streamId === normalizedRunId) {
      return message
    }
  }
  return undefined
}

export function shouldRefetchCompletedRunMessages(params: {
  messages: AgentMessage[]
  latestRun: Pick<ThreadRunResponse, 'run_id' | 'status'> | null | undefined
}): boolean {
  const { messages, latestRun } = params
  if (!latestRun || latestRun.status !== 'completed') return false
  return findAssistantMessageForRun(messages, latestRun.run_id) == null
}

function extractWidgetArguments(data: unknown): { title?: string; html?: string } {
  const args = agentEventToolInput(data)
  if (!args || typeof args !== 'object') return {}
  const typed = args as Record<string, unknown>
  return {
    title: typeof typed.title === 'string' ? typed.title : undefined,
    html: typeof typed.widget_code === 'string' ? typed.widget_code : undefined,
  }
}

export function buildMessageWidgetsFromAgentEvents(events: AgentUIEvent[]): WidgetRef[] {
  const widgets: WidgetRef[] = []
  const seen = new Set<string>()

  for (const event of events) {
    if (event.type !== 'tool-call') continue
    const toolName = pickToolName(event.data, event.toolName)
    if (toolName !== 'show_widget') continue

    const { title, html } = extractWidgetArguments(event.data)
    if (!html) continue

    const id = pickToolCallId(event)
    if (seen.has(id)) continue
    seen.add(id)

    widgets.push({
      id,
      title: title?.trim() || 'Widget',
      html,
    })
  }

  return widgets
}

export function buildMessageThinkingFromAgentEvents(events: AgentUIEvent[]): MessageThinkingRef | null {
  let topLevelThinking = ''
  let activeSegmentId: string | null = null
  const segments: Array<{
    segmentId: string
    kind: string
    mode: string
    label: string
    content: string
  }> = []
  const indexBySegmentId = new Map<string, number>()

  for (const event of events) {
    if (event.type === 'segment-start') {
      const obj = agentEventDataRecord(event.data) ?? {}
      const segmentId = typeof obj?.segmentId === 'string' ? obj.segmentId : ''
      if (!segmentId) continue
      const kind = typeof obj.kind === 'string' ? obj.kind : 'planning_round'
      const display = (obj.display ?? {}) as { mode?: unknown; label?: unknown }
      const mode = typeof display.mode === 'string' ? display.mode : 'collapsed'
      const label = typeof display.label === 'string' ? display.label : ''
      const idx = segments.length
      segments.push({ segmentId, kind, mode, label, content: '' })
      indexBySegmentId.set(segmentId, idx)
      activeSegmentId = segmentId
      continue
    }

    if (event.type === 'segment-end') {
      const obj = agentEventDataRecord(event.data) ?? {}
      const segmentId = typeof obj?.segmentId === 'string' ? obj.segmentId : ''
      if (segmentId && activeSegmentId === segmentId) {
        activeSegmentId = null
      }
      continue
    }

    if (event.type !== 'assistant-delta') continue
    const obj = agentEventDataRecord(event.data) ?? {}
    if (obj.role != null && obj.role !== 'assistant') continue
    if (typeof obj.delta !== 'string' || obj.delta === '') continue
    const delta = obj.delta

    if (activeSegmentId) {
      const idx = indexBySegmentId.get(activeSegmentId)
      if (idx != null && segments[idx]) {
        segments[idx].content += delta
      }
      continue
    }

    if (obj.channel === 'thinking') {
      topLevelThinking += delta
    }
  }

  const compactSegments = segments.filter((s) => s.content.trim() !== '' && s.mode !== 'hidden')
  const thinkingText = topLevelThinking.trim()
  if (thinkingText === '' && compactSegments.length === 0) {
    return null
  }
  return {
    thinkingText: topLevelThinking,
    segments: compactSegments,
  }
}

// --- Browser action processing ---

type BrowserActionToolCallPatch = {
  nextActions: BrowserActionRef[]
  appended?: BrowserActionRef
}

type BrowserActionToolResultPatch = {
  nextActions: BrowserActionRef[]
  updated?: BrowserActionRef
}

function extractBrowserCommand(args: unknown): string {
  if (!args || typeof args !== 'object') return ''
  const raw = (args as { command?: unknown }).command
  return typeof raw === 'string' ? raw : ''
}

function extractBrowserScreenshotArtifact(result: unknown): ArtifactRef | undefined {
  if (!result || typeof result !== 'object') return undefined
  const artifacts = (result as { artifacts?: unknown[] }).artifacts
  if (!Array.isArray(artifacts)) return undefined
  const screenshot = artifacts.find((a): a is Record<string, unknown> =>
    a != null &&
    typeof a === 'object' &&
    typeof (a as Record<string, unknown>).mime_type === 'string' &&
    ((a as Record<string, unknown>).mime_type as string).startsWith('image/'),
  )
  if (!screenshot) return undefined
  return {
    key: screenshot.key as string,
    filename: typeof screenshot.filename === 'string' ? screenshot.filename : 'screenshot.png',
    size: typeof screenshot.size === 'number' ? screenshot.size : 0,
    mime_type: screenshot.mime_type as string,
  }
}

function extractBrowserOutput(result: unknown): { output?: string; exitCode?: number; url?: string } {
  if (!result || typeof result !== 'object') return {}
  const typed = result as { output?: unknown; stdout?: unknown; exit_code?: unknown; url?: unknown }
  const output = typeof typed.output === 'string' ? typed.output
    : typeof typed.stdout === 'string' ? typed.stdout
    : undefined
  const exitCode = typeof typed.exit_code === 'number' ? typed.exit_code : undefined
  const url = typeof typed.url === 'string' ? typed.url : undefined
  return { output, exitCode, url }
}

export function applyBrowserToolCall(
  actions: BrowserActionRef[],
  event: AgentUIEvent,
): BrowserActionToolCallPatch {
  if (event.type !== 'tool-call') return { nextActions: actions }
  const toolName = pickToolName(event.data, event.toolName)
  if (toolName !== 'browser') return { nextActions: actions }

  const args = agentEventToolInput(event.data)
  const command = extractBrowserCommand(args)
  const appended: BrowserActionRef = {
    id: pickToolCallId(event),
    command,
  }
  return { appended, nextActions: [...actions, appended] }
}

export function applyBrowserToolResult(
  actions: BrowserActionRef[],
  event: AgentUIEvent,
): BrowserActionToolResultPatch {
  if (event.type !== 'tool-result') return { nextActions: actions }
  const toolName = pickToolName(event.data, event.toolName)
  if (toolName !== 'browser') return { nextActions: actions }

  const result = agentEventToolOutput(event.data)
  const toolCallId = pickToolCallId(event)
  const { output, exitCode, url } = extractBrowserOutput(result)
  const screenshotArtifact = extractBrowserScreenshotArtifact(result)

  const targetIndex = actions.findIndex((a) => a.id === toolCallId)
  if (targetIndex >= 0) {
    const updated: BrowserActionRef = {
      ...actions[targetIndex],
      output,
      exitCode,
      url,
      screenshotArtifact,
    }
    return {
      updated,
      nextActions: actions.map((a, i) => i === targetIndex ? updated : a),
    }
  }

  // no matching call found — append as standalone result
  const appended: BrowserActionRef = {
    id: toolCallId,
    command: '',
    output,
    exitCode,
    url,
    screenshotArtifact,
  }
  return { updated: appended, nextActions: [...actions, appended] }
}

export function buildMessageBrowserActionsFromAgentEvents(events: AgentUIEvent[]): BrowserActionRef[] {
  let actions: BrowserActionRef[] = []
  for (const event of events) {
    if (event.type === 'tool-call') {
      actions = applyBrowserToolCall(actions, event).nextActions
    } else if (event.type === 'tool-result') {
      actions = applyBrowserToolResult(actions, event).nextActions
    }
  }
  return actions
}

// --- Sub-agent processing ---

type SubAgentToolCallPatch = {
  nextAgents: SubAgentRef[]
  appended?: SubAgentRef
}

type SubAgentToolResultPatch = {
  nextAgents: SubAgentRef[]
  updated?: SubAgentRef
}

const SUB_AGENT_CALL_TOOL_NAMES = new Set(['spawn_agent'])
const SUB_AGENT_RESULT_TOOL_NAMES = new Set([
  'spawn_agent', 'send_input', 'wait_agent', 'resume_agent', 'close_agent', 'interrupt_agent',
])

function extractSpawnArguments(data: unknown): Partial<SubAgentRef> {
  const args = agentEventToolInput(data)
  if (!args || typeof args !== 'object') return {}
  const typed = args as Record<string, unknown>
  return {
    nickname: typeof typed.nickname === 'string' ? typed.nickname : undefined,
    role: typeof typed.role === 'string' ? typed.role : undefined,
    personaId: typeof typed.persona_id === 'string' ? typed.persona_id : undefined,
    contextMode: typeof typed.context_mode === 'string' ? typed.context_mode : undefined,
    input: typeof typed.input === 'string' ? typed.input : undefined,
  }
}

function extractSubAgentResult(data: unknown): Record<string, unknown> {
  const result = agentEventToolOutput(data)
  if (!result || typeof result !== 'object') return {}
  return result as Record<string, unknown>
}

function extractSubAgentError(data: unknown): string | undefined {
  const rawError = agentEventDataRecord(data)?.error
  if (!rawError || typeof rawError !== 'object') return undefined
  const typed = rawError as { message?: unknown; errorClass?: unknown }
  if (typeof typed.message === 'string') return typed.message
  if (typeof typed.errorClass === 'string') return typed.errorClass
  return undefined
}

function hasSubAgentError(data: unknown): boolean {
  const rawError = agentEventDataRecord(data)?.error
  return rawError != null && typeof rawError === 'object'
}

function findAgentIndexByToolCallId(agents: SubAgentRef[], toolCallId: string): number {
  return agents.findIndex((a) => a.id === toolCallId)
}

function findAgentIndexBySubAgentId(agents: SubAgentRef[], subAgentId: string): number {
  return agents.findIndex((a) => a.subAgentId === subAgentId)
}

export function applySubAgentToolCall(
  agents: SubAgentRef[],
  event: AgentUIEvent,
): SubAgentToolCallPatch {
  if (event.type !== 'tool-call') return { nextAgents: agents }
  const toolName = pickToolName(event.data, event.toolName)
  if (!SUB_AGENT_CALL_TOOL_NAMES.has(toolName)) return { nextAgents: agents }

  const fields = extractSpawnArguments(event.data)
  const appended: SubAgentRef = {
    id: pickToolCallId(event),
    status: 'spawning',
    nickname: fields.nickname,
    role: fields.role,
    personaId: fields.personaId,
    contextMode: fields.contextMode,
    input: fields.input,
    seq: event.order,
  }
  return { appended, nextAgents: [...agents, appended] }
}

export function applySubAgentToolResult(
  agents: SubAgentRef[],
  event: AgentUIEvent,
): SubAgentToolResultPatch {
  if (event.type !== 'tool-result') return { nextAgents: agents }
  const toolName = pickToolName(event.data, event.toolName)
  if (!SUB_AGENT_RESULT_TOOL_NAMES.has(toolName)) return { nextAgents: agents }

  const toolCallId = pickToolCallId(event)
  const result = extractSubAgentResult(event.data)
  const errorMessage = extractSubAgentError(event.data)
  const isError = hasSubAgentError(event.data)
  const subAgentId = typeof result.sub_agent_id === 'string' ? result.sub_agent_id : undefined
  const output = typeof result.output === 'string' ? result.output : undefined
  const nickname = typeof result.nickname === 'string' ? result.nickname : undefined
  const depth = typeof result.depth === 'number' ? result.depth : undefined
  const resultStatus = typeof result.status === 'string' ? result.status : undefined

  if (toolName === 'spawn_agent') {
    const idx = findAgentIndexByToolCallId(agents, toolCallId)
    if (idx < 0) return { nextAgents: agents }
    const currentRunId = typeof result.current_run_id === 'string' ? result.current_run_id : undefined
    const updated: SubAgentRef = {
      ...agents[idx],
      subAgentId,
      output,
      depth,
      status: isError ? 'failed' : 'active',
      error: errorMessage,
      currentRunId: currentRunId ?? agents[idx].currentRunId,
    }
    if (nickname) updated.nickname = nickname
    return { updated, nextAgents: agents.map((a, i) => i === idx ? updated : a) }
  }

  // For other tools, locate by sub_agent_id in result
  const targetIdx = subAgentId ? findAgentIndexBySubAgentId(agents, subAgentId) : -1

  if (toolName === 'close_agent') {
    if (targetIdx < 0) return { nextAgents: agents }
    const updated: SubAgentRef = {
      ...agents[targetIdx],
      status: isError ? 'failed' : 'closed',
      error: errorMessage,
    }
    return { updated, nextAgents: agents.map((a, i) => i === targetIdx ? updated : a) }
  }

  if (toolName === 'interrupt_agent') {
    if (targetIdx < 0) return { nextAgents: agents }
    const updated: SubAgentRef = {
      ...agents[targetIdx],
      status: isError ? 'failed' : agents[targetIdx].status,
      error: errorMessage,
    }
    if (output) updated.output = output
    return { updated, nextAgents: agents.map((a, i) => i === targetIdx ? updated : a) }
  }

  // wait_agent, send_input, resume_agent
  if (targetIdx < 0) return { nextAgents: agents }
  const resolvedStatus = isError
    ? 'failed' as const
    : resultStatus === 'completed' ? 'completed' as const : agents[targetIdx].status
  const updated: SubAgentRef = {
    ...agents[targetIdx],
    status: resolvedStatus,
    error: errorMessage,
  }
  if (output) updated.output = output
  if (nickname) updated.nickname = nickname
  return { updated, nextAgents: agents.map((a, i) => i === targetIdx ? updated : a) }
}

export function buildMessageSubAgentsFromAgentEvents(events: AgentUIEvent[]): SubAgentRef[] {
  let agents: SubAgentRef[] = []
  for (const event of events) {
    if (event.type === 'tool-call') {
      agents = applySubAgentToolCall(agents, event).nextAgents
    } else if (event.type === 'tool-result') {
      agents = applySubAgentToolResult(agents, event).nextAgents
    }
  }
  return agents
}

// --- File operation processing ---

function normalizeFileOpToolName(toolName: string): string {
  if (toolName === 'read' || toolName.startsWith('read.')) return 'read_file'
  return toolName
}

function pickReadFilePath(args: Record<string, unknown>): string {
  const direct = typeof args.file_path === 'string' ? args.file_path : ''
  if (direct) return direct
  const source = args.source
  if (!source || typeof source !== 'object' || Array.isArray(source)) return ''
  return typeof (source as { file_path?: unknown }).file_path === 'string'
    ? (source as { file_path: string }).file_path
    : ''
}

type FileOpToolCallPatch = {
  nextOps: FileOpRef[]
  appended?: FileOpRef
}

type FileOpToolResultPatch = {
  nextOps: FileOpRef[]
  updated?: FileOpRef
}

function fileOpLabel(toolName: string, args: Record<string, unknown>): string {
  toolName = normalizeFileOpToolName(toolName)
  switch (toolName) {
    case 'grep': {
      const pattern = typeof args.pattern === 'string' ? args.pattern : ''
      const path = typeof args.path === 'string' ? args.path : ''
      const label = `grep "${truncate(pattern, 32)}"`
      return path ? `${label} in ${truncate(basename(path), 24)}` : label
    }
    case 'glob': {
      const pattern = typeof args.pattern === 'string' ? args.pattern : ''
      const path = typeof args.path === 'string' ? args.path : ''
      const label = `glob "${truncate(pattern, 32)}"`
      return path ? `${label} in ${truncate(basename(path), 24)}` : label
    }
    case 'read_file': {
      const filePath = pickReadFilePath(args)
      return filePath ? `Read ${truncate(basename(filePath), 48)}` : 'Read file'
    }
    case 'write_file': {
      const filePath = typeof args.file_path === 'string' ? args.file_path : ''
      return filePath ? truncate(basename(filePath), 48) : 'write file'
    }
    case 'edit':
    case 'edit_file': {
      const filePath = typeof args.file_path === 'string' ? args.file_path : ''
      return filePath ? truncate(basename(filePath), 48) : 'edit file'
    }
    case 'load_tools': {
      const queries = Array.isArray(args.queries)
        ? (args.queries as unknown[]).filter((q): q is string => typeof q === 'string')
        : []
      if (queries.length > 0) {
        const qs = queries.slice(0, 2).map((q) => `"${truncate(q, 24)}"`).join(', ')
        return `load_tools ${qs}${queries.length > 2 ? ', …' : ''}`
      }
      return 'load_tools'
    }
    case 'memory_write': {
      const key = typeof args.key === 'string' ? args.key : ''
      const category = typeof args.category === 'string' ? args.category : ''
      if (key) return `memory_write ${category ? category + '/' : ''}${truncate(key, 32)}`
      return 'memory_write'
    }
    case 'memory_edit': {
      const uri = typeof args.uri === 'string' ? args.uri : ''
      return uri ? `memory_edit ${truncate(uri, 40)}` : 'memory_edit'
    }
    case 'memory_search': {
      const query = typeof args.query === 'string' ? args.query : ''
      return query ? `memory_search "${truncate(query, 36)}"` : 'memory_search'
    }
    case 'memory_read': {
      const uri = typeof args.uri === 'string' ? args.uri : ''
      return uri ? `memory_read ${truncate(uri, 40)}` : 'memory_read'
    }
    case 'memory_forget': {
      const uri = typeof args.uri === 'string' ? args.uri : ''
      return uri ? `memory_forget ${truncate(uri, 40)}` : 'memory_forget'
    }
    case 'notebook_write': {
      const key = typeof args.key === 'string' ? args.key : ''
      const category = typeof args.category === 'string' ? args.category : ''
      if (key) return `notebook_write ${category ? category + '/' : ''}${truncate(key, 32)}`
      return 'notebook_write'
    }
    case 'notebook_edit': {
      const key = typeof args.key === 'string' ? args.key : ''
      const category = typeof args.category === 'string' ? args.category : ''
      if (key) return `notebook_edit ${category ? category + '/' : ''}${truncate(key, 32)}`
      return 'notebook_edit'
    }
    case 'notebook_read': {
      const uri = typeof args.uri === 'string' ? args.uri : ''
      return uri ? `notebook_read ${truncate(uri, 40)}` : 'notebook_read'
    }
    case 'notebook_forget': {
      const uri = typeof args.uri === 'string' ? args.uri : ''
      return uri ? `notebook_forget ${truncate(uri, 40)}` : 'notebook_forget'
    }
    default:
      return toolName
  }
}

function fileOpInputPreview(toolName: string, args: Record<string, unknown>): string | undefined {
  toolName = normalizeFileOpToolName(toolName)
  const pick = (key: string) => {
    const value = args[key]
    return typeof value === 'string' && value.trim() !== '' ? value : undefined
  }
  switch (toolName) {
    case 'write_file':
      return pick('content')
    case 'edit':
    case 'edit_file':
      return pick('new_string') ?? pick('replacement') ?? pick('content')
    default:
      return undefined
  }
}

function memorySearchHitsToOutput(list: unknown[]): string {
  const trimAbstract = (s: string, max: number) =>
    s.length > max ? s.slice(0, max) + '…' : s
  const maxPerLine = 280
  const maxLines = 40

  const count = list.length
  const head = `${count} result${count === 1 ? '' : 's'}`
  const lines: string[] = []

  for (const item of list.slice(0, maxLines)) {
    if (item && typeof item === 'object') {
      const o = item as Record<string, unknown>
      const abs = typeof o.abstract === 'string' ? o.abstract.trim() : ''
      if (abs) {
        lines.push(trimAbstract(abs, maxPerLine))
        continue
      }
      const uri = typeof o.uri === 'string' ? o.uri.trim() : ''
      if (uri) lines.push(trimAbstract(uri, maxPerLine))
    } else if (typeof item === 'string') {
      const t = item.trim()
      if (t) lines.push(trimAbstract(t, maxPerLine))
    }
  }

  if (lines.length === 0) return head
  const omitted = count - maxLines
  const tail = omitted > 0 ? `\n… ${omitted} more` : ''
  return `${head}\n${lines.join('\n')}${tail}`
}


function compactToolOutputLines(lines: string[], maxLines = 80): string {
  const normalized = lines.map((line) => line.trim()).filter(Boolean)
  if (normalized.length <= maxLines) return normalized.join('\n')
  return `${normalized.slice(0, maxLines).join('\n')}\n… ${normalized.length - maxLines} more`
}

function globFilesOutput(files: unknown[]): string {
  return compactToolOutputLines(files.map((file) => {
    if (typeof file === 'string') return file
    if (file && typeof file === 'object') {
      const path = (file as Record<string, unknown>).path
      return typeof path === 'string' ? path : ''
    }
    return ''
  }))
}

export function fileOpOutputFromResult(toolName: string, result: unknown): string | undefined {
  return formatFileOpResult(toolName, result).output
}

export function formatFileOpResult(toolName: string, result: unknown): ToolOutputFormat {
  toolName = normalizeFileOpToolName(toolName)
  if (!result || typeof result !== 'object') {
    switch (toolName) {
      case 'read_file': return { emptyLabel: 'Read completed; no displayable content returned' }
      case 'grep': return { emptyLabel: 'Search completed; no match data returned' }
      case 'glob': return { emptyLabel: 'Glob completed; no file list returned' }
      default: return { emptyLabel: 'Completed; no displayable output returned' }
    }
  }
  const r = result as Record<string, unknown>

  switch (toolName) {
    case 'grep': {
      const matches = Array.isArray(r.matches)
        ? (r.matches as unknown[]).map((item) => {
            if (typeof item === 'string') return item
            if (item && typeof item === 'object') {
              const o = item as Record<string, unknown>
              const path = typeof o.path === 'string' ? o.path : typeof o.file === 'string' ? o.file : ''
              const line = typeof o.line === 'number' ? `${o.line}:` : ''
              const text = typeof o.text === 'string' ? o.text : typeof o.line_text === 'string' ? o.line_text : ''
              return [path, line, text].filter(Boolean).join(path && (line || text) ? ':' : ' ')
            }
            return ''
          }).filter(Boolean).join('\n')
        : typeof r.matches === 'string' ? r.matches.trim() : ''
      const count = typeof r.count === 'number' ? r.count : matches ? matches.split('\n').filter(Boolean).length : 0
      if (count === 0) return { output: '(no matches)' }
      const body = matches ? compactToolOutputLines(matches.split('\n')) : ''
      return { output: `${count} match${count === 1 ? '' : 'es'}${body ? '\n' + body : ''}` }
    }
    case 'glob': {
      const files = Array.isArray(r.files) ? r.files as unknown[] : []
      const count = typeof r.count === 'number' ? r.count : files.length
      if (count === 0) return { output: '(no files)' }
      const body = globFilesOutput(files)
      return { output: `${count} file${count === 1 ? '' : 's'}${body ? '\n' + body : ''}` }
    }
    case 'read_file': {
      if (typeof r.content === 'string') {
        const content = r.content.trim()
        return content ? { output: truncateForToolPreview(content) } : { emptyLabel: 'Read completed; file is empty' }
      }
      const bytes = typeof r.bytes === 'number' ? r.bytes : typeof r.size === 'number' ? r.size : undefined
      const lines = typeof r.lines === 'number' ? r.lines : undefined
      const suffix = [typeof lines === 'number' ? `${lines} lines` : '', typeof bytes === 'number' ? `${bytes} bytes` : ''].filter(Boolean).join(', ')
      return { emptyLabel: suffix ? `Read completed; ${suffix}; no displayable content returned` : 'Read completed; no displayable content returned' }
    }
    case 'write_file': {
      const filePath = typeof r.file_path === 'string' ? r.file_path : ''
      return { output: filePath ? `written: ${filePath}` : 'written' }
    }
    case 'edit':
    case 'edit_file': {
      const diff = pickStringField(r, ['diff', 'patch', 'unified_diff'])
      if (diff) return { output: truncateForToolPreview(diff, 4000) }
      const filePath = typeof r.file_path === 'string' ? r.file_path : ''
      return { output: filePath ? `edited: ${filePath}` : 'edited' }
    }
    case 'load_tools': {
      const matched = Array.isArray(r.matched) ? r.matched as unknown[] : []
      const count = typeof r.count === 'number' ? r.count : matched.length
      if (count === 0 && matched.length === 0) return { output: '(no matches)' }
      const statusSummary = summarizeLoadToolsResult(r)
      if (statusSummary) return { output: statusSummary }
      const names = matched.slice(0, 5).map((m) => {
        if (typeof m === 'string') return m
        if (m && typeof m === 'object') return String((m as Record<string, unknown>).name ?? '')
        return ''
      }).filter(Boolean)
      return { output: `${count} match${count === 1 ? '' : 'es'}${names.length > 0 ? ': ' + names.join(', ') : ''}` }
    }
    case 'memory_write': {
      const stored = typeof r.stored === 'boolean' ? r.stored : true
      return { output: stored ? 'stored' : 'failed' }
    }
    case 'memory_edit': {
      return { output: 'updated' }
    }
    case 'memory_search': {
      const list = Array.isArray(r.hits)
        ? (r.hits as unknown[])
        : Array.isArray(r.results)
          ? (r.results as unknown[])
          : []
      const count = list.length
      if (count === 0) return { output: '(no results)' }
      return { output: memorySearchHitsToOutput(list) }
    }
    case 'memory_read': {
      const content = typeof r.content === 'string' ? r.content.trim() : ''
      return content ? { output: content.slice(0, 80) + (content.length > 80 ? '…' : '') } : { output: 'read', emptyLabel: 'Memory read completed; no text returned' }
    }
    case 'memory_forget': {
      return { output: 'forgotten' }
    }
    case 'notebook_write': {
      return { output: 'saved' }
    }
    case 'notebook_edit': {
      return { output: 'updated' }
    }
    case 'notebook_read': {
      const content = typeof r.content === 'string' ? r.content.trim() : ''
      return content ? { output: content.slice(0, 80) + (content.length > 80 ? '…' : '') } : { output: 'read', emptyLabel: 'Notebook read completed; no text returned' }
    }
    case 'notebook_forget': {
      return { output: 'deleted' }
    }
    default:
      return { emptyLabel: 'Completed; no displayable output returned' }
  }
}

function summarizeLoadToolsResult(result: Record<string, unknown>): string | undefined {
  const matched = Array.isArray(result.matched) ? result.matched as unknown[] : []
  if (matched.length === 0) return undefined

  const hasStateInfo = matched.some((entry) => {
    if (!entry || typeof entry !== 'object') return false
    const typed = entry as Record<string, unknown>
    return (
      typeof typed.state === 'string'
      || typed.already_active === true
      || typed.already_loaded === true
    )
  })
  if (!hasStateInfo) return undefined

  const counts = new Map<string, { count: number; names: string[] }>()

  for (const entry of matched) {
    let name: string | undefined
    let rawState: string | undefined

    if (typeof entry === 'string') {
      name = entry
    } else if (entry && typeof entry === 'object') {
      const typed = entry as Record<string, unknown>
      if (typeof typed.name === 'string') {
        name = typed.name
      }
      if (typeof typed.state === 'string') {
        rawState = typed.state
      }
      if (!rawState && typed.already_active === true) {
        rawState = 'already_active'
      } else if (!rawState && typed.already_loaded === true) {
        rawState = 'already_loaded'
      }
    }

    const state = rawState === 'activated' ? 'loaded' : rawState || 'loaded'
    const bucket = counts.get(state) ?? { count: 0, names: [] }
    bucket.count += 1
    if (name && bucket.names.length < 2) {
      bucket.names.push(name)
    }
    counts.set(state, bucket)
  }

  if (counts.size === 0) return undefined

  const stateOrder = ['loaded', 'already_loaded', 'already_active', 'available']
  const stateLabels: Record<string, string> = {
    loaded: 'loaded',
    already_loaded: 'already loaded',
    already_active: 'already active',
    available: 'available',
  }

  const orderedStates = [
    ...stateOrder.filter((state) => counts.has(state)),
    ...[...counts.keys()].filter((state) => !stateOrder.includes(state)),
  ]

  const parts = orderedStates.map((state) => {
    const bucket = counts.get(state)
    if (!bucket) return undefined
    const sample = bucket.names.length > 0 ? ` (${bucket.names.join(', ')}${bucket.names.length < bucket.count ? ', …' : ''})` : ''
    const label = stateLabels[state] ?? state
    return `${label} ${bucket.count}${sample}`
  }).filter(Boolean)

  return parts.length > 0 ? parts.join('; ') : undefined
}

export function applyFileOpToolCall(
  ops: FileOpRef[],
  event: AgentUIEvent,
): FileOpToolCallPatch {
  if (event.type !== 'tool-call') return { nextOps: ops }
  const rawToolName = pickToolName(event.data, event.toolName)
  const toolName = normalizeFileOpToolName(rawToolName)
  if (!FILE_OP_TOOL_NAMES.has(rawToolName) && !FILE_OP_TOOL_NAMES.has(toolName)) return { nextOps: ops }

  const args = agentEventToolInput(event.data) ?? {}
  const eventDisplayDescription = agentEventDataRecord(event.data)?.displayDescription
  const overrideLabel = typeof eventDisplayDescription === 'string' && eventDisplayDescription.trim() !== ''
    ? eventDisplayDescription.trim()
    : undefined
  const fallbackLabel = fileOpLabel(toolName, args)
  const presentation = presentationForTool(toolName, args, fallbackLabel)
  const inputPreview = fileOpInputPreview(toolName, args)
  // write_file: diff = content line count
  let diffAdded: number | undefined
  let diffRemoved: number | undefined
  if (toolName === 'write_file' && typeof args.content === 'string') {
    diffAdded = args.content.replace(/\r\n/g, '\n').split('\n').length
    diffRemoved = 0
  }

  const appended: FileOpRef = {
    id: pickToolCallId(event),
    toolName,
    label: overrideLabel ?? presentation.description,
    status: 'running',
    seq: event.order,
    filePath: pickReadFilePath(args) || (typeof args.file_path === 'string' ? args.file_path : undefined),
    pattern: typeof args.pattern === 'string' ? args.pattern : typeof args.query === 'string' ? args.query : undefined,
    operation: typeof args.operation === 'string' ? args.operation : undefined,
    displayKind: presentation.kind,
    displayDescription: overrideLabel ?? presentation.description,
    displayText: overrideLabel ? contentText(overrideLabel) : presentation.text,
    displaySubject: presentation.subject,
    displayDetail: presentation.detail,
    ...(inputPreview ? { output: inputPreview } : {}),
    ...(diffAdded !== undefined ? { diffAdded, diffRemoved } : {}),
  }
  return { appended, nextOps: [...ops, appended] }
}

export function applyFileOpToolResult(
  ops: FileOpRef[],
  event: AgentUIEvent,
): FileOpToolResultPatch {
  if (event.type !== 'tool-result') return { nextOps: ops }
  const rawToolName = pickToolName(event.data, event.toolName)
  const toolName = normalizeFileOpToolName(rawToolName)
  if (!FILE_OP_TOOL_NAMES.has(rawToolName) && !FILE_OP_TOOL_NAMES.has(toolName)) return { nextOps: ops }

  const toolCallId = pickToolCallId(event)
  const result = agentEventToolOutput(event.data)
  const error = extractEventError(event)
  const hasError = !!(error.errorClass || error.errorMessage)

  const targetIdx = ops.findIndex((o) => o.id === toolCallId)
  if (targetIdx < 0) return { nextOps: ops }

  const formatted = hasError ? {} : formatFileOpResult(toolName, result)
  // edit / edit_file: parse diff counts from result output
  let resultDiffAdded: number | undefined
  let resultDiffRemoved: number | undefined
  if (!hasError && (toolName === 'edit' || toolName === 'edit_file') && typeof formatted.output === 'string') {
    let add = 0
    let rm = 0
    for (const line of formatted.output.replace(/\r\n/g, '\n').split('\n')) {
      if (line.startsWith('+++') || line.startsWith('---')) continue
      if (line.startsWith('+')) add += 1
      else if (line.startsWith('-')) rm += 1
    }
    if (add > 0 || rm > 0) { resultDiffAdded = add; resultDiffRemoved = rm }
  }
  const updated: FileOpRef = {
    ...ops[targetIdx],
    status: hasError ? 'failed' : 'success',
    ...(formatted.output ? { output: formatted.output } : {}),
    ...(formatted.emptyLabel ? { emptyLabel: formatted.emptyLabel } : {}),
    ...(hasError ? { errorMessage: error.errorMessage ?? error.errorClass } : {}),
    ...(resultDiffAdded !== undefined ? { diffAdded: resultDiffAdded, diffRemoved: resultDiffRemoved } : {}),
  }
  return {
    updated,
    nextOps: ops.map((o, i) => i === targetIdx ? updated : o),
  }
}

export function buildMessageFileOpsFromAgentEvents(events: AgentUIEvent[]): FileOpRef[] {
  let ops: FileOpRef[] = []
  for (const event of events) {
    if (event.type === 'tool-call') {
      ops = applyFileOpToolCall(ops, event).nextOps
    } else if (event.type === 'tool-result') {
      ops = applyFileOpToolResult(ops, event).nextOps
    }
  }
  return ops
}

// --- Web Fetch processing ---

type WebFetchToolCallPatch = {
  nextFetches: WebFetchRef[]
  appended?: WebFetchRef
}

type WebFetchToolResultPatch = {
  nextFetches: WebFetchRef[]
  updated?: WebFetchRef
}

export function applyWebFetchToolCall(
  fetches: WebFetchRef[],
  event: AgentUIEvent,
): WebFetchToolCallPatch {
  if (event.type !== 'tool-call') return { nextFetches: fetches }
  const toolName = pickToolName(event.data, event.toolName)
  if (!isWebFetchToolName(toolName)) return { nextFetches: fetches }

  const args = agentEventToolInput(event.data) ?? {}
  const url = typeof args.url === 'string' ? args.url : ''
  const appended: WebFetchRef = {
    id: pickToolCallId(event),
    url,
    status: 'fetching',
    seq: event.order,
  }
  return { appended, nextFetches: [...fetches, appended] }
}

export function applyWebFetchToolResult(
  fetches: WebFetchRef[],
  event: AgentUIEvent,
): WebFetchToolResultPatch {
  if (event.type !== 'tool-result') return { nextFetches: fetches }
  const toolName = pickToolName(event.data, event.toolName)
  if (!isWebFetchToolName(toolName)) return { nextFetches: fetches }

  const toolCallId = pickToolCallId(event)
  const result = agentEventToolOutput(event.data) as Record<string, unknown> | undefined
  const error = extractEventError(event)
  const hasError = !!(event.errorCode || error.errorClass || error.errorMessage)
  const title = typeof result?.title === 'string' ? result.title : undefined
  const statusCode = typeof result?.status_code === 'number' ? result.status_code : undefined

  const targetIdx = fetches.findIndex((f) => f.id === toolCallId)
  if (targetIdx < 0) return { nextFetches: fetches }

  const updated: WebFetchRef = {
    ...fetches[targetIdx],
    title,
    statusCode,
    status: hasError ? 'failed' : 'done',
    ...(hasError ? { errorMessage: error.errorMessage ?? error.errorClass ?? event.errorCode } : {}),
  }
  return {
    updated,
    nextFetches: fetches.map((f, i) => i === targetIdx ? updated : f),
  }
}

export function buildMessageWebFetchesFromAgentEvents(events: AgentUIEvent[]): WebFetchRef[] {
  let fetches: WebFetchRef[] = []
  for (const event of events) {
    if (event.type === 'tool-call') {
      fetches = applyWebFetchToolCall(fetches, event).nextFetches
    } else if (event.type === 'tool-result') {
      fetches = applyWebFetchToolResult(fetches, event).nextFetches
    }
  }
  return fetches
}

export function buildTodosFromAgentEvents(
  events: AgentUIEvent[],
): Array<{ id: string; content: string; activeForm?: string; status: string }> {
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i]
    if (event.type !== 'todo-updated') continue
    const obj = agentEventDataRecord(event.data)
    if (!Array.isArray(obj?.todos)) continue
    return (obj.todos as unknown[]).flatMap((t) => {
      if (!t || typeof t !== 'object') return []
      const item = t as { id?: unknown; content?: unknown; status?: unknown; activeForm?: unknown }
      if (
        typeof item.id !== 'string' ||
        typeof item.content !== 'string' ||
        typeof item.status !== 'string'
      )
        return []
      const activeForm = typeof item.activeForm === 'string'
        ? item.activeForm.trim()
        : ''
      return [{ id: item.id, content: item.content, ...(activeForm ? { activeForm } : {}), status: item.status }]
    })
  }
  return []
}
