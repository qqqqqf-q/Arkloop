import { useState, useEffect, useRef, useCallback } from 'react'
import { X, Download } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { Attachment } from '../ChatInput'
import { LIGHTBOX_ANIM_MS } from '../messagebubble/utils'
import { CopyIconButton } from '../CopyIconButton'

export const BAR_COUNT = 52

export function hasTransferFiles(dataTransfer?: DataTransfer | null): boolean {
  if (!dataTransfer) return false
  const types = Array.from(dataTransfer.types ?? [])
  if (types.includes('Files')) return true
  if ((dataTransfer.files?.length ?? 0) > 0) return true
  if (Array.from(dataTransfer.items ?? []).some((item) => item.kind === 'file')) return true
  // Electron: clipboard images from screenshots/apps may only expose image/* types
  if (types.some((t) => t.startsWith('image/'))) return true
  return false
}

export function extractFilesFromTransfer(dataTransfer?: DataTransfer | null): File[] {
  if (!dataTransfer) return []
  const files: File[] = []
  const seenTypes = new Set<string>()

  const items = Array.from(dataTransfer.items ?? [])

  // Prefer items API (supports clipboard images in Electron)
  const itemFiles = items
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((f): f is File => f != null)

  const dtFiles = Array.from(dataTransfer.files ?? [])

  const allFiles = itemFiles.length > 0 ? itemFiles : dtFiles

  if (allFiles.length > 0) {
    for (const file of allFiles) {
      const prefix = file.type.split('/')[0]
      if (prefix === 'image') {
        if (seenTypes.has('image')) continue
        seenTypes.add('image')
      }
      files.push(file)
    }
    return files
  }

  // Electron fallback: clipboard image items may be typed image/* with kind 'file'
  // but getAsFile() returned null. Try to build a Blob from the DataTransferItem.
  // This handles cases where the clipboard image kind check passes but file is null.
  for (const item of items) {
    if (!item.type.startsWith('image/')) continue
    if (seenTypes.has('image')) continue
    const file = item.getAsFile()
    if (file) {
      seenTypes.add('image')
      files.push(file)
    }
  }

  return files
}

