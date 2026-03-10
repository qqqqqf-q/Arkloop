import { useState, useRef, useEffect } from 'react'
import { Copy, Check, RefreshCw, Share2, Split, Paperclip, Pencil, MoreHorizontal, Flag } from 'lucide-react'
import type { MessageResponse } from '../api'
import type { WebSource, ArtifactRef } from '../storage'
import type { BrowserActionRef } from '../storage'
import { MarkdownRenderer } from './MarkdownRenderer'
import { useTypewriter } from '../hooks/useTypewriter'
import { ArtifactImage } from './ArtifactImage'
import { ArtifactDownload } from './ArtifactDownload'
import { DocumentCard } from './DocumentCard'
import { BrowserScreenshotCard } from './BrowserScreenshotCard'
import { useLocale } from '../contexts/LocaleContext'
import { extractLegacyFilesFromContent, isFilePart, isImagePart, messageAttachmentParts, messageTextContent } from '../messageContent'

function isDocumentArtifact(artifact: ArtifactRef): boolean {
  return !artifact.mime_type.startsWith('image/') && artifact.mime_type !== 'text/html'
}

function formatShortDate(dateStr: string): string {
  const d = new Date(dateStr)
  const month = d.toLocaleString('en-US', { month: 'short' })
  return `${month}. ${d.getDate()}`
}

function formatFullDate(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  })
}

function MessageDate({ createdAt }: { createdAt: string }) {
  const [hovered, setHovered] = useState(false)
  return (
    <span
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        position: 'relative',
        fontSize: '11px',
        lineHeight: 1,
        color: 'var(--c-text-muted)',
        whiteSpace: 'nowrap',
        userSelect: 'none',
        cursor: 'default',
      }}
    >
      {formatShortDate(createdAt)}
      {hovered && (
        <span
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            right: 0,
            fontSize: '11px',
            lineHeight: 1,
            color: 'var(--c-text-primary)',
            background: 'var(--c-bg-deep)',
            borderRadius: '6px',
            padding: '4px 8px',
            whiteSpace: 'nowrap',
            pointerEvents: 'none',
            zIndex: 10,
          }}
        >
          {formatFullDate(createdAt)}
        </span>
      )}
    </span>
  )
}

function isArtifactReferenced(content: string, key: string): boolean {
  return content.includes(`artifact:${key}`)
}

type Props = {
  message: MessageResponse
  onRetry?: () => void
  onEdit?: (newContent: string) => void
  onFork?: () => void
  onShare?: () => void
  onReport?: () => void
  shareState?: 'idle' | 'sharing' | 'shared'
  webSources?: WebSource[]
  artifacts?: ArtifactRef[]
  browserActions?: BrowserActionRef[]
  accessToken?: string
  onShowSources?: () => void
  onOpenDocument?: (artifact: ArtifactRef) => void
  activePanelArtifactKey?: string | null
}

function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
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


