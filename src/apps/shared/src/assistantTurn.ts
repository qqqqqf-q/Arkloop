import { pickLogicalToolName } from './tool-names'

export type AssistantTurnEvent = {
  event_id: string
  run_id: string
  seq: number
  ts: string
  type: string
  data: unknown
  tool_name?: string
  error_class?: string
}

export type TurnToolCallRef = {
  toolCallId: string
  toolName: string
  arguments: Record<string, unknown>
  displayDescription?: string
  result?: unknown
  errorClass?: string
  errorMessage?: string
}

export type CopBlockItem =
  | { kind: 'thinking'; content: string; seq: number; startedAtMs?: number; endedAtMs?: number }
  | { kind: 'assistant_text'; content: string; seq: number }
  | { kind: 'call'; call: TurnToolCallRef; seq: number }

export type AssistantTurnSegment =
  | { type: 'text'; content: string }
  | { type: 'cop'; title: string | null; items: CopBlockItem[] }

export type AssistantTurnUi = { segments: AssistantTurnSegment[]; durationMs?: number }

export type WorkGroup = {
  durationMs: number
  segments: AssistantTurnSegment[]
}

/**
 * Split segments into work group (pre-final) and final text.
 * The last text segment is the final answer; everything before it goes into the work group.
 * Returns null workGroup when there's nothing meaningful to collapse.
 */
export function splitWorkGroup(
  segments: AssistantTurnSegment[],
  durationMs: number,
): { workGroup: WorkGroup | null; finalText: string | null } {
  // Find the index of the last text segment
  let lastTextIndex = -1
  for (let i = segments.length - 1; i >= 0; i--) {
    if (segments[i]!.type === 'text') {
      lastTextIndex = i
      break
    }
  }

  // No text segment at all
  if (lastTextIndex === -1) {
    return { workGroup: null, finalText: null }
  }

  const finalSegment = segments[lastTextIndex]!
  const finalText = finalSegment.type === 'text' ? finalSegment.content : null

  // Need at least 2 segments before final to justify a work group.
  // A single pre-final segment (text or cop) stays inline.
  if (lastTextIndex < 2) {
    return { workGroup: null, finalText }
  }

  const workGroupSegments = segments.slice(0, lastTextIndex)
  return {
    workGroup: { durationMs, segments: workGroupSegments },
    finalText,
  }
}

export type AssistantTurnFoldState = {
  segments: AssistantTurnSegment[]
  currentCop: { type: 'cop'; title: string | null; items: CopBlockItem[] } | null
  thinkingMustBreakBeforeNext: boolean
  lastEventTimeMs: number | null
}

const TIMELINE_TITLE_TOOL = 'timeline_title'
const HIDDEN_COP_TOOLS = new Set(['end_reply'])

function shouldHideCopTool(toolName: string): boolean {
  return HIDDEN_COP_TOOLS.has(toolName.trim())
}

export function copSegmentCalls(segment: { type: 'cop'; items: CopBlockItem[] }): TurnToolCallRef[] {
  return segment.items
    .filter((i): i is Extract<CopBlockItem, { kind: 'call' }> => i.kind === 'call')
    .map((i) => i.call)
    .filter((call) => !shouldHideCopTool(call.toolName))
}

function pickToolName(data: unknown): string {
  return pickLogicalToolName(data)
}

function pickToolCallId(event: AssistantTurnEvent): string {
  if (!event.data || typeof event.data !== 'object') return event.event_id
  const raw = (event.data as { tool_call_id?: unknown }).tool_call_id
  return typeof raw === 'string' && raw.trim() !== '' ? raw : event.event_id
}

function sortAssistantTurnEvents(events: readonly AssistantTurnEvent[]): AssistantTurnEvent[] {
  return [...events].sort((left, right) => left.seq - right.seq || left.ts.localeCompare(right.ts))
}

function assistantTurnEventTimeMs(event: AssistantTurnEvent): number {
  const t = Date.parse(event.ts)
  return Number.isFinite(t) ? t : Date.now()
}

