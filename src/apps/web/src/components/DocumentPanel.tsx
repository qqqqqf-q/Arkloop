import { useState, useEffect, useCallback } from 'react'
import { X, FileText, Download, Eye, Code } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { ArtifactRef } from '../storage'
import { MarkdownRenderer } from './MarkdownRenderer'
import { ArtifactIframe } from './ArtifactIframe'
import { useLocale } from '../contexts/LocaleContext'

const toggleButtonWidth = 36
const toggleButtonHeight = 30
const toggleButtonGap = 2
const toggleButtonOffset = toggleButtonWidth + toggleButtonGap
const actionButtonSize = 30

const textLikeMimeTypes = new Set([
  'application/json',
  'application/ld+json',
  'application/xml',
  'application/javascript',
  'application/ecmascript',
  'application/typescript',
  'application/yaml',
  'application/x-yaml',
  'application/toml',
  'application/x-toml',
  'application/markdown',
  'application/x-markdown',
])

const textFallbackExtensions = new Set([
  'md', 'markdown', 'txt', 'log', 'json', 'jsonl', 'xml', 'yml', 'yaml', 'toml', 'ini', 'cfg', 'conf',
  'csv', 'tsv', 'js', 'jsx', 'mjs', 'cjs', 'ts', 'tsx', 'css', 'html', 'htm', 'sh', 'bash', 'zsh',
  'py', 'go', 'rs', 'java', 'c', 'cc', 'cpp', 'h', 'hpp', 'sql',
])

function normalizeMime(mime: string | null | undefined): string {
  return (mime ?? '').split(';', 1)[0]?.trim().toLowerCase() ?? ''
}

function getFilenameExtension(filename: string): string {
  const dot = filename.lastIndexOf('.')
  if (dot < 0 || dot === filename.length - 1) return ''
  return filename.slice(dot + 1).trim().toLowerCase()
}

function isTextMime(mime: string | null | undefined): boolean {
  const normalized = normalizeMime(mime)
  return normalized.startsWith('text/') || textLikeMimeTypes.has(normalized)
}

const iframeRenderableMimes = new Set(['text/html', 'image/svg+xml'])
const iframeRenderableExtensions = new Set(['html', 'htm', 'svg'])

function shouldRenderAsIframe(artifact: ArtifactRef): boolean {
  if (iframeRenderableMimes.has(normalizeMime(artifact.mime_type))) return true
  return iframeRenderableExtensions.has(getFilenameExtension(artifact.filename))
}

export function canPreviewDocumentAsText(serverMime: string | null | undefined, artifactMime: string | null | undefined, filename: string): boolean {
  if (isTextMime(serverMime) || isTextMime(artifactMime)) return true

  const normalizedServerMime = normalizeMime(serverMime)
  const normalizedArtifactMime = normalizeMime(artifactMime)
  const shouldUseExtensionFallback = normalizedServerMime === ''
    || normalizedServerMime === 'application/octet-stream'
    || normalizedArtifactMime === ''
    || normalizedArtifactMime === 'application/octet-stream'

  if (!shouldUseExtensionFallback) return false
  return textFallbackExtensions.has(getFilenameExtension(filename))
}

type ViewMode = 'preview' | 'source'

type Props = {
  artifact: ArtifactRef
  artifacts?: ArtifactRef[]
  accessToken: string
  runId?: string
  onClose: () => void
}

type LoadState =
  | { status: 'loading' }
  | { status: 'text'; content: string }
  | { status: 'binary' }
  | { status: 'error'; message: string }

