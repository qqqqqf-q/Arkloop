import { useRef, useEffect } from 'react'
import { ArtifactIframe, type ArtifactIframeHandle, type ArtifactAction } from './ArtifactIframe'
import type { ArtifactRef } from '../storage'

export type StreamingArtifactEntry = {
  toolCallIndex: number
  toolCallId?: string
  toolName?: string
  argumentsBuffer: string
  title?: string
  filename?: string
  display?: 'inline' | 'panel'
  content?: string
  complete: boolean
  artifactRef?: ArtifactRef
}

type Props = {
  entry: StreamingArtifactEntry
  accessToken?: string
  onAction?: (action: ArtifactAction) => void
}

export function extractPartialArtifactFields(buffer: string): {
  title?: string
  filename?: string
  display?: string
  content?: string
} {
  const result: ReturnType<typeof extractPartialArtifactFields> = {}

  const titleMatch = buffer.match(/"title"\s*:\s*"([^"]*)"/)
  if (titleMatch) result.title = titleMatch[1]

  const filenameMatch = buffer.match(/"filename"\s*:\s*"([^"]*)"/)
  if (filenameMatch) result.filename = filenameMatch[1]

  const displayMatch = buffer.match(/"display"\s*:\s*"([^"]*)"/)
  if (displayMatch) result.display = displayMatch[1]

  const contentMarker = '"content":"'
  const contentIdx = buffer.indexOf(contentMarker)
  if (contentIdx !== -1) {
    let raw = buffer.slice(contentIdx + contentMarker.length)
    if (raw.endsWith('"}')) raw = raw.slice(0, -2)
    else if (raw.endsWith('"')) raw = raw.slice(0, -1)
    result.content = unescapeJsonString(raw)
  }
  return result
}

function unescapeJsonString(s: string): string {
  return s
    .replace(/\\n/g, '\n')
    .replace(/\\r/g, '\r')
    .replace(/\\t/g, '\t')
    .replace(/\\"/g, '"')
    .replace(/\\\\/g, '\\')
    .replace(/\\u([0-9a-fA-F]{4})/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)))
}

export function ArtifactStreamBlock({ entry, accessToken, onAction }: Props) {
  const iframeRef = useRef<ArtifactIframeHandle>(null)
  const lastContentRef = useRef<string>('')

  useEffect(() => {
    if (!entry.content || entry.content === lastContentRef.current) return
    lastContentRef.current = entry.content
    if (entry.complete) {
      iframeRef.current?.finalizeContent(entry.content)
    } else {
      iframeRef.current?.setStreamingContent(entry.content)
    }
  }, [entry.content, entry.complete])

  // display=panel artifacts are not rendered inline during streaming;
  // they just show as a compact card
  if (entry.display === 'panel' && !entry.content) {
    return null
  }

  const isInline = entry.display !== 'panel'
  const title = entry.title || entry.filename || 'Artifact'

  if (entry.artifactRef && !isInline) {
    return null
  }

  // already have static artifact? render static iframe
  if (entry.artifactRef && isInline) {
    return (
      <div style={{ margin: '8px 0', maxWidth: '720px' }}>
        <div style={{
          fontSize: '12px',
          fontWeight: 500,
          color: 'var(--c-text-secondary)',
          marginBottom: '6px',
        }}>
          {title}
        </div>
        <ArtifactIframe
          mode="static"
          artifact={entry.artifactRef}
          accessToken={accessToken}
          onAction={onAction}
          style={{ minHeight: '300px' }}
        />
      </div>
    )
  }

  // streaming mode
  return (
    <div style={{ margin: '8px 0', maxWidth: '720px' }}>
      <div style={{
        fontSize: '12px',
        fontWeight: 500,
        color: 'var(--c-text-secondary)',
        marginBottom: '6px',
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
      }}>
        {title}
        {!entry.complete && (
          <span style={{
            display: 'inline-block',
            width: '6px',
            height: '6px',
            borderRadius: '50%',
            background: 'var(--c-text-tertiary)',
            animation: '_fadeIn 0.6s ease infinite alternate',
          }} />
        )}
      </div>
      <ArtifactIframe
        ref={iframeRef}
        mode="streaming"
        onAction={onAction}
        style={{ minHeight: '200px' }}
      />
    </div>
  )
}