function closeOpenThinkingItems(items: CopBlockItem[], endMs: number): void {
  for (const it of items) {
    if (it.kind !== 'thinking' || it.endedAtMs != null) continue
    if (it.startedAtMs == null) it.startedAtMs = endMs
    it.endedAtMs = endMs
  }
}

function closeThinkingBeforeLastCall(items: CopBlockItem[], endMs: number): void {
  for (let i = items.length - 2; i >= 0; i--) {
    const it = items[i]
    if (it.kind === 'call') break
    if (it.kind === 'thinking' && it.endedAtMs == null) {
      it.endedAtMs = endMs
    }
  }
}

function extractArguments(data: unknown): Record<string, unknown> {
  if (!data || typeof data !== 'object') return {}
  const raw = (data as { arguments?: unknown }).arguments
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    return { ...(raw as Record<string, unknown>) }
  }
  return {}
}

function pickDisplayDescription(data: unknown): string | undefined {
  if (!data || typeof data !== 'object') return undefined
  const raw = (data as { display_description?: unknown }).display_description
  return typeof raw === 'string' && raw.trim() !== '' ? raw.trim() : undefined
}

function extractErrorInfo(data: unknown, fallbackErrorClass?: string): { errorClass?: string; errorMessage?: string } {
  const error = data && typeof data === 'object' && !Array.isArray(data)
    ? (data as { error?: unknown }).error
    : undefined
  const record = error && typeof error === 'object' && !Array.isArray(error)
    ? error as Record<string, unknown>
    : undefined
  const errorClassRaw = record?.error_class ?? record?.errorClass ?? record?.code
  const messageRaw = record?.message
  const errorClass = typeof errorClassRaw === 'string' && errorClassRaw.trim() !== ''
    ? errorClassRaw.trim()
    : typeof fallbackErrorClass === 'string' && fallbackErrorClass.trim() !== '' ? fallbackErrorClass.trim() : undefined
  const errorMessage = typeof messageRaw === 'string' && messageRaw.trim() !== '' ? messageRaw.trim() : undefined
  return {
    ...(errorClass ? { errorClass } : {}),
    ...(errorMessage ? { errorMessage } : {}),
  }
}

function extractResultPayload(event: AssistantTurnEvent): unknown {
  if (!event.data || typeof event.data !== 'object') return undefined
  return (event.data as { result?: unknown }).result
}

function isCopEmpty(cop: { title: string | null; items: CopBlockItem[] }): boolean {
  return cop.items.length === 0
}

function cloneTurnToolCall(c: TurnToolCallRef): TurnToolCallRef {
  return {
    toolCallId: c.toolCallId,
    toolName: c.toolName,
    arguments: { ...c.arguments },
    ...(c.displayDescription != null ? { displayDescription: c.displayDescription } : {}),
    result: c.result,
    errorClass: c.errorClass,
    ...(c.errorMessage != null ? { errorMessage: c.errorMessage } : {}),
  }
}

function cloneCopItem(i: CopBlockItem): CopBlockItem {
  if (i.kind === 'thinking') {
    return {
      kind: 'thinking',
      content: i.content,
      seq: i.seq,
      ...(i.startedAtMs != null ? { startedAtMs: i.startedAtMs } : {}),
      ...(i.endedAtMs != null ? { endedAtMs: i.endedAtMs } : {}),
    }
  }
  if (i.kind === 'assistant_text') {
    return { kind: 'assistant_text', content: i.content, seq: i.seq }
  }
  return { kind: 'call', call: cloneTurnToolCall(i.call), seq: i.seq }
}

function cloneSegment(s: AssistantTurnSegment): AssistantTurnSegment {
  if (s.type === 'text') return { type: 'text', content: s.content }
  return {
    type: 'cop',
    title: s.title,
    items: s.items.map(cloneCopItem),
  }
}

export function finalizeAndDrainTurn(state: AssistantTurnFoldState, endMs?: number): AssistantTurnUi {
  finalizeAssistantTurnFoldState(state, endMs)
  const turn: AssistantTurnUi = { segments: state.segments.map(cloneSegment) }
  state.segments = []
  state.currentCop = null
  state.thinkingMustBreakBeforeNext = false
  state.lastEventTimeMs = null
  return turn
}

