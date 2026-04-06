import React, { useState, useEffect, useRef, useCallback, useMemo, Fragment, type ComponentProps } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowDown, Info, X } from 'lucide-react'
import { AutoResizeTextarea, Button, DebugTrigger } from '@arkloop/shared'
import { ChatInput } from './ChatInput'
import { RunDetailPanel } from './RunDetailPanel'
import type { CodeExecution } from './CodeExecutionCard'
import {
  CopTimeline,
  type WebSearchPhaseStep,
} from './CopTimeline'
import { MarkdownRenderer } from './MarkdownRenderer'
import { recordPerfCount, recordPerfValue } from '../perfDebug'
import { useTypewriter } from '../hooks/useTypewriter'
import { ArtifactStreamBlock, extractPartialArtifactFields, type StreamingArtifactEntry } from './ArtifactStreamBlock'
import { WidgetBlock } from './WidgetBlock'
import UserInputCard from './UserInputCard'
import { resolveMessageSourcesForRender } from './chatSourceResolver'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { ShareModal } from './ShareModal'
import { SourcesPanel } from './SourcesPanel'
import { CodeExecutionPanel } from './CodeExecutionPanel'
import { DocumentPanel } from './DocumentPanel'
import { WorkRightPanel } from './WorkRightPanel'
import { ChatTitleMenu } from './ChatTitleMenu'
import { MessageList } from './MessageList'
import { ContextCompactBar } from './ContextCompactBar'
import { IncognitoDivider } from './IncognitoDivider'
import { SSEApiError } from '../sse'
import { getInjectionBlockMessage, shouldSuppressLiveRunEventAfterInjectionBlock } from '../liveRunSecurity'
import {
  applyCodeExecutionToolCall,
  applyCodeExecutionToolResult,
  applyTerminalDelta,
  buildMessageCodeExecutionsFromRunEvents,
  patchCodeExecutionList,
  buildMessageWidgetsFromRunEvents,
  findAssistantMessageForRun,
  selectFreshRunEvents,
  shouldRefetchCompletedRunMessages,
  shouldReplayMessageCodeExecutions,
  applyBrowserToolCall,
  applyBrowserToolResult,
  buildMessageBrowserActionsFromRunEvents,
  applySubAgentToolCall,
  applySubAgentToolResult,
  buildMessageSubAgentsFromRunEvents,
  applyFileOpToolCall,
  applyFileOpToolResult,
  buildMessageFileOpsFromRunEvents,
  applyWebFetchToolCall,
  applyWebFetchToolResult,
  buildMessageWebFetchesFromRunEvents,
  isWebFetchToolName,
} from '../runEventProcessing'
import {
  buildAssistantTurnFromRunEvents,
  copSegmentCalls,
  foldAssistantTurnEvent,
  requestAssistantTurnThinkingBreak,
  type AssistantTurnSegment,
  type AssistantTurnUi,
} from '../assistantTurnSegments'
import { copTimelinePayloadForSegment, toolCallIdsInCopTimelines } from '../copSegmentTimeline'
import { applyRunEventToWebSearchSteps, isWebSearchToolName, webSearchSourcesFromResult } from '../webSearchTimelineFromRunEvent'
import { useLocale } from '../contexts/LocaleContext'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useAppUI } from '../contexts/app-ui'
import { useCredits } from '../contexts/credits'
import { useChatSession } from '../contexts/chat-session'
import { useMessageStore } from '../contexts/message-store'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useMessageMeta, type MessageMeta } from '../contexts/message-meta'
import { useStream } from '../contexts/stream'
import { usePanels } from '../contexts/panels'
import { useScrollPin } from '../hooks/useScrollPin'
import { useDevTools } from '../hooks/useDevTools'
import { useChatActions } from '../hooks/useChatActions'
import { useAttachmentActions } from '../hooks/useAttachmentActions'
import { useMessageMetaCompat } from '../hooks/useMessageMetaCompat'
import { useRunTransition, type TerminalRunCache } from '../hooks/useRunTransition'
import {
  isTerminalRunEventType,
  normalizeError,
  mergeVisibleSegmentsIntoAssistantTurn,
  buildFrozenAssistantTurnFromRunEvents,
  interruptedErrorFromRunEvents,
  failedErrorFromRunEvents,
  assistantTurnHasVisibleOutput,
  finalizeSearchSteps,
  patchLegacySearchSteps,
  liveTurnHasThinkingSegment,
  thinkingRowsForCop,
  copInlineTextRowsForCop,
  buildStreamingArtifactsFromHandoff,
  resolveCopHeaderOverride,
} from '../lib/chat-helpers'
import { apiBaseUrl } from '@arkloop/shared/api'
import { isACPDelegateEventData } from '@arkloop/shared'
import { ChatSkeleton } from './ChatSkeleton'
import type { RequestedSchema } from '../userInputTypes'
import {
  createMessage,
  createRun,
  editMessage,
  forkThread,
  listMessages,
  listRunEvents,
  listThreadRuns,
  uploadStagingAttachment,
  isApiError,
  type MessageContent,
  type MessageResponse,
} from '../api'
import { buildMessageRequest } from '../messageContent'
import {
  addSearchThreadId,
  SEARCH_PERSONA_KEY,
  isSearchThreadId,
  readThreadRunHandoff,
  clearThreadRunHandoff,
  readMessageSources,
  readMessageArtifacts,
  readMessageCodeExecutions,
  writeMessageCodeExecutions,
  readMessageBrowserActions,
  writeMessageBrowserActions,
  readMessageSearchSteps,
  writeMessageSearchSteps,
  readMessageAssistantTurn,
  writeMessageAssistantTurn,
  type WebSource,
  type ArtifactRef,
  type CodeExecutionRef,
  type BrowserActionRef,
  type SubAgentRef,
  type FileOpRef,
  type MessageThinkingRef,
  type MessageSearchStepRef,
  readMessageSubAgents,
  writeMessageSubAgents,
  readMessageFileOps,
  writeMessageFileOps,
  readMessageWebFetches,
  writeMessageWebFetches,
  type WebFetchRef,
  readMessageTerminalStatus,
  writeMessageTerminalStatus,
  type MessageTerminalStatusRef,
  readMessageWidgets,
  writeMessageWidgets,
  type WidgetRef,
  migrateMessageMetadata,
  readMsgRunEvents,
  type MsgRunEvent,
  readThreadWorkFolder,
} from '../storage'

const sidePanelWidth = 360
const documentPanelWidth = 560
const chatContentPadding = { panelClosed: '60px', panelOpen: '40px' } as const
const chatInputPadding = { panelClosed: '60px', panelOpen: '40px', work: '14px' } as const

function FailedRunRetryCard({
  title,
  actionLabel,
  onRetry,
}: {
  title: string
  actionLabel?: string
  onRetry?: () => void
}) {
  return (
    <div
      className="flex w-full max-w-[756px] items-center justify-between gap-3 rounded-2xl px-4 py-4"
      style={{ background: 'var(--c-bg-sub)', border: '0.75px solid var(--c-border)' }}
    >
      <div className="flex min-w-0 items-center gap-2 text-[var(--c-text-secondary)]">
        <Info size={16} className="shrink-0 text-[var(--c-text-tertiary)]" />
        <span className="truncate text-[14px]">{title}</span>
      </div>
      {actionLabel && (
        <Button
          variant="outline"
          size="md"
          onClick={onRetry}
          disabled={!onRetry}
          className="failed-run-retry-button shrink-0"
        >
          {actionLabel}
        </Button>
      )}
    </div>
  )
}

type LocationState = {
  initialRunId?: string
  isSearch?: boolean
  isIncognitoFork?: boolean
  forkBaseCount?: number
  userEnterMessageId?: string
  welcomeUserMessage?: MessageResponse
} | null

type DocumentPanelState = {
  artifact: ArtifactRef
  artifacts: ArtifactRef[]
  runId?: string
}

type LiveRunPaneProps = {
  showPendingThinkingShell: boolean
  preserveLiveRunUi: boolean
  leadingLiveCop: CopSegment | null
  trailingLiveSegments: AssistantTurnUi['segments']
  liveSegments: AssistantTurnUi['segments']
  liveRunUiActive: boolean
  liveRunUiVisible: boolean
  liveAssistantTurn: AssistantTurnUi | null
  allStreamItemsForUi: Array<{ id: string }>
  dedupedTopLevelCodeExecutions: CodeExecutionRef[]
  topLevelSubAgents: SubAgentRef[]
  topLevelFileOps: FileOpRef[]
  topLevelWebFetches: WebFetchRef[]
  codePanelExecutionId?: string | null
  currentRunSources: WebSource[]
  currentRunArtifacts: ArtifactRef[]
  activeRunId: string | null
  accessToken: string
  baseUrl: string
  thinkingHint?: string
  visibleStreamingWidgets: StreamingArtifactEntry[]
  visibleStreamingArtifacts: StreamingArtifactEntry[]
  injectionBlocked: string | null
  awaitingInput: boolean
  checkInDraft: string
  checkInSubmitting: boolean
  onCheckInDraftChange: (value: string) => void
  onCheckInSubmit: () => void
  terminalSseError: AppError | null
  pendingIncognito: boolean
  incognitoDividerText: string
  onIncognitoDividerComplete: () => void
  terminalRunHandoffStatus: MessageTerminalStatusRef | null
  terminalRunDisplayId: string | null
  terminalRunHasOutput: boolean
  failedRunRetryTitle: string
  runInterruptedLabel: string
  runCancelledLabel: string
  actionLabelForTerminalRun: (params: {
    status: MessageTerminalStatusRef | null
    hasOutput: boolean
  }) => string | undefined
  actionHandlerForTerminalRun: (params: {
    runId: string | null | undefined
    status: MessageTerminalStatusRef | null
    hasOutput: boolean
  }) => (() => void) | undefined
  onOpenDocument: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  onOpenCodeExecution: (ce: CodeExecution) => void
  onArtifactAction: ComponentProps<typeof WidgetBlock>['onAction']
  renderLiveCopSegment: (seg: CopSegment, si: number, key?: string) => React.ReactNode
  bottomRef: React.RefObject<HTMLDivElement | null>
  messages: MessageResponse[]
}

