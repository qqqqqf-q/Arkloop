import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Download, ExternalLink, FileCode2, X } from 'lucide-react'

const ANIM_MS = 120

function apiBaseUrl(): string {
  const raw = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
  return raw.replace(/\/$/, '')
}

export type WorkspaceFileRef = {
  path: string
  filename: string
  mime_type?: string
}

type Props = {
  file: WorkspaceFileRef
  runId?: string
  accessToken: string
}

type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'binary'; blobUrl: string; mimeType: string }
  | { status: 'text'; content: string; mimeType: string }

function normalizeWorkspacePath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed) return '/'
  let normalized = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  if (normalized === '/workspace') return '/'
  if (normalized.startsWith('/workspace/')) {
    normalized = normalized.slice('/workspace'.length)
  }
  return normalized
}

function buildWorkspaceUrl(runId: string, path: string): string {
  const sp = new URLSearchParams({ run_id: runId, path: normalizeWorkspacePath(path) })
  return `${apiBaseUrl()}/v1/workspace-files?${sp.toString()}`
}

const EXT_MIME: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif',
  svg: 'image/svg+xml', webp: 'image/webp', html: 'text/html', htm: 'text/html',
  md: 'text/markdown', txt: 'text/plain', json: 'application/json', csv: 'text/csv',
  log: 'text/plain', py: 'text/x-python', ts: 'text/typescript', tsx: 'text/typescript',
  js: 'text/javascript', jsx: 'text/javascript', sh: 'text/x-shellscript', go: 'text/plain',
  yml: 'text/yaml', yaml: 'text/yaml', xml: 'application/xml', sql: 'text/plain',
}

function guessMimeType(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  return EXT_MIME[ext] ?? 'application/octet-stream'
}

function normalizeMimeType(value: string | null | undefined, path: string): string {
  const raw = (value ?? '').split(';', 1)[0].trim().toLowerCase()
  return raw || guessMimeType(path)
}

function isTextMime(mimeType: string): boolean {
  if (mimeType.startsWith('text/')) return true
  return mimeType === 'application/json' || mimeType === 'application/xml'
}

function workspaceKind(mimeType: string): 'image' | 'html' | 'text' | 'binary' {
  if (mimeType.startsWith('image/')) return 'image'
  if (mimeType === 'text/html') return 'html'
  if (isTextMime(mimeType)) return 'text'
  return 'binary'
}

function fileExtension(filename: string): string {
  const ext = filename.split('.').pop()?.trim().toLowerCase()
  return ext || 'file'
}

