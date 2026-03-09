import type { RunEvent } from './sse'
import type { ArtifactRef, BrowserActionRef, CodeExecutionRef, MessageThinkingRef } from './storage'

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
    : (stdout || fallbackOutput)

  return {
    output: rawOutput || undefined,
    exitCode,
  }
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
      if (executions[i].language === 'shell' && executions[i].exitCode == null) {
        return i
      }
    }
  }
  return -1
}

function patchExecution(
  execution: CodeExecutionRef,
  params: { sessionId?: string; output?: string; exitCode?: number },
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
    })
    const current = executions[targetIndex]
    if (
      current.output === updated.output &&
      current.exitCode === updated.exitCode &&
      current.sessionId === updated.sessionId
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

function extractBrowserOutput(result: unknown): { output?: string; exitCode?: number; url?: string; sessionRef?: string } {
  if (!result || typeof result !== 'object') return {}
  const typed = result as { output?: unknown; stdout?: unknown; exit_code?: unknown; session_ref?: unknown }
  const output = typeof typed.output === 'string' ? typed.output
    : typeof typed.stdout === 'string' ? typed.stdout
    : undefined
  const exitCode = typeof typed.exit_code === 'number' ? typed.exit_code : undefined
  const sessionRef = typeof typed.session_ref === 'string' ? typed.session_ref : undefined
  return { output, exitCode, sessionRef }
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
  const { output, exitCode, sessionRef } = extractBrowserOutput(result)
  const screenshotArtifact = extractBrowserScreenshotArtifact(result)

  const targetIndex = actions.findIndex((a) => a.id === toolCallId)
  if (targetIndex >= 0) {
    const updated: BrowserActionRef = {
      ...actions[targetIndex],
      output,
      exitCode,
      sessionRef,
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
    sessionRef,
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
