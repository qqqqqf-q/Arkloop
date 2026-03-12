import { X } from 'lucide-react'
import type { WebSource } from '../storage'

function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

type Props = {
  sources: WebSource[]
  userQuery?: string
  onClose: () => void
}

export function SourcesPanel({ sources, userQuery, onClose }: Props) {
  return (
    <div
      style={{
        width: '420px',
        background: 'var(--c-bg-page)',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '16px 20px',
          flexShrink: 0,
        }}
      >
        <span style={{ fontSize: '16px', fontWeight: 600, color: 'var(--c-text-primary)' }}>
          {sources.length} sources
        </span>
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
            color: 'var(--c-text-secondary)',
            cursor: 'pointer',
            transition: 'background 150ms',
          }}
          className="hover:bg-[var(--c-bg-deep)]"
        >
          <X size={18} />
        </button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: '0 20px 20px' }}>
        {userQuery && (
          <p
            style={{
              fontSize: '14px',
              color: 'var(--c-text-secondary)',
              lineHeight: 1.6,
              margin: '0 0 20px',
            }}
          >
            Sources for {userQuery}
          </p>
        )}

        <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
          {sources.map((source, idx) => {
            const domain = getDomain(source.url)
            return (
              <a
                key={idx}
                href={source.url}
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  textDecoration: 'none',
                  display: 'flex',
                  gap: '12px',
                  padding: '12px',
                  borderRadius: '10px',
                  transition: 'background 150ms',
                }}
                className="hover:bg-[var(--c-bg-deep)]"
              >
                <div
                  style={{
                    width: '28px',
                    height: '28px',
                    borderRadius: '50%',
                    background: 'var(--c-bg-deep)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    flexShrink: 0,
                    marginTop: '2px',
                    overflow: 'hidden',
                  }}
                >
                  <img
                    src={`https://www.google.com/s2/favicons?domain=${domain}&sz=32`}
                    width={20}
                    height={20}
                    style={{ borderRadius: '50%' }}
                    onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                    alt=""
                  />
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: '13px', color: 'var(--c-text-muted)', marginBottom: '2px' }}>
                    {domain}
                  </div>
                  <div
                    style={{
                      fontSize: '15px',
                      fontWeight: 600,
                      color: 'var(--c-text-primary)',
                      lineHeight: 1.4,
                      marginBottom: '4px',
                    }}
                  >
                    {source.title || domain}
                  </div>
                  {source.snippet && (
                    <div
                      style={{
                        fontSize: '14px',
                        color: 'var(--c-text-secondary)',
                        lineHeight: 1.55,
                        overflow: 'hidden',
                        display: '-webkit-box',
                        WebkitLineClamp: 2,
                        WebkitBoxOrient: 'vertical',
                      }}
                    >
                      {source.snippet}
                    </div>
                  )}
                </div>
              </a>
            )
          })}
        </div>
      </div>
    </div>
  )
}
