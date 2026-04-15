import { useEffect, useRef } from 'react'
import { canonicalToolName, pickLogicalToolName } from '@arkloop/shared'
import { useAuth } from '../contexts/auth'
import { useChatSession } from '../contexts/chat-session'
import { useCredits } from '../contexts/credits'
import { useMessageMeta } from '../contexts/message-meta'
import { useMessageStore } from '../contexts/message-store'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useStream } from '../contexts/stream'
import { useThreadList } from '../contexts/thread-list'
import { SSEApiError } from '../sse'
import type { MsgRunEvent } from '../storage'
import {
  applyBrowserToolCall,
  applyBrowserToolResult,
  applyCodeExecutionToolCall,
  applyCodeExecutionToolResult,
  applyFileOpToolCall,
  applyFileOpToolResult,
  applySubAgentToolCall,
  applySubAgentToolResult,
  applyTerminalDelta,
  applyWebFetchToolCall,
  applyWebFetchToolResult,
  findAssistantMessageForRun,
  patchCodeExecutionList,
  selectFreshRunEvents,
} from '../runEventProcessing'
import {
  createEmptyAssistantTurnFoldState,
  snapshotAssistantTurn,
} from '../assistantTurnSegments'
import { applyRunEventToWebSearchSteps, webSearchSourcesFromResult, isWebSearchToolName } from '../webSearchTimelineFromRunEvent'
import {
  buildFrozenAssistantTurnFromRunEvents,
  collectCompletedWidgets,
  finalizeSearchSteps,
  isTerminalRunEventType,
} from '../lib/chat-helpers'
import { extractPartialArtifactFields, extractPartialWidgetFields } from '../components/ArtifactStreamBlock'
import type { ArtifactRef } from '../storage'
import { getInjectionBlockMessage, shouldSuppressLiveRunEventAfterInjectionBlock } from '../liveRunSecurity'
import { isACPDelegateEventData } from '@arkloop/shared'
import { isWebFetchToolName } from '../runEventProcessing'
import type { UserInputRequest, RequestedSchema } from '../userInputTypes'
import { useLatest } from './useLatest'

