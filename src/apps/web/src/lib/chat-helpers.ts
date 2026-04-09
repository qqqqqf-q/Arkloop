import type { AppError } from '../components/ErrorCallout'
import { isApiError } from '../api'
import { SSEApiError } from '../sse'
import {
  buildAssistantTurnFromRunEvents,
  assistantTurnPlainText,
  copSegmentCalls,
  type AssistantTurnSegment,
  type AssistantTurnUi,
} from '../assistantTurnSegments'
import type { WebSearchPhaseStep } from '../components/CopTimeline'
import { timelineStepDisplayLabel } from '../components/cop-timeline/types'
import type { StreamingArtifactEntry } from '../components/ArtifactStreamBlock'
import type { MessageSearchStepRef, WidgetRef, MsgRunEvent, ThreadRunHandoffRef } from '../storage'

const TERMINAL_RUN_EVENT_TYPES = new Set([
  'run.completed',
  'run.cancelled',
  'run.failed',
  'run.interrupted',
])

export function isTerminalRunEventType(type: string): boolean {
  return TERMINAL_RUN_EVENT_TYPES.has(type)
}

export function normalizeError(error: unknown): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof SSEApiError) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: '请求失败' }
}

export function mergeVisibleSegmentsIntoAssistantTurn(
  turn: AssistantTurnUi,
  liveSegments: Array<{ mode: string; content: string }>,
): AssistantTurnUi {
  const merged = [...turn.segments]
  for (const segment of liveSegments) {
    if (segment.mode === 'hidden') continue
    if (segment.content.trim() === '') continue
    const last = merged[merged.length - 1]
    if (last?.type === 'text') {
      last.content += segment.content
      continue
    }
    merged.push({ type: 'text', content: segment.content })
  }
  return { segments: merged }
}

export function buildFrozenAssistantTurnFromRunEvents(events: MsgRunEvent[]): AssistantTurnUi {
  return buildAssistantTurnFromRunEvents(events)
}

export function interruptedErrorFromRunEvents(
  events: ReadonlyArray<{ type: string; data: unknown }>,
  fallbackMessage: string,
): AppError {
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i]
    if (!event || event.type !== 'run.interrupted') {
      continue
    }
    const data = event.data
    const payload = data && typeof data === 'object' && !Array.isArray(data)
      ? data as Record<string, unknown>
      : {}
    const details = payload.details && typeof payload.details === 'object' && !Array.isArray(payload.details)
      ? payload.details as Record<string, unknown>
      : undefined
    return {
      message: typeof payload.message === 'string' && payload.message.trim() !== ''
        ? payload.message
        : fallbackMessage,
      code: typeof payload.code === 'string'
        ? payload.code
        : typeof payload.error_class === 'string'
          ? payload.error_class
          : undefined,
      details,
    }
  }
  return { message: fallbackMessage }
}

export function failedErrorFromRunEvents(
  events: ReadonlyArray<{ type: string; data: unknown }>,
  fallbackMessage: string,
): AppError {
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i]
    if (!event || event.type !== 'run.failed') {
      continue
    }
    const data = event.data
    const payload = data && typeof data === 'object' && !Array.isArray(data)
      ? data as Record<string, unknown>
      : {}
    const details = payload.details && typeof payload.details === 'object' && !Array.isArray(payload.details)
      ? payload.details as Record<string, unknown>
      : undefined
    return {
      message: typeof payload.message === 'string' && payload.message.trim() !== ''
        ? payload.message
        : fallbackMessage,
      code: typeof payload.code === 'string'
        ? payload.code
        : typeof payload.error_class === 'string'
          ? payload.error_class
          : undefined,
      details,
    }
  }
  return { message: fallbackMessage }
}

export function assistantTurnHasVisibleOutput(turn: AssistantTurnUi | null | undefined): boolean {
  if (!turn) return false
  return assistantTurnPlainText(turn).trim() !== ''
}

function finalizeBlockSteps(steps: WebSearchPhaseStep[]): MessageSearchStepRef[] {
  if (steps.length === 0) return []
  return steps.map((step) => ({
    id: step.id,
    kind: step.kind,
    label: step.label,
    status: 'done',
    queries: step.queries ? [...step.queries] : undefined,
    resultSeq: step.resultSeq,
    sources: step.sources ? [...step.sources] : undefined,
    seq: step.seq,
  }))
}

