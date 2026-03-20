import { useCallback, useEffect, useRef, useState } from 'react'
import { ArtifactIframe, type ArtifactAction, type ArtifactIframeHandle } from './ArtifactIframe'

const LOADING_ROTATE_MS = 2200

type Props = {
  html: string
  title: string
  complete: boolean
  loadingMessages?: string[]
  onAction?: (action: ArtifactAction) => void
}

export function WidgetBlock({ html, title, complete, loadingMessages, onAction }: Props) {
  const iframeRef = useRef<ArtifactIframeHandle>(null)
  const lastRenderRef = useRef<{ html: string; complete: boolean } | null>(null)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [loadingIdx, setLoadingIdx] = useState(0)

  const showLoadingStrip = !complete && (loadingMessages?.length ?? 0) > 0

  useEffect(() => {
    const id = requestAnimationFrame(() => setLoadingIdx(0))
    return () => cancelAnimationFrame(id)
  }, [loadingMessages])

  useEffect(() => {
    if (!showLoadingStrip || !loadingMessages?.length) return
    const t = window.setInterval(() => {
      setLoadingIdx((i) => (i + 1) % loadingMessages.length)
    }, LOADING_ROTATE_MS)
    return () => window.clearInterval(t)
  }, [showLoadingStrip, loadingMessages])

  useEffect(() => {
    const previous = lastRenderRef.current
    if (previous?.html === html && previous.complete === complete) return
    if (!html) {
      if (complete) lastRenderRef.current = { html, complete }
      return
    }
    lastRenderRef.current = { html, complete }
    if (complete) {
      iframeRef.current?.finalizeContent(html)
      return
    }
    iframeRef.current?.setStreamingContent(html)
  }, [html, complete])

  useEffect(() => {
    const id = requestAnimationFrame(() => setRuntimeError(null))
    return () => cancelAnimationFrame(id)
  }, [html])

  const handleAction = useCallback((action: ArtifactAction) => {
    if (action.type === 'error') {
      setRuntimeError(action.message)
    }
    onAction?.(action)
  }, [onAction])

  return (
    <div style={{ margin: '2px 0 4px', maxWidth: '720px' }}>
      {showLoadingStrip && loadingMessages && (
        <div
          style={{
            fontSize: '12px',
            fontWeight: 400,
            color: 'var(--c-text-tertiary)',
            marginBottom: '6px',
            minHeight: '1.25em',
          }}
        >
          {loadingMessages[loadingIdx % loadingMessages.length]}
        </div>
      )}
      <ArtifactIframe
        ref={iframeRef}
        mode="streaming"
        frameTitle={title}
        onAction={handleAction}
        style={{
          minHeight: '120px',
          border: 'none',
          borderRadius: '0',
          background: 'transparent',
        }}
      />
      {runtimeError && (
        <div style={{
          marginTop: '6px',
          fontSize: '12px',
          color: 'var(--c-status-error-text)',
        }}>
          {runtimeError}
        </div>
      )}
    </div>
  )
}
