import { useState, useMemo, useCallback, useEffect } from 'react'
import { File, FileCode, FileText, FileVideo, FileAudio, Download } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import { useMessageMeta } from '../contexts/message-meta'
import { useLocale } from '../contexts/LocaleContext'
import type { ArtifactRef } from '../storage'
import './ArtifactGalleryPanel.css'

type FilterType = 'all' | 'media' | 'text'

const PATH_PREFIX = '/v1/artifacts'

function isMediaType(mime: string): boolean {
  return mime.startsWith('image/') || mime.startsWith('video/') || mime.startsWith('audio/')
}

function isTextType(mime: string): boolean {
  return !isMediaType(mime)
}

function extension(filename: string): string {
  const dot = filename.lastIndexOf('.')
  return dot >= 0 ? filename.slice(dot + 1).toUpperCase() : '?'
}

function FileIcon({ mimeType }: { mimeType: string }) {
  if (mimeType.startsWith('video/')) return <FileVideo size={28} />
  if (mimeType.startsWith('audio/')) return <FileAudio size={28} />
  if (mimeType === 'text/plain') return <FileText size={28} />
  if (mimeType.startsWith('text/') ||
      mimeType === 'application/json' ||
      mimeType === 'application/xml' ||
      mimeType === 'application/javascript' ||
      mimeType === 'text/html') return <FileCode size={28} />
  return <File size={28} />
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function ArtifactThumbnail({ artifact, accessToken, onClick }: {
  artifact: ArtifactRef
  accessToken: string
  onClick: () => void
}) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [imgError, setImgError] = useState(false)

  useEffect(() => {
    if (!isMediaType(artifact.mime_type) || !artifact.mime_type.startsWith('image/')) return
    let cancelled = false
    const url = `${apiBaseUrl()}${PATH_PREFIX}/${artifact.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        return res.blob()
      })
      .then((blob) => {
        if (cancelled) return
        setBlobUrl(URL.createObjectURL(blob))
      })
      .catch(() => {
        if (cancelled) return
        setImgError(true)
      })
    return () => { cancelled = true }
  }, [artifact.key, artifact.mime_type, accessToken])

  useEffect(() => {
    return () => {
      if (blobUrl) URL.revokeObjectURL(blobUrl)
    }
  }, [blobUrl])

  const showImage = artifact.mime_type.startsWith('image/') && blobUrl && !imgError

  return (
    <div className="artifact-card" onClick={onClick} role="button" tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onClick() }}>
      <div className="artifact-card__thumb">
        {showImage ? (
          <img src={blobUrl} alt={artifact.filename} loading="lazy"
            className="artifact-card__img"
            onError={() => setImgError(true)} />
        ) : (
          <div className="artifact-card__icon">
            <FileIcon mimeType={artifact.mime_type} />
            <span className="artifact-card__ext">{extension(artifact.filename)}</span>
            {artifact.size > 0 && (
              <span className="artifact-card__size">{formatSize(artifact.size)}</span>
            )}
          </div>
        )}
      </div>
      <div className="artifact-card__footer">
        <span className="artifact-card__name" title={artifact.filename}>{artifact.filename}</span>
        <DownloadButton artifact={artifact} accessToken={accessToken} />
      </div>
    </div>
  )
}

function DownloadButton({ artifact, accessToken }: { artifact: ArtifactRef; accessToken: string }) {
  const handleDownload = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      const url = `${apiBaseUrl()}${PATH_PREFIX}/${artifact.key}`
      const res = await fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
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
    } catch { /* ignore */ }
  }, [artifact.key, artifact.filename, accessToken])

  return (
    <button className="artifact-card__download" onClick={handleDownload}
      title="Download" aria-label={`Download ${artifact.filename}`}>
      <Download size={12} />
    </button>
  )
}

export function ArtifactGalleryPanel({ accessToken, onOpenArtifact }: {
  accessToken: string
  onOpenArtifact: (artifact: ArtifactRef) => void
}) {
  const { metaMap } = useMessageMeta()
  const { t } = useLocale()
  const [filter, setFilter] = useState<FilterType>('all')

  const artifacts = useMemo(() => {
    const seen = new Set<string>()
    const result: ArtifactRef[] = []
    for (const meta of metaMap.values()) {
      if (!meta.artifacts) continue
      for (const a of meta.artifacts) {
        if (seen.has(a.key)) continue
        seen.add(a.key)
        result.push(a)
      }
    }
    if (filter === 'all') return result
    if (filter === 'media') return result.filter(a => isMediaType(a.mime_type))
    return result.filter(a => isTextType(a.mime_type))
  }, [metaMap, filter])

  return (
    <div className="artifact-gallery">
      <div className="artifact-gallery__filter">
        <div className="artifact-gallery__filter-group">
          {(['all', 'media', 'text'] as const).map((type) => (
            <button key={type}
              className={`artifact-gallery__filter-btn${filter === type ? ' artifact-gallery__filter-btn--active' : ''}`}
              onClick={() => setFilter(type)}>
              {type === 'all' ? t.rightPanel.allFiles : type === 'media' ? t.rightPanel.mediaFiles : t.rightPanel.textFiles}
            </button>
          ))}
        </div>
        <span className="artifact-gallery__count">{artifacts.length} files</span>
      </div>
      {artifacts.length === 0 ? (
        <div className="artifact-gallery__empty">
          {filter === 'all' ? t.rightPanel.noArtifacts : t.rightPanel.noMatchingFiles}
        </div>
      ) : (
        <div className="artifact-gallery__grid">
          {artifacts.map((a) => (
            <ArtifactThumbnail key={a.key} artifact={a} accessToken={accessToken}
              onClick={() => onOpenArtifact(a)} />
          ))}
        </div>
      )}
    </div>
  )
}
