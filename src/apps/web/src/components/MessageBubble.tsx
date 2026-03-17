import type { MessageResponse } from '../api'
import type { WebSource, ArtifactRef, BrowserActionRef } from '../storage'
import { useTypewriter } from '../hooks/useTypewriter'
import { MarkdownRenderer } from './MarkdownRenderer'
import { BrowserScreenshotCard } from './BrowserScreenshotCard'
import { UserMessage } from './messagebubble/UserMessage'
import { AssistantMessage } from './messagebubble/AssistantMessage'

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
  accessToken?: string
  onShowSources?: () => void
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  activePanelArtifactKey?: string | null
  onViewRunDetail?: () => void
  contentPrefix?: string
}

export function MessageBubble({ message, onRetry, onEdit, onFork, onShare, shareState, webSources, artifacts, browserActions, accessToken, onShowSources, onOpenDocument, activePanelArtifactKey, onViewRunDetail, contentPrefix }: Props) {
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
      accessToken={accessToken}
      onShowSources={onShowSources}
      onOpenDocument={onOpenDocument}
      activePanelArtifactKey={activePanelArtifactKey}
      onViewRunDetail={onViewRunDetail}
      contentPrefix={contentPrefix}
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
  webSources?: WebSource[]
  browserActions?: BrowserActionRef[]
  accessToken?: string
}

export function StreamingBubble({ content, webSources, browserActions, accessToken }: StreamingBubbleProps) {
  const displayed = useTypewriter(content)

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        {renderBrowserScreenshots(browserActions, accessToken)}
        <MarkdownRenderer content={displayed} disableMath webSources={webSources} />
      </div>
    </div>
  )
}