export function finalizeSearchSteps(steps: WebSearchPhaseStep[]): MessageSearchStepRef[] {
  return finalizeBlockSteps(steps)
}

export function patchLegacySearchSteps(steps: MessageSearchStepRef[]): { steps: MessageSearchStepRef[]; changed: boolean } {
  return { steps, changed: false }
}

export { finalizeBlockSteps }

export function collectCompletedWidgets(entries: StreamingArtifactEntry[]): WidgetRef[] {
  return entries
    .filter((entry) => entry.toolName === 'show_widget' && entry.complete && entry.content && entry.toolCallId)
    .map((entry) => ({
      id: entry.toolCallId!,
      title: entry.title ?? 'Widget',
      html: entry.content!,
    }))
}

export function buildStreamingArtifactsFromHandoff(handoff: ThreadRunHandoffRef): StreamingArtifactEntry[] {
  const entries: StreamingArtifactEntry[] = []
  let toolCallIndex = 0
  for (const widget of handoff.widgets) {
    entries.push({
      toolCallIndex,
      toolCallId: widget.id,
      toolName: 'show_widget',
      argumentsBuffer: '',
      title: widget.title,
      content: widget.html,
      complete: true,
    })
    toolCallIndex += 1
  }
  for (const artifact of handoff.artifacts) {
    entries.push({
      toolCallIndex,
      toolCallId: artifact.key,
      toolName: 'create_artifact',
      argumentsBuffer: '',
      title: artifact.title,
      filename: artifact.filename,
      display: artifact.display,
      complete: true,
      artifactRef: artifact,
    })
    toolCallIndex += 1
  }
  return entries
}

export type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>

export function widgetToolCallIdsPlacedInTurn(turn: AssistantTurnUi, widgets: WidgetRef[] | undefined | null): Set<string> {
  const placed = new Set<string>()
  const want = new Set((widgets ?? []).map((w) => w.id))
  if (want.size === 0) return placed
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'show_widget' && want.has(c.toolCallId)) placed.add(c.toolCallId)
    }
  }
  return placed
}

export function historicWidgetsForCop(seg: CopSegment, widgets: WidgetRef[] | undefined | null): WidgetRef[] {
  if (!widgets?.length) return []
  const ids = new Set(copSegmentCalls(seg).filter((c) => c.toolName === 'show_widget').map((c) => c.toolCallId))
  if (ids.size === 0) return []
  return widgets.filter((w) => ids.has(w.id))
}

export function liveStreamingWidgetEntriesForCop(seg: CopSegment, entries: StreamingArtifactEntry[]): StreamingArtifactEntry[] {
  const out: StreamingArtifactEntry[] = []
  for (const c of copSegmentCalls(seg)) {
    if (c.toolName !== 'show_widget') continue
    const e = entries.find((x) => x.toolName === 'show_widget' && x.toolCallId === c.toolCallId)
    if (!e) continue
    if ((e.content != null && e.content.length > 0) || (e.loadingMessages != null && e.loadingMessages.length > 0)) {
      out.push(e)
    }
  }
  return out
}

export function liveInlineArtifactEntriesForCop(seg: CopSegment, entries: StreamingArtifactEntry[]): StreamingArtifactEntry[] {
  const out: StreamingArtifactEntry[] = []
  for (const c of copSegmentCalls(seg)) {
    if (c.toolName !== 'create_artifact') continue
    const e = entries.find((x) => x.toolName === 'create_artifact' && x.toolCallId === c.toolCallId)
    if (e && e.content && e.display !== 'panel') out.push(e)
  }
  return out
}

export function liveCopShowWidgetCallIds(turn: AssistantTurnUi | null): Set<string> {
  const ids = new Set<string>()
  if (!turn) return ids
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'show_widget' && c.toolCallId) ids.add(c.toolCallId)
    }
  }
  return ids
}

export function liveCopCreateArtifactCallIds(turn: AssistantTurnUi | null): Set<string> {
  const ids = new Set<string>()
  if (!turn) return ids
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'create_artifact' && c.toolCallId) ids.add(c.toolCallId)
    }
  }
  return ids
}

