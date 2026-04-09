import { isACPDelegateEventData, canonicalToolName, pickLogicalToolName } from '@arkloop/shared'
import type { WebSearchPhaseStep } from './components/CopTimeline'
import type { WebSource } from './storage'
import type { RunEvent } from './sse'

export const DEFAULT_SEARCHING_LABEL = 'Searching'
export const COMPLETED_SEARCHING_LABEL = 'Search completed'

/** 不同模型/供应商可能用 web_search、web_search.tavily、大小写或连字符变体 */
export function isWebSearchToolName(toolName: string): boolean {
  const t = canonicalToolName(toolName)
  if (!t) return false
  const n = t.toLowerCase().replace(/-/g, '_')
  if (n === 'web_search' || n === 'websearch') return true
  return n.startsWith('web_search.')
}

export function webSearchQueriesFromArguments(
  args: Record<string, unknown> | undefined,
): string[] | undefined {
  if (!args) return undefined
  const out: string[] = []
  if (typeof args.query === 'string' && args.query.trim()) out.push(args.query.trim())
  if (Array.isArray(args.queries)) {
    for (const q of args.queries) {
      if (typeof q === 'string' && q.trim()) out.push(q.trim())
    }
  }
  return out.length ? out : undefined
}

export function webSearchSourcesFromResult(result: unknown): WebSource[] | undefined {
  if (!result || typeof result !== 'object') return undefined
  const raw = (result as { results?: unknown }).results
  if (!Array.isArray(raw)) return undefined
  const sources = raw
    .filter((item): item is Record<string, unknown> => item != null && typeof item === 'object')
    .map((item): WebSource | null => {
      const url = typeof item.url === 'string' ? item.url : ''
      if (!url) return null
      return {
        title: typeof item.title === 'string' ? item.title : '',
        url,
        snippet: typeof item.snippet === 'string' ? item.snippet : undefined,
      }
    })
    .filter((item): item is WebSource => item != null)
  return sources.length > 0 ? sources : undefined
}

/**
 * 与 useSubAgentCop reducer 中的步骤逻辑一致（不含 sources），供主会话 COP 时间轴增量更新。
 */
export function applyRunEventToWebSearchSteps(
  steps: WebSearchPhaseStep[],
  event: RunEvent,
): WebSearchPhaseStep[] {
  if (event.type === 'run.segment.start') {
    const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
    const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
    const kind = typeof obj.kind === 'string' ? obj.kind : ''
    if (!segmentId || !kind.startsWith('search_')) return steps
    if (kind === 'search_planning') return steps
    const stepKind: WebSearchPhaseStep['kind'] =
      kind === 'search_queries' ? 'searching'
      : kind === 'search_reviewing' ? 'reviewing'
      : 'searching'
    const display = (obj.display ?? {}) as { label?: unknown; queries?: unknown }
    const label = typeof display.label === 'string' ? display.label : ''
    const queries = Array.isArray(display.queries)
      ? (display.queries as unknown[]).filter((q): q is string => typeof q === 'string')
      : undefined
    const step: WebSearchPhaseStep = {
      id: segmentId,
      kind: stepKind,
      label,
      status: 'active',
      queries,
    }
    return [...steps, step]
  }

  if (event.type === 'run.segment.end') {
    const obj = event.data as { segment_id?: unknown }
    const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
    if (!segmentId) return steps
    return steps.map((s) =>
      s.id === segmentId ? { ...s, status: 'done' as const } : s,
    )
  }

  if (event.type === 'tool.call') {
    if (isACPDelegateEventData(event.data)) return steps
    const obj = event.data as { tool_call_id?: unknown; arguments?: unknown }
    const toolName = pickLogicalToolName(event.data, event.tool_name)
    if (!isWebSearchToolName(toolName)) return steps
    const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
    if (steps.some((s) => s.id === callId)) return steps
    const args = obj.arguments as Record<string, unknown> | undefined
    const queries = webSearchQueriesFromArguments(args)
    const step: WebSearchPhaseStep = {
      id: callId,
      kind: 'searching',
      label: DEFAULT_SEARCHING_LABEL,
      status: 'active',
      queries,
      seq: event.seq,
    }
    return [...steps, step]
  }

  if (event.type === 'tool.result') {
    if (isACPDelegateEventData(event.data)) return steps
    const obj = event.data as { tool_call_id?: unknown; result?: unknown }
    const toolName = pickLogicalToolName(event.data, event.tool_name)
    if (!isWebSearchToolName(toolName)) return steps
    const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
    const sources = webSearchSourcesFromResult(obj.result)
    const next = steps.map((s) =>
      s.id === callId
        ? {
            ...s,
            status: 'done' as const,
            ...(s.label.trim() === DEFAULT_SEARCHING_LABEL ? { label: COMPLETED_SEARCHING_LABEL } : {}),
            ...(typeof event.seq === 'number' ? { resultSeq: event.seq } : {}),
            ...(sources ? { sources } : {}),
          }
        : s,
    )
    return next
  }

  if (
    event.type === 'run.completed' ||
    event.type === 'run.failed' ||
    event.type === 'run.cancelled' ||
    event.type === 'run.interrupted'
  ) {
    if (isACPDelegateEventData(event.data)) return steps
    return steps.map((s) =>
      s.status === 'active' ? { ...s, status: 'done' as const } : s,
    )
  }

  return steps
}
