import React, { useState, useEffect, useRef, useCallback, useMemo, useSyncExternalStore, memo, Fragment, type ComponentProps } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowDown, ArrowUpFromLine, ChevronDown, ChevronRight, ClipboardList, CornerDownLeft, Globe2, Pencil, Trash2 } from 'lucide-react'
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
import { AgentPanel } from './AgentPanel'
import { RightPanel, type RightPanelTab } from './RightPanel'
import { rightPanelIconSize } from './rightPanelControls'
import { LocalFilesPanel } from './local-files/LocalFilesPanel'
import { resolveLocalFileIconUrl } from './local-files/fileIconResolver'
import { ResourcePreviewPanel } from './resource-preview/ResourcePreviewPanel'
import { BrowserSiteIcon } from './resource-preview/BrowserSiteIcon'
import type { BrowserResourceRef, LocalFileResourceRef, ResourceRef } from './resource-preview/types'
import { resourceTitle } from './resource-preview/resourceUri'
import { isPlanMarkdownPath } from '../planMetadata'
import { ChatTitleMenu } from './ChatTitleMenu'
import { MessageList, type MessageListHandle } from './MessageList'
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
import { getThreadTodos, setThreadTodos, clearThreadTodos, type TodoItem } from '../todoDb'
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
import { useThreadList, useThreadLiveState } from '../contexts/thread-list'
import { useAppModeUI, useRightPanelActions, useSettingsUI, useTitleBarRightPanelUI } from '../contexts/app-ui'
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
import {
  buildMessageRequest,
  buildOptimisticUserMessage,
  buildUserMessageRetryRequest,
  createClientMessageId,
  markDeliveryFailed,
  messageClientMessageId,
  replaceLocalUserMessage,
  undeliveredLocalUserMessages,
  withMessageDeliveryStatus,
} from '../messageContent'
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
  readThreadRightPanelState,
  readThreadWorkFolder,
  readThreadReasoningMode,
  writeInputDraftAttachments,
  writeThreadRightPanelState,
  readRunThinkingHint,
  writeRunThinkingHint,
} from '../storage'

const chatContentPadding = { panelClosed: 'clamp(24px, 5vw, 60px)', panelOpen: 'clamp(16px, 3vw, 40px)' } as const
const chatInputPadding = { panelClosed: 'clamp(24px, 5vw, 60px)', panelOpen: 'clamp(16px, 3vw, 40px)', work: '14px' } as const
const rightPanelDefaultRatio = 0.4
const rightPanelMinWidth = 420
const chatViewMinWidth = 520
const rightPanelLayoutTransition = { duration: 0.22, ease: [0.16, 1, 0.3, 1] } as const
const rightPanelLayoutTransitionCss = '220ms cubic-bezier(0.16, 1, 0.3, 1)'

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

function resourceTabId(resource: ResourceRef): string {
  if (resource.kind === 'artifact') return `resource:artifact:${resource.key}`
  if (resource.kind === 'browser') return `resource:browser:${resource.url}`
  if (resource.kind === 'local-file') return `resource:local:${resource.rootPath}:${resource.path}`
  return `resource:workspace:${resource.projectId ?? resource.runId ?? ''}:${resource.path}`
}

function localFileTabIcon(resource: LocalFileResourceRef | null) {
  if (!resource) return undefined
  if (isPlanMarkdownPath(resource.path)) {
    return <ClipboardList size={rightPanelIconSize} strokeWidth={1.8} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
  }
  const iconUrl = resolveLocalFileIconUrl({
    name: resource.name ?? resource.filename ?? resource.path.split('/').filter(Boolean).at(-1) ?? resource.path,
    path: resource.path,
    type: 'file',
    size: resource.size,
  })
  return iconUrl ? <img src={iconUrl} alt="" aria-hidden="true" draggable={false} style={{ width: rightPanelIconSize, height: rightPanelIconSize, flexShrink: 0 }} /> : undefined
}

function resourcePanelIcon(resource: ResourceRef) {
  const planPath = 'path' in resource ? resource.path : 'filename' in resource ? resource.filename : undefined
  if (isPlanMarkdownPath(planPath)) {
    return <ClipboardList size={rightPanelIconSize} strokeWidth={1.8} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
  }
  if (resource.kind === 'local-file') return localFileTabIcon(resource)
  if (resource.kind === 'browser') return <BrowserSiteIcon url={resource.url} faviconUrl={resource.faviconUrl} size={rightPanelIconSize} />
  return undefined
}

function sameBrowserResource(left: BrowserResourceRef | null, right: BrowserResourceRef): boolean {
  return left?.url === right.url
    && left?.title === right.title
    && left?.faviconUrl === right.faviconUrl
}

