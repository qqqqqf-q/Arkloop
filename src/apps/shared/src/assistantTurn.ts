import { isACPDelegateEventData } from './runEventDelegate'
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
  result?: unknown
  errorClass?: string
}

export type CopBlockItem =
  | { kind: 'thinking'; content: string; seq: number; startedAtMs?: number; endedAtMs?: number }
  | { kind: 'assistant_text'; content: string; seq: number }
  | { kind: 'call'; call: TurnToolCallRef; seq: number }

export type AssistantTurnSegment =
  | { type: 'text'; content: string }
  | { type: 'cop'; title: string | null; items: CopBlockItem[] }

export type AssistantTurnUi = { segments: AssistantTurnSegment[] }

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

function sortRunEvents(events: readonly AssistantTurnEvent[]): AssistantTurnEvent[] {
  return [...events].sort((left, right) => left.seq - right.seq || left.ts.localeCompare(right.ts))
}

function runEventTimeMs(event: AssistantTurnEvent): number {
  const t = Date.parse(event.ts)
  return Number.isFinite(t) ? t : Date.now()
}

function sealOpenThinkingInCop(items: CopBlockItem[], endMs: number): void {
  for (const it of items) {
    if (it.kind !== 'thinking' || it.endedAtMs != null) continue
    if (it.startedAtMs == null) it.startedAtMs = endMs
    it.endedAtMs = endMs
  }
}

