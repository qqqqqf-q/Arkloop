import { useState, useEffect, useCallback, useRef } from 'react'
import { X, Download, ExternalLink } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { ArtifactRef } from '../storage'

const ANIM_MS = 120

type Props = {
  artifact: ArtifactRef
  accessToken: string
  pathPrefix?: string
}

export function ArtifactImage({ artifact, accessToken, pathPrefix = '/v1/artifacts' }: Props) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [error, setError] = useState(false)
  const [loading, setLoading] = useState(true)
  const [visible, setVisible] = useState(false)
  const [show, setShow] = useState(false)
  const closingTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let cancelled = false
    const url = `${apiBaseUrl()}${pathPrefix}/${artifact.key}`

    fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
    })
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

    return () => {
      cancelled = true
    }
  }, [artifact.key, accessToken])

  useEffect(() => {
    return () => {
      if (blobUrl) URL.revokeObjectURL(blobUrl)
    }
  }, [blobUrl])

  useEffect(() => {
    return () => {
      if (closingTimer.current) clearTimeout(closingTimer.current)
    }
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
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeLightbox()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [visible, closeLightbox])

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) closeLightbox()
    },
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

  if (error) return null
  if (loading) {
    return (
      <div
        style={{
          width: '100%',
          maxWidth: '480px',
          height: '200px',
          borderRadius: '10px',
          background: 'var(--c-bg-sub)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'var(--c-text-tertiary)',
          fontSize: '13px',
        }}
      />
    )
  }

  const transition = `all ${ANIM_MS}ms ease-out`

  return (
    <>
      <div
        style={{
          display: 'inline-block',
          border: '0.5px solid var(--c-border-subtle)',
          borderRadius: '12px',
          padding: '8px',
        }}
      >
        <img
          src={blobUrl!}
          alt={artifact.filename}
          draggable={false}
          onClick={openLightbox}
          style={{
            maxWidth: '100%',
            display: 'block',
            borderRadius: '6px',
            cursor: 'default',
          }}
        />
      </div>
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
                background: 'var(--c-bg-sub)',
                color: 'var(--c-text-primary)',
                fontSize: 14,
                textDecoration: 'none',
                fontFamily: 'inherit',
                transition: 'background 150ms',
              }}
              onMouseEnter={(e) => {
                ;(e.currentTarget as HTMLAnchorElement).style.background = 'var(--c-bg-deep)'
              }}
              onMouseLeave={(e) => {
                ;(e.currentTarget as HTMLAnchorElement).style.background = 'var(--c-bg-sub)'
              }}
            >
              <span
                style={{
                  maxWidth: 220,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
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
                background: 'var(--c-bg-sub)',
                color: 'var(--c-text-icon)',
                cursor: 'pointer',
                fontFamily: 'inherit',
                transition: 'background 150ms',
              }}
              onMouseEnter={(e) => {
                ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)'
              }}
              onMouseLeave={(e) => {
                ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-sub)'
              }}
            >
              <Download size={16} />
            </button>
          </div>
        </div>
      )}
    </>
  )
}