function LiveRunPane({
  showPendingThinkingShell,
  preserveLiveRunUi,
  leadingLiveCop,
  trailingLiveSegments,
  liveSegments,
  liveRunUiActive,
  liveRunUiVisible,
  liveAssistantTurn,
  allStreamItemsForUi,
  dedupedTopLevelCodeExecutions,
  topLevelSubAgents,
  topLevelFileOps,
  topLevelWebFetches,
  codePanelExecutionId,
  currentRunSources,
  currentRunArtifacts,
  activeRunId,
  accessToken,
  baseUrl,
  thinkingHint,
  visibleStreamingWidgets,
  visibleStreamingArtifacts,
  injectionBlocked,
  awaitingInput,
  checkInDraft,
  checkInSubmitting,
  onCheckInDraftChange,
  onCheckInSubmit,
  terminalSseError,
  pendingIncognito,
  incognitoDividerText,
  onIncognitoDividerComplete,
  terminalRunHandoffStatus,
  terminalRunDisplayId,
  terminalRunHasOutput,
  failedRunRetryTitle,
  runInterruptedLabel,
  runCancelledLabel,
  actionLabelForTerminalRun,
  actionHandlerForTerminalRun,
  onOpenDocument,
  onOpenCodeExecution,
  onArtifactAction,
  renderLiveCopSegment,
  bottomRef,
  messages,
}: LiveRunPaneProps) {
  return (
    <>
      {(showPendingThinkingShell || liveSegments.length > 0) && (
        <div data-testid={preserveLiveRunUi ? 'current-run-handoff' : undefined} style={{ display: 'flex', flexDirection: 'column', gap: 0, maxWidth: '663px' }}>
          {(showPendingThinkingShell || leadingLiveCop) && (
            <Fragment>
              {leadingLiveCop
                ? renderLiveCopSegment(leadingLiveCop, 0, 'cop-leading')
                : (
                  <CopTimeline
                    key="cop-leading-inner"
                    steps={[]}
                    sources={[]}
                    isComplete={false}
                    live
                    shimmer
                    thinkingHint={thinkingHint}
                    accessToken={accessToken}
                    baseUrl={baseUrl}
                  />
                )}
            </Fragment>
          )}
          {trailingLiveSegments.map((seg, idx) => {
            const si = leadingLiveCop ? idx + 1 : idx
            const lastSegIdx = liveSegments.length - 1
            const lastTurnSeg = liveSegments[lastSegIdx]
            const mdTypewriterDone =
              !liveRunUiActive ||
              lastTurnSeg?.type !== 'text' ||
              si !== lastSegIdx

            return seg.type === 'text' ? (
              <LiveTurnMarkdown
                key={`live-at-${si}`}
                content={seg.content}
                typewriterDone={mdTypewriterDone}
                webSources={currentRunSources.length > 0 ? currentRunSources : undefined}
                artifacts={currentRunArtifacts.length > 0 ? currentRunArtifacts : undefined}
                accessToken={accessToken}
                runId={activeRunId ?? undefined}
                onOpenDocument={onOpenDocument}
                trimTrailingMargin={
                  liveSegments[si + 1] == null ||
                  liveSegments[si + 1]?.type === 'cop'
                }
              />
            ) : (
              renderLiveCopSegment(seg, si, `live-acw-${si}`)
            )
          })}
        </div>
      )}

      {!liveRunUiVisible &&
        liveAssistantTurn == null &&
        allStreamItemsForUi.length === 0 &&
        (dedupedTopLevelCodeExecutions.length > 0 || topLevelSubAgents.length > 0 || topLevelFileOps.length > 0 || topLevelWebFetches.length > 0) && (
        <div style={{ maxWidth: '663px' }}>
          <CopTimeline
            steps={[]}
            sources={[]}
            isComplete
            codeExecutions={dedupedTopLevelCodeExecutions.length > 0 ? dedupedTopLevelCodeExecutions : undefined}
            onOpenCodeExecution={onOpenCodeExecution}
            activeCodeExecutionId={codePanelExecutionId ?? undefined}
            subAgents={topLevelSubAgents.length > 0 ? topLevelSubAgents : undefined}
            fileOps={topLevelFileOps.length > 0 ? topLevelFileOps : undefined}
            webFetches={topLevelWebFetches.length > 0 ? topLevelWebFetches : undefined}
            accessToken={accessToken}
            baseUrl={baseUrl}
          />
        </div>
      )}

      {visibleStreamingWidgets.map((entry) => (
        <WidgetBlock
          key={`streaming-widget-${entry.toolCallIndex}`}
          html={entry.content ?? ''}
          title={entry.title ?? 'Widget'}
          complete={entry.complete}
          loadingMessages={entry.loadingMessages}
          onAction={onArtifactAction}
        />
      ))}

      {visibleStreamingArtifacts.map((entry) => (
        <ArtifactStreamBlock
          key={`streaming-artifact-${entry.toolCallIndex}`}
          entry={entry}
          accessToken={accessToken}
          onAction={onArtifactAction}
        />
      ))}

      {injectionBlocked && (
        <div className="max-w-[720px] rounded-xl border-[0.5px] border-[var(--c-error-border)] bg-[var(--c-error-bg)] px-4 py-3 text-sm text-[var(--c-error-text)]">
          {injectionBlocked}
        </div>
      )}

      {awaitingInput && (
        <div
          className="flex flex-col gap-2 rounded-xl px-4 py-3"
          style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
        >
          <AutoResizeTextarea
            autoFocus
            rows={3}
            minRows={3}
            maxHeight={240}
            value={checkInDraft}
            onChange={(e) => onCheckInDraftChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                onCheckInSubmit()
              }
            }}
            disabled={checkInSubmitting}
            className="w-full resize-none rounded-lg bg-transparent px-1 py-0.5 text-sm outline-none"
            style={{ color: 'var(--c-text-primary)', caretColor: 'var(--c-text-primary)' }}
            placeholder=" "
          />
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onCheckInSubmit}
              disabled={checkInSubmitting || !checkInDraft.trim()}
              className="rounded-lg px-3 py-1 text-xs font-medium transition-opacity disabled:opacity-40"
              style={{ background: 'var(--c-brand)', color: '#fff' }}
            >
              {checkInSubmitting ? '...' : 'Send'}
            </button>
          </div>
        </div>
      )}

      {terminalSseError && <ErrorCallout error={terminalSseError} />}

      {pendingIncognito && (
        <IncognitoDivider
          text={incognitoDividerText}
          onComplete={onIncognitoDividerComplete}
        />
      )}

      {(terminalRunHandoffStatus === 'failed' || terminalRunHandoffStatus === 'interrupted' || terminalRunHandoffStatus === 'cancelled') && terminalRunDisplayId && !messages.some((msg) => msg.role === 'assistant' && msg.run_id === terminalRunDisplayId) && (
        <FailedRunRetryCard
          title={terminalRunHandoffStatus === 'interrupted' ? runInterruptedLabel : terminalRunHandoffStatus === 'cancelled' ? runCancelledLabel : failedRunRetryTitle}
          actionLabel={actionLabelForTerminalRun({
            status: terminalRunHandoffStatus,
            hasOutput: terminalRunHasOutput,
          })}
          onRetry={actionHandlerForTerminalRun({
            runId: terminalRunDisplayId,
            status: terminalRunHandoffStatus,
            hasOutput: terminalRunHasOutput,
          })}
        />
      )}
      <div ref={bottomRef} />
    </>
  )
}

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>

function liveStreamingWidgetEntriesForCop(seg: CopSegment, entries: StreamingArtifactEntry[]): StreamingArtifactEntry[] {
  const out: StreamingArtifactEntry[] = []
  for (const c of copSegmentCalls(seg)) {
    if (c.toolName !== 'show_widget') continue
    const e = entries.find((x) => x.toolName === 'show_widget' && x.toolCallId === c.toolCallId)
    if (!e) continue
    if ((e.content != null && e.content.length > 0) || (e.loadingMessages != null && e.loadingMessages.length > 0)) {
      out.push(e)
    }
  }
  return out
}

function liveInlineArtifactEntriesForCop(seg: CopSegment, entries: StreamingArtifactEntry[]): StreamingArtifactEntry[] {
  const out: StreamingArtifactEntry[] = []
  for (const c of copSegmentCalls(seg)) {
    if (c.toolName !== 'create_artifact') continue
    const e = entries.find((x) => x.toolName === 'create_artifact' && x.toolCallId === c.toolCallId)
    if (e && e.content && e.display !== 'panel') out.push(e)
  }
  return out
}

function liveCopShowWidgetCallIds(turn: AssistantTurnUi | null): Set<string> {
  const ids = new Set<string>()
  if (!turn) return ids
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'show_widget' && c.toolCallId) ids.add(c.toolCallId)
    }
  }
  return ids
}

function liveCopCreateArtifactCallIds(turn: AssistantTurnUi | null): Set<string> {
  const ids = new Set<string>()
  if (!turn) return ids
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'create_artifact' && c.toolCallId) ids.add(c.toolCallId)
    }
  }
  return ids
}

function LiveTurnMarkdown({
  content,
  typewriterDone,
  ...rest
}: {
  content: string
  typewriterDone: boolean
} & Omit<ComponentProps<typeof MarkdownRenderer>, 'content'>) {
  const displayed = useTypewriter(content, typewriterDone)
  useEffect(() => {
    recordPerfCount('live_turn_markdown_render', 1, {
      contentLength: content.length,
      displayedLength: displayed.length,
      typewriterDone,
    })
    recordPerfValue('live_turn_markdown_displayed', displayed.length, 'chars', {
      contentLength: content.length,
      typewriterDone,
    })
  }, [content.length, displayed.length, typewriterDone])
  return <MarkdownRenderer content={displayed} streaming={!typewriterDone} {...rest} />
}

