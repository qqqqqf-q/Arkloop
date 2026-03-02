import { useState, useRef, useEffect, createContext, useContext } from 'react'
import type { WebSource } from '../storage'

export const WebSourcesContext = createContext<WebSource[]>([])

// 用于 favicon 请求（需要完整域名）
function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

// 用于展示：去掉 TLD，如 facebook.com→facebook，docs.github.com→docs.github
function formatDomain(url: string): string {
  try {
    const hostname = new URL(url).hostname.replace(/^www\./, '')
    const parts = hostname.split('.')
    return parts.length > 1 ? parts.slice(0, -1).join('.') : hostname
  } catch {
    return url
  }
}

type PopoverStyle = {
  position: 'fixed'
  left: string
  top?: string
  bottom?: string
  width: string
  zIndex: number
}

type Props = {
  indices: number[]
}

export function CitationBadge({ indices }: Props) {
  const webSources = useContext(WebSourcesContext)
  const [open, setOpen] = useState(false)
  const [page, setPage] = useState(0)
  const [popoverStyle, setPopoverStyle] = useState<PopoverStyle | null>(null)
  const badgeRef = useRef<HTMLButtonElement>(null)
  const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const validSources = indices
    .map((i) => {
      // 优先 1-based 查找；若越界则回退到 0-based（兼容 LLM 偶尔输出 web:0 的情况）
      const src = webSources[i - 1]
      if (src != null && src.url) return src
      return webSources[i]
    })
    .filter((s): s is WebSource => s != null && !!s.url)

  const openPopover = () => {
    if (closeTimerRef.current) clearTimeout(closeTimerRef.current)
    if (!badgeRef.current) return
    const rect = badgeRef.current.getBoundingClientRect()
    const popoverWidth = 320
    let left = rect.left
    if (left + popoverWidth > window.innerWidth - 8) {
      left = window.innerWidth - popoverWidth - 8
    }
    left = Math.max(8, left)
    const spaceBelow = window.innerHeight - rect.bottom
    const style: PopoverStyle = {
      position: 'fixed',
      left: `${left}px`,
      width: `${popoverWidth}px`,
      zIndex: 1000,
    }
    if (spaceBelow >= 220 || rect.top < 220) {
      style.top = `${rect.bottom + 6}px`
    } else {
      style.bottom = `${window.innerHeight - rect.top + 6}px`
    }
    setPopoverStyle(style)
    setOpen(true)
  }

  const scheduleClose = () => {
    closeTimerRef.current = setTimeout(() => setOpen(false), 120)
  }

  const cancelClose = () => {
    if (closeTimerRef.current) clearTimeout(closeTimerRef.current)
  }

  useEffect(() => () => { if (closeTimerRef.current) clearTimeout(closeTimerRef.current) }, [])

  if (validSources.length === 0) {
    return (
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          padding: '1px 5px',
          borderRadius: '4px',
          background: 'var(--c-bg-deep)',
          fontSize: '11px',
          color: 'var(--c-text-muted)',
          verticalAlign: 'middle',
          margin: '0 2px',
          lineHeight: '1.4',
        }}
      >
        {indices.map((i) => `web:${i}`).join(' ')}
      </span>
    )
	}

	const firstSource = validSources[0]
	const displayName = formatDomain(firstSource.url)
	const extraCount = validSources.length - 1
	const currentSource = validSources[page]
	const currentDomain = getDomain(currentSource.url)
	const multiSource = validSources.length > 1

  return (
    <span style={{ position: 'relative', display: 'inline-block' }}>
      <button
        ref={badgeRef}
        onMouseEnter={openPopover}
        onMouseLeave={scheduleClose}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '3px',
          padding: '1px 6px',
          borderRadius: '4px',
          background: open ? 'var(--c-bg-sub)' : 'var(--c-bg-deep)',
          border: 'none',
          boxShadow: 'none',
          fontSize: '11.5px',
          color: 'var(--c-text-secondary)',
          cursor: 'default',
          verticalAlign: 'middle',
          margin: '0 2px',
          lineHeight: '1.5',
          fontFamily: 'inherit',
          transition: 'background 120ms',
        }}
      >
        {displayName}
        {extraCount > 0 && (
          <span
            style={{
              fontSize: '10px',
              opacity: 0.6,
            }}
          >
            +{extraCount}
          </span>
        )}
      </button>

      {open && popoverStyle && (
        <div
          style={{
            ...popoverStyle,
            background: 'var(--c-bg-page)',
            border: '0.5px solid var(--c-border-mid)',
            borderRadius: '12px',
            boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
            padding: '14px',
            overflow: 'hidden',
            animation: 'citationPopoverIn 150ms ease-out',
          }}
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
        >
          <style>{`
            @keyframes citationPopoverIn {
              from { opacity: 0; transform: translateY(-4px) scale(0.97); }
              to { opacity: 1; transform: translateY(0) scale(1); }
            }
          `}</style>

          {/* pagination header - 只在多个 source 时显示 */}
          {multiSource && (
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '10px',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                <button
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                  disabled={page === 0}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: '24px',
                    height: '24px',
                    background: 'none',
                    border: 'none',
                    borderRadius: '6px',
                    cursor: page === 0 ? 'default' : 'pointer',
                    color: 'var(--c-text-muted)',
                    opacity: page === 0 ? 0.3 : 0.8,
                    fontSize: '16px',
                    lineHeight: 1,
                    transition: 'background 120ms, opacity 120ms',
                  }}
                  onMouseEnter={(e) => { if (page > 0) { e.currentTarget.style.background = 'var(--c-bg-deep)'; e.currentTarget.style.opacity = '1' } }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = 'none'; if (page > 0) e.currentTarget.style.opacity = '0.8' }}
                >
                  &#8249;
                </button>
                <span style={{ fontSize: '12px', color: 'var(--c-text-secondary)', minWidth: '36px', textAlign: 'center' }}>
                  {page + 1} / {validSources.length}
                </span>
                <button
                  onClick={() => setPage((p) => Math.min(validSources.length - 1, p + 1))}
                  disabled={page === validSources.length - 1}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: '24px',
                    height: '24px',
                    background: 'none',
                    border: 'none',
                    borderRadius: '6px',
                    cursor: page === validSources.length - 1 ? 'default' : 'pointer',
                    color: 'var(--c-text-muted)',
                    opacity: page === validSources.length - 1 ? 0.3 : 0.8,
                    fontSize: '16px',
                    lineHeight: 1,
                    transition: 'background 120ms, opacity 120ms',
                  }}
                  onMouseEnter={(e) => { if (page < validSources.length - 1) { e.currentTarget.style.background = 'var(--c-bg-deep)'; e.currentTarget.style.opacity = '1' } }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = 'none'; if (page < validSources.length - 1) e.currentTarget.style.opacity = '0.8' }}
                >
                  &#8250;
                </button>
              </div>
              <span style={{ fontSize: '12px', color: 'var(--c-text-muted)' }}>
                {validSources.length} sources
              </span>
            </div>
          )}

          {/* source card */}
          <a
            href={currentSource.url}
            target="_blank"
            rel="noopener noreferrer"
            style={{ textDecoration: 'none', display: 'block' }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '5px' }}>
              <img
                src={`https://www.google.com/s2/favicons?domain=${currentDomain}&sz=16`}
                width={14}
                height={14}
                style={{ borderRadius: '2px', flexShrink: 0 }}
                onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                alt=""
              />
              <span style={{ fontSize: '12px', color: 'var(--c-text-muted)' }}>{currentDomain}</span>
            </div>
            <div
              style={{
                fontSize: '14px',
                fontWeight: 600,
                color: 'var(--c-text-primary)',
                marginBottom: '5px',
                lineHeight: 1.4,
                overflow: 'hidden',
                display: '-webkit-box',
                WebkitLineClamp: 2,
                WebkitBoxOrient: 'vertical',
              }}
            >
              {currentSource.title || currentDomain}
            </div>
            {currentSource.snippet && (
              <div
                style={{
                  fontSize: '13px',
                  color: 'var(--c-text-secondary)',
                  lineHeight: 1.55,
                  overflow: 'hidden',
                  display: '-webkit-box',
                  WebkitLineClamp: 3,
                  WebkitBoxOrient: 'vertical',
                }}
              >
                {currentSource.snippet}
              </div>
            )}
          </a>
        </div>
      )}
    </span>
  )
}
