import type { WebSearchPhaseStep } from './components/CopTimeline'
import type { RunEvent } from './sse'

/** 不同模型/供应商可能用 web_search、web_search.tavily、大小写或连字符变体 */
export function isWebSearchToolName(toolName: string): boolean {
  const t = toolName.trim()
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
    const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; arguments?: unknown }
    const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
    if (!isWebSearchToolName(toolName)) return steps
    const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
    if (steps.some((s) => s.id === callId)) return steps
    const args = obj.arguments as Record<string, unknown> | undefined
    const queries = webSearchQueriesFromArguments(args)
    const step: WebSearchPhaseStep = {
      id: callId,
      kind: 'searching',
      label: 'Searching',
      status: 'active',
      queries,
    }
    return [...steps, step]
  }

  if (event.type === 'tool.result') {
    const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; result?: unknown }
    const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
    if (!isWebSearchToolName(toolName)) return steps
    const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
    let next = steps.map((s) =>
      s.id === callId ? { ...s, status: 'done' as const } : s,
    )
    const allSearchDone = next
      .filter((s) => s.kind === 'searching')
      .every((s) => s.status === 'done')
    if (allSearchDone && !next.some((s) => s.kind === 'reviewing')) {
      next = [
        ...next,
        {
          id: 'auto-reviewing',
          kind: 'reviewing' as const,
          label: 'Reviewing sources',
          status: 'active' as const,
        },
      ]
    }
    return next
  }

  if (
    event.type === 'run.completed' ||
    event.type === 'run.failed' ||
    event.type === 'run.cancelled'
  ) {
    return steps.map((s) =>
      s.status === 'active' ? { ...s, status: 'done' as const } : s,
    )
  }

  return steps
}