export function ChatView() {
  const { accessToken, logout: onLoggedOut } = useAuth()
  const {
    threads, addThread: onThreadCreated,
    markRunning: onRunStarted, markIdle: onRunEnded,
    updateTitle: onThreadTitleUpdated,
  } = useThreadList()
  const {
    appMode,
    openSettings: onOpenSettings,
    setAppMode,
  } = useAppUI()
  const { refreshCredits } = useCredits()
  const { threadId } = useChatSession()
  const {
    messages,
    setMessages,
    messagesLoading,
    setMessagesLoading,
    draft,
    setDraft,
    attachments,
    setAttachments,
    setUserEnterMessageId,
    pendingIncognito,
    setPendingIncognito,
    sendMessageRef,
    beginMessageSync,
    isMessageSyncCurrent,
    invalidateMessageSync,
    readConsistentMessages,
    refreshMessages,
  } = useMessageStore()
  const {
    activeRunId,
    setActiveRunId,
    sending,
    setSending,
    cancelSubmitting,
    setCancelSubmitting,
    setError,
    injectionBlocked,
    setInjectionBlocked,
    injectionBlockedRunIdRef,
    queuedDraft,
    setQueuedDraft,
    awaitingInput,
    setAwaitingInput,
    pendingUserInput,
    setPendingUserInput,
    checkInDraft,
    setCheckInDraft,
    checkInSubmitting,
    contextCompactBar,
    setContextCompactBar,
    terminalRunDisplayId,
    setTerminalRunDisplayId,
    terminalRunHandoffStatus,
    setTerminalRunHandoffStatus,
    markTerminalRunHistory: markTerminalRunHistoryState,
    completedTitleTailRunId,
    clearCompletedTitleTail: clearCompletedTitleTailState,
    armCompletedTitleTail: armCompletedTitleTailState,
    sse,
    sseRunId,
    isStreaming,
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
    loadFromStorage,
    setMetaBatch,
    clearAll: clearAllMeta,
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
    liveAssistantTurn,
    setLiveAssistantTurn,
    preserveLiveRunUi,
    setPreserveLiveRunUi,
    assistantTurnFoldStateRef,
    bumpSnapshot: bumpAssistantTurnSnapshotState,
    searchSteps,
    setSearchSteps,
    searchStepsRef,
    resetSearchSteps: resetSearchStepsState,
    streamingArtifacts,
    setStreamingArtifacts,
    streamingArtifactsRef,
    setSegments,
    segmentsRef,
    activeSegmentIdRef,
    pendingThinking,
    setPendingThinking,
    thinkingHint,
    setThinkingHint,
    topLevelCodeExecutions,
    setTopLevelCodeExecutions,
    topLevelSubAgents,
    setTopLevelSubAgents,
    topLevelFileOps,
    setTopLevelFileOps,
    topLevelWebFetches,
    setTopLevelWebFetches,
    workTodos,
    setWorkTodos,
  } = useStream()
  const {
    activePanel,
    shareModal,
    openSourcePanel,
    openCodePanel: openCodePanelState,
    openDocumentPanel: openDocumentPanelState,
    closePanel,
    closeShareModal,
  } = usePanels()
  const threadsRef = useRef(threads)
  useEffect(() => { threadsRef.current = threads }, [threads])
  const location = useLocation()
  const locationState = location.state as LocationState
  const navigate = useNavigate()
  const { t } = useLocale()
  const welcomeUserMessage = locationState?.welcomeUserMessage
  const currentThread = threadId ? threads.find((thread) => thread.id === threadId) : undefined
  const shouldSkipInitialSkeleton = !!(
    welcomeUserMessage &&
    locationState?.userEnterMessageId === welcomeUserMessage.id &&
    welcomeUserMessage.role === 'user'
  )

  const baseUrl = apiBaseUrl()

  const [isSearchThread, setIsSearchThread] = useState(
    () => locationState?.isSearch === true || isSearchThreadId(threadId ?? ''),
  )

  const {
    messageSourcesMap,
    messageFileOpsMap,
  } = useMessageMetaCompat()

  const shareModalOpen = shareModal.open
  const sourcePanelMessageId = activePanel?.type === 'source' ? activePanel.messageId : null
  const codePanelExecution = activePanel?.type === 'code' ? activePanel.execution : null
  const documentPanelArtifact = activePanel?.type === 'document' ? activePanel.artifact : null
  const setSourcePanelMessageId = useCallback<React.Dispatch<React.SetStateAction<string | null>>>((value) => {
    const next = typeof value === 'function' ? value(sourcePanelMessageId) : value
    if (next) openSourcePanel(next)
    else if (activePanel?.type === 'source') closePanel()
  }, [activePanel, closePanel, openSourcePanel, sourcePanelMessageId])
  const setCodePanelExecution = useCallback<React.Dispatch<React.SetStateAction<CodeExecution | null>>>((value) => {
    const next = typeof value === 'function' ? value(codePanelExecution) : value
    if (next) openCodePanelState(next)
    else if (activePanel?.type === 'code') closePanel()
  }, [activePanel, closePanel, codePanelExecution, openCodePanelState])
  const setDocumentPanelArtifact = useCallback<React.Dispatch<React.SetStateAction<DocumentPanelState | null>>>((value) => {
    const next = typeof value === 'function' ? value(documentPanelArtifact) : value
    if (next) openDocumentPanelState(next)
    else if (activePanel?.type === 'document') closePanel()
  }, [activePanel, closePanel, documentPanelArtifact, openDocumentPanelState])
  const lastCodePanelRef = useRef<CodeExecution | null>(null)
  const lastDocumentPanelRef = useRef<DocumentPanelState | null>(null)
  // 关闭动画期间保留上一次的数据
  const lastPanelSourcesRef = useRef<WebSource[] | undefined>(undefined)
  const lastPanelQueryRef = useRef<string | undefined>(undefined)
  const contextCompactHideTimerRef = useRef<number | null>(null)
  const clearContextCompactHideTimer = useCallback(() => {
    if (contextCompactHideTimerRef.current != null) {
      clearTimeout(contextCompactHideTimerRef.current)
      contextCompactHideTimerRef.current = null
    }
  }, [])

  // --- Work todo 进度 ---
  const { showRunEvents, showDebugPanel, runDetailPanelRunId, setRunDetailPanelRunId } = useDevTools()

  const markTerminalRunHistory = useCallback((messageId: string | null, expanded = true) => {
    markTerminalRunHistoryState(messageId, expanded)
  }, [markTerminalRunHistoryState])
  const bumpAssistantTurnSnapshot = useCallback(() => {
    bumpAssistantTurnSnapshotState()
  }, [bumpAssistantTurnSnapshotState])
  const resetSearchSteps = useCallback(() => {
    resetSearchStepsState()
  }, [resetSearchStepsState])
  // applySearchSteps queues finalized steps for storage once the message ID is
  // known (handled in the run.completed refreshMessages callback).
  const applySearchSteps = useCallback((getter: () => MessageSearchStepRef[]) => {
    pendingSearchStepsRef.current = getter()
  }, [])
  const clearQueuedDraft = useCallback(() => {
    pendingMessageRef.current = null
    setPreserveLiveRunUi(false)
    setQueuedDraft(null)
  }, [])
  const clearCompletedTitleTail = useCallback(() => {
    clearCompletedTitleTailState()
  }, [clearCompletedTitleTailState])
  const armCompletedTitleTail = useCallback((runId: string) => {
    armCompletedTitleTailState(runId)
  }, [armCompletedTitleTailState])
  const restoreQueuedDraftToInput = useCallback(() => {
    const pending = pendingMessageRef.current
    pendingMessageRef.current = null
    setQueuedDraft(null)
    if (!pending) return
    setDraft((prev) => prev.trim() ? prev : pending)
  }, [])

  const liveRunUiVisible = isStreaming || preserveLiveRunUi
  const liveRunUiActive =
    isStreaming ||
    (preserveLiveRunUi && terminalRunHandoffStatus !== 'cancelled' && terminalRunHandoffStatus !== 'failed')
  const showPendingThinkingShell =
    pendingThinking &&
    !liveTurnHasThinkingSegment(liveAssistantTurn) &&
    (sending || activeRunId != null)

  const {
    isAtBottom,
    bottomRef,
    scrollContainerRef,
    lastUserMsgRef,
    inputAreaRef,
    forceInstantBottomScrollRef,
    isAtBottomRef,
    handleScrollContainerScroll,
    stabilizeDocumentPanelScroll,
    scrollToBottom,
  } = useScrollPin({
    messagesLoading,
    messages,
    liveAssistantTurn,
    liveRunUiVisible,
    topLevelCodeExecutionsLength: topLevelCodeExecutions.length,
  })
  const {
    resetAssistantTurnLive,
    clearLiveRunSecurityArtifacts,
    releaseCompletedHandoffToHistory,
    captureTerminalRunCache,
    persistRunDataToMessage,
    persistThreadRunHandoff,
  } = useRunTransition({ forceInstantBottomScrollRef, lastUserMsgRef })

  const prevActiveRunIdRef = useRef<string | null>(null)
  useEffect(() => {
    if (activeRunId && activeRunId !== prevActiveRunIdRef.current) {
      setWorkTodos([])
    }
    prevActiveRunIdRef.current = activeRunId
  }, [activeRunId])

  useEffect(() => {
    if (messagesLoading) {
      forceInstantBottomScrollRef.current = false
    }
  }, [messagesLoading, forceInstantBottomScrollRef])

  const canCancel =
    activeRunId != null &&
    (sse.state === 'connecting' || sse.state === 'connected' || sse.state === 'reconnecting')
  const {
    sendMessage,
    handleEditMessage,
    handleRetry,
    handleContinue,
    handleFork,
    handleCancel,
    handleCheckInSubmit,
    handleUserInputSubmit,
    handleUserInputDismiss,
    handleAsrError,
    handleArtifactAction,
  } = useChatActions({ scrollToBottom })
  void sendMessage

  // 加载 thread 数据
  useEffect(() => {
    if (!threadId) return
    const syncVersion = beginMessageSync()
    let disposed = false

    if (!shouldSkipInitialSkeleton) {
      setMessagesLoading(true)
      setUserEnterMessageId(null)
    } else if (welcomeUserMessage) {
      setMessages([welcomeUserMessage])
      setMessagesLoading(false)
    }
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    const navUserEnterMessageId = locationState?.userEnterMessageId

    void (async () => {
      let loadedItems: MessageResponse[] | null = null
      try {
        const [initialItems, runs] = await Promise.all([
          listMessages(accessToken, threadId),
          listThreadRuns(accessToken, threadId, 1),
        ])
        if (disposed || !isMessageSyncCurrent(syncVersion)) return

        const latest = runs[0]
        const items = shouldRefetchCompletedRunMessages({ messages: initialItems, latestRun: latest })
          ? await readConsistentMessages(latest.run_id)
          : initialItems
        if (disposed || !isMessageSyncCurrent(syncVersion)) return

        loadedItems = items
        setMessages(items)
        let interruptedError = latest?.status === 'interrupted'
          ? { message: t.runInterrupted }
          : null
        let failedError = latest?.status === 'failed'
          ? { message: t.failedRunRetryTitle }
          : null

        // 加载各消息缓存的 web 来源
        const sourcesMap = new Map<string, WebSource[]>()
        const artifactsMap = new Map<string, ArtifactRef[]>()
        const widgetsMap = new Map<string, WidgetRef[]>()
        const codeExecMap = new Map<string, CodeExecutionRef[]>()
        const browserActionsMap = new Map<string, BrowserActionRef[]>()
        const subAgentsMap = new Map<string, SubAgentRef[]>()
        const fileOpsMap = new Map<string, FileOpRef[]>()
        const webFetchesMap = new Map<string, WebFetchRef[]>()
        const thinkingMap = new Map<string, MessageThinkingRef>()
        const searchStepsMap = new Map<string, MessageSearchStepRef[]>()
        const terminalStatusMap = new Map<string, MessageTerminalStatusRef>()

        const runEventsMap = new Map<string, MsgRunEvent[]>()
        const assistantTurnMap = new Map<string, AssistantTurnUi>()
        for (const msg of items) {
          if (msg.role !== 'assistant') continue

          const cached = readMessageSources(msg.id)
          if (cached) sourcesMap.set(msg.id, cached)
          const cachedArt = readMessageArtifacts(msg.id)
          if (cachedArt) artifactsMap.set(msg.id, cachedArt)
          const cachedWidgets = readMessageWidgets(msg.id)
          if (cachedWidgets) widgetsMap.set(msg.id, cachedWidgets)
          const cachedExec = readMessageCodeExecutions(msg.id)
          if (cachedExec) codeExecMap.set(msg.id, cachedExec)
          const cachedBrowserActions = readMessageBrowserActions(msg.id)
          if (cachedBrowserActions) browserActionsMap.set(msg.id, cachedBrowserActions)
          const cachedSubAgents = readMessageSubAgents(msg.id)
          if (cachedSubAgents) subAgentsMap.set(msg.id, cachedSubAgents)
          const cachedFileOps = readMessageFileOps(msg.id)
          if (cachedFileOps) fileOpsMap.set(msg.id, cachedFileOps)
          const cachedWebFetches = readMessageWebFetches(msg.id)
          if (cachedWebFetches) webFetchesMap.set(msg.id, cachedWebFetches)
          const cachedSearchSteps = readMessageSearchSteps(msg.id)
          if (cachedSearchSteps) {
            const patched = patchLegacySearchSteps(cachedSearchSteps)
            if (patched.changed) writeMessageSearchSteps(msg.id, patched.steps)
            searchStepsMap.set(msg.id, patched.steps)
          }
          const cachedTerminalStatus = readMessageTerminalStatus(msg.id)
          if (cachedTerminalStatus) {
            terminalStatusMap.set(msg.id, cachedTerminalStatus)
          }

          const cachedRunEvents = readMsgRunEvents(msg.id)
          let hydratedAssistantTurn: AssistantTurnUi | null = null
          if (cachedRunEvents) {
            runEventsMap.set(msg.id, cachedRunEvents)
            if (latest?.status === 'interrupted' && latest.run_id && msg.run_id === latest.run_id) {
              interruptedError = interruptedErrorFromRunEvents(cachedRunEvents, t.runInterrupted)
            }
            if (latest?.status === 'failed' && latest.run_id && msg.run_id === latest.run_id) {
              failedError = failedErrorFromRunEvents(cachedRunEvents, t.failedRunRetryTitle)
            }
            const rebuiltTurn = buildAssistantTurnFromRunEvents(cachedRunEvents)
            if (rebuiltTurn.segments.length > 0) {
              hydratedAssistantTurn = rebuiltTurn
              writeMessageAssistantTurn(msg.id, rebuiltTurn)
            }
          }
          if (!hydratedAssistantTurn) {
            const cachedTurn = readMessageAssistantTurn(msg.id)
            if (cachedTurn) {
              hydratedAssistantTurn = cachedTurn
            }
          }
          if (hydratedAssistantTurn) {
            assistantTurnMap.set(msg.id, hydratedAssistantTurn)
          }
        }

        // 服务端回放：补齐最新一轮的运行缓存
        const lastAssistant = latest
          ? findAssistantMessageForRun(items, latest.run_id)
          : [...items].reverse().find((m) => m.role === 'assistant')
        const replayWidgetsNeeded = !!(lastAssistant && !widgetsMap.has(lastAssistant.id))
        const replayCodeExecNeeded = !!(lastAssistant && shouldReplayMessageCodeExecutions(codeExecMap.get(lastAssistant.id)))
        const replayBrowserActionsNeeded = !!(lastAssistant && !browserActionsMap.has(lastAssistant.id))
        const replaySubAgentsNeeded = !!(lastAssistant && !subAgentsMap.has(lastAssistant.id))
        const replayFileOpsNeeded = !!(lastAssistant && !fileOpsMap.has(lastAssistant.id))
        const replayWebFetchesNeeded = !!(lastAssistant && !webFetchesMap.has(lastAssistant.id))
        const replayAssistantTurnNeeded = !!(lastAssistant && !assistantTurnMap.has(lastAssistant.id))
        const shouldReplayLatestRun =
          !!latest &&
          latest.status !== 'running' &&
          (
            latest.status === 'interrupted' ||
            latest.status === 'failed' ||
            (lastAssistant != null && (
              replayWidgetsNeeded ||
              replayCodeExecNeeded ||
              replayBrowserActionsNeeded ||
              replaySubAgentsNeeded ||
              replayFileOpsNeeded ||
              replayWebFetchesNeeded ||
              replayAssistantTurnNeeded
            ))
          )
        if (shouldReplayLatestRun && latest) {
          try {
            const replayEvents = await listRunEvents(accessToken, latest.run_id, { follow: false })
            if (latest.status === 'interrupted') {
              interruptedError = interruptedErrorFromRunEvents(replayEvents, t.runInterrupted)
            }
            if (latest.status === 'failed') {
              failedError = failedErrorFromRunEvents(replayEvents, t.failedRunRetryTitle)
            }
            if (lastAssistant && replayWidgetsNeeded) {
              const replayWidgets = buildMessageWidgetsFromRunEvents(replayEvents)
              if (replayWidgets.length > 0) {
                widgetsMap.set(lastAssistant.id, replayWidgets)
                writeMessageWidgets(lastAssistant.id, replayWidgets)
              }
            }
            if (lastAssistant && replayCodeExecNeeded) {
              const replayExecs = buildMessageCodeExecutionsFromRunEvents(replayEvents)
              codeExecMap.set(lastAssistant.id, replayExecs)
              writeMessageCodeExecutions(lastAssistant.id, replayExecs)
            }
            if (lastAssistant && replayBrowserActionsNeeded) {
              const replayActions = buildMessageBrowserActionsFromRunEvents(replayEvents)
              if (replayActions.length > 0) {
                browserActionsMap.set(lastAssistant.id, replayActions)
                writeMessageBrowserActions(lastAssistant.id, replayActions)
              }
            }
            if (lastAssistant && replaySubAgentsNeeded) {
              const replayAgents = buildMessageSubAgentsFromRunEvents(replayEvents)
              if (replayAgents.length > 0) {
                subAgentsMap.set(lastAssistant.id, replayAgents)
                writeMessageSubAgents(lastAssistant.id, replayAgents)
              }
            }
            if (lastAssistant && replayFileOpsNeeded) {
              const replayFileOps = buildMessageFileOpsFromRunEvents(replayEvents)
              if (replayFileOps.length > 0) {
                fileOpsMap.set(lastAssistant.id, replayFileOps)
                writeMessageFileOps(lastAssistant.id, replayFileOps)
              }
            }
            if (lastAssistant && replayWebFetchesNeeded) {
              const replayWebFetches = buildMessageWebFetchesFromRunEvents(replayEvents)
              if (replayWebFetches.length > 0) {
                webFetchesMap.set(lastAssistant.id, replayWebFetches)
                writeMessageWebFetches(lastAssistant.id, replayWebFetches)
              }
            }
            if (lastAssistant && !searchStepsMap.has(lastAssistant.id)) {
              const replaySearchSteps = finalizeSearchSteps(
                replayEvents.reduce<WebSearchPhaseStep[]>((acc, event) => applyRunEventToWebSearchSteps(acc, event), []),
              )
              if (replaySearchSteps.length > 0) {
                searchStepsMap.set(lastAssistant.id, replaySearchSteps)
                writeMessageSearchSteps(lastAssistant.id, replaySearchSteps)
              }
            }
            if (lastAssistant && replayAssistantTurnNeeded) {
              const replayTurn = buildAssistantTurnFromRunEvents(replayEvents)
              if (replayTurn.segments.length > 0) {
                assistantTurnMap.set(lastAssistant.id, replayTurn)
                writeMessageAssistantTurn(lastAssistant.id, replayTurn)
              }
            }
            if (lastAssistant && (latest.status === 'completed' || latest.status === 'cancelled' || latest.status === 'interrupted')) {
              terminalStatusMap.set(lastAssistant.id, latest.status)
              writeMessageTerminalStatus(lastAssistant.id, latest.status)
            }
            if (lastAssistant && latest.status === 'failed') {
              terminalStatusMap.set(lastAssistant.id, 'failed')
              writeMessageTerminalStatus(lastAssistant.id, 'failed')
            }
          } catch {
            // 回放失败不影响主流程
          }
        }

        loadFromStorage(items.filter((msg) => msg.role === 'assistant').map((msg) => msg.id))
        const metaEntries = new Map<string, Partial<MessageMeta>>()
        const mergeMeta = (id: string, partial: Partial<MessageMeta>) => {
          const prev = metaEntries.get(id) ?? {}
          metaEntries.set(id, { ...prev, ...partial })
        }
        sourcesMap.forEach((sources, id) => mergeMeta(id, { sources }))
        artifactsMap.forEach((artifacts, id) => mergeMeta(id, { artifacts }))
        widgetsMap.forEach((widgets, id) => mergeMeta(id, { widgets }))
        codeExecMap.forEach((codeExecutions, id) => mergeMeta(id, { codeExecutions }))
        browserActionsMap.forEach((browserActions, id) => mergeMeta(id, { browserActions }))
        subAgentsMap.forEach((subAgents, id) => mergeMeta(id, { subAgents }))
        fileOpsMap.forEach((fileOps, id) => mergeMeta(id, { fileOps }))
        webFetchesMap.forEach((webFetches, id) => mergeMeta(id, { webFetches }))
        thinkingMap.forEach((thinking, id) => mergeMeta(id, { thinking }))
        searchStepsMap.forEach((searchSteps, id) => mergeMeta(id, { searchSteps }))
        assistantTurnMap.forEach((assistantTurn, id) => mergeMeta(id, { assistantTurn }))
        runEventsMap.forEach((runEvents, id) => mergeMeta(id, { runEvents }))
        setMetaBatch(Array.from(metaEntries.entries()))
        if (interruptedError) {
          setError(interruptedError)
        }
        if (failedError) {
          setError(failedError)
        }
        if (latest?.status === 'failed') {
          setTerminalRunDisplayId(latest.run_id)
          setTerminalRunHandoffStatus('failed')
        }

        const cachedThreadHandoff = readThreadRunHandoff(threadId)
        const shouldRestoreThreadHandoff =
          !!cachedThreadHandoff &&
          (!latest || latest.run_id === cachedThreadHandoff.runId) &&
          !findAssistantMessageForRun(items, cachedThreadHandoff.runId)
        if (
          cachedThreadHandoff &&
          shouldRestoreThreadHandoff
        ) {
          setPreserveLiveRunUi(true)
          setTerminalRunDisplayId(cachedThreadHandoff.runId)
          setTerminalRunHandoffStatus(cachedThreadHandoff.status)
          setLiveAssistantTurn(cachedThreadHandoff.assistantTurn ?? null)
          currentRunSourcesRef.current = [...cachedThreadHandoff.sources]
          currentRunArtifactsRef.current = [...cachedThreadHandoff.artifacts]
          currentRunCodeExecutionsRef.current = [...cachedThreadHandoff.codeExecutions]
          currentRunBrowserActionsRef.current = [...cachedThreadHandoff.browserActions]
          currentRunSubAgentsRef.current = [...cachedThreadHandoff.subAgents]
          currentRunFileOpsRef.current = [...cachedThreadHandoff.fileOps]
          currentRunWebFetchesRef.current = [...cachedThreadHandoff.webFetches]
          setTopLevelCodeExecutions(cachedThreadHandoff.codeExecutions)
          setTopLevelSubAgents(cachedThreadHandoff.subAgents)
          setTopLevelFileOps(cachedThreadHandoff.fileOps)
          setTopLevelWebFetches(cachedThreadHandoff.webFetches)
          const restoredStreamingArtifacts = buildStreamingArtifactsFromHandoff(cachedThreadHandoff)
          streamingArtifactsRef.current = restoredStreamingArtifacts
          setStreamingArtifacts(restoredStreamingArtifacts)
          searchStepsRef.current = cachedThreadHandoff.searchSteps
          setSearchSteps(cachedThreadHandoff.searchSteps)
        } else if (threadId) {
          clearThreadRunHandoff(threadId)
        }

        // 若 location state 已提供 initialRunId，优先使用（来自 WelcomePage 新建后导航）
        // 必须显式调用 setActiveRunId，因为 React Router 复用组件实例，useState 初始值不会重新求值
        if (
          locationState?.initialRunId &&
          (!latest || (latest.run_id === locationState.initialRunId && (latest.status === 'running' || latest.status === 'cancelling')))
        ) {
          setActiveRunId(locationState.initialRunId)
          setPendingThinking(true)
          const hints = t.copThinkingHints
          setThinkingHint(hints[Math.floor(Math.random() * hints.length)])
          if (threadId) onRunStarted(threadId)
        } else {
          const isActiveRun = !shouldRestoreThreadHandoff && (latest?.status === 'running' || latest?.status === 'cancelling')
          setActiveRunId(isActiveRun ? latest.run_id : null)
          if (isActiveRun && threadId) onRunStarted(threadId)
          else if (threadId) onRunEnded(threadId)
        }
      } catch (err) {
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
          return
        }
        setError(normalizeError(err))
      } finally {
        if (!disposed && isMessageSyncCurrent(syncVersion)) {
          if (
            !shouldSkipInitialSkeleton &&
            navUserEnterMessageId &&
            loadedItems &&
            loadedItems.some((m) => m.id === navUserEnterMessageId && m.role === 'user')
          ) {
            setUserEnterMessageId(navUserEnterMessageId)
          }
          setMessagesLoading(false)
          if (locationState?.userEnterMessageId || locationState?.welcomeUserMessage) {
            const rest: LocationState = { ...locationState }
            delete rest.userEnterMessageId
            delete rest.welcomeUserMessage
            queueMicrotask(() => {
              navigate('.', { replace: true, state: Object.keys(rest as object).length > 0 ? rest : undefined })
            })
          }
        }
      }
    })()
    return () => {
      disposed = true
    }
  // 只在 threadId 变化时重新加载，避免依赖 locationState 导致重复触发
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accessToken, threadId, loadFromStorage, setMetaBatch])

  // 切换 thread 时清理 SSE 和排队消息，并重置 pendingIncognito
  useEffect(() => {
    setActiveRunId(null)
    clearCompletedTitleTail()
    resetAssistantTurnLive()
    seenFirstToolCallInRunRef.current = false
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    setPendingThinking(false)
    setSegments([])
    activeSegmentIdRef.current = null
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    setCancelSubmitting(false)
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
    pendingMessageRef.current = null
    setQueuedDraft(null)
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    clearAllMeta()
    setSourcePanelMessageId(null)
    sse.disconnect()
    sse.clearEvents()
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    resetSearchSteps()
    // 不重置 processedEventCountRef: clearEvents 是异步的，若此处归零，
    // 同一 effects 阶段内事件处理 effect 会重放旧事件导致串线。
    // activeRunId effect 在新 run 启动时负责归零。
    setPendingIncognito(false)
    setDraft('')
    setAttachments((prev) => {
      prev.forEach((attachment) => {
        if (attachment.preview_url) URL.revokeObjectURL(attachment.preview_url)
      })
      return []
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threadId, clearCompletedTitleTail, resetAssistantTurnLive])

  // 连接 SSE
  useEffect(() => {
    if (!activeRunId) return
    if (threadId) {
      clearThreadRunHandoff(threadId)
    }
    clearCompletedTitleTail()
    freezeCutoffRef.current = null
    injectionBlockedRunIdRef.current = null
    setPreserveLiveRunUi(false)
    sseTerminalFallbackRunIdRef.current = activeRunId
    sseTerminalFallbackArmedRef.current = false
    seenFirstToolCallInRunRef.current = false
    sse.reset()
    sse.connect()
    processedEventCountRef.current = 0
    lastVisibleNonTerminalSeqRef.current = 0
    setPreserveLiveRunUi(false)
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    resetAssistantTurnLive()
    setSegments([])
    activeSegmentIdRef.current = null
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    setCancelSubmitting(false)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeRunId, baseUrl, clearCompletedTitleTail, resetAssistantTurnLive, threadId])

  useEffect(() => {
    if (!sseRunId) return
    sse.connect()
    return () => { sse.disconnect() }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sseRunId, baseUrl])

  useEffect(() => {
    if (!activeRunId) {
      lastVisibleNonTerminalSeqRef.current = 0
      return
    }
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
  }, [activeRunId])

  useEffect(() => {
    if (!activeRunId) return
    markTerminalRunHistory(null)
  }, [activeRunId, markTerminalRunHistory])

  // 避免上一轮 run 的 closed/error 状态误触发当前 run 的终端兜底。
  useEffect(() => {
    if (!activeRunId) {
      sseTerminalFallbackRunIdRef.current = null
      sseTerminalFallbackArmedRef.current = false
      return
    }
    if (
      sse.state === 'connecting' ||
      sse.state === 'connected' ||
      sse.state === 'reconnecting'
    ) {
      sseTerminalFallbackRunIdRef.current = activeRunId
      sseTerminalFallbackArmedRef.current = true
    }
  }, [activeRunId, sse.state])

  // 页面从后台回到前台时，若 SSE 已断开则重连
  useEffect(() => {
    const onVisibilityChange = () => {
      if (document.visibilityState !== 'visible') return
      if (!sseRunId) return
      const s = sse.state
      if (s === 'closed' || s === 'error' || s === 'idle') {
        sse.reconnect()
      }
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [sseRunId, sse.state, sse.reconnect]) // eslint-disable-line react-hooks/exhaustive-deps

  // 处理 SSE 事件
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
            // LLM summarization failed - show brief warning then clear
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
          const entry: CodeExecution = codeExecutionCall.appended
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
        // show_widget tool.call: mark streaming entry as complete
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
        // create_artifact tool.call: mark streaming entry as complete
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

      // Handle terminal output delta events for real-time streaming
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
        // 检测 sandbox 执行产物 + document_write / create_artifact 产物 + browser 产物
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
              // link artifact refs to streaming entries
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
            const target: CodeExecution = codeExecutionResult.updated
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
        // browser tool.result
        if (obj.tool_name === 'browser') {
          const browserResult = applyBrowserToolResult(currentRunBrowserActionsRef.current, event)
          if (browserResult.updated) {
            currentRunBrowserActionsRef.current = browserResult.nextActions
          }
        }
        // sub-agent tool.result
        const subAgentResult = applySubAgentToolResult(currentRunSubAgentsRef.current, event)
        if (subAgentResult.updated) {
          currentRunSubAgentsRef.current = subAgentResult.nextAgents
          setTopLevelSubAgents((prev) => {
            const idx = prev.findIndex((a) => a.id === subAgentResult.updated!.id)
            if (idx >= 0) return prev.map((a, i) => i === idx ? subAgentResult.updated! : a)
            return [...prev, subAgentResult.updated!]
          })
        }
        // file op tool.result
        const fileOpResult = applyFileOpToolResult(currentRunFileOpsRef.current, event)
        if (fileOpResult.updated) {
          currentRunFileOpsRef.current = fileOpResult.nextOps
          setTopLevelFileOps((prev) => {
            const idx = prev.findIndex((o) => o.id === fileOpResult.updated!.id)
            if (idx >= 0) return prev.map((o, i) => i === idx ? fileOpResult.updated! : o)
            return [...prev, fileOpResult.updated!]
          })
        }
        // web_fetch tool.result
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
          // 规范化 required 字段，防止 LLM 传非数组值导致前端崩溃
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
        if (runSearchSteps.length > 0) applySearchSteps(() => runSearchSteps)
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
              const pendingSearchSteps = pendingSearchStepsRef.current
              pendingSearchStepsRef.current = null
              persistRunDataToMessage(completedAssistant.id, {
                ...runCache,
                pendingSearchSteps,
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
          .catch(() => {})
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
            .catch(() => {})
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
          // 注入拦截：渲染在对话流中，不用底部 error card
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
            .catch(() => {})
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
          message: typeof obj?.message === 'string' ? obj.message : t.runInterrupted,
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
            .catch(() => {})
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

  // SSE 流结束但未收到终端事件时的兜底清理
  // 正常流程中 run.completed 会先 setActiveRunId(null)，所以此处不会触发
  // 仅在 SSE 异常断连（如网络中断达到重试上限）时才生效
  useEffect(() => {
    if (!activeRunId) return
    if (sse.state !== 'closed' && sse.state !== 'error') return
    if (!sseTerminalFallbackArmedRef.current) return
    if (sseTerminalFallbackRunIdRef.current !== activeRunId) return
    const terminalRunId = activeRunId

    // run.completed 等终端事件处理中会同步 setActiveRunId(null)，
    // React 批量更新后 activeRunId 已经为 null，不会到达此处。
    // 到达此处说明 SSE 关闭时确实没有处理过终端事件。
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
      .catch(() => {})
  }, [activeRunId, sse.state, persistRunDataToMessage, persistThreadRunHandoff]) // eslint-disable-line react-hooks/exhaustive-deps

  const {
    revokeDraftAttachment,
    handleAttachFiles,
    handlePasteContent,
    handleRemoveAttachment,
  } = useAttachmentActions()

  const handleSend = async (e: React.FormEvent<HTMLFormElement>, personaKey: string, modelOverride?: string) => {
    e.preventDefault()
    if (sending || !threadId) return
    markTerminalRunHistory(null)
    clearThreadRunHandoff(threadId)

    // streaming 期间排队，输出结束后自动发送
    if (isStreaming) {
      const text = draft.trim()
      if (text) {
        pendingMessageRef.current = text
        setQueuedDraft(text)
        setDraft('')
      }
      return
    }

    const text = draft.trim()
    if (!text && attachments.length === 0) return

    setSending(true)
    setPendingThinking(true)
    setThinkingHint(t.copThinkingHints[Math.floor(Math.random() * t.copThinkingHints.length)])
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    try {
      const uploadAttachments = async () => {
        return await Promise.all(
          attachments.map(async (attachment) => {
            if (attachment.uploaded) return attachment.uploaded
            return await uploadStagingAttachment(accessToken, attachment.file)
          }),
        )
      }

      // 首次在无痕模式下发送：先 fork 出一个 private thread，再在其中发送
      if (pendingIncognito && messages.length > 0) {
        const lastMessageId = messages[messages.length - 1].id
        const forked = await forkThread(accessToken, threadId, lastMessageId, true)
        if (forked.id_mapping) migrateMessageMetadata(forked.id_mapping)
        onThreadCreated(forked)
        const uploaded = await uploadAttachments()
        const forkUserMessage = await createMessage(accessToken, forked.id, buildMessageRequest(text, uploaded))
        const run = await createRun(accessToken, forked.id, personaKey, modelOverride, readThreadWorkFolder(threadId) ?? undefined)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(forked.id)
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setDraft('')
        setAttachments([])
        navigate(`/t/${forked.id}`, {
          state: {
            isIncognitoFork: true,
            initialRunId: run.run_id,
            forkBaseCount: messages.length,
            userEnterMessageId: forkUserMessage.id,
          },
          replace: false,
        })
        onRunStarted(forked.id)
        return
      }

      const replaceMessageId = replaceOnCancelRef.current
      replaceOnCancelRef.current = null

      if (replaceMessageId && attachments.length === 0) {
        // 取消后重发：替换上一条未响应的用户消息
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setDraft('')
        setAttachments([])
        injectionBlockedRunIdRef.current = null
        invalidateMessageSync()
        const originalMsg = messages.find((m) => m.id === replaceMessageId)
        const nonTextParts = originalMsg?.content_json?.parts?.filter((p) => p.type !== 'text') ?? []
        const replacedContentJson: MessageContent | undefined = originalMsg?.content_json
          ? { parts: [{ type: 'text', text }, ...nonTextParts] }
          : undefined
        setMessages((prev) => {
          const idx = prev.findIndex((m) => m.id === replaceMessageId)
          if (idx === -1) return prev
          return prev.slice(0, idx + 1).map((m, i) =>
            i === idx ? { ...m, content: text, content_json: replacedContentJson ?? m.content_json } : m,
          )
        })
        const run = await editMessage(accessToken, threadId, replaceMessageId, text, replacedContentJson)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
        noResponseMsgIdRef.current = replaceMessageId
        resetSearchSteps()
        setActiveRunId(run.run_id)
        onRunStarted(threadId)
        scrollToBottom()
      } else {
        const uploaded = await uploadAttachments()
        const message = await createMessage(accessToken, threadId, buildMessageRequest(text, uploaded))
        invalidateMessageSync()
        setUserEnterMessageId(message.id)
        setMessages((prev) => [...prev, message])
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setDraft('')
        setAttachments([])
        injectionBlockedRunIdRef.current = null
        noResponseMsgIdRef.current = message.id

        const run = await createRun(accessToken, threadId, personaKey, modelOverride, readThreadWorkFolder(threadId) ?? undefined)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
        resetSearchSteps()
        setActiveRunId(run.run_id)
        onRunStarted(threadId)
        scrollToBottom()
      }
    } catch (err) {
      setPendingThinking(false)
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }

  const actionLabelForTerminalRun = useCallback((params: {
    status: MessageTerminalStatusRef | null
    hasOutput: boolean
  }) => {
    if (isStreaming || sending) return undefined
    if (params.status === 'cancelled' || params.status === 'interrupted') {
      return params.hasOutput ? t.continueBtn : t.retryAction
    }
    if (params.status === 'failed') {
      return t.retryAction
    }
    return undefined
  }, [isStreaming, sending, t.continueBtn, t.retryAction])

  const actionHandlerForTerminalRun = useCallback((params: {
    runId: string | null | undefined
    status: MessageTerminalStatusRef | null
    hasOutput: boolean
  }) => {
    if (isStreaming || sending) return undefined
    if ((params.status === 'cancelled' || params.status === 'interrupted') && params.hasOutput && params.runId) {
      return () => void handleContinue(params.runId!)
    }
    if (params.status === 'cancelled' || params.status === 'interrupted' || params.status === 'failed') {
      return handleRetry
    }
    return undefined
  }, [isStreaming, sending, handleContinue, handleRetry])

  const terminalSseError = useMemo(() => {
    if (!sse.error) return null
    return normalizeError(sse.error)
  }, [sse.error])

  const lastUserMsgIdx = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'user') return i
    }
    return -1
  }, [messages])

  const lastTurnStartIdx = lastUserMsgIdx >= 0 ? lastUserMsgIdx : messages.length

  const clearUserEnterAnimation = useCallback(() => {
    setUserEnterMessageId(null)
  }, [])

  const resolvedMessageSources = useMemo(() => {
    return resolveMessageSourcesForRender(messages, messageSourcesMap)
  }, [messages, messageSourcesMap])

  const sourcePanelSources = sourcePanelMessageId ? resolvedMessageSources.get(sourcePanelMessageId) : undefined
  const sourcePanelUserQuery = useMemo(() => {
    if (!sourcePanelMessageId) return undefined
    const idx = messages.findIndex((m) => m.id === sourcePanelMessageId)
    for (let i = idx - 1; i >= 0; i--) {
      if (messages[i].role === 'user') return messages[i].content
    }
    return undefined
  }, [sourcePanelMessageId, messages])

  // 保留最近一次数据，使关闭时面板内容在过渡动画期间仍可见
  if (sourcePanelSources) lastPanelSourcesRef.current = sourcePanelSources
  if (sourcePanelUserQuery !== undefined) lastPanelQueryRef.current = sourcePanelUserQuery
  if (codePanelExecution) lastCodePanelRef.current = codePanelExecution
  if (documentPanelArtifact) lastDocumentPanelRef.current = documentPanelArtifact
  const panelDisplaySources = sourcePanelSources ?? lastPanelSourcesRef.current
  const panelDisplayQuery = sourcePanelUserQuery ?? lastPanelQueryRef.current
  const codePanelDisplay = codePanelExecution ?? lastCodePanelRef.current
  const documentPanelDisplay = documentPanelArtifact ?? lastDocumentPanelRef.current
  const isSourcePanelOpen = !!(sourcePanelSources && sourcePanelSources.length > 0)
  const isCodePanelOpen = !!codePanelExecution
  const isDocumentPanelOpen = !!documentPanelArtifact
  const isPanelOpen = isSourcePanelOpen || isCodePanelOpen || isDocumentPanelOpen

  const allReadFiles = useMemo(() => {
    const seen = new Set<string>()
    const result: string[] = []
    const addOps = (ops: FileOpRef[]) => {
      for (const op of ops) {
        if ((op.toolName === 'read_file' || op.toolName === 'read') && op.status === 'success' && op.label && op.label !== 'read file') {
          if (!seen.has(op.label)) { seen.add(op.label); result.push(op.label) }
        }
      }
    }
    for (const ops of messageFileOpsMap.values()) addOps(ops)
    addOps(topLevelFileOps)
    return result
  }, [messageFileOpsMap, topLevelFileOps])

  const openCodePanel = useCallback((ce: CodeExecution) => {
    if (codePanelExecution?.id === ce.id) {
      closePanel()
      return
    }
    openCodePanelState(ce)
  }, [closePanel, codePanelExecution?.id, openCodePanelState])

  const openDocumentPanel = useCallback((artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => {
    stabilizeDocumentPanelScroll(options?.trigger)
    if (documentPanelArtifact?.artifact.key === artifact.key) {
      closePanel()
      return
    }
    openDocumentPanelState({
      artifact,
      artifacts: options?.artifacts ?? [],
      runId: options?.runId,
    })
  }, [closePanel, documentPanelArtifact?.artifact.key, openDocumentPanelState, stabilizeDocumentPanelScroll])

  // COP step 计数：timeline 中所有非 finished 的点
  const dedupedTopLevelCodeExecutions = useMemo(() => {
    const lastIdxById = new Map<string, number>()
    topLevelCodeExecutions.forEach((ce, i) => lastIdxById.set(ce.id, i))
    return topLevelCodeExecutions.filter((ce, i) => lastIdxById.get(ce.id) === i)
  }, [topLevelCodeExecutions])

  const allStreamItems = useMemo(() => [
    ...dedupedTopLevelCodeExecutions.map(ce => ({ kind: 'code' as const, id: ce.id, seq: ce.seq ?? 0, item: ce })),
    ...topLevelSubAgents.map(a => ({ kind: 'agent' as const, id: a.id, seq: a.seq ?? 0, item: a })),
    ...topLevelFileOps.map(op => ({ kind: 'fileop' as const, id: op.id, seq: op.seq ?? 0, item: op })),
    ...topLevelWebFetches.map(wf => ({ kind: 'fetch' as const, id: wf.id, seq: wf.seq ?? 0, item: wf })),
  ].sort((a, b) => a.seq - b.seq), [dedupedTopLevelCodeExecutions, topLevelSubAgents, topLevelFileOps, topLevelWebFetches])

  const livePlacedShowWidgetCallIds = useMemo(() => liveCopShowWidgetCallIds(liveAssistantTurn), [liveAssistantTurn])
  const livePlacedCreateArtifactCallIds = useMemo(() => liveCopCreateArtifactCallIds(liveAssistantTurn), [liveAssistantTurn])
  const visibleStreamingWidgets = useMemo(
    () => streamingArtifacts.filter((e) => e.toolName === 'show_widget' && (
      (e.content != null && e.content.length > 0) ||
      (e.loadingMessages != null && e.loadingMessages.length > 0)
    ) && (!e.toolCallId || !livePlacedShowWidgetCallIds.has(e.toolCallId))),
    [streamingArtifacts, livePlacedShowWidgetCallIds],
  )
  const visibleStreamingArtifacts = useMemo(
    () => streamingArtifacts.filter((e) => e.toolName === 'create_artifact' && e.content && e.display !== 'panel' && (!e.toolCallId || !livePlacedCreateArtifactCallIds.has(e.toolCallId))),
    [streamingArtifacts, livePlacedCreateArtifactCallIds],
  )

  const copTimelineStreamHiddenIds = useMemo(() => {
    if (!liveAssistantTurn || liveAssistantTurn.segments.length === 0) return new Set<string>()
    return toolCallIdsInCopTimelines(liveAssistantTurn, {
      codeExecutions: dedupedTopLevelCodeExecutions,
      fileOps: topLevelFileOps,
      webFetches: topLevelWebFetches,
      subAgents: topLevelSubAgents,
      searchSteps,
      sources: currentRunSourcesRef.current,
    })
  }, [liveAssistantTurn, dedupedTopLevelCodeExecutions, topLevelFileOps, topLevelWebFetches, topLevelSubAgents, searchSteps])

  const allStreamItemsForUi = useMemo(() => {
    if (copTimelineStreamHiddenIds.size === 0) return allStreamItems
    return allStreamItems.filter((e) => !copTimelineStreamHiddenIds.has(e.id))
  }, [allStreamItems, copTimelineStreamHiddenIds])
  const terminalRunHasOutput = useMemo(() => {
    if ((liveAssistantTurn?.segments.length ?? 0) > 0 && assistantTurnHasVisibleOutput(liveAssistantTurn)) {
      return true
    }
    return segmentsRef.current.some((segment) => segment.content.trim() !== '')
  }, [liveAssistantTurn])

  const currentRunCopHeaderOverride = useCallback((params: {
    title?: string | null
    steps: WebSearchPhaseStep[]
    hasCodeExecutions: boolean
    hasSubAgents: boolean
    hasFileOps: boolean
    hasWebFetches: boolean
    hasGenericTools: boolean
    hasThinking: boolean
    handoffStatus?: 'completed' | 'cancelled' | 'interrupted' | 'failed' | null
  }): string | undefined => {
    return resolveCopHeaderOverride({
      ...params,
      labels: {
        stopped: t.connection.stopped,
        failed: t.failedRunRetryTitle,
        liveProgress: t.copTimelineLiveProgress,
        thinking: t.copThinkingInlineTitle,
      },
    })
  }, [t])
  const liveSegments = liveAssistantTurn?.segments ?? []
  const leadingLiveCop =
    liveSegments[0]?.type === 'cop'
      ? liveSegments[0]
      : null
  const trailingLiveSegments = leadingLiveCop ? liveSegments.slice(1) : liveSegments
  const renderLiveCopSegment = (
    seg: Extract<AssistantTurnSegment, { type: 'cop' }>,
    si: number,
    key?: string,
  ) => {
    const lastSegIdx = liveSegments.length - 1
    const preservingHandoffSegments = preserveLiveRunUi && !isStreaming
    const copClosedByFollowingSeg = si < lastSegIdx
    const copTimelineLive = liveRunUiActive && !copClosedByFollowingSeg
    const copTimelineComplete = !copTimelineLive
    const payload = copTimelinePayloadForSegment(seg, {
      codeExecutions: dedupedTopLevelCodeExecutions,
      fileOps: topLevelFileOps,
      webFetches: topLevelWebFetches,
      subAgents: topLevelSubAgents,
      searchSteps,
      sources: currentRunSourcesRef.current,
    })
    const liveWidgets = liveStreamingWidgetEntriesForCop(seg, streamingArtifacts)
    const liveArts = liveInlineArtifactEntriesForCop(seg, streamingArtifacts)
    const thinkingRowsLive = !isSearchThread
      ? thinkingRowsForCop(seg, {
          live: liveRunUiActive,
          segmentIndex: si,
          lastSegmentIndex: lastSegIdx,
        })
      : []
    const copInlineLive = !isSearchThread
      ? copInlineTextRowsForCop(seg, {
          live: liveRunUiActive,
          segmentIndex: si,
          lastSegmentIndex: lastSegIdx,
        })
      : []
    if (
      copSegmentCalls(seg).length === 0 &&
      thinkingRowsLive.length === 0 &&
      copInlineLive.length === 0 &&
      liveWidgets.length === 0 &&
      liveArts.length === 0
    ) {
      return null
    }
    const timelineTitleOverride =
      preservingHandoffSegments
        ? currentRunCopHeaderOverride({
            title: seg.title,
            steps: payload.steps,
            hasCodeExecutions: !!(payload.codeExecutions && payload.codeExecutions.length > 0),
            hasSubAgents: !!(payload.subAgents && payload.subAgents.length > 0),
            hasFileOps: !!(payload.fileOps && payload.fileOps.length > 0),
            hasWebFetches: !!(payload.webFetches && payload.webFetches.length > 0),
            hasGenericTools: !!(payload.genericTools && payload.genericTools.length > 0),
            hasThinking: thinkingRowsLive.length > 0 || copInlineLive.length > 0,
            handoffStatus: terminalRunHandoffStatus,
          })
        : seg.title?.trim() || undefined
    const trailSeg = si + 1 <= lastSegIdx ? liveSegments[si + 1] : undefined
    const trailingAssistantTextPresent =
      trailSeg?.type === 'text' && trailSeg.content.length > 0
    return (
      <Fragment key={key ?? `live-cop-${si}`}>
        <CopTimeline
          key={si === 0 ? 'cop-leading-inner' : undefined}
          steps={payload.steps}
          sources={payload.sources}
          isComplete={copTimelineComplete}
          codeExecutions={payload.codeExecutions}
          onOpenCodeExecution={openCodePanel}
          activeCodeExecutionId={codePanelExecution?.id}
          subAgents={payload.subAgents}
          fileOps={payload.fileOps}
          webFetches={payload.webFetches}
          genericTools={payload.genericTools}
          headerOverride={timelineTitleOverride}
          thinkingRows={thinkingRowsLive.length > 0 ? thinkingRowsLive : undefined}
          copInlineTextRows={copInlineLive.length > 0 ? copInlineLive : undefined}
          shimmer={copTimelineLive}
          live={copTimelineLive}
          preserveExpanded={preservingHandoffSegments}
          trailingAssistantTextPresent={trailingAssistantTextPresent}
          thinkingHint={thinkingHint}
          accessToken={accessToken}
          baseUrl={baseUrl}
        />
        {liveWidgets.map((entry) => (
          <WidgetBlock
            key={`live-w-${entry.toolCallId ?? entry.toolCallIndex}`}
            html={entry.content ?? ''}
            title={entry.title ?? 'Widget'}
            complete={entry.complete}
            loadingMessages={entry.loadingMessages}
            onAction={handleArtifactAction}
          />
        ))}
        {liveArts.map((entry) => (
          <ArtifactStreamBlock
            key={`live-art-${entry.toolCallId ?? entry.toolCallIndex}`}
            entry={entry}
            accessToken={accessToken}
            onAction={handleArtifactAction}
          />
        ))}
      </Fragment>
    )
  }

  return (
    <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-[var(--c-bg-page)]">
      <ChatTitleMenu />

      {/* 主体区域：消息 + 输入 + 可选的 sources 侧边面板 */}
      <div className="relative flex flex-1 min-h-0">
        <div className="relative flex flex-1 min-w-0 flex-col">
          <div className="pointer-events-none absolute inset-x-0 top-0 z-10 h-10" style={{ background: 'linear-gradient(to bottom, var(--c-bg-page), transparent)' }} />
          {/* 消息列表 */}
          <div
            ref={scrollContainerRef}
            onScroll={handleScrollContainerScroll}
            className="chat-scroll-hidden relative flex-1 min-h-0 overflow-y-auto bg-[var(--c-bg-page)] [scrollbar-gutter:stable]"
          >
        <div
          style={{ maxWidth: 800, margin: '0 auto', padding: `50px ${isPanelOpen ? chatContentPadding.panelOpen : chatContentPadding.panelClosed} 200px` }}
          className="flex w-full flex-col gap-6"
        >
          {messagesLoading ? (
            <ChatSkeleton />
          ) : (
            <>
              {contextCompactBar && (
                <ContextCompactBar
                  variant={contextCompactBar}
                  runningLabel={t.desktopSettings.chatCompactBannerRunning}
                  doneLabel={t.desktopSettings.chatCompactBannerDone}
                  trimLabel={t.desktopSettings.chatCompactBannerTrim}
                  llmFailedLabel={t.desktopSettings.chatCompactBannerLlmFailed}
                />
              )}
              <MessageList
                lastTurnStartIdx={lastTurnStartIdx}
                lastTurnRef={lastUserMsgRef}
                lastTurnChildren={
                  <LiveRunPane
                    showPendingThinkingShell={showPendingThinkingShell}
                    preserveLiveRunUi={preserveLiveRunUi}
                    leadingLiveCop={leadingLiveCop}
                    trailingLiveSegments={trailingLiveSegments}
                    liveSegments={liveSegments}
                    liveRunUiActive={liveRunUiActive}
                    liveRunUiVisible={liveRunUiVisible}
                    liveAssistantTurn={liveAssistantTurn}
                    allStreamItemsForUi={allStreamItemsForUi}
                    dedupedTopLevelCodeExecutions={dedupedTopLevelCodeExecutions}
                    topLevelSubAgents={topLevelSubAgents}
                    topLevelFileOps={topLevelFileOps}
                    topLevelWebFetches={topLevelWebFetches}
                    codePanelExecutionId={codePanelExecution?.id}
                    currentRunSources={currentRunSourcesRef.current}
                    currentRunArtifacts={currentRunArtifactsRef.current}
                    activeRunId={activeRunId}
                    accessToken={accessToken}
                    baseUrl={baseUrl}
                    thinkingHint={thinkingHint}
                    visibleStreamingWidgets={visibleStreamingWidgets}
                    visibleStreamingArtifacts={visibleStreamingArtifacts}
                    injectionBlocked={injectionBlocked}
                    awaitingInput={awaitingInput}
                    checkInDraft={checkInDraft}
                    checkInSubmitting={checkInSubmitting}
                    onCheckInDraftChange={setCheckInDraft}
                    onCheckInSubmit={() => void handleCheckInSubmit()}
                    terminalSseError={terminalSseError}
                    pendingIncognito={pendingIncognito}
                    incognitoDividerText={t.incognitoForkDivider}
                    onIncognitoDividerComplete={() => {
                      if (isAtBottomRef.current) {
                        bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
                      }
                    }}
                    terminalRunHandoffStatus={terminalRunHandoffStatus}
                    terminalRunDisplayId={terminalRunDisplayId}
                    terminalRunHasOutput={terminalRunHasOutput}
                    failedRunRetryTitle={t.failedRunRetryTitle}
                    runInterruptedLabel={t.runInterrupted}
                    runCancelledLabel={t.runCancelled}
                    actionLabelForTerminalRun={actionLabelForTerminalRun}
                    actionHandlerForTerminalRun={actionHandlerForTerminalRun}
                    onOpenDocument={openDocumentPanel}
                    onOpenCodeExecution={openCodePanel}
                    onArtifactAction={handleArtifactAction}
                    renderLiveCopSegment={renderLiveCopSegment}
                    bottomRef={bottomRef}
                    messages={messages}
                  />
                }
                showRunEvents={showRunEvents}
                currentRunCopHeaderOverride={currentRunCopHeaderOverride}
                actionLabelForTerminalRun={actionLabelForTerminalRun}
                actionHandlerForTerminalRun={actionHandlerForTerminalRun}
                handleRetry={handleRetry}
                handleEditMessage={handleEditMessage}
                handleFork={handleFork}
                handleArtifactAction={handleArtifactAction}
                openDocumentPanel={openDocumentPanel}
                openCodePanel={openCodePanel}
                sourcePanelMessageId={sourcePanelMessageId}
                setRunDetailPanelRunId={setRunDetailPanelRunId}
                clearUserEnterAnimation={clearUserEnterAnimation}
              />

            </>
          )}
        </div>
      </div>

      {/* 输入区域 */}
      <div
        ref={inputAreaRef}
        style={{ maxWidth: 1200, margin: '0 auto', padding: `12px ${appMode === 'work' ? chatInputPadding.work : isPanelOpen ? chatInputPadding.panelOpen : chatInputPadding.panelClosed} ${appMode === 'work' ? '22px' : '8px'}`, position: 'absolute', bottom: 0, left: 0, right: 0, zIndex: 10, background: 'linear-gradient(to bottom, transparent 0%, var(--c-bg-page) 24px)' }}
        className="flex w-full flex-col items-center gap-2"
      >
        {/* 滚动到底部按钮：始终锚定在输入框顶边正上方 */}
        <button
          onClick={scrollToBottom}
          style={{
            position: 'absolute',
            top: 0,
            left: '50%',
            transform: 'translate(-50%, calc(-100% - 8px))',
            zIndex: 1,
            opacity: isAtBottom ? 0 : 1,
            pointerEvents: isAtBottom ? 'none' : 'auto',
            transition: 'opacity 200ms ease',
            width: 36,
            height: 36,
            borderRadius: '50%',
            border: '0.5px solid var(--c-border)',
            background: 'var(--c-bg-sidebar)',
            color: 'var(--c-text-secondary)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'pointer',
          }}
        >
          <ArrowDown size={16} className={liveRunUiActive && !isAtBottom ? 'arrow-breathe' : ''} />
        </button>
        {queuedDraft && (
          <div
            className="flex w-full max-w-[756px] items-center gap-2 rounded-xl px-3 py-2"
            style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <span
              className="flex-1 truncate text-sm"
              style={{ color: 'var(--c-text-secondary)' }}
            >
              {queuedDraft}
            </span>
            <button
              type="button"
              onClick={() => { pendingMessageRef.current = null; setQueuedDraft(null) }}
              className="flex items-center justify-center rounded opacity-70 transition-opacity hover:opacity-100"
              style={{ color: 'var(--c-text-muted)' }}
            >
              <X size={12} />
            </button>
          </div>
        )}
        {pendingUserInput ? (
          <motion.div
            key="user-input-card"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
            transition={{ duration: 0.25, ease: 'easeOut' }}
            className="w-full max-w-[840px] px-4"
          >
            <UserInputCard
              key={pendingUserInput.request_id}
              request={pendingUserInput}
              onSubmit={handleUserInputSubmit}
              onDismiss={handleUserInputDismiss}
              disabled={!activeRunId}
            />
          </motion.div>
        ) : (
          <ChatInput
              value={draft}
              onChange={setDraft}
              onSubmit={handleSend}
              onCancel={handleCancel}
              placeholder={t.replyPlaceholder}
              disabled={sending}
              isStreaming={isStreaming}
              canCancel={canCancel}
            cancelSubmitting={cancelSubmitting}
            attachments={attachments}
            onAttachFiles={handleAttachFiles}
            onPasteContent={handlePasteContent}
            onRemoveAttachment={handleRemoveAttachment}
            accessToken={accessToken}
            onAsrError={handleAsrError}
            searchMode={isSearchThread}
            onPersonaChange={(personaKey) => setIsSearchThread(personaKey === SEARCH_PERSONA_KEY)}
            onOpenSettings={onOpenSettings}
            appMode={appMode}
            hasMessages={messages.length > 0}
            workThreadId={threadId}
            />
        )}
        <p style={{ color: 'var(--c-text-muted)', fontSize: '11px', letterSpacing: '-0.3px', textAlign: 'center', marginBottom: 0, marginTop: '-2px' }}>
          Arkloop is AI and can make mistakes. Please double-check responses.
        </p>
      </div>

        </div>
        {/* 右侧面板：flex 兄弟节点；chat 模式下用 motion 驱动 width，避免嵌套 flex + CSS transition 偶发不插值 */}
        {appMode === 'work' ? (
          <div
            style={{
              width: '300px',
              flexShrink: 0,
              overflow: 'hidden',
            }}
          >
            <WorkRightPanel
              accessToken={accessToken}
              projectId={currentThread?.project_id || undefined}
              steps={workTodos.map((td) => ({
                id: td.id,
                label: td.content,
                status: td.status === 'completed' ? 'done' : td.status === 'in_progress' ? 'active' : 'pending',
              }))}
              onForbidden={() => setAppMode('chat')}
              readFiles={allReadFiles}
              threadId={threadId}
            />
          </div>
        ) : (
          <motion.div
            className="flex-shrink-0 overflow-hidden"
            initial={false}
            animate={{
              width: isDocumentPanelOpen
                ? documentPanelWidth
                : (isSourcePanelOpen || isCodePanelOpen)
                  ? sidePanelWidth
                  : 0,
            }}
            transition={{ duration: 0.28, ease: [0.16, 1, 0.3, 1] }}
            style={{
              borderLeft: (panelDisplaySources || codePanelDisplay || documentPanelDisplay)
                ? '0.5px solid var(--c-border-subtle)'
                : 'none',
            }}
          >
            {isSourcePanelOpen && panelDisplaySources && panelDisplaySources.length > 0 && (
              <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
                <SourcesPanel
                  sources={panelDisplaySources}
                  userQuery={panelDisplayQuery}
                  onClose={() => setSourcePanelMessageId(null)}
                />
              </div>
            )}
            {isCodePanelOpen && codePanelDisplay && (
              <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
                <CodeExecutionPanel
                  execution={codePanelDisplay}
                  onClose={() => setCodePanelExecution(null)}
                />
              </div>
            )}
            {isDocumentPanelOpen && documentPanelDisplay && (
              <div style={{ width: `${documentPanelWidth}px`, height: '100%', contain: 'layout style' }}>
                <DocumentPanel
                  artifact={documentPanelDisplay.artifact}
                  artifacts={documentPanelDisplay.artifacts}
                  accessToken={accessToken}
                  runId={documentPanelDisplay.runId}
                  onClose={() => {
                    stabilizeDocumentPanelScroll()
                    setDocumentPanelArtifact(null)
                  }}
                />
              </div>
            )}
          </motion.div>
        )}
      </div>

      {threadId && (
        <ShareModal
          accessToken={accessToken}
          threadId={threadId}
          open={shareModalOpen}
          onClose={() => closeShareModal()}
        />
      )}

      {runDetailPanelRunId && (
        <RunDetailPanel
          runId={runDetailPanelRunId}
          accessToken={accessToken}
          onClose={() => setRunDetailPanelRunId(null)}
        />
      )}

      {showDebugPanel && <DebugTrigger />}
    </div>
  )
}