export function createEmptyAssistantTurnFoldState(): AssistantTurnFoldState {
  return { segments: [], currentCop: null, thinkingMustBreakBeforeNext: false, lastEventTimeMs: null }
}

export function requestAssistantTurnThinkingBreak(state: AssistantTurnFoldState): void {
  state.thinkingMustBreakBeforeNext = true
}

function pushCurrentCopToSegments(
  segments: AssistantTurnSegment[],
  currentCop: AssistantTurnFoldState['currentCop'],
): void {
  if (currentCop == null) return
  if (!isCopEmpty(currentCop)) {
    segments.push({
      type: 'cop',
      title: currentCop.title,
      items: currentCop.items.map(cloneCopItem),
    })
  }
}


function lastCopSegmentHasCalls(segments: AssistantTurnSegment[]): boolean {
  const last = segments[segments.length - 1]
  return !!(last?.type === 'cop' && last.items.some((item) => item.kind === 'call'))
}

function appendThinkingToPreviousToolCop(segments: AssistantTurnSegment[], delta: string, seq: number, startedAtMs: number): boolean {
  const last = segments[segments.length - 1]
  if (last?.type !== 'cop') return false
  if (!last.items.some((item) => item.kind === 'call')) return false
  const tail = last.items[last.items.length - 1]
  if (tail?.kind === 'thinking') {
    tail.content += delta
    if (tail.startedAtMs == null) tail.startedAtMs = startedAtMs
  } else {
    last.items.push({ kind: 'thinking', content: delta, seq, startedAtMs })
  }
  return true
}

function attachResultToItems(
  items: CopBlockItem[],
  toolCallId: string,
  result: unknown,
  errorClass?: string,
  errorMessage?: string,
): boolean {
  for (const item of items) {
    if (item.kind !== 'call') continue
    if (item.call.toolCallId !== toolCallId) continue
    item.call.result = result
    if (errorClass) item.call.errorClass = errorClass
    if (errorMessage) item.call.errorMessage = errorMessage
    return true
  }
  return false
}

function attachResultToSegments(
  segments: AssistantTurnSegment[],
  toolCallId: string,
  result: unknown,
  errorClass?: string,
  errorMessage?: string,
): boolean {
  for (let i = segments.length - 1; i >= 0; i--) {
    const segment = segments[i]
    if (segment?.type !== 'cop') continue
    if (attachResultToItems(segment.items, toolCallId, result, errorClass, errorMessage)) {
      return true
    }
  }
  return false
}

export function snapshotAssistantTurn(state: AssistantTurnFoldState): AssistantTurnUi {
  const segments = state.segments.map(cloneSegment)
  pushCurrentCopToSegments(segments, state.currentCop)
  return { segments }
}

/**
 * 事件折叠状态机，核心规则：
 * - 新 COP 块：首个非隐藏 tool.call 创建新块
 * - 追加：同类别连续 tool.call 合入同一 COP，text/thinking 追加到当前块或最近的 tool COP
 * - Flush 触发：run.segment.start/end、message.delta 遇非空白普通文本
 * - timeline_title：仅命名当前 COP，不切断
 * - thinkingMustBreakBeforeNext：tool.call 后置位，确保下一段 thinking 不与 call 前的 thinking 合并
 */
