import { useEffect, useReducer, useRef } from 'react'
import { pickLogicalToolName } from '@arkloop/shared'
import { useAgentStream } from './useAgentStream'
import {
  agentEventDataRecord,
  agentEventToolInput,
  agentEventToolOutput,
  useAgentClient,
} from '../agent-ui'
import type { WebSearchPhaseStep } from '../components/CopTimeline'
import type { WebSource } from '../storage'
import type { AgentUIEvent } from '../agent-ui'
import {
  COMPLETED_SEARCHING_LABEL,
  DEFAULT_SEARCHING_LABEL,
  isWebSearchToolName,
  isXSearchToolName,
  webSearchQueriesFromArguments,
  webSearchSourcesFromResult,
} from '../webSearchTimelineFromAgentEvent'

type CopState = {
  steps: WebSearchPhaseStep[]
  sources: WebSource[]
  isComplete: boolean
}

type CopAction =
  | { type: 'segment_start'; segmentId: string; kind: string; label: string; queries?: string[] }
  | { type: 'segment_end'; segmentId: string }
  | { type: 'web_search_call'; callId: string; queries?: string[]; sourceKind: 'web' | 'x' }
  | { type: 'web_search_result'; callId: string; sources: WebSource[]; sourceKind: 'web' | 'x' }
  | { type: 'complete' }
  | { type: 'reset' }

const initialState: CopState = { steps: [], sources: [], isComplete: false }

function reducer(state: CopState, action: CopAction): CopState {
  switch (action.type) {
    case 'segment_start': {
      if (action.kind === 'search_planning') return state
      const stepKind: WebSearchPhaseStep['kind'] =
        action.kind === 'search_queries' ? 'searching'
        : action.kind === 'search_reviewing' ? 'reviewing'
        : 'searching'
      const step: WebSearchPhaseStep = {
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
      const step: WebSearchPhaseStep = {
        id: action.callId,
        kind: 'searching',
        label: DEFAULT_SEARCHING_LABEL,
        status: 'active',
        queries: action.queries,
        sourceKind: action.sourceKind,
      }
      return { ...state, steps: [...state.steps, step] }
    }

    case 'web_search_result': {
      let steps = state.steps.map((s) =>
        s.id === action.callId
          ? {
              ...s,
              status: 'done' as const,
              ...(s.label.trim() === DEFAULT_SEARCHING_LABEL ? { label: COMPLETED_SEARCHING_LABEL } : {}),
            }
          : s,
      )
      const searchSteps = steps.filter((s) => s.kind === 'searching')
      const allSearchDone = searchSteps.every((s) => s.status === 'done')
      const onlyXSearch = searchSteps.length > 0 && searchSteps.every((s) => s.sourceKind === 'x')
      if (allSearchDone && !onlyXSearch && !steps.some((s) => s.kind === 'reviewing')) {
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

function processEvent(event: AgentUIEvent, dispatch: React.Dispatch<CopAction>): void {
  if (event.type === 'segment-start') {
    const obj = agentEventDataRecord(event.data) ?? {}
    const segmentId = typeof obj?.segmentId === 'string' ? obj.segmentId : ''
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

  if (event.type === 'segment-end') {
    const obj = agentEventDataRecord(event.data)
    const segmentId = typeof obj?.segmentId === 'string' ? obj.segmentId : ''
    if (segmentId) dispatch({ type: 'segment_end', segmentId })
    return
  }

  if (event.type === 'tool-call') {
    const obj = agentEventDataRecord(event.data)
    const toolName = pickLogicalToolName(event.data, event.toolName)
    if (isWebSearchToolName(toolName)) {
      const callId = typeof obj?.toolCallId === 'string' ? obj.toolCallId : event.id
      const args = agentEventToolInput(event.data)
      const queries = webSearchQueriesFromArguments(args)
      dispatch({ type: 'web_search_call', callId, queries, sourceKind: isXSearchToolName(toolName) ? 'x' : 'web' })
    }
    return
  }

  if (event.type === 'tool-result') {
    const obj = agentEventDataRecord(event.data)
    const toolName = pickLogicalToolName(event.data, event.toolName)
    if (isWebSearchToolName(toolName)) {
      const callId = typeof obj?.toolCallId === 'string' ? obj.toolCallId : event.id
      const sources = webSearchSourcesFromResult(agentEventToolOutput(event.data)) ?? []
      dispatch({ type: 'web_search_result', callId, sources, sourceKind: isXSearchToolName(toolName) ? 'x' : 'web' })
    }
    return
  }

  if (
    event.type === 'run-completed' ||
    event.type === 'run-failed' ||
    event.type === 'run-cancelled' ||
    event.type === 'run-interrupted'
  ) {
    dispatch({ type: 'complete' })
  }
}

export type SubAgentCopResult = CopState & { isStreaming: boolean }

export function useSubAgentCop(params: {
  runId: string | undefined
  enabled: boolean
}): SubAgentCopResult {
  const { runId, enabled } = params
  const agentClient = useAgentClient()
  const [state, dispatch] = useReducer(reducer, initialState)
  const processedCountRef = useRef(0)
  const drainEventsRef = useRef<() => void>(() => {})

  const sse = useAgentStream({ runId: runId ?? '', client: agentClient })

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

  useEffect(() => {
    return sse.subscribeEvents(() => {
      drainEventsRef.current()
    })
  }, [sse])

  const drainEvents = () => {
    if (sse.events.length <= processedCountRef.current) return
    const fresh = sse.events.slice(processedCountRef.current)
    processedCountRef.current = sse.events.length
    for (const event of fresh) {
      processEvent(event, dispatch)
    }
  }

  useEffect(() => {
    drainEventsRef.current = drainEvents
    drainEvents()
  })

  const isStreaming =
    enabled &&
    !!runId &&
    (sse.state === 'connecting' || sse.state === 'connected' || sse.state === 'reconnecting') &&
    !state.isComplete

  return { ...state, isStreaming }
}
