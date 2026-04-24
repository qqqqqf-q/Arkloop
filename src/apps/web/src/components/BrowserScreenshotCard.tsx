import { useState, useEffect, useCallback, useRef } from 'react'
import { X, Download, ExternalLink, Globe, Loader2 } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { ArtifactRef } from '../storage'

const ANIM_MS = 120

type Props = {
  artifact: ArtifactRef
  accessToken: string
  command?: string
  url?: string
}

type SummaryProps = {
  command?: string
  url?: string
  output?: string
  exitCode?: number
}

export function BrowserActionSummaryCard({ command, url, output, exitCode }: SummaryProps) {
  const displayUrl = url || extractUrlFromCommand(command)
  const failed = typeof exitCode === 'number' && exitCode !== 0
  const statusText = typeof exitCode === 'number'
    ? failed ? `failed · exit ${exitCode}` : `completed · exit ${exitCode}`
    : 'completed'
  const outputText = output?.trim()
  return (
    <div style={{
      borderRadius: '10px',
      border: '0.5px solid var(--c-border-subtle)',
      background: 'var(--c-bg-page)',
      maxWidth: '560px',
      width: '100%',
      overflow: 'hidden',
    }}>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        padding: '8px 10px',
        background: 'var(--c-bg-sub)',
      }}>
        <Globe size={12} style={{ color: failed ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-muted)', flexShrink: 0 }} />
        <span style={{ fontSize: '12px', color: 'var(--c-text-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {displayUrl || command || 'browser action'}
        </span>
        <span style={{ marginLeft: 'auto', fontSize: '11px', color: failed ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-muted)', flexShrink: 0 }}>
          {statusText}
        </span>
      </div>
      {outputText && (
        <pre style={{
          margin: 0,
          padding: '8px 10px 10px',
          color: failed ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)',
          fontSize: '11px',
          lineHeight: 1.45,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          fontFamily: 'var(--font-mono, monospace)',
        }}>
          {outputText}
        </pre>
      )}
    </div>
  )
}

export function BrowserScreenshotCard({ artifact, accessToken, command, url }: Props) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [error, setError] = useState(false)
  const [loading, setLoading] = useState(true)
  const [visible, setVisible] = useState(false)
  const [show, setShow] = useState(false)
  const closingTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let cancelled = false
    const fetchUrl = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
    fetch(fetchUrl, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        return res.blob()
      })
      .then((blob) => {
        if (cancelled) return
        setBlobUrl(URL.createObjectURL(blob))
        setLoading(false)
      })
      .catch(() => {
        if (cancelled) return
        setError(true)
        setLoading(false)
      })
    return () => { cancelled = true }
  }, [artifact.key, accessToken])

  useEffect(() => {
    return () => { if (blobUrl) URL.revokeObjectURL(blobUrl) }
  }, [blobUrl])

  useEffect(() => {
    return () => { if (closingTimer.current) clearTimeout(closingTimer.current) }
  }, [])

  const openLightbox = useCallback(() => {
    if (closingTimer.current) clearTimeout(closingTimer.current)
    setVisible(true)
    requestAnimationFrame(() => requestAnimationFrame(() => setShow(true)))
  }, [])

  const closeLightbox = useCallback(() => {
    setShow(false)
    closingTimer.current = setTimeout(() => setVisible(false), ANIM_MS)
  }, [])

  useEffect(() => {
    if (!visible) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') closeLightbox() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [visible, closeLightbox])

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => { if (e.target === e.currentTarget) closeLightbox() },
    [closeLightbox],
  )

  const handleDownload = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (!blobUrl) return
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = artifact.filename
      a.click()
    },
    [blobUrl, artifact.filename],
  )

  const displayUrl = url || extractUrlFromCommand(command)

  const transition = `all ${ANIM_MS}ms ease-out`

  return (
    <>
      <div style={{
        borderRadius: '10px',
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-page)',
        overflow: 'hidden',
        maxWidth: '560px',
        width: '100%',
      }}>
        {/* address bar */}
        <div style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '6px 10px',
          borderBottom: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-sub)',
        }}>
          <Globe size={12} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
          <span style={{
            fontSize: '11px',
            color: 'var(--c-text-tertiary)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            fontFamily: 'var(--font-mono, monospace)',
          }}>
            {displayUrl || 'browser'}
          </span>
        </div>

        {/* screenshot area - 16:9 aspect ratio */}
        <div
          onClick={blobUrl ? openLightbox : undefined}
          style={{
            position: 'relative',
            width: '100%',
            paddingBottom: '56.25%', // 16:9
            cursor: blobUrl ? 'pointer' : 'default',
            background: 'var(--c-bg-deep)',
          }}
        >
          {loading && (
            <div style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}>
              <Loader2 size={20} className="animate-spin" style={{ color: 'var(--c-text-muted)' }} />
            </div>
          )}
          {error && (
            <div style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--c-text-muted)',
              fontSize: '12px',
            }} />
          )}
          {blobUrl && (
            <img
              src={blobUrl}
              alt={artifact.filename}
              draggable={false}
              style={{
                position: 'absolute',
                inset: 0,
                width: '100%',
                height: '100%',
                objectFit: 'contain',
                borderRadius: '0 0 9px 9px',
              }}
            />
          )}
        </div>
      </div>

      {/* lightbox */}
      {visible && (
        <div
          onClick={handleOverlayClick}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9999,
            background: show ? 'var(--c-lightbox-overlay)' : 'transparent',
            backdropFilter: show ? 'blur(12px)' : 'blur(0px)',
            WebkitBackdropFilter: show ? 'blur(12px)' : 'blur(0px)',
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
            className="flex h-7 w-7 items-center justify-center rounded-lg transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{
              position: 'absolute',
              top: 16,
              right: 16,
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-muted)',
              cursor: 'pointer',
              opacity: show ? 1 : 0,
              transition,
            }}
          >
            <X size={16} />
          </button>

          <img
            src={blobUrl!}
            alt={artifact.filename}
            draggable={false}
            onClick={closeLightbox}
            style={{
              maxWidth: '90vw',
              maxHeight: 'calc(90vh - 64px)',
              borderRadius: '8px',
              cursor: 'pointer',
              transform: show ? 'scale(1)' : 'scale(0.94)',
              opacity: show ? 1 : 0,
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
              transform: show ? 'translateY(0)' : 'translateY(6px)',
              opacity: show ? 1 : 0,
              transition,
            }}
          >
            <a
              href={blobUrl!}
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

function extractUrlFromCommand(command?: string): string | undefined {
  if (!command) return undefined
  const trimmed = command.trim()
  if (trimmed.startsWith('navigate ')) {
    return trimmed.slice('navigate '.length).trim()
  }
  if (trimmed.startsWith('tab new ')) {
    return trimmed.slice('tab new '.length).trim()
  }
  return undefined
}
