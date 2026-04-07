import React, { useState, useEffect, useRef, useCallback, useMemo, memo, Fragment, type ComponentProps } from 'react'
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
import { ArtifactStreamBlock, type StreamingArtifactEntry } from './ArtifactStreamBlock'
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
import {
  buildMessageCodeExecutionsFromRunEvents,
  buildMessageWidgetsFromRunEvents,
  findAssistantMessageForRun,
  shouldRefetchCompletedRunMessages,
  shouldReplayMessageCodeExecutions,
  buildMessageBrowserActionsFromRunEvents,
  buildMessageSubAgentsFromRunEvents,
  buildMessageFileOpsFromRunEvents,
  buildMessageWebFetchesFromRunEvents,
} from '../runEventProcessing'
import {
  buildAssistantTurnFromRunEvents,
  copSegmentCalls,
  type AssistantTurnSegment,
  type AssistantTurnUi,
} from '../assistantTurnSegments'
import { copTimelinePayloadForSegment, toolCallIdsInCopTimelines } from '../copSegmentTimeline'
import { applyRunEventToWebSearchSteps } from '../webSearchTimelineFromRunEvent'
import { useLocale } from '../contexts/LocaleContext'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useAppModeUI, useSettingsUI } from '../contexts/app-ui'
import { useChatSession } from '../contexts/chat-session'
import { useMessageStore } from '../contexts/message-store'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useMessageMeta, type MessageMeta } from '../contexts/message-meta'
import { useStream } from '../contexts/stream'
import { usePanels } from '../contexts/panels'
import { useScrollPin, SCROLL_BOTTOM_PAD } from '../hooks/useScrollPin'
import { useDevTools } from '../hooks/useDevTools'
import { useChatActions } from '../hooks/useChatActions'
import { useThreadSseEffect } from '../hooks/useThreadSseEffect'
import { useAttachmentActions } from '../hooks/useAttachmentActions'
import { useMessageMetaCompat } from '../hooks/useMessageMetaCompat'
import { useRunTransition } from '../hooks/useRunTransition'
import {
  normalizeError,
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
import { ChatSkeleton } from './ChatSkeleton'
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

const LiveRunPane = memo(function LiveRunPane({
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
})

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
  } = useThreadList()
  const { appMode, setAppMode } = useAppModeUI()
  const { openSettings: onOpenSettings } = useSettingsUI()
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
    beginMessageSync,
    isMessageSyncCurrent,
    invalidateMessageSync,
    readConsistentMessages,
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
    terminalRunDisplayId,
    setTerminalRunDisplayId,
    terminalRunHandoffStatus,
    setTerminalRunHandoffStatus,
    markTerminalRunHistory: markTerminalRunHistoryState,
    clearCompletedTitleTail: clearCompletedTitleTailState,
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
  } = useMessageMeta()
  const {
    liveAssistantTurn,
    setLiveAssistantTurn,
    preserveLiveRunUi,
    setPreserveLiveRunUi,
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

  // --- Work todo 进度 ---
  const { showRunEvents, showDebugPanel, runDetailPanelRunId, setRunDetailPanelRunId } = useDevTools()

  const markTerminalRunHistory = useCallback((messageId: string | null, expanded = true) => {
    markTerminalRunHistoryState(messageId, expanded)
  }, [markTerminalRunHistoryState])
  const resetSearchSteps = useCallback(() => {
    resetSearchStepsState()
  }, [resetSearchStepsState])
  const clearQueuedDraft = useCallback(() => {
    pendingMessageRef.current = null
    setPreserveLiveRunUi(false)
    setQueuedDraft(null)
  }, [])
  const clearCompletedTitleTail = useCallback(() => {
    clearCompletedTitleTailState()
  }, [clearCompletedTitleTailState])
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
    programmaticScrollDepthRef,
    handleScrollContainerScroll,
    stabilizeDocumentPanelScroll,
    scrollToBottom,
    activateAnchor,
    spacerRef,
  } = useScrollPin({
    messagesLoading,
    messages,
    liveAssistantTurn,
    liveRunUiVisible,
    topLevelCodeExecutionsLength: topLevelCodeExecutions.length,
  })

  const { resetAssistantTurnLive } = useRunTransition({ forceInstantBottomScrollRef, lastUserMsgRef, programmaticScrollDepthRef })

  useThreadSseEffect({
    restoreQueuedDraftToInput,
    clearQueuedDraft,
    forceInstantBottomScrollRef,
    lastUserMsgRef,
    programmaticScrollDepthRef,
  })

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
  } = useChatActions({ scrollToBottom: activateAnchor })
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

  const draftRef = useRef(draft)
  draftRef.current = draft
  const attachmentsRef = useRef(attachments)
  attachmentsRef.current = attachments
  const pendingIncognitoRef = useRef(pendingIncognito)
  pendingIncognitoRef.current = pendingIncognito
  const messagesRef = useRef(messages)
  messagesRef.current = messages

  const {
    revokeDraftAttachment,
    handleAttachFiles,
    handlePasteContent,
    handleRemoveAttachment,
  } = useAttachmentActions()

  const handleSend = useCallback(async (e: React.FormEvent<HTMLFormElement>, personaKey: string, modelOverride?: string) => {
    e.preventDefault()
    if (sending || !threadId) return
    markTerminalRunHistory(null)
    clearThreadRunHandoff(threadId)

    const draft = draftRef.current
    const attachments = attachmentsRef.current
    const pendingIncognito = pendingIncognitoRef.current
    const messages = messagesRef.current

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
        activateAnchor()
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
        activateAnchor()
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
  }, [
    accessToken,
    invalidateMessageSync,
    isStreaming,
    markTerminalRunHistory,
    navigate,
    onLoggedOut,
    onRunStarted,
    onThreadCreated,
    resetSearchSteps,
    revokeDraftAttachment,
    activateAnchor,
    sending,
    setActiveRunId,
    setAttachments,
    setDraft,
    setError,
    setInjectionBlocked,
    setMessages,
    setQueuedDraft,
    setSending,
    setPendingThinking,
    setThinkingHint,
    setUserEnterMessageId,
    t.copThinkingHints,
    threadId,
  ])

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

  const sourcesPanelContent = useMemo(() => {
    if (!isSourcePanelOpen || !panelDisplaySources || panelDisplaySources.length === 0) return null
    return (
      <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
        <SourcesPanel
          sources={panelDisplaySources}
          userQuery={panelDisplayQuery}
          onClose={() => setSourcePanelMessageId(null)}
        />
      </div>
    )
  }, [isSourcePanelOpen, panelDisplaySources, panelDisplayQuery, setSourcePanelMessageId])

  const codePanelContent = useMemo(() => {
    if (!isCodePanelOpen || !codePanelDisplay) return null
    return (
      <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
        <CodeExecutionPanel
          execution={codePanelDisplay}
          onClose={() => setCodePanelExecution(null)}
        />
      </div>
    )
  }, [isCodePanelOpen, codePanelDisplay, setCodePanelExecution])

  const documentPanelContent = useMemo(() => {
    if (!isDocumentPanelOpen || !documentPanelDisplay) return null
    return (
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
    )
  }, [isDocumentPanelOpen, documentPanelDisplay, accessToken, stabilizeDocumentPanelScroll, setDocumentPanelArtifact])

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

  const shareModalEl = useMemo(() => {
    if (!threadId) return null
    return (
      <ShareModal
        accessToken={accessToken}
        threadId={threadId}
        open={shareModalOpen}
        onClose={() => closeShareModal()}
      />
    )
  }, [threadId, accessToken, shareModalOpen, closeShareModal])

  const runDetailEl = useMemo(() => {
    if (!runDetailPanelRunId) return null
    return (
      <RunDetailPanel
        runId={runDetailPanelRunId}
        accessToken={accessToken}
        onClose={() => setRunDetailPanelRunId(null)}
      />
    )
  }, [runDetailPanelRunId, accessToken, setRunDetailPanelRunId])

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
  const handlePersonaChange = useCallback((personaKey: string) => {
    setIsSearchThread(personaKey === SEARCH_PERSONA_KEY)
  }, [])

  const hasMessages = messages.length > 0

  const chatInputEl = useMemo(() => (
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
      onPersonaChange={handlePersonaChange}
      onOpenSettings={onOpenSettings}
      appMode={appMode}
      hasMessages={hasMessages}
      workThreadId={threadId}
    />
  ), [draft, attachments, sending, isStreaming, canCancel, cancelSubmitting, appMode, isSearchThread, hasMessages, threadId, accessToken, t.replyPlaceholder, handleSend, handleCancel, handleAttachFiles, handlePasteContent, handleRemoveAttachment, handleAsrError, handlePersonaChange, onOpenSettings, setDraft])

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
          style={{ maxWidth: 800, margin: '0 auto', padding: `50px ${isPanelOpen ? chatContentPadding.panelOpen : chatContentPadding.panelClosed} ${SCROLL_BOTTOM_PAD}px` }}
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
                        activateAnchor()
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
        <div ref={spacerRef} style={{ flexShrink: 0, overflowAnchor: 'none' }} />
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
          chatInputEl
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
            {sourcesPanelContent}
            {codePanelContent}
            {documentPanelContent}
          </motion.div>
        )}
      </div>

      {shareModalEl}
      {runDetailEl}
      {showDebugPanel && <DebugTrigger />}
    </div>
  )
}
