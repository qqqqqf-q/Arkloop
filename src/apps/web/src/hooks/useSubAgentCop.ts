import { useEffect, useReducer, useRef } from 'react'
import { useSSE } from './useSSE'
import type { SearchStep } from '../components/SearchTimeline'
import type { WebSource } from '../storage'
import type { RunEvent } from '../sse'

type CopState = {
  steps: SearchStep[]
  sources: WebSource[]
  isComplete: boolean
}

type CopAction =
  | { type: 'segment_start'; segmentId: string; kind: string; label: string; queries?: string[] }
  | { type: 'segment_end'; segmentId: string }
  | { type: 'web_search_call'; callId: string; queries?: string[] }
  | { type: 'web_search_result'; callId: string; sources: WebSource[] }
  | { type: 'complete' }
  | { type: 'reset' }

const initialState: CopState = { steps: [], sources: [], isComplete: false }

function reducer(state: CopState, action: CopAction): CopState {
  switch (action.type) {
    case 'segment_start': {
      if (action.kind === 'search_planning') return state
      const stepKind: SearchStep['kind'] =
        action.kind === 'search_queries' ? 'searching'
        : action.kind === 'search_reviewing' ? 'reviewing'
        : 'searching'
      const step: SearchStep = {
        id: action.segmentId,
        kind: stepKind,
        label: action.label,
        status: 'active',
        queries: action.queries,
      }
      return { ...state, steps: [...state.steps, step] }
    }

    case 'segment_end':
      return {
        ...state,
        steps: state.steps.map((s) =>
          s.id === action.segmentId ? { ...s, status: 'done' as const } : s,
        ),
      }

    case 'web_search_call': {
      // 只在没有 segment 覆盖时补一个 searching 步骤
      if (state.steps.some((s) => s.id === action.callId)) return state
      const step: SearchStep = {
        id: action.callId,
        kind: 'searching',
        label: 'Searching',
        status: 'active',
        queries: action.queries,
      }
      return { ...state, steps: [...state.steps, step] }
    }

    case 'web_search_result': {
      let steps = state.steps.map((s) =>
        s.id === action.callId ? { ...s, status: 'done' as const } : s,
      )
      const allSearchDone = steps
        .filter((s) => s.kind === 'searching')
        .every((s) => s.status === 'done')
      if (allSearchDone && !steps.some((s) => s.kind === 'reviewing')) {
        steps = [
          ...steps,
          {
            id: 'auto-reviewing',
            kind: 'reviewing' as const,
            label: 'Reviewing sources',
            status: 'active' as const,
          },
        ]
      }
      return { ...state, steps, sources: [...state.sources, ...action.sources] }
    }

    case 'complete':
      return {
        ...state,
        isComplete: true,
        steps: state.steps.map((s) =>
          s.status === 'active' ? { ...s, status: 'done' as const } : s,
        ),
      }

    case 'reset':
      return initialState

    default:
      return state
  }
}

function processEvent(event: RunEvent, dispatch: React.Dispatch<CopAction>): void {
  if (event.type === 'run.segment.start') {
    const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
    const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
    const kind = typeof obj.kind === 'string' ? obj.kind : ''
    if (!segmentId || !kind.startsWith('search_')) return
    const display = (obj.display ?? {}) as { label?: unknown; queries?: unknown }
    const label = typeof display.label === 'string' ? display.label : ''
    const queries = Array.isArray(display.queries)
      ? (display.queries as unknown[]).filter((q): q is string => typeof q === 'string')
      : undefined
    dispatch({ type: 'segment_start', segmentId, kind, label, queries })
    return
  }

  if (event.type === 'run.segment.end') {
    const obj = event.data as { segment_id?: unknown }
    const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
    if (segmentId) dispatch({ type: 'segment_end', segmentId })
    return
  }

  if (event.type === 'tool.call') {
    const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; arguments?: unknown }
    const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
    if (toolName === 'web_search' || toolName.startsWith('web_search.')) {
      const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
      const args = obj.arguments as Record<string, unknown> | undefined
      const queries = typeof args?.query === 'string' ? [args.query] : undefined
      dispatch({ type: 'web_search_call', callId, queries })
    }
    return
  }

  if (event.type === 'tool.result') {
    const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; result?: unknown }
    const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
    if (toolName === 'web_search' || toolName.startsWith('web_search.')) {
      const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
      const result = obj.result as { results?: unknown[] } | undefined
      const sources: WebSource[] = Array.isArray(result?.results)
        ? (result.results as unknown[])
            .filter((r): r is Record<string, unknown> => r != null && typeof r === 'object')
            .map((r) => ({
              title: typeof r.title === 'string' ? r.title : '',
              url: typeof r.url === 'string' ? r.url : '',
              snippet: typeof r.snippet === 'string' ? r.snippet : undefined,
            }))
            .filter((s) => !!s.url)
        : []
      dispatch({ type: 'web_search_result', callId, sources })
    }
    return
  }

  if (
    event.type === 'run.completed' ||
    event.type === 'run.failed' ||
    event.type === 'run.cancelled'
  ) {
    dispatch({ type: 'complete' })
  }
}

export type SubAgentCopResult = CopState & { isStreaming: boolean }

export function useSubAgentCop(params: {
  runId: string | undefined
  accessToken: string
  baseUrl?: string
  enabled: boolean
}): SubAgentCopResult {
  const { runId, accessToken, baseUrl = '', enabled } = params
  const [state, dispatch] = useReducer(reducer, initialState)
  const processedCountRef = useRef(0)

  const sse = useSSE({ runId: runId ?? '', accessToken, baseUrl })

  const prevRunIdRef = useRef(runId)
  useEffect(() => {
    if (prevRunIdRef.current !== runId) {
      prevRunIdRef.current = runId
      processedCountRef.current = 0
      dispatch({ type: 'reset' })
    }
  }, [runId])

  // 随 enabled 状态连接/断开
  useEffect(() => {
    if (enabled && runId) {
      sse.connect()
    } else {
      sse.disconnect()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, runId])

  // 处理新到事件
  useEffect(() => {
    if (sse.events.length <= processedCountRef.current) return
    const fresh = sse.events.slice(processedCountRef.current)
    processedCountRef.current = sse.events.length
    for (const event of fresh) {
      processEvent(event, dispatch)
    }
  }, [sse.events])

  const isStreaming =
    enabled &&
    !!runId &&
    (sse.state === 'connecting' || sse.state === 'connected' || sse.state === 'reconnecting') &&
    !state.isComplete

  return { ...state, isStreaming }
}