export function useSseDispatch(params: {
  restoreQueuedDraftRef: React.RefObject<(() => void) | null>
}): void {
  const { restoreQueuedDraftRef } = params

  const { logout } = useAuth()
  const session = useChatSession()
  const run = useRunLifecycle()
  const stream = useStream()
  const meta = useMessageMeta()
  const msgs = useMessageStore()
  const threadList = useThreadList()
  const credits = useCredits()

  // Stable refs to avoid stale closures in effects
  const threadIdRef = useLatest(session.threadId)
  const completedTitleTailRunIdRef = useLatest(run.completedTitleTailRunId)
  const refreshMessagesRef = useLatest(msgs.refreshMessages)
  const refreshCreditsRef = useLatest(credits.refreshCredits)
  const markIdleRef = useLatest(threadList.markIdle)
  const updateTitleRef = useLatest(threadList.updateTitle)

  // Timer for contextCompactBar auto-hide
  const contextCompactHideTimerRef = useRef<number | null>(null)

  const clearContextCompactHideTimer = () => {
    if (contextCompactHideTimerRef.current != null) {
      clearTimeout(contextCompactHideTimerRef.current)
      contextCompactHideTimerRef.current = null
    }
  }

  // ── helpers ─────────────────────────────────���──────────────────────────────

  const captureTerminalRunCache = (terminalStatus?: Parameters<typeof meta.persistRunDataToMessage>[1]['terminalStatus']) => {
    const assistantTurn = snapshotAssistantTurn(stream.assistantTurnFoldStateRef.current)
    return {
      runSources: [...meta.currentRunSourcesRef.current],
      runArtifacts: [...meta.currentRunArtifactsRef.current],
      runWidgets: collectCompletedWidgets(stream.streamingArtifactsRef.current),
      runCodeExecs: [...meta.currentRunCodeExecutionsRef.current],
      runBrowserActions: [...meta.currentRunBrowserActionsRef.current],
      runSubAgents: [...meta.currentRunSubAgentsRef.current],
      runFileOps: [...meta.currentRunFileOpsRef.current],
      runWebFetches: [...meta.currentRunWebFetchesRef.current],
      runAssistantTurn: assistantTurn,
      handoffAssistantTurn: assistantTurn,
      terminalStatus: terminalStatus ?? null,
    }
  }

  const resetTerminalRunState = (options?: {
    restoreQueuedDraft?: boolean
    preserveSearchSteps?: boolean
    handoffRunCache?: ReturnType<typeof captureTerminalRunCache>
  }) => {
    run.freezeCutoffRef.current = null
    run.injectionBlockedRunIdRef.current = null
    run.clearCompletedTitleTail()
    run.sse.disconnect()
    run.setActiveRunId(null)
    run.setCancelSubmitting(false)
    stream.setPendingThinking(false)

    const handoffRunCache = options?.handoffRunCache
    if (handoffRunCache) {
      stream.setPreserveLiveRunUi(true)
      stream.setLiveAssistantTurn(
        handoffRunCache.handoffAssistantTurn.segments.length > 0
          ? handoffRunCache.handoffAssistantTurn
          : null,
      )
    } else {
      stream.setPreserveLiveRunUi(false)
      stream.setLiveAssistantTurn(null)
      stream.setTopLevelCodeExecutions([])
      stream.setTopLevelSubAgents([])
      stream.setTopLevelFileOps([])
      stream.setTopLevelWebFetches([])
    }

    if (options?.restoreQueuedDraft) {
      restoreQueuedDraftRef.current?.()
    } else {
      run.pendingMessageRef.current = null
      run.setQueuedDraft(null)
    }

    if (!handoffRunCache) {
      stream.streamingArtifactsRef.current = []
      stream.setStreamingArtifacts([])
      stream.setSegments([])
      stream.segmentsRef.current = []
      stream.assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
      stream.activeSegmentIdRef.current = null
      meta.currentRunSourcesRef.current = []
      meta.currentRunArtifactsRef.current = []
      meta.currentRunCodeExecutionsRef.current = []
      meta.currentRunBrowserActionsRef.current = []
      meta.currentRunSubAgentsRef.current = []
      meta.currentRunFileOpsRef.current = []
      meta.currentRunWebFetchesRef.current = []
    }

    if (!options?.preserveSearchSteps) {
      stream.resetSearchSteps()
    }
    meta.pendingSearchStepsRef.current = null
    run.setAwaitingInput(false)
    run.setPendingUserInput(null)
    run.setCheckInDraft('')

    const threadId = threadIdRef.current
    if (threadId) markIdleRef.current(threadId)
  }

  // ── main SSE event dispatch ────────────────────────────────────────────────

  useEffect(() => {
    const sseRunId = run.sseRunId
    if (!sseRunId) return

    const { fresh, nextProcessedCount } = selectFreshRunEvents({
      events: run.sse.events,
      activeRunId: sseRunId,
      processedCount: run.processedEventCountRef.current,
    })
    run.processedEventCountRef.current = nextProcessedCount

    let needsBumpSnapshot = false

    for (const event of fresh) {
      const freezeCutoff = run.freezeCutoffRef.current
      if (
        freezeCutoff != null &&
        typeof event.seq === 'number' &&
        event.seq > freezeCutoff &&
        !isTerminalRunEventType(event.type)
      ) {
        continue
      }
      if (shouldSuppressLiveRunEventAfterInjectionBlock({
        activeRunId: run.activeRunId,
        blockedRunId: run.injectionBlockedRunIdRef.current,
        event,
      })) {
        continue
      }

      // Update web search steps
      const nextSearchSteps = applyRunEventToWebSearchSteps(stream.searchStepsRef.current, event)
      if (nextSearchSteps !== stream.searchStepsRef.current) {
        stream.searchStepsRef.current = nextSearchSteps
        stream.setSearchSteps(nextSearchSteps)
      }

      // ── run.segment.start ─────────────────────────────────────────────────
      if (event.type === 'run.segment.start') {
        const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        const kind = typeof obj.kind === 'string' ? obj.kind : 'planning_round'
        const display = (obj.display ?? {}) as { mode?: unknown; label?: unknown }
        const mode = typeof display.mode === 'string' ? display.mode : 'collapsed'
        const label = typeof display.label === 'string' ? display.label : ''
        if (!segmentId) continue
        stream.activeSegmentIdRef.current = segmentId
        stream.requestAssistantTurnThinkingBreak()
        if (kind.startsWith('search_')) continue
        stream.setSegments((prev) => [...prev, { segmentId, kind, mode, label, content: '', isStreaming: true, codeExecutions: [] }])
        continue
      }

      // ── run.segment.end ───────────────────────────────────────────────────
      if (event.type === 'run.segment.end') {
        const obj = event.data as { segment_id?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        if (segmentId && stream.activeSegmentIdRef.current === segmentId) {
          stream.activeSegmentIdRef.current = null
        }
        stream.requestAssistantTurnThinkingBreak()
        stream.setSegments((prev) =>
          prev.map((s) => (s.segmentId === segmentId ? { ...s, isStreaming: false } : s)),
        )
        continue
      }

      // ── run.context_compact ───────────────────────────────────────────────
      if (event.type === 'run.context_compact') {
        const obj = event.data as { phase?: unknown; op?: unknown; dropped_prefix?: unknown }
        const op = typeof obj.op === 'string' ? obj.op : undefined
        const phase = typeof obj.phase === 'string' ? obj.phase : undefined

        if (op === 'persist') {
          if (phase === 'started') {
            clearContextCompactHideTimer()
            run.setContextCompactBar({ type: 'persist', status: 'running' })
          } else if (phase === 'completed' || phase === undefined) {
            clearContextCompactHideTimer()
            run.setContextCompactBar({ type: 'persist', status: 'done' })
            contextCompactHideTimerRef.current = window.setTimeout(() => {
              run.setContextCompactBar(null)
              contextCompactHideTimerRef.current = null
            }, 2800)
          } else if (phase === 'llm_failed') {
            clearContextCompactHideTimer()
            run.setContextCompactBar({ type: 'persist', status: 'llm_failed' })
            contextCompactHideTimerRef.current = window.setTimeout(() => {
              run.setContextCompactBar(null)
              contextCompactHideTimerRef.current = null
            }, 4000)
          }
        } else if (op === 'trim') {
          if (phase === 'completed') {
            const dropped = typeof obj.dropped_prefix === 'number' ? obj.dropped_prefix : 0
            if (dropped > 0) {
              clearContextCompactHideTimer()
              run.setContextCompactBar({ type: 'trim', status: 'done', dropped })
              contextCompactHideTimerRef.current = window.setTimeout(() => {
                run.setContextCompactBar(null)
                contextCompactHideTimerRef.current = null
              }, 1500)
            }
          }
        }
        continue
      }

      // ── todo.updated ──────────────────────────────────────────────────────
      if (event.type === 'todo.updated') {
        const obj = event.data as { todos?: unknown }
        if (Array.isArray(obj.todos)) {
          const items = (obj.todos as unknown[]).flatMap((t) => {
            if (!t || typeof t !== 'object') return []
            const item = t as { id?: unknown; content?: unknown; status?: unknown }
            if (typeof item.id !== 'string' || typeof item.content !== 'string' || typeof item.status !== 'string') return []
            return [{ id: item.id, content: item.content, status: item.status }]
          })
          stream.setWorkTodos(items)
        }
        continue
      }

      // ── message.delta ─────────────────────────────────────────────────────
      if (event.type === 'message.delta') {
        if (isACPDelegateEventData(event.data)) continue
        run.noResponseMsgIdRef.current = null
        const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
        if (obj.role != null && obj.role !== 'assistant') continue
        if (typeof obj.content_delta !== 'string' || !obj.content_delta) continue
        const delta = obj.content_delta
        const channel = typeof obj.channel === 'string' ? obj.channel : ''
        const isThinking = channel === 'thinking'
        const eventSeq = typeof event.seq === 'number' ? event.seq : 0
        if (!isThinking && channel.trim() === '') {
          if (eventSeq > run.lastVisibleNonTerminalSeqRef.current) {
            run.lastVisibleNonTerminalSeqRef.current = eventSeq
          }
        }
        const activeSeg = stream.activeSegmentIdRef.current
        if (isThinking) {
          stream.setPendingThinking(false)
          stream.foldAssistantTurnEvent(event)
          needsBumpSnapshot = true
          continue
        }
        stream.setPendingThinking(false)
        if (activeSeg) {
          const activeSegment = stream.segmentsRef.current.find((segment) => segment.segmentId === activeSeg)
          const activeSegmentVisible = !!activeSegment && activeSegment.mode !== 'hidden'
          stream.requestAssistantTurnThinkingBreak()
          stream.setSegments((prev) =>
            prev.map((s) =>
              s.segmentId === activeSeg && s.mode !== 'hidden'
                ? { ...s, content: s.content + delta }
                : s,
            ),
          )
          if (activeSegmentVisible) {
            stream.foldAssistantTurnEvent(event)
            needsBumpSnapshot = true
          }
          continue
        }
        stream.foldAssistantTurnEvent(event)
        needsBumpSnapshot = true
        continue
      }

      // ── tool.call.delta ───────────────────────────────────────────────────
      if (event.type === 'tool.call.delta') {
        const obj = event.data as { tool_call_index?: number; tool_call_id?: string; tool_name?: string; arguments_delta?: string }
        const idx = typeof obj.tool_call_index === 'number' ? obj.tool_call_index : -1
        if (idx >= 0 && typeof obj.arguments_delta === 'string') {
          let entry = stream.streamingArtifactsRef.current.find((e) => e.toolCallIndex === idx)
          if (!entry) {
            entry = { toolCallIndex: idx, argumentsBuffer: '', complete: false }
            stream.streamingArtifactsRef.current = [...stream.streamingArtifactsRef.current, entry]
          }
          if (obj.tool_call_id) entry.toolCallId = obj.tool_call_id
          if (obj.tool_name) entry.toolName = canonicalToolName(obj.tool_name)
          entry.argumentsBuffer += obj.arguments_delta

          if (entry.toolName === 'show_widget' || (!entry.toolName && entry.argumentsBuffer.includes('"widget_code"'))) {
            const parsed = extractPartialWidgetFields(entry.argumentsBuffer)
            if (parsed.title !== undefined) entry.title = parsed.title
            if (parsed.widgetCode !== undefined) entry.content = parsed.widgetCode
            if (parsed.loadingMessages !== undefined) entry.loadingMessages = parsed.loadingMessages
            stream.setStreamingArtifacts([...stream.streamingArtifactsRef.current])
          } else if (entry.toolName === 'create_artifact' || (!entry.toolName && entry.argumentsBuffer.includes('"content"'))) {
            const parsed = extractPartialArtifactFields(entry.argumentsBuffer)
            if (parsed.title !== undefined) entry.title = parsed.title
            if (parsed.filename !== undefined) entry.filename = parsed.filename
            if (parsed.display !== undefined) entry.display = parsed.display as 'inline' | 'panel'
            if (parsed.content !== undefined) entry.content = parsed.content
            if (parsed.loadingMessages !== undefined) entry.loadingMessages = parsed.loadingMessages
            stream.setStreamingArtifacts([...stream.streamingArtifactsRef.current])
          }
        }
        continue
      }

      // ── tool.call ─────────────────────────────────────────────────────────
      if (event.type === 'tool.call') {
        if (isACPDelegateEventData(event.data)) continue
        stream.setPendingThinking(false)
        run.seenFirstToolCallInRunRef.current = true
        const obj = event.data as { tool_call_id?: unknown; arguments?: unknown }
        const toolName = pickLogicalToolName(event.data, event.tool_name)

        const codeExecutionCall = applyCodeExecutionToolCall(meta.currentRunCodeExecutionsRef.current, event)
        if (codeExecutionCall.appended) {
          meta.currentRunCodeExecutionsRef.current = codeExecutionCall.nextExecutions
          const activeSeg = stream.activeSegmentIdRef.current
          if (activeSeg) {
            stream.setSegments((prev) =>
              prev.map((s) =>
                s.segmentId === activeSeg
                  ? { ...s, codeExecutions: [...s.codeExecutions, codeExecutionCall.appended!] }
                  : s,
              ),
            )
          }
          stream.addTopLevelCodeExecution(codeExecutionCall.appended)
        }

        const browserCall = applyBrowserToolCall(meta.currentRunBrowserActionsRef.current, event)
        if (browserCall.appended) {
          meta.currentRunBrowserActionsRef.current = browserCall.nextActions
        }

        const subAgentCall = applySubAgentToolCall(meta.currentRunSubAgentsRef.current, event)
        if (subAgentCall.appended) {
          meta.currentRunSubAgentsRef.current = subAgentCall.nextAgents
          stream.addTopLevelSubAgent(subAgentCall.appended)
        }

        const fileOpCall = applyFileOpToolCall(meta.currentRunFileOpsRef.current, event)
        if (fileOpCall.appended) {
          meta.currentRunFileOpsRef.current = fileOpCall.nextOps
          stream.addTopLevelFileOp(fileOpCall.appended)
        }

        const webFetchCall = applyWebFetchToolCall(meta.currentRunWebFetchesRef.current, event)
        if (webFetchCall.appended) {
          meta.currentRunWebFetchesRef.current = webFetchCall.nextFetches
          stream.addTopLevelWebFetch(webFetchCall.appended)
        }

        if (toolName === 'show_widget') {
          const args = obj.arguments as Record<string, unknown> | undefined
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          let entry = callId
            ? stream.streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
            : undefined
          if (!entry) {
            entry = { toolCallIndex: stream.streamingArtifactsRef.current.length, toolCallId: callId, toolName: 'show_widget', argumentsBuffer: '', complete: false }
            stream.streamingArtifactsRef.current = [...stream.streamingArtifactsRef.current, entry]
          }
          entry.complete = true
          entry.toolName = 'show_widget'
          if (typeof args?.widget_code === 'string') entry.content = args.widget_code
          if (typeof args?.title === 'string') entry.title = args.title
          if (Array.isArray(args?.loading_messages)) {
            const messages = (args?.loading_messages as unknown[])
              .filter((x): x is string => typeof x === 'string')
              .map((x) => x.trim())
              .filter((x) => x.length > 0)
            if (messages.length > 0) entry.loadingMessages = messages
          }
          stream.setStreamingArtifacts([...stream.streamingArtifactsRef.current])
        }

        if (toolName === 'create_artifact') {
          const args = obj.arguments as Record<string, unknown> | undefined
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          let entry = callId
            ? stream.streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
            : undefined
          if (!entry) {
            entry = { toolCallIndex: stream.streamingArtifactsRef.current.length, toolCallId: callId, toolName: 'create_artifact', argumentsBuffer: '', complete: false }
            stream.streamingArtifactsRef.current = [...stream.streamingArtifactsRef.current, entry]
          }
          entry.complete = true
          entry.toolName = 'create_artifact'
          if (typeof args?.content === 'string') entry.content = args.content
          if (typeof args?.title === 'string') entry.title = args.title
          if (typeof args?.filename === 'string') entry.filename = args.filename
          if (typeof args?.display === 'string') entry.display = args.display as 'inline' | 'panel'
          if (Array.isArray(args?.loading_messages)) {
            const messages = (args?.loading_messages as unknown[])
              .filter((x): x is string => typeof x === 'string')
              .map((x) => x.trim())
              .filter((x) => x.length > 0)
            if (messages.length > 0) entry.loadingMessages = messages
          }
          stream.setStreamingArtifacts([...stream.streamingArtifactsRef.current])
        }

        stream.foldAssistantTurnEvent(event)
        needsBumpSnapshot = true
        continue
      }

      // ── terminal.stdout_delta / terminal.stderr_delta ─────────────────────
      if (event.type === 'terminal.stdout_delta' || event.type === 'terminal.stderr_delta') {
        const deltaPatch = applyTerminalDelta(meta.currentRunCodeExecutionsRef.current, event)
        if (deltaPatch.updated) {
          meta.currentRunCodeExecutionsRef.current = deltaPatch.nextExecutions
          stream.setTopLevelCodeExecutions((prev) => patchCodeExecutionList(prev, deltaPatch.updated!).next)
          stream.setSegments((prev) =>
            prev.map((segment) => ({
              ...segment,
              codeExecutions: patchCodeExecutionList(segment.codeExecutions, deltaPatch.updated!).next,
            })),
          )
        }
        continue
      }

      // ── tool.result ───────────────────────────────────────────────────────
      if (event.type === 'tool.result') {
        if (isACPDelegateEventData(event.data)) continue
        const obj = event.data as { tool_call_id?: unknown; result?: unknown }
        const resultToolName = pickLogicalToolName(event.data, event.tool_name)

        if (isWebSearchToolName(resultToolName)) {
          const newSources = webSearchSourcesFromResult(obj.result)
          if (newSources && newSources.length > 0) {
            meta.currentRunSourcesRef.current = [...meta.currentRunSourcesRef.current, ...newSources]
          }
        }

        if (resultToolName === 'python_execute' || resultToolName === 'exec_command' || resultToolName === 'continue_process' || resultToolName === 'terminate_process' || resultToolName === 'document_write' || resultToolName === 'create_artifact' || resultToolName === 'browser' || isWebFetchToolName(resultToolName)) {
          const result = obj.result as { artifacts?: unknown[] } | undefined
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
              meta.currentRunArtifactsRef.current = [...meta.currentRunArtifactsRef.current, ...newArtifacts]
              if (resultToolName === 'create_artifact') {
                const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
                for (const art of newArtifacts) {
                  const entry = callId
                    ? stream.streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
                    : undefined
                  if (entry) entry.artifactRef = art
                }
                stream.setStreamingArtifacts([...stream.streamingArtifactsRef.current])
              }
            }
          }

          const codeExecResult = applyCodeExecutionToolResult(meta.currentRunCodeExecutionsRef.current, event)
          if (codeExecResult.updated) {
            meta.currentRunCodeExecutionsRef.current = codeExecResult.nextExecutions
            const target = codeExecResult.updated
            if (codeExecResult.appended) {
              stream.addTopLevelCodeExecution(target)
            } else {
              stream.setTopLevelCodeExecutions((prev) => patchCodeExecutionList(prev, target).next)
              stream.setSegments((prev) =>
                prev.map((s) => ({ ...s, codeExecutions: patchCodeExecutionList(s.codeExecutions, target).next })),
              )
            }
          }
        }

        if (resultToolName === 'browser') {
          const browserResult = applyBrowserToolResult(meta.currentRunBrowserActionsRef.current, event)
          if (browserResult.updated) meta.currentRunBrowserActionsRef.current = browserResult.nextActions
        }

        const subAgentResult = applySubAgentToolResult(meta.currentRunSubAgentsRef.current, event)
        if (subAgentResult.updated) {
          meta.currentRunSubAgentsRef.current = subAgentResult.nextAgents
          stream.setTopLevelSubAgents((prev) => {
            const idx = prev.findIndex((a) => a.id === subAgentResult.updated!.id)
            if (idx >= 0) return prev.map((a, i) => i === idx ? subAgentResult.updated! : a)
            return [...prev, subAgentResult.updated!]
          })
        }

        const fileOpResult = applyFileOpToolResult(meta.currentRunFileOpsRef.current, event)
        if (fileOpResult.updated) {
          meta.currentRunFileOpsRef.current = fileOpResult.nextOps
          stream.setTopLevelFileOps((prev) => {
            const idx = prev.findIndex((o) => o.id === fileOpResult.updated!.id)
            if (idx >= 0) return prev.map((o, i) => i === idx ? fileOpResult.updated! : o)
            return [...prev, fileOpResult.updated!]
          })
        }

        const webFetchResult = applyWebFetchToolResult(meta.currentRunWebFetchesRef.current, event)
        if (webFetchResult.updated) {
          meta.currentRunWebFetchesRef.current = webFetchResult.nextFetches
          stream.setTopLevelWebFetches((prev) => {
            const idx = prev.findIndex((f) => f.id === webFetchResult.updated!.id)
            if (idx >= 0) return prev.map((f, i) => i === idx ? webFetchResult.updated! : f)
            return [...prev, webFetchResult.updated!]
          })
        }

        stream.foldAssistantTurnEvent(event)
        needsBumpSnapshot = true
        continue
      }

      // ── thread.title.updated ──────────────────────────────────────────────
      if (event.type === 'thread.title.updated') {
        const obj = event.data as { thread_id?: unknown; title?: unknown }
        const tid = typeof obj.thread_id === 'string' ? obj.thread_id : session.threadId
        const title = typeof obj.title === 'string' ? obj.title : ''
        if (tid && title) updateTitleRef.current(tid, title)
        if (event.run_id && event.run_id === completedTitleTailRunIdRef.current) {
          run.clearCompletedTitleTail()
        }
        continue
      }

      // ── run.input_requested ───────────────────────────────────────────────
      if (event.type === 'run.input_requested') {
        const data = event.data as Record<string, unknown> | undefined
        const message = data?.message as string | undefined
        const schema = data?.requestedSchema as RequestedSchema | undefined
        if (message && schema && schema.properties && Object.keys(schema.properties).length > 0) {
          const safeSchema: RequestedSchema = {
            ...schema,
            required: Array.isArray(schema.required) ? schema.required : undefined,
          }
          run.setPendingUserInput({
            request_id: (data?.request_id as string) ?? '',
            message,
            requestedSchema: safeSchema,
          } as UserInputRequest)
        } else {
          run.setAwaitingInput(true)
        }
        continue
      }

      // ── security.injection.blocked ────────────────────────────────────────
      if (event.type === 'security.injection.blocked') {
        run.freezeCutoffRef.current = null
        run.injectionBlockedRunIdRef.current = event.run_id
        run.sseTerminalFallbackArmedRef.current = false
        run.sseTerminalFallbackRunIdRef.current = null
        restoreQueuedDraftRef.current?.()
        run.sse.disconnect()
        run.setActiveRunId(null)
        run.setCancelSubmitting(false)
        run.setError(null)
        // Clear live run transient state
        stream.streamingArtifactsRef.current = []
        stream.setStreamingArtifacts([])
        meta.currentRunSourcesRef.current = []
        meta.currentRunArtifactsRef.current = []
        meta.currentRunCodeExecutionsRef.current = []
        meta.currentRunBrowserActionsRef.current = []
        meta.clearRunRefs()
        stream.resetLiveState()
        run.seenFirstToolCallInRunRef.current = false
        stream.resetSearchSteps()
        meta.pendingSearchStepsRef.current = null
        run.setAwaitingInput(false)
        run.setPendingUserInput(null)
        run.setCheckInDraft('')
        run.setInjectionBlocked(getInjectionBlockMessage(event))
        const tid = session.threadId
        if (tid) markIdleRef.current(tid)
        continue
      }

      // ── run.completed ─────────────────────────────────────────────────────
      if (event.type === 'run.completed') {
        if (isACPDelegateEventData(event.data)) continue
        run.freezeCutoffRef.current = null
        const completedRunId = event.run_id
        run.injectionBlockedRunIdRef.current = null
        run.noResponseMsgIdRef.current = null
        run.replaceOnCancelRef.current = null
        stream.setPreserveLiveRunUi(true)
        run.setTerminalRunDisplayId(completedRunId)
        run.setTerminalRunHandoffStatus('completed')

        const runEventsForMessage = (run.sse.events as MsgRunEvent[]).filter((e) => {
          if (e.run_id !== completedRunId || typeof e.seq !== 'number') return false
          return e.seq <= event.seq
        })
        const runCache = captureTerminalRunCache('completed')
        if (runEventsForMessage.length > 0) {
          const frozenTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          if (frozenTurn.segments.length > 0) {
            runCache.handoffAssistantTurn = frozenTurn
            runCache.runAssistantTurn = frozenTurn
          }
        }
        stream.setLiveAssistantTurn(runCache.handoffAssistantTurn.segments.length > 0 ? runCache.handoffAssistantTurn : null)
        run.armCompletedTitleTail(completedRunId)
        run.setActiveRunId(null)
        run.setCancelSubmitting(false)
        stream.setPendingThinking(false)

        const runSearchSteps = finalizeSearchSteps(stream.searchStepsRef.current)
        if (runSearchSteps.length > 0) {
          meta.pendingSearchStepsRef.current = runSearchSteps
        }
        run.setQueuedDraft(null)
        run.setAwaitingInput(false)
        run.setPendingUserInput(null)
        run.setCheckInDraft('')

        const tid = session.threadId
        if (tid) markIdleRef.current(tid)
        refreshCreditsRef.current()

        void refreshMessagesRef.current({ requiredCompletedRunId: completedRunId })
          .then((items) => {
            const completedAssistant = findAssistantMessageForRun(items, completedRunId)
            if (completedAssistant) {
              const pendingSearchSteps = meta.pendingSearchStepsRef.current
              meta.pendingSearchStepsRef.current = null
              meta.persistRunDataToMessage(completedAssistant.id, {
                ...runCache,
                pendingSearchSteps,
              }, runEventsForMessage)
              run.markTerminalRunHistory(completedAssistant.id, false)
              stream.releaseCompletedHandoffToHistory()
            }
            const pending = run.pendingMessageRef.current
            if (pending) {
              run.pendingMessageRef.current = null
              void msgs.sendMessageRef.current?.(pending)
            }
          })
          .catch((err) => console.error('persist run data failed', err))
        continue
      }

      // ── run.cancelled ─────────────────────────────────────────────────────
      if (event.type === 'run.cancelled') {
        if (isACPDelegateEventData(event.data)) continue
        const blockedByInjection = run.injectionBlockedRunIdRef.current === event.run_id
        const runId = event.run_id
        run.setTerminalRunDisplayId(runId)
        run.setTerminalRunHandoffStatus('cancelled')
        const runSearchSteps = finalizeSearchSteps(stream.searchStepsRef.current)
        if (runSearchSteps.length > 0) {
          meta.pendingSearchStepsRef.current = runSearchSteps
        }
        const runEventsForMessage = runId
          ? (run.sse.events as MsgRunEvent[]).filter((e) => {
            if (e.run_id !== runId || typeof e.seq !== 'number') return false
            return e.seq <= event.seq
          })
          : []
        const runCache = captureTerminalRunCache('cancelled')
        if (runCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
          const frozenTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          runCache.handoffAssistantTurn = frozenTurn
          runCache.runAssistantTurn = frozenTurn
        }
        if (runId && session.threadId) {
          meta.persistThreadRunHandoff(session.threadId, runId, runCache)
        }
        resetTerminalRunState({ restoreQueuedDraft: true, preserveSearchSteps: true, handoffRunCache: runCache })
        if (!blockedByInjection) run.setError(null)
        if (runId) {
          void refreshMessagesRef.current({ requiredCompletedRunId: runId })
            .then((items) => {
              const assistant = findAssistantMessageForRun(items, runId)
              if (assistant) {
                meta.persistRunDataToMessage(assistant.id, runCache, runEventsForMessage)
                run.markTerminalRunHistory(assistant.id, false)
              }
            })
            .catch((err) => console.error('persist run data failed', err))
        }
        continue
      }

      // ── run.failed ────────────────────────────────────────────────────────
      if (event.type === 'run.failed') {
        if (isACPDelegateEventData(event.data)) continue
        const runId = event.run_id
        run.setTerminalRunDisplayId(runId)
        run.setTerminalRunHandoffStatus('failed')
        const runEventsForMessage = runId
          ? (run.sse.events as MsgRunEvent[]).filter((e) => {
            if (e.run_id !== runId || typeof e.seq !== 'number') return false
            return e.seq <= event.seq
          })
          : []
        const runCache = captureTerminalRunCache('failed')
        if (runCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
          const frozenTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          runCache.handoffAssistantTurn = frozenTurn
          runCache.runAssistantTurn = frozenTurn
        }
        if (runId && session.threadId) {
          meta.persistThreadRunHandoff(session.threadId, runId, runCache)
        }
        resetTerminalRunState({ restoreQueuedDraft: true, preserveSearchSteps: true, handoffRunCache: runCache })
        const obj = event.data as { message?: unknown; error_class?: unknown; code?: unknown; details?: unknown }
        const errorClass = typeof obj?.error_class === 'string' ? obj.error_class : undefined
        const details = (obj?.details && typeof obj.details === 'object' && !Array.isArray(obj.details))
          ? obj.details as Record<string, unknown>
          : undefined
        if (errorClass === 'security.injection_blocked') {
          run.setInjectionBlocked(typeof obj?.message === 'string' ? obj.message : 'blocked')
        } else {
          run.setError({
            message: typeof obj?.message === 'string' ? obj.message : '运行失败',
            code: typeof obj?.code === 'string' ? obj.code : errorClass,
            details,
          })
        }
        if (runId) {
          void refreshMessagesRef.current({ requiredCompletedRunId: runId })
            .then((items) => {
              const assistant = findAssistantMessageForRun(items, runId!)
              if (assistant) meta.persistRunDataToMessage(assistant.id, runCache, runEventsForMessage)
            })
            .catch((err) => console.error('persist run data failed', err))
        }
        continue
      }

      // ── run.interrupted ───────────────────────────────────────────────────
      if (event.type === 'run.interrupted') {
        if (isACPDelegateEventData(event.data)) continue
        const runId = event.run_id
        run.setTerminalRunDisplayId(runId)
        run.setTerminalRunHandoffStatus('interrupted')
        const runEventsForMessage = runId
          ? (run.sse.events as MsgRunEvent[]).filter((e) => {
            if (e.run_id !== runId || typeof e.seq !== 'number') return false
            return e.seq <= event.seq
          })
          : []
        const runCache = captureTerminalRunCache('interrupted')
        if (runCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
          const frozenTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
          runCache.handoffAssistantTurn = frozenTurn
          runCache.runAssistantTurn = frozenTurn
        }
        if (runId && session.threadId) {
          meta.persistThreadRunHandoff(session.threadId, runId, runCache)
        }
        resetTerminalRunState({ restoreQueuedDraft: true, preserveSearchSteps: true, handoffRunCache: runCache })
        const obj = event.data as { message?: unknown; error_class?: unknown; code?: unknown; details?: unknown }
        const errorClass = typeof obj?.error_class === 'string' ? obj.error_class : undefined
        const details = (obj?.details && typeof obj.details === 'object' && !Array.isArray(obj.details))
          ? obj.details as Record<string, unknown>
          : undefined
        run.setError({
          message: typeof obj?.message === 'string' ? obj.message : '运行中断',
          code: typeof obj?.code === 'string' ? obj.code : errorClass,
          details,
        })
        if (runId) {
          void refreshMessagesRef.current({ requiredCompletedRunId: runId })
            .then((items) => {
              const assistant = findAssistantMessageForRun(items, runId!)
              if (assistant) meta.persistRunDataToMessage(assistant.id, runCache, runEventsForMessage)
            })
            .catch((err) => console.error('persist run data failed', err))
        }
        continue
      }
    }

    if (needsBumpSnapshot) stream.bumpSnapshot()

  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run.sse.events])

  // ── 401 SSE 错误登出 ──────────────────────────────────────────────────────
  useEffect(() => {
    if (run.sse.error instanceof SSEApiError && run.sse.error.status === 401) {
      logout()
    }
  }, [run.sse.error, logout])

  // ── SSE fallback：连接断开但未收到终端事件 ────────────────────────────────
  useEffect(() => {
    if (!run.activeRunId) return
    if (run.sse.state !== 'closed' && run.sse.state !== 'error') return
    if (!run.sseTerminalFallbackArmedRef.current) return
    if (run.sseTerminalFallbackRunIdRef.current !== run.activeRunId) return

    const terminalRunId = run.activeRunId
    run.sseTerminalFallbackArmedRef.current = false
    run.sseTerminalFallbackRunIdRef.current = null

    const terminalRunMaxSeq = (run.sse.events as MsgRunEvent[])
      .filter((e) => e.run_id === terminalRunId && typeof e.seq === 'number')
      .reduce((max, e) => Math.max(max, e.seq), 0)
    const runEventsForMessage = (run.sse.events as MsgRunEvent[]).filter((e) =>
      e.run_id === terminalRunId &&
      typeof e.seq === 'number' &&
      e.seq <= terminalRunMaxSeq,
    )
    const terminalCache = captureTerminalRunCache()
    if (terminalCache.handoffAssistantTurn.segments.length === 0 && runEventsForMessage.length > 0) {
      const frozenTurn = buildFrozenAssistantTurnFromRunEvents(runEventsForMessage)
      terminalCache.handoffAssistantTurn = frozenTurn
      terminalCache.runAssistantTurn = frozenTurn
    }

    run.setTerminalRunDisplayId(terminalRunId)
    stream.setPreserveLiveRunUi(true)
    run.setTerminalRunHandoffStatus('interrupted')
    stream.setLiveAssistantTurn(terminalCache.handoffAssistantTurn.segments.length > 0 ? terminalCache.handoffAssistantTurn : null)

    if (session.threadId) {
      meta.persistThreadRunHandoff(session.threadId, terminalRunId, { ...terminalCache, terminalStatus: 'interrupted' })
    }

    run.setActiveRunId(null)
    stream.setPendingThinking(false)
    run.setQueuedDraft(null)
    run.setAwaitingInput(false)
    run.setPendingUserInput(null)
    run.setCheckInDraft('')
    const tid = session.threadId
    if (tid) markIdleRef.current(tid)
    refreshCreditsRef.current()

    void refreshMessagesRef.current({ requiredCompletedRunId: terminalRunId })
      .then((items) => {
        const completedAssistant = findAssistantMessageForRun(items, terminalRunId)
        if (completedAssistant) {
          meta.persistRunDataToMessage(completedAssistant.id, terminalCache, runEventsForMessage)
        }
      })
      .catch((err) => console.error('persist run data failed', err))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run.activeRunId, run.sse.state])

  // cleanup on unmount
  useEffect(() => {
    return () => { clearContextCompactHideTimer() }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
}
