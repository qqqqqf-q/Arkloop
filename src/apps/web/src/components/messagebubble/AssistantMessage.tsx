import { useEffect, useRef, useState, useLayoutEffect } from 'react'
import { Check, Share2, Split, Terminal } from 'lucide-react'
import type { MessageResponse } from '../../api'
import type { WebSource, ArtifactRef, BrowserActionRef, WidgetRef } from '../../storage'
import { WidgetBlock } from '../WidgetBlock'
import { MarkdownRenderer } from '../MarkdownRenderer'
import { recordPerfCount, recordPerfValue } from '../../perfDebug'
import { DocumentCard } from '../DocumentCard'
import { BrowserScreenshotCard } from '../BrowserScreenshotCard'
import type { ArtifactAction } from '../ArtifactIframe'
import { useLocale } from '../../contexts/LocaleContext'
import { useTypewriter } from '../../hooks/useTypewriter'
import { isDesktop } from '@arkloop/shared/desktop'
import { isDocumentArtifact, isArtifactReferenced, getDomain } from './utils'
import { messageTextContent } from '../../messageContent'
import { CopyIconButton } from '../CopyIconButton'
import { RefreshIconButton } from '../RefreshIconButton'
import { ActionIconButton } from '../ActionIconButton'

type Props = {
  message: MessageResponse
  /** 与 ChatPage 中「正在流式且为最后一条助手气泡」对齐；未分段正文用 */
  streamMarkdown?: boolean
  onRetry?: () => void
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
  /** 与正文展示解耦的复制文本（例如分段 Markdown 合并） */
  plainTextForCopy?: string
  isLast?: boolean
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

export function AssistantMessage({
  message,
  streamMarkdown = false,
  onRetry,
  onFork,
  onShare,
  shareState,
  webSources,
  artifacts,
  browserActions,
  widgets,
  accessToken,
  onWidgetAction,
  onShowSources,
  onOpenDocument,
  activePanelArtifactKey,
  onViewRunDetail,
  contentPrefix,
  contentOverride,
  plainTextForCopy,
  isLast,
}: Props) {
  const { t } = useLocale()
  const messageText = messageTextContent(message)
  const renderedContent = contentOverride ?? (contentPrefix && messageText.startsWith(contentPrefix) ? messageText.slice(contentPrefix.length).trimStart() : messageText)
  const textForCopy = plainTextForCopy ?? renderedContent
  const displayedAssistantMd = useTypewriter(renderedContent, !streamMarkdown)
  const widgetCount = widgets?.length ?? 0
  const artifactCount = artifacts?.length ?? 0

  useEffect(() => {
    recordPerfCount('assistant_message_render', 1, {
      messageId: message.id,
      contentLength: renderedContent.length,
      displayedLength: displayedAssistantMd.length,
      streamMarkdown,
      widgetCount,
      artifactCount,
    })
    recordPerfValue('assistant_message_displayed', displayedAssistantMd.length, 'chars', {
      messageId: message.id,
      contentLength: renderedContent.length,
      streamMarkdown,
    })
  }, [artifactCount, displayedAssistantMd.length, message.id, renderedContent.length, streamMarkdown, widgetCount])

  const handleCopy = () => {
    void navigator.clipboard.writeText(textForCopy)
  }

  const contentRef = useRef<HTMLDivElement>(null)
  const [contentCompact, setContentCompact] = useState(false)
  useLayoutEffect(() => {
    const el = contentRef.current
    if (!el) return
    setContentCompact(el.scrollHeight <= 36)
  }, [displayedAssistantMd])

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      {widgets && widgets.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '8px', width: '100%' }}>
          {widgets.map((w) => (
            <WidgetBlock key={w.id} html={w.html} title={w.title} complete={true} onAction={onWidgetAction} />
          ))}
        </div>
      )}
      <div style={{ maxWidth: '663px' }}>
        {artifacts && onOpenDocument && (() => {
          const referenced = artifacts.filter((a) => isDocumentArtifact(a) && isArtifactReferenced(messageText, a.key))
          if (referenced.length === 0) return null
          return (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px', marginBottom: '14px' }}>
              {referenced.map((artifact) => (
                <DocumentCard
                  key={artifact.key}
                  artifact={artifact}
                  onClick={(trigger) => onOpenDocument(artifact, { trigger, artifacts, runId: message.run_id })}
                  active={activePanelArtifactKey === artifact.key}
                />
              ))}
            </div>
          )
        })()}
        {renderBrowserScreenshots(browserActions, accessToken)}
        <div ref={contentRef}>
          <MarkdownRenderer
            content={displayedAssistantMd}
            streaming={streamMarkdown}
            webSources={webSources}
            artifacts={artifacts}
            accessToken={accessToken}
            runId={message.run_id}
            onOpenDocument={onOpenDocument}
            trimTrailingMargin
          />
        </div>
        <div
          className={isLast ? '' : 'pointer-events-none opacity-0 group-hover/turn:pointer-events-auto group-hover/turn:opacity-100 transition-[opacity] duration-[180ms] ease-out'}
          style={{ marginTop: contentCompact ? '-4px' : '4px', marginLeft: '-6px' }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '3px' }}>
            <CopyIconButton
              onCopy={handleCopy}
              size={16}
              hoverBackground="var(--c-bg-deep)"
              className="flex h-9 w-9 items-center justify-center rounded-[7px] text-[var(--c-text-secondary)] opacity-60 transition-[opacity,color] duration-[60ms] hover:opacity-100 hover:text-[var(--c-text-primary)] cursor-pointer border-none bg-transparent"
              resetDelay={1500}
            />
            <RefreshIconButton
              onRefresh={onRetry!}
              disabled={!onRetry}
              size={16}
              className={`flex h-9 w-9 items-center justify-center rounded-[7px] text-[var(--c-text-secondary)] transition-[opacity,color] duration-[60ms] border-none bg-transparent ${onRetry ? 'opacity-60 hover:opacity-100 hover:text-[var(--c-text-primary)] cursor-pointer' : 'opacity-25 cursor-default'}`}
            />
            {!isDesktop() && (
            <div style={{ position: 'relative', display: 'inline-flex' }}>
              <ActionIconButton
                onClick={onShare}
                disabled={!onShare || shareState === 'sharing'}
                hoverBackground="var(--c-bg-deep)"
                className={`flex h-9 w-9 items-center justify-center rounded-[7px] text-[var(--c-text-secondary)] transition-[opacity,color] duration-[60ms] border-none bg-transparent ${onShare && shareState !== 'sharing' ? 'opacity-60 hover:opacity-100 hover:text-[var(--c-text-primary)] cursor-pointer' : 'opacity-25 cursor-default'}`}
              >
                {shareState === 'shared' ? <Check size={16} /> : <Share2 size={16} />}
              </ActionIconButton>
              <span
                className="absolute -top-7 left-1/2 -translate-x-1/2 rounded px-1.5 py-0.5 text-[11px]"
                style={{
                  backgroundColor: 'var(--c-bg-deep)',
                  color: 'var(--c-text-primary)',
                  padding: '2px 6px',
                  whiteSpace: 'nowrap',
                  opacity: shareState === 'shared' ? 1 : 0,
                  transition: 'opacity 150ms ease',
                  pointerEvents: 'none',
                  userSelect: 'none',
                  zIndex: 10,
                }}
              >
                {t.shareLinkCopied}
              </span>
            </div>
            )}
            <ActionIconButton
              onClick={onFork}
              disabled={!onFork}
              tooltip="Fork"
              hoverBackground="var(--c-bg-deep)"
              className={`flex h-9 w-9 items-center justify-center rounded-[7px] text-[var(--c-text-secondary)] transition-[opacity,color] duration-[60ms] border-none bg-transparent ${onFork ? 'opacity-60 hover:opacity-100 hover:text-[var(--c-text-primary)] cursor-pointer' : 'opacity-25 cursor-default'}`}
            >
              <Split size={16} />
            </ActionIconButton>
            {onViewRunDetail && (
              <ActionIconButton
                onClick={onViewRunDetail}
                tooltip={t.desktopSettings.viewRunDetail}
                hoverBackground="var(--c-bg-deep)"
                className="flex h-9 w-9 items-center justify-center rounded-[7px] text-[var(--c-text-secondary)] opacity-60 transition-[opacity,color] duration-[60ms] hover:opacity-100 hover:text-[var(--c-text-primary)] cursor-pointer border-none bg-transparent"
              >
                <Terminal size={16} />
              </ActionIconButton>
            )}
            {webSources && webSources.length > 0 && onShowSources && (
              <button
                onClick={onShowSources}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '6px',
                  padding: '4px 12px 4px 6px',
                  borderRadius: '999px',
                  border: 'none',
                  cursor: 'pointer',
                  marginLeft: '4px',
                  transition: 'background 60ms',
                  fontFamily: 'inherit',
                }}
                className="bg-[var(--c-bg-deep)] hover:bg-[var(--c-bg-plus)]"
              >
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  {webSources.slice(0, 3).map((s, i) => {
                    const domain = getDomain(s.url)
                    return (
                      <img
                        key={i}
                        src={`https://www.google.com/s2/favicons?domain=${domain}&sz=16`}
                        width={18}
                        height={18}
                        style={{
                          borderRadius: '50%',
                          border: '1.5px solid var(--c-bg-deep)',
                          marginLeft: i > 0 ? '-6px' : 0,
                          position: 'relative',
                          zIndex: 3 - i,
                          background: 'var(--c-bg-page)',
                        }}
                        onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                        alt=""
                      />
                    )
                  })}
                </div>
                <span style={{ fontSize: '13px', color: 'var(--c-text-secondary)', fontWeight: 500 }}>
                  {webSources.length} sources
                </span>
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
