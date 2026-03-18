import type { MessageResponse } from '../api'
import type { WebSource, ArtifactRef, BrowserActionRef, WidgetRef } from '../storage'
import { MarkdownRenderer } from './MarkdownRenderer'
import { BrowserScreenshotCard } from './BrowserScreenshotCard'
import { UserMessage } from './messagebubble/UserMessage'
import { AssistantMessage } from './messagebubble/AssistantMessage'
import type { ArtifactAction } from './ArtifactIframe'
import { useTypewriter } from '../hooks/useTypewriter'

type Props = {
  message: MessageResponse
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
  onWidgetAction?: (action: ArtifactAction) => void
  onShowSources?: () => void
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  activePanelArtifactKey?: string | null
  onViewRunDetail?: () => void
  contentPrefix?: string
  contentOverride?: string
}

export function MessageBubble({ message, onRetry, onEdit, onFork, onShare, shareState, webSources, artifacts, browserActions, widgets, accessToken, onWidgetAction, onShowSources, onOpenDocument, activePanelArtifactKey, onViewRunDetail, contentPrefix, contentOverride }: Props) {
  if (message.role === 'user') {
    return (
      <UserMessage
        message={message}
        onRetry={onRetry}
        onEdit={onEdit}
        accessToken={accessToken}
      />
    )
  }

  return (
    <AssistantMessage
      message={message}
      onRetry={onRetry}
      onFork={onFork}
      onShare={onShare}
      shareState={shareState}
      webSources={webSources}
      artifacts={artifacts}
      browserActions={browserActions}
      widgets={widgets}
      accessToken={accessToken}
      onWidgetAction={onWidgetAction}
      onShowSources={onShowSources}
      onOpenDocument={onOpenDocument}
      activePanelArtifactKey={activePanelArtifactKey}
      onViewRunDetail={onViewRunDetail}
      contentPrefix={contentPrefix}
      contentOverride={contentOverride}
    />
  )
}

function renderBrowserScreenshots(browserActions?: BrowserActionRef[], accessToken?: string) {
  if (!browserActions || browserActions.length === 0 || !accessToken) return null
  const withScreenshot = browserActions.filter((action) => action.screenshotArtifact)
  if (withScreenshot.length === 0) return null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '14px' }}>
      {withScreenshot.map((action) => (
        <BrowserScreenshotCard
          key={action.id}
          artifact={action.screenshotArtifact!}
          accessToken={accessToken}
          command={action.command}
          url={action.url}
        />
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
        <MarkdownRenderer content={displayed} disableMath webSources={webSources} />
      </div>
    </div>
  )
}
