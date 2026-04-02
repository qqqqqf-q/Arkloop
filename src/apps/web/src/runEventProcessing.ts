import { isACPDelegateEventData } from '@arkloop/shared'
import type { MessageResponse, ThreadRunResponse } from './api'
import type { RunEvent } from './sse'
import type { ArtifactRef, BrowserActionRef, CodeExecutionRef, FileOpRef, MessageThinkingRef, SubAgentRef, WebFetchRef, WidgetRef } from './storage'

const CODE_EXECUTION_TOOL_NAMES = new Set(['python_execute', 'exec_command'])
const CODE_EXECUTION_RESULT_TOOL_NAMES = new Set(['python_execute', 'exec_command', 'write_stdin'])
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

type CodeExecutionDeltaPatch = {
  nextExecutions: CodeExecutionRef[]
  updated?: CodeExecutionRef
}

function pickToolName(data: unknown): string {
  if (!data || typeof data !== 'object') return ''
  const raw = (data as { tool_name?: unknown }).tool_name
  return typeof raw === 'string' ? raw : ''
}

function pickToolCallId(event: RunEvent): string {
  if (!event.data || typeof event.data !== 'object') return event.event_id
  const raw = (event.data as { tool_call_id?: unknown }).tool_call_id
  return typeof raw === 'string' && raw.trim() !== '' ? raw : event.event_id
}

export function isWebFetchToolName(toolName: string): boolean {
  const t = toolName.trim()
  if (!t) return false
  const n = t.toLowerCase().replace(/-/g, '_')
  if (n === 'web_fetch' || n === 'webfetch') return true
  return n.startsWith('web_fetch.')
}

function pickSessionId(result: unknown): string | undefined {
  if (!result || typeof result !== 'object') return undefined
  const raw = (result as { session_id?: unknown }).session_id
  return typeof raw === 'string' && raw.trim() !== '' ? raw : undefined
}

function detectCodeExecutionLanguage(toolName: string): CodeExecutionRef['language'] | null {
  if (toolName === 'python_execute') return 'python'
  if (toolName === 'exec_command' || toolName === 'write_stdin') return 'shell'
  return null
}

function sanitizeTerminalOutput(value: string): string {
  return value.replace(TERMINAL_CONTROL_SEQUENCE_PATTERN, '')
}

function extractCodeExecutionOutput(result: unknown): { output?: string; exitCode?: number } {
  if (!result || typeof result !== 'object') return {}
  const typed = result as {
    stdout?: unknown
    stderr?: unknown
    output?: unknown
    exit_code?: unknown
  }
  const exitCode = typeof typed.exit_code === 'number' ? typed.exit_code : undefined
  const stdout = typeof typed.stdout === 'string' ? sanitizeTerminalOutput(typed.stdout) : ''
  const stderr = typeof typed.stderr === 'string' ? sanitizeTerminalOutput(typed.stderr) : ''
  const fallbackOutput = typeof typed.output === 'string' ? sanitizeTerminalOutput(typed.output) : ''
  const rawOutput = exitCode != null && exitCode !== 0
    ? (stderr || stdout || fallbackOutput)
    : (stdout || stderr || fallbackOutput)

  return {
    output: rawOutput || undefined,
    exitCode,
  }
}

function extractCodeExecutionError(event: RunEvent): CodeExecutionErrorDetails {
  if (!event.data || typeof event.data !== 'object') {
    return {
      errorClass: typeof event.error_class === 'string' ? event.error_class : undefined,
    }
  }
  const rawError = (event.data as { error?: unknown }).error
  if (!rawError || typeof rawError !== 'object') {
    return {
      errorClass: typeof event.error_class === 'string' ? event.error_class : undefined,
    }
  }
  const typed = rawError as { error_class?: unknown; message?: unknown }
  return {
    errorClass: typeof typed.error_class === 'string'
      ? typed.error_class
      : typeof event.error_class === 'string' ? event.error_class : undefined,
    errorMessage: typeof typed.message === 'string' ? typed.message : undefined,
  }
}

function pickExecutionRunning(result: unknown): boolean {
  if (!result || typeof result !== 'object') return false
  return (result as { running?: unknown }).running === true
}

