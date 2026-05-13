import { canonicalToolName, pickLogicalToolName } from '@arkloop/shared'
import type { WebSearchPhaseStep } from './components/CopTimeline'
import type { WebSource } from './storage'
import type { TimelineText } from './timelineText'
import {
  agentEventDataRecord,
  agentEventToolInput,
  agentEventToolOutput,
  type AgentUIEvent,
} from './agent-ui'

export const DEFAULT_SEARCHING_LABEL = 'Searching'
export const COMPLETED_SEARCHING_LABEL = 'Search completed'

function searchStepText(kind: WebSearchPhaseStep['kind'], label: string, status: WebSearchPhaseStep['status']): TimelineText {
  if (kind === 'reviewing') return { kind: 'reviewing_sources' }
  const trimmed = label.trim()
  if (!trimmed || trimmed === DEFAULT_SEARCHING_LABEL || trimmed === COMPLETED_SEARCHING_LABEL) {
    return status === 'done' ? { kind: 'search_completed' } : { kind: 'search', tense: 'live' }
  }
  return { kind: 'content', text: trimmed }
}

function completedSearchLabel(label: string): string {
  const trimmed = label.trim()
  return !trimmed || trimmed === DEFAULT_SEARCHING_LABEL ? COMPLETED_SEARCHING_LABEL : label
}

function completeSearchStep(step: WebSearchPhaseStep): WebSearchPhaseStep {
  const label = completedSearchLabel(step.label)
  return { ...step, label, text: searchStepText(step.kind, label, 'done'), status: 'done' }
}

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
export function applyAgentEventToWebSearchSteps(
  steps: WebSearchPhaseStep[],
  event: AgentUIEvent,
): WebSearchPhaseStep[] {
  if (event.type === 'segment-start') {
    const obj = agentEventDataRecord(event.data) ?? {}
    const segmentId = typeof obj?.segmentId === 'string' ? obj.segmentId : ''
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
      text: searchStepText(stepKind, label, 'active'),
      status: 'active',
      queries,
    }
    return [...steps, step]
  }

  if (event.type === 'segment-end') {
    const obj = agentEventDataRecord(event.data)
    const segmentId = typeof obj?.segmentId === 'string' ? obj.segmentId : ''
    if (!segmentId) return steps
    return steps.map((s) =>
      s.id === segmentId ? completeSearchStep(s) : s,
    )
  }

  if (event.type === 'tool-call') {
    const obj = agentEventDataRecord(event.data)
    const toolName = pickLogicalToolName(event.data, event.toolName)
    if (!isWebSearchToolName(toolName)) return steps
    const callId = typeof obj?.toolCallId === 'string' ? obj.toolCallId : event.id
    if (steps.some((s) => s.id === callId)) return steps
    const args = agentEventToolInput(event.data)
    const queries = webSearchQueriesFromArguments(args)
    const step: WebSearchPhaseStep = {
      id: callId,
      kind: 'searching',
      label: DEFAULT_SEARCHING_LABEL,
      text: { kind: 'search', tense: 'live' },
      status: 'active',
      queries,
      seq: event.order,
    }
    return [...steps, step]
  }

  if (event.type === 'tool-result') {
    const obj = agentEventDataRecord(event.data)
    const toolName = pickLogicalToolName(event.data, event.toolName)
    if (!isWebSearchToolName(toolName)) return steps
    const callId = typeof obj?.toolCallId === 'string' ? obj.toolCallId : event.id
    const sources = webSearchSourcesFromResult(agentEventToolOutput(event.data))
    const next = steps.map((s) =>
      s.id === callId
        ? {
            ...completeSearchStep(s),
            ...(typeof event.order === 'number' ? { resultSeq: event.order } : {}),
            ...(sources ? { sources } : {}),
          }
        : s,
    )
    return next
  }

  if (
    event.type === 'run-completed' ||
    event.type === 'run-failed' ||
    event.type === 'run-cancelled' ||
    event.type === 'run-interrupted'
  ) {
    return steps.map((s) =>
      s.status === 'active' ? completeSearchStep(s) : s,
    )
  }

  return steps
}
