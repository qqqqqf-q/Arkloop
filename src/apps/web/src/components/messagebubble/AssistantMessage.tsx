import { useState } from 'react'
import { Copy, Check, RefreshCw, Share2, Split, Terminal } from 'lucide-react'
import type { MessageResponse } from '../../api'
import type { WebSource, ArtifactRef, BrowserActionRef, WidgetRef } from '../../storage'
import { WidgetBlock } from '../WidgetBlock'
import { MarkdownRenderer } from '../MarkdownRenderer'
import { DocumentCard } from '../DocumentCard'
import { BrowserScreenshotCard } from '../BrowserScreenshotCard'
import type { ArtifactAction } from '../ArtifactIframe'
import { useLocale } from '../../contexts/LocaleContext'
import { isDesktop } from '@arkloop/shared/desktop'
import { isDocumentArtifact, isArtifactReferenced, getDomain } from './utils'

type Props = {
  message: MessageResponse
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
}: Props) {
  const { t } = useLocale()
  const [copied, setCopied] = useState(false)
  const renderedContent = contentOverride ?? (contentPrefix && message.content.startsWith(contentPrefix) ? message.content.slice(contentPrefix.length).trimStart() : message.content)

  const handleCopy = () => {
    void navigator.clipboard.writeText(renderedContent).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

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
          const referenced = artifacts.filter((a) => isDocumentArtifact(a) && isArtifactReferenced(message.content, a.key))
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
        <MarkdownRenderer content={renderedContent} webSources={webSources} artifacts={artifacts} accessToken={accessToken} runId={message.run_id} onOpenDocument={onOpenDocument} />
        <div style={{ marginTop: '16px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
            <div style={{ position: 'relative' }}>
              <button
                onClick={handleCopy}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-60 transition-[opacity,background] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer border-none bg-transparent"
              >
                {copied ? <Check size={15} /> : <Copy size={15} />}
              </button>
              <span
                style={{
                  position: 'absolute',
                  top: '100%',
                  left: '50%',
                  transform: copied
                    ? 'translateX(-50%) translateY(2px)'
                    : 'translateX(-50%) translateY(-2px)',
                  marginTop: '4px',
                  fontSize: '11px',
                  color: 'var(--c-text-tertiary)',
                  background: 'var(--c-bg-deep)',
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '5px',
                  padding: '2px 6px',
                  whiteSpace: 'nowrap',
                  opacity: copied ? 1 : 0,
                  transition: 'opacity 150ms ease, transform 150ms ease',
                  pointerEvents: 'none',
                  userSelect: 'none',
                  zIndex: 10,
                }}
              >
                已复制
              </span>
            </div>
            <button
              onClick={onRetry}
              disabled={!onRetry}
              className={`flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[opacity,background] duration-[60ms] border-none bg-transparent ${onRetry ? 'opacity-60 hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer' : 'opacity-25 cursor-default'}`}
            >
              <RefreshCw size={15} />
            </button>
            {!isDesktop() && (
            <div style={{ position: 'relative', display: 'inline-flex' }}>
              <button
                onClick={onShare}
                disabled={!onShare || shareState === 'sharing'}
                className={`flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[opacity,background] duration-[60ms] border-none bg-transparent ${onShare && shareState !== 'sharing' ? 'opacity-60 hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer' : 'opacity-25 cursor-default'}`}
              >
                {shareState === 'shared' ? <Check size={15} /> : <Share2 size={15} />}
              </button>
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
            <button
              onClick={onFork}
              disabled={!onFork}
              className={`flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[opacity,background] duration-[60ms] border-none bg-transparent ${onFork ? 'opacity-60 hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer' : 'opacity-25 cursor-default'}`}
            >
              <Split size={15} />
            </button>
            {onViewRunDetail && (
              <button
                onClick={onViewRunDetail}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-60 transition-[opacity,background] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer border-none bg-transparent"
                title={t.desktopSettings.viewRunDetail}
              >
                <Terminal size={15} />
              </button>
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