export function liveTurnHasThinkingSegment(turn: AssistantTurnUi | null): boolean {
  if (!turn) return false
  return turn.segments.some(
    (s) => s.type === 'cop' && s.items.some((i) => i.kind === 'thinking'),
  )
}

export function thinkingBlockDurationSec(
  it: CopSegment['items'][number],
): number {
  if (it.kind !== 'thinking') return 0
  if (it.startedAtMs == null || it.endedAtMs == null) return 0
  return Math.max(0, Math.round((it.endedAtMs - it.startedAtMs) / 1000))
}

export function thinkingRowsForCop(
  seg: CopSegment,
  opts: { live: boolean; segmentIndex: number; lastSegmentIndex: number },
): Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec: number; startedAtMs?: number }> {
  let lastThinkIdx = -1
  for (let i = seg.items.length - 1; i >= 0; i--) {
    if (seg.items[i]?.kind === 'thinking') {
      lastThinkIdx = i
      break
    }
  }
  const tailKind = seg.items[seg.items.length - 1]?.kind
  const out: Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec: number; startedAtMs?: number }> = []
  seg.items.forEach((it, itemIdx) => {
    if (it.kind !== 'thinking') return
    const rowLive =
      opts.live &&
      opts.segmentIndex === opts.lastSegmentIndex &&
      itemIdx === lastThinkIdx &&
      tailKind === 'thinking'
    out.push({
      id: `think-${opts.segmentIndex}-${itemIdx}-${it.seq}`,
      markdown: it.content,
      seq: it.seq,
      live: rowLive,
      durationSec: thinkingBlockDurationSec(it),
      startedAtMs: it.startedAtMs,
    })
  })
  return out
}

export function copInlineTextRowsForCop(
  seg: CopSegment,
  opts: { live: boolean; segmentIndex: number; lastSegmentIndex: number },
): Array<{ id: string; text: string; live?: boolean; seq: number }> {
  let lastInlineIdx = -1
  for (let i = seg.items.length - 1; i >= 0; i--) {
    if (seg.items[i]?.kind === 'assistant_text') {
      lastInlineIdx = i
      break
    }
  }
  const out: Array<{ id: string; text: string; live?: boolean; seq: number }> = []
  seg.items.forEach((it, itemIdx) => {
    if (it.kind !== 'assistant_text') return
    const rowLive =
      opts.live && opts.segmentIndex === opts.lastSegmentIndex && itemIdx === lastInlineIdx
    out.push({
      id: `inline-${opts.segmentIndex}-${itemIdx}-${it.seq}`,
      text: it.content,
      seq: it.seq,
      live: rowLive,
    })
  })
  return out
}

export function turnHasCopThinkingItems(turn: AssistantTurnUi): boolean {
  return turn.segments.some(
    (s) => s.type === 'cop' && s.items.some((i) => i.kind === 'thinking'),
  )
}

export function resolveCopHeaderOverride(params: {
  title?: string | null
  steps: WebSearchPhaseStep[]
  hasCodeExecutions: boolean
  hasSubAgents: boolean
  hasFileOps: boolean
  hasWebFetches: boolean
  hasGenericTools: boolean
  hasThinking: boolean
  handoffStatus?: 'completed' | 'cancelled' | 'interrupted' | 'failed' | null
  labels: {
    stopped: string
    failed: string
    liveProgress: string
    thinking: string
  }
}): string | undefined {
  const explicitTitle = params.title?.trim()
  if (explicitTitle) {
    return explicitTitle
  }
  if (params.handoffStatus === 'completed') {
    return undefined
  }
  const statusLabel =
    params.handoffStatus === 'cancelled' || params.handoffStatus === 'interrupted'
      ? params.labels.stopped
      : params.handoffStatus === 'failed'
        ? params.labels.failed
        : undefined
  if (params.steps.length > 0) {
    return statusLabel ?? timelineStepDisplayLabel(params.steps[params.steps.length - 1]!) ?? params.labels.liveProgress
  }
  if (params.hasCodeExecutions || params.hasSubAgents || params.hasFileOps || params.hasWebFetches || params.hasGenericTools) {
    return statusLabel ?? params.labels.liveProgress
  }
  if (params.hasThinking) {
    return statusLabel ? undefined : params.labels.thinking
  }
  return statusLabel
}