function sealThinkingBeforeLatestCall(items: CopBlockItem[], endMs: number): void {
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

function extractResultPayload(event: AssistantTurnEvent): unknown {
  if (!event.data || typeof event.data !== 'object') return undefined
  return (event.data as { result?: unknown }).result
}

function copIsEmpty(cop: { title: string | null; items: CopBlockItem[] }): boolean {
  return cop.items.length === 0
}

function cloneTurnToolCall(c: TurnToolCallRef): TurnToolCallRef {
  return {
    toolCallId: c.toolCallId,
    toolName: c.toolName,
    arguments: { ...c.arguments },
    result: c.result,
    errorClass: c.errorClass,
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

export function drainAssistantTurnForPersist(state: AssistantTurnFoldState, endMs?: number): AssistantTurnUi {
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

function flushCopToSegments(
  segments: AssistantTurnSegment[],
  currentCop: AssistantTurnFoldState['currentCop'],
): void {
  if (currentCop == null) return
  if (!copIsEmpty(currentCop)) {
    segments.push({
      type: 'cop',
      title: currentCop.title,
      items: currentCop.items.map(cloneCopItem),
    })
  }
}

function attachResultToItems(
  items: CopBlockItem[],
  toolCallId: string,
  result: unknown,
  errorClass?: string,
): boolean {
  for (const item of items) {
    if (item.kind !== 'call') continue
    if (item.call.toolCallId !== toolCallId) continue
    item.call.result = result
    if (errorClass) item.call.errorClass = errorClass
    return true
  }
  return false
}

function attachResultToSegments(
  segments: AssistantTurnSegment[],
  toolCallId: string,
  result: unknown,
  errorClass?: string,
): boolean {
  for (let i = segments.length - 1; i >= 0; i--) {
    const segment = segments[i]
    if (segment?.type !== 'cop') continue
    if (attachResultToItems(segment.items, toolCallId, result, errorClass)) {
      return true
    }
  }
  return false
}

export function snapshotAssistantTurn(state: AssistantTurnFoldState): AssistantTurnUi {
  const segments = state.segments.map(cloneSegment)
  flushCopToSegments(segments, state.currentCop)
  return { segments }
}

export function foldAssistantTurnEvent(state: AssistantTurnFoldState, event: AssistantTurnEvent): void {
  const { segments } = state
  let { currentCop } = state
  const eventTs = runEventTimeMs(event)

  const flushCop = (endMs: number) => {
    if (currentCop == null) return
    if (!copIsEmpty(currentCop)) {
      sealOpenThinkingInCop(currentCop.items, endMs)
      segments.push({
        type: 'cop',
        title: currentCop.title,
        items: currentCop.items.map(cloneCopItem),
      })
    }
    currentCop = null
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

  const attachResultToCop = (toolCallId: string, toolName: string, result: unknown, errorClass?: string) => {
    if (currentCop && attachResultToItems(currentCop.items, toolCallId, result, errorClass)) {
      return
    }
    if (attachResultToSegments(segments, toolCallId, result, errorClass)) return
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
      },
      seq: event.seq,
    })
    sealThinkingBeforeLatestCall(targetCop.items, eventTs)
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
    if (isACPDelegateEventData(event.data)) return
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
      ensureCop()
      const items = currentCop!.items
      const last = items[items.length - 1]
      const forceNew = state.thinkingMustBreakBeforeNext
      if (forceNew) {
        state.thinkingMustBreakBeforeNext = false
      }
      if (!forceNew && last?.kind === 'thinking') {
        last.content += delta
        if (last.startedAtMs == null) last.startedAtMs = eventTs
      } else {
        items.push({ kind: 'thinking', content: delta, seq: event.seq, startedAtMs: eventTs })
      }
      state.currentCop = currentCop
      return
    }

    const hasCallsInOpenCop = currentCop != null && currentCop.items.some((i) => i.kind === 'call')

    if (delta.trim() === '') {
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
    return
  }

  if (event.type === 'tool.call') {
    if (isACPDelegateEventData(event.data)) return
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
    ensureCop()
    currentCop!.items.push({
      kind: 'call',
      call: {
        toolCallId: pickToolCallId(event),
        toolName,
        arguments: extractArguments(event.data),
      },
      seq: event.seq,
    })
    sealThinkingBeforeLatestCall(currentCop!.items, eventTs)
    state.thinkingMustBreakBeforeNext = true
    state.currentCop = currentCop
    return
  }

  if (event.type === 'tool.result') {
    if (isACPDelegateEventData(event.data)) return
    const toolName = pickToolName(event.data)
    if (toolName === TIMELINE_TITLE_TOOL) return
    if (shouldHideCopTool(toolName)) return
    const toolCallId = pickToolCallId(event)
    const result = extractResultPayload(event)
    const err =
      typeof event.error_class === 'string' && event.error_class.trim() !== ''
        ? event.error_class
        : undefined
    attachResultToCop(toolCallId, toolName, result, err)
    const tail = currentCop?.items.at(-1)
    if (tail?.kind === 'call') {
      state.thinkingMustBreakBeforeNext = true
    }
    state.currentCop = currentCop
  }
}

export function finalizeAssistantTurnFoldState(state: AssistantTurnFoldState, endMs?: number): void {
  if (state.currentCop == null) return
  if (!copIsEmpty(state.currentCop)) {
    const target = endMs ?? state.lastEventTimeMs ?? Date.now()
    sealOpenThinkingInCop(state.currentCop.items, target)
    state.segments.push({
      type: 'cop',
      title: state.currentCop.title,
      items: state.currentCop.items.map(cloneCopItem),
    })
  }
  state.currentCop = null
}

export function buildAssistantTurnFromRunEvents(events: readonly AssistantTurnEvent[]): AssistantTurnUi {
  const state = createEmptyAssistantTurnFoldState()
  const orderedEvents = sortRunEvents(events)
  for (const event of orderedEvents) {
    foldAssistantTurnEvent(state, event)
  }
  const finalEndMs =
    orderedEvents.length > 0
      ? runEventTimeMs(orderedEvents[orderedEvents.length - 1]!)
      : undefined
  finalizeAssistantTurnFoldState(state, finalEndMs)
  return { segments: state.segments.map(cloneSegment) }
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

export function assistantTurnThinkingPlainText(turn: AssistantTurnUi): string {
  let out = ''
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const it of s.items) {
      if (it.kind === 'thinking') out += it.content
    }
  }
  return out
}
