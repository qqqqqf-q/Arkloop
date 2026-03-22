import { isACPDelegateEventData } from '@arkloop/shared'
import type { RunEvent } from './sse'

export type TurnToolCallRef = {
  toolCallId: string
  toolName: string
  arguments: Record<string, unknown>
  result?: unknown
  errorClass?: string
}

/** COP 段内有序项：与多 tool 同段，thinking 不单独成顶层 segment */
export type CopBlockItem =
  | { kind: 'thinking'; content: string; seq: number; startedAtMs?: number; endedAtMs?: number }
  | { kind: 'assistant_text'; content: string; seq: number }
  | { kind: 'call'; call: TurnToolCallRef; seq: number }

export type AssistantTurnSegment =
  | { type: 'text'; content: string }
  | { type: 'cop'; title: string | null; items: CopBlockItem[] }

export type AssistantTurnUi = { segments: AssistantTurnSegment[] }

/** SSE 递增折叠用状态（事件按 seq 递增到达，禁止对 live 路径全量 sort）。 */
export type AssistantTurnFoldState = {
  segments: AssistantTurnSegment[]
  currentCop: { type: 'cop'; title: string | null; items: CopBlockItem[] } | null
  /** 为 true 时下一次 thinking 必须新开一项（不拼进上一段 thinking） */
  thinkingMustBreakBeforeNext: boolean
}

const TIMELINE_TITLE_TOOL = 'timeline_title'

/**
 * 无 tool 的 open cop 内可累计的可见短正文上限（thinking 之后的主通道正文已 flush 成 text，不进此项）。
 */
const MAX_COP_INLINE_ASSISTANT_CHARS = 512

