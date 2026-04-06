import { useEffect, useRef, useCallback } from 'react'
import { useAuth } from '../contexts/auth'
import { useChatSession } from '../contexts/chat-session'
import { useCredits } from '../contexts/credits'
import { useMessageMeta } from '../contexts/message-meta'
import { useMessageStore } from '../contexts/message-store'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useStream } from '../contexts/stream'
import { useThreadList } from '../contexts/thread-list'
import { useRunTransition, type TerminalRunCache } from './useRunTransition'
import { SSEApiError } from '../sse'
import {
  applyCodeExecutionToolCall,
  applyCodeExecutionToolResult,
  applyTerminalDelta,
  patchCodeExecutionList,
  findAssistantMessageForRun,
  selectFreshRunEvents,
  applyBrowserToolCall,
  applyBrowserToolResult,
  applySubAgentToolCall,
  applySubAgentToolResult,
  applyFileOpToolCall,
  applyFileOpToolResult,
  applyWebFetchToolCall,
  applyWebFetchToolResult,
  isWebFetchToolName,
} from '../runEventProcessing'
import {
  foldAssistantTurnEvent,
  requestAssistantTurnThinkingBreak,
} from '../assistantTurnSegments'
import {
  applyRunEventToWebSearchSteps,
  isWebSearchToolName,
  webSearchSourcesFromResult,
} from '../webSearchTimelineFromRunEvent'
import {
  isTerminalRunEventType,
  mergeVisibleSegmentsIntoAssistantTurn,
  buildFrozenAssistantTurnFromRunEvents,
  finalizeSearchSteps,
} from '../lib/chat-helpers'
import { extractPartialArtifactFields } from '../components/ArtifactStreamBlock'
import type { ArtifactRef } from '../storage'
import type { MsgRunEvent } from '../storage'
import { getInjectionBlockMessage, shouldSuppressLiveRunEventAfterInjectionBlock } from '../liveRunSecurity'
import { isACPDelegateEventData } from '@arkloop/shared'
import type { RequestedSchema } from '../userInputTypes'

type UseThreadSseEffectDeps = {
  restoreQueuedDraftToInput: () => void
  clearQueuedDraft: () => void
  forceInstantBottomScrollRef: React.RefObject<boolean>
  lastUserMsgRef: React.RefObject<HTMLDivElement | null>
}

