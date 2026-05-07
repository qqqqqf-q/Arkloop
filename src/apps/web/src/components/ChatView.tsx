import React, { useState, useEffect, useRef, useCallback, useMemo, useSyncExternalStore, memo, Fragment, type ComponentProps } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowDown, ArrowUpFromLine, ChevronDown, ChevronRight, CornerDownLeft, Pencil, Trash2 } from 'lucide-react'
import { AutoResizeTextarea, DebugTrigger } from '@arkloop/shared'
import { ChatInput, type Attachment, type ChatInputHandle } from './ChatInput'
import { RunDetailPanel } from './RunDetailPanel'
import type { CodeExecution } from './CodeExecutionCard'
import {
  CopTimeline,
  type WebSearchPhaseStep,
} from './CopTimeline'
import { CopTimelineLocalExpansionProvider } from './cop-timeline/ToolRows'
import { MarkdownRenderer } from './MarkdownRenderer'
import { recordPerfCount, recordPerfValue } from '../perfDebug'
import { noteShowWidgetStatus } from '../streamDebug'
import { useTypewriter } from '../hooks/useTypewriter'
import { ArtifactStreamBlock, type StreamingArtifactEntry } from './ArtifactStreamBlock'
import { WidgetBlock } from './WidgetBlock'
import UserInputCard from './UserInputCard'
import { resolveMessageSourcesForRender } from './chatSourceResolver'
import { RunErrorNotice, type AppError } from './ErrorCallout'
import { ShareModal } from './ShareModal'
import { SourcesPanel } from './SourcesPanel'
import { CodeExecutionPanel } from './CodeExecutionPanel'
import { DocumentPanel } from './DocumentPanel'
import { AgentPanel } from './AgentPanel'
import { ChatTitleMenu } from './ChatTitleMenu'
import { MessageList } from './MessageList'
import { CopSegmentBlocks } from './CopSegmentBlocks'
import { TopLevelCopToolBlock } from './TopLevelCopToolBlock'
import { ContextCompactBar } from './ContextCompactBar'
import { IncognitoDivider } from './IncognitoDivider'
import { AssistantActionBar } from './messagebubble/AssistantMessage'
import {
  buildMessageArtifactsFromAgentEvents,
  buildMessageCodeExecutionsFromAgentEvents,
  buildMessageWidgetsFromAgentEvents,
  findAssistantMessageForRun,
  shouldRefetchCompletedRunMessages,
  shouldReplayMessageCodeExecutions,
  buildMessageBrowserActionsFromAgentEvents,
  buildMessageSubAgentsFromAgentEvents,
  buildMessageFileOpsFromAgentEvents,
  buildMessageWebFetchesFromAgentEvents,
  buildMessageThinkingFromAgentEvents,
  buildTodosFromAgentEvents,
} from '../agentEventProcessing'
import { getThreadTodos, setThreadTodos, clearThreadTodos } from '../todoDb'
import {
  buildAssistantTurnFromAgentEvents,
  copSegmentCalls,
  createEmptyAssistantTurnFoldState,
  type AssistantTurnSegment,
  type AssistantTurnUi,
} from '../assistantTurnSegments'
import { copTimelinePayloadForSegment, toolCallIdsInCopTimelines } from '../copSegmentTimeline'
import { buildResolvedPool, EMPTY_POOL, buildFallbackSegments } from '../copSubSegment'
import { applyAgentEventToWebSearchSteps } from '../webSearchTimelineFromAgentEvent'
import { useLocale } from '../contexts/LocaleContext'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useAppModeUI, useSettingsUI } from '../contexts/app-ui'
import { useChatSession } from '../contexts/chat-session'
import { useMessageStore } from '../contexts/message-store'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useMessageMeta, type MessageMeta } from '../contexts/message-meta'
import { useStream, useStreamingContent } from '../contexts/stream'
import { usePanels } from '../contexts/panels'
import { useScrollPin } from '../hooks/useScrollPin'
import { useDevTools } from '../hooks/useDevTools'
import { useChatActions } from '../hooks/useChatActions'
import { useThreadSseEffect } from '../hooks/useThreadSseEffect'
import { useAttachmentActions } from '../hooks/useAttachmentActions'
import { useMessageMetaCompat } from '../hooks/useMessageMetaCompat'
import { useRunTransition } from '../hooks/useRunTransition'
import {
  normalizeError,
  interruptedErrorFromAgentEvents,
  failedErrorFromAgentEvents,
  hasRecoverableRunOutput,
  finalizeSearchSteps,
  patchLegacySearchSteps,
  liveTurnHasThinkingSegment,
  buildStreamingArtifactsFromHandoff,
  resolveCopHeaderOverride,
} from '../lib/chat-helpers'
import { apiBaseUrl } from '@arkloop/shared/api'
import { ChatSkeleton } from './ChatSkeleton'
import { buildDraftAttachmentRecords, restoreAttachmentFromDraftRecord } from '../draftAttachments'
import {
  forkThread,
  getThread,
  listThreadRuns,
  updateThreadCollaborationMode,
  updateThreadLearningMode,
  uploadStagingAttachment,
  isApiError,
  type CollaborationMode,
  type RunReasoningMode,
  type UploadedThreadAttachment,
} from '../api'
import { readAgentUIEvents, type AgentMessage, useAgentClient } from '../agent-ui'
import { buildMessageRequest } from '../messageContent'
import { createQueuedPrompt, type QueuedPrompt } from '../queuedPrompts'
import {
  addSearchThreadId,
  SEARCH_PERSONA_KEY,
  type InputDraftScope,
  isSearchThreadId,
  readThreadRunHandoff,
  clearThreadRunHandoff,
  readMessageSources,
  writeMessageSources,
  readMessageArtifacts,
  writeMessageArtifacts,
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
  readMessageCoveredRunIds,
  readMessageWidgets,
  writeMessageWidgets,
  type WidgetRef,
  migrateMessageMetadata,
  readMessageAgentEvents,
  writeMessageAgentEvents,
  type MessageAgentEvent,
  readInputDraftAttachments,
  readThreadWorkFolder,
  readThreadReasoningMode,
  writeInputDraftAttachments,
  readRunThinkingHint,
  writeRunThinkingHint,
} from '../storage'

const sidePanelWidth = 360
const documentPanelWidth = 560
const chatContentPadding = { panelClosed: '60px', panelOpen: '40px' } as const
const chatInputPadding = { panelClosed: '60px', panelOpen: '40px', work: '14px' } as const

function errorNoticeKey(error: AppError): string {
  let details = ''
  try {
    details = JSON.stringify(error.details ?? null)
  } catch {
    details = String(error.details ?? '')
  }
  return [error.message ?? '', error.code ?? '', error.traceId ?? '', details].join('\u001f')
}

function isInterruptedRunStatus(status: string | null | undefined): boolean {
  return status === 'cancelled' || status === 'interrupted' || status === 'failed'
}

function chooseThinkingHint(hints: readonly string[]): string {
  if (hints.length === 0) return ''
  return hints[Math.floor(Math.random() * hints.length)] ?? hints[0] ?? ''
}

function isSameDraftDomain(left: InputDraftScope | null, right: InputDraftScope): boolean {
  if (!left) return false
  return left.page === right.page
    && (left.threadId ?? null) === (right.threadId ?? null)
    && left.appMode === right.appMode
    && !!left.searchMode === !!right.searchMode
}

type LocationState = {
  initialRunId?: string
  isSearch?: boolean
  isIncognitoFork?: boolean
  forkBaseCount?: number
  userEnterMessageId?: string
  welcomeUserMessage?: AgentMessage
} | null

type DocumentPanelState = {
  artifact: ArtifactRef
  artifacts: ArtifactRef[]
  runId?: string
}

type LiveRunPaneProps = {
  isWorkMode: boolean
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
  activeSegmentId: string | null
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
  pendingIncognito: boolean
  incognitoDividerText: string
  onIncognitoDividerComplete: () => void
  terminalRunHandoffStatus: LiveRunHandoffStatus
  terminalRunDisplayId: string | null
  showRunDetailButton: boolean
  setRunDetailPanelRunId: (runId: string | null) => void
  onOpenDocument: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  onOpenCodeExecution: (ce: CodeExecution) => void
  onOpenSubAgent: (agent: SubAgentRef) => void
  onArtifactAction: ComponentProps<typeof WidgetBlock>['onAction']
  renderLiveCopItems: (seg: CopSegment, si: number) => React.ReactNode[]
  renderLiveCopSegment: (seg: CopSegment, si: number, key?: string) => React.ReactNode
  bottomRef: React.RefObject<HTMLDivElement | null>
}