export function copSegmentCalls(segment: { type: 'cop'; items: CopBlockItem[] }): TurnToolCallRef[] {
  return segment.items.filter((i): i is Extract<CopBlockItem, { kind: 'call' }> => i.kind === 'call').map((i) => i.call)
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

function sortRunEvents(events: readonly RunEvent[]): RunEvent[] {
  return [...events].sort((left, right) => left.seq - right.seq || left.ts.localeCompare(right.ts))
}

function runEventTimeMs(event: RunEvent): number {
  const t = Date.parse(event.ts)
  return Number.isFinite(t) ? t : Date.now()
}

/** flush / finalize：仍未结束的 thinking 用同一时刻收口 */
function sealOpenThinkingInCop(items: CopBlockItem[], endMs: number): void {
  for (const it of items) {
    if (it.kind !== 'thinking' || it.endedAtMs != null) continue
    if (it.startedAtMs == null) it.startedAtMs = endMs
    it.endedAtMs = endMs
  }
}

/**
 * 新 tool call 已追加在 items 末尾时：从倒数第二项向前扫到「上一个 call」为止，
 * 其间所有未结束的 thinking 打上 endedAt（覆盖 thinking → 短正文 → call）。
 */
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

function extractResultPayload(event: RunEvent): unknown {
  if (!event.data || typeof event.data !== 'object') return undefined
  return (event.data as { result?: unknown }).result
}

function copIsEmpty(cop: { title: string | null; items: CopBlockItem[] }): boolean {
  return cop.title == null && cop.items.length === 0
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

/** 结束 run 时收尾并取出不可变快照，清空 fold state。 */
export function drainAssistantTurnForPersist(state: AssistantTurnFoldState): AssistantTurnUi {
  finalizeAssistantTurnFoldState(state)
  const turn: AssistantTurnUi = { segments: state.segments.map(cloneSegment) }
  state.segments = []
  state.currentCop = null
  state.thinkingMustBreakBeforeNext = false
  return turn
}

export function createEmptyAssistantTurnFoldState(): AssistantTurnFoldState {
  return { segments: [], currentCop: null, thinkingMustBreakBeforeNext: false }
}

/** segment 可见正文、run 段起止等与 tool 同类：后续 thinking 不得并回上一块 */
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

/** 将当前 open cop 结束前推入 segments 的不可变快照（供 React state）。 */
export function snapshotAssistantTurn(state: AssistantTurnFoldState): AssistantTurnUi {
  const segments = state.segments.map(cloneSegment)
  flushCopToSegments(segments, state.currentCop)
  return { segments }
}

/** 处理单条事件（仅 message.delta / tool.call / tool.result）；可变 state。 */
export function foldAssistantTurnEvent(state: AssistantTurnFoldState, event: RunEvent): void {
  const { segments } = state
  let { currentCop } = state

  const flushCop = () => {
    if (currentCop == null) return
    if (!copIsEmpty(currentCop)) {
      sealOpenThinkingInCop(currentCop.items, Date.now())
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
    flushCop()
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
    if (!currentCop) return
    for (const item of currentCop.items) {
      if (item.kind !== 'call') continue
      if (item.call.toolCallId !== toolCallId) continue
      item.call.result = result
      if (errorClass) item.call.errorClass = errorClass
      return
    }
    const ts = runEventTimeMs(event)
    currentCop.items.push({
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
    sealThinkingBeforeLatestCall(currentCop.items, ts)
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
      const ts = runEventTimeMs(event)
      if (forceNew) {
        state.thinkingMustBreakBeforeNext = false
      }
      if (!forceNew && last?.kind === 'thinking') {
        last.content += delta
        if (last.startedAtMs == null) last.startedAtMs = ts
      } else {
        items.push({ kind: 'thinking', content: delta, seq: event.seq, startedAtMs: ts })
      }
      state.currentCop = currentCop
      return
    }

    const hasCallsInOpenCop = currentCop != null && currentCop.items.some((i) => i.kind === 'call')

    if (delta.trim() === '') {
      if (currentCop != null && !hasCallsInOpenCop) {
        const lastItem = currentCop.items[currentCop.items.length - 1]
        if (lastItem?.kind === 'thinking' || lastItem?.kind === 'assistant_text') {
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
      // thinking 之后的主通道正文应是独立 text 段，不要并进 COP 时间轴（否则整段回复挂在「Thought」下面）
      if (lastCopItem?.kind === 'thinking') {
        appendAssistantDelta(delta)
        state.currentCop = currentCop
        return
      }
      const inlineUsed = currentCop.items
        .filter((i): i is Extract<CopBlockItem, { kind: 'assistant_text' }> => i.kind === 'assistant_text')
        .reduce((n, i) => n + i.content.length, 0)
      if (inlineUsed + delta.length <= MAX_COP_INLINE_ASSISTANT_CHARS) {
        const items = currentCop.items
        const last = items[items.length - 1]
        if (last?.kind === 'assistant_text') {
          last.content += delta
        } else {
          items.push({ kind: 'assistant_text', content: delta, seq: event.seq })
        }
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
    ensureCop()
    const ts = runEventTimeMs(event)
    currentCop!.items.push({
      kind: 'call',
      call: {
        toolCallId: pickToolCallId(event),
        toolName,
        arguments: extractArguments(event.data),
      },
      seq: event.seq,
    })
    sealThinkingBeforeLatestCall(currentCop!.items, ts)
    state.thinkingMustBreakBeforeNext = true
    state.currentCop = currentCop
    return
  }

  if (event.type === 'tool.result') {
    if (isACPDelegateEventData(event.data)) return
    const toolName = pickToolName(event.data)
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

/** run 结束时关闭未决 cop（仍在同一 state 上操作，再 snapshot）。 */
export function finalizeAssistantTurnFoldState(state: AssistantTurnFoldState): void {
  if (state.currentCop == null) return
  if (!copIsEmpty(state.currentCop)) {
    sealOpenThinkingInCop(state.currentCop.items, Date.now())
    state.segments.push({
      type: 'cop',
      title: state.currentCop.title,
      items: state.currentCop.items.map(cloneCopItem),
    })
  }
  state.currentCop = null
}

/** 从一次 run 的事件流构建 assistant turn（重放时按 seq 排序）。 */
export function buildAssistantTurnFromRunEvents(events: readonly RunEvent[]): AssistantTurnUi {
  const state = createEmptyAssistantTurnFoldState()
  for (const event of sortRunEvents(events)) {
    foldAssistantTurnEvent(state, event)
  }
  finalizeAssistantTurnFoldState(state)
  return { segments: state.segments.map(cloneSegment) }
}

/** 将所有 text 段与 COP 内 assistant_text 按段顺序拼接（复制、与 message.content 对照）。 */
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

/** COP 内全部 thinking 拼接（与 MessageThinkingRef.thinkingText 对齐）。 */
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
