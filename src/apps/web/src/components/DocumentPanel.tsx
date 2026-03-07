import { useState, useEffect, useCallback } from 'react'
import { X, FileText, Download, Eye, Code } from 'lucide-react'
import type { ArtifactRef } from '../storage'
import { MarkdownRenderer } from './MarkdownRenderer'

function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

function isTextMime(mime: string): boolean {
  return mime.startsWith('text/')
}

type ViewMode = 'preview' | 'source'

type Props = {
  artifact: ArtifactRef
  accessToken: string
  onClose: () => void
}

type LoadState =
  | { status: 'loading' }
  | { status: 'text'; content: string }
  | { status: 'binary' }
  | { status: 'error'; message: string }

export function DocumentPanel({ artifact, accessToken, onClose }: Props) {
  const [loadState, setLoadState] = useState<LoadState>({ status: 'loading' })
  const [downloading, setDownloading] = useState(false)
  const [mode, setMode] = useState<ViewMode>('preview')

  useEffect(() => {
    setLoadState({ status: 'loading' })
    const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        // 以服务器返回的 Content-Type 为准，artifact.mime_type 仅作兜底
        const serverMime = res.headers.get('content-type') ?? artifact.mime_type ?? ''
        if (isTextMime(serverMime)) {
          const text = await res.text()
          setLoadState({ status: 'text', content: text })
        } else {
          setLoadState({ status: 'binary' })
        }
      })
      .catch((err: unknown) => {
        setLoadState({ status: 'error', message: err instanceof Error ? err.message : '加载失败' })
      })
  }, [artifact.key, artifact.mime_type, accessToken])

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
    <div style={{ width: '540px', display: 'flex', flexDirection: 'column', height: '100%' }}>
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
                  width: '26px',
                  height: '26px',
                  borderRadius: '6px',
                  background: 'var(--c-bg-page)',
                  border: '0.5px solid var(--c-border-subtle)',
                  transition: 'transform 180ms cubic-bezier(0.16,1,0.3,1)',
                  transform: mode === 'preview' ? 'translateX(0)' : 'translateX(28px)',
                  pointerEvents: 'none',
                }}
              />
              <button
                onClick={() => setMode('preview')}
                title="预览"
                style={{
                  width: '26px',
                  height: '26px',
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
                <Eye size={13} />
              </button>
              <button
                onClick={() => setMode('source')}
                title="源码"
                style={{
                  width: '26px',
                  height: '26px',
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
                <Code size={13} />
              </button>
            </div>
          )}
          <button
            onClick={() => void handleDownload()}
            disabled={downloading}
            title="下载"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: '28px',
              height: '28px',
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
              width: '28px',
              height: '28px',
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

        {loadState.status === 'text' && mode === 'preview' && (
          <div style={{ padding: '20px 28px' }}>
            <MarkdownRenderer content={loadState.content} />
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
            <span>该格式暂不支持预览</span>
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
              下载文件
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
            加载失败：{loadState.message}
          </div>
        )}
      </div>
    </div>
  )
}
