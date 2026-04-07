import { memo, Fragment, type ComponentProps } from 'react'
import { Info } from 'lucide-react'
import { Button } from '@arkloop/shared'
import { MessageBubble } from './MessageBubble'
import { CopTimeline, type WebSearchPhaseStep } from './CopTimeline'
import { MarkdownRenderer } from './MarkdownRenderer'
import { WidgetBlock } from './WidgetBlock'
import { IncognitoDivider } from './IncognitoDivider'
import { useLocale } from '../contexts/LocaleContext'
import { useChatSession } from '../contexts/chat-session'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useMessageStore } from '../contexts/message-store'
import { useMessageMeta } from '../contexts/message-meta'
import { useStream } from '../contexts/stream'
import { usePanels } from '../contexts/panels'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { apiBaseUrl } from '@arkloop/shared/api'
import { copTimelinePayloadForSegment } from '../copSegmentTimeline'
import { copSegmentCalls, assistantTurnPlainText } from '../assistantTurnSegments'
import { resolveMessageSourcesForRender } from './chatSourceResolver'
import { createThreadShare } from '../api'
import { readMessageTerminalStatus, readMessageWidgets, type ArtifactRef, type MessageTerminalStatusRef, type WebSource } from '../storage'
import { useLocation } from 'react-router-dom'
import type { CodeExecution } from './CodeExecutionCard'
import {
  assistantTurnHasVisibleOutput,
  turnHasCopThinkingItems,
  thinkingRowsForCop,
  copInlineTextRowsForCop,
  widgetToolCallIdsPlacedInTurn,
  historicWidgetsForCop,
} from '../lib/chat-helpers'

type LocationState = {
  initialRunId?: string
  isSearch?: boolean
  isIncognitoFork?: boolean
  forkBaseCount?: number
  userEnterMessageId?: string
} | null

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