export function WorkspaceResource({ file, runId, accessToken }: Props) {
  const [loadState, setLoadState] = useState<LoadState>({ status: 'loading' })
  const [visible, setVisible] = useState(false)
  const [show, setShow] = useState(false)
  const closingTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const iframeRef = useRef<HTMLIFrameElement>(null)

  const normalizedPath = useMemo(() => normalizeWorkspacePath(file.path), [file.path])
  const expectedKind = useMemo(() => workspaceKind(normalizeMimeType(file.mime_type, file.filename)), [file.filename, file.mime_type])

  useEffect(() => {
    if (!runId || !accessToken) {
      setLoadState({ status: 'error' })
      return
    }

    let cancelled = false
    let localBlobUrl: string | null = null
    const url = buildWorkspaceUrl(runId, normalizedPath)

    setLoadState({ status: 'loading' })
    fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        const mimeType = normalizeMimeType(res.headers.get('content-type') ?? file.mime_type, file.filename)
        if (workspaceKind(mimeType) === 'text') {
          const content = await res.text()
          if (cancelled) return
          setLoadState({ status: 'text', content, mimeType })
          return
        }

        const blob = await res.blob()
        if (cancelled) return
        localBlobUrl = URL.createObjectURL(blob)
        setLoadState({ status: 'binary', blobUrl: localBlobUrl, mimeType })
      })
      .catch(() => {
        if (cancelled) return
        setLoadState({ status: 'error' })
      })

    return () => {
      cancelled = true
      if (localBlobUrl) URL.revokeObjectURL(localBlobUrl)
    }
  }, [accessToken, file.filename, file.mime_type, normalizedPath, runId])

  useEffect(() => () => {
    if (closingTimer.current) clearTimeout(closingTimer.current)
  }, [])

  useEffect(() => {
    const handler = (event: MessageEvent) => {
      const iframe = iframeRef.current
      if (!iframe) return
      if (event.source !== iframe.contentWindow) return
      if (event.data?.type !== 'arkloop-iframe-resize') return
      if (typeof event.data.height !== 'number') return
      iframe.style.height = `${event.data.height}px`
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [])

  const openLightbox = useCallback(() => {
    if (loadState.status !== 'binary' || workspaceKind(loadState.mimeType) !== 'image') return
    if (closingTimer.current) clearTimeout(closingTimer.current)
    setVisible(true)
    requestAnimationFrame(() => requestAnimationFrame(() => setShow(true)))
  }, [loadState])

  const closeLightbox = useCallback(() => {
    setShow(false)
    closingTimer.current = setTimeout(() => setVisible(false), ANIM_MS)
  }, [])

  const downloadCurrentFile = useCallback(() => {
    if (loadState.status !== 'binary') return
    const anchor = document.createElement('a')
    anchor.href = loadState.blobUrl
    anchor.download = file.filename
    anchor.click()
  }, [file.filename, loadState])

  if (loadState.status === 'error') {
    return (
      <span style={{ color: 'var(--c-text-tertiary)', fontSize: '13px' }}>
        {file.filename}
      </span>
    )
  }

  if (loadState.status === 'loading') {
    return (
      <div
        data-workspace-kind="loading"
        data-workspace-preview={expectedKind}
        style={{
          width: '100%',
          minHeight: '84px',
          borderRadius: '10px',
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-sub)',
          padding: '12px',
          display: 'flex',
          flexDirection: 'column',
          gap: '8px',
        }}
      >
        <span style={{ fontSize: '12px', color: 'var(--c-text-secondary)' }}>{file.filename}</span>
        <div style={{ height: '36px', borderRadius: '8px', background: 'var(--c-bg-deep)' }} />
      </div>
    )
  }

  if (loadState.status === 'text') {
    return (
      <div
        data-workspace-kind="text"
        style={{
          margin: '8px 0',
          borderRadius: '12px',
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-sub)',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: '8px',
            padding: '10px 12px',
            borderBottom: '0.5px solid var(--c-border-subtle)',
            background: 'var(--c-bg-deep)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', minWidth: 0 }}>
            <FileCode2 size={14} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
            <span style={{ fontSize: '13px', color: 'var(--c-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {file.filename}
            </span>
          </div>
          <span style={{ fontSize: '11px', color: 'var(--c-text-tertiary)', textTransform: 'uppercase' }}>
            {fileExtension(file.filename)}
          </span>
        </div>
        <pre
          style={{
            margin: 0,
            padding: '12px',
            maxHeight: '320px',
            overflow: 'auto',
            fontSize: '12px',
            lineHeight: 1.6,
            color: 'var(--c-text-primary)',
            background: 'var(--c-bg-sub)',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          <code>{loadState.content}</code>
        </pre>
      </div>
    )
  }

  const kind = workspaceKind(loadState.mimeType)
  if (kind === 'html') {
    return (
      <iframe
        ref={iframeRef}
        data-workspace-kind="html"
        src={loadState.blobUrl}
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

  if (kind === 'image') {
    const transition = `all ${ANIM_MS}ms ease-out`
    return (
      <>
        <div
          data-workspace-kind="image"
          style={{ display: 'inline-block', border: '0.5px solid var(--c-border-subtle)', borderRadius: '12px', padding: '8px' }}
        >
          <img
            src={loadState.blobUrl}
            alt={file.filename}
            draggable={false}
            onClick={openLightbox}
            style={{ maxWidth: '100%', display: 'block', borderRadius: '6px', cursor: 'default' }}
          />
        </div>
        {visible && (
          <div
            onClick={(event) => {
              if (event.target === event.currentTarget) closeLightbox()
            }}
            style={{
              position: 'fixed',
              inset: 0,
              zIndex: 9999,
              background: show ? 'rgba(0,0,0,0.45)' : 'rgba(0,0,0,0)',
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
              src={loadState.blobUrl}
              alt={file.filename}
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
              onClick={(event) => event.stopPropagation()}
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
                href={loadState.blobUrl}
                target="_blank"
                rel="noreferrer"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  borderRadius: 999,
                  border: '0.5px solid rgba(255,255,255,0.18)',
                  background: 'rgba(17, 24, 39, 0.82)',
                  padding: '8px 12px',
                  color: 'rgba(255,255,255,0.92)',
                  textDecoration: 'none',
                  backdropFilter: 'blur(10px)',
                  WebkitBackdropFilter: 'blur(10px)',
                }}
              >
                <span style={{ maxWidth: '50vw', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 13 }}>
                  {file.filename}
                </span>
                <ExternalLink size={14} />
              </a>
              <button
                onClick={downloadCurrentFile}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  borderRadius: 999,
                  border: '0.5px solid rgba(255,255,255,0.18)',
                  background: 'rgba(17, 24, 39, 0.82)',
                  padding: '8px 12px',
                  color: 'rgba(255,255,255,0.92)',
                  cursor: 'pointer',
                  backdropFilter: 'blur(10px)',
                  WebkitBackdropFilter: 'blur(10px)',
                }}
              >
                <Download size={14} />
                <span style={{ fontSize: 13 }}>下载</span>
              </button>
            </div>
          </div>
        )}
      </>
    )
  }

  return (
    <button
      data-workspace-kind="binary"
      onClick={downloadCurrentFile}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '6px 10px',
        borderRadius: '9px',
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-sub)',
        cursor: 'pointer',
        fontFamily: 'inherit',
        transition: 'background 150ms',
        maxWidth: '100%',
        verticalAlign: 'middle',
        margin: '2px 4px',
        lineHeight: 1,
      }}
    >
      <Download size={14} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
      <span style={{ fontSize: '13px', color: 'var(--c-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', lineHeight: '16px' }}>
        {file.filename}
      </span>
    </button>
  )
}