export function useThreadSseEffect({
  restoreQueuedDraftToInput,
  clearQueuedDraft,
  forceInstantBottomScrollRef,
  lastUserMsgRef,
}: UseThreadSseEffectDeps): void {
  const { logout: onLoggedOut } = useAuth()
  const { threadId } = useChatSession()
  const {
    markIdle: onRunEnded,
    updateTitle: onThreadTitleUpdated,
  } = useThreadList()
  const { refreshCredits } = useCredits()
  const {
    activeRunId,
    setActiveRunId,
    setCancelSubmitting,
    setError,
    setInjectionBlocked,
    injectionBlockedRunIdRef,
    setQueuedDraft,
    setAwaitingInput,
    setPendingUserInput,
    setCheckInDraft,
    contextCompactBar: _contextCompactBar,
    setContextCompactBar,
    terminalRunDisplayId: _terminalRunDisplayId,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    markTerminalRunHistory: markTerminalRunHistoryState,
    completedTitleTailRunId,
    clearCompletedTitleTail: clearCompletedTitleTailState,
    armCompletedTitleTail: armCompletedTitleTailState,
    sse,
    sseRunId,
    processedEventCountRef,
    freezeCutoffRef,
    lastVisibleNonTerminalSeqRef,
    sseTerminalFallbackRunIdRef,
    sseTerminalFallbackArmedRef,
    noResponseMsgIdRef,
    replaceOnCancelRef,
    pendingMessageRef,
    seenFirstToolCallInRunRef,
  } = useRunLifecycle()
  const {
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
    setLiveAssistantTurn,
    setPreserveLiveRunUi,
    assistantTurnFoldStateRef,
    bumpSnapshot: bumpAssistantTurnSnapshot,
    searchStepsRef,
    setSearchSteps,
    resetSearchSteps,
    setStreamingArtifacts,
    streamingArtifactsRef,
    setSegments,
    segmentsRef,
    activeSegmentIdRef,
    setPendingThinking,
    setThinkingHint: _setThinkingHint,
    setTopLevelCodeExecutions,
    setTopLevelSubAgents,
    setTopLevelFileOps,
    setTopLevelWebFetches,
    setWorkTodos,
  } = useStream()
  const {
    refreshMessages,
    sendMessageRef,
  } = useMessageStore()
  const {
    resetAssistantTurnLive,
    clearLiveRunSecurityArtifacts,
    releaseCompletedHandoffToHistory,
    captureTerminalRunCache,
    persistRunDataToMessage,
    persistThreadRunHandoff,
  } = useRunTransition({ forceInstantBottomScrollRef, lastUserMsgRef })

  const markTerminalRunHistory = useCallback((messageId: string | null, expanded = true) => {
    markTerminalRunHistoryState(messageId, expanded)
  }, [markTerminalRunHistoryState])
  const clearCompletedTitleTail = useCallback(() => {
    clearCompletedTitleTailState()
  }, [clearCompletedTitleTailState])
  const armCompletedTitleTail = useCallback((runId: string) => {
    armCompletedTitleTailState(runId)
  }, [armCompletedTitleTailState])

  const contextCompactHideTimerRef = useRef<number | null>(null)
  const clearContextCompactHideTimer = useCallback(() => {
    if (contextCompactHideTimerRef.current != null) {
      clearTimeout(contextCompactHideTimerRef.current)
      contextCompactHideTimerRef.current = null
    }
  }, [])

  useEffect(() => {
    return () => { clearContextCompactHideTimer() }
  }, [clearContextCompactHideTimer])

  // SSE 事件处理
  useEffect(() => {
    if (!sseRunId) return
    const resetTerminalRunState = (options?: {
      restoreQueuedDraft?: boolean
      preserveSearchSteps?: boolean
      handoffRunCache?: TerminalRunCache
    }) => {
      freezeCutoffRef.current = null
      injectionBlockedRunIdRef.current = null
      clearCompletedTitleTail()
      sse.disconnect()
      setActiveRunId(null)
      setCancelSubmitting(false)
      setPendingThinking(false)
      const handoffRunCache = options?.handoffRunCache
      if (handoffRunCache) {
        setPreserveLiveRunUi(true)
        setLiveAssistantTurn(
          handoffRunCache.handoffAssistantTurn.segments.length > 0
            ? handoffRunCache.handoffAssistantTurn
            : null,
        )
      } else {
        setPreserveLiveRunUi(false)
        setLiveAssistantTurn(null)
        setTopLevelCodeExecutions([])
        setTopLevelSubAgents([])
        setTopLevelFileOps([])
        setTopLevelWebFetches([])
      }
      if (options?.restoreQueuedDraft) {
        restoreQueuedDraftToInput()
      } else {
        clearQueuedDraft()
      }
      if (!handoffRunCache) {
        streamingArtifactsRef.current = []
        setStreamingArtifacts([])
        setSegments([])
        resetAssistantTurnLive()
        activeSegmentIdRef.current = null
        currentRunSourcesRef.current = []
        currentRunArtifactsRef.current = []
        currentRunCodeExecutionsRef.current = []
        currentRunBrowserActionsRef.current = []
        currentRunSubAgentsRef.current = []
        currentRunFileOpsRef.current = []
        currentRunWebFetchesRef.current = []
      }
      if (!options?.preserveSearchSteps) {
        resetSearchSteps()
      }
      pendingSearchStepsRef.current = null
      setAwaitingInput(false)
      setPendingUserInput(null)
      setCheckInDraft('')
      if (threadId) onRunEnded(threadId)
    }
    const { fresh, nextProcessedCount } = selectFreshRunEvents({
      events: sse.events,
      activeRunId: sseRunId,
      processedCount: processedEventCountRef.current,
    })
    processedEventCountRef.current = nextProcessedCount
    for (const event of fresh) {
      const freezeCutoff = freezeCutoffRef.current
      if (
        freezeCutoff != null &&
        typeof event.seq === 'number' &&
        event.seq > freezeCutoff &&
        !isTerminalRunEventType(event.type)
      ) {
        continue
      }
      if (shouldSuppressLiveRunEventAfterInjectionBlock({
        activeRunId,
        blockedRunId: injectionBlockedRunIdRef.current,
        event,
      })) {
        continue
      }
      const nextWebSearchSteps = applyRunEventToWebSearchSteps(searchStepsRef.current, event)
      if (nextWebSearchSteps !== searchStepsRef.current) {
        searchStepsRef.current = nextWebSearchSteps
        setSearchSteps(nextWebSearchSteps)
      }

      if (event.type === 'run.segment.start') {
        const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        const kind = typeof obj.kind === 'string' ? obj.kind : 'planning_round'
        const display = (obj.display ?? {}) as { mode?: unknown; label?: unknown; queries?: unknown }
        const mode = typeof display.mode === 'string' ? display.mode : 'collapsed'
        const label = typeof display.label === 'string' ? display.label : ''
        if (!segmentId) continue
        activeSegmentIdRef.current = segmentId
        requestAssistantTurnThinkingBreak(assistantTurnFoldStateRef.current)
        if (kind.startsWith('search_')) {
          continue
        }
        setSegments((prev) => [...prev, { segmentId, kind, mode, label, content: '', isStreaming: true, codeExecutions: [] }])
        continue
      }

      if (event.type === 'run.context_compact') {
        const obj = event.data as { phase?: unknown; op?: unknown; dropped_prefix?: unknown }
        const op = typeof obj.op === 'string' ? obj.op : undefined
        const phase = typeof obj.phase === 'string' ? obj.phase : undefined

        if (op === 'persist') {
          if (phase === 'started') {
            clearContextCompactHideTimer()
            setContextCompactBar({ type: 'persist', status: 'running' })
          } else if (phase === 'completed' || phase === undefined) {
            clearContextCompactHideTimer()
            setContextCompactBar({ type: 'persist', status: 'done' })
            contextCompactHideTimerRef.current = window.setTimeout(() => {
              setContextCompactBar(null)
              contextCompactHideTimerRef.current = null
            }, 2800)
          } else if (phase === 'llm_failed') {
            clearContextCompactHideTimer()
            setContextCompactBar({ type: 'persist', status: 'llm_failed' })
            contextCompactHideTimerRef.current = window.setTimeout(() => {
              setContextCompactBar(null)
              contextCompactHideTimerRef.current = null
            }, 4000)
          }
        } else if (op === 'trim') {
          if (phase === 'completed') {
            const dropped = typeof obj.dropped_prefix === 'number' ? obj.dropped_prefix : 0
            if (dropped > 0) {
              clearContextCompactHideTimer()
              setContextCompactBar({ type: 'trim', status: 'done', dropped })
              contextCompactHideTimerRef.current = window.setTimeout(() => {
                setContextCompactBar(null)
                contextCompactHideTimerRef.current = null
              }, 1500)
            }
          }
        }
        continue
      }

      if (event.type === 'todo.updated') {
        const obj = event.data as { todos?: unknown }
        if (Array.isArray(obj.todos)) {
          const items = (obj.todos as unknown[]).flatMap((t) => {
            if (!t || typeof t !== 'object') return []
            const item = t as { id?: unknown; content?: unknown; status?: unknown }
            if (typeof item.id !== 'string' || typeof item.content !== 'string' || typeof item.status !== 'string') return []
            return [{ id: item.id, content: item.content, status: item.status }]
          })
          setWorkTodos(items)
        }
        continue
      }

      if (event.type === 'run.segment.end') {
        const obj = event.data as { segment_id?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        if (segmentId && activeSegmentIdRef.current === segmentId) {
          activeSegmentIdRef.current = null
        }
        requestAssistantTurnThinkingBreak(assistantTurnFoldStateRef.current)
        setSegments((prev) =>
          prev.map((s) => (s.segmentId === segmentId ? { ...s, isStreaming: false } : s)),
        )
        continue
      }

      if (event.type === 'message.delta') {
        if (isACPDelegateEventData(event.data)) continue
        noResponseMsgIdRef.current = null
        const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
        if (obj.role != null && obj.role !== 'assistant') continue
        if (typeof obj.content_delta !== 'string' || !obj.content_delta) continue
        const delta = obj.content_delta
        const channel = typeof obj.channel === 'string' ? obj.channel : ''
        const isThinking = channel === 'thinking'
        const eventSeq = typeof event.seq === 'number' ? event.seq : 0
        if (!isThinking && channel.trim() === '') {
          if (eventSeq > lastVisibleNonTerminalSeqRef.current) {
            lastVisibleNonTerminalSeqRef.current = eventSeq
          }
        }
        const activeSeg = activeSegmentIdRef.current
        if (isThinking) {
          setPendingThinking(false)
          foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
          bumpAssistantTurnSnapshot()
          continue
        }
        setPendingThinking(false)
        if (activeSeg) {
          requestAssistantTurnThinkingBreak(assistantTurnFoldStateRef.current)
          setSegments((prev) =>
            prev.map((s) =>
              s.segmentId === activeSeg && s.mode !== 'hidden'
                ? { ...s, content: s.content + delta }
                : s,
            ),
          )
          continue
        }
        foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
        bumpAssistantTurnSnapshot()
        continue
      }

      if (event.type === 'tool.call.delta') {
        const obj = event.data as { tool_call_index?: number; tool_call_id?: string; tool_name?: string; arguments_delta?: string }
        const idx = typeof obj.tool_call_index === 'number' ? obj.tool_call_index : -1
        if (idx >= 0 && typeof obj.arguments_delta === 'string') {
          let entry = streamingArtifactsRef.current.find((e) => e.toolCallIndex === idx)
          if (!entry) {
            entry = { toolCallIndex: idx, argumentsBuffer: '', complete: false }
            streamingArtifactsRef.current = [...streamingArtifactsRef.current, entry]
          }
          if (obj.tool_call_id) entry.toolCallId = obj.tool_call_id
          if (obj.tool_name) entry.toolName = obj.tool_name
          entry.argumentsBuffer += obj.arguments_delta

          if (entry.toolName === 'show_widget' || entry.toolName === 'create_artifact' || (!entry.toolName && (entry.argumentsBuffer.includes('"content"') || entry.argumentsBuffer.includes('"widget_code"')))) {
            const parsed = extractPartialArtifactFields(entry.argumentsBuffer)
            if (parsed.title !== undefined) entry.title = parsed.title
            if (parsed.filename !== undefined) entry.filename = parsed.filename
            if (parsed.display !== undefined) entry.display = parsed.display as 'inline' | 'panel'
            if (parsed.content !== undefined) entry.content = parsed.content
            setStreamingArtifacts([...streamingArtifactsRef.current])
          }
        }
        continue
      }

      if (event.type === 'tool.call') {
        if (isACPDelegateEventData(event.data)) continue
        setPendingThinking(false)
        seenFirstToolCallInRunRef.current = true
        const obj = event.data as { tool_name?: unknown; llm_name?: unknown; tool_call_id?: unknown; arguments?: unknown }
        const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : event.tool_name
        const codeExecutionCall = applyCodeExecutionToolCall(currentRunCodeExecutionsRef.current, event)
        if (codeExecutionCall.appended) {
          const entry = codeExecutionCall.appended
          currentRunCodeExecutionsRef.current = codeExecutionCall.nextExecutions
          const activeSeg = activeSegmentIdRef.current
          if (activeSeg) {
            setSegments((prev) =>
              prev.map((s) =>
                s.segmentId === activeSeg
                  ? { ...s, codeExecutions: [...s.codeExecutions, entry] }
                  : s,
              ),
            )
          }
          setTopLevelCodeExecutions((prev) => [...prev, entry])
        }
        const browserCall = applyBrowserToolCall(currentRunBrowserActionsRef.current, event)
        if (browserCall.appended) {
          currentRunBrowserActionsRef.current = browserCall.nextActions
        }
        const subAgentCall = applySubAgentToolCall(currentRunSubAgentsRef.current, event)
        if (subAgentCall.appended) {
          currentRunSubAgentsRef.current = subAgentCall.nextAgents
          setTopLevelSubAgents((prev) => [...prev, subAgentCall.appended!])
        }
        const fileOpCall = applyFileOpToolCall(currentRunFileOpsRef.current, event)
        if (fileOpCall.appended) {
          currentRunFileOpsRef.current = fileOpCall.nextOps
          setTopLevelFileOps((prev) => [...prev, fileOpCall.appended!])
        }
        const webFetchCall = applyWebFetchToolCall(currentRunWebFetchesRef.current, event)
        if (webFetchCall.appended) {
          currentRunWebFetchesRef.current = webFetchCall.nextFetches
          setTopLevelWebFetches((prev) => [...prev, webFetchCall.appended!])
        }
        if (toolName === 'show_widget') {
          const args = obj.arguments as Record<string, unknown> | undefined
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          let entry = callId
            ? streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
            : undefined
          if (!entry) {
            entry = {
              toolCallIndex: streamingArtifactsRef.current.length,
              toolCallId: callId,
              toolName: 'show_widget',
              argumentsBuffer: '',
              complete: false,
            }
            streamingArtifactsRef.current = [...streamingArtifactsRef.current, entry]
          }
          entry.complete = true
          entry.toolName = 'show_widget'
          if (typeof args?.widget_code === 'string') entry.content = args.widget_code
          if (typeof args?.title === 'string') entry.title = args.title
          setStreamingArtifacts([...streamingArtifactsRef.current])
        }
        if (toolName === 'create_artifact') {
          const args = obj.arguments as Record<string, unknown> | undefined
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          let entry = callId
            ? streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
            : undefined
          if (!entry) {
            entry = {
              toolCallIndex: streamingArtifactsRef.current.length,
              toolCallId: callId,
              toolName: 'create_artifact',
              argumentsBuffer: '',
              complete: false,
            }
            streamingArtifactsRef.current = [...streamingArtifactsRef.current, entry]
          }
          entry.complete = true
          entry.toolName = 'create_artifact'
          if (typeof args?.content === 'string') entry.content = args.content
          if (typeof args?.title === 'string') entry.title = args.title
          if (typeof args?.filename === 'string') entry.filename = args.filename
          if (typeof args?.display === 'string') entry.display = args.display as 'inline' | 'panel'
          setStreamingArtifacts([...streamingArtifactsRef.current])
        }
        foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
        bumpAssistantTurnSnapshot()
        continue
      }

      if (event.type === 'terminal.stdout_delta' || event.type === 'terminal.stderr_delta') {
        const deltaPatch = applyTerminalDelta(currentRunCodeExecutionsRef.current, event)
        if (deltaPatch.updated) {
          currentRunCodeExecutionsRef.current = deltaPatch.nextExecutions
          setTopLevelCodeExecutions((prev) => patchCodeExecutionList(prev, deltaPatch.updated!).next)
          setSegments((prev) =>
            prev.map((segment) => ({
              ...segment,
              codeExecutions: patchCodeExecutionList(segment.codeExecutions, deltaPatch.updated!).next,
            })),
          )
        }
        continue
      }

      if (event.type === 'tool.result') {
        if (isACPDelegateEventData(event.data)) continue
        const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; result?: unknown; error?: unknown }
        const resultToolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
        if (isWebSearchToolName(resultToolName)) {
          const newSources = webSearchSourcesFromResult(obj.result)
          if (newSources && newSources.length > 0) {
            currentRunSourcesRef.current = [...currentRunSourcesRef.current, ...newSources]
          }
        }
        if (obj.tool_name === 'python_execute' || obj.tool_name === 'exec_command' || obj.tool_name === 'write_stdin' || obj.tool_name === 'document_write' || obj.tool_name === 'create_artifact' || obj.tool_name === 'browser' || isWebFetchToolName(resultToolName)) {
          const result = obj.result as { artifacts?: unknown[]; stdout?: unknown; stderr?: unknown; exit_code?: unknown; output?: unknown } | undefined
          if (Array.isArray(result?.artifacts)) {
            const newArtifacts: ArtifactRef[] = result.artifacts
              .filter((a): a is Record<string, unknown> => a != null && typeof a === 'object')
              .filter((a) => typeof a.key === 'string' && typeof a.filename === 'string')
              .map((a) => ({
                key: a.key as string,
                filename: a.filename as string,
                size: typeof a.size === 'number' ? a.size : 0,
                mime_type: typeof a.mime_type === 'string' ? a.mime_type : '',
                title: typeof a.title === 'string' ? a.title : undefined,
                display: a.display === 'inline' || a.display === 'panel' ? a.display as 'inline' | 'panel' : undefined,
              }))
            if (newArtifacts.length > 0) {
              currentRunArtifactsRef.current = [...currentRunArtifactsRef.current, ...newArtifacts]
              if (obj.tool_name === 'create_artifact') {
                const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
                for (const art of newArtifacts) {
                  const entry = callId
                    ? streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
                    : undefined
                  if (entry) {
                    entry.artifactRef = art
                  }
                }
                setStreamingArtifacts([...streamingArtifactsRef.current])
              }
            }
          }
          const codeExecutionResult = applyCodeExecutionToolResult(currentRunCodeExecutionsRef.current, event)
          if (codeExecutionResult.updated) {
            currentRunCodeExecutionsRef.current = codeExecutionResult.nextExecutions
            const target = codeExecutionResult.updated
            if (codeExecutionResult.appended) {
              setTopLevelCodeExecutions((prev) => [...prev, target])
            } else {
              setTopLevelCodeExecutions((prev) => patchCodeExecutionList(prev, target).next)
              setSegments((prev) =>
                prev.map((segment) => ({
                  ...segment,
                  codeExecutions: patchCodeExecutionList(segment.codeExecutions, target).next,
                })),
              )
            }
          }
        }
        if (obj.tool_name === 'browser') {
          const browserResult = applyBrowserToolResult(currentRunBrowserActionsRef.current, event)
          if (browserResult.updated) {
            currentRunBrowserActionsRef.current = browserResult.nextActions
          }
        }
        const subAgentResult = applySubAgentToolResult(currentRunSubAgentsRef.current, event)
        if (subAgentResult.updated) {
          currentRunSubAgentsRef.current = subAgentResult.nextAgents
          setTopLevelSubAgents((prev) => {
            const idx = prev.findIndex((a) => a.id === subAgentResult.updated!.id)
            if (idx >= 0) return prev.map((a, i) => i === idx ? subAgentResult.updated! : a)
            return [...prev, subAgentResult.updated!]
          })
        }
        const fileOpResult = applyFileOpToolResult(currentRunFileOpsRef.current, event)
        if (fileOpResult.updated) {
          currentRunFileOpsRef.current = fileOpResult.nextOps
          setTopLevelFileOps((prev) => {
            const idx = prev.findIndex((o) => o.id === fileOpResult.updated!.id)
            if (idx >= 0) return prev.map((o, i) => i === idx ? fileOpResult.updated! : o)
            return [...prev, fileOpResult.updated!]
          })
        }
        const webFetchResult = applyWebFetchToolResult(currentRunWebFetchesRef.current, event)
        if (webFetchResult.updated) {
          currentRunWebFetchesRef.current = webFetchResult.nextFetches
          setTopLevelWebFetches((prev) => {
            const idx = prev.findIndex((f) => f.id === webFetchResult.updated!.id)
            if (idx >= 0) return prev.map((f, i) => i === idx ? webFetchResult.updated! : f)
            return [...prev, webFetchResult.updated!]
          })
        }
        foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
        bumpAssistantTurnSnapshot()
        continue
      }

      if (event.type === 'thread.title.updated') {
        const obj = event.data as { thread_id?: unknown; title?: unknown }
        const tid = typeof obj.thread_id === 'string' ? obj.thread_id : threadId
        const title = typeof obj.title === 'string' ? obj.title : ''
        if (tid && title) onThreadTitleUpdated(tid, title)
        if (event.run_id && event.run_id === completedTitleTailRunId) {
          clearCompletedTitleTail()
        }
        continue
      }

      if (event.type === 'run.input_requested') {
        const data = event.data as Record<string, unknown> | undefined
        const message = data?.message as string | undefined
        const schema = data?.requestedSchema as RequestedSchema | undefined
        if (message && schema && schema.properties && Object.keys(schema.properties).length > 0) {
          const safeSchema: RequestedSchema = {
            ...schema,
            required: Array.isArray(schema.required) ? schema.required : undefined,
          }
          setPendingUserInput({
            request_id: (data?.request_id as string) ?? '',
            message,
            requestedSchema: safeSchema,
          })
        } else {
          setAwaitingInput(true)
        }
        continue
      }

      if (event.type === 'security.injection.blocked') {
        freezeCutoffRef.current = null
        injectionBlockedRunIdRef.current = event.run_id
        sseTerminalFallbackArmedRef.current = false
        sseTerminalFallbackRunIdRef.current = null
        restoreQueuedDraftToInput()
        sse.disconnect()
        setActiveRunId(null)
        setCancelSubmitting(false)
        setError(null)
        clearLiveRunSecurityArtifacts()
        setInjectionBlocked(getInjectionBlockMessage(event))
        if (threadId) onRunEnded(threadId)
        continue
      }

      if (event.type === 'run.completed') {
        if (isACPDelegateEventData(event.data)) continue
        const visibleNonTerminalSeqCutoff = freezeCutoffRef.current ?? lastVisibleNonTerminalSeqRef.current
        freezeCutoffRef.current = null
        const completedRunId = event.run_id
        injectionBlockedRunIdRef.current = null
        noResponseMsgIdRef.current = null
        replaceOnCancelRef.current = null
        setPreserveLiveRunUi(true)
        setTerminalRunDisplayId(completedRunId)
        setTerminalRunHandoffStatus('completed')
        const runEventsForMessage = (sse.events as MsgRunEvent[]).filter((e) => {
          if (e.run_id !== completedRunId || typeof e.seq !== 'number') {
            return false
          }
          if (isTerminalRunEventType(e.type)) {
            return e.seq <= event.seq
          }
          return e.seq <= visibleNonTerminalSeqCutoff
        })
        const runCache = captureTerminalRunCache('completed')
        if (runEventsForMessage.length > 0) {
          const frozenAssistantTurn = mergeVisibleSegmentsIntoAssistantTurn(
            buildFrozenAssistantTurnFromRunEvents(runEventsForMessage),
            segmentsRef.current,
          )
          if (frozenAssistantTurn.segments.length > 0) {
            runCache.handoffAssistantTurn = frozenAssistantTurn
            runCache.runAssistantTurn = frozenAssistantTurn
          }
        }
        setLiveAssistantTurn(runCache.handoffAssistantTurn.segments.length > 0 ? runCache.handoffAssistantTurn : null)
        armCompletedTitleTail(completedRunId)
        setActiveRunId(null)
        setCancelSubmitting(false)
        setPendingThinking(false)

        const runSearchSteps = finalizeSearchSteps(searchStepsRef.current)
        if (runSearchSteps.length > 0) {
          pendingSearchStepsRef.current = runSearchSteps
        }
        setQueuedDraft(null)
        setAwaitingInput(false)
        setPendingUserInput(null)
        setCheckInDraft('')
        if (threadId) onRunEnded(threadId)
        refreshCredits()
        void refreshMessages({ requiredCompletedRunId: completedRunId })
          .then((items) => {
            const completedAssistant = findAssistantMessageForRun(items, completedRunId)
            if (completedAssistant) {
              const pendingSteps = pendingSearchStepsRef.current
              pendingSearchStepsRef.current = null
              persistRunDataToMessage(completedAssistant.id, {
                ...runCache,
                pendingSearchSteps: pendingSteps,
              }, runEventsForMessage)
              markTerminalRunHistory(completedAssistant.id, false)
              releaseCompletedHandoffToHistory()
            }
            const pending = pendingMessageRef.current
            if (pending) {
              pendingMessageRef.current = null
              sendMessageRef.current?.(pending)
            }
          })
          .catch((err) => console.error('persist run data failed', err))
        continue
      }

      if (event.type === 'run.cancelled') {
        if (isACPDelegateEventData(event.data)) continue
        const blockedByInjection = injectionBlockedRunIdRef.current === event.run_id
        const runId = event.run_id
        setTerminalRunDisplayId(runId)
        setTerminalRunHandoffStatus('cancelled')
        const visibleNonTerminalSeqCutoff = freezeCutoffRef.current ?? lastVisibleNonTerminalSeqRef.current
        const runEventsForMessage = runId
          ? (sse.events as MsgRunEvent[]).filter((e) => {
            if (e.run_id !== runId || typeof e.seq !== 'number') {
              return false
            }
            if (isTerminalRunEventType(e.type)) {
              return e.seq <= event.seq
            }
            return e.seq <= visibleNonTerminalSeqCutoff
          })
          : []
        const runCache = captureTerminalRunCache('cancelled')
        if (runCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
          runCache.handoffAssistantTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          runCache.runAssistantTurn = runCache.handoffAssistantTurn
        }
        if (runId) {
          persistThreadRunHandoff(runId, runCache)
        }
        resetTerminalRunState({
          restoreQueuedDraft: true,
          preserveSearchSteps: true,
          handoffRunCache: runCache,
        })
        if (!blockedByInjection) {
          setError(null)
        }
        if (runId) {
          void refreshMessages({ requiredCompletedRunId: runId })
            .then((items) => {
              const assistant = findAssistantMessageForRun(items, runId)
              if (assistant) {
                persistRunDataToMessage(assistant.id, runCache, runEventsForMessage)
                markTerminalRunHistory(assistant.id, false)
              }
            })
            .catch((err) => console.error('persist run data failed', err))
        }
        continue
      }

      if (event.type === 'run.failed') {
        if (isACPDelegateEventData(event.data)) continue
        const runId = event.run_id
        setTerminalRunDisplayId(runId)
        setTerminalRunHandoffStatus('failed')
        const visibleNonTerminalSeqCutoff = freezeCutoffRef.current ?? lastVisibleNonTerminalSeqRef.current
        const runEventsForMessage = runId
          ? (sse.events as MsgRunEvent[]).filter((e) => {
            if (e.run_id !== runId || typeof e.seq !== 'number') {
              return false
            }
            if (isTerminalRunEventType(e.type)) {
              return e.seq <= event.seq
            }
            return e.seq <= visibleNonTerminalSeqCutoff
          })
          : []
        const runCache = captureTerminalRunCache('failed')
        if (runCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
          runCache.handoffAssistantTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          runCache.runAssistantTurn = runCache.handoffAssistantTurn
        }
        if (runId) {
          persistThreadRunHandoff(runId, runCache)
        }
        resetTerminalRunState({
          restoreQueuedDraft: true,
          preserveSearchSteps: true,
          handoffRunCache: runCache,
        })
        const obj = event.data as { message?: unknown; error_class?: unknown; code?: unknown; details?: unknown }
        const errorClass = typeof obj?.error_class === 'string' ? obj.error_class : undefined
        const details = (obj?.details && typeof obj.details === 'object' && !Array.isArray(obj.details))
          ? obj.details as Record<string, unknown>
          : undefined

        if (errorClass === 'security.injection_blocked') {
          setInjectionBlocked(typeof obj?.message === 'string' ? obj.message : 'blocked')
        } else {
          setError({
            message: typeof obj?.message === 'string' ? obj.message : '运行失败',
            code: typeof obj?.code === 'string' ? obj.code : errorClass,
            details,
          })
        }
        if (runId) {
          void refreshMessages({ requiredCompletedRunId: runId })
            .then((items) => {
              const assistant = findAssistantMessageForRun(items, runId)
              if (assistant) {
                persistRunDataToMessage(assistant.id, runCache, runEventsForMessage)
              }
            })
            .catch((err) => console.error('persist run data failed', err))
        }
        continue
      }

      if (event.type === 'run.interrupted') {
        if (isACPDelegateEventData(event.data)) continue
        const runId = event.run_id
        setTerminalRunDisplayId(runId)
        setTerminalRunHandoffStatus('interrupted')
        const visibleNonTerminalSeqCutoff = freezeCutoffRef.current ?? lastVisibleNonTerminalSeqRef.current
        const runEventsForMessage = runId
          ? (sse.events as MsgRunEvent[]).filter((e) => {
            if (e.run_id !== runId || typeof e.seq !== 'number') {
              return false
            }
            if (isTerminalRunEventType(e.type)) {
              return e.seq <= event.seq
            }
            return e.seq <= visibleNonTerminalSeqCutoff
          })
          : []
        const runCache = captureTerminalRunCache('interrupted')
        if (runCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
          runCache.handoffAssistantTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          runCache.runAssistantTurn = runCache.handoffAssistantTurn
        }
        if (runId) {
          persistThreadRunHandoff(runId, runCache)
        }
        resetTerminalRunState({
          restoreQueuedDraft: true,
          preserveSearchSteps: true,
          handoffRunCache: runCache,
        })
        const obj = event.data as { message?: unknown; error_class?: unknown; code?: unknown; details?: unknown }
        const errorClass = typeof obj?.error_class === 'string' ? obj.error_class : undefined
        const details = (obj?.details && typeof obj.details === 'object' && !Array.isArray(obj.details))
          ? obj.details as Record<string, unknown>
          : undefined

        setError({
          message: typeof obj?.message === 'string' ? obj.message : '运行中断',
          code: typeof obj?.code === 'string' ? obj.code : errorClass,
          details,
        })
        if (event.run_id) {
          void refreshMessages({ requiredCompletedRunId: runId! })
            .then((items) => {
              const assistant = findAssistantMessageForRun(items, runId!)
              if (assistant) {
                persistRunDataToMessage(assistant.id, runCache, runEventsForMessage)
              }
            })
            .catch((err) => console.error('persist run data failed', err))
        }
        continue
      }
    }
  }, [sseRunId, activeRunId, armCompletedTitleTail, clearCompletedTitleTail, clearContextCompactHideTimer, clearLiveRunSecurityArtifacts, clearQueuedDraft, completedTitleTailRunId, persistThreadRunHandoff, refreshMessages, refreshCredits, resetSearchSteps, restoreQueuedDraftToInput, sse.events]) // eslint-disable-line react-hooks/exhaustive-deps

  // 401 SSE 错误时登出
  useEffect(() => {
    if (sse.error instanceof SSEApiError && sse.error.status === 401) {
      onLoggedOut()
    }
  }, [sse.error, onLoggedOut])

  // SSE fallback 清理
  useEffect(() => {
    if (!activeRunId) return
    if (sse.state !== 'closed' && sse.state !== 'error') return
    if (!sseTerminalFallbackArmedRef.current) return
    if (sseTerminalFallbackRunIdRef.current !== activeRunId) return
    const terminalRunId = activeRunId

    sseTerminalFallbackArmedRef.current = false
    sseTerminalFallbackRunIdRef.current = null
    const visibleNonTerminalSeqCutoff = freezeCutoffRef.current ?? lastVisibleNonTerminalSeqRef.current
    const runEventsForMessage = (sse.events as MsgRunEvent[]).filter((e) =>
      e.run_id === terminalRunId &&
      typeof e.seq === 'number' &&
      e.seq <= visibleNonTerminalSeqCutoff,
    )
    const terminalCache = captureTerminalRunCache()
    if (terminalCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
      terminalCache.handoffAssistantTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
      terminalCache.runAssistantTurn = terminalCache.handoffAssistantTurn
    }
    setTerminalRunDisplayId(terminalRunId)
    setPreserveLiveRunUi(true)
    setTerminalRunHandoffStatus('interrupted')
    setLiveAssistantTurn(terminalCache.handoffAssistantTurn.segments.length > 0 ? terminalCache.handoffAssistantTurn : null)
    persistThreadRunHandoff(terminalRunId, {
      ...terminalCache,
      terminalStatus: 'interrupted',
    })

    setActiveRunId(null)
    setPendingThinking(false)
    setQueuedDraft(null)
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
    if (threadId) onRunEnded(threadId)
    refreshCredits()

    void refreshMessages({ requiredCompletedRunId: terminalRunId })
      .then((items) => {
        const completedAssistant = findAssistantMessageForRun(items, terminalRunId)
        if (completedAssistant) {
          persistRunDataToMessage(completedAssistant.id, terminalCache, runEventsForMessage)
        }
      })
      .catch((err) => console.error('persist run data failed', err))
  }, [activeRunId, sse.state, persistRunDataToMessage, persistThreadRunHandoff]) // eslint-disable-line react-hooks/exhaustive-deps
}
