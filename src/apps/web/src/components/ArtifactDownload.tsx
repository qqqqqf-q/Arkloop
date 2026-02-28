import { useState, useCallback } from 'react'
import { FileDown } from 'lucide-react'
import type { ArtifactRef } from '../storage'

function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

type Props = {
  artifact: ArtifactRef
  accessToken: string
}

export function ArtifactDownload({ artifact, accessToken }: Props) {
  const [downloading, setDownloading] = useState(false)

  const handleDownload = useCallback(async () => {
    if (downloading) return
    setDownloading(true)
    try {
      const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
      const res = await fetch(url, {
        headers: { Authorization: `Bearer ${accessToken}` },
      })
      if (!res.ok) throw new Error(`${res.status}`)
      const blob = await res.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = artifact.filename
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(blobUrl)
    } catch {
      // 静默失败，不阻断 UI
    } finally {
      setDownloading(false)
    }
  }, [artifact, accessToken, downloading])

  return (
    <button
      onClick={handleDownload}
      disabled={downloading}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '8px',
        padding: '8px 14px',
        borderRadius: '10px',
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-sub)',
        cursor: downloading ? 'default' : 'pointer',
        fontFamily: 'inherit',
        transition: 'background 150ms',
        maxWidth: '100%',
      }}
      onMouseEnter={(e) => { if (!downloading) (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-sub)' }}
    >
      <FileDown size={16} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
      <span
        style={{
          fontSize: '14px',
          color: 'var(--c-text-primary)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {artifact.filename}
      </span>
      {artifact.size > 0 && (
        <span style={{ fontSize: '12px', color: 'var(--c-text-tertiary)', flexShrink: 0 }}>
          {formatSize(artifact.size)}
        </span>
      )}
    </button>
  )
}