export function foldAssistantTurnEvent(state: AssistantTurnFoldState, event: AssistantTurnEvent): void {
  const { segments } = state
  let { currentCop } = state
  const eventTs = assistantTurnEventTimeMs(event)

  const flushCop = (endMs: number) => {
    if (currentCop == null) return
    if (!isCopEmpty(currentCop)) {
      closeOpenThinkingItems(currentCop.items, endMs)
      segments.push({
        type: 'cop',
        title: currentCop.title,
        items: currentCop.items.map(cloneCopItem),
      })
    }
    currentCop = null
    state.thinkingMustBreakBeforeNext = false
  }

  const appendAssistantDelta = (delta: string) => {
    if (delta === '') return
    if (delta.trim() === '') {
      const last = segments[segments.length - 1]
      if (last?.type === 'text') last.content += delta
      return
    }
    flushCop(eventTs)
    const last = segments[segments.length - 1]
    if (last?.type === 'text') {
      last.content += delta
    } else {
      segments.push({ type: 'text', content: delta })
    }
  }

  const ensureCop = () => {
    if (currentCop == null) {
      currentCop = { type: 'cop', title: null, items: [] }
    }
  }

  const shouldBreakCopBeforeTool = (_toolName: string): boolean => {
    return false
  }

  const shouldBreakCopBeforeOrphanResult = (_toolName: string): boolean => {
    return false
  }

  const attachResultToCop = (toolCallId: string, toolName: string, result: unknown, errorClass?: string, errorMessage?: string) => {
    if (currentCop && attachResultToItems(currentCop.items, toolCallId, result, errorClass, errorMessage)) {
      return
    }
    if (attachResultToSegments(segments, toolCallId, result, errorClass, errorMessage)) return
    if (shouldBreakCopBeforeOrphanResult(toolName)) flushCop(eventTs)
    ensureCop()
    const targetCop = currentCop
    if (targetCop == null) return
    targetCop.items.push({
      kind: 'call',
      call: {
        toolCallId,
        toolName: toolName || 'unknown',
        arguments: {},
        result,
        errorClass,
        errorMessage,
      },
      seq: event.seq,
    })
    closeThinkingBeforeLastCall(targetCop.items, eventTs)
  }

  const handleThinkingChannel = (delta: string, eventSeq: number, eventTs: number) => {
    const forceNew = state.thinkingMustBreakBeforeNext
    if (forceNew) {
      state.thinkingMustBreakBeforeNext = false
    }
    if (currentCop == null && forceNew && appendThinkingToPreviousToolCop(segments, delta, eventSeq, eventTs)) {
      state.currentCop = currentCop
      return
    }
    ensureCop()
    const items = currentCop!.items
    const last = items[items.length - 1]
    if (!forceNew && last?.kind === 'thinking') {
      last.content += delta
      if (last.startedAtMs == null) last.startedAtMs = eventTs
    } else {
      items.push({ kind: 'thinking', content: delta, seq: eventSeq, startedAtMs: eventTs })
    }
    state.currentCop = currentCop
  }

  const handleAssistantTextChannel = (delta: string, eventSeq: number, eventTs: number) => {
    const hasCallsInOpenCop = currentCop != null && currentCop.items.some((i) => i.kind === 'call')
    const previousToolCopHasTrailingThinking = currentCop == null && lastCopSegmentHasCalls(segments)

    if (delta.trim() === '') {
      if (previousToolCopHasTrailingThinking) {
        appendThinkingToPreviousToolCop(segments, delta, eventSeq, eventTs)
        state.currentCop = currentCop
        return
      }
      if (currentCop != null && !hasCallsInOpenCop) {
        const lastItem = currentCop.items[currentCop.items.length - 1]
        if (lastItem?.kind === 'thinking') {
          lastItem.content += delta
          state.currentCop = currentCop
          return
        }
      }
      appendAssistantDelta(delta)
      state.currentCop = currentCop
      return
    }

    if (currentCop != null && !hasCallsInOpenCop) {
      const lastCopItem = currentCop.items[currentCop.items.length - 1]
      if (lastCopItem?.kind === 'thinking') {
        appendAssistantDelta(delta)
        state.currentCop = currentCop
        return
      }
    }

    appendAssistantDelta(delta)
    state.currentCop = currentCop
  }

  const handleToolCall = (event: AssistantTurnEvent) => {
    const toolName = pickToolName(event.data)
    if (toolName === TIMELINE_TITLE_TOOL) {
      ensureCop()
      const args = extractArguments(event.data)
      const labelRaw = args.label
      const label = typeof labelRaw === 'string' ? labelRaw.trim() : ''
      if (label !== '' && currentCop) {
        currentCop.title = label
      }
      state.currentCop = currentCop
      return
    }
    if (shouldHideCopTool(toolName)) {
      state.currentCop = currentCop
      return
    }
    if (shouldBreakCopBeforeTool(toolName)) flushCop(eventTs)
    ensureCop()
    currentCop!.items.push({
      kind: 'call',
      call: {
        toolCallId: pickToolCallId(event),
        toolName,
        arguments: extractArguments(event.data),
        displayDescription: pickDisplayDescription(event.data),
      },
      seq: event.seq,
    })
    closeThinkingBeforeLastCall(currentCop!.items, eventTs)
    state.thinkingMustBreakBeforeNext = true
    state.currentCop = currentCop
  }

  const handleToolResult = (event: AssistantTurnEvent) => {
    const toolName = pickToolName(event.data)
    if (toolName === TIMELINE_TITLE_TOOL) return
    if (shouldHideCopTool(toolName)) return
    const toolCallId = pickToolCallId(event)
    const result = extractResultPayload(event)
    const err = extractErrorInfo(event.data, event.error_class)
    attachResultToCop(toolCallId, toolName, result, err.errorClass, err.errorMessage)
    const tail = currentCop?.items.at(-1)
    if (tail?.kind === 'call') {
      state.thinkingMustBreakBeforeNext = true
    }
    state.currentCop = currentCop
  }

  if (event.type === 'run.segment.start') {
    flushCop(eventTs)
    state.thinkingMustBreakBeforeNext = false
    state.currentCop = currentCop
    return
  }

  if (event.type === 'run.segment.end') {
    flushCop(eventTs)
    state.thinkingMustBreakBeforeNext = false
    state.currentCop = currentCop
    return
  }

  if (event.type === 'message.delta') {
    const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
    if (obj.role != null && obj.role !== 'assistant') {
      state.currentCop = currentCop
      return
    }
    const delta = obj.content_delta
    if (typeof delta !== 'string' || delta === '') {
      state.currentCop = currentCop
      return
    }
    if (obj.channel === 'thinking') {
      handleThinkingChannel(delta, event.seq, eventTs)
      return
    }

    handleAssistantTextChannel(delta, event.seq, eventTs)
    return
  }

  if (event.type === 'tool.call') {
    handleToolCall(event)
    return
  }

  if (event.type === 'tool.result') {
    handleToolResult(event)
    return
  }
}

