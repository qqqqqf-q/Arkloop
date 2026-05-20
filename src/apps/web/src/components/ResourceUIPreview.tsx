import { useState, useEffect } from 'react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { McpAppResource } from '../storage'
import { McpAppIframe } from './McpAppIframe'

type Props = {
  resource: McpAppResource
  accessToken?: string
}

export function ResourceUIPreview({ resource, accessToken }: Props) {
  const [content, setContent] = useState<string | undefined>(
    typeof resource.initialData === 'string' ? resource.initialData : undefined
  )
  const [error, setError] = useState(false)

  useEffect(() => {
    // 已有 inline content，无需 fetch
    if (typeof resource.initialData === 'string') return
    if (!accessToken) return
    let cancelled = false

    const url = `${apiBaseUrl()}/v1/artifacts/${resource.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        const html = await res.text()
        if (!cancelled) setContent(html)
      })
      .catch(() => {
        if (!cancelled) setError(true)
      })

    return () => { cancelled = true }
  }, [resource.key, resource.initialData, accessToken])

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
      style={{ minHeight: '300px' }}
    />
  )
}
