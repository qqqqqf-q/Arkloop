import { memo } from 'react'
import { Globe, Loader2 } from 'lucide-react'
import type { WebFetchRef } from '../../storage'
import { getDomain, isHttpUrl, getShortName, getUrlScheme } from './utils'
import { SourceFavicon } from './SourceList'

export const WebFetchItem = memo(function WebFetchItem({ fetch: f, live: _live }: { fetch: WebFetchRef; live?: boolean }) {
  const isFetching = f.status === 'fetching'
  const isHttp = isHttpUrl(f.url)
  const isFailed = f.status === 'failed'
  const domain = isHttp ? getDomain(f.url) : ''
  const scheme = getUrlScheme(f.url)
  const shortName = isHttp ? getShortName(f.url) : (scheme || 'invalid')
  const primaryText = f.title || (isHttp ? domain : (f.url || 'Invalid URL'))
  const secondaryText = typeof f.statusCode === 'number'
    ? `${f.statusCode}`
    : isFailed && f.errorMessage
      ? f.errorMessage
      : shortName
  const content = (
    <>
      <div
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: '20px',
          height: '20px',
          borderRadius: '5px',
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-menu)',
          flexShrink: 0,
        }}
      >
        {isFetching ? (
          <Loader2 size={11} className="animate-spin" style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
        ) : (
          isHttp ? (
            <SourceFavicon domain={domain} isFailed={isFailed} />
          ) : (
            <Globe
              size={11}
              style={{
                color: isFailed ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-muted)',
                flexShrink: 0,
              }}
            />
          )
        )}
      </div>
      <span
        style={{
          fontSize: '12px',
          color: 'var(--c-text-secondary)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          flex: 1,
          minWidth: 0,
        }}
      >
        {primaryText}
      </span>
      <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', flexShrink: 0 }}>
        {secondaryText}
      </span>
    </>
  )

  if (!isFetching && isHttp) {
    return (
      <a
        href={f.url}
        target="_blank"
        rel="noopener noreferrer"
        title={f.url}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '7px',
          padding: '3px 0',
          textDecoration: 'none',
          cursor: 'pointer',
        }}
      >
        {content}
      </a>
    )
  }

  return (
    <div
      title={f.url}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '7px',
        padding: '3px 0',
        cursor: isFetching ? 'default' : 'not-allowed',
      }}
    >
      {content}
    </div>
  )
})