export function DocumentPanel({ artifact, artifacts, accessToken, runId, onClose }: Props) {
  const { t } = useLocale()
  const [loadState, setLoadState] = useState<LoadState>({ status: 'loading' })
  const [downloading, setDownloading] = useState(false)
  const [mode, setMode] = useState<ViewMode>('preview')

  useEffect(() => {
    setLoadState({ status: 'loading' })
    const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        const serverMime = res.headers.get('content-type') ?? artifact.mime_type ?? ''
        if (canPreviewDocumentAsText(serverMime, artifact.mime_type, artifact.filename)) {
          const text = await res.text()
          setLoadState({ status: 'text', content: text })
        } else {
          setLoadState({ status: 'binary' })
        }
      })
      .catch((err: unknown) => {
        setLoadState({ status: 'error', message: err instanceof Error ? err.message : 'unknown' })
      })
  }, [artifact.filename, artifact.key, artifact.mime_type, accessToken])

  const handleDownload = useCallback(async () => {
    if (downloading) return
    setDownloading(true)
    try {
      const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
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
    } catch {
      // 静默失败
    } finally {
      setDownloading(false)
    }
  }, [artifact, accessToken, downloading])

  return (
    <div style={{ width: '100%', minWidth: 0, display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '12px 16px',
          flexShrink: 0,
          background: 'var(--c-bg-page)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', minWidth: 0 }}>
          <FileText size={16} color="var(--c-text-tertiary)" strokeWidth={2} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1px', minWidth: 0 }}>
            <span
              style={{
                fontSize: '13px',
                fontWeight: 500,
                color: 'var(--c-text-secondary)',
                lineHeight: '16px',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {artifact.filename}
            </span>
            <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', lineHeight: '14px' }}>
              Document
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexShrink: 0 }}>
          {/* 预览/源码切换：滑动 pill 动画 */}
          {loadState.status === 'text' && (
            <div
              style={{
                position: 'relative',
                display: 'flex',
                columnGap: `${toggleButtonGap}px`,
                padding: '2px',
                borderRadius: '8px',
                background: 'var(--c-bg-deep)',
              }}
            >
              {/* 滑动指示器 */}
              <div
                style={{
                  position: 'absolute',
                  top: '2px',
                  left: '2px',
                  width: `${toggleButtonWidth}px`,
                  height: `${toggleButtonHeight}px`,
                  borderRadius: '6px',
                  background: 'var(--c-bg-page)',
                  border: '0.5px solid var(--c-border-subtle)',
                  transition: 'transform 180ms cubic-bezier(0.16,1,0.3,1)',
                  transform: mode === 'preview' ? 'translateX(0)' : `translateX(${toggleButtonOffset}px)`,
                  pointerEvents: 'none',
                }}
              />
              <button
                onClick={() => setMode('preview')}
                title={t.documentPanel.preview}
                style={{
                  width: `${toggleButtonWidth}px`,
                  height: `${toggleButtonHeight}px`,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderRadius: '6px',
                  border: 'none',
                  background: 'transparent',
                  color: mode === 'preview' ? 'var(--c-text-primary)' : 'var(--c-text-muted)',
                  cursor: 'pointer',
                  position: 'relative',
                  zIndex: 1,
                  transition: 'color 180ms',
                }}
              >
                <Eye size={14} />
              </button>
              <button
                onClick={() => setMode('source')}
                title={t.documentPanel.source}
                style={{
                  width: `${toggleButtonWidth}px`,
                  height: `${toggleButtonHeight}px`,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderRadius: '6px',
                  border: 'none',
                  background: 'transparent',
                  color: mode === 'source' ? 'var(--c-text-primary)' : 'var(--c-text-muted)',
                  cursor: 'pointer',
                  position: 'relative',
                  zIndex: 1,
                  transition: 'color 180ms',
                }}
              >
                <Code size={14} />
              </button>
            </div>
          )}
          <button
            onClick={() => void handleDownload()}
            disabled={downloading}
            title={t.documentPanel.download}
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: `${actionButtonSize}px`,
              height: `${actionButtonSize}px`,
              borderRadius: '8px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: downloading ? 'default' : 'pointer',
              opacity: downloading ? 0.5 : 1,
              transition: 'background 150ms',
            }}
            onMouseEnter={(e) => {
              if (!downloading) (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)'
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
            }}
          >
            <Download size={16} />
          </button>
          <button
            onClick={onClose}
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: `${actionButtonSize}px`,
              height: `${actionButtonSize}px`,
              borderRadius: '8px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 150ms',
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)'
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
            }}
          >
            <X size={18} />
          </button>
        </div>
      </div>

      {/* body */}
      <div style={{ flex: 1, overflowY: 'auto', background: 'var(--c-code-panel-bg)' }}>
        {loadState.status === 'loading' && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '120px',
              color: 'var(--c-text-muted)',
              fontSize: '13px',
            }}
          >
            加载中...
          </div>
        )}

        {loadState.status === 'text' && mode === 'preview' && shouldRenderAsIframe(artifact) && (
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
            <ArtifactIframe
              mode="static"
              content={loadState.content}
              contentType={artifact.mime_type}
              frameTitle={artifact.title ?? artifact.filename}
              style={{ flex: 1, minHeight: '400px', border: 'none', borderRadius: 0 }}
            />
          </div>
        )}

        {loadState.status === 'text' && mode === 'preview' && !shouldRenderAsIframe(artifact) && (
          <div style={{ padding: '20px 28px' }}>
            <MarkdownRenderer content={loadState.content} artifacts={artifacts} accessToken={accessToken} runId={runId} compact />
          </div>
        )}

        {loadState.status === 'text' && mode === 'source' && (
          <pre
            style={{
              margin: 0,
              padding: '20px 28px',
              fontSize: '13px',
              lineHeight: 1.65,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              color: 'var(--c-text-secondary)',
              fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
            }}
          >
            {loadState.content}
          </pre>
        )}

        {loadState.status === 'binary' && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: '12px',
              padding: '40px 24px',
              color: 'var(--c-text-muted)',
              fontSize: '13px',
              textAlign: 'center',
            }}
          >
            <span>{t.documentPanel.previewUnsupported}</span>
            <button
              onClick={() => void handleDownload()}
              disabled={downloading}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '6px',
                padding: '7px 14px',
                borderRadius: '9px',
                border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-sub)',
                color: 'var(--c-text-primary)',
                fontSize: '13px',
                cursor: downloading ? 'default' : 'pointer',
                fontFamily: 'inherit',
                transition: 'background 150ms',
              }}
              onMouseEnter={(e) => {
                if (!downloading) (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)'
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-sub)'
              }}
            >
              <Download size={14} />
              {t.documentPanel.downloadFile}
            </button>
          </div>
        )}

        {loadState.status === 'error' && (
          <div
            style={{
              padding: '40px 24px',
              color: 'var(--c-text-muted)',
              fontSize: '13px',
              textAlign: 'center',
            }}
          >
            {t.documentPanel.loadFailed(loadState.message)}
          </div>
        )}
      </div>
    </div>
  )
}
