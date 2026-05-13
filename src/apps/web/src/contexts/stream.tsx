import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react'
import type { CodeExecutionRef, FileOpRef, SubAgentRef, ThreadRunHandoffRef, WebFetchRef } from '../storage'
import type { StreamingArtifactEntry } from '../components/ArtifactStreamBlock'
import type { WebSearchPhaseStep } from '../components/CopTimeline'
import type {
  AssistantTurnFoldState,
  AssistantTurnUi,
} from '../assistantTurnSegments'
import {
  createEmptyAssistantTurnFoldState,
  foldAssistantTurnEvent as foldEvent,
  requestAssistantTurnThinkingBreak as requestThinkingBreak,
  snapshotAssistantTurn,
} from '../assistantTurnSegments'
import type { AgentUIEvent } from '../agent-ui'

export type Segment = {
  segmentId: string
  kind: string
  mode: string
  label: string
  content: string
  isStreaming: boolean
  codeExecutions: CodeExecutionRef[]
}

interface StreamContextValue {
  segments: Segment[]
  streamingArtifacts: StreamingArtifactEntry[]
  pendingThinking: boolean
  thinkingHint: string
  searchSteps: WebSearchPhaseStep[]
  topLevelCodeExecutions: CodeExecutionRef[]
  topLevelSubAgents: SubAgentRef[]
  topLevelFileOps: FileOpRef[]
  topLevelWebFetches: WebFetchRef[]
  liveAssistantTurn: AssistantTurnUi | null
  preserveLiveRunUi: boolean
  workTodos: Array<{ id: string; content: string; activeForm?: string; status: string }>

  // internal refs (双写：SSE 热路径先写 ref，渲染时 flush)
  segmentsRef: React.RefObject<Segment[]>
  searchStepsRef: React.RefObject<WebSearchPhaseStep[]>
  streamingArtifactsRef: React.RefObject<StreamingArtifactEntry[]>
  activeSegmentIdRef: React.RefObject<string | null>
  assistantTurnFoldStateRef: React.RefObject<AssistantTurnFoldState>

  setSegments: React.Dispatch<React.SetStateAction<Segment[]>>
  setStreamingArtifacts: React.Dispatch<React.SetStateAction<StreamingArtifactEntry[]>>
  setPendingThinking: (v: boolean) => void
  setThinkingHint: (hint: string) => void
  setSearchSteps: React.Dispatch<React.SetStateAction<WebSearchPhaseStep[]>>
  addTopLevelCodeExecution: (exec: CodeExecutionRef) => void
  setTopLevelCodeExecutions: React.Dispatch<React.SetStateAction<CodeExecutionRef[]>>
  addTopLevelSubAgent: (agent: SubAgentRef) => void
  setTopLevelSubAgents: React.Dispatch<React.SetStateAction<SubAgentRef[]>>
  addTopLevelFileOp: (op: FileOpRef) => void
  setTopLevelFileOps: React.Dispatch<React.SetStateAction<FileOpRef[]>>
  addTopLevelWebFetch: (fetch: WebFetchRef) => void
  setTopLevelWebFetches: React.Dispatch<React.SetStateAction<WebFetchRef[]>>
  foldAssistantTurnEvent: (event: AgentUIEvent) => void
  bumpSnapshot: (opts?: { immediate?: boolean }) => void
  resetLiveState: () => void
  setWorkTodos: React.Dispatch<React.SetStateAction<Array<{ id: string; content: string; activeForm?: string; status: string }>>>
  setPreserveLiveRunUi: (v: boolean) => void
  setLiveAssistantTurn: React.Dispatch<React.SetStateAction<AssistantTurnUi | null>>
  requestAssistantTurnThinkingBreak: () => void
  releaseCompletedHandoffToHistory: () => void
  resetSearchSteps: () => void

  // 流式增量更新 API（不走 setState，直接 mutate ref + notify subscribers）
  appendSegmentContent: (segmentId: string, delta: string) => void
  endSegmentStream: (segmentId: string) => void
  addSegment: (segment: Segment) => void
  flushSegmentsRefToState: () => void
}

const StreamContext = createContext<StreamContextValue | null>(null)

// 单个 segment content 的订阅系统，用于 useStreamingContent
type ContentListener = () => void
const contentListeners = new Map<string, Set<ContentListener>>()

function notifyContentListeners(segmentId: string) {
  const listeners = contentListeners.get(segmentId)
  if (listeners) {
    for (const cb of listeners) {
      cb()
    }
  }
}