function clampRightPanelWidth(width: number, containerWidth: number): number {
  const maxWidth = Math.max(0, containerWidth - chatViewMinWidth)
  if (maxWidth <= rightPanelMinWidth) return maxWidth
  return Math.min(Math.max(width, rightPanelMinWidth), maxWidth)
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

type RightPanelStoredTab =
  | { id: string; kind: 'source'; title: string; messageId: string }
  | { id: string; kind: 'code'; title: string; execution: CodeExecution }
  | { id: string; kind: 'agent'; title: string; agent: SubAgentRef }
  | { id: string; kind: 'resource'; title: string; resource: ResourceRef; artifacts?: ArtifactRef[]; runId?: string }

function browserTabSeqFromTabs(tabs: Array<{ id: string }>): number {
  return tabs.reduce((max, tab) => {
    const match = /^web:(\d+)$/.exec(tab.id)
    if (!match) return max
    return Math.max(max, Number(match[1]))
  }, 0)
}

function localFileTabSeqFromTabs(tabs: Array<{ id: string }>): number {
  return tabs.reduce((max, tab) => {
    const match = /^local-file:(\d+)$/.exec(tab.id)
    if (!match) return max
    return Math.max(max, Number(match[1]))
  }, 0)
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
  workFolder?: string | null
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
  onOpenResource: (resource: ResourceRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
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
  workFolder,
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
  onOpenResource,
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
                workFolder={workFolder}
                onOpenDocument={onOpenDocument}
                onOpenResource={onOpenResource}
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
    markCompletionRead,
  } = useThreadList()
  const { completedUnreadThreadIds } = useThreadLiveState()
  const { appMode } = useAppModeUI()
  const { setRightPanelOpen } = useRightPanelActions()
  const { setTitleBarRightPanelClick } = useTitleBarRightPanelUI()
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
  const [rightPanelVisible, setRightPanelVisible] = useState(false)
  const chatViewRootRef = useRef<HTMLDivElement>(null)
  const rightPanelShellRef = useRef<HTMLDivElement>(null)
  const rightPanelContentRef = useRef<HTMLDivElement>(null)
  const rightPanelRatioRef = useRef(0)
  const [rightPanelResizing, setRightPanelResizing] = useState(false)
  const [rightPanelWidth, setRightPanelWidth] = useState(rightPanelMinWidth)
  const [rightPanelTabs, setRightPanelTabs] = useState<RightPanelStoredTab[]>([])
  const [activeRightPanelTabId, setActiveRightPanelTabId] = useState<string | null>(null)
  const [rightPanelTabOrder, setRightPanelTabOrder] = useState<string[]>([])
  const [webPanelResource, setWebPanelResource] = useState<BrowserResourceRef | null>(null)
  const [extraBrowserTabs, setExtraBrowserTabs] = useState<Array<{ id: string; resource: BrowserResourceRef | null }>>([])
  const [filesPreviewResource, setFilesPreviewResource] = useState<LocalFileResourceRef | null>(null)
  const browserTabSeqRef = useRef(0)
  const localFileTabSeqRef = useRef(0)
  const restoredRightPanelThreadRef = useRef<string | null>(null)
  const skipRightPanelSaveRef = useRef(false)
  const isPanelOpenRef = useRef(false)
  const effectiveRightPanelTabIdRef = useRef<string | null>(null)
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
    openResourcePanel: openResourcePanelState,
    openAgentPanel: openAgentPanelState,
    closePanel,
    closeShareModal,
  } = usePanels()
  const closePanelRef = useRef(closePanel)
  useEffect(() => { closePanelRef.current = closePanel }, [closePanel])
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
  const resourcePanelResource = activePanel?.type === 'resource' ? activePanel.resource : null
  // Mirror volatile activePanel-derived values into refs so callbacks passed to
  // MessageList stay referentially stable when the panel context changes.
  const documentPanelArtifactKeyRef = useRef(documentPanelArtifact?.artifact.key)
  useEffect(() => { documentPanelArtifactKeyRef.current = documentPanelArtifact?.artifact.key }, [documentPanelArtifact?.artifact.key])
  const codePanelExecutionIdRef = useRef(codePanelExecution?.id)
  useEffect(() => { codePanelExecutionIdRef.current = codePanelExecution?.id }, [codePanelExecution?.id])
  const agentPanelAgentIdRef = useRef(agentPanelAgent?.id)
  useEffect(() => { agentPanelAgentIdRef.current = agentPanelAgent?.id }, [agentPanelAgent?.id])
  const resourcePanelResourceRef = useRef(resourcePanelResource)
  useEffect(() => { resourcePanelResourceRef.current = resourcePanelResource }, [resourcePanelResource])
  const sourcePanelMessageIdRef = useRef(sourcePanelMessageId)
  useEffect(() => { sourcePanelMessageIdRef.current = sourcePanelMessageId }, [sourcePanelMessageId])
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

  const messageListRef = useRef<MessageListHandle>(null)
  // 提供给 useScrollPin 的桥：scrollToBottom 触发时先让 Virtuoso 跳过中间区间，
  // 避免浏览器原生 smooth scroll 逐帧穿过数千 px 引发挂载风暴。
  const jumpHistoryToEnd = useCallback(() => {
    messageListRef.current?.scrollHistoryToEnd('auto')
  }, [])

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
    jumpHistoryToEnd,
  })

  // 把 scrollContainer 的 DOM 元素以 state 暴露出来，供 Virtuoso 的 customScrollParent 使用。
  // 必须保持与 scrollContainerRef.current 同步——同一个 div 同时承担 useScrollPin 的 ref 目标与 Virtuoso 的滚动父容器。
  const [scrollParent, setScrollParent] = useState<HTMLDivElement | null>(null)
  const setScrollContainerEl = useCallback((el: HTMLDivElement | null) => {
    (scrollContainerRef as React.MutableRefObject<HTMLDivElement | null>).current = el
    setScrollParent(el)
  }, [scrollContainerRef])

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
  const handleBuildPlan = useCallback((message: string) => {
    void sendMessage(message)
  }, [sendMessage])

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
        let replayedTodos: TodoItem[] = []
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
            replayedTodos = buildTodosFromAgentEvents(replayEvents)
            if (replayedTodos.length > 0) {
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
        if (replayedTodos.length > 0) {
          setWorkTodos(replayedTodos)
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
    if (!threadId) return
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

    if (sending && !isStreaming) {
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
    const deliveryAttemptKeys: Array<{ messageId: string; clientMessageId: string }> = []

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
        setSending(true)
        setPendingThinking(true)
        setThinkingHint(hint)
        setError(null)
        setInjectionBlocked(null)
        injectionBlockedRunIdRef.current = null
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

      const uploaded = queueReadyAttachments(attachments)
      if (!uploaded) {
        setError({ message: 'Attachments are still uploading.' })
        return
      }
      const clientMessageId = createClientMessageId()
      const request = buildMessageRequest(text, uploaded)
      const localMessage = buildOptimisticUserMessage(request, clientMessageId)

      setSending(true)
      setPendingThinking(true)
      setThinkingHint(hint)
      setError(null)
      setInjectionBlocked(null)
      injectionBlockedRunIdRef.current = null
      invalidateMessageSync()
      setUserEnterMessageId(localMessage.id)
      setMessages((prev) => [...prev, localMessage])
      if (shouldPinNewPrompt) {
        activateAnchor()
      } else {
        scrollToBottom()
      }
      attachments.forEach((attachment) => revokeDraftAttachment(attachment))
      chatInputRef.current?.clear()
      setAttachments([])

      let lastCreatedMessage: AgentMessage | null = null
      const messagesToCreate = undeliveredLocalUserMessages([...messages, localMessage])
      for (const messageToCreate of messagesToCreate) {
        const retryClientMessageId = messageClientMessageId(messageToCreate) ?? createClientMessageId()
        deliveryAttemptKeys.push({ messageId: messageToCreate.id, clientMessageId: retryClientMessageId })
        setMessages((prev) => prev.map((item) =>
          item.id === messageToCreate.id
            ? withMessageDeliveryStatus(item, 'pending', retryClientMessageId)
            : item,
        ))
        const created = await agentClient.createMessage({
          threadId,
          request: buildUserMessageRetryRequest(messageToCreate, retryClientMessageId),
        })
        invalidateMessageSync()
        setUserEnterMessageId(created.id)
        setMessages((prev) => replaceLocalUserMessage(prev, messageToCreate.id, retryClientMessageId, created))
        lastCreatedMessage = created
      }

      if (!lastCreatedMessage) return
      const syncedMessages = terminalRunIdToSync
        ? await readConsistentMessages(terminalRunIdToSync)
        : null
      setMessages((prev) => {
        const base = syncedMessages && syncedMessages.length > 0 ? syncedMessages : prev
        return base
      })
      if (terminalRunIdToSync && syncedMessages?.some((item) => item.role === 'assistant' && item.streamId === terminalRunIdToSync)) {
        clearThreadRunHandoff(threadId)
      }
      injectionBlockedRunIdRef.current = null
      noResponseMsgIdRef.current = lastCreatedMessage.id

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
      setMessages((prev) => markDeliveryFailed(prev, deliveryAttemptKeys))
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
  const workPanelFolder = threadId ? resolveThreadWorkFolder(threadId) : undefined
  const isSourcePanelOpen = !!(sourcePanelSources && sourcePanelSources.length > 0)
  const isPanelOpen = rightPanelVisible

  useEffect(() => {
    skipRightPanelSaveRef.current = true
    const previousThreadId = restoredRightPanelThreadRef.current
    if (!threadId) {
      restoredRightPanelThreadRef.current = null
      setRightPanelVisible(false)
      setActiveRightPanelTabId(null)
      setRightPanelTabOrder([])
      setWebPanelResource(null)
      setExtraBrowserTabs([])
      setFilesPreviewResource(null)
      setRightPanelTabs([])
      closePanelRef.current()
      browserTabSeqRef.current = 0
      return
    }
    if (previousThreadId !== threadId) {
      setRightPanelTabs([])
      closePanelRef.current()
    }
    const saved = readThreadRightPanelState(threadId, { workFolder: workPanelFolder })
    restoredRightPanelThreadRef.current = threadId
    setRightPanelVisible(saved?.visible ?? false)
    setActiveRightPanelTabId(saved?.activeTabId ?? null)
    setRightPanelTabOrder(saved?.tabOrder ?? [])
    setWebPanelResource(saved?.web ?? null)
    setExtraBrowserTabs(saved?.browserTabs ?? [])
    setRightPanelTabs(saved?.resourceTabs.map((tab) => ({
      id: tab.id,
      kind: 'resource',
      title: tab.title,
      resource: tab.resource,
    })) ?? [])
    setFilesPreviewResource(saved?.filesPreview ?? null)
    browserTabSeqRef.current = saved ? browserTabSeqFromTabs(saved.browserTabs) : 0
    localFileTabSeqRef.current = saved ? localFileTabSeqFromTabs(saved.resourceTabs) : 0
  }, [threadId, workPanelFolder])

  useEffect(() => {
    if (activePanel) {
      setRightPanelVisible(true)
      setRightPanelOpen(true)
    }
  }, [activePanel, setRightPanelOpen])

  useEffect(() => {
    setRightPanelOpen(isPanelOpen)
  }, [isPanelOpen, setRightPanelOpen])

  useEffect(() => {
    setTitleBarRightPanelClick(() => {
      setRightPanelVisible((visible) => !visible)
    })
    return () => setTitleBarRightPanelClick(null)
  }, [setTitleBarRightPanelClick])

  useEffect(() => {
    if (!isPanelOpen) return
    const root = chatViewRootRef.current
    if (!root) return

    const adaptToContainer = () => {
      const containerWidth = root.clientWidth
      setRightPanelWidth((current) => {
        const target = rightPanelRatioRef.current === 0
          ? containerWidth * rightPanelDefaultRatio
          : current
        const next = clampRightPanelWidth(target, containerWidth)
        rightPanelRatioRef.current = next / Math.max(containerWidth, 1)
        if (Math.abs(current - next) < 1) return current
        return next
      })
    }

    adaptToContainer()
    const observer = new ResizeObserver(adaptToContainer)
    observer.observe(root)
    return () => observer.disconnect()
  }, [isPanelOpen])

  const handleRightPanelResizeStart = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    event.preventDefault()
    const root = chatViewRootRef.current
    if (!root) return
    setRightPanelResizing(true)
    const pointerId = event.pointerId
    event.currentTarget.setPointerCapture(pointerId)
    const rect = root.getBoundingClientRect()
    const shell = rightPanelShellRef.current
    const content = rightPanelContentRef.current
    let frame = 0
    let nextWidth = rightPanelWidth

    if (shell) shell.style.transition = 'none'
    if (content) content.style.transition = 'none'

    const applyWidth = () => {
      frame = 0
      if (shell) shell.style.width = `${nextWidth}px`
      if (content) content.style.width = `${nextWidth}px`
    }

    const handlePointerMove = (moveEvent: PointerEvent) => {
      nextWidth = clampRightPanelWidth(rect.right - moveEvent.clientX, rect.width)
      rightPanelRatioRef.current = nextWidth / Math.max(rect.width, 1)
      if (frame === 0) frame = requestAnimationFrame(applyWidth)
    }
    const stopResize = () => {
      if (frame !== 0) {
        cancelAnimationFrame(frame)
        applyWidth()
      }
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('pointerup', stopResize)
      window.removeEventListener('pointercancel', stopResize)
      setRightPanelWidth(nextWidth)
      requestAnimationFrame(() => requestAnimationFrame(() => {
        if (shell) shell.style.transition = ''
        if (content) content.style.transition = ''
        setRightPanelResizing(false)
      }))
    }

    window.addEventListener('pointermove', handlePointerMove)
    window.addEventListener('pointerup', stopResize)
    window.addEventListener('pointercancel', stopResize)
  }, [rightPanelWidth])

  const upsertRightPanelTab = useCallback((tab: RightPanelStoredTab) => {
    setRightPanelTabs((current) => {
      const index = current.findIndex((item) => item.id === tab.id)
      if (index < 0) return [...current, tab]
      const next = [...current]
      next[index] = tab
      return next
    })
    setActiveRightPanelTabId(tab.id)
  }, [])

  const pinLocalFileResource = useCallback((resource: LocalFileResourceRef) => {
    upsertRightPanelTab({
      id: `local-file:${++localFileTabSeqRef.current}`,
      kind: 'resource',
      title: resourceTitle(resource),
      resource,
    })
    setRightPanelVisible(true)
  }, [upsertRightPanelTab])

  const closeRightPanelTab = useCallback((id: string) => {
    if (id === 'web') {
      setWebPanelResource(null)
      setActiveRightPanelTabId('web')
      return
    }
    if (id.startsWith('web:')) {
      setExtraBrowserTabs((current) => current.filter((tab) => tab.id !== id))
      setActiveRightPanelTabId((activeId) => activeId === id ? 'web' : activeId)
      return
    }
    setRightPanelTabs((current) => {
      const index = current.findIndex((item) => item.id === id)
      if (index < 0) return current
      const target = current[index]
      if (target.kind === 'source') setSourcePanelMessageId(null)
      else if (target.kind === 'code' && activePanel?.type === 'code' && activePanel.execution.id === target.execution.id) setCodePanelExecution(null)
      else if (target.kind === 'agent' && activePanel?.type === 'agent' && activePanel.agent.id === target.agent.id) closePanel()
      else if (target.kind === 'resource') {
        if (activePanel?.type === 'resource' && resourceTabId(activePanel.resource) === target.id) closePanel()
        else if (activePanel?.type === 'document' && target.resource.kind === 'artifact' && activePanel.artifact.artifact.key === target.resource.key) closePanel()
      }

      const next = current.filter((item) => item.id !== id)
      setActiveRightPanelTabId((activeId) => {
        if (activeId !== id) return activeId
        return next[index]?.id ?? next[index - 1]?.id ?? (workPanelFolder?.trim() ? 'files' : 'web')
      })
      return next
    })
  }, [activePanel, closePanel, setCodePanelExecution, setSourcePanelMessageId, workPanelFolder])

  const setBrowserResourceForCurrentTab = useCallback((resource: BrowserResourceRef) => {
    const activeId = effectiveRightPanelTabIdRef.current
    if (activeId?.startsWith('web:')) {
      setExtraBrowserTabs((current) => current.map((tab) => (
        tab.id === activeId && !sameBrowserResource(tab.resource, resource) ? { ...tab, resource } : tab
      )))
      setActiveRightPanelTabId(activeId)
      return
    }
    setWebPanelResource((current) => sameBrowserResource(current, resource) ? current : resource)
    setActiveRightPanelTabId('web')
  }, [])

  useEffect(() => {
    if (isSourcePanelOpen && sourcePanelMessageId) {
      upsertRightPanelTab({ id: `source:${sourcePanelMessageId}`, kind: 'source', title: 'Sources', messageId: sourcePanelMessageId })
    }
  }, [isSourcePanelOpen, sourcePanelMessageId, upsertRightPanelTab])

  useEffect(() => {
    if (codePanelExecution) {
      upsertRightPanelTab({ id: `code:${codePanelExecution.id}`, kind: 'code', title: 'Execution', execution: codePanelExecution })
    }
  }, [codePanelExecution, upsertRightPanelTab])

  useEffect(() => {
    if (documentPanelArtifact) {
      const artifact = documentPanelArtifact.artifact
      const resource: ResourceRef = {
        kind: 'artifact',
        key: artifact.key,
        filename: artifact.filename,
        mimeType: artifact.mime_type,
        size: artifact.size,
        title: artifact.title,
      }
      upsertRightPanelTab({
        id: resourceTabId(resource),
        kind: 'resource',
        title: artifact.title ?? artifact.filename,
        resource,
        artifacts: documentPanelArtifact.artifacts,
        runId: documentPanelArtifact.runId,
      })
    }
  }, [documentPanelArtifact, upsertRightPanelTab])

  useEffect(() => {
    if (agentPanelAgent) {
      upsertRightPanelTab({
        id: `agent:${agentPanelAgent.id}`,
        kind: 'agent',
        title: agentPanelAgent.nickname || agentPanelAgent.personaId || 'Agent',
        agent: agentPanelAgent,
      })
    }
  }, [agentPanelAgent, upsertRightPanelTab])

  useEffect(() => {
    if (resourcePanelResource) {
      if (resourcePanelResource.kind === 'browser') {
        setBrowserResourceForCurrentTab(resourcePanelResource)
        return
      }
      const tabId = resourceTabId(resourcePanelResource)
      setRightPanelTabs((current) => {
        const index = current.findIndex((item) => item.id === tabId)
        const previous = index >= 0 ? current[index] : null
        const nextTab: RightPanelStoredTab = {
          id: tabId,
          kind: 'resource',
          title: previous?.kind === 'resource' ? previous.title : resourceTitle(resourcePanelResource),
          resource: resourcePanelResource,
          artifacts: previous?.kind === 'resource' ? previous.artifacts : undefined,
          runId: previous?.kind === 'resource' ? previous.runId : undefined,
        }
        if (index < 0) return [...current, nextTab]
        const next = [...current]
        next[index] = nextTab
        return next
      })
      setActiveRightPanelTabId(tabId)
    }
  }, [resourcePanelResource, setBrowserResourceForCurrentTab])

  const buildStoredPanelTab = useCallback((tab: RightPanelStoredTab): RightPanelTab | null => {
    if (tab.kind === 'resource' && tab.resource.kind === 'browser') return null
    if (tab.kind === 'source') {
      const sources = resolvedMessageSources.get(tab.messageId)
      if (!sources || sources.length === 0) return null
      return {
        id: tab.id,
        kind: tab.kind,
        title: tab.title,
        content: (
          <div style={{ width: '100%', height: '100%', contain: 'layout style' }}>
            <SourcesPanel sources={sources} onClose={() => closeRightPanelTab(tab.id)} />
          </div>
        ),
      }
    }
    if (tab.kind === 'code') {
      return {
        id: tab.id,
        kind: tab.kind,
        title: tab.title,
        content: (
          <div style={{ width: '100%', height: '100%', contain: 'layout style' }}>
            <CodeExecutionPanel execution={tab.execution} onClose={() => closeRightPanelTab(tab.id)} />
          </div>
        ),
      }
    }
    if (tab.kind === 'agent') {
      return {
        id: tab.id,
        kind: tab.kind,
        title: tab.title,
        content: (
          <div style={{ width: '100%', height: '100%', contain: 'layout style' }}>
            <AgentPanel agent={tab.agent} onClose={() => closeRightPanelTab(tab.id)} />
          </div>
        ),
      }
    }
    return {
      id: tab.id,
      kind: tab.kind,
      title: tab.title,
      icon: resourcePanelIcon(tab.resource),
      content: (
        <ResourcePreviewPanel
          resource={tab.resource}
          accessToken={accessToken}
          artifacts={tab.artifacts}
          runId={tab.runId}
          workFolder={workPanelFolder}
          onClose={() => closeRightPanelTab(tab.id)}
          onBuildPlan={handleBuildPlan}
          onOpenModelSettings={() => onOpenSettings('models')}
          onPlanTitleChange={(title) => {
            setRightPanelTabs((current) => {
              const target = current.find((item) => item.id === tab.id && item.kind === 'resource')
              if (!target || target.title === title) return current
              return current.map((item) => (
                item.id === tab.id && item.kind === 'resource' ? { ...item, title } : item
              ))
            })
          }}
        />
      ),
    }
  }, [accessToken, closeRightPanelTab, handleBuildPlan, onOpenSettings, resolvedMessageSources, workPanelFolder])

  const handleWebPanelResourceChange = useCallback((resource: ResourceRef) => {
    if (resource.kind !== 'browser') return
    setWebPanelResource((current) => sameBrowserResource(current, resource) ? current : resource)
  }, [])

  const updateExtraBrowserTabResource = useCallback((id: string, resource: BrowserResourceRef) => {
    setExtraBrowserTabs((current) => current.map((tab) => (
      tab.id === id && !sameBrowserResource(tab.resource, resource) ? { ...tab, resource } : tab
    )))
  }, [])

  // Individual tab memos — when only one dep changes, other tabs stay stable.
  // This prevents e.g. adding a document tab from rebuilding the files tab (incl. LocalFileTree).
  const webPanelTab = useMemo<RightPanelTab>(() => ({
    id: 'web',
    kind: 'web',
    title: webPanelResource ? resourceTitle(webPanelResource) : t.rightPanel.browser,
    closable: !!webPanelResource,
    hideTitle: !webPanelResource,
    icon: <BrowserSiteIcon url={webPanelResource?.url} faviconUrl={webPanelResource?.faviconUrl} size={rightPanelIconSize} />,
    content: (
      <ResourcePreviewPanel
        resource={webPanelResource ?? { kind: 'browser', url: '', title: t.rightPanel.browser }}
        accessToken={accessToken}
        onResourceChange={handleWebPanelResourceChange}
      />
    ),
  }), [webPanelResource, accessToken, handleWebPanelResourceChange, t.rightPanel.browser])

  const extraBrowserPanelTabs = useMemo<RightPanelTab[]>(() =>
    extraBrowserTabs.map((browserTab) => ({
      id: browserTab.id,
      kind: 'web' as const,
      title: browserTab.resource ? resourceTitle(browserTab.resource) : t.rightPanel.browser,
      closable: true,
      hideTitle: !browserTab.resource,
      icon: <BrowserSiteIcon url={browserTab.resource?.url} faviconUrl={browserTab.resource?.faviconUrl} size={rightPanelIconSize} />,
      content: (
        <ResourcePreviewPanel
          resource={browserTab.resource ?? { kind: 'browser', url: '', title: t.rightPanel.browser }}
          accessToken={accessToken}
          onResourceChange={(resource) => {
            if (resource.kind !== 'browser') return
            updateExtraBrowserTabResource(browserTab.id, resource)
          }}
        />
      ),
    })),
  [extraBrowserTabs, accessToken, t.rightPanel.browser, updateExtraBrowserTabResource])

  const filesPanelTab = useMemo<RightPanelTab | null>(() => {
    if (!workPanelFolder?.trim()) return null
    const filesPreviewTitle = filesPreviewResource ? resourceTitle(filesPreviewResource) : t.rightPanel.files
    return {
      id: 'files',
      kind: 'files',
      title: filesPreviewTitle,
      closable: false,
      icon: localFileTabIcon(filesPreviewResource),
      hideTitle: !filesPreviewResource,
      content: (
        <LocalFilesPanel
          rootPath={workPanelFolder}
          accessToken={accessToken}
          previewResource={filesPreviewResource}
          onPreviewResourceChange={setFilesPreviewResource}
          onPinResource={pinLocalFileResource}
        />
      ),
    }
  }, [workPanelFolder, accessToken, filesPreviewResource, pinLocalFileResource, setFilesPreviewResource, t.rightPanel.files])

  const resourcePanelTabs = useMemo<RightPanelTab[]>(() => {
    const result: RightPanelTab[] = []
    for (const tab of rightPanelTabs) {
      const rendered = buildStoredPanelTab(tab)
      if (rendered) result.push(rendered)
    }
    return result
  }, [rightPanelTabs, buildStoredPanelTab])

  const rightPanelRenderedTabs = useMemo<RightPanelTab[]>(() => {
    const tabs: RightPanelTab[] = [webPanelTab, ...extraBrowserPanelTabs]
    if (filesPanelTab) tabs.push(filesPanelTab)
    tabs.push(...resourcePanelTabs)
    return tabs
  }, [webPanelTab, extraBrowserPanelTabs, filesPanelTab, resourcePanelTabs])

  const effectiveRightPanelTabId = rightPanelRenderedTabs.some((tab) => tab.id === activeRightPanelTabId)
    ? activeRightPanelTabId
    : rightPanelRenderedTabs[0]?.id ?? null
  isPanelOpenRef.current = isPanelOpen
  effectiveRightPanelTabIdRef.current = effectiveRightPanelTabId

  const rightPanelSaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (skipRightPanelSaveRef.current) {
      skipRightPanelSaveRef.current = false
      return
    }
    if (!threadId || restoredRightPanelThreadRef.current !== threadId) return
    if (rightPanelSaveTimerRef.current) clearTimeout(rightPanelSaveTimerRef.current)
    rightPanelSaveTimerRef.current = setTimeout(() => {
      writeThreadRightPanelState(threadId, {
        visible: rightPanelVisible,
        activeTabId: effectiveRightPanelTabId,
        tabOrder: rightPanelTabOrder,
        web: webPanelResource,
        browserTabs: extraBrowserTabs,
        resourceTabs: rightPanelTabs
          .filter((tab): tab is Extract<RightPanelStoredTab, { kind: 'resource' }> & { resource: Exclude<ResourceRef, BrowserResourceRef> } => (
            tab.kind === 'resource' && tab.resource.kind !== 'browser'
          ))
          .map((tab) => ({
            id: tab.id,
            title: tab.title,
            resource: tab.resource,
          })),
        filesPreview: filesPreviewResource,
      }, { workFolder: workPanelFolder })
    }, 300)
    return () => {
      if (rightPanelSaveTimerRef.current) clearTimeout(rightPanelSaveTimerRef.current)
    }
  }, [
    effectiveRightPanelTabId,
    extraBrowserTabs,
    filesPreviewResource,
    rightPanelTabs,
    rightPanelTabOrder,
    rightPanelVisible,
    threadId,
    webPanelResource,
    workPanelFolder,
  ])

  const rightPanelAddOptions = useMemo(() => [
    {
      id: 'web',
      label: t.rightPanel.browser,
      icon: <Globe2 size={14} />,
      onSelect: () => {
        const id = `web:${browserTabSeqRef.current + 1}`
        browserTabSeqRef.current += 1
        setExtraBrowserTabs((current) => [...current, { id, resource: null }])
        setRightPanelVisible(true)
        setActiveRightPanelTabId(id)
      },
    },
  ], [t.rightPanel.browser])

  const openCodePanel = useCallback((ce: CodeExecution) => {
    const tabId = `code:${ce.id}`
    if (codePanelExecutionIdRef.current === ce.id) {
      if (isPanelOpenRef.current && effectiveRightPanelTabIdRef.current === tabId) {
        closeRightPanelTab(tabId)
        setRightPanelVisible(false)
      } else {
        setRightPanelVisible(true)
        setActiveRightPanelTabId(tabId)
      }
      return
    }
    openCodePanelState(ce)
  }, [closeRightPanelTab, openCodePanelState])

  const openDocumentPanel = useCallback((artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => {
    stabilizeDocumentPanelScroll(options?.trigger)
    const tabId = `resource:artifact:${artifact.key}`
    if (documentPanelArtifactKeyRef.current === artifact.key) {
      if (isPanelOpenRef.current && effectiveRightPanelTabIdRef.current === tabId) {
        closeRightPanelTab(tabId)
        setRightPanelVisible(false)
      } else {
        setRightPanelVisible(true)
        setActiveRightPanelTabId(tabId)
      }
      return
    }
    openDocumentPanelState({
      artifact,
      artifacts: options?.artifacts ?? [],
      runId: options?.runId,
    })
  }, [closeRightPanelTab, openDocumentPanelState, stabilizeDocumentPanelScroll])

  const openResourcePanel = useCallback((resource: ResourceRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => {
    stabilizeDocumentPanelScroll(options?.trigger)
    if (resource.kind === 'browser') {
      setBrowserResourceForCurrentTab(resource)
      setRightPanelVisible(true)
      return
    }
    const tabId = resourceTabId(resource)
    const current = resourcePanelResourceRef.current
    if (current && resourceTabId(current) === tabId) {
      if (isPanelOpenRef.current && effectiveRightPanelTabIdRef.current === tabId) {
        closeRightPanelTab(tabId)
        setRightPanelVisible(false)
      } else {
        upsertRightPanelTab({
          id: tabId,
          kind: 'resource',
          title: resourceTitle(resource),
          resource,
          artifacts: options?.artifacts,
          runId: options?.runId,
        })
        setRightPanelVisible(true)
        setActiveRightPanelTabId(tabId)
      }
      return
    }
    upsertRightPanelTab({
      id: tabId,
      kind: 'resource',
      title: resourceTitle(resource),
      resource,
      artifacts: options?.artifacts,
      runId: options?.runId,
    })
    openResourcePanelState(resource)
  }, [closeRightPanelTab, openResourcePanelState, setBrowserResourceForCurrentTab, stabilizeDocumentPanelScroll, upsertRightPanelTab])

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
      }
    })
    learningModeUpdateRef.current = updatePromise
    await updatePromise
  }, [accessToken, onThreadUpserted, setError, threadId])

  const hasMessages = messages.length > 0
  const inputHorizontalPadding = isWorkMode
    ? chatInputPadding.work
    : (isPanelOpen ? chatInputPadding.panelOpen : chatInputPadding.panelClosed)
  const messageHorizontalPadding = isPanelOpen ? chatContentPadding.panelOpen : chatContentPadding.panelClosed

  const chatInputEl = useMemo(() => (
    <ChatInput
      key={`${threadId ?? '__no_thread__'}:${effectiveAppMode}:${isSearchThread ? 'search' : 'default'}`}
      ref={chatInputRef}
      onSubmit={handleSend}
      onCancel={handleCancel}
      placeholder={isStreaming ? t.followUpPlaceholder : t.replyPlaceholder}
      disabled={false}
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
      onToggleLearningMode={handleToggleLearningMode}
    />
  ), [attachments, isStreaming, canCancel, cancelSubmitting, effectiveAppMode, isSearchThread, hasMessages, messagesLoading, threadId, accessToken, me?.id, t.followUpPlaceholder, t.replyPlaceholder, handleSend, handleCancel, handleAttachFiles, handlePasteContent, handleRemoveAttachment, handleAsrError, handlePersonaChange, onOpenSettings, editingQueuedPromptId, cancelQueuedPromptEdit, currentThread?.collaboration_mode, currentThread?.learning_mode_enabled, handleTogglePlanMode, handleToggleLearningMode])

  const renderLiveCopItems = useCallback((
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
  }, [
    accessToken,
    activeRunId,
    baseUrl,
    codePanelExecution?.id,
    currentRunCopHeaderOverride,
    dedupedTopLevelCodeExecutions,
    handleArtifactAction,
    isStreaming,
    isWorkMode,
    liveAssistantTurn,
    liveRunUiActive,
    liveSegments,
    openAgentPanelState,
    openCodePanel,
    preserveLiveRunUi,
    searchSteps,
    streamingArtifacts,
    terminalRunHandoffStatus,
    thinkingHint,
    topLevelFileOps,
    topLevelSubAgents,
    topLevelWebFetches,
  ])

  const renderLiveCopSegment = useCallback((
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
  }, [renderLiveCopItems])

  const handleLiveCheckInSubmit = useCallback(() => {
    void handleCheckInSubmit()
  }, [handleCheckInSubmit])

  const handleIncognitoDividerComplete = useCallback(() => {
    if (isAtBottomRef.current) {
      activateAnchor()
    }
  }, [activateAnchor])

  const lastTurnChildren = useMemo(() => (
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
      workFolder={workPanelFolder}
      baseUrl={baseUrl}
      thinkingHint={thinkingHint}
      visibleStreamingWidgets={visibleStreamingWidgets}
      visibleStreamingArtifacts={visibleStreamingArtifacts}
      injectionBlocked={injectionBlocked}
      awaitingInput={awaitingInput}
      checkInDraft={checkInDraft}
      checkInSubmitting={checkInSubmitting}
      onCheckInDraftChange={setCheckInDraft}
      onCheckInSubmit={handleLiveCheckInSubmit}
      pendingIncognito={pendingIncognito}
      incognitoDividerText={t.incognitoForkDivider}
      onIncognitoDividerComplete={handleIncognitoDividerComplete}
      terminalRunHandoffStatus={terminalRunHandoffStatus}
      terminalRunDisplayId={terminalRunDisplayId}
      showRunDetailButton={showRunDetailButton}
      setRunDetailPanelRunId={setRunDetailPanelRunId}
      onOpenDocument={openDocumentPanel}
      onOpenResource={openResourcePanel}
      onOpenCodeExecution={openCodePanel}
      onOpenSubAgent={openAgentPanelState}
      onArtifactAction={handleArtifactAction}
      renderLiveCopItems={renderLiveCopItems}
      renderLiveCopSegment={renderLiveCopSegment}
      bottomRef={bottomRef}
    />
  ), [
    accessToken,
    activeRunId,
    allStreamItemsForUi,
    awaitingInput,
    baseUrl,
    checkInDraft,
    checkInSubmitting,
    codePanelExecution?.id,
    dedupedTopLevelCodeExecutions,
    handleArtifactAction,
    handleIncognitoDividerComplete,
    handleLiveCheckInSubmit,
    injectionBlocked,
    isWorkMode,
    leadingLiveCop,
    liveAssistantTurn,
    liveRunUiActive,
    liveRunUiVisible,
    liveSegments,
    openAgentPanelState,
    openCodePanel,
    openDocumentPanel,
    openResourcePanel,
    pendingIncognito,
    preserveLiveRunUi,
    renderLiveCopItems,
    renderLiveCopSegment,
    showPendingThinkingShell,
    showRunDetailButton,
    t.incognitoForkDivider,
    terminalRunDisplayId,
    terminalRunHandoffStatus,
    thinkingHint,
    topLevelFileOps,
    topLevelSubAgents,
    topLevelWebFetches,
    trailingLiveSegments,
    visibleStreamingArtifacts,
    visibleStreamingWidgets,
    workPanelFolder,
  ])

  // Message list area: memoized so panel state changes (activePanel/isPanelOpen)
  // don't force the full message tree to reconcile. Padding values are driven by
  // CSS custom properties set on the parent div.
  const messageListArea = useMemo(() => (
    <div
      ref={setScrollContainerEl}
      onScroll={handleScrollContainerScroll}
      className="theme-surface-page chat-scroll-hidden relative flex-1 min-h-0 overflow-y-auto bg-[var(--c-bg-page)] [scrollbar-gutter:stable]"
      style={{ contain: 'layout paint style' }}
    >
      <div
        style={{
          maxWidth: isWorkMode ? 1000 : 800,
          margin: '0 auto',
          paddingTop: '50px',
          paddingRight: 'calc(var(--chat-message-horizontal-padding) + var(--main-content-axis-padding-right, 0px))',
          paddingBottom: 'var(--chat-input-area-height)',
          paddingLeft: 'calc(var(--chat-message-horizontal-padding) + var(--main-content-axis-padding-left, 0px))',
          gap: isWorkMode ? 0 : undefined,
          transition: `padding ${rightPanelLayoutTransitionCss}`,
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
              ref={messageListRef}
              scrollParent={scrollParent}
              isWorkMode={isWorkMode}
              lastTurnStartIdx={lastTurnStartIdx}
              lastTurnRef={lastUserMsgRef}
              lastUserPromptRef={lastUserPromptRef}
              lastTurnChildren={lastTurnChildren}
              showRunDetailButton={showRunDetailButton}
              currentRunCopHeaderOverride={currentRunCopHeaderOverride}
              handleRetryUserMessage={handleRetryUserMessage}
              handleEditMessage={handleEditMessage}
              handleFork={handleFork}
              handleArtifactAction={handleArtifactAction}
              openDocumentPanel={openDocumentPanel}
              openResourcePanel={openResourcePanel}
              openCodePanel={openCodePanel}
              openAgentPanel={openAgentPanelState}
              sourcePanelMessageId={sourcePanelMessageId}
              setRunDetailPanelRunId={setRunDetailPanelRunId}
              clearUserEnterAnimation={clearUserEnterAnimation}
              workFolder={workPanelFolder}
              />
            </CopTimelineLocalExpansionProvider>
          </>
        )}
      </div>
      <div ref={spacerRef} style={{ flexShrink: 0, overflowAnchor: 'none' }} />
    </div>
  ), [
    contextCompactBar,
    currentRunCopHeaderOverride,
    handleArtifactAction,
    handleEditMessage,
    handleFork,
    handleRetryUserMessage,
    handleScrollContainerScroll,
    isWorkMode,
    lastTurnChildren,
    lastTurnStartIdx,
    messagesLoading,
    openAgentPanelState,
    openCodePanel,
    openDocumentPanel,
    openResourcePanel,
    scrollParent,
    setScrollContainerEl,
    sourcePanelMessageId,
    setRunDetailPanelRunId,
    stabilizeDocumentPanelScroll,
    t.desktopSettings.chatCompactBannerDone,
    t.desktopSettings.chatCompactBannerRunning,
    t.desktopSettings.chatCompactBannerTrim,
    t.desktopSettings.chatCompactBannerLlmFailed,
    workPanelFolder,
  ])

  return (
    <div ref={chatViewRootRef} className="theme-surface-page theme-chat-surface relative flex min-w-0 flex-1 overflow-hidden bg-[var(--c-bg-page)]">
      {/* Chat column + right panel: starts below the desktop Chat/Work titlebar. */}
      <div className="relative flex flex-1 min-h-0 min-w-0">
        <div
          className="relative flex flex-1 min-w-0 flex-col"
          style={{
            '--chat-message-horizontal-padding': messageHorizontalPadding,
            minWidth: isPanelOpen ? chatViewMinWidth : 0,
            transition: `min-width ${rightPanelLayoutTransitionCss}`,
          } as React.CSSProperties}
        >
          <ChatTitleMenu />
          <div className="pointer-events-none absolute inset-x-0 top-[60px] z-10 h-10" style={{ background: 'linear-gradient(to bottom, var(--c-chat-bg-gradient-stop, var(--c-bg-page-gradient-stop, var(--c-bg-page))), transparent)' }} />
          {/* 消息列表 */}
          {messageListArea}

      {/* 输入区域 */}
      <div
        ref={inputAreaRef}
        style={{
          '--chat-input-horizontal-padding': inputHorizontalPadding,
          maxWidth: isWorkMode ? 1000 : 1200,
          margin: '0 auto',
          paddingTop: '12px',
          paddingRight: 'calc(var(--chat-input-horizontal-padding) + var(--main-content-axis-padding-right, 0px))',
          paddingBottom: '8px',
          paddingLeft: 'calc(var(--chat-input-horizontal-padding) + var(--main-content-axis-padding-left, 0px))',
          position: 'absolute',
          bottom: 0,
          left: 0,
          right: 0,
          zIndex: 10,
          background: 'linear-gradient(to bottom, transparent 0%, var(--c-chat-bg-gradient-stop, var(--c-bg-page-gradient-stop, var(--c-bg-page))) 24px)',
          transition: `padding ${rightPanelLayoutTransitionCss}`,
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
              left: 'calc(var(--chat-input-horizontal-padding) + var(--main-content-axis-padding-left, 0px))',
              right: 'calc(var(--chat-input-horizontal-padding) + var(--main-content-axis-padding-right, 0px))',
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

      <motion.div
        ref={rightPanelShellRef}
        className="relative flex-shrink-0 overflow-hidden bg-[var(--c-bg-page)]"
        initial={false}
        animate={{
          width: isPanelOpen ? rightPanelWidth : 0,
          opacity: isPanelOpen ? 1 : 0,
          pointerEvents: isPanelOpen ? 'auto' : 'none',
        }}
        transition={rightPanelResizing ? { duration: 0 } : rightPanelLayoutTransition}
        style={{
          borderLeft: isPanelOpen ? '0.5px solid var(--c-border-subtle)' : 'none',
          willChange: 'width, opacity',
        }}
      >
        <div ref={rightPanelContentRef} style={{ width: rightPanelWidth, height: '100%' }}>
          <div
            role="separator"
            aria-orientation="vertical"
            title="Resize"
            onPointerDown={handleRightPanelResizeStart}
            className="absolute inset-y-0 left-0 z-10 w-2 cursor-col-resize"
          />
          <RightPanel
            tabs={rightPanelRenderedTabs}
            activeTabId={effectiveRightPanelTabId}
            onSelectTab={setActiveRightPanelTabId}
            onCloseTab={closeRightPanelTab}
            tabOrder={rightPanelTabOrder}
            onTabOrderChange={setRightPanelTabOrder}
            addOptions={rightPanelAddOptions}
            addLabel={t.rightPanel.newTab}
            emptyLabel={t.rightPanel.empty}
          />
        </div>
      </motion.div>
      </div>

      {shareModalEl}
      {runDetailEl}
      {showDebugPanel && <DebugTrigger />}
    </div>
  )
})
