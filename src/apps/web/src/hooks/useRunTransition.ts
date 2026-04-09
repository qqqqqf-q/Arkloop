import { useCallback } from 'react'
import { useChatSession } from '../contexts/chat-session'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useMessageMeta } from '../contexts/message-meta'
import { useStream } from '../contexts/stream'
import { createEmptyAssistantTurnFoldState, snapshotAssistantTurn, type AssistantTurnUi } from '../assistantTurnSegments'
import { collectCompletedWidgets } from '../lib/chat-helpers'
import { clearThreadRunHandoff, type ArtifactRef, type BrowserActionRef, type CodeExecutionRef, type FileOpRef, type MessageSearchStepRef, type MessageTerminalStatusRef, type MsgRunEvent, type SubAgentRef, type WebFetchRef, type WebSource, type WidgetRef } from '../storage'

export type TerminalRunCache = {
  runSources: WebSource[]
  runArtifacts: ArtifactRef[]
  runWidgets: WidgetRef[]
  runCodeExecs: CodeExecutionRef[]
  runBrowserActions: BrowserActionRef[]
  runSubAgents: SubAgentRef[]
  runFileOps: FileOpRef[]
  runWebFetches: WebFetchRef[]
  runAssistantTurn: AssistantTurnUi
  handoffAssistantTurn: AssistantTurnUi
  pendingSearchSteps?: MessageSearchStepRef[] | null
  terminalStatus?: MessageTerminalStatusRef | null
}

type PersistRunDataOptions = {
  persistThinking?: boolean
  persistAssistantTurn?: boolean
  cacheAssistantTurn?: boolean
}

export function useRunTransition() {
  const { threadId } = useChatSession()
  const {
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    terminalRunHandoffStatus,
    setAwaitingInput,
    setPendingUserInput,
    setCheckInDraft,
  } = useRunLifecycle()
  const {
    persistRunDataToMessage: persistRunDataToMessageState,
    persistThreadRunHandoff: persistThreadRunHandoffState,
    clearRunRefs,
    currentRunSourcesRef,
    currentRunArtifactsRef,
    currentRunCodeExecutionsRef,
    currentRunBrowserActionsRef,
    currentRunSubAgentsRef,
    currentRunFileOpsRef,
    currentRunWebFetchesRef,
    pendingSearchStepsRef,
  } = useMessageMeta()
  const {
    assistantTurnFoldStateRef,
    setPreserveLiveRunUi,
    setLiveAssistantTurn,
    setPendingThinking,
    setSegments,
    activeSegmentIdRef,
    setTopLevelCodeExecutions,
    setTopLevelSubAgents,
    setTopLevelFileOps,
    setTopLevelWebFetches,
    streamingArtifactsRef,
    setStreamingArtifacts,
    resetSearchSteps,
  } = useStream()

  const resetAssistantTurnLive = useCallback(() => {
    assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
    setPreserveLiveRunUi(false)
    setLiveAssistantTurn(null)
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
  }, [assistantTurnFoldStateRef, setLiveAssistantTurn, setPreserveLiveRunUi, setTerminalRunDisplayId, setTerminalRunHandoffStatus])

  const clearLiveRunTransientState = useCallback(() => {
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    clearRunRefs()
  }, [clearRunRefs, setStreamingArtifacts, streamingArtifactsRef])

  const clearLiveRunSecurityArtifacts = useCallback(() => {
    clearLiveRunTransientState()
    resetAssistantTurnLive()
    setPendingThinking(false)
    setTopLevelCodeExecutions([])
    setSegments([])
    activeSegmentIdRef.current = null
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    resetSearchSteps()
    pendingSearchStepsRef.current = null
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
  }, [
    activeSegmentIdRef,
    clearLiveRunTransientState,
    currentRunArtifactsRef,
    currentRunBrowserActionsRef,
    currentRunCodeExecutionsRef,
    currentRunSourcesRef,
    pendingSearchStepsRef,
    resetAssistantTurnLive,
    resetSearchSteps,
    setAwaitingInput,
    setCheckInDraft,
    setPendingThinking,
    setPendingUserInput,
    setSegments,
    setTopLevelCodeExecutions,
  ])

  const releaseCompletedHandoffToHistory = useCallback(() => {
    assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
    setPreserveLiveRunUi(false)
    setLiveAssistantTurn(null)
    setPendingThinking(false)
    setSegments([])
    activeSegmentIdRef.current = null
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    clearLiveRunTransientState()
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
  }, [
    activeSegmentIdRef,
    assistantTurnFoldStateRef,
    clearLiveRunTransientState,
    setLiveAssistantTurn,
    setPendingThinking,
    setPreserveLiveRunUi,
    setSegments,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    setTopLevelCodeExecutions,
    setTopLevelFileOps,
    setTopLevelSubAgents,
    setTopLevelWebFetches,
  ])

  const captureTerminalRunCache = useCallback((terminalStatus?: MessageTerminalStatusRef | null): TerminalRunCache => {
    const handoffAssistantTurn = snapshotAssistantTurn(assistantTurnFoldStateRef.current)
    return {
      runSources: [...currentRunSourcesRef.current],
      runArtifacts: [...currentRunArtifactsRef.current],
      runWidgets: collectCompletedWidgets(streamingArtifactsRef.current),
      runCodeExecs: [...currentRunCodeExecutionsRef.current],
      runBrowserActions: [...currentRunBrowserActionsRef.current],
      runSubAgents: [...currentRunSubAgentsRef.current],
      runFileOps: [...currentRunFileOpsRef.current],
      runWebFetches: [...currentRunWebFetchesRef.current],
      runAssistantTurn: handoffAssistantTurn,
      handoffAssistantTurn,
      pendingSearchSteps: pendingSearchStepsRef.current,
      terminalStatus: terminalStatus ?? terminalRunHandoffStatus,
    }
  }, [
    assistantTurnFoldStateRef,
    currentRunArtifactsRef,
    currentRunBrowserActionsRef,
    currentRunCodeExecutionsRef,
    currentRunFileOpsRef,
    currentRunSourcesRef,
    currentRunSubAgentsRef,
    currentRunWebFetchesRef,
    pendingSearchStepsRef,
    streamingArtifactsRef,
    terminalRunHandoffStatus,
  ])

  const persistRunDataToMessage = useCallback((
    messageId: string,
    runData: TerminalRunCache,
    runEvents: MsgRunEvent[],
    options?: PersistRunDataOptions,
  ) => {
    persistRunDataToMessageState(messageId, runData, runEvents, options)
    if (threadId) {
      clearThreadRunHandoff(threadId)
    }
  }, [persistRunDataToMessageState, threadId])

  const persistThreadRunHandoff = useCallback((runId: string, runData: TerminalRunCache) => {
    if (!threadId || !runId) return
    persistThreadRunHandoffState(threadId, runId, runData)
  }, [persistThreadRunHandoffState, threadId])

  return {
    resetAssistantTurnLive,
    clearLiveRunTransientState,
    clearLiveRunSecurityArtifacts,
    releaseCompletedHandoffToHistory,
    captureTerminalRunCache,
    persistRunDataToMessage,
    persistThreadRunHandoff,
  }
}