export function useStreamingContent(segmentId: string | null | undefined): string {
  return useSyncExternalStore(
    useCallback((callback: () => void) => {
      if (!segmentId) return () => {}
      let listeners = contentListeners.get(segmentId)
      if (!listeners) {
        listeners = new Set()
        contentListeners.set(segmentId, listeners)
      }
      listeners.add(callback)
      return () => {
        listeners!.delete(callback)
        if (listeners!.size === 0) {
          contentListeners.delete(segmentId)
        }
      }
    }, [segmentId]),
    () => {
      // 在 StreamProvider 内部，通过 context 获取最新值
      // 但 useSyncExternalStore 要求 getSnapshot 是 O(1) 且稳定
      // 这里我们通过一个全局 weak ref 来访问当前 provider 的 segmentsRef
      // 实际上由于 React 的同步渲染保证，这个 snapshot 会在 render 时读取
      return segmentId ? (globalSegmentsRef?.current.find((s) => s.segmentId === segmentId)?.content ?? '') : ''
    },
    () => '',
  )
}

// 全局 ref，让 useStreamingContent 的 getSnapshot 能访问到当前 provider 的 segmentsRef
let globalSegmentsRef: React.RefObject<Segment[]> | null = null

export function StreamProvider({ children }: { children: ReactNode }) {
  const [segments, setSegments] = useState<Segment[]>([])
  const [streamingArtifacts, setStreamingArtifacts] = useState<StreamingArtifactEntry[]>([])
  const [pendingThinking, setPendingThinking] = useState(false)
  const [thinkingHint, setThinkingHint] = useState('')
  const [searchSteps, setSearchSteps] = useState<WebSearchPhaseStep[]>([])
  const [topLevelCodeExecutions, setTopLevelCodeExecutions] = useState<CodeExecutionRef[]>([])
  const [topLevelSubAgents, setTopLevelSubAgents] = useState<SubAgentRef[]>([])
  const [topLevelFileOps, setTopLevelFileOps] = useState<FileOpRef[]>([])
  const [topLevelWebFetches, setTopLevelWebFetches] = useState<WebFetchRef[]>([])
  const [liveAssistantTurn, setLiveAssistantTurn] = useState<AssistantTurnUi | null>(null)
  const [preserveLiveRunUi, setPreserveLiveRunUiState] = useState(false)
  const [workTodos, setWorkTodos] = useState<Array<{ id: string; content: string; activeForm?: string; status: string }>>([])

  const segmentsRef = useRef<Segment[]>([])
  useEffect(() => { segmentsRef.current = segments }, [segments])
  // 同步到全局 ref，供 useStreamingContent 读取
  useEffect(() => {
    globalSegmentsRef = segmentsRef
    return () => { globalSegmentsRef = null }
  }, [])
  const searchStepsRef = useRef<WebSearchPhaseStep[]>([])
  const streamingArtifactsRef = useRef<StreamingArtifactEntry[]>([])
  const activeSegmentIdRef = useRef<string | null>(null)
  const assistantTurnFoldStateRef = useRef<AssistantTurnFoldState>(createEmptyAssistantTurnFoldState())
  const bumpPendingRef = useRef(false)
  const bumpRafRef = useRef<number | null>(null)

  const addTopLevelCodeExecution = useCallback((exec: CodeExecutionRef) => {
    setTopLevelCodeExecutions((prev) => [...prev, exec])
  }, [])

  const addTopLevelSubAgent = useCallback((agent: SubAgentRef) => {
    setTopLevelSubAgents((prev) => [...prev, agent])
  }, [])

  const addTopLevelFileOp = useCallback((op: FileOpRef) => {
    setTopLevelFileOps((prev) => [...prev, op])
  }, [])

  const addTopLevelWebFetch = useCallback((fetch: WebFetchRef) => {
    setTopLevelWebFetches((prev) => [...prev, fetch])
  }, [])

  const foldAssistantTurnEvent = useCallback((event: AgentUIEvent) => {
    foldEvent(assistantTurnFoldStateRef.current, event)
  }, [])

  const bumpSnapshot = useCallback((opts?: { immediate?: boolean }) => {
    if (opts?.immediate) {
      if (bumpRafRef.current !== null) {
        cancelAnimationFrame(bumpRafRef.current)
        bumpRafRef.current = null
      }
      bumpPendingRef.current = false
      setLiveAssistantTurn(snapshotAssistantTurn(assistantTurnFoldStateRef.current))
      return
    }

    if (bumpPendingRef.current) return
    bumpPendingRef.current = true

    bumpRafRef.current = requestAnimationFrame(() => {
      bumpPendingRef.current = false
      bumpRafRef.current = null
      setLiveAssistantTurn(snapshotAssistantTurn(assistantTurnFoldStateRef.current))
    })
  }, [])

  const setPreserveLiveRunUi = useCallback((v: boolean) => {
    setPreserveLiveRunUiState(v)
  }, [])

  const resetLiveState = useCallback(() => {
    if (bumpRafRef.current !== null) {
      cancelAnimationFrame(bumpRafRef.current)
      bumpRafRef.current = null
    }
    bumpPendingRef.current = false
    setSegments([])
    segmentsRef.current = []
    setStreamingArtifacts([])
    streamingArtifactsRef.current = []
    setSearchSteps([])
    searchStepsRef.current = []
    activeSegmentIdRef.current = null
    assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
    setPendingThinking(false)
    setThinkingHint('')
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    setLiveAssistantTurn(null)
    setPreserveLiveRunUiState(false)
    setWorkTodos([])
  }, [])

  const requestAssistantTurnThinkingBreakAction = useCallback(() => {
    requestThinkingBreak(assistantTurnFoldStateRef.current)
  }, [])

  const releaseCompletedHandoffToHistory = useCallback(() => {
    assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
    setPreserveLiveRunUiState(false)
    setLiveAssistantTurn(null)
    setPendingThinking(false)
    setSegments([])
    segmentsRef.current = []
    activeSegmentIdRef.current = null
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
  }, [])

  const resetSearchSteps = useCallback(() => {
    searchStepsRef.current = []
    setSearchSteps([])
  }, [])

  // 流式增量更新：直接 mutate ref + notify subscribers，不走 setState
  const appendSegmentContent = useCallback((segmentId: string, delta: string) => {
    const seg = segmentsRef.current.find((s) => s.segmentId === segmentId)
    if (seg && seg.mode !== 'hidden') {
      seg.content += delta
      notifyContentListeners(segmentId)
    }
  }, [])

  const endSegmentStream = useCallback((segmentId: string) => {
    const seg = segmentsRef.current.find((s) => s.segmentId === segmentId)
    if (seg) {
      seg.isStreaming = false
      notifyContentListeners(segmentId)
    }
  }, [])

  const addSegment = useCallback((segment: Segment) => {
    segmentsRef.current = [...segmentsRef.current, segment]
    // 新 segment 需要触发一次整体渲染（让 MessageList 知道有新 segment）
    setSegments(segmentsRef.current)
  }, [])

  const flushSegmentsRefToState = useCallback(() => {
    setSegments([...segmentsRef.current])
  }, [])

  const value = useMemo<StreamContextValue>(() => ({
    segments,
    streamingArtifacts,
    pendingThinking,
    thinkingHint,
    searchSteps,
    topLevelCodeExecutions,
    topLevelSubAgents,
    topLevelFileOps,
    topLevelWebFetches,
    liveAssistantTurn,
    preserveLiveRunUi,
    workTodos,
    segmentsRef,
    searchStepsRef,
    streamingArtifactsRef,
    activeSegmentIdRef,
    assistantTurnFoldStateRef,
    setSegments,
    setStreamingArtifacts,
    setPendingThinking,
    setThinkingHint,
    setSearchSteps,
    addTopLevelCodeExecution,
    setTopLevelCodeExecutions,
    addTopLevelSubAgent,
    setTopLevelSubAgents,
    addTopLevelFileOp,
    setTopLevelFileOps,
    addTopLevelWebFetch,
    setTopLevelWebFetches,
    foldAssistantTurnEvent,
    bumpSnapshot,
    resetLiveState,
    setWorkTodos,
    setPreserveLiveRunUi,
    setLiveAssistantTurn,
    requestAssistantTurnThinkingBreak: requestAssistantTurnThinkingBreakAction,
    releaseCompletedHandoffToHistory,
    resetSearchSteps,
    appendSegmentContent,
    endSegmentStream,
    addSegment,
    flushSegmentsRefToState,
  }), [
    segments,
    streamingArtifacts,
    pendingThinking,
    thinkingHint,
    searchSteps,
    topLevelCodeExecutions,
    topLevelSubAgents,
    topLevelFileOps,
    topLevelWebFetches,
    liveAssistantTurn,
    preserveLiveRunUi,
    workTodos,
    addTopLevelCodeExecution,
    addTopLevelSubAgent,
    addTopLevelFileOp,
    addTopLevelWebFetch,
    foldAssistantTurnEvent,
    bumpSnapshot,
    resetLiveState,
    setPreserveLiveRunUi,
    requestAssistantTurnThinkingBreakAction,
    releaseCompletedHandoffToHistory,
    resetSearchSteps,
    appendSegmentContent,
    endSegmentStream,
    addSegment,
    flushSegmentsRefToState,
  ])

  return (
    <StreamContext.Provider value={value}>
      {children}
    </StreamContext.Provider>
  )
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

export function useStream(): StreamContextValue {
  const ctx = useContext(StreamContext)
  if (!ctx) throw new Error('useStream must be used within StreamProvider')
  return ctx
}