export const MessageList = memo(function MessageList({
  lastTurnRef,
  lastTurnChildren,
  lastTurnStartIdx,
  handleRetry,
  handleEditMessage,
  handleFork,
  handleArtifactAction,
  openDocumentPanel,
  openCodePanel,
  showRunEvents,
  sourcePanelMessageId,
  setRunDetailPanelRunId,
  currentRunCopHeaderOverride,
  actionLabelForTerminalRun,
  actionHandlerForTerminalRun,
  clearUserEnterAnimation,
}: {
  lastTurnRef: React.RefObject<HTMLDivElement | null>
  lastTurnChildren?: React.ReactNode
  lastTurnStartIdx: number
  handleRetry: () => void
  handleEditMessage: (message: import('../api').MessageResponse, newContent: string) => void
  handleFork: (messageId: string) => Promise<void>
  handleArtifactAction: ComponentProps<typeof WidgetBlock>['onAction']
  openDocumentPanel: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  openCodePanel: (ce: CodeExecution) => void
  showRunEvents: boolean
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
  actionLabelForTerminalRun: (params: {
    status: MessageTerminalStatusRef | null
    hasOutput: boolean
  }) => string | undefined
  actionHandlerForTerminalRun: (params: {
    runId: string | null | undefined
    status: MessageTerminalStatusRef | null
    hasOutput: boolean
  }) => (() => void) | undefined
  clearUserEnterAnimation: () => void
}) {
  const { threadId, isSearchThread } = useChatSession()
  const { accessToken } = useAuth()
  const { t } = useLocale()
  const run = useRunLifecycle()
  const msgs = useMessageStore()
  const meta = useMessageMeta()
  const stream = useStream()
  const panels = usePanels()
  const threadList = useThreadList()
  const location = useLocation()
  const locationState = location.state as LocationState
  const baseUrl = apiBaseUrl()

  const messages = msgs.messages
  const isStreaming = run.isStreaming
  const sending = run.sending
  const terminalRunDisplayId = run.terminalRunDisplayId
  const terminalRunHandoffStatus = run.terminalRunHandoffStatus
  const terminalRunHistoryExpanded = run.terminalRunHistoryExpanded
  const terminalRunAssistantMessageId = run.terminalRunAssistantMessageId
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

  const resolvedMessageSources = resolveMessageSourcesForRender(messages, (() => {
    const map = new Map<string, WebSource[]>()
    for (const msg of messages) {
      if (msg.role !== 'assistant') continue
      const m = meta.getMeta(msg.id)
      if (m?.sources) map.set(msg.id, m.sources)
    }
    return map
  })())

  const codePanelExecutionId = panels.activePanel?.type === 'code' ? panels.activePanel.execution.id : null
  const documentPanelArtifactKey = panels.activePanel?.type === 'document' ? panels.activePanel.artifact.artifact.key : null

  const sharingMessageId = panels.shareModal.sharingMessageId
  const sharedMessageId = panels.shareModal.sharedMessageId

  const createShareForMessage = (messageId: string) => {
    if (!threadId || sharingMessageId) return
    panels.setShareState(messageId, null)
    createThreadShare(accessToken, threadId, 'public')
      .then((share) => {
        const url = `${window.location.origin}/s/${share.token}`
        void navigator.clipboard.writeText(url)
        panels.setShareState(null, messageId)
        setTimeout(() => panels.setShareState(null, null), 1500)
      })
      .catch(() => {
        panels.setShareState(null, null)
      })
  }

  const renderMessage = (msg: import('../api').MessageResponse, idx: number) => {
    const hideTerminalRunMessage =
      msg.role === 'assistant' &&
      hasCurrentRunHandoffUi &&
      terminalRunDisplayId != null &&
      msg.run_id === terminalRunDisplayId
    if (hideTerminalRunMessage) return null

    const msgMeta = msg.role === 'assistant' ? meta.getMeta(msg.id) : undefined
    const resolvedSources = msg.role === 'assistant' ? resolvedMessageSources.get(msg.id) : undefined
    const isCurrentTerminalRunMessage =
      msg.role === 'assistant' &&
      terminalRunDisplayId != null &&
      msg.run_id === terminalRunDisplayId
    const persistedTerminalStatus =
      msg.role === 'assistant' ? readMessageTerminalStatus(msg.id) : null
    const effectiveTerminalStatus =
      isCurrentTerminalRunMessage ? terminalRunHandoffStatus : persistedTerminalStatus
    const canShowSources = !!(resolvedSources && resolvedSources.length > 0)
    const historicalTurn = msgMeta?.assistantTurn
    const hasAssistantTurn = !!(historicalTurn && historicalTurn.segments.length > 0)
    const hasTerminalOutput =
      msg.role === 'assistant' &&
      (
        !!msg.content.trim() ||
        assistantTurnHasVisibleOutput(historicalTurn)
      )
    const terminalActionLabel = actionLabelForTerminalRun({
      status: effectiveTerminalStatus,
      hasOutput: hasTerminalOutput,
    })
    const terminalActionHandler = actionHandlerForTerminalRun({
      runId: msg.run_id,
      status: effectiveTerminalStatus,
      hasOutput: hasTerminalOutput,
    })
    const historicalSegments = historicalTurn?.segments ?? []
    const msgWidgetsRaw = msg.role === 'assistant'
      ? (msgMeta?.widgets ?? readMessageWidgets(msg.id) ?? undefined)
      : undefined
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

    return (
      <div key={msg.id} className="group/turn">
        {msg.role === 'assistant' && hasAssistantTurn && (
          <div style={{ marginBottom: '6px', display: 'flex', flexDirection: 'column', gap: 0, maxWidth: '663px' }}>
            {!isSearchThread &&
              msgThinking != null &&
              msgThinking.thinkingText.trim() !== '' &&
              !turnHasCopThinkingItems(historicalTurn!) && (
              <CopTimeline
                key={`${msg.id}-legacy-thinking`}
                steps={[]}
                sources={[]}
                isComplete
                assistantThinking={{ markdown: msgThinking.thinkingText, live: false }}
                accessToken={accessToken}
                baseUrl={baseUrl}
              />
            )}
            {historicalSegments.map((seg, si) =>
              seg.type === 'text' ? (
                <MarkdownRenderer
                  key={`${msg.id}-at-${si}`}
                  content={seg.content}
                  webSources={resolvedSources}
                  artifacts={msgMeta?.artifacts}
                  accessToken={accessToken}
                  runId={msg.run_id ?? undefined}
                  onOpenDocument={openDocumentPanel}
                  trimTrailingMargin={
                    historicalSegments[si + 1] == null ||
                    historicalSegments[si + 1]?.type === 'cop'
                  }
                />
              ) : (
                (() => {
                  const payload = copTimelinePayloadForSegment(seg, {
                    codeExecutions: messageCodeExecutions,
                    fileOps: messageFileOps,
                    webFetches: messageWebFetches,
                    subAgents: messageSubAgents,
                    searchSteps: messageSearchSteps ?? [],
                    sources: resolvedSources ?? [],
                  })
                  const histWidgets = historicWidgetsForCop(seg, msgWidgetsRaw)
                  const thinkingRowsHist = !isSearchThread
                    ? thinkingRowsForCop(seg, {
                        live: false,
                        segmentIndex: si,
                        lastSegmentIndex: historicalTurn!.segments.length - 1,
                      })
                    : []
                  const copInlineHist = !isSearchThread
                    ? copInlineTextRowsForCop(seg, {
                        live: false,
                        segmentIndex: si,
                        lastSegmentIndex: historicalSegments.length - 1,
                      })
                    : []
                  if (
                    copSegmentCalls(seg).length === 0 &&
                    thinkingRowsHist.length === 0 &&
                    copInlineHist.length === 0 &&
                    histWidgets.length === 0
                  ) {
                    return null
                  }
                  const timelineTitleOverride = effectiveTerminalStatus != null
                    ? (
                        !isCurrentTerminalRunMessage &&
                        (effectiveTerminalStatus === 'cancelled' || effectiveTerminalStatus === 'interrupted') &&
                        !seg.title?.trim()
                          ? t.connection.stopped
                          : currentRunCopHeaderOverride({
                              title: seg.title,
                              steps: payload.steps,
                              hasCodeExecutions: !!(payload.codeExecutions && payload.codeExecutions.length > 0),
                              hasSubAgents: !!(payload.subAgents && payload.subAgents.length > 0),
                              hasFileOps: !!(payload.fileOps && payload.fileOps.length > 0),
                              hasWebFetches: !!(payload.webFetches && payload.webFetches.length > 0),
                              hasGenericTools: !!(payload.genericTools && payload.genericTools.length > 0),
                              hasThinking: thinkingRowsHist.length > 0 || copInlineHist.length > 0,
                              handoffStatus: effectiveTerminalStatus,
                            })
                      )
                    : seg.title?.trim() || undefined
                  const histTrail = historicalSegments[si + 1]
                  const histTrailingText =
                    histTrail?.type === 'text' && histTrail.content.length > 0
                  return (
                    <Fragment key={`${msg.id}-acw-${si}`}>
                      <CopTimeline
                        steps={payload.steps}
                        sources={payload.sources}
                        isComplete
                        codeExecutions={payload.codeExecutions}
                        onOpenCodeExecution={openCodePanel}
                        activeCodeExecutionId={codePanelExecutionId ?? undefined}
                        subAgents={payload.subAgents}
                        fileOps={payload.fileOps}
                        webFetches={payload.webFetches}
                        genericTools={payload.genericTools}
                        headerOverride={timelineTitleOverride}
                        preserveExpanded={terminalRunHistoryExpanded && terminalRunAssistantMessageId === msg.id}
                        thinkingRows={thinkingRowsHist.length > 0 ? thinkingRowsHist : undefined}
                        copInlineTextRows={copInlineHist.length > 0 ? copInlineHist : undefined}
                        trailingAssistantTextPresent={histTrailingText}
                        accessToken={accessToken}
                        baseUrl={baseUrl}
                      />
                      {histWidgets.map((w) => (
                        <WidgetBlock
                          key={w.id}
                          html={w.html}
                          title={w.title}
                          complete
                          onAction={handleArtifactAction}
                        />
                      ))}
                    </Fragment>
                  )
                })()
              ),
            )}
          </div>
        )}
        {msg.role === 'assistant' && !hasAssistantTurn && (timelineSteps.length > 0 || hasMessageCodeExecutions || (messageSubAgents && messageSubAgents.length > 0) || (messageFileOps && messageFileOps.length > 0) || (messageWebFetches && messageWebFetches.length > 0)) && (
          <div style={{ marginBottom: '12px' }}>
            {timelineSteps.length > 0 && (
              <CopTimeline
                steps={timelineSteps}
                sources={resolvedSources ?? []}
                isComplete
                codeExecutions={messageCodeExecutions}
                onOpenCodeExecution={openCodePanel}
                activeCodeExecutionId={codePanelExecutionId ?? undefined}
                subAgents={messageSubAgents}
                fileOps={messageFileOps}
                webFetches={messageWebFetches}
                accessToken={accessToken}
                baseUrl={baseUrl}
              />
            )}
            {timelineSteps.length === 0 && (hasMessageCodeExecutions || (messageSubAgents && messageSubAgents.length > 0) || (messageFileOps && messageFileOps.length > 0) || (messageWebFetches && messageWebFetches.length > 0)) && (
              <CopTimeline
                steps={[]}
                sources={[]}
                isComplete
                codeExecutions={messageCodeExecutions}
                onOpenCodeExecution={openCodePanel}
                activeCodeExecutionId={codePanelExecutionId ?? undefined}
                subAgents={messageSubAgents}
                fileOps={messageFileOps}
                webFetches={messageWebFetches}
                accessToken={accessToken}
                baseUrl={baseUrl}
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
          onRetry={
            msg.role === 'assistant' && idx === messages.length - 1 && !isStreaming && !sending
              ? handleRetry
              : undefined
          }
          onEdit={
            msg.role === 'user' && !isStreaming && !sending
              ? (newContent) => handleEditMessage(msg, newContent)
              : undefined
          }
          onFork={
            msg.role === 'assistant' && !isStreaming && !sending
              ? () => void handleFork(msg.id)
              : undefined
          }
          onShare={
            msg.role === 'assistant' && !isStreaming && !sending && threadId && !privateThreadIds.has(threadId)
              ? () => createShareForMessage(msg.id)
              : undefined
          }
          shareState={
            sharingMessageId === msg.id ? 'sharing' : sharedMessageId === msg.id ? 'shared' : 'idle'
          }
          webSources={resolvedSources}
          artifacts={msg.role === 'assistant' ? msgMeta?.artifacts : undefined}
          browserActions={msg.role === 'assistant' ? msgMeta?.browserActions : undefined}
          widgets={bubbleWidgets}
          accessToken={accessToken}
          onWidgetAction={msg.role === 'assistant' ? handleArtifactAction : undefined}
          onShowSources={
            msg.role === 'assistant' && canShowSources
              ? () => {
                  if (sourcePanelMessageId === msg.id) {
                    panels.closePanel()
                    return
                  }
                  panels.closePanel()
                  panels.openSourcePanel(msg.id)
                }
              : undefined
          }
          onOpenDocument={msg.role === 'assistant' ? openDocumentPanel : undefined}
          activePanelArtifactKey={documentPanelArtifactKey}
          onViewRunDetail={
            showRunEvents && msg.role === 'assistant' && msg.run_id
              ? () => setRunDetailPanelRunId(msg.run_id!)
              : undefined
          }
          contentOverride={msg.role === 'assistant' && hasAssistantTurn ? '' : undefined}
          plainTextForCopy={msg.role === 'assistant' && hasAssistantTurn ? assistantTurnPlainText(historicalTurn!) : undefined}
        />
        {msg.role === 'assistant' && (effectiveTerminalStatus === 'failed' || effectiveTerminalStatus === 'interrupted' || effectiveTerminalStatus === 'cancelled') && (
          <FailedRunRetryCard
            title={effectiveTerminalStatus === 'interrupted' ? t.runInterrupted : effectiveTerminalStatus === 'cancelled' ? t.runCancelled : t.failedRunRetryTitle}
            actionLabel={!isStreaming && !sending ? terminalActionLabel : undefined}
            onRetry={!isStreaming && !sending ? terminalActionHandler : undefined}
          />
        )}
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
        <div ref={lastTurnRef} className="flex flex-col gap-6">
          {hasLastTurn && messages.slice(lastTurnStartIdx).map((msg, i) => renderMessage(msg, lastTurnStartIdx + i))}
          {lastTurnChildren}
        </div>
      )}
    </>
  )
})
