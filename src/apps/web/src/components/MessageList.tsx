import { memo, Fragment, type ComponentProps, useCallback, useMemo } from 'react'
import { MessageBubble } from './MessageBubble'
import { CopTimeline, type WebSearchPhaseStep } from './cop-timeline/CopTimeline'
import { CopSegmentBlocks } from './CopSegmentBlocks'
import { TopLevelCopToolBlock } from './TopLevelCopToolBlock'
import { AssistantActionBar } from './messagebubble/AssistantMessage'
import { MarkdownRenderer } from './MarkdownRenderer'
import { WidgetBlock } from './WidgetBlock'
import { IncognitoDivider } from './IncognitoDivider'
import { useLocale } from '../contexts/LocaleContext'
import { useChatSession } from '../contexts/chat-session'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { isLocalTerminalMessage, useMessageStore } from '../contexts/message-store'
import { useMessageMeta } from '../contexts/message-meta'
import { useStream } from '../contexts/stream'
import { usePanelActions, usePanels } from '../contexts/panels'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { AgentMessage } from '../agent-ui'
import { copTimelinePayloadForSegment, type CopTimelinePayload, type TodoWriteRef } from '../copSegmentTimeline'
import { buildResolvedPool, EMPTY_POOL, buildFallbackSegments } from '../copSubSegment'
import { assistantTurnPlainText, splitWorkGroup, type AssistantTurnSegment, type WorkGroup as WorkGroupType } from '../assistantTurnSegments'
import { WorkGroup } from './WorkGroup'
import { resolveMessageSourcesForRender } from './chatSourceResolver'
import { createThreadShare } from '../api'
import { readMessageTerminalStatus, readMessageWidgets, type ArtifactRef, type SubAgentRef, type WebSource, type WidgetRef } from '../storage'
import { useLocation } from 'react-router-dom'
import type { CodeExecution } from './CodeExecutionCard'
import type { ResourceRef } from './resource-preview/types'
import {
  turnHasCopThinkingItems,
  widgetToolCallIdsPlacedInTurn,
  historicWidgetsForCop,
} from '../lib/chat-helpers'
import { isLocalUserMessage, messageClientMessageId } from '../messageContent'

type LocationState = {
  initialRunId?: string
  isSearch?: boolean
  isIncognitoFork?: boolean
  forkBaseCount?: number
  userEnterMessageId?: string
} | null

