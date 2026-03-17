import type { MessageResponse, ThreadRunResponse } from './api'
import type { RunEvent } from './sse'
import type { ArtifactRef, BrowserActionRef, CodeExecutionRef, FileOpRef, MessageCopBlocksRef, MessageSearchStepRef, MessageThinkingRef, SubAgentRef, WebFetchRef } from './storage'

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
  if (pickExecutionRunning(result)) {
    return 'running'
  }
  if (exitCode != null) {
    return exitCode === 0 ? 'success' : 'failed'
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

export function applyCodeExecutionToolCall(
  executions: CodeExecutionRef[],
  event: RunEvent,
): CodeExecutionToolCallPatch {
  if (event.type !== 'tool.call') {
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
    fresh: slice.filter((event) => event.run_id === activeRunId),
    nextProcessedCount: events.length,
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

const SUB_AGENT_CALL_TOOL_NAMES = new Set(['spawn_agent'])
const SUB_AGENT_RESULT_TOOL_NAMES = new Set([
  'spawn_agent', 'send_input', 'wait_agent', 'resume_agent', 'close_agent', 'interrupt_agent',
])

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
  const toolName = pickToolName(event.data)
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
  }
  return { appended, nextAgents: [...agents, appended] }
}

export function applySubAgentToolResult(
  agents: SubAgentRef[],
  event: RunEvent,
): SubAgentToolResultPatch {
  if (event.type !== 'tool.result') return { nextAgents: agents }
  const toolName = pickToolName(event.data)
  if (!SUB_AGENT_RESULT_TOOL_NAMES.has(toolName)) return { nextAgents: agents }

  const toolCallId = pickToolCallId(event)
  const result = extractSubAgentResult(event.data)
  const errorMessage = extractSubAgentError(event.data)
  const isError = hasSubAgentError(event.data)
  const subAgentId = typeof result.sub_agent_id === 'string' ? result.sub_agent_id : undefined
  const output = typeof result.output === 'string' ? result.output : undefined
  const nickname = typeof result.nickname === 'string' ? result.nickname : undefined
  const depth = typeof result.depth === 'number' ? result.depth : undefined

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
  const resultStatus = typeof result.status === 'string' ? result.status : undefined
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

const FILE_OP_TOOL_NAMES = new Set(['grep', 'glob', 'read_file', 'write_file', 'edit', 'edit_file', 'search_tools'])

type FileOpToolCallPatch = {
  nextOps: FileOpRef[]
  appended?: FileOpRef
}

type FileOpToolResultPatch = {
  nextOps: FileOpRef[]
  updated?: FileOpRef
}

function fileOpLabel(toolName: string, args: Record<string, unknown>): string {
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
      const filePath = typeof args.file_path === 'string' ? args.file_path : ''
      return filePath ? truncate(basename(filePath), 48) : 'read file'
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
    case 'search_tools': {
      const queries = Array.isArray(args.queries)
        ? (args.queries as unknown[]).filter((q): q is string => typeof q === 'string')
        : []
      if (queries.length > 0) {
        const qs = queries.slice(0, 2).map((q) => `"${truncate(q, 24)}"`).join(', ')
        return `search_tools ${qs}${queries.length > 2 ? ', …' : ''}`
      }
      return 'search_tools'
    }
    default:
      return toolName
  }
}

function fileOpOutputFromResult(toolName: string, result: unknown): string | undefined {
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
    case 'search_tools': {
      const count = typeof r.count === 'number' ? r.count : 0
      if (count === 0) return '(no matches)'
      const matched = Array.isArray(r.matched) ? r.matched as unknown[] : []
      const names = matched.slice(0, 5).map((m) => {
        if (typeof m === 'string') return m
        if (m && typeof m === 'object') return String((m as Record<string, unknown>).name ?? '')
        return ''
      }).filter(Boolean)
      return `${count} match${count === 1 ? '' : 'es'}${names.length > 0 ? ': ' + names.join(', ') : ''}`
    }
    default:
      return undefined
  }
}