export function isEditableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  const tagName = target.tagName
  return tagName === 'INPUT' || tagName === 'TEXTAREA' || tagName === 'SELECT'
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function AttachmentCard({
  attachment,
  onRemove,
  accessToken,
}: {
  attachment: Attachment
  onRemove: () => void
  accessToken?: string
}) {
  const [imageLoaded, setImageLoaded] = useState(false)
  const [lineCount, setLineCount] = useState<number | null>(null)
  const [cardHovered, setCardHovered] = useState(false)
  const [lbVisible, setLbVisible] = useState(false)
  const [lbShow, setLbShow] = useState(false)
  const [remoteBlobUrl, setRemoteBlobUrl] = useState<string | null>(null)
  const [fetchError, setFetchError] = useState(false)
  const closingTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isImage = attachment.mime_type.startsWith('image/')
  const previewSrc = attachment.preview_url ?? remoteBlobUrl

  useEffect(() => {
    if (isImage) {
      setLineCount(null)
      return
    }
    if (attachment.pasted?.lineCount != null) {
      setLineCount(attachment.pasted.lineCount)
      return
    }
    if (attachment.uploaded?.extracted_text) {
      setLineCount(attachment.uploaded.extracted_text.split('\n').length)
      return
    }
    if (!attachment.file) {
      setLineCount(0)
      return
    }
    const reader = new FileReader()
    reader.onload = (e) => {
      const text = e.target?.result as string
      setLineCount(text.split('\n').length)
    }
    reader.readAsText(attachment.file)
  }, [attachment.file, attachment.pasted?.lineCount, attachment.uploaded?.extracted_text, isImage])

  useEffect(() => {
    setImageLoaded(false)
    setFetchError(false)
    if (!attachment.preview_url) setRemoteBlobUrl(null)
  }, [attachment.id, attachment.preview_url])

  useEffect(() => {
    return () => {
      if (remoteBlobUrl) URL.revokeObjectURL(remoteBlobUrl)
    }
  }, [remoteBlobUrl])

  useEffect(() => {
    if (!isImage || attachment.preview_url || !attachment.uploaded?.key || !accessToken) return
    let cancelled = false
    let localBlobUrl: string | null = null
    const url = `${apiBaseUrl()}/v1/attachments/${attachment.uploaded.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        return res.blob()
      })
      .then((blob) => {
        localBlobUrl = URL.createObjectURL(blob)
        if (!cancelled) setRemoteBlobUrl(localBlobUrl)
      })
      .catch(() => {
        if (!cancelled) setFetchError(true)
      })
    return () => {
      cancelled = true
      if (localBlobUrl) URL.revokeObjectURL(localBlobUrl)
    }
  }, [accessToken, attachment.preview_url, attachment.uploaded?.key, isImage])

  useEffect(() => {
    return () => { if (closingTimer.current) clearTimeout(closingTimer.current) }
  }, [])

  const openLightbox = useCallback(() => {
    if (!isImage || !previewSrc) return
    if (closingTimer.current) clearTimeout(closingTimer.current)
    setLbVisible(true)
    requestAnimationFrame(() => requestAnimationFrame(() => setLbShow(true)))
  }, [isImage, previewSrc])

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
    if (!previewSrc) return
    const a = document.createElement('a')
    a.href = previewSrc
    a.download = attachment.name
    a.click()
  }, [attachment.name, previewSrc])

  const handleCopyImage = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!previewSrc || !navigator.clipboard?.write) return
    try {
      const res = await fetch(previewSrc)
      const blob = await res.blob()
      const mime = blob.type && blob.type !== '' ? blob.type : 'image/png'
      await navigator.clipboard.write([new ClipboardItem({ [mime]: blob })])
    } catch {
      // clipboard / permission
    }
  }, [previewSrc])

  const ext = attachment.name.includes('.')
    ? attachment.name.split('.').pop()!.toUpperCase()
    : ''
  const uploading = attachment.status === 'uploading'
  const ready = !uploading && (isImage ? (imageLoaded || fetchError) : lineCount !== null)
  const transition = `all ${LIGHTBOX_ANIM_MS}ms ease-out`

  return (
    <>
      <div style={{ position: 'relative', flexShrink: 0 }}
        onMouseEnter={() => setCardHovered(true)}
        onMouseLeave={() => setCardHovered(false)}
      >
        <div
          onClick={isImage ? openLightbox : undefined}
          style={{
            width: '120px',
            height: '120px',
            borderRadius: '10px',
            background: 'var(--c-bg-input)',
            overflow: 'hidden',
            borderWidth: '0.7px',
            borderStyle: 'solid',
            borderColor: cardHovered ? 'var(--c-attachment-border-hover)' : 'var(--c-input-border-color)',
            transition: 'border-color 0.2s ease',
            cursor: isImage ? 'pointer' : 'default',
          }}
        >
          {!ready && (
            <div style={{
              position: 'absolute', inset: 0, padding: '10px',
              display: 'flex', flexDirection: 'column', gap: '8px',
            }}>
              <div className="attachment-shimmer" style={{ width: '80%', height: '10px', borderRadius: '5px' }} />
              <div className="attachment-shimmer" style={{ width: '55%', height: '10px', borderRadius: '5px' }} />
              <div style={{ flex: 1 }} />
              <div className="attachment-shimmer" style={{ width: '30%', height: '10px', borderRadius: '5px' }} />
            </div>
          )}

          {isImage ? (
            previewSrc ? (
              <img
                src={previewSrc}
                alt={attachment.name}
                onLoad={() => setImageLoaded(true)}
                onError={() => setFetchError(true)}
                style={{
                  width: '100%',
                  height: '100%',
                  objectFit: 'cover',
                  opacity: ready ? 1 : 0,
                  transition: 'opacity 0.2s ease',
                  display: 'block',
                }}
              />
            ) : fetchError ? (
              <div style={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--c-bg-deep)' }}>
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--c-text-muted)' }}>
                  <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" strokeWidth="1.5" fill="none" />
                  <path d="M3 16l5-5 4 4 3-3 6 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" fill="none" />
                  <circle cx="8.5" cy="8.5" r="1.5" fill="currentColor" />
                </svg>
              </div>
            ) : null
          ) : (
            <div style={{
              padding: '10px',
              display: 'flex', flexDirection: 'column',
              height: '100%',
              opacity: ready ? 1 : 0,
              transition: 'opacity 0.2s ease',
            }}>
              <span style={{
                color: 'var(--c-text-heading)',
                fontSize: '12px',
                fontWeight: 300,
                lineHeight: '1.35',
                wordBreak: 'break-all',
                display: '-webkit-box',
                WebkitLineClamp: 3,
                WebkitBoxOrient: 'vertical',
                overflow: 'hidden',
              }}>
                {attachment.name}
              </span>
              {lineCount !== null && (
                <span style={{ color: 'var(--c-text-muted)', fontSize: '11px', marginTop: '3px' }}>
                  {lineCount} lines
                </span>
              )}
              <div style={{ flex: 1 }} />
              {ext && (
                <span style={{
                  alignSelf: 'flex-start',
                  padding: '2px 6px',
                  borderRadius: '5px',
                  background: 'var(--c-attachment-bg)',
                  border: '0.5px solid var(--c-attachment-badge-border)',
                  color: 'var(--c-text-secondary)',
                  fontSize: '10px',
                  fontWeight: 500,
                }}>
                  {ext}
                </span>
              )}
            </div>
          )}
        </div>

        <button
          type="button"
          className="attachment-close-btn"
          onClick={(e) => { e.stopPropagation(); onRemove() }}
          style={{
            position: 'absolute',
            top: '-5px',
            left: '-5px',
            width: '18px',
            height: '18px',
            borderRadius: '50%',
            background: 'var(--c-bg-input)',
            border: '0.5px solid var(--c-attachment-close-border)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'pointer',
            opacity: cardHovered ? 1 : 0,
            transition: 'opacity 0.15s ease',
            pointerEvents: cardHovered ? 'auto' : 'none',
            zIndex: 1,
          }}
        >
          <X size={9} />
        </button>
      </div>

      {lbVisible && previewSrc && isImage && (
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
              top: 14,
              right: 15,
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
              opacity: lbShow ? 1 : 0,
              transition,
            }}
            className="hover:bg-[var(--c-bg-deep)]"
          >
            <X size={16} />
          </button>

          <img
            src={previewSrc}
            alt={attachment.name}
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
              href={previewSrc}
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
              className="bg-[var(--c-bg-input)] hover:bg-[var(--c-bg-sub)]"
            >
              <span style={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {attachment.name}
              </span>
            </a>
            <CopyIconButton
              onCopy={() => handleCopyImage({} as React.MouseEvent)}
              size={16}
              tooltip="复制图片"
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
              className="bg-[var(--c-bg-input)] hover:bg-[var(--c-bg-sub)]"
            />
            <button
              type="button"
              onClick={handleDownload}
              title="下载"
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
              className="bg-[var(--c-bg-input)] hover:bg-[var(--c-bg-sub)]"
            >
              <Download size={16} />
            </button>
          </div>
        </div>
      )}
    </>
  )
}