export const MessageList = memo(function MessageList({
  lastTurnRef,
  lastUserPromptRef,
  lastTurnChildren,
  lastTurnStartIdx,
  handleRetryUserMessage,
  handleEditMessage,
  handleFork,
  handleArtifactAction,
  openDocumentPanel,
  openResourcePanel,
  openCodePanel,
  openAgentPanel,
  showRunDetailButton,
  sourcePanelMessageId,
  setRunDetailPanelRunId,
  currentRunCopHeaderOverride,
  clearUserEnterAnimation,
  isWorkMode,
  workFolder,
}: {
  lastTurnRef: React.RefObject<HTMLDivElement | null>
  lastUserPromptRef: React.RefObject<HTMLDivElement | null>
  lastTurnChildren?: React.ReactNode
  lastTurnStartIdx: number
  handleRetryUserMessage: (message: AgentMessage) => void
  handleEditMessage: (message: AgentMessage, newContent: string) => void
  handleFork: (messageId: string) => Promise<void>
  handleArtifactAction: ComponentProps<typeof WidgetBlock>['onAction']
  openDocumentPanel: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  openResourcePanel: (resource: ResourceRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  openCodePanel: (ce: CodeExecution) => void
  openAgentPanel: (agent: SubAgentRef) => void
  showRunDetailButton: boolean
  sourcePanelMessageId: string | null
  setRunDetailPanelRunId: (runId: string | null) => void
  currentRunCopHeaderOverride: (params: {
    title?: string | null
    steps: WebSearchPhaseStep[]
    hasCodeExecutions: boolean
    hasSubAgents: boolean
    hasFileOps: boolean
    hasWebFetches: boolean
    hasGenericTools: boolean
    hasThinking: boolean
    handoffStatus?: 'completed' | 'cancelled' | 'interrupted' | 'failed' | null
  }) => string | undefined
  clearUserEnterAnimation: () => void
  isWorkMode?: boolean
  workFolder?: string | null
}) {
  const { threadId, isSearchThread } = useChatSession()
  const { accessToken } = useAuth()
  const { t } = useLocale()
  const run = useRunLifecycle()
  const msgs = useMessageStore()
  const meta = useMessageMeta()
  const stream = useStream()
  const { activePanel, shareModal } = usePanels()
  const { closePanel, openSourcePanel, setShareState } = usePanelActions()
  const threadList = useThreadList()
  const location = useLocation()
  const locationState = location.state as LocationState
  const baseUrl = apiBaseUrl()

  const messages = msgs.messages
  const isStreaming = run.isStreaming
  const sending = run.sending
  const terminalRunDisplayId = run.terminalRunDisplayId
  const terminalRunHandoffStatus = run.terminalRunHandoffStatus
  const terminalRunCoveredRunIds = run.terminalRunCoveredRunIds
  const userEnterMessageId = msgs.userEnterMessageId
  const privateThreadIds = threadList.privateThreadIds

  const hasCurrentRunHandoffUi =
    stream.preserveLiveRunUi &&
    terminalRunDisplayId != null &&
    (
      (stream.liveAssistantTurn?.segments.length ?? 0) > 0 ||
      stream.topLevelCodeExecutions.length > 0 ||
      stream.topLevelSubAgents.length > 0 ||
      stream.topLevelFileOps.length > 0 ||
      stream.topLevelWebFetches.length > 0 ||
      stream.streamingArtifacts.length > 0
    )

  const coveredRunIdsForHistory = useMemo(() => {
    const covered = new Set<string>()
    for (const msg of messages) {
      if (msg.role !== 'assistant') continue
      const coveredRunIds = meta.getMeta(msg.id)?.coveredRunIds ?? []
      for (const runId of coveredRunIds) {
        if (runId.trim() !== '') covered.add(runId.trim())
      }
    }
    if (hasCurrentRunHandoffUi) {
      for (const runId of terminalRunCoveredRunIds) {
        if (runId.trim() !== '') covered.add(runId.trim())
      }
    }
    return covered
  }, [hasCurrentRunHandoffUi, messages, meta, terminalRunCoveredRunIds])

  const resolvedMessageSources = resolveMessageSourcesForRender(messages, (() => {
    const map = new Map<string, WebSource[]>()
    for (const msg of messages) {
      if (msg.role !== 'assistant') continue
      const m = meta.getMeta(msg.id)
      if (m?.sources) map.set(msg.id, m.sources)
    }
    return map
  })())

  const codePanelExecutionId = activePanel?.type === 'code' ? activePanel.execution.id : null

  const sharingMessageId = shareModal.sharingMessageId
  const sharedMessageId = shareModal.sharedMessageId

  const createShareForMessage = useCallback((messageId: string) => {
    if (!threadId || sharingMessageId) return
    setShareState(messageId, null)
    createThreadShare(accessToken, threadId, 'public')
      .then((share) => {
        const url = `${window.location.origin}/s/${share.token}`
        void navigator.clipboard.writeText(url)
        setShareState(null, messageId)
        setTimeout(() => setShareState(null, null), 1500)
      })
      .catch(() => {
        setShareState(null, null)
      })
  }, [threadId, sharingMessageId, accessToken, setShareState])

  // Pre-build stable callbacks per message to make MessageBubble's React.memo effective.
  // Rebuilds only when messages, streaming state, or parent callbacks change —
  // not on every bumpSnapshot/liveAssistantTurn change during streaming.
  const bubbleCallbacksByMessageId = useMemo(() => {
    const map = new Map<string, {
      onRetry?: () => void
      onEdit?: (newContent: string) => void
      onFork?: () => void
      onShare?: () => void
      onOpenDocument?: typeof openDocumentPanel
      onOpenResource?: typeof openResourcePanel
      onViewRunDetail?: () => void
    }>()

    for (const msg of messages) {
      if (msg.role === 'user' && !isStreaming && !sending) {
        map.set(msg.id, {
          onRetry: () => handleRetryUserMessage(msg),
          ...(!isLocalUserMessage(msg) ? { onEdit: (newContent: string) => handleEditMessage(msg, newContent) } : {}),
        })
      } else if (msg.role === 'assistant') {
        const callbacks: {
          onFork?: () => void
          onShare?: () => void
          onOpenDocument?: typeof openDocumentPanel
          onOpenResource?: typeof openResourcePanel
          onViewRunDetail?: () => void
        } = {
          onOpenDocument: openDocumentPanel,
          onOpenResource: openResourcePanel,
        }
        if (!isStreaming && !sending) {
          callbacks.onFork = () => { void handleFork(msg.id) }
          if (threadId && !privateThreadIds.has(threadId)) {
            callbacks.onShare = () => createShareForMessage(msg.id)
          }
        }
        if (showRunDetailButton && msg.streamId) {
          callbacks.onViewRunDetail = () => setRunDetailPanelRunId(msg.streamId!)
        }
        map.set(msg.id, callbacks)
      }
    }
    return map
  }, [messages, isStreaming, sending, threadId, privateThreadIds, handleRetryUserMessage, handleEditMessage, handleFork, openDocumentPanel, openResourcePanel, showRunDetailButton, setRunDetailPanelRunId, createShareForMessage])

  // Pre-compute cop timeline payloads to avoid calling copTimelinePayloadForSegment
  // on every render for every message with cop segments.
  // Uses meta.getMeta (stable ref-read) so it does not rebuild when meta state bumps
  // during streaming — only when messages actually change.
  const copPayloadsByMessageId = useMemo(() => {
    const getMeta = meta.getMeta
    const map = new Map<string, {
      payloads: Map<string, CopTimelinePayload>
      turnTodoWrites: TodoWriteRef[]
      histWidgetsMap: Map<string, WidgetRef[]>
    }>()

    for (const msg of messages) {
      if (msg.role !== 'assistant') continue
      const msgMeta = getMeta(msg.id)
      const historicalTurn = msgMeta?.assistantTurn
      if (!historicalTurn || historicalTurn.segments.length === 0) continue

      const msgWidgetsRaw = msgMeta?.widgets ?? readMessageWidgets(msg.id) ?? undefined

      const timelinePools = {
        codeExecutions: msgMeta?.codeExecutions ?? undefined,
        fileOps: msgMeta?.fileOps,
        webFetches: msgMeta?.webFetches,
        subAgents: msgMeta?.subAgents,
        searchSteps: msgMeta?.searchSteps ?? [],
        sources: [] as WebSource[],
      }

      const payloads = new Map<string, CopTimelinePayload>()
      const turnTodoWrites: TodoWriteRef[] = []
      const histWidgetsMap = new Map<string, WidgetRef[]>()

      for (let si = 0; si < historicalTurn.segments.length; si++) {
        const seg = historicalTurn.segments[si]!
        if (seg.type === 'cop') {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const payload = copTimelinePayloadForSegment(seg, timelinePools as any)
          payloads.set(String(si), payload)
          if (payload.todoWrites) turnTodoWrites.push(...payload.todoWrites)
          const histWidgets = historicWidgetsForCop(seg, msgWidgetsRaw)
          histWidgetsMap.set(String(si), histWidgets)
        }
      }

      map.set(msg.id, { payloads, turnTodoWrites, histWidgetsMap })
    }
    return map
    // meta.getMeta is a stable useCallback (empty deps), intentionally excluded
    // to avoid rebuilds when meta state changes during streaming.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messages])

  const renderMessage = (msg: AgentMessage, idx: number) => {
    const hideTerminalRunMessage =
      msg.role === 'assistant' &&
      !isLocalTerminalMessage(msg) &&
      (
        (hasCurrentRunHandoffUi && terminalRunDisplayId != null && msg.streamId === terminalRunDisplayId) ||
        (msg.streamId != null && coveredRunIdsForHistory.has(msg.streamId))
      )
    if (hideTerminalRunMessage) return null

    const msgMeta = msg.role === 'assistant' ? meta.getMeta(msg.id) : undefined
    const resolvedSources = msg.role === 'assistant' ? resolvedMessageSources.get(msg.id) : undefined
    const isCurrentTerminalRunMessage =
      msg.role === 'assistant' &&
      terminalRunDisplayId != null &&
      msg.streamId === terminalRunDisplayId
    const persistedTerminalStatus =
      msg.role === 'assistant' ? readMessageTerminalStatus(msg.id) : null
    const effectiveTerminalStatus =
      isCurrentTerminalRunMessage ? terminalRunHandoffStatus : persistedTerminalStatus
    const displayTerminalStatus =
      effectiveTerminalStatus === 'running' ? null : effectiveTerminalStatus
    const canShowSources = !!(resolvedSources && resolvedSources.length > 0)
    const historicalTurn = msgMeta?.assistantTurn
    const hasAssistantTurn = !!(historicalTurn && historicalTurn.segments.length > 0)
    const historicalSegments = historicalTurn?.segments ?? []
    const msgWidgetsRaw = msg.role === 'assistant'
      ? (msgMeta?.widgets ?? readMessageWidgets(msg.id) ?? undefined)
      : undefined
    const currentRunMessageLive =
      isCurrentTerminalRunMessage &&
      !hasCurrentRunHandoffUi &&
      (isStreaming || effectiveTerminalStatus == null)
    const bubbleWidgets =
      msg.role === 'assistant' && historicalTurn && historicalTurn.segments.length > 0
        ? msgWidgetsRaw?.filter((w) => !widgetToolCallIdsPlacedInTurn(historicalTurn, msgWidgetsRaw).has(w.id))
        : msgWidgetsRaw

    const messageCodeExecutions = msg.role === 'assistant' ? msgMeta?.codeExecutions as CodeExecution[] | undefined : undefined
    const hasMessageCodeExecutions = !!(messageCodeExecutions && messageCodeExecutions.length > 0)
    const messageSubAgents = msg.role === 'assistant' ? msgMeta?.subAgents : undefined
    const messageSearchSteps = msg.role === 'assistant' ? msgMeta?.searchSteps : undefined
    const timelineSteps = messageSearchSteps ?? []
    const messageFileOps = msg.role === 'assistant' ? msgMeta?.fileOps : undefined
    const messageWebFetches = msg.role === 'assistant' ? msgMeta?.webFetches : undefined
    const msgThinking = msg.role === 'assistant' ? msgMeta?.thinking : undefined
    const durationMs = historicalTurn?.durationMs ?? 0
    const workGroupSplit = hasAssistantTurn ? splitWorkGroup(historicalSegments, durationMs) : { workGroup: null as WorkGroupType | null, finalText: null as string | null }
    const bubbleCallbacks = bubbleCallbacksByMessageId.get(msg.id)
    return (
      <div
        key={messageClientMessageId(msg) ?? msg.id}
        ref={msg.role === 'user' && idx === lastTurnStartIdx ? lastUserPromptRef : undefined}
        className="group/turn"
      >
        {msg.role === 'assistant' && hasAssistantTurn && (
          <div style={{ marginBottom: '6px', display: 'flex', flexDirection: 'column', gap: 0, maxWidth: isWorkMode ? undefined : '663px' }}>
            {!isSearchThread &&
              msgThinking != null &&
              msgThinking.thinkingText.trim() !== '' &&
              !turnHasCopThinkingItems(historicalTurn!) && (
                <CopTimeline
                  key={`${msg.id}-legacy-thinking`}
                  segments={[]}
                  pool={EMPTY_POOL}
                  isComplete
                  thinkingOnly={{ markdown: msgThinking.thinkingText, live: false, durationSec: 0 }}
                  accessToken={accessToken}
                  baseUrl={baseUrl}
                  typography={isWorkMode ? 'work' : 'default'}
                />
              )}
            {(() => {
              const precomputed = copPayloadsByMessageId.get(msg.id)
              const timelinePools = {
                codeExecutions: messageCodeExecutions,
                fileOps: messageFileOps,
                webFetches: messageWebFetches,
                subAgents: messageSubAgents,
                searchSteps: messageSearchSteps ?? [],
                sources: resolvedSources ?? [],
              }

              const renderSegment = (seg: AssistantTurnSegment, si: number, segments: AssistantTurnSegment[], isLive: boolean) => {
                if (seg.type === 'text') {
                  return (
                    <MarkdownRenderer
                      key={`${msg.id}-at-${si}`}
                      content={seg.content}
                      webSources={resolvedSources}
                      artifacts={msgMeta?.artifacts}
                      accessToken={accessToken}
                      runId={msg.streamId ?? undefined}
                      workFolder={workFolder}
                      onOpenDocument={openDocumentPanel}
                      onOpenResource={openResourcePanel}
                      typography={isWorkMode ? 'work' : 'default'}
                      trimTrailingMargin={
                        segments[si + 1] == null ||
                        segments[si + 1]?.type === 'cop'
                      }
                    />
                  )
                }

                const turnTodoWrites = precomputed?.turnTodoWrites ?? historicalSegments
                  .flatMap((entry) => entry.type === 'cop'
                    ? copTimelinePayloadForSegment(entry, timelinePools).todoWrites ?? []
                    : [])
                const payload = precomputed?.payloads.get(String(si)) ?? copTimelinePayloadForSegment(seg, timelinePools)
                const histWidgets = precomputed?.histWidgetsMap.get(String(si)) ?? historicWidgetsForCop(seg, msgWidgetsRaw)

                const timelineTitleOverride = displayTerminalStatus != null
                  ? currentRunCopHeaderOverride({
                      title: seg.title,
                      steps: payload.steps,
                      hasCodeExecutions: !!(payload.codeExecutions && payload.codeExecutions.length > 0),
                      hasSubAgents: !!(payload.subAgents && payload.subAgents.length > 0),
                      hasFileOps: !!(payload.fileOps && payload.fileOps.length > 0),
                      hasWebFetches: !!(payload.webFetches && payload.webFetches.length > 0),
                      hasGenericTools: !!(payload.genericTools && payload.genericTools.length > 0),
                      hasThinking: seg.items.some((item) => item.kind === 'thinking'),
                      handoffStatus: displayTerminalStatus,
                    })
                  : seg.title?.trim() || undefined

                const entryComplete = !isLive
                const promotedNodes = [(
                  <CopSegmentBlocks
                    key={`${msg.id}-timeline-${si}`}
                    segment={seg}
                    keyPrefix={`${msg.id}-timeline-${si}`}
                    {...timelinePools}
                    isComplete={entryComplete}
                    live={isLive}
                    headerOverride={timelineTitleOverride}
                    compactNarrativeEnd={idx < lastTurnStartIdx}
                    onOpenCodeExecution={openCodePanel}
                    onOpenSubAgent={openAgentPanel}
                    activeCodeExecutionId={codePanelExecutionId ?? undefined}
                    accessToken={accessToken}
                    baseUrl={baseUrl}
                    typography={isWorkMode ? 'work' : 'default'}
                    todoWritesForFinalDisplay={turnTodoWrites}
                  />
                )]

                return (
                  <Fragment key={`${msg.id}-acw-${si}`}>
                    {promotedNodes}
                    {histWidgets.map((w) => (
                      <WidgetBlock
                        key={w.id}
                        html={w.html}
                        title={w.title}
                        complete
                        compact
                        onAction={handleArtifactAction}
                      />
                    ))}
                  </Fragment>
                )
              }

              if (workGroupSplit.workGroup != null) {
                return (
                  <>
                    <WorkGroup durationMs={durationMs}>
                      {workGroupSplit.workGroup.segments.map((seg, si) =>
                        renderSegment(seg, si, workGroupSplit.workGroup!.segments, false)
                      )}
                    </WorkGroup>
                    {workGroupSplit.finalText != null && (
                      <MarkdownRenderer
                        key={`${msg.id}-final`}
                        content={workGroupSplit.finalText}
                        webSources={resolvedSources}
                        artifacts={msgMeta?.artifacts}
                        accessToken={accessToken}
                        runId={msg.streamId ?? undefined}
                        workFolder={workFolder}
                        onOpenDocument={openDocumentPanel}
                        onOpenResource={openResourcePanel}
                        typography={isWorkMode ? 'work' : 'default'}
                        trimTrailingMargin={true}
                      />
                    )}
                  </>
                )
              }

              return historicalSegments.map((seg, si) =>
                renderSegment(seg, si, historicalSegments, currentRunMessageLive && si === historicalSegments.length - 1)
              )
            })()}
          {idx === messages.length - 1 && !isStreaming && !sending && (
            <AssistantActionBar
              textToCopy={assistantTurnPlainText(historicalTurn!)}
              onFork={() => void handleFork(msg.id)}
              onShare={threadId && !privateThreadIds.has(threadId) ? () => createShareForMessage(msg.id) : undefined}
              shareState={sharingMessageId === msg.id ? 'sharing' : sharedMessageId === msg.id ? 'shared' : 'idle'}
              webSources={resolvedSources}
              onShowSources={canShowSources ? () => {
                if (sourcePanelMessageId === msg.id) { closePanel(); return }
                closePanel()
                openSourcePanel(msg.id)
              } : undefined}
              onViewRunDetail={showRunDetailButton && msg.streamId ? () => setRunDetailPanelRunId(msg.streamId!) : undefined}
              isLast={true}
            />
          )}
          </div>
        )}
        {msg.role === 'assistant' && !hasAssistantTurn && (timelineSteps.length > 0 || hasMessageCodeExecutions || (messageSubAgents && messageSubAgents.length > 0) || (messageFileOps && messageFileOps.length > 0) || (messageWebFetches && messageWebFetches.length > 0)) && (
          <div style={{ marginBottom: '12px' }}>
            {messageCodeExecutions?.map((ce) => (
              <TopLevelCopToolBlock
                key={`fallback-code-${msg.id}-${ce.id}`}
                entry={{ kind: 'code', id: ce.id, seq: ce.seq ?? 0, item: ce }}
                onOpenCodeExecution={openCodePanel}
                activeCodeExecutionId={codePanelExecutionId ?? undefined}
              />
            ))}
            {(timelineSteps.length > 0 || (messageSubAgents && messageSubAgents.length > 0) || (messageFileOps && messageFileOps.length > 0) || (messageWebFetches && messageWebFetches.length > 0)) && (
              <CopTimeline
                segments={buildFallbackSegments({
                  subAgents: messageSubAgents,
                  fileOps: messageFileOps,
                  webFetches: messageWebFetches,
                })}
                pool={buildResolvedPool({
                  steps: timelineSteps,
                  sources: resolvedSources ?? [],
                  subAgents: messageSubAgents,
                  fileOps: messageFileOps,
                  webFetches: messageWebFetches,
                })}
                isComplete
                onOpenSubAgent={openAgentPanel}
                accessToken={accessToken}
                baseUrl={baseUrl}
                typography={isWorkMode ? 'work' : 'default'}
              />
            )}
          </div>
        )}
        <MessageBubble
          message={msg}
          isLast={idx === messages.length - 1}
          streamAssistantMarkdown={
            isStreaming && msg.role === 'assistant' && idx === messages.length - 1
          }
          animateUserEnter={msg.role === 'user' && msg.id === userEnterMessageId}
          onUserEnterAnimationEnd={msg.role === 'user' && msg.id === userEnterMessageId ? clearUserEnterAnimation : undefined}
          onRetry={bubbleCallbacks?.onRetry}
          onEdit={bubbleCallbacks?.onEdit}
          onFork={bubbleCallbacks?.onFork}
          onShare={bubbleCallbacks?.onShare}
          shareState={
            sharingMessageId === msg.id ? 'sharing' : sharedMessageId === msg.id ? 'shared' : 'idle'
          }
          webSources={resolvedSources}
          artifacts={msg.role === 'assistant' ? msgMeta?.artifacts : undefined}
          browserActions={msg.role === 'assistant' ? msgMeta?.browserActions : undefined}
          widgets={bubbleWidgets}
          accessToken={accessToken}
          workFolder={workFolder}
          isWorkMode={isWorkMode}
          onWidgetAction={msg.role === 'assistant' ? handleArtifactAction : undefined}
          onShowSources={
            msg.role === 'assistant' && canShowSources
              ? () => {
                  if (sourcePanelMessageId === msg.id) {
                    closePanel()
                    return
                  }
                  closePanel()
                  openSourcePanel(msg.id)
                }
              : undefined
          }
          onOpenDocument={bubbleCallbacks?.onOpenDocument}
          onOpenResource={bubbleCallbacks?.onOpenResource}
          onViewRunDetail={bubbleCallbacks?.onViewRunDetail}
          contentOverride={msg.role === 'assistant' && hasAssistantTurn ? '' : undefined}
          plainTextForCopy={msg.role === 'assistant' && hasAssistantTurn ? assistantTurnPlainText(historicalTurn!) : undefined}
          suppressActionBar={msg.role === 'assistant' && hasAssistantTurn && idx === messages.length - 1 && !isStreaming && !sending}
        />
        {locationState?.isIncognitoFork && locationState.forkBaseCount != null && idx === locationState.forkBaseCount - 1 && (
          <IncognitoDivider text={t.incognitoForkDivider} />
        )}
      </div>
    )
  }

  const hasLastTurn = lastTurnStartIdx < messages.length
  return (
    <>
      {messages.slice(0, lastTurnStartIdx).map(renderMessage)}
      {(hasLastTurn || lastTurnChildren) && (
        <div ref={lastTurnRef} className="flex flex-col" style={{ gap: isWorkMode ? 0 : '1.5rem' }}>
          {hasLastTurn && messages.slice(lastTurnStartIdx).map((msg, i) => renderMessage(msg, lastTurnStartIdx + i))}
          {lastTurnChildren}
        </div>
      )}
    </>
  )
})