export function finalizeAssistantTurnFoldState(state: AssistantTurnFoldState, endMs?: number): void {
  if (state.currentCop == null) return
  if (!isCopEmpty(state.currentCop)) {
    const target = endMs ?? state.lastEventTimeMs ?? Date.now()
    closeOpenThinkingItems(state.currentCop.items, target)
    state.segments.push({
      type: 'cop',
      title: state.currentCop.title,
      items: state.currentCop.items.map(cloneCopItem),
    })
  }
  state.currentCop = null
}

export function buildAssistantTurnFromEvents(events: readonly AssistantTurnEvent[]): AssistantTurnUi {
  const state = createEmptyAssistantTurnFoldState()
  const orderedEvents = sortAssistantTurnEvents(events)
  for (const event of orderedEvents) {
    foldAssistantTurnEvent(state, event)
  }
  const finalEndMs =
    orderedEvents.length > 0
      ? assistantTurnEventTimeMs(orderedEvents[orderedEvents.length - 1]!)
      : undefined
  finalizeAssistantTurnFoldState(state, finalEndMs)
  const firstTs = orderedEvents.length > 0 ? assistantTurnEventTimeMs(orderedEvents[0]!) : undefined
  const lastTs = orderedEvents.length > 0 ? assistantTurnEventTimeMs(orderedEvents[orderedEvents.length - 1]!) : undefined
  const durationMs = firstTs != null && lastTs != null ? lastTs - firstTs : undefined
  return {
    segments: state.segments.map(cloneSegment),
    ...(durationMs != null && durationMs > 0 ? { durationMs } : {}),
  }
}

export function assistantTurnPlainText(turn: AssistantTurnUi): string {
  let out = ''
  for (const s of turn.segments) {
    if (s.type === 'text') {
      out += s.content
      continue
    }
    for (const it of s.items) {
      if (it.kind === 'assistant_text') out += it.content
    }
  }
  return out
}