export function MessageBubble({ message, onRetry, onEdit, onFork, onShare, onReport, shareState, webSources, artifacts, browserActions, accessToken, onShowSources, onOpenDocument, activePanelArtifactKey }: Props) {
  const { t } = useLocale()
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editText, setEditText] = useState('')
  const [moreOpen, setMoreOpen] = useState(false)
  const moreRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // close popover on outside click
  useEffect(() => {
    if (!moreOpen) return
    const handler = (e: MouseEvent) => {
      if (moreRef.current && !moreRef.current.contains(e.target as Node)) setMoreOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [moreOpen])

  const handleCopy = () => {
    const plainText = message.role === 'user' ? messageTextContent(message) : message.content
    void navigator.clipboard.writeText(plainText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  const handleEditStart = () => {
    setEditText(messageTextContent(message))
    setEditing(true)
  }

  const handleEditCancel = () => {
    setEditing(false)
    setEditText('')
  }

  const handleEditDone = () => {
    const trimmed = editText.trim()
    if (trimmed && onEdit) {
      onEdit(trimmed)
    }
    setEditing(false)
    setEditText('')
  }

  // 自动调整 textarea 高度
  useEffect(() => {
    if (editing && textareaRef.current) {
      const el = textareaRef.current
      el.style.height = 'auto'
      el.style.height = `${el.scrollHeight}px`
      el.focus()
      el.setSelectionRange(el.value.length, el.value.length)
    }
  }, [editing])

  const handleTextareaInput = () => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
      textareaRef.current.style.height = `${textareaRef.current.scrollHeight}px`
    }
  }

  if (message.role === 'user') {
    const legacy = extractLegacyFilesFromContent(message.content)
    const attachmentParts = messageAttachmentParts(message)
    const imageAttachments = attachmentParts.filter(isImagePart)
    const fileAttachments = attachmentParts.filter(isFilePart)
    const text = messageTextContent(message)
    const displayText = !accessToken && attachmentParts.length > 0 ? message.content : text
    const fileNames = attachmentParts.length > 0
      ? [...imageAttachments, ...fileAttachments].map((part) => part.attachment.filename)
      : legacy.fileNames

    if (editing) {
      return (
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', width: '100%', maxWidth: '663px' }}>
            {fileNames.length > 0 && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
                {fileNames.map((name) => (
                  <div
                    key={name}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '6px',
                      background: 'var(--c-bg-sub)',
                      border: '0.5px solid var(--c-border-subtle)',
                      borderRadius: '8px',
                      padding: '4px 10px',
                      fontSize: '12px',
                      color: 'var(--c-text-secondary)',
                    }}
                  >
                    <Paperclip size={11} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                    <span style={{ maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {name}
                    </span>
                  </div>
                ))}
              </div>
            )}
            <div style={{ position: 'relative', background: 'var(--c-bg-deep)', borderRadius: '11px', padding: '10px 16px' }}>
              <textarea
                ref={textareaRef}
                value={editText}
                onChange={(e) => setEditText(e.target.value)}
                onInput={handleTextareaInput}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') handleEditCancel()
                }}
                style={{
                  width: '100%',
                  background: 'transparent',
                  border: 'none',
                  outline: 'none',
                  resize: 'none',
                  color: 'var(--c-text-primary)',
                  fontSize: '16px',
                  lineHeight: 1.6,
                  letterSpacing: '-0.64px',
                  fontFamily: 'inherit',
                  minHeight: '28px',
                  overflow: 'hidden',
                }}
              />
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
              <button
                onClick={handleEditCancel}
                style={{
                  padding: '6px 14px',
                  borderRadius: '8px',
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'transparent',
                  color: 'var(--c-text-primary)',
                  fontSize: '14px',
                  cursor: 'pointer',
                  fontFamily: 'inherit',
                }}
              >
                Cancel
              </button>
              <button
                onClick={handleEditDone}
                disabled={!editText.trim()}
                style={{
                  padding: '6px 14px',
                  borderRadius: '8px',
                  border: 'none',
                  background: editText.trim() ? 'var(--c-text-primary)' : 'var(--c-text-muted)',
                  color: 'var(--c-bg-page)',
                  fontSize: '14px',
                  cursor: editText.trim() ? 'pointer' : 'default',
                  fontFamily: 'inherit',
                  fontWeight: 500,
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )
    }

    return (
      <div
        className="group"
        style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}
      >
        {/* hover 时左侧操作按钮 + 日期 */}
        <div
          className="opacity-0 group-hover:opacity-100 pointer-events-none group-hover:pointer-events-auto transition-opacity duration-[60ms]"
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-end',
            position: 'sticky',
            top: '6px',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '2px' }}>
          <button
            onClick={handleCopy}
            title="复制"
            style={{
              width: '32px',
              height: '32px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '8px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 60ms',
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
          >
            {copied ? <Check size={16} /> : <Copy size={16} />}
          </button>
          <button
            onClick={handleEditStart}
            title="编辑"
            style={{
              width: '32px',
              height: '32px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '8px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 60ms',
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
          >
            <Pencil size={16} />
          </button>
          </div>
          <div style={{ marginTop: '4px', paddingRight: '2px' }}>
            <MessageDate createdAt={message.created_at} />
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '8px', maxWidth: '663px' }}>
          {imageAttachments.length > 0 && accessToken && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', alignItems: 'flex-end' }}>
              {imageAttachments.map((part) => (
                <ArtifactImage
                  key={part.attachment.key}
                  artifact={part.attachment as ArtifactRef}
                  accessToken={accessToken}
                  pathPrefix="/v1/attachments"
                />
              ))}
            </div>
          )}
          {fileAttachments.length > 0 && accessToken && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
              {fileAttachments.map((part) => (
                <ArtifactDownload
                  key={part.attachment.key}
                  artifact={part.attachment as ArtifactRef}
                  accessToken={accessToken}
                  pathPrefix="/v1/attachments"
                />
              ))}
            </div>
          )}
          {((!accessToken && fileNames.length > 0) || (fileAttachments.length === 0 && imageAttachments.length === 0 && fileNames.length > 0)) && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
              {fileNames.map((name) => (
                <div
                  key={name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '6px',
                    background: 'var(--c-bg-sub)',
                    border: '0.5px solid var(--c-border-subtle)',
                    borderRadius: '8px',
                    padding: '4px 10px',
                    fontSize: '12px',
                    color: 'var(--c-text-secondary)',
                  }}
                >
                  <Paperclip size={11} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                  <span style={{ maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {name}
                  </span>
                </div>
              ))}
            </div>
          )}
          {displayText && (
            <div
              style={{
                background: 'var(--c-bg-deep)',
                borderRadius: '11px',
                padding: '10px 16px',
                color: 'var(--c-text-primary)',
                fontSize: '16px',
                fontWeight: 300,
                lineHeight: 1.6,
                letterSpacing: '-0.64px',
                wordBreak: 'break-word',
              }}
            >
              {displayText.split(/(\n{2,})/).map((part, i) =>
                /^\n{2,}$/.test(part)
                  ? <div key={i} style={{ height: '0.3em' }} />
                  : <span key={i} style={{ whiteSpace: 'pre-wrap' }}>{part}</span>
              )}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        {/* 文档类 artifact 卡片：仅显示被 message.content 引用的 */}
        {artifacts && onOpenDocument && (() => {
          const referenced = artifacts.filter((a) => isDocumentArtifact(a) && isArtifactReferenced(message.content, a.key))
          if (referenced.length === 0) return null
          return (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px', marginBottom: '14px' }}>
              {referenced.map((artifact) => (
                <DocumentCard
                  key={artifact.key}
                  artifact={artifact}
                  onClick={() => onOpenDocument(artifact)}
                  active={activePanelArtifactKey === artifact.key}
                />
              ))}
            </div>
          )
        })()}
        {/* Browser 截图卡片 */}
        {renderBrowserScreenshots(browserActions, accessToken)}
        <MarkdownRenderer content={message.content} webSources={webSources} artifacts={artifacts} accessToken={accessToken} runId={message.run_id} onOpenDocument={onOpenDocument} />
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
            <button
              onClick={onFork}
              disabled={!onFork}
              className={`flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[opacity,background] duration-[60ms] border-none bg-transparent ${onFork ? 'opacity-60 hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer' : 'opacity-25 cursor-default'}`}
            >
              <Split size={15} />
            </button>
            {onReport && (
              <div ref={moreRef} style={{ position: 'relative' }}>
                <button
                  onClick={() => setMoreOpen(v => !v)}
                  className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-60 transition-[opacity,background] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer border-none bg-transparent"
                >
                  <MoreHorizontal size={15} />
                </button>
                {moreOpen && (
                  <div
                    className="dropdown-menu"
                    style={{
                      position: 'absolute',
                      top: '100%',
                      left: 0,
                      marginTop: '4px',
                      background: 'var(--c-bg-menu)',
                      border: '0.5px solid var(--c-border-subtle)',
                      borderRadius: '10px',
                      padding: '4px',
                      zIndex: 20,
                      minWidth: '120px',
                      boxShadow: 'var(--c-dropdown-shadow)',
                    }}
                  >
                    <button
                      onClick={() => { setMoreOpen(false); onReport() }}
                      className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                      style={{ border: 'none', cursor: 'pointer', fontFamily: 'inherit' }}
                    >
                      <Flag size={13} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
                      {t.reportButton}
                    </button>
                  </div>
                )}
              </div>
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
                  background: 'var(--c-bg-deep)',
                  cursor: 'pointer',
                  marginLeft: '4px',
                  transition: 'background 60ms',
                  fontFamily: 'inherit',
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-plus)' }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
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
