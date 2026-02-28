import { useState, useEffect, useRef } from 'react'
import type { ArtifactRef } from '../storage'

function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

type Props = {
  artifact: ArtifactRef
  accessToken: string
}

export function ArtifactHtmlPreview({ artifact, accessToken }: Props) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [error, setError] = useState(false)
  const [loading, setLoading] = useState(true)
  const iframeRef = useRef<HTMLIFrameElement>(null)

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

  useEffect(() => {
    return () => {
      if (blobUrl) URL.revokeObjectURL(blobUrl)
    }
  }, [blobUrl])

  // 通过 postMessage 监听 iframe 内容高度
  useEffect(() => {
    const handler = (e: MessageEvent) => {
      if (e.data?.type === 'arkloop-iframe-resize' && typeof e.data.height === 'number' && iframeRef.current) {
        iframeRef.current.style.height = `${e.data.height}px`
      }
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [])

  if (error) return null
  if (loading) {
    return (
      <div
        style={{
          width: '100%',
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
    <iframe
      ref={iframeRef}
      src={blobUrl!}
      sandbox="allow-scripts"
      style={{
        width: '100%',
        minHeight: '300px',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        background: '#fff',
      }}
    />
  )
}
