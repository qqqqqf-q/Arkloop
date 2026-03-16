import { useState, useRef, useEffect, useCallback } from 'react'
import { Copy, Check, RefreshCw, Share2, Split, Paperclip, Pencil, MoreHorizontal, Flag, X, Download, ExternalLink } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { MessageResponse } from '../api'
import type { WebSource, ArtifactRef } from '../storage'
import type { BrowserActionRef } from '../storage'
import { MarkdownRenderer } from './MarkdownRenderer'
import { useTypewriter } from '../hooks/useTypewriter'
import { ArtifactDownload } from './ArtifactDownload'
import { DocumentCard } from './DocumentCard'
import { BrowserScreenshotCard } from './BrowserScreenshotCard'
import { PastedContentModal } from './PastedContentModal'
import { useLocale } from '../contexts/LocaleContext'
import { extractLegacyFilesFromContent, isFilePart, isImagePart, isPastedFile, messageAttachmentParts, messageTextContent } from '../messageContent'

function isDocumentArtifact(artifact: ArtifactRef): boolean {
  if (artifact.display === 'panel') return true
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
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  activePanelArtifactKey?: string | null
}

function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

const LIGHTBOX_ANIM_MS = 120

const USER_TEXT_LINE_HEIGHT = 25.6 // 16px * 1.6
const USER_TEXT_MAX_LINES = 9
const USER_TEXT_COLLAPSED_HEIGHT = USER_TEXT_LINE_HEIGHT * USER_TEXT_MAX_LINES
const USER_TEXT_FADE_HEIGHT = USER_TEXT_LINE_HEIGHT * 2