function resolveCodeExecutionStatus(params: {
  event: RunEvent
  result: unknown
  exitCode?: number
}): CodeExecutionRef['status'] {
  const { event, result, exitCode } = params
  const error = extractCodeExecutionError(event)
  if (error.errorClass || error.errorMessage) {
    return 'failed'
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
  params: { toolCallId?: string; sessionId?: string; preferSession: boolean },
): number {
  const { toolCallId, sessionId, preferSession } = params
  const findBySession = () => sessionId ? executions.findIndex((item) => item.sessionId === sessionId) : -1
  const findByCallId = () => toolCallId ? executions.findIndex((item) => item.id === toolCallId) : -1

  const primary = preferSession ? findBySession() : findByCallId()
  if (primary >= 0) return primary
  const secondary = preferSession ? findByCallId() : findBySession()
  if (secondary >= 0) return secondary

  // write_stdin fallback: match last shell entry still awaiting output
  if (preferSession) {
    for (let i = executions.length - 1; i >= 0; i--) {
      if (executions[i].language === 'shell' && executions[i].status === 'running') {
        return i
      }
    }
  }
  return -1
}

function patchExecution(
  execution: CodeExecutionRef,
  params: {
    sessionId?: string
    output?: string
    exitCode?: number
    status: CodeExecutionRef['status']
    errorClass?: string
    errorMessage?: string
  },
): CodeExecutionRef {
  const next: CodeExecutionRef = { ...execution }
  if (params.sessionId) {
    next.sessionId = params.sessionId
  }
  const mergedOutput = mergeExecutionOutput(execution.output, params.output)
  if (mergedOutput) {
    next.output = mergedOutput
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
  event: RunEvent,
): CodeExecutionDeltaPatch {
  const eventType = event.type
  if (eventType !== 'terminal.stdout_delta' && eventType !== 'terminal.stderr_delta') {
    return { nextExecutions: executions }
  }
  if (isACPDelegateEventData(event.data)) {
    return { nextExecutions: executions }
  }

  const data = event.data as { session_ref?: unknown; chunk?: unknown }
  const sessionRef = typeof data?.session_ref === 'string' ? data.session_ref : undefined
  const chunk = typeof data?.chunk === 'string' ? data.chunk : undefined
  if (!sessionRef || !chunk) {
    return { nextExecutions: executions }
  }

  const targetIndex = findExecutionIndex(executions, {
    sessionId: sessionRef,
    preferSession: true,
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
  event: RunEvent,
): CodeExecutionToolCallPatch {
  if (event.type !== 'tool.call') {
    return { nextExecutions: executions }
  }
  if (isACPDelegateEventData(event.data)) {
    return { nextExecutions: executions }
  }

  const toolName = pickToolName(event.data)
  if (!CODE_EXECUTION_TOOL_NAMES.has(toolName)) {
    return { nextExecutions: executions }
  }

  const language = detectCodeExecutionLanguage(toolName)
  if (!language) {
    return { nextExecutions: executions }
  }

  const args = event.data && typeof event.data === 'object'
    ? (event.data as { arguments?: unknown }).arguments as Record<string, unknown> | undefined
    : undefined
  const code = typeof args?.code === 'string' ? args.code
    : typeof args?.command === 'string' ? args.command
    : undefined
  const appended: CodeExecutionRef = {
    id: pickToolCallId(event),
    language,
    code,
    status: 'running',
    seq: event.seq,
  }
  return {
    appended,
    nextExecutions: [...executions, appended],
  }
}

export function applyCodeExecutionToolResult(
  executions: CodeExecutionRef[],
  event: RunEvent,
): CodeExecutionToolResultPatch {
  if (event.type !== 'tool.result') {
    return { nextExecutions: executions }
  }
  if (isACPDelegateEventData(event.data)) {
    return { nextExecutions: executions }
  }

  const toolName = pickToolName(event.data)
  if (!CODE_EXECUTION_RESULT_TOOL_NAMES.has(toolName)) {
    return { nextExecutions: executions }
  }

  const data = event.data && typeof event.data === 'object'
    ? event.data as { result?: unknown; tool_call_id?: unknown }
    : undefined
  const result = data?.result
  const sessionId = pickSessionId(result)
  const toolCallId = pickToolCallId(event)
  const outputPatch = extractCodeExecutionOutput(result)
  const error = extractCodeExecutionError(event)
  const status = resolveCodeExecutionStatus({
    event,
    result,
    exitCode: outputPatch.exitCode,
  })

  const targetIndex = findExecutionIndex(executions, {
    toolCallId,
    sessionId,
    preferSession: toolName === 'write_stdin',
  })

  if (targetIndex >= 0) {
    const updated = patchExecution(executions[targetIndex], {
      sessionId,
      output: outputPatch.output,
      exitCode: outputPatch.exitCode,
      status,
      errorClass: error.errorClass,
      errorMessage: error.errorMessage,
    })
    const current = executions[targetIndex]
    if (
      current.output === updated.output &&
      current.exitCode === updated.exitCode &&
      current.sessionId === updated.sessionId &&
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

  if (toolName !== 'write_stdin') {
    return { nextExecutions: executions }
  }

  const language = detectCodeExecutionLanguage(toolName)
  if (!language) {
    return { nextExecutions: executions }
  }

  const appended: CodeExecutionRef = {
    id: toolCallId,
    language,
    sessionId,
    output: outputPatch.output,
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

export function buildMessageCodeExecutionsFromRunEvents(events: RunEvent[]): CodeExecutionRef[] {
  let executions: CodeExecutionRef[] = []
  for (const event of events) {
    if (event.type === 'tool.call') {
      executions = applyCodeExecutionToolCall(executions, event).nextExecutions
      continue
    }
    if (event.type === 'tool.result') {
      executions = applyCodeExecutionToolResult(executions, event).nextExecutions
      continue
    }
    if (event.type === 'terminal.stdout_delta' || event.type === 'terminal.stderr_delta') {
      executions = applyTerminalDelta(executions, event).nextExecutions
    }
  }
  return executions
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
  return executions.some((item) => item.language === 'shell' && !item.sessionId)
}

export function selectFreshRunEvents(params: {
  events: RunEvent[]
  activeRunId: string
  processedCount: number
}): { fresh: RunEvent[]; nextProcessedCount: number } {
  const { events, activeRunId } = params
  const normalizedProcessedCount = params.processedCount > events.length ? 0 : params.processedCount

  if (events.length <= normalizedProcessedCount) {
    return { fresh: [], nextProcessedCount: normalizedProcessedCount }
  }

  const slice = events.slice(normalizedProcessedCount)
  return {
    fresh: slice
      .filter((event) => event.run_id === activeRunId)
      .sort((left, right) => left.seq - right.seq || left.ts.localeCompare(right.ts)),
    nextProcessedCount: events.length,
  }
}

/** 首包「处理中」占位：仅此类事件表示用户可见的助手正文或工具链路（segment / thinking delta 等不算）。 */
export function runEventDismissesAssistantPlaceholder(event: RunEvent): boolean {
  switch (event.type) {
    case 'message.delta': {
      if (isACPDelegateEventData(event.data)) return false
      const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
      if (obj.role != null && obj.role !== 'assistant') return false
      if (obj.channel === 'thinking') return false
      return typeof obj.content_delta === 'string' && obj.content_delta.length > 0
    }
    case 'tool.call':
    case 'tool.call.delta':
    case 'tool.result':
      return !isACPDelegateEventData(event.data)
    default:
      return false
  }
}

export function findAssistantMessageForRun(
  messages: MessageResponse[],
  runId: string | null | undefined,
): MessageResponse | undefined {
  const normalizedRunId = typeof runId === 'string' ? runId.trim() : ''
  if (!normalizedRunId) return undefined

  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index]
    if (message.role !== 'assistant') continue
    if (typeof message.run_id === 'string' && message.run_id === normalizedRunId) {
      return message
    }
  }
  return undefined
}

export function shouldRefetchCompletedRunMessages(params: {
  messages: MessageResponse[]
  latestRun: Pick<ThreadRunResponse, 'run_id' | 'status'> | null | undefined
}): boolean {
  const { messages, latestRun } = params
  if (!latestRun || latestRun.status !== 'completed') return false
  return findAssistantMessageForRun(messages, latestRun.run_id) == null
}

function extractWidgetArguments(data: unknown): { title?: string; html?: string } {
  if (!data || typeof data !== 'object') return {}
  const args = (data as { arguments?: unknown }).arguments
  if (!args || typeof args !== 'object') return {}
  const typed = args as Record<string, unknown>
  return {
    title: typeof typed.title === 'string' ? typed.title : undefined,
    html: typeof typed.widget_code === 'string' ? typed.widget_code : undefined,
  }
}

export function buildMessageWidgetsFromRunEvents(events: RunEvent[]): WidgetRef[] {
  const widgets: WidgetRef[] = []
  const seen = new Set<string>()

  for (const event of events) {
    if (event.type !== 'tool.call') continue
    if (isACPDelegateEventData(event.data)) continue
    const toolName = pickToolName(event.data) || event.tool_name || ''
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

export function buildMessageThinkingFromRunEvents(events: RunEvent[]): MessageThinkingRef | null {
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
    if (event.type === 'run.segment.start') {
      const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
      const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
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

    if (event.type === 'run.segment.end') {
      const obj = event.data as { segment_id?: unknown }
      const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
      if (segmentId && activeSegmentId === segmentId) {
        activeSegmentId = null
      }
      continue
    }

    if (event.type !== 'message.delta') continue
    if (isACPDelegateEventData(event.data)) continue
    const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
    if (obj.role != null && obj.role !== 'assistant') continue
    if (typeof obj.content_delta !== 'string' || obj.content_delta === '') continue
    const delta = obj.content_delta

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
  event: RunEvent,
): BrowserActionToolCallPatch {
  if (event.type !== 'tool.call') return { nextActions: actions }
  if (isACPDelegateEventData(event.data)) return { nextActions: actions }
  const toolName = pickToolName(event.data)
  if (toolName !== 'browser') return { nextActions: actions }

  const args = event.data && typeof event.data === 'object'
    ? (event.data as { arguments?: unknown }).arguments
    : undefined
  const command = extractBrowserCommand(args)
  const appended: BrowserActionRef = {
    id: pickToolCallId(event),
    command,
  }
  return { appended, nextActions: [...actions, appended] }
}

export function applyBrowserToolResult(
  actions: BrowserActionRef[],
  event: RunEvent,
): BrowserActionToolResultPatch {
  if (event.type !== 'tool.result') return { nextActions: actions }
  if (isACPDelegateEventData(event.data)) return { nextActions: actions }
  const toolName = pickToolName(event.data)
  if (toolName !== 'browser') return { nextActions: actions }

  const data = event.data && typeof event.data === 'object'
    ? event.data as { result?: unknown; tool_call_id?: unknown }
    : undefined
  const result = data?.result
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

export function buildMessageBrowserActionsFromRunEvents(events: RunEvent[]): BrowserActionRef[] {
  let actions: BrowserActionRef[] = []
  for (const event of events) {
    if (event.type === 'tool.call') {
      actions = applyBrowserToolCall(actions, event).nextActions
    } else if (event.type === 'tool.result') {
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

const SUB_AGENT_CALL_TOOL_NAMES = new Set(['spawn_agent', 'acp_agent', 'spawn_acp'])
const SUB_AGENT_RESULT_TOOL_NAMES = new Set([
  'spawn_agent', 'acp_agent', 'spawn_acp', 'send_input', 'wait_agent', 'resume_agent', 'close_agent', 'interrupt_agent',
  'send_acp', 'wait_acp', 'close_acp', 'interrupt_acp',
])

function extractAcpAgentCallArgs(data: unknown): { task?: string; provider?: string } {
  if (!data || typeof data !== 'object') return {}
  const args = (data as { arguments?: unknown }).arguments
  if (!args || typeof args !== 'object') return {}
  const typed = args as Record<string, unknown>
  return {
    task: typeof typed.task === 'string' ? typed.task : undefined,
    provider: typeof typed.provider === 'string' ? typed.provider : undefined,
  }
}

function extractSpawnArguments(data: unknown): Partial<SubAgentRef> {
  if (!data || typeof data !== 'object') return {}
  const args = (data as { arguments?: unknown }).arguments
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

function extractSpawnACPArgs(data: unknown): { task?: string; provider?: string } {
  if (!data || typeof data !== 'object') return {}
  const args = (data as { arguments?: unknown }).arguments
  if (!args || typeof args !== 'object') return {}
  const typed = args as Record<string, unknown>
  return {
    task: typeof typed.task === 'string' ? typed.task : undefined,
    provider: typeof typed.provider === 'string' ? typed.provider : undefined,
  }
}

function extractACPHandleId(result: Record<string, unknown>): string | undefined {
  const handle = result.handle_id
  return typeof handle === 'string' && handle.trim() ? handle : undefined
}

function resolveAcpStatus(
  existing: SubAgentRef['status'],
  resultStatus: string | undefined,
  isError: boolean,
): SubAgentRef['status'] {
  if (isError) return 'failed'
  switch (resultStatus) {
    case 'completed':
      return 'completed'
    case 'closed':
      return 'closed'
    case 'failed':
      return 'failed'
    case 'running':
      return 'active'
    case 'interrupting':
      return 'active'
    case 'interrupted':
      return 'failed'
  }
  return existing
}

function extractSubAgentResult(data: unknown): Record<string, unknown> {
  if (!data || typeof data !== 'object') return {}
  const result = (data as { result?: unknown }).result
  if (!result || typeof result !== 'object') return {}
  return result as Record<string, unknown>
}

function extractSubAgentError(data: unknown): string | undefined {
  if (!data || typeof data !== 'object') return undefined
  const rawError = (data as { error?: unknown }).error
  if (!rawError || typeof rawError !== 'object') return undefined
  const typed = rawError as { message?: unknown; error_class?: unknown }
  if (typeof typed.message === 'string') return typed.message
  if (typeof typed.error_class === 'string') return typed.error_class
  return undefined
}

function hasSubAgentError(data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  const rawError = (data as { error?: unknown }).error
  return rawError != null && typeof rawError === 'object'
}

function findAgentByToolCallId(agents: SubAgentRef[], toolCallId: string): number {
  return agents.findIndex((a) => a.id === toolCallId)
}

function findAgentBySubAgentId(agents: SubAgentRef[], subAgentId: string): number {
  return agents.findIndex((a) => a.subAgentId === subAgentId)
}

export function applySubAgentToolCall(
  agents: SubAgentRef[],
  event: RunEvent,
): SubAgentToolCallPatch {
  if (event.type !== 'tool.call') return { nextAgents: agents }
  if (isACPDelegateEventData(event.data)) return { nextAgents: agents }
  const toolName = pickToolName(event.data)
  if (!SUB_AGENT_CALL_TOOL_NAMES.has(toolName)) return { nextAgents: agents }

  if (toolName === 'acp_agent' || toolName === 'spawn_acp') {
    const args = toolName === 'spawn_acp'
      ? extractSpawnACPArgs(event.data)
      : extractAcpAgentCallArgs(event.data)
    const appended: SubAgentRef = {
      id: pickToolCallId(event),
      status: toolName === 'spawn_acp' ? 'spawning' : 'active',
      input: args.task,
      personaId: args.provider,
      sourceTool: 'acp_agent',
      seq: event.seq,
    }
    return { appended, nextAgents: [...agents, appended] }
  }

  const fields = extractSpawnArguments(event.data)
  const appended: SubAgentRef = {
    id: pickToolCallId(event),
    status: 'spawning',
    nickname: fields.nickname,
    role: fields.role,
    personaId: fields.personaId,
    contextMode: fields.contextMode,
    input: fields.input,
    seq: event.seq,
  }
  return { appended, nextAgents: [...agents, appended] }
}

export function applySubAgentToolResult(
  agents: SubAgentRef[],
  event: RunEvent,
): SubAgentToolResultPatch {
  if (event.type !== 'tool.result') return { nextAgents: agents }
  if (isACPDelegateEventData(event.data)) return { nextAgents: agents }
  const toolName = pickToolName(event.data)
  if (!SUB_AGENT_RESULT_TOOL_NAMES.has(toolName)) return { nextAgents: agents }

  const toolCallId = pickToolCallId(event)
  const result = extractSubAgentResult(event.data)
  const errorMessage = extractSubAgentError(event.data)
  const isError = hasSubAgentError(event.data)
  const subAgentId = typeof result.sub_agent_id === 'string' ? result.sub_agent_id : undefined
  const acpHandleId = extractACPHandleId(result)
  const output = typeof result.output === 'string' ? result.output : undefined
  const nickname = typeof result.nickname === 'string' ? result.nickname : undefined
  const depth = typeof result.depth === 'number' ? result.depth : undefined
  const resultStatus = typeof result.status === 'string' ? result.status : undefined

  if (toolName === 'acp_agent') {
    const idx = findAgentByToolCallId(agents, toolCallId)
    if (idx < 0) return { nextAgents: agents }
    const summary = typeof result.summary === 'string' ? result.summary.trim() : ''
    const out = typeof result.output === 'string' ? result.output.trim() : ''
    const text = [summary, out].filter((x) => x.length > 0).join('\n\n')
    const updated: SubAgentRef = {
      ...agents[idx],
      output: text || out || summary || undefined,
      status: isError ? 'failed' : 'completed',
      error: errorMessage,
    }
    return { updated, nextAgents: agents.map((a, i) => (i === idx ? updated : a)) }
  }

  if (toolName === 'spawn_agent') {
    const idx = findAgentByToolCallId(agents, toolCallId)
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

  if (toolName === 'spawn_acp') {
    const idx = findAgentByToolCallId(agents, toolCallId)
    if (idx < 0) return { nextAgents: agents }
    const updated: SubAgentRef = {
      ...agents[idx],
      subAgentId: acpHandleId ?? agents[idx].subAgentId,
      output: output ?? agents[idx].output,
      status: resolveAcpStatus(agents[idx].status, resultStatus, isError),
      error: isError ? errorMessage : undefined,
      depth,
    }
    return { updated, nextAgents: agents.map((a, i) => (i === idx ? updated : a)) }
  }

  if (toolName === 'send_acp' || toolName === 'wait_acp' || toolName === 'interrupt_acp' || toolName === 'close_acp') {
    const idx = acpHandleId ? findAgentBySubAgentId(agents, acpHandleId) : -1
    const nextError = errorMessage ?? (resultStatus === 'interrupted' ? 'interrupted' : undefined)

    if (idx < 0) {
      if (!acpHandleId) return { nextAgents: agents }
      const appended: SubAgentRef = {
        id: toolCallId,
        subAgentId: acpHandleId,
        output,
        status: resolveAcpStatus('active', resultStatus, isError),
        error: nextError,
        sourceTool: 'acp_agent',
        seq: event.seq,
      }
      return { updated: appended, nextAgents: [...agents, appended] }
    }

    const updated: SubAgentRef = {
      ...agents[idx],
      output: output ?? agents[idx].output,
      status: resolveAcpStatus(agents[idx].status, resultStatus, isError),
      error: nextError,
      seq: event.seq,
    }
    return { updated, nextAgents: agents.map((a, i) => i === idx ? updated : a) }
  }

  // For other tools, locate by sub_agent_id in result
  const targetIdx = subAgentId ? findAgentBySubAgentId(agents, subAgentId) : -1

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

export function buildMessageSubAgentsFromRunEvents(events: RunEvent[]): SubAgentRef[] {
  let agents: SubAgentRef[] = []
  for (const event of events) {
    if (event.type === 'tool.call') {
      agents = applySubAgentToolCall(agents, event).nextAgents
    } else if (event.type === 'tool.result') {
      agents = applySubAgentToolResult(agents, event).nextAgents
    }
  }
  return agents
}

// --- File operation processing ---

const FILE_OP_TOOL_NAMES = new Set(['grep', 'glob', 'read_file', 'read', 'write_file', 'edit', 'edit_file', 'load_tools', 'memory_write', 'memory_edit', 'memory_search', 'memory_read', 'memory_forget', 'notebook_write', 'notebook_read', 'notebook_edit', 'notebook_forget'])

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
  const truncate = (s: string, max: number) => s.length > max ? s.slice(0, max) + '…' : s
  const basename = (p: string) => p.replace(/\\/g, '/').split('/').pop() ?? p

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

export function fileOpOutputFromResult(toolName: string, result: unknown): string | undefined {
  toolName = normalizeFileOpToolName(toolName)
  if (!result || typeof result !== 'object') return undefined
  const r = result as Record<string, unknown>

  switch (toolName) {
    case 'grep': {
      const count = typeof r.count === 'number' ? r.count : 0
      const matches = typeof r.matches === 'string' ? r.matches.trim() : ''
      if (count === 0) return '(no matches)'
      return `${count} match${count === 1 ? '' : 'es'}\n${matches}`
    }
    case 'glob': {
      const files = Array.isArray(r.files) ? (r.files as unknown[]).filter((f): f is string => typeof f === 'string') : []
      const count = typeof r.count === 'number' ? r.count : files.length
      if (count === 0) return '(no files)'
      return `${count} file${count === 1 ? '' : 's'}\n${files.join('\n')}`
    }
    case 'read_file': {
      const content = typeof r.content === 'string' ? r.content.trim() : ''
      return content || undefined
    }
    case 'write_file': {
      const filePath = typeof r.file_path === 'string' ? r.file_path : ''
      return filePath ? `written: ${filePath}` : 'written'
    }
    case 'edit':
    case 'edit_file': {
      const filePath = typeof r.file_path === 'string' ? r.file_path : ''
      return filePath ? `edited: ${filePath}` : 'edited'
    }
    case 'load_tools': {
      const matched = Array.isArray(r.matched) ? r.matched as unknown[] : []
      const count = typeof r.count === 'number' ? r.count : matched.length
      if (count === 0 && matched.length === 0) return '(no matches)'
      const statusSummary = summarizeLoadToolsResult(r)
      if (statusSummary) return statusSummary
      const names = matched.slice(0, 5).map((m) => {
        if (typeof m === 'string') return m
        if (m && typeof m === 'object') return String((m as Record<string, unknown>).name ?? '')
        return ''
      }).filter(Boolean)
      return `${count} match${count === 1 ? '' : 'es'}${names.length > 0 ? ': ' + names.join(', ') : ''}`
    }
    case 'memory_write': {
      const stored = typeof r.stored === 'boolean' ? r.stored : true
      return stored ? 'stored' : 'failed'
    }
    case 'memory_edit': {
      return 'updated'
    }
    case 'memory_search': {
      const list = Array.isArray(r.hits)
        ? (r.hits as unknown[])
        : Array.isArray(r.results)
          ? (r.results as unknown[])
          : []
      const count = list.length
      if (count === 0) return '(no results)'
      return memorySearchHitsToOutput(list)
    }
    case 'memory_read': {
      const content = typeof r.content === 'string' ? r.content.trim() : ''
      return content ? content.slice(0, 80) + (content.length > 80 ? '…' : '') : 'read'
    }
    case 'memory_forget': {
      return 'forgotten'
    }
    case 'notebook_write': {
      return 'saved'
    }
    case 'notebook_edit': {
      return 'updated'
    }
    case 'notebook_read': {
      const content = typeof r.content === 'string' ? r.content.trim() : ''
      return content ? content.slice(0, 80) + (content.length > 80 ? '…' : '') : 'read'
    }
    case 'notebook_forget': {
      return 'deleted'
    }
    default:
      return undefined
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
  event: RunEvent,
): FileOpToolCallPatch {
  if (event.type !== 'tool.call') return { nextOps: ops }
  if (isACPDelegateEventData(event.data)) return { nextOps: ops }
  const rawToolName = pickToolName(event.data)
  const toolName = normalizeFileOpToolName(rawToolName)
  if (!FILE_OP_TOOL_NAMES.has(rawToolName) && !FILE_OP_TOOL_NAMES.has(toolName)) return { nextOps: ops }

  const args = event.data && typeof event.data === 'object'
    ? (event.data as { arguments?: unknown }).arguments as Record<string, unknown> | undefined ?? {}
    : {}
  const appended: FileOpRef = {
    id: pickToolCallId(event),
    toolName,
    label: fileOpLabel(toolName, args),
    status: 'running',
    seq: event.seq,
  }
  return { appended, nextOps: [...ops, appended] }
}

export function applyFileOpToolResult(
  ops: FileOpRef[],
  event: RunEvent,
): FileOpToolResultPatch {
  if (event.type !== 'tool.result') return { nextOps: ops }
  if (isACPDelegateEventData(event.data)) return { nextOps: ops }
  const rawToolName = pickToolName(event.data)
  const toolName = normalizeFileOpToolName(rawToolName)
  if (!FILE_OP_TOOL_NAMES.has(rawToolName) && !FILE_OP_TOOL_NAMES.has(toolName)) return { nextOps: ops }

  const toolCallId = pickToolCallId(event)
  const data = event.data && typeof event.data === 'object'
    ? event.data as { result?: unknown }
    : undefined
  const result = data?.result
  const error = extractCodeExecutionError(event)
  const hasError = !!(error.errorClass || error.errorMessage)

  const targetIdx = ops.findIndex((o) => o.id === toolCallId)
  if (targetIdx < 0) return { nextOps: ops }

  const updated: FileOpRef = {
    ...ops[targetIdx],
    status: hasError ? 'failed' : 'success',
    output: hasError ? undefined : fileOpOutputFromResult(toolName, result),
    errorMessage: hasError ? (error.errorMessage ?? error.errorClass) : undefined,
  }
  return {
    updated,
    nextOps: ops.map((o, i) => i === targetIdx ? updated : o),
  }
}

export function buildMessageFileOpsFromRunEvents(events: RunEvent[]): FileOpRef[] {
  let ops: FileOpRef[] = []
  for (const event of events) {
    if (event.type === 'tool.call') {
      ops = applyFileOpToolCall(ops, event).nextOps
    } else if (event.type === 'tool.result') {
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
  event: RunEvent,
): WebFetchToolCallPatch {
  if (event.type !== 'tool.call') return { nextFetches: fetches }
  if (isACPDelegateEventData(event.data)) return { nextFetches: fetches }
  const toolName = pickToolName(event.data)
  if (!isWebFetchToolName(toolName)) return { nextFetches: fetches }

  const args = event.data && typeof event.data === 'object'
    ? (event.data as { arguments?: unknown }).arguments as Record<string, unknown> | undefined ?? {}
    : {}
  const url = typeof args.url === 'string' ? args.url : ''
  const appended: WebFetchRef = {
    id: pickToolCallId(event),
    url,
    status: 'fetching',
    seq: event.seq,
  }
  return { appended, nextFetches: [...fetches, appended] }
}

export function applyWebFetchToolResult(
  fetches: WebFetchRef[],
  event: RunEvent,
): WebFetchToolResultPatch {
  if (event.type !== 'tool.result') return { nextFetches: fetches }
  if (isACPDelegateEventData(event.data)) return { nextFetches: fetches }
  const toolName = pickToolName(event.data)
  if (!isWebFetchToolName(toolName)) return { nextFetches: fetches }

  const toolCallId = pickToolCallId(event)
  const data = event.data && typeof event.data === 'object'
    ? event.data as { result?: unknown }
    : undefined
  const result = data?.result as Record<string, unknown> | undefined
  const hasError = !!(event.error_class)
  const title = typeof result?.title === 'string' ? result.title : undefined
  const statusCode = typeof result?.status_code === 'number' ? result.status_code : undefined

  const targetIdx = fetches.findIndex((f) => f.id === toolCallId)
  if (targetIdx < 0) return { nextFetches: fetches }

  const updated: WebFetchRef = {
    ...fetches[targetIdx],
    title,
    statusCode,
    status: hasError ? 'failed' : 'done',
  }
  return {
    updated,
    nextFetches: fetches.map((f, i) => i === targetIdx ? updated : f),
  }
}

export function buildMessageWebFetchesFromRunEvents(events: RunEvent[]): WebFetchRef[] {
  let fetches: WebFetchRef[] = []
  for (const event of events) {
    if (event.type === 'tool.call') {
      fetches = applyWebFetchToolCall(fetches, event).nextFetches
    } else if (event.type === 'tool.result') {
      fetches = applyWebFetchToolResult(fetches, event).nextFetches
    }
  }
  return fetches
}