export function applyFileOpToolCall(
  ops: FileOpRef[],
  event: RunEvent,
): FileOpToolCallPatch {
  if (event.type !== 'tool.call') return { nextOps: ops }
  const toolName = pickToolName(event.data)
  if (!FILE_OP_TOOL_NAMES.has(toolName)) return { nextOps: ops }

  const args = event.data && typeof event.data === 'object'
    ? (event.data as { arguments?: unknown }).arguments as Record<string, unknown> | undefined ?? {}
    : {}
  const appended: FileOpRef = {
    id: pickToolCallId(event),
    toolName,
    label: fileOpLabel(toolName, args),
    status: 'running',
  }
  return { appended, nextOps: [...ops, appended] }
}

export function applyFileOpToolResult(
  ops: FileOpRef[],
  event: RunEvent,
): FileOpToolResultPatch {
  if (event.type !== 'tool.result') return { nextOps: ops }
  const toolName = pickToolName(event.data)
  if (!FILE_OP_TOOL_NAMES.has(toolName)) return { nextOps: ops }

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
  const toolName = pickToolName(event.data)
  if (toolName !== 'web_fetch') return { nextFetches: fetches }

  const args = event.data && typeof event.data === 'object'
    ? (event.data as { arguments?: unknown }).arguments as Record<string, unknown> | undefined ?? {}
    : {}
  const url = typeof args.url === 'string' ? args.url : ''
  const appended: WebFetchRef = {
    id: pickToolCallId(event),
    url,
    status: 'fetching',
  }
  return { appended, nextFetches: [...fetches, appended] }
}

export function applyWebFetchToolResult(
  fetches: WebFetchRef[],
  event: RunEvent,
): WebFetchToolResultPatch {
  if (event.type !== 'tool.result') return { nextFetches: fetches }
  const toolName = pickToolName(event.data)
  if (toolName !== 'web_fetch') return { nextFetches: fetches }

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

export function buildMessageCopBlocksFromRunEvents(events: RunEvent[]): MessageCopBlocksRef | null {
  type Block = { id: string; title: string; steps: MessageSearchStepRef[]; sources: [] }
  const blocks: Block[] = []

  const ensureBlock = () => {
    if (blocks.length === 0) {
      blocks.push({ id: crypto.randomUUID(), title: '', steps: [], sources: [] })
    }
  }

  for (const event of events) {
    if (event.type === 'tool.call' && pickToolName(event.data) === 'timeline_title') {
      const args = event.data && typeof event.data === 'object'
        ? (event.data as { arguments?: unknown }).arguments as Record<string, unknown> | undefined ?? {}
        : {}
      const label = typeof args.label === 'string' ? args.label.trim() : ''
      if (blocks.length > 0) {
        const last = blocks[blocks.length - 1]
        if (last.title === '' || last.steps.length === 0) {
          last.title = label
          continue
        }
      }
      blocks.push({ id: crypto.randomUUID(), title: label, steps: [], sources: [] })
      continue
    }

    if (event.type === 'run.segment.start') {
      const obj = event.data as { segment_id?: unknown; kind?: unknown; label?: unknown; display?: unknown }
      const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
      const kind = typeof obj.kind === 'string' ? obj.kind : ''
      if (!segmentId || !kind.startsWith('search_')) continue
      if (kind === 'search_planning') {
        ensureBlock()
        continue
      }
      const stepKind: MessageSearchStepRef['kind'] = kind === 'search_queries' ? 'searching'
        : kind === 'search_reviewing' ? 'reviewing'
        : 'searching'
      const display = obj.display && typeof obj.display === 'object'
        ? obj.display as Record<string, unknown>
        : {}
      const label = typeof display.label === 'string' ? display.label : ''
      const queries = Array.isArray(display.queries)
        ? (display.queries as unknown[]).filter((q): q is string => typeof q === 'string')
        : undefined
      ensureBlock()
      blocks[blocks.length - 1].steps.push({ id: segmentId, kind: stepKind, label, status: 'done', queries })
    }
  }

  if (blocks.length === 0) return null
  return { blocks, bridgeTexts: [] }
}
