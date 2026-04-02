import { useState, useRef, useEffect, useCallback } from 'react'
import { X, Download, ExternalLink } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { ArtifactRef } from '../../storage'
import { LIGHTBOX_ANIM_MS } from './utils'
import { CopyIconButton } from '../CopyIconButton'

type Props = {
  artifact: ArtifactRef
  accessToken: string
  pathPrefix?: string
}

export function ImageThumbnailCard({ artifact, accessToken, pathPrefix = '/v1/artifacts' }: Props) {
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

  const handleCopyImage = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!blobUrl || !navigator.clipboard?.write) return
    try {
      const res = await fetch(blobUrl)
      const blob = await res.blob()
      const mime = blob.type && blob.type !== '' ? blob.type : 'image/png'
      await navigator.clipboard.write([new ClipboardItem({ [mime]: blob })])
    } catch {
      // clipboard / permission
    }
  }, [blobUrl])

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
            draggable={false}
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
            <CopyIconButton
              onCopy={() => handleCopyImage({} as React.MouseEvent)}
              size={16}
              tooltip="Copy"
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
