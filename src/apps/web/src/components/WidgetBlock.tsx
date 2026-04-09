import { useCallback, useEffect, useRef, useState } from 'react'
import { ArtifactIframe, type ArtifactAction, type ArtifactIframeHandle } from './ArtifactIframe'
import { CopTimelineHeaderLabel } from './cop-timeline/CopTimelineHeader'
import { noteShowWidgetPhase } from '../streamDebug'

const LOADING_ROTATE_MS = 10000

type Props = {
  html: string
  title: string
  complete: boolean
  loadingMessages?: string[]
  compact?: boolean
  debugMeta?: {
    runId: string
    toolCallId?: string | null
    toolCallIndex: number
  }
  onAction?: (action: ArtifactAction) => void
}

export function WidgetBlock({ html, title, complete, loadingMessages, compact = false, debugMeta, onAction }: Props) {
  const iframeRef = useRef<ArtifactIframeHandle>(null)
  const lastRenderRef = useRef<{ html: string; complete: boolean } | null>(null)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [loadingIdx, setLoadingIdx] = useState(0)

  const showLoadingHeader = !complete && (loadingMessages?.length ?? 0) > 0
  const currentLoadingMessage = showLoadingHeader && loadingMessages
    ? loadingMessages[loadingIdx % loadingMessages.length]
    : ''
  const headerText = showLoadingHeader ? currentLoadingMessage : title

  useEffect(() => {
    const id = requestAnimationFrame(() => setLoadingIdx(0))
    return () => cancelAnimationFrame(id)
  }, [loadingMessages])

  useEffect(() => {
    if (!showLoadingHeader || !loadingMessages?.length) return
    const t = window.setInterval(() => {
      setLoadingIdx((i) => (i + 1) % loadingMessages.length)
    }, LOADING_ROTATE_MS)
    return () => window.clearInterval(t)
  }, [showLoadingHeader, loadingMessages])

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
    if (action.type === 'debug' && debugMeta) {
      noteShowWidgetPhase({
        runId: debugMeta.runId,
        toolCallId: debugMeta.toolCallId,
        toolCallIndex: debugMeta.toolCallIndex,
        title,
        contentLength: html.length,
        phase: action.phase,
      })
    }
    onAction?.(action)
  }, [debugMeta, html.length, onAction, title])

  return (
    <div style={{ margin: compact ? '0 0 2px' : '2px 0 4px', maxWidth: '720px' }}>
      {headerText && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
            padding: '4px 0 2px',
            color: 'var(--c-text-secondary)',
            fontSize: '13px',
            fontWeight: 400,
            marginBottom: compact ? '2px' : '6px',
            lineHeight: '20px',
            maxWidth: '100%',
            minWidth: 0,
          }}
        >
          <CopTimelineHeaderLabel
            key={showLoadingHeader ? `widget-loading-${loadingIdx}-${headerText}` : `widget-title-${headerText}`}
            text={headerText}
            phaseKey={showLoadingHeader ? `widget-loading-${loadingIdx}` : 'widget-title'}
            incremental={showLoadingHeader}
          />
        </div>
      )}
      <ArtifactIframe
        ref={iframeRef}
        mode="streaming"
        frameTitle={title}
        onAction={handleAction}
        compactSpacing={compact}
        style={{
          minHeight: html ? (compact ? '112px' : '120px') : '0px',
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
