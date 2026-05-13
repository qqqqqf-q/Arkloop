import type { AgentMessage } from '../agent-ui'
import type { WebSource, ArtifactRef, BrowserActionRef, WidgetRef } from '../storage'
import { MarkdownRenderer } from './MarkdownRenderer'
import { BrowserScreenshotCard, BrowserActionSummaryCard } from './BrowserScreenshotCard'
import { UserMessage } from './messagebubble/UserMessage'
import { AssistantMessage } from './messagebubble/AssistantMessage'
import type { ArtifactAction } from './ArtifactIframe'
import { memo } from 'react'
import { useTypewriter } from '../hooks/useTypewriter'
import type { ResourceRef } from './resource-preview/types'

type Props = {
  message: AgentMessage
  /** 仅当前线程正在 SSE 且本条为最后一条助手消息时为 true */
  streamAssistantMarkdown?: boolean
  animateUserEnter?: boolean
  onUserEnterAnimationEnd?: () => void
  onRetry?: () => void
  onEdit?: (newContent: string) => void
  onFork?: () => void
  onShare?: () => void
  shareState?: 'idle' | 'sharing' | 'shared'
  webSources?: WebSource[]
  artifacts?: ArtifactRef[]
  browserActions?: BrowserActionRef[]
  widgets?: WidgetRef[]
  accessToken?: string
  workFolder?: string | null
  onWidgetAction?: (action: ArtifactAction) => void
  onShowSources?: () => void
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  onOpenResource?: (resource: ResourceRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  onViewRunDetail?: () => void
  contentPrefix?: string
  contentOverride?: string
  plainTextForCopy?: string
  isLast?: boolean
  isWorkMode?: boolean
  suppressActionBar?: boolean
}

export const MessageBubble = memo(function MessageBubble({ message, streamAssistantMarkdown, animateUserEnter, onUserEnterAnimationEnd, onRetry, onEdit, onFork, onShare, shareState, webSources, artifacts, browserActions, widgets, accessToken, workFolder, onWidgetAction, onShowSources, onOpenDocument, onOpenResource, onViewRunDetail, contentPrefix, contentOverride, plainTextForCopy, isLast, isWorkMode, suppressActionBar }: Props) {
  if (message.role === 'user') {
    return (
      <UserMessage
        message={message}
        animateEnter={animateUserEnter}
        onEnterAnimationEnd={onUserEnterAnimationEnd}
        onRetry={onRetry}
        onEdit={onEdit}
        accessToken={accessToken}
        isWorkMode={isWorkMode}
      />
    )
  }

  return (
    <AssistantMessage
      message={message}
      streamMarkdown={streamAssistantMarkdown}
      onFork={onFork}
      onShare={onShare}
      shareState={shareState}
      webSources={webSources}
      artifacts={artifacts}
      browserActions={browserActions}
      widgets={widgets}
      accessToken={accessToken}
      workFolder={workFolder}
      onWidgetAction={onWidgetAction}
      onShowSources={onShowSources}
      onOpenDocument={onOpenDocument}
      onOpenResource={onOpenResource}
      onViewRunDetail={onViewRunDetail}
      contentPrefix={contentPrefix}
      contentOverride={contentOverride}
      plainTextForCopy={plainTextForCopy}
      isLast={isLast}
      isWorkMode={isWorkMode}
      suppressActionBar={suppressActionBar}
    />
  )
})

function renderBrowserScreenshots(browserActions?: BrowserActionRef[], accessToken?: string) {
  if (!browserActions || browserActions.length === 0) return null
  const visibleActions = browserActions.filter((action) => action.screenshotArtifact || action.output || action.url || action.command)
  if (visibleActions.length === 0) return null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '14px' }}>
      {visibleActions.map((action) => (
        action.screenshotArtifact && accessToken ? (
          <BrowserScreenshotCard
            key={action.id}
            artifact={action.screenshotArtifact}
            accessToken={accessToken}
            command={action.command}
            url={action.url}
          />
        ) : (
          <BrowserActionSummaryCard key={action.id} command={action.command} url={action.url} output={action.output} exitCode={action.exitCode} />
        )
      ))}
    </div>
  )
}

type StreamingBubbleProps = {
  content: string
  isComplete?: boolean
  webSources?: WebSource[]
  browserActions?: BrowserActionRef[]
  accessToken?: string
}

export function StreamingBubble({ content, isComplete, webSources, browserActions, accessToken }: StreamingBubbleProps) {
  const displayed = useTypewriter(content, isComplete)

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        {renderBrowserScreenshots(browserActions, accessToken)}
        <MarkdownRenderer content={displayed} disableMath streaming={!isComplete} webSources={webSources} />
      </div>
    </div>
  )
}
