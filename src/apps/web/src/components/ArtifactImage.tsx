import { useState, useEffect, useCallback } from 'react'
import type { ArtifactRef } from '../storage'

function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

type Props = {
  artifact: ArtifactRef
  accessToken: string
}

export function ArtifactImage({ artifact, accessToken }: Props) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [error, setError] = useState(false)
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    let cancelled = false
    const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`

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

  // 释放 blob URL
  useEffect(() => {
    return () => {
      if (blobUrl) URL.revokeObjectURL(blobUrl)
    }
  }, [blobUrl])

  const handleClose = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    setExpanded(false)
  }, [])

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
          onClick={() => setExpanded(true)}
          style={{
            maxWidth: '100%',
            display: 'block',
            borderRadius: '6px',
            cursor: 'pointer',
            transition: 'opacity 150ms',
          }}
        />
      </div>
      {expanded && (
        <div
          onClick={handleClose}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9999,
            background: 'rgba(0,0,0,0.75)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'zoom-out',
          }}
        >
          <img
            src={blobUrl!}
            alt={artifact.filename}
            style={{
              maxWidth: '90vw',
              maxHeight: '90vh',
              borderRadius: '8px',
            }}
          />
        </div>
      )}
    </>
  )
}