const LiveRunPane = memo(function LiveRunPane({
  isWorkMode,
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
  activeSegmentId,
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
  pendingIncognito,
  incognitoDividerText,
  onIncognitoDividerComplete,
  terminalRunHandoffStatus,
  terminalRunDisplayId,
  showRunDetailButton,
  setRunDetailPanelRunId,
  onOpenDocument,
  onOpenCodeExecution,
  onOpenSubAgent,
  onArtifactAction,
  renderLiveCopItems,
  renderLiveCopSegment,
  bottomRef,
}: LiveRunPaneProps) {
  const terminalActionText = assistantTurnActionText(liveAssistantTurn)
  const terminalActionRunId = terminalRunDisplayId ?? activeRunId
  const terminalActionStatus =
    terminalRunHandoffStatus === 'completed' ||
    terminalRunHandoffStatus === 'cancelled' ||
    terminalRunHandoffStatus === 'failed' ||
    terminalRunHandoffStatus === 'interrupted'
  const terminalActionHasContent =
    terminalActionText.trim() !== '' ||
    (liveAssistantTurn?.segments.length ?? 0) > 0 ||
    allStreamItemsForUi.length > 0 ||
    dedupedTopLevelCodeExecutions.length > 0 ||
    topLevelSubAgents.length > 0 ||
    topLevelFileOps.length > 0 ||
    topLevelWebFetches.length > 0 ||
    visibleStreamingWidgets.length > 0 ||
    visibleStreamingArtifacts.length > 0
  const liveContentMaxWidth = isWorkMode ? undefined : '663px'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
      {(showPendingThinkingShell || liveSegments.length > 0) && (
        <div data-testid={preserveLiveRunUi ? 'current-run-handoff' : undefined} style={{ display: 'flex', flexDirection: 'column', gap: 0, maxWidth: liveContentMaxWidth }}>
          {(showPendingThinkingShell || leadingLiveCop) && (
            <Fragment key="cop-leading">
              {leadingLiveCop
                ? renderLiveCopItems(leadingLiveCop, 0)
                : [
                    <CopTimeline
                      key="cop-leading-inner"
                      segments={[]}
                      pool={EMPTY_POOL}
                      isComplete={false}
                      live
                      shimmer
                      thinkingHint={thinkingHint}
                      accessToken={accessToken}
                      baseUrl={baseUrl}
                      typography={isWorkMode ? 'work' : 'default'}
                    />,
                  ]}
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
                streamSegmentId={!mdTypewriterDone ? activeSegmentId : null}
                webSources={currentRunSources.length > 0 ? currentRunSources : undefined}
                artifacts={currentRunArtifacts.length > 0 ? currentRunArtifacts : undefined}
                accessToken={accessToken}
                runId={activeRunId ?? undefined}
                onOpenDocument={onOpenDocument}
                typography={isWorkMode ? 'work' : 'default'}
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

      {terminalActionStatus && terminalActionHasContent && (
        <div style={{ maxWidth: liveContentMaxWidth }}>
          <AssistantActionBar
            textToCopy={terminalActionText}
            onViewRunDetail={showRunDetailButton && terminalActionRunId ? () => setRunDetailPanelRunId(terminalActionRunId) : undefined}
            isLast
          />
        </div>
      )}

      {!liveRunUiVisible &&
        liveAssistantTurn == null &&
        allStreamItemsForUi.length === 0 &&
        (dedupedTopLevelCodeExecutions.length > 0 || topLevelSubAgents.length > 0 || topLevelFileOps.length > 0 || topLevelWebFetches.length > 0) && (
        <div style={{ maxWidth: liveContentMaxWidth }}>
          {dedupedTopLevelCodeExecutions.map((ce) => (
            <TopLevelCopToolBlock
              key={`fallback-code-${ce.id}`}
              entry={{ kind: 'code', id: ce.id, seq: ce.seq ?? 0, item: ce }}
              onOpenCodeExecution={onOpenCodeExecution}
              activeCodeExecutionId={codePanelExecutionId ?? undefined}
            />
          ))}
          {(topLevelSubAgents.length > 0 || topLevelFileOps.length > 0 || topLevelWebFetches.length > 0) && (
            <CopTimeline
              segments={buildFallbackSegments({
                subAgents: topLevelSubAgents,
                fileOps: topLevelFileOps,
                webFetches: topLevelWebFetches,
              })}
              pool={buildResolvedPool({
                steps: [],
                sources: [],
                subAgents: topLevelSubAgents,
                fileOps: topLevelFileOps,
                webFetches: topLevelWebFetches,
              })}
              isComplete
              onOpenSubAgent={onOpenSubAgent}
              accessToken={accessToken}
              baseUrl={baseUrl}
              typography={isWorkMode ? 'work' : 'default'}
            />
          )}
        </div>
      )}

      {visibleStreamingWidgets.map((entry) => (
        <WidgetBlock
          key={`streaming-widget-${entry.toolCallIndex}`}
          html={entry.content ?? ''}
          title={entry.title ?? 'Widget'}
          complete={entry.complete}
          loadingMessages={entry.loadingMessages}
          compact
          onAction={onArtifactAction}
        />
      ))}

      {visibleStreamingArtifacts.map((entry) => (
        <ArtifactStreamBlock
          key={`streaming-artifact-${entry.toolCallIndex}`}
          entry={entry}
          accessToken={accessToken}
          compact
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

      {pendingIncognito && (
        <IncognitoDivider
          text={incognitoDividerText}
          onComplete={onIncognitoDividerComplete}
        />
      )}

      {/* reserve action bar height to prevent text from touching input area during streaming */}
      <div style={{ height: '36px' }} />
      <div ref={bottomRef} />
    </div>
  )
})

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>
type LiveRunHandoffStatus = 'running' | MessageTerminalStatusRef | null

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

function assistantTurnActionText(turn: AssistantTurnUi | null): string {
  if (!turn) return ''
  const parts: string[] = []
  for (const segment of turn.segments) {
    if (segment.type === 'text') {
      const text = segment.content.trim()
      if (text) parts.push(text)
      continue
    }
    for (const item of segment.items) {
      if (item.kind === 'thinking' || item.kind === 'assistant_text') {
        const text = item.content.trim()
        if (text) parts.push(text)
        continue
      }
      const label = item.call.displayDescription?.trim() || item.call.toolName.trim()
      if (label) parts.push(label)
    }
  }
  return parts.join('\n')
}

function LiveTurnMarkdown({
  content,
  typewriterDone,
  streamSegmentId,
  ...rest
}: {
  content: string
  typewriterDone: boolean
  streamSegmentId?: string | null
} & Omit<ComponentProps<typeof MarkdownRenderer>, 'content'>) {
  const streamingContent = useStreamingContent(streamSegmentId)
  const targetContent = streamSegmentId ? (streamingContent || content) : content
  const displayed = useTypewriter(targetContent, typewriterDone)
  useEffect(() => {
    recordPerfCount('live_turn_markdown_render', 1, {
      contentLength: targetContent.length,
      displayedLength: displayed.length,
      typewriterDone,
    })
    recordPerfValue('live_turn_markdown_displayed', displayed.length, 'chars', {
      contentLength: targetContent.length,
      typewriterDone,
    })
  }, [targetContent.length, displayed.length, typewriterDone])
  return <MarkdownRenderer content={displayed} streaming={!typewriterDone} {...rest} />
}

const ScrollToBottomButton = memo(function ScrollToBottomButton({
  onScrollToBottom,
  liveRunUiActive,
  subscribeIsAtBottom,
  getIsAtBottomSnapshot,
}: {
  onScrollToBottom: () => void
  liveRunUiActive: boolean
  subscribeIsAtBottom: (listener: () => void) => () => void
  getIsAtBottomSnapshot: () => boolean
}) {
  const isAtBottom = useSyncExternalStore(
    subscribeIsAtBottom,
    getIsAtBottomSnapshot,
    getIsAtBottomSnapshot,
  )

  return (
    <button
      onClick={onScrollToBottom}
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
  )
})

type QueuedPromptNoticeProps = {
  items: QueuedPrompt[]
  editingId: string | null
  activeRunId: string | null
  onEdit: (item: QueuedPrompt) => void
  onSendNow: (item: QueuedPrompt) => void
  onDelete: (item: QueuedPrompt) => void
}

function QueuedPromptNotice({
  items,
  editingId,
  activeRunId,
  onEdit,
  onSendNow,
  onDelete,
}: QueuedPromptNoticeProps) {
  const [expanded, setExpanded] = useState(true)
  if (items.length === 0) return null
  return (
    <div
      className="chat-input-box overflow-hidden"
      style={{
        width: '100%',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: 'var(--c-input-border-color-focus)',
        borderRadius: '12px',
        background: 'var(--c-bg-input)',
        boxShadow: 'var(--c-input-shadow-focus)',
      }}
    >
      <div
        className="flex items-center justify-between px-4 py-2"
        style={{
          color: 'var(--c-text-secondary)',
          fontSize: '14px',
          lineHeight: '1.4',
        }}
      >
        <div className="flex items-center gap-2">
          <span style={{ fontWeight: 460, color: 'var(--c-text-primary)' }}>
            {items.length} Queued
          </span>
          <span className="inline-flex items-center gap-1" style={{ color: 'var(--c-text-muted)' }}>
            <CornerDownLeft size={14} />
            <span>to Send</span>
          </span>
        </div>
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="flex items-center justify-center rounded-md p-1.5 transition-colors hover:bg-[var(--c-bg-sub)]"
          style={{ color: 'var(--c-text-muted)' }}
        >
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </button>
      </div>
      <div
        style={{
          display: 'grid',
          gridTemplateRows: expanded ? '1fr' : '0fr',
          transition: 'grid-template-rows 0.25s cubic-bezier(0.4, 0, 0.2, 1)',
        }}
      >
        <div style={{ minHeight: 0, overflow: 'hidden' }}>
          <div className="px-2 pb-2">
            {items.map((item) => {
            const isEditing = item.id === editingId
            const sendNowDisabled = !!activeRunId && item.attachments.length > 0
            return (
              <div
                key={item.id}
                className="group flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-[var(--c-bg-sub)]"
                style={{ color: 'var(--c-text-primary)', transition: 'background-color 0.15s ease' }}
              >
                <div
                  className="min-w-0 flex-1 whitespace-pre-wrap break-words text-[14px] leading-[1.4]"
                  style={{ fontWeight: 340 }}
                >
                  {item.text || item.attachments.map((attachment) => attachment.filename).join(', ')}
                </div>
                {isEditing ? (
                  <span
                    className="shrink-0 text-sm flex items-center"
                    style={{ color: 'var(--c-text-muted)', fontWeight: 400, height: '22px' }}
                  >
                    Editing
                  </span>
                ) : (
                  <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity duration-150 group-hover:opacity-100">
                    <button
                      type="button"
                      aria-label="Edit queued prompt"
                      title="Edit"
                      onClick={() => onEdit(item)}
                      className="flex items-center justify-center rounded-md transition-colors hover:bg-[var(--c-bg-deep)]"
                      style={{ color: 'var(--c-text-secondary)', width: '22px', height: '22px' }}
                    >
                      <Pencil size={13} />
                    </button>
                    <button
                      type="button"
                      aria-label="Send queued prompt now"
                      title={sendNowDisabled ? 'Attachments can be sent after this run finishes' : 'Send now'}
                      onClick={() => onSendNow(item)}
                      disabled={sendNowDisabled}
                      className="flex items-center justify-center rounded-md transition-colors hover:bg-[var(--c-bg-deep)] disabled:cursor-not-allowed disabled:opacity-35"
                      style={{ color: 'var(--c-text-primary)', width: '22px', height: '22px' }}
                    >
                      <ArrowUpFromLine size={14} />
                    </button>
                    <button
                      type="button"
                      aria-label="Delete queued prompt"
                      title="Delete"
                      onClick={() => onDelete(item)}
                      className="flex items-center justify-center rounded-md transition-colors hover:bg-[var(--c-bg-deep)]"
                      style={{ color: 'var(--c-text-secondary)', width: '22px', height: '22px' }}
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                )}
              </div>
            )
          })}
          </div>
        </div>
      </div>
    </div>
  )
}

export const ChatView = memo(function ChatView() {
  const { accessToken, logout: onLoggedOut, me } = useAuth()
  const {
    threads, addThread: onThreadCreated,
    upsertThread: onThreadUpserted,
    markRunning: onRunStarted, markIdle: onRunEnded,
    completedUnreadThreadIds,
    markCompletionRead,
  } = useThreadList()
  const { appMode } = useAppModeUI()
  const { openSettings: onOpenSettings } = useSettingsUI()
  const { threadId } = useChatSession()
  const currentThread = useMemo(
    () => threads.find((thread) => thread.id === threadId) ?? null,
    [threadId, threads],
  )
  const resolveThreadWorkFolder = useCallback((id: string): string | undefined => {
    const thread = threads.find((item) => item.id === id)
    return thread?.sidebar_work_folder ?? readThreadWorkFolder(id) ?? undefined
  }, [threads])
  const effectiveAppMode = currentThread?.mode === 'work' ? 'work' : currentThread?.mode === 'chat' ? 'chat' : appMode
  const isWorkMode = effectiveAppMode === 'work'
  useEffect(() => {
    if (!threadId || !completedUnreadThreadIds.has(threadId)) return
    markCompletionRead(threadId)
  }, [threadId, completedUnreadThreadIds, markCompletionRead])
  const planModeUpdateRef = useRef<Promise<void> | null>(null)
  const planModeRequestSeqRef = useRef(0)
  const learningModeUpdateRef = useRef<Promise<void> | null>(null)
  const learningModeRequestSeqRef = useRef(0)
  const [learningModeUpdating, setLearningModeUpdating] = useState(false)
  const waitForThreadModeUpdates = useCallback(async () => {
    const pending = [planModeUpdateRef.current, learningModeUpdateRef.current].filter((item): item is Promise<void> => !!item)
    if (pending.length > 0) await Promise.all(pending)
  }, [])
  const {
    messages,
    setMessages,
    messagesLoading,
    setMessagesLoading,
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
    error,
    setError,
    injectionBlocked,
    setInjectionBlocked,
    injectionBlockedRunIdRef,
    queuedPrompts,
    setQueuedPrompts,
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
    setTerminalRunCoveredRunIds,
    markTerminalRunHistory: markTerminalRunHistoryState,
    clearCompletedTitleTail: clearCompletedTitleTailState,
    sse,
    isStreaming,
    processedEventCountRef,
    freezeCutoffRef,
    lastVisibleNonTerminalSeqRef,
    sseTerminalFallbackRunIdRef,
    sseTerminalFallbackArmedRef,
    noResponseMsgIdRef,
    seenFirstToolCallInRunRef,
  } = useRunLifecycle()
  const {
    setMetaBatch,
    primeMetaBatch,
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
    assistantTurnFoldStateRef,
    resetSearchSteps: resetSearchStepsState,
    streamingArtifacts,
    setStreamingArtifacts,
    streamingArtifactsRef,
    setSegments,
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
    setWorkTodos,
  } = useStream()
  const {
    activePanel,
    shareModal,
    openSourcePanel,
    openCodePanel: openCodePanelState,
    openDocumentPanel: openDocumentPanelState,
    openAgentPanel: openAgentPanelState,
    closePanel,
    closeShareModal,
  } = usePanels()
  const threadsRef = useRef(threads)
  useEffect(() => { threadsRef.current = threads }, [threads])
  const location = useLocation()
  const locationState = location.state as LocationState
  const navigate = useNavigate()
  const { t } = useLocale()
  const agentClient = useAgentClient()
  const welcomeUserMessage = locationState?.welcomeUserMessage
  const shouldSkipInitialSkeleton = !!(
    welcomeUserMessage &&
    locationState?.userEnterMessageId === welcomeUserMessage.id &&
    welcomeUserMessage.role === 'user'
  )

  const baseUrl = apiBaseUrl()

  const [isSearchThread, setIsSearchThread] = useState(
    () => locationState?.isSearch === true || isSearchThreadId(threadId ?? ''),
  )
  const [dismissedInputErrorKey, setDismissedInputErrorKey] = useState<string | null>(null)

  const { messageSourcesMap } = useMessageMetaCompat()

  const shareModalOpen = shareModal.open
  const sourcePanelMessageId = activePanel?.type === 'source' ? activePanel.messageId : null
  const codePanelExecution = activePanel?.type === 'code' ? activePanel.execution : null
  const documentPanelArtifact = activePanel?.type === 'document' ? activePanel.artifact : null
  const agentPanelAgent = activePanel?.type === 'agent' ? activePanel.agent : null
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
  const lastAgentPanelRef = useRef<SubAgentRef | null>(null)
  // 关闭动画期间保留上一次的数据
  const lastPanelSourcesRef = useRef<WebSource[] | undefined>(undefined)
  const lastPanelQueryRef = useRef<string | undefined>(undefined)

  // --- Work todo 进度 ---
  const { showRunDetailButton, showDebugPanel, runDetailPanelRunId, setRunDetailPanelRunId } = useDevTools()

  const markTerminalRunHistory = useCallback((messageId: string | null, expanded = true) => {
    markTerminalRunHistoryState(messageId, expanded)
  }, [markTerminalRunHistoryState])
  const resetSearchSteps = useCallback(() => {
    resetSearchStepsState()
  }, [resetSearchStepsState])
  const drainQueuedPromptRef = useRef<(() => void) | null>(null)
  const drainForcedQueuedPromptRef = useRef<((terminal: { runId: string; status: 'completed' | 'cancelled' | 'failed' | 'interrupted' }) => boolean) | null>(null)
  const forcedQueuedPromptRef = useRef<{ prompt: QueuedPrompt; resumeFromRunId: string } | null>(null)
  const queuedPromptsRef = useRef<QueuedPrompt[]>(queuedPrompts)
  queuedPromptsRef.current = queuedPrompts
  const [editingQueuedPromptId, setEditingQueuedPromptId] = useState<string | null>(null)
  const queuedEditPreviousDraftRef = useRef('')

  const clearCompletedTitleTail = useCallback(() => {
    clearCompletedTitleTailState()
  }, [clearCompletedTitleTailState])

  const liveRunUiVisible = isStreaming || preserveLiveRunUi
  const liveRunUiActive =
    isStreaming ||
    (
      preserveLiveRunUi &&
      terminalRunHandoffStatus !== 'completed' &&
      terminalRunHandoffStatus !== 'cancelled' &&
      terminalRunHandoffStatus !== 'failed' &&
      terminalRunHandoffStatus !== 'interrupted'
    )
  const showPendingThinkingShell =
    pendingThinking &&
    !liveTurnHasThinkingSegment(liveAssistantTurn) &&
    (sending || activeRunId != null)

  const {
    bottomRef,
    scrollContainerRef,
    lastUserMsgRef,
    lastUserPromptRef,
    inputAreaRef,
    forceInstantBottomScrollRef,
    isAtBottomRef,
    handleScrollContainerScroll,
    stabilizeDocumentPanelScroll,
    scrollToBottom,
    activateAnchor,
    spacerRef,
    subscribeIsAtBottom,
    getIsAtBottomSnapshot,
  } = useScrollPin({
    messagesLoading,
    messages,
    liveAssistantTurn,
    liveRunUiVisible,
    topLevelCodeExecutionsLength: topLevelCodeExecutions.length,
    promptPinningDisabled: isWorkMode,
  })

  const { resetAssistantTurnLive, captureTerminalRunCache, persistThreadRunHandoff } = useRunTransition()

  useThreadSseEffect({ drainQueuedPromptRef, drainForcedQueuedPromptRef })

  const prevActiveRunIdRef = useRef<string | null>(null)
  useEffect(() => {
    if (activeRunId && activeRunId !== prevActiveRunIdRef.current) {
      setWorkTodos([])
      if (threadId) clearThreadTodos(threadId).catch(() => {})
    }
    prevActiveRunIdRef.current = activeRunId
  }, [activeRunId, threadId])

  useEffect(() => {
    if (!threadId || !activeRunId || !preserveLiveRunUi) return
    if (terminalRunDisplayId !== activeRunId) return
    persistThreadRunHandoff(activeRunId, captureTerminalRunCache('running'))
  }, [
    activeRunId,
    captureTerminalRunCache,
    liveAssistantTurn,
    persistThreadRunHandoff,
    preserveLiveRunUi,
    searchSteps,
    streamingArtifacts,
    terminalRunDisplayId,
    threadId,
    topLevelCodeExecutions,
    topLevelFileOps,
    topLevelSubAgents,
    topLevelWebFetches,
  ])

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
    handleRetryUserMessage,
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
      setUserEnterMessageId(welcomeUserMessage.id)
      setMessagesLoading(false)
    }
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    const navUserEnterMessageId = locationState?.userEnterMessageId

    void (async () => {
      getThreadTodos(threadId).then((cached) => {
        if (cached.length > 0 && !disposed) setWorkTodos(cached)
      })
      let loadedItems: AgentMessage[] | null = null
      try {
        const [thread, initialItems, runs] = await Promise.all([
          getThread(accessToken, threadId),
          agentClient.listMessages(threadId),
          listThreadRuns(accessToken, threadId, 1),
        ])
        if (disposed || !isMessageSyncCurrent(syncVersion)) return
        onThreadUpserted(thread)

        const latest = runs[0]
        let items = initialItems
        if (shouldRefetchCompletedRunMessages({ messages: initialItems, latestRun: latest })) {
          const refreshedItems = await agentClient.listMessages(threadId)
          items = findAssistantMessageForRun(refreshedItems, latest.run_id) != null
            ? refreshedItems
            : initialItems
        }
        if (disposed || !isMessageSyncCurrent(syncVersion)) return

        loadedItems = items
        setMessages(items)
        let interruptedError = latest?.status === 'interrupted'
          ? { message: t.runInterrupted }
          : null
        let failedError = latest?.status === 'failed'
          ? { message: t.failedRunTitle }
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
        const failedErrorMap = new Map<string, AppError>()

        const agentEventsMap = new Map<string, MessageAgentEvent[]>()
        const assistantTurnMap = new Map<string, AssistantTurnUi>()
        const metaEntries = new Map<string, Partial<MessageMeta>>()
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
          const cachedCoveredRunIds = readMessageCoveredRunIds(msg.id)
          if (cachedCoveredRunIds && cachedCoveredRunIds.length > 0) {
            metaEntries.set(msg.id, { coveredRunIds: cachedCoveredRunIds })
          }

          const cachedAgentEvents = readMessageAgentEvents(msg.id)
          let hydratedAssistantTurn: AssistantTurnUi | null = null
          if (cachedAgentEvents) {
            agentEventsMap.set(msg.id, cachedAgentEvents)
            if (latest?.status === 'interrupted' && latest.run_id && msg.streamId === latest.run_id) {
              interruptedError = interruptedErrorFromAgentEvents(cachedAgentEvents, t.runInterrupted)
            }
            if (latest?.status === 'failed' && latest.run_id && msg.streamId === latest.run_id) {
              failedError = failedErrorFromAgentEvents(cachedAgentEvents, t.failedRunTitle)
              if (failedError) {
                failedErrorMap.set(msg.id, failedError)
              }
            }
            const rebuiltTurn = buildAssistantTurnFromAgentEvents(cachedAgentEvents)
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
        const latestAssistant = latest ? findAssistantMessageForRun(items, latest.run_id) : null
        const lastAssistant = latest ? latestAssistant : [...items].reverse().find((m) => m.role === 'assistant')
        const replayWidgetsNeeded = !!(lastAssistant && !widgetsMap.has(lastAssistant.id))
        const replayCodeExecNeeded = !!(lastAssistant && shouldReplayMessageCodeExecutions(codeExecMap.get(lastAssistant.id)))
        const replayBrowserActionsNeeded = !!(lastAssistant && !browserActionsMap.has(lastAssistant.id))
        const replaySubAgentsNeeded = !!(lastAssistant && !subAgentsMap.has(lastAssistant.id))
        const replayFileOpsNeeded = !!(lastAssistant && !fileOpsMap.has(lastAssistant.id))
        const replayWebFetchesNeeded = !!(lastAssistant && !webFetchesMap.has(lastAssistant.id))
        const replaySearchStepsNeeded = !!(lastAssistant && !searchStepsMap.has(lastAssistant.id))
        const replayAssistantTurnNeeded = !!(lastAssistant && !assistantTurnMap.has(lastAssistant.id))
        const shouldReplayLatestRun =
          !!latest &&
          latest.status !== 'running' &&
          (
            latest.status === 'interrupted' ||
            latest.status === 'failed' ||
            latest.status === 'cancelled' ||
            latest.status === 'completed' ||
            (lastAssistant != null && (
              replayWidgetsNeeded ||
              replayCodeExecNeeded ||
              replayBrowserActionsNeeded ||
              replaySubAgentsNeeded ||
              replayFileOpsNeeded ||
              replayWebFetchesNeeded ||
              replaySearchStepsNeeded ||
              replayAssistantTurnNeeded ||
              true // always replay to restore todos
            ))
          )
        let replayThreadHandoff: ReturnType<typeof readThreadRunHandoff> = null
        if (shouldReplayLatestRun && latest) {
          try {
            const replayEvents = await readAgentUIEvents(
              agentClient.openMessageChunkStream(latest.run_id, { live: false }),
            )
            const replayArtifacts = buildMessageArtifactsFromAgentEvents(replayEvents)
            const replayWidgets = buildMessageWidgetsFromAgentEvents(replayEvents)
            const replayExecs = buildMessageCodeExecutionsFromAgentEvents(replayEvents)
            const replayBrowserActions = buildMessageBrowserActionsFromAgentEvents(replayEvents)
            const replayAgents = buildMessageSubAgentsFromAgentEvents(replayEvents)
            const replayFileOps = buildMessageFileOpsFromAgentEvents(replayEvents)
            const replayWebFetches = buildMessageWebFetchesFromAgentEvents(replayEvents)
            const replaySearchSteps = finalizeSearchSteps(
              replayEvents.reduce<WebSearchPhaseStep[]>((acc, event) => applyAgentEventToWebSearchSteps(acc, event), []),
            )
            const replayThinking = buildMessageThinkingFromAgentEvents(replayEvents)
            let replayTurn = buildAssistantTurnFromAgentEvents(replayEvents)
            if (latest.status === 'interrupted') {
              interruptedError = interruptedErrorFromAgentEvents(replayEvents, t.runInterrupted)
            }
            if (latest.status === 'failed') {
              failedError = failedErrorFromAgentEvents(replayEvents, t.failedRunTitle)
              if (failedError && lastAssistant) {
                failedErrorMap.set(lastAssistant.id, failedError)
              }
            }
            if (lastAssistant && !artifactsMap.has(lastAssistant.id)) {
              if (replayArtifacts.length > 0) {
                artifactsMap.set(lastAssistant.id, replayArtifacts)
                writeMessageArtifacts(lastAssistant.id, replayArtifacts)
              }
            }
            if (lastAssistant && replayWidgetsNeeded) {
              if (replayWidgets.length > 0) {
                widgetsMap.set(lastAssistant.id, replayWidgets)
                writeMessageWidgets(lastAssistant.id, replayWidgets)
              }
            }
            if (lastAssistant && replayCodeExecNeeded) {
              codeExecMap.set(lastAssistant.id, replayExecs)
              writeMessageCodeExecutions(lastAssistant.id, replayExecs)
            }
            if (lastAssistant && replayBrowserActionsNeeded) {
              if (replayBrowserActions.length > 0) {
                browserActionsMap.set(lastAssistant.id, replayBrowserActions)
                writeMessageBrowserActions(lastAssistant.id, replayBrowserActions)
              }
            }
            if (lastAssistant && replaySubAgentsNeeded) {
              if (replayAgents.length > 0) {
                subAgentsMap.set(lastAssistant.id, replayAgents)
                writeMessageSubAgents(lastAssistant.id, replayAgents)
              }
            }
            if (lastAssistant && replayFileOpsNeeded) {
              if (replayFileOps.length > 0) {
                fileOpsMap.set(lastAssistant.id, replayFileOps)
                writeMessageFileOps(lastAssistant.id, replayFileOps)
              }
            }
            if (lastAssistant && replayWebFetchesNeeded) {
              if (replayWebFetches.length > 0) {
                webFetchesMap.set(lastAssistant.id, replayWebFetches)
                writeMessageWebFetches(lastAssistant.id, replayWebFetches)
              }
            }
            if (lastAssistant && !searchStepsMap.has(lastAssistant.id)) {
              if (replaySearchSteps.length > 0) {
                searchStepsMap.set(lastAssistant.id, replaySearchSteps)
                writeMessageSearchSteps(lastAssistant.id, replaySearchSteps)
              }
            }
            if (lastAssistant && !sourcesMap.has(lastAssistant.id)) {
              const replaySources = replaySearchSteps.flatMap((step) => step.sources ?? [])
              if (replaySources.length > 0) {
                sourcesMap.set(lastAssistant.id, replaySources)
                writeMessageSources(lastAssistant.id, replaySources)
              }
            }
            if (lastAssistant) {
              agentEventsMap.set(lastAssistant.id, replayEvents)
              writeMessageAgentEvents(lastAssistant.id, replayEvents)
            }
            if (lastAssistant && replayAssistantTurnNeeded) {
              replayTurn = buildAssistantTurnFromAgentEvents(replayEvents)
              if (replayTurn.segments.length > 0) {
                assistantTurnMap.set(lastAssistant.id, replayTurn)
                writeMessageAssistantTurn(lastAssistant.id, replayTurn)
              }
            }
            if (
              latest.run_id &&
              !lastAssistant &&
              hasRecoverableRunOutput({
                assistantTurn: replayTurn,
                thinking: replayThinking,
                searchSteps: replaySearchSteps,
                widgets: replayWidgets,
                codeExecutions: replayExecs,
                subAgents: replayAgents,
                fileOps: replayFileOps,
                webFetches: replayWebFetches,
              })
            ) {
              replayThreadHandoff = {
                runId: latest.run_id,
                status: latest.status === 'completed' ? 'completed' : latest.status === 'cancelled' ? 'cancelled' : latest.status === 'interrupted' ? 'interrupted' : 'failed',
                coveredRunIds: [],
                assistantTurn: replayTurn.segments.length > 0 ? replayTurn : null,
                sources: replaySearchSteps.flatMap((step) => step.sources ?? []),
                artifacts: replayArtifacts,
                widgets: replayWidgets,
                codeExecutions: replayExecs,
                browserActions: replayBrowserActions,
                subAgents: replayAgents,
                fileOps: replayFileOps,
                webFetches: replayWebFetches,
                searchSteps: replaySearchSteps,
              }
            }
            const replayedTodos = buildTodosFromAgentEvents(replayEvents)
            if (replayedTodos.length > 0) {
              setWorkTodos(replayedTodos)
              setThreadTodos(threadId, replayedTodos).catch(() => {})
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
        agentEventsMap.forEach((agentEvents, id) => mergeMeta(id, { agentEvents }))
        failedErrorMap.forEach((failedError, id) => mergeMeta(id, { failedError }))
        const metaBatch = Array.from(metaEntries.entries())
        primeMetaBatch(metaBatch)
        setMetaBatch(metaBatch)
        if (metaBatch.length > 0) {
          queueMicrotask(() => setMessages((prev) => [...prev]))
        }
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

        const restoreThreadHandoffUi = (
          handoff: NonNullable<ReturnType<typeof readThreadRunHandoff>>,
          options?: { displayRunId?: string; status?: LiveRunHandoffStatus },
        ) => {
          const displayRunId = options?.displayRunId ?? handoff.runId
          const displayStatus = options?.status ?? handoff.status
          setPreserveLiveRunUi(true)
          setTerminalRunDisplayId(displayRunId)
          setTerminalRunHandoffStatus(displayStatus)
          setTerminalRunCoveredRunIds(handoff.coveredRunIds)
          assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
          assistantTurnFoldStateRef.current.segments = (handoff.assistantTurn?.segments ?? []).map((segment) => structuredClone(segment))
          setLiveAssistantTurn(handoff.assistantTurn ?? null)
          setSegments([])
          activeSegmentIdRef.current = null
          currentRunSourcesRef.current = [...handoff.sources]
          currentRunArtifactsRef.current = [...handoff.artifacts]
          currentRunCodeExecutionsRef.current = [...handoff.codeExecutions]
          currentRunBrowserActionsRef.current = [...handoff.browserActions]
          currentRunSubAgentsRef.current = [...handoff.subAgents]
          currentRunFileOpsRef.current = [...handoff.fileOps]
          currentRunWebFetchesRef.current = [...handoff.webFetches]
          setTopLevelCodeExecutions(handoff.codeExecutions)
          setTopLevelSubAgents(handoff.subAgents)
          setTopLevelFileOps(handoff.fileOps)
          setTopLevelWebFetches(handoff.webFetches)
          const restoredStreamingArtifacts = buildStreamingArtifactsFromHandoff(handoff)
          streamingArtifactsRef.current = restoredStreamingArtifacts
          setStreamingArtifacts(restoredStreamingArtifacts)
          searchStepsRef.current = handoff.searchSteps
          setSearchSteps(handoff.searchSteps)
        }

        const cachedThreadHandoff = readThreadRunHandoff(threadId)
        const latestTerminalHandoffStatus =
          latest?.status === 'failed' || latest?.status === 'cancelled' || latest?.status === 'interrupted'
            ? latest.status
            : null
        const effectiveCachedThreadHandoff =
          cachedThreadHandoff &&
          latest &&
          latest.run_id === cachedThreadHandoff.runId &&
          latestTerminalHandoffStatus
            ? {
                ...cachedThreadHandoff,
                status: latestTerminalHandoffStatus,
              }
            : cachedThreadHandoff
        const shouldPreferReplayThreadHandoff =
          !!replayThreadHandoff &&
          !!latest &&
          latest.run_id === replayThreadHandoff.runId
        const shouldRestoreThreadHandoff =
          !!effectiveCachedThreadHandoff &&
          !(
            effectiveCachedThreadHandoff.status === 'completed' &&
            !!latest &&
            latest.run_id === effectiveCachedThreadHandoff.runId &&
            latestAssistant != null
          ) &&
          (
            !latest ||
            latest.run_id === effectiveCachedThreadHandoff.runId
          )
        if (
          shouldPreferReplayThreadHandoff &&
          replayThreadHandoff
        ) {
          restoreThreadHandoffUi(replayThreadHandoff)
        } else if (
          effectiveCachedThreadHandoff &&
          shouldRestoreThreadHandoff
        ) {
          restoreThreadHandoffUi(effectiveCachedThreadHandoff)
        } else if (
          replayThreadHandoff &&
          (!latest || latest.run_id === replayThreadHandoff.runId)
        ) {
          restoreThreadHandoffUi(replayThreadHandoff)
        } else if (threadId) {
          clearThreadRunHandoff(threadId)
          setTerminalRunCoveredRunIds([])
        }

        // 若 location state 已提供 initialRunId，优先使用（来自 WelcomePage 新建后导航）
        // 必须显式调用 setActiveRunId，因为 React Router 复用组件实例，useState 初始值不会重新求值
        if (
          locationState?.initialRunId &&
          (!latest || (latest.run_id === locationState.initialRunId && (latest.status === 'running' || latest.status === 'cancelling')))
        ) {
          const hint = readRunThinkingHint(locationState.initialRunId) ?? chooseThinkingHint(t.copThinkingHints)
          setActiveRunId(locationState.initialRunId)
          setPendingThinking(true)
          setThinkingHint(hint)
          writeRunThinkingHint(locationState.initialRunId, hint)
          if (threadId) onRunStarted(threadId)
        } else {
          const shouldResumeActiveRunFromHandoff =
            shouldRestoreThreadHandoff &&
            effectiveCachedThreadHandoff?.status === 'running' &&
            (latest?.status === 'running' || latest?.status === 'cancelling')
          const isActiveRun =
            shouldResumeActiveRunFromHandoff ||
            (!shouldRestoreThreadHandoff && (latest?.status === 'running' || latest?.status === 'cancelling'))
          setActiveRunId(isActiveRun ? latest.run_id : null)
          if (isActiveRun && latest) {
            const restoredRunHasVisibleUi =
              shouldResumeActiveRunFromHandoff &&
              !!effectiveCachedThreadHandoff &&
              hasRecoverableRunOutput({
                assistantTurn: effectiveCachedThreadHandoff.assistantTurn ?? null,
                searchSteps: effectiveCachedThreadHandoff.searchSteps,
                widgets: effectiveCachedThreadHandoff.widgets,
                codeExecutions: effectiveCachedThreadHandoff.codeExecutions,
                subAgents: effectiveCachedThreadHandoff.subAgents,
                fileOps: effectiveCachedThreadHandoff.fileOps,
                webFetches: effectiveCachedThreadHandoff.webFetches,
              })
            const latestRunAlreadyHasAssistant = latestAssistant != null
            if (!restoredRunHasVisibleUi && !latestRunAlreadyHasAssistant) {
              const hint = readRunThinkingHint(latest.run_id) ?? chooseThinkingHint(t.copThinkingHints)
              setPendingThinking(true)
              setThinkingHint(hint)
              writeRunThinkingHint(latest.run_id, hint)
            } else {
              setPendingThinking(false)
            }
            if (threadId) onRunStarted(threadId)
          } else {
            setPendingThinking(false)
            if (threadId) onRunEnded(threadId)
          }
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
  }, [accessToken, agentClient, threadId, setMetaBatch, onThreadUpserted])

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
    setQueuedPrompts([])
    setEditingQueuedPromptId(null)
    queuedEditPreviousDraftRef.current = ''
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
  }, [threadId, clearCompletedTitleTail, resetAssistantTurnLive])

  // 新 run 启动时重置 ChatView 局部渲染状态；stream 连接由 RunLifecycleProvider 统一持有。
  useEffect(() => {
    if (!activeRunId) return
    clearCompletedTitleTail()
    freezeCutoffRef.current = null
    injectionBlockedRunIdRef.current = null
    sseTerminalFallbackRunIdRef.current = activeRunId
    sseTerminalFallbackArmedRef.current = false
    seenFirstToolCallInRunRef.current = false
    processedEventCountRef.current = 0
    lastVisibleNonTerminalSeqRef.current = 0
    const shouldCarryRunningHandoff =
      preserveLiveRunUi &&
      terminalRunHandoffStatus === 'running' &&
      terminalRunDisplayId === activeRunId
    if (!shouldCarryRunningHandoff && threadId) {
      clearThreadRunHandoff(threadId)
    }
    if (!shouldCarryRunningHandoff) {
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
    }
    setCancelSubmitting(false)
  }, [activeRunId, clearCompletedTitleTail, resetAssistantTurnLive, threadId])

  useEffect(() => {
    if (!activeRunId) {
      lastVisibleNonTerminalSeqRef.current = 0
      return
    }
    const shouldCarryRunningHandoff =
      preserveLiveRunUi &&
      terminalRunHandoffStatus === 'running' &&
      terminalRunDisplayId === activeRunId
    if (!shouldCarryRunningHandoff) {
      setTerminalRunDisplayId(null)
      setTerminalRunHandoffStatus(null)
      setTerminalRunCoveredRunIds([])
    }
  }, [activeRunId, preserveLiveRunUi, setTerminalRunCoveredRunIds, setTerminalRunDisplayId, setTerminalRunHandoffStatus, terminalRunDisplayId, terminalRunHandoffStatus])

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

  const chatInputRef = useRef<ChatInputHandle>(null)
  const attachmentsRef = useRef(attachments)
  attachmentsRef.current = attachments
  const skipAttachmentDraftPersistRef = useRef(false)
  const prevAttachmentDraftScopeRef = useRef<InputDraftScope | null>(null)
  const pendingIncognitoRef = useRef(pendingIncognito)
  pendingIncognitoRef.current = pendingIncognito
  const messagesRef = useRef(messages)
  messagesRef.current = messages
  const draftScope = useMemo<InputDraftScope>(() => ({
    ownerKey: me?.id,
    page: 'thread',
    threadId,
    appMode: effectiveAppMode,
    searchMode: isSearchThread,
  }), [effectiveAppMode, isSearchThread, me?.id, threadId])
  const draftScopeKey = useMemo(() => JSON.stringify(draftScope), [draftScope])

  const {
    revokeDraftAttachment,
    handleAttachFiles,
    handlePasteContent,
    handleRemoveAttachment,
  } = useAttachmentActions()

  useEffect(() => {
    const prevScope = prevAttachmentDraftScopeRef.current
    const storedAttachments = readInputDraftAttachments(draftScope)
    const shouldMigrateCurrent =
      isSameDraftDomain(prevScope, draftScope)
      && prevScope?.ownerKey !== draftScope.ownerKey
      && storedAttachments.length === 0
      && attachmentsRef.current.length > 0
    const nextAttachments = shouldMigrateCurrent
      ? buildDraftAttachmentRecords(attachmentsRef.current)
      : storedAttachments
    if (shouldMigrateCurrent) {
      writeInputDraftAttachments(draftScope, nextAttachments)
    }
    prevAttachmentDraftScopeRef.current = draftScope
    skipAttachmentDraftPersistRef.current = true
    setAttachments((prev) => {
      prev.forEach((attachment) => revokeDraftAttachment(attachment))
      return nextAttachments.map(restoreAttachmentFromDraftRecord)
    })
  }, [draftScope, draftScopeKey, revokeDraftAttachment, setAttachments])

  useEffect(() => {
    if (skipAttachmentDraftPersistRef.current) {
      skipAttachmentDraftPersistRef.current = false
      return
    }
    writeInputDraftAttachments(draftScope, buildDraftAttachmentRecords(attachments))
  }, [attachments, draftScope, draftScopeKey])

  const queueReadyAttachments = useCallback((items: Attachment[]): UploadedThreadAttachment[] | null => {
    const uploads: UploadedThreadAttachment[] = []
    for (const item of items) {
      if (item.status !== 'ready' || !item.uploaded) return null
      uploads.push(item.uploaded)
    }
    return uploads
  }, [])

  const resolveReasoningMode = useCallback((): RunReasoningMode | undefined => {
    if (!threadId) return undefined
    const mode = readThreadReasoningMode(threadId)
    return mode !== 'off' ? mode as RunReasoningMode : undefined
  }, [threadId])

  const appendQueuedPrompt = useCallback((prompt: QueuedPrompt) => {
    setQueuedPrompts((prev) => [...prev, prompt])
  }, [setQueuedPrompts])

  const removeQueuedPrompt = useCallback((id: string) => {
    setQueuedPrompts((prev) => prev.filter((item) => item.id !== id))
    if (editingQueuedPromptId === id) {
      setEditingQueuedPromptId(null)
      queuedEditPreviousDraftRef.current = ''
    }
  }, [editingQueuedPromptId, setQueuedPrompts])

  const runQueuedPrompt = useCallback(async (prompt: QueuedPrompt, options?: { resumeFromRunId?: string }) => {
    if (!threadId) return
    const hint = chooseThinkingHint(t.copThinkingHints)
    setSending(true)
    setPendingThinking(true)
    setThinkingHint(hint)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    try {
      const message = await agentClient.createMessage({
        threadId,
        request: buildMessageRequest(prompt.text, prompt.attachments),
      })
      invalidateMessageSync()
      setUserEnterMessageId(message.id)
      setMessages((prev) => (prev.some((item) => item.id === message.id) ? prev : [...prev, message]))
      const run = await agentClient.createRun({
        threadId,
        personaId: prompt.personaKey,
        modelOverride: prompt.modelOverride,
        workDir: prompt.workDir,
        reasoningMode: prompt.reasoningMode,
        options: { resumeFromRunId: options?.resumeFromRunId },
      })
      writeRunThinkingHint(run.id, hint)
      if (prompt.personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
      resetSearchSteps()
      setActiveRunId(run.id)
      onRunStarted(threadId)
      activateAnchor()
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setQueuedPrompts((prev) => [prompt, ...prev])
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [
    activateAnchor,
    agentClient,
    injectionBlockedRunIdRef,
    invalidateMessageSync,
    onLoggedOut,
    onRunStarted,
    resetSearchSteps,
    setActiveRunId,
    setError,
    setInjectionBlocked,
    setMessages,
    setPendingThinking,
    setQueuedPrompts,
    setSending,
    setThinkingHint,
    setUserEnterMessageId,
    t.copThinkingHints,
    threadId,
  ])

  const drainNextQueuedPrompt = useCallback(() => {
    const next = queuedPromptsRef.current[0]
    if (!next) return
    setQueuedPrompts((prev) => prev.filter((item) => item.id !== next.id))
    void runQueuedPrompt(next)
  }, [runQueuedPrompt, setQueuedPrompts])

  const drainForcedQueuedPrompt = useCallback((terminal: { runId: string; status: 'completed' | 'cancelled' | 'failed' | 'interrupted' }) => {
    const next = forcedQueuedPromptRef.current
    if (!next) return false
    if (next.resumeFromRunId !== terminal.runId) return false
    forcedQueuedPromptRef.current = null
    setQueuedPrompts((prev) => prev.filter((item) => item.id !== next.prompt.id))
    void runQueuedPrompt(next.prompt, {
      resumeFromRunId: isInterruptedRunStatus(terminal.status) ? next.resumeFromRunId : undefined,
    })
    return true
  }, [runQueuedPrompt, setQueuedPrompts])

  useEffect(() => {
    drainQueuedPromptRef.current = drainNextQueuedPrompt
    drainForcedQueuedPromptRef.current = drainForcedQueuedPrompt
    return () => {
      if (drainQueuedPromptRef.current === drainNextQueuedPrompt) {
        drainQueuedPromptRef.current = null
      }
      if (drainForcedQueuedPromptRef.current === drainForcedQueuedPrompt) {
        drainForcedQueuedPromptRef.current = null
      }
    }
  }, [drainForcedQueuedPrompt, drainNextQueuedPrompt])

  useEffect(() => {
    if (editingQueuedPromptId && !queuedPrompts.some((item) => item.id === editingQueuedPromptId)) {
      setEditingQueuedPromptId(null)
      queuedEditPreviousDraftRef.current = ''
    }
  }, [editingQueuedPromptId, queuedPrompts])

  const restoreDraftAfterQueuedEdit = useCallback(() => {
    chatInputRef.current?.setValue(queuedEditPreviousDraftRef.current)
    queuedEditPreviousDraftRef.current = ''
  }, [])

  const saveQueuedPromptEdit = useCallback(() => {
    if (!editingQueuedPromptId) return false
    const text = chatInputRef.current?.getValue().trim() ?? ''
    if (!text) return true
    setQueuedPrompts((prev) => prev.map((item) =>
      item.id === editingQueuedPromptId ? { ...item, text } : item,
    ))
    setEditingQueuedPromptId(null)
    restoreDraftAfterQueuedEdit()
    return true
  }, [editingQueuedPromptId, restoreDraftAfterQueuedEdit, setQueuedPrompts])

  const cancelQueuedPromptEdit = useCallback(() => {
    setEditingQueuedPromptId(null)
    restoreDraftAfterQueuedEdit()
  }, [restoreDraftAfterQueuedEdit])

  const startQueuedPromptEdit = useCallback((item: QueuedPrompt) => {
    if (editingQueuedPromptId && editingQueuedPromptId !== item.id) {
      saveQueuedPromptEdit()
    }
    queuedEditPreviousDraftRef.current = chatInputRef.current?.getValue() ?? ''
    setEditingQueuedPromptId(item.id)
    chatInputRef.current?.setValue(item.text)
  }, [editingQueuedPromptId, saveQueuedPromptEdit])

  const sendQueuedPromptNow = useCallback(async (item: QueuedPrompt) => {
    if (!activeRunId) {
      removeQueuedPrompt(item.id)
      void runQueuedPrompt(item)
      return
    }
    if (cancelSubmitting) return

    forcedQueuedPromptRef.current = { prompt: item, resumeFromRunId: activeRunId }
    const cancelBoundary = Math.max(0, lastVisibleNonTerminalSeqRef.current)
    freezeCutoffRef.current = cancelBoundary
    noResponseMsgIdRef.current = null
    setCancelSubmitting(true)
    setError(null)
    setInjectionBlocked(null)

    try {
      await agentClient.cancelRun(activeRunId, cancelBoundary)
    } catch (err) {
      forcedQueuedPromptRef.current = null
      freezeCutoffRef.current = null
      setCancelSubmitting(false)
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    }
  }, [
    activeRunId,
    agentClient,
    cancelSubmitting,
    freezeCutoffRef,
    lastVisibleNonTerminalSeqRef,
    noResponseMsgIdRef,
    onLoggedOut,
    removeQueuedPrompt,
    runQueuedPrompt,
    setCancelSubmitting,
    setError,
    setInjectionBlocked,
  ])

  const handleSend = useCallback(async (e: React.FormEvent<HTMLFormElement>, personaKey: string, modelOverride?: string) => {
    e.preventDefault()
    if (sending || !threadId) return
    if (editingQueuedPromptId) {
      saveQueuedPromptEdit()
      return
    }
    const terminalRunIdToSync =
      terminalRunDisplayId &&
      terminalRunHandoffStatus !== 'running' &&
      terminalRunHandoffStatus != null
        ? terminalRunDisplayId
        : undefined
    markTerminalRunHistory(null)
    if (!terminalRunIdToSync) {
      clearThreadRunHandoff(threadId)
    }

    const draft = chatInputRef.current?.getValue() ?? ''
    const attachments = attachmentsRef.current
    const pendingIncognito = pendingIncognitoRef.current
    const messages = messagesRef.current
    const lastAssistantTerminalStatus = (() => {
      for (let index = messages.length - 1; index >= 0; index -= 1) {
        const item = messages[index]
        if (item.role === 'assistant') return readMessageTerminalStatus(item.id)
      }
      return null
    })()
    const shouldPinNewPrompt =
      !isInterruptedRunStatus(terminalRunHandoffStatus) &&
      !isInterruptedRunStatus(lastAssistantTerminalStatus)

    if (isStreaming) {
      const text = draft.trim()
      if (text || attachments.length > 0) {
        const queuedAttachments = queueReadyAttachments(attachments)
        if (!queuedAttachments) {
          setError({ message: 'Attachments are still uploading.' })
          return
        }
        appendQueuedPrompt(createQueuedPrompt({
          text,
          attachments: queuedAttachments,
          personaKey,
          modelOverride,
          workDir: resolveThreadWorkFolder(threadId),
          reasoningMode: resolveReasoningMode(),
        }))
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setAttachments([])
        chatInputRef.current?.clear()
      }
      return
    }

    const text = draft.trim()
    if (!text && attachments.length === 0) return

    const hint = chooseThinkingHint(t.copThinkingHints)
    setSending(true)
    setPendingThinking(true)
    setThinkingHint(hint)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    try {
      const uploadAttachments = async () => {
        return await Promise.all(
          attachments.map(async (attachment) => {
            if (attachment.uploaded) return attachment.uploaded
            if (!attachment.file) throw new Error('attachment file missing')
            return await uploadStagingAttachment(accessToken, attachment.file)
          }),
        )
      }

      if (pendingIncognito && messages.length > 0) {
        await waitForThreadModeUpdates()
        const lastMessageId = messages[messages.length - 1].id
        const forked = await forkThread(accessToken, threadId, lastMessageId, true)
        if (forked.id_mapping) migrateMessageMetadata(forked.id_mapping)
        onThreadCreated(forked)
        const uploaded = await uploadAttachments()
        const forkUserMessage = await agentClient.createMessage({
          threadId: forked.id,
          request: buildMessageRequest(text, uploaded),
        })
        const run = await agentClient.createRun({
          threadId: forked.id,
          personaId: personaKey,
          modelOverride,
          workDir: resolveThreadWorkFolder(threadId),
          reasoningMode: readThreadReasoningMode(threadId) !== 'off' ? readThreadReasoningMode(threadId) as RunReasoningMode : undefined,
        })
        writeRunThinkingHint(run.id, hint)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(forked.id)
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        chatInputRef.current?.clear()
        setAttachments([])
        navigate(`/t/${forked.id}`, {
          state: {
            isIncognitoFork: true,
            initialRunId: run.id,
            forkBaseCount: messages.length,
            userEnterMessageId: forkUserMessage.id,
          },
          replace: false,
        })
        onRunStarted(forked.id)
        return
      }

      const uploaded = await uploadAttachments()
      const message = await agentClient.createMessage({
        threadId,
        request: buildMessageRequest(text, uploaded),
      })
      invalidateMessageSync()
      const syncedMessages = terminalRunIdToSync
        ? await readConsistentMessages(terminalRunIdToSync)
        : null
      setUserEnterMessageId(message.id)
      setMessages((prev) => {
        const base = syncedMessages && syncedMessages.length > 0 ? syncedMessages : prev
        return base.some((item) => item.id === message.id) ? base : [...base, message]
      })
      if (terminalRunIdToSync && syncedMessages?.some((item) => item.role === 'assistant' && item.streamId === terminalRunIdToSync)) {
        clearThreadRunHandoff(threadId)
      }
      if (shouldPinNewPrompt) {
        activateAnchor()
      } else {
        scrollToBottom()
      }
      attachments.forEach((attachment) => revokeDraftAttachment(attachment))
      chatInputRef.current?.clear()
      setAttachments([])
      injectionBlockedRunIdRef.current = null
      noResponseMsgIdRef.current = message.id

      await waitForThreadModeUpdates()
      const run = await agentClient.createRun({
        threadId,
        personaId: personaKey,
        modelOverride,
        workDir: resolveThreadWorkFolder(threadId),
        reasoningMode: readThreadReasoningMode(threadId) !== 'off' ? readThreadReasoningMode(threadId) as RunReasoningMode : undefined,
      })
      writeRunThinkingHint(run.id, hint)
      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
      resetSearchSteps()
      setActiveRunId(run.id)
      onRunStarted(threadId)
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
    agentClient,
    appendQueuedPrompt,
    editingQueuedPromptId,
    invalidateMessageSync,
    isStreaming,
    terminalRunDisplayId,
    terminalRunHandoffStatus,
    markTerminalRunHistory,
    navigate,
    onLoggedOut,
    onRunStarted,
    onThreadCreated,
    readConsistentMessages,
    resetSearchSteps,
    revokeDraftAttachment,
    activateAnchor,
    scrollToBottom,
    sending,
    saveQueuedPromptEdit,
    setActiveRunId,
    setAttachments,
    setError,
    setInjectionBlocked,
    setMessages,
    setSending,
    setPendingThinking,
    setThinkingHint,
    setUserEnterMessageId,
    t.copThinkingHints,
    threadId,
    waitForThreadModeUpdates,
    queueReadyAttachments,
    resolveThreadWorkFolder,
    resolveReasoningMode,
  ])

  const terminalSseError = useMemo(() => {
    if (!sse.error) return null
    return normalizeError(sse.error)
  }, [sse.error])
  const inputError = error ?? terminalSseError
  const inputErrorKey = useMemo(() => (inputError ? errorNoticeKey(inputError) : null), [inputError])
  const showInputError = !!inputError && inputErrorKey !== dismissedInputErrorKey

  useEffect(() => {
    if (!inputError) setDismissedInputErrorKey(null)
  }, [inputError])

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
  if (agentPanelAgent) lastAgentPanelRef.current = agentPanelAgent
  const panelDisplaySources = sourcePanelSources ?? lastPanelSourcesRef.current
  const panelDisplayQuery = sourcePanelUserQuery ?? lastPanelQueryRef.current
  const codePanelDisplay = codePanelExecution ?? lastCodePanelRef.current
  const documentPanelDisplay = documentPanelArtifact ?? lastDocumentPanelRef.current
  const agentPanelDisplay = agentPanelAgent ?? lastAgentPanelRef.current
  const isSourcePanelOpen = !!(sourcePanelSources && sourcePanelSources.length > 0)
  const isCodePanelOpen = !!codePanelExecution
  const isDocumentPanelOpen = !!documentPanelArtifact
  const isAgentPanelOpen = !!agentPanelAgent
  const isPanelOpen = isSourcePanelOpen || isCodePanelOpen || isDocumentPanelOpen || isAgentPanelOpen

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

  const agentPanelContent = useMemo(() => {
    if (!isAgentPanelOpen || !agentPanelDisplay) return null
    return (
      <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
        <AgentPanel agent={agentPanelDisplay} onClose={closePanel} />
      </div>
    )
  }, [isAgentPanelOpen, agentPanelDisplay, closePanel])

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
  const visibleStreamingWidgetsSignature = useMemo(() => JSON.stringify(
    streamingArtifacts
      .filter((entry) => entry.toolName === 'show_widget')
      .map((entry) => ({
        toolCallId: entry.toolCallId ?? null,
        toolCallIndex: entry.toolCallIndex,
        contentLength: entry.content?.length ?? 0,
        loadingMessagesCount: entry.loadingMessages?.length ?? 0,
        complete: entry.complete,
        hiddenByCop: !!(entry.toolCallId && livePlacedShowWidgetCallIds.has(entry.toolCallId)),
        visible: visibleStreamingWidgets.some((visible) => visible.toolCallIndex === entry.toolCallIndex),
      })),
  ), [livePlacedShowWidgetCallIds, streamingArtifacts, visibleStreamingWidgets])
  const lastVisibleStreamingWidgetsSignatureRef = useRef('')
  useEffect(() => {
    if (visibleStreamingWidgetsSignature === lastVisibleStreamingWidgetsSignatureRef.current) return
    lastVisibleStreamingWidgetsSignatureRef.current = visibleStreamingWidgetsSignature
    let entries: Array<{
      toolCallId: string | null
      toolCallIndex: number
      contentLength: number
      complete: boolean
      visible: boolean
    }> = []
    try {
      entries = JSON.parse(visibleStreamingWidgetsSignature)
    } catch {
      entries = []
    }
    if (!activeRunId) return
    for (const entry of entries) {
      noteShowWidgetStatus({
        runId: activeRunId,
        toolCallId: entry.toolCallId ?? '',
        toolCallIndex: entry.toolCallIndex,
        contentLength: entry.contentLength,
        visible: entry.visible,
        complete: entry.complete,
      })
    }
  }, [activeRunId, visibleStreamingWidgetsSignature])

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
        failed: t.failedRunTitle,
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

  const handleTogglePlanMode = useCallback(async (currentMode: boolean) => {
    if (!threadId || !isWorkMode) return
    const nextMode: CollaborationMode = currentMode ? 'default' : 'plan'
    const requestSeq = ++planModeRequestSeqRef.current
    const updatePromise: Promise<void> = updateThreadCollaborationMode(accessToken, threadId, nextMode).then((thread) => {
      if (planModeRequestSeqRef.current === requestSeq) {
        onThreadUpserted(thread)
      }
    }).catch((err) => {
      if (planModeRequestSeqRef.current === requestSeq) {
        setError(normalizeError(err))
        throw err
      }
    }).finally(() => {
      if (planModeUpdateRef.current === updatePromise) {
        planModeUpdateRef.current = null
      }
    })
    planModeUpdateRef.current = updatePromise
    await updatePromise
  }, [accessToken, isWorkMode, onThreadUpserted, setError, threadId])

  const handleToggleLearningMode = useCallback(async (currentMode: boolean) => {
    if (!threadId || learningModeUpdateRef.current) return
    const requestSeq = ++learningModeRequestSeqRef.current
    setLearningModeUpdating(true)
    const updatePromise: Promise<void> = updateThreadLearningMode(accessToken, threadId, !currentMode).then((thread) => {
      if (learningModeRequestSeqRef.current === requestSeq) {
        onThreadUpserted(thread)
      }
    }).catch((err) => {
      if (learningModeRequestSeqRef.current === requestSeq) {
        setError(normalizeError(err))
        throw err
      }
    }).finally(() => {
      if (learningModeUpdateRef.current === updatePromise) {
        learningModeUpdateRef.current = null
        setLearningModeUpdating(false)
      }
    })
    learningModeUpdateRef.current = updatePromise
    await updatePromise
  }, [accessToken, onThreadUpserted, setError, threadId])

  const hasMessages = messages.length > 0
  const inputHorizontalPadding = isWorkMode
    ? (isPanelOpen ? chatContentPadding.panelOpen : chatContentPadding.panelClosed)
    : (isPanelOpen ? chatInputPadding.panelOpen : chatInputPadding.panelClosed)

  const chatInputEl = useMemo(() => (
    <ChatInput
      key={`${threadId ?? '__no_thread__'}:${effectiveAppMode}:${isSearchThread ? 'search' : 'default'}`}
      ref={chatInputRef}
      onSubmit={handleSend}
      onCancel={handleCancel}
      placeholder={isStreaming ? t.followUpPlaceholder : t.replyPlaceholder}
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
      appMode={effectiveAppMode}
      hasMessages={hasMessages}
      messagesLoading={messagesLoading}
      workThreadId={threadId}
      queuedEditLabel={editingQueuedPromptId ? 'Edit Queued' : undefined}
      onCancelQueuedEdit={cancelQueuedPromptEdit}
      draftOwnerKey={me?.id}
      planMode={currentThread?.collaboration_mode === 'plan'}
      onTogglePlanMode={handleTogglePlanMode}
      learningModeEnabled={!!currentThread?.learning_mode_enabled}
      learningModeUpdating={learningModeUpdating}
      onToggleLearningMode={handleToggleLearningMode}
    />
  ), [attachments, sending, isStreaming, canCancel, cancelSubmitting, effectiveAppMode, isSearchThread, hasMessages, messagesLoading, threadId, accessToken, me?.id, t.followUpPlaceholder, t.replyPlaceholder, handleSend, handleCancel, handleAttachFiles, handlePasteContent, handleRemoveAttachment, handleAsrError, handlePersonaChange, onOpenSettings, editingQueuedPromptId, cancelQueuedPromptEdit, currentThread?.collaboration_mode, currentThread?.learning_mode_enabled, learningModeUpdating, handleTogglePlanMode, handleToggleLearningMode])

  const renderLiveCopItems = (
    seg: Extract<AssistantTurnSegment, { type: 'cop' }>,
    si: number,
  ): React.ReactNode[] => {
    const lastSegIdx = liveSegments.length - 1
    const preservingHandoffSegments =
      preserveLiveRunUi &&
      !isStreaming &&
      terminalRunHandoffStatus !== 'completed'
    const copClosedByFollowingSeg = si < lastSegIdx
    const copTimelineLive = liveRunUiActive && !copClosedByFollowingSeg

    const timelinePools = {
      codeExecutions: dedupedTopLevelCodeExecutions,
      fileOps: topLevelFileOps,
      webFetches: topLevelWebFetches,
      subAgents: topLevelSubAgents,
      searchSteps,
      sources: currentRunSourcesRef.current,
    }
    const turnTodoWrites = liveSegments
      .flatMap((entry) => entry.type === 'cop'
        ? copTimelinePayloadForSegment(entry, timelinePools).todoWrites ?? []
        : [])
    const payload = copTimelinePayloadForSegment(seg, timelinePools)

    const liveWidgets = liveStreamingWidgetEntriesForCop(seg, streamingArtifacts)
    const liveArts = liveInlineArtifactEntriesForCop(seg, streamingArtifacts)

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
            hasThinking: seg.items.some((item) => item.kind === 'thinking'),
            handoffStatus: terminalRunHandoffStatus === 'running' ? null : terminalRunHandoffStatus,
          })
        : seg.title?.trim() || undefined

    return [
      <CopSegmentBlocks
          key={`live-cop-${si}`}
          segment={seg}
          keyPrefix={`live-cop-${si}`}
          {...timelinePools}
          isComplete={!copTimelineLive}
          live={copTimelineLive}
          shimmer={copTimelineLive}
          thinkingHint={thinkingHint}
          headerOverride={timelineTitleOverride}
          onOpenCodeExecution={openCodePanel}
          onOpenSubAgent={openAgentPanelState}
          activeCodeExecutionId={codePanelExecution?.id}
          accessToken={accessToken}
          baseUrl={baseUrl}
          typography={isWorkMode ? 'work' : 'default'}
          todoWritesForFinalDisplay={turnTodoWrites}
        />,
      ...liveWidgets.map((entry) => (
        <WidgetBlock
          key={`live-w-${entry.toolCallId ?? entry.toolCallIndex}`}
          html={entry.content ?? ''}
          title={entry.title ?? 'Widget'}
          complete={entry.complete}
          loadingMessages={entry.loadingMessages}
          compact
          debugMeta={activeRunId ? {
            runId: activeRunId,
            toolCallId: entry.toolCallId,
            toolCallIndex: entry.toolCallIndex,
          } : undefined}
          onAction={handleArtifactAction}
        />
      )),
      ...liveArts.map((entry) => (
        <ArtifactStreamBlock
          key={`live-art-${entry.toolCallId ?? entry.toolCallIndex}`}
          entry={entry}
          accessToken={accessToken}
          compact
          onAction={handleArtifactAction}
        />
      )),
    ].filter(Boolean)
  }

  const renderLiveCopSegment = (
    seg: Extract<AssistantTurnSegment, { type: 'cop' }>,
    si: number,
    key?: string,
  ) => {
    const items = renderLiveCopItems(seg, si)
    if (items.length === 0) return null
    return (
      <Fragment key={key ?? `live-cop-${si}`}>
        {items}
      </Fragment>
    )
  }

  return (
    <div className="theme-surface-page relative flex min-w-0 flex-1 flex-col overflow-hidden bg-[var(--c-bg-page)]">
      <ChatTitleMenu />

      {/* 主体区域：消息 + 输入 + 可选的 sources 侧边面板 */}
      <div className="relative flex flex-1 min-h-0">
        <div className="relative flex flex-1 min-w-0 flex-col">
          <div className="pointer-events-none absolute inset-x-0 top-0 z-10 h-10" style={{ background: 'linear-gradient(to bottom, var(--c-bg-page-gradient-stop, var(--c-bg-page)), transparent)' }} />
          {/* 消息列表 */}
          <div
            ref={scrollContainerRef}
            onScroll={handleScrollContainerScroll}
            className="theme-surface-page chat-scroll-hidden relative flex-1 min-h-0 overflow-y-auto bg-[var(--c-bg-page)] [scrollbar-gutter:stable]"
            style={{ contain: 'layout paint style' }}
          >
        <div
          style={{
            maxWidth: isWorkMode ? 1000 : 800,
            margin: '0 auto',
            padding: `50px ${isPanelOpen ? chatContentPadding.panelOpen : chatContentPadding.panelClosed} var(--chat-input-area-height)`,
            gap: isWorkMode ? 0 : undefined,
          }}
          className="flex w-full flex-col gap-6"
        >
          {messagesLoading ? (
            <ChatSkeleton isWorkMode={isWorkMode} />
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
              <CopTimelineLocalExpansionProvider stabilizeScroll={stabilizeDocumentPanelScroll}>
                <MessageList
                isWorkMode={isWorkMode}
                lastTurnStartIdx={lastTurnStartIdx}
                lastTurnRef={lastUserMsgRef}
                lastUserPromptRef={lastUserPromptRef}
                lastTurnChildren={
                  <LiveRunPane
                    isWorkMode={isWorkMode}
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
                    activeSegmentId={activeSegmentIdRef.current}
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
                    pendingIncognito={pendingIncognito}
                    incognitoDividerText={t.incognitoForkDivider}
                    onIncognitoDividerComplete={() => {
                      if (isAtBottomRef.current) {
                        activateAnchor()
                      }
                    }}
                    terminalRunHandoffStatus={terminalRunHandoffStatus}
                    terminalRunDisplayId={terminalRunDisplayId}
                    showRunDetailButton={showRunDetailButton}
                    setRunDetailPanelRunId={setRunDetailPanelRunId}
                    onOpenDocument={openDocumentPanel}
                    onOpenCodeExecution={openCodePanel}
                    onOpenSubAgent={openAgentPanelState}
                    onArtifactAction={handleArtifactAction}
                    renderLiveCopItems={renderLiveCopItems}
                    renderLiveCopSegment={renderLiveCopSegment}
                    bottomRef={bottomRef}
                  />
                }
                showRunDetailButton={showRunDetailButton}
                currentRunCopHeaderOverride={currentRunCopHeaderOverride}
                handleRetryUserMessage={handleRetryUserMessage}
                handleEditMessage={handleEditMessage}
                handleFork={handleFork}
                handleArtifactAction={handleArtifactAction}
                openDocumentPanel={openDocumentPanel}
                openCodePanel={openCodePanel}
                openAgentPanel={openAgentPanelState}
                sourcePanelMessageId={sourcePanelMessageId}
                setRunDetailPanelRunId={setRunDetailPanelRunId}
                clearUserEnterAnimation={clearUserEnterAnimation}
                />
              </CopTimelineLocalExpansionProvider>

            </>
          )}
        </div>
        <div ref={spacerRef} style={{ flexShrink: 0, overflowAnchor: 'none' }} />
      </div>

      {/* 输入区域 */}
      <div
        ref={inputAreaRef}
        style={{
          '--chat-input-horizontal-padding': inputHorizontalPadding,
          maxWidth: isWorkMode ? 1000 : 1200,
          margin: '0 auto',
          padding: '12px var(--chat-input-horizontal-padding) 8px',
          position: 'absolute',
          bottom: 0,
          left: 0,
          right: 0,
          zIndex: 10,
          background: 'linear-gradient(to bottom, transparent 0%, var(--c-bg-page-gradient-stop, var(--c-bg-page)) 24px)',
        } as React.CSSProperties}
        className="flex w-full flex-col items-center gap-2"
      >
        {/* 滚动到底部按钮：始终锚定在输入框顶边正上方 */}
        <ScrollToBottomButton
          onScrollToBottom={scrollToBottom}
          liveRunUiActive={liveRunUiActive}
          subscribeIsAtBottom={subscribeIsAtBottom}
          getIsAtBottomSnapshot={getIsAtBottomSnapshot}
        />
        {(showInputError || queuedPrompts.length > 0) && (
          <div
            className="pointer-events-none absolute flex flex-col gap-2"
            style={{
              left: 'var(--chat-input-horizontal-padding)',
              right: 'var(--chat-input-horizontal-padding)',
              bottom: 'calc(100% + 6px)',
              zIndex: 30,
            }}
          >
            {showInputError && inputError && (
              <div
                className="pointer-events-auto w-full"
                style={{ maxWidth: isWorkMode ? undefined : '720px', margin: '0 auto' }}
              >
                <RunErrorNotice
                  error={inputError}
                  onDismiss={() => {
                    if (inputErrorKey) setDismissedInputErrorKey(inputErrorKey)
                  }}
                />
              </div>
            )}
            {queuedPrompts.length > 0 && (
              <div
                className="pointer-events-auto w-full"
                style={{ maxWidth: isWorkMode ? undefined : '720px', margin: '0 auto' }}
              >
                <QueuedPromptNotice
                  items={queuedPrompts}
                  editingId={editingQueuedPromptId}
                  activeRunId={activeRunId}
                  onEdit={startQueuedPromptEdit}
                  onSendNow={(item) => { void sendQueuedPromptNow(item) }}
                  onDelete={(item) => removeQueuedPrompt(item.id)}
                />
              </div>
            )}
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
        {/* 右侧面板：flex 兄弟节点；用 motion 驱动 width，避免嵌套 flex + CSS transition 偶发不插值 */}
          <motion.div
            className="flex-shrink-0 overflow-hidden"
            initial={false}
            animate={{
              width: isDocumentPanelOpen
                ? documentPanelWidth
                : (isSourcePanelOpen || isCodePanelOpen || isAgentPanelOpen)
                  ? sidePanelWidth
                  : 0,
            }}
            transition={{ duration: 0.28, ease: [0.16, 1, 0.3, 1] }}
            style={{
              borderLeft: (panelDisplaySources || codePanelDisplay || documentPanelDisplay || agentPanelDisplay)
                ? '0.5px solid var(--c-border-subtle)'
                : 'none',
            }}
          >
            {sourcesPanelContent}
            {codePanelContent}
            {documentPanelContent}
            {agentPanelContent}
          </motion.div>
      </div>

      {shareModalEl}
      {runDetailEl}
      {showDebugPanel && <DebugTrigger />}
    </div>
  )
})
