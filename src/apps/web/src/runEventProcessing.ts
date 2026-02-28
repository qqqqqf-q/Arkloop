import type { RunEvent } from './sse'
import type { MessageThinkingRef } from './storage'

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