function ImageThumbnailCard({
  artifact,
  accessToken,
  pathPrefix = '/v1/artifacts',
}: {
  artifact: ArtifactRef
  accessToken: string
  pathPrefix?: string
}) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [lbVisible, setLbVisible] = useState(false)
  const [lbShow, setLbShow] = useState(false)
  const closingTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let cancelled = false
    const url = `${apiBaseUrl()}${pathPrefix}/${artifact.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        return res.blob()
      })
      .then((blob) => {
        if (!cancelled) setBlobUrl(URL.createObjectURL(blob))
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [artifact.key, accessToken, pathPrefix])

  useEffect(() => {
    return () => { if (blobUrl) URL.revokeObjectURL(blobUrl) }
  }, [blobUrl])

  useEffect(() => {
    return () => { if (closingTimer.current) clearTimeout(closingTimer.current) }
  }, [])

  const openLightbox = useCallback(() => {
    if (closingTimer.current) clearTimeout(closingTimer.current)
    setLbVisible(true)
    requestAnimationFrame(() => requestAnimationFrame(() => setLbShow(true)))
  }, [])

  const closeLightbox = useCallback(() => {
    setLbShow(false)
    closingTimer.current = setTimeout(() => setLbVisible(false), LIGHTBOX_ANIM_MS)
  }, [])

  useEffect(() => {
    if (!lbVisible) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') closeLightbox() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [lbVisible, closeLightbox])

  const handleDownload = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    if (!blobUrl) return
    const a = document.createElement('a')
    a.href = blobUrl
    a.download = artifact.filename
    a.click()
  }, [blobUrl, artifact.filename])

  const transition = `all ${LIGHTBOX_ANIM_MS}ms ease-out`

  return (
    <>
      <div
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        onClick={openLightbox}
        style={{
          width: '120px',
          height: '120px',
          borderRadius: '10px',
          overflow: 'hidden',
          borderWidth: '0.7px',
          borderStyle: 'solid',
          borderColor: hovered ? 'var(--c-attachment-border-hover)' : 'var(--c-attachment-border)',
          transition: 'border-color 0.2s ease',
          background: 'var(--c-attachment-bg)',
          flexShrink: 0,
          cursor: 'pointer',
        }}
      >
        {!loaded && (
          <div style={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <div className="attachment-shimmer" style={{ width: '80%', height: '80%', borderRadius: '6px' }} />
          </div>
        )}
        {blobUrl && (
          <img
            src={blobUrl}
            alt={artifact.filename}
            onLoad={() => setLoaded(true)}
            style={{
              width: '100%',
              height: '100%',
              objectFit: 'cover',
              display: 'block',
              opacity: loaded ? 1 : 0,
              transition: 'opacity 0.2s ease',
            }}
          />
        )}
      </div>

      {lbVisible && blobUrl && (
        <div
          onClick={(e) => { if (e.target === e.currentTarget) closeLightbox() }}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9999,
            background: lbShow ? 'var(--c-lightbox-overlay)' : 'transparent',
            backdropFilter: lbShow ? 'blur(12px)' : 'blur(0px)',
            WebkitBackdropFilter: lbShow ? 'blur(12px)' : 'blur(0px)',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'default',
            transition,
          }}
        >
          <button
            onClick={closeLightbox}
            style={{
              position: 'absolute',
              top: 16,
              right: 16,
              width: '28px',
              height: '28px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '8px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-muted)',
              cursor: 'pointer',
              opacity: lbShow ? 1 : 0,
              transition,
            }}
          >
            <X size={16} />
          </button>

          <img
            src={blobUrl}
            alt={artifact.filename}
            draggable={false}
            onClick={closeLightbox}
            style={{
              maxWidth: '90vw',
              maxHeight: 'calc(90vh - 64px)',
              borderRadius: '8px',
              cursor: 'pointer',
              transform: lbShow ? 'scale(1)' : 'scale(0.94)',
              opacity: lbShow ? 1 : 0,
              transition,
            }}
          />

          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              marginTop: 16,
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              cursor: 'default',
              transform: lbShow ? 'translateY(0)' : 'translateY(6px)',
              opacity: lbShow ? 1 : 0,
              transition,
            }}
          >
            <a
              href={blobUrl}
              target="_blank"
              rel="noopener noreferrer"
              draggable={false}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 8,
                padding: '8px 14px',
                borderRadius: 10,
                border: '0.5px solid var(--c-border-subtle)',
                color: 'var(--c-text-primary)',
                fontSize: 14,
                textDecoration: 'none',
                fontFamily: 'inherit',
                transition: 'background 150ms',
              }}
              className="bg-[var(--c-bg-sub)] hover:bg-[var(--c-bg-deep)]"
            >
              <span style={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {artifact.filename}
              </span>
              <ExternalLink size={14} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
            </a>
            <button
              onClick={handleDownload}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 36,
                height: 36,
                borderRadius: 10,
                border: '0.5px solid var(--c-border-subtle)',
                color: 'var(--c-text-icon)',
                cursor: 'pointer',
                fontFamily: 'inherit',
                transition: 'background 150ms',
              }}
              className="bg-[var(--c-bg-sub)] hover:bg-[var(--c-bg-deep)]"
            >
              <Download size={16} />
            </button>
          </div>
        </div>
      )}
    </>
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

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function PastedBubbleCard({
  preview,
  fullText,
  size,
}: {
  preview: string
  fullText: string
  size: number
}) {
  const [hovered, setHovered] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const lineCount = fullText.split('\n').length

  return (
    <>
      <div
        onClick={() => setModalOpen(true)}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          width: '120px',
          height: '120px',
          borderRadius: '10px',
          background: 'var(--c-bg-page)',
          overflow: 'hidden',
          borderWidth: '0.7px',
          borderStyle: 'solid',
          borderColor: hovered ? 'var(--c-attachment-border-hover)' : 'var(--c-border-subtle)',
          transition: 'border-color 0.2s ease',
          cursor: 'pointer',
          padding: '10px',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{
          color: 'var(--c-text-secondary)',
          fontSize: '11px',
          lineHeight: '1.4',
          display: '-webkit-box',
          WebkitLineClamp: 4,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
          wordBreak: 'break-all',
          flex: 1,
        }}>
          {preview}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '4px' }}>
          <span style={{ fontSize: '9px', color: 'var(--c-text-muted)', whiteSpace: 'nowrap' }}>
            {formatSize(size)}
          </span>
          <span style={{
            padding: '1px 6px',
            borderRadius: '5px',
            background: 'var(--c-attachment-bg)',
            border: '0.5px solid var(--c-attachment-badge-border)',
            color: 'var(--c-text-secondary)',
            fontSize: '10px',
            fontWeight: 500,
          }}>
            PASTED
          </span>
        </div>
      </div>

      {modalOpen && (
        <PastedContentModal
          text={fullText}
          size={size}
          lineCount={lineCount}
          onClose={() => setModalOpen(false)}
        />
      )}
    </>
  )
}


export function MessageBubble({ message, onRetry, onEdit, onFork, onShare, onReport, shareState, webSources, artifacts, browserActions, accessToken, onShowSources, onOpenDocument, activePanelArtifactKey }: Props) {
  const { t } = useLocale()
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editText, setEditText] = useState('')
  const [moreOpen, setMoreOpen] = useState(false)
  const [userTextExpanded, setUserTextExpanded] = useState(false)
  const [userTextOverflows, setUserTextOverflows] = useState(false)
  const moreRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const userTextRef = useRef<HTMLDivElement>(null)

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

  useEffect(() => {
    if (message.role !== 'user' || !userTextRef.current) return
    const overflows = userTextRef.current.scrollHeight > USER_TEXT_COLLAPSED_HEIGHT + 1
    setUserTextOverflows(overflows)
    if (!overflows) setUserTextExpanded(false)
  }, [message.role, message.content])

  if (message.role === 'user') {
    const legacy = extractLegacyFilesFromContent(message.content)
    const attachmentParts = messageAttachmentParts(message)
    const imageAttachments = attachmentParts.filter(isImagePart)
    const allFileAttachments = attachmentParts.filter(isFilePart)
    const pastedAttachments = allFileAttachments.filter((p) => isPastedFile(p.attachment.filename))
    const fileAttachments = allFileAttachments.filter((p) => !isPastedFile(p.attachment.filename))
    const text = messageTextContent(message)
    const displayText = !accessToken && attachmentParts.length > 0 ? message.content : text
    const fileNames = attachmentParts.length > 0
      ? [...imageAttachments, ...allFileAttachments].map((part) => part.attachment.filename)
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
            title={t.copyAction}
            style={{
              width: '32px',
              height: '32px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '8px',
              border: 'none',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 60ms',
            }}
            className="hover:bg-[var(--c-bg-deep)]"
          >
            {copied ? <Check size={16} /> : <Copy size={16} />}
          </button>
          <button
            onClick={handleEditStart}
            title={t.editAction}
            style={{
              width: '32px',
              height: '32px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '8px',
              border: 'none',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 60ms',
            }}
            className="hover:bg-[var(--c-bg-deep)]"
          >
            <Pencil size={16} />
          </button>
          </div>
          <div style={{ marginTop: '4px', paddingRight: '2px' }}>
            <MessageDate createdAt={message.created_at} />
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '8px', maxWidth: '663px' }}>
          {(imageAttachments.length > 0 || pastedAttachments.length > 0) && accessToken && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px', justifyContent: 'flex-end' }}>
              {imageAttachments.map((part) => (
                <ImageThumbnailCard
                  key={part.attachment.key}
                  artifact={part.attachment as ArtifactRef}
                  accessToken={accessToken}
                  pathPrefix="/v1/attachments"
                />
              ))}
              {pastedAttachments.map((part) => {
                const fullText = part.extracted_text || ''
                const preview = fullText.split('\n').slice(0, 4).join('\n')
                return (
                  <PastedBubbleCard
                    key={part.attachment.key}
                    preview={preview}
                    fullText={fullText}
                    size={part.attachment.size}
                  />
                )
              })}
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
          {displayText && (() => {
            const isCollapsed = userTextOverflows && !userTextExpanded
            const fadeMask = `linear-gradient(to bottom, black calc(100% - ${USER_TEXT_FADE_HEIGHT}px), transparent)`
            return (
              <div
                style={{
                  background: 'var(--c-bg-deep)',
                  borderRadius: '11px',
                  padding: '10px 16px',
                  color: 'var(--c-text-primary)',
                  fontSize: '16.5px',
                  fontWeight: 350,
                  lineHeight: 1.6,
                  letterSpacing: '-0.64px',
                  wordBreak: 'break-word',
                }}
              >
                <div
                  ref={userTextRef}
                  style={{
                    maxHeight: !userTextExpanded ? `${USER_TEXT_COLLAPSED_HEIGHT}px` : undefined,
                    overflow: 'hidden',
                    ...(isCollapsed ? {
                      WebkitMaskImage: fadeMask,
                      maskImage: fadeMask,
                    } : {}),
                  }}
                >
                  {displayText.split(/(\n{2,})/).map((part, i) =>
                    /^\n{2,}$/.test(part)
                      ? <div key={i} style={{ height: '0.3em' }} />
                      : <span key={i} style={{ whiteSpace: 'pre-wrap' }}>{part}</span>
                  )}
                </div>
                {userTextOverflows && (
                  <div
                    onClick={() => setUserTextExpanded(prev => !prev)}
                    className="text-[var(--c-text-muted)] hover:text-[var(--c-text-icon)]"
                    style={{
                      marginTop: '6px',
                      fontSize: '13px',
                      fontWeight: 300,
                      cursor: 'pointer',
                      userSelect: 'none',
                      transition: 'color 150ms',
                    }}
                  >
                    {userTextExpanded ? 'Show less' : 'Show more'}
                  </div>
                )}
              </div>
            )
          })()}
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
                  onClick={(trigger) => onOpenDocument(artifact, { trigger, artifacts, runId: message.run_id })}
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
