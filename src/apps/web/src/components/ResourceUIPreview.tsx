import { useState, useEffect, useRef } from 'react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { McpAppResource } from '../storage'
import { McpAppIframe } from './McpAppIframe'

type Props = {
  resource: McpAppResource
  accessToken?: string
  displayMode?: 'inline' | 'fullscreen'
  onExpand?: () => void
  onSendMessage?: (text: string) => void
}

export function ResourceUIPreview({ resource, accessToken, displayMode = 'inline', onExpand, onSendMessage }: Props) {
  const [content, setContent] = useState<string | undefined>(
    typeof resource.content === 'string' ? resource.content : undefined
  )
  const [error, setError] = useState(false)
  const retriesRef = useRef(0)
  const maxRetries = 10

  useEffect(() => {
    if (typeof resource.content === 'string') return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout>

    const attemptFetch = () => {
      if (cancelled || retriesRef.current >= maxRetries) return

      const url = `${apiBaseUrl()}/v1/artifacts/${resource.key}`
      fetch(url, accessToken ? { headers: { Authorization: `Bearer ${accessToken}` } } : undefined)
        .then(async (res) => {
          if (cancelled) return
          if (!res.ok) throw new Error(`${res.status}`)
          const html = await res.text()
          if (!cancelled) {
            setContent(html)
            setError(false)
          }
        })
        .catch(() => {
          if (cancelled) return
          retriesRef.current++
          timer = setTimeout(attemptFetch, 2000)
        })
    }

    retriesRef.current = 0
    attemptFetch()

    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [resource.key, resource.content, accessToken])

  if (error) {
    return (
      <div style={{
        padding: '16px',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        color: 'var(--c-status-error)',
        fontSize: '13px',
      }}>
        Failed to load MCP app content
      </div>
    )
  }

  if (!content) {
    return null
  }

  return (
    <McpAppIframe
      uri={resource.uri}
      content={content}
      toolOutput={resource.initialData}
      csp={resource.csp}
      serverId={resource.serverId}
      displayMode={displayMode}
      onExpand={onExpand}
      name={resource.serverId}
      accessToken={accessToken}
      onSendMessage={onSendMessage}
      toolName={resource.toolName}
      toolInput={resource.toolInput}
      style={{ minHeight: displayMode === 'fullscreen' ? undefined : '300px' }}
    />
  )
}
