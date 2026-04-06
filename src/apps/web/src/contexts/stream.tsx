import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
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
import type { RunEvent } from '../sse'

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
  workTodos: Array<{ id: string; content: string; status: string }>

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
  foldAssistantTurnEvent: (event: RunEvent) => void
  bumpSnapshot: () => void
  resetLiveState: () => void
  setWorkTodos: React.Dispatch<React.SetStateAction<Array<{ id: string; content: string; status: string }>>>
  setPreserveLiveRunUi: (v: boolean) => void
  setLiveAssistantTurn: React.Dispatch<React.SetStateAction<AssistantTurnUi | null>>
  requestAssistantTurnThinkingBreak: () => void
  releaseCompletedHandoffToHistory: () => void
  resetSearchSteps: () => void
}

const StreamContext = createContext<StreamContextValue | null>(null)

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
  const [workTodos, setWorkTodos] = useState<Array<{ id: string; content: string; status: string }>>([])

  const segmentsRef = useRef<Segment[]>([])
  useEffect(() => { segmentsRef.current = segments }, [segments])
  const searchStepsRef = useRef<WebSearchPhaseStep[]>([])
  const streamingArtifactsRef = useRef<StreamingArtifactEntry[]>([])
  const activeSegmentIdRef = useRef<string | null>(null)
  const assistantTurnFoldStateRef = useRef<AssistantTurnFoldState>(createEmptyAssistantTurnFoldState())

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

  const foldAssistantTurnEvent = useCallback((event: RunEvent) => {
    foldEvent(assistantTurnFoldStateRef.current, event)
  }, [])

  const bumpSnapshot = useCallback(() => {
    setLiveAssistantTurn(snapshotAssistantTurn(assistantTurnFoldStateRef.current))
  }, [])

  const setPreserveLiveRunUi = useCallback((v: boolean) => {
    setPreserveLiveRunUiState(v)
  }, [])

  const resetLiveState = useCallback(() => {
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
