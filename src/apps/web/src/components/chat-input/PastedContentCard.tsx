import { useState } from 'react'
import { X } from 'lucide-react'
import type { Attachment } from '../ChatInput'

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function PastedContentCard({
  attachment,
  onRemove,
  onClick,
}: {
  attachment: Attachment
  onRemove: () => void
  onClick: () => void
}) {
  const [cardHovered, setCardHovered] = useState(false)
  const uploading = attachment.status === 'uploading'
  const text = attachment.pasted?.text ?? ''

  return (
    <div style={{ position: 'relative', flexShrink: 0 }}
      onMouseEnter={() => setCardHovered(true)}
      onMouseLeave={() => setCardHovered(false)}
    >
      <div
        onClick={onClick}
        style={{
          width: '120px',
          height: '120px',
          borderRadius: '10px',
          background: 'var(--c-bg-input)',
          overflow: 'hidden',
          borderWidth: '0.7px',
          borderStyle: 'solid',
          borderColor: cardHovered ? 'var(--c-attachment-border-hover)' : 'var(--c-input-border-color)',
          transition: 'border-color 0.2s ease, background 0.2s ease',
          cursor: 'pointer',
          display: 'flex',
          flexDirection: 'column',
          padding: '10px',
        }}
      >
        {uploading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', flex: 1 }}>
            <div className="attachment-shimmer" style={{ width: '90%', height: '10px', borderRadius: '5px' }} />
            <div className="attachment-shimmer" style={{ width: '70%', height: '10px', borderRadius: '5px' }} />
            <div className="attachment-shimmer" style={{ width: '50%', height: '10px', borderRadius: '5px' }} />
          </div>
        ) : (
          <>
            <div style={{
              flex: 1,
              overflow: 'hidden',
              color: 'var(--c-text-muted)',
              fontSize: '10px',
              lineHeight: '1.35',
              fontWeight: 300,
              display: '-webkit-box',
              WebkitLineClamp: 4,
              WebkitBoxOrient: 'vertical',
              wordBreak: 'break-all',
            }}>
              {text}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '4px' }}>
              <span style={{
                fontSize: '9px',
                color: 'var(--c-text-muted)',
                whiteSpace: 'nowrap',
              }}>
                {formatSize(attachment.size)}
              </span>
              <span style={{
                padding: '1px 6px',
                borderRadius: '5px',
                background: 'var(--c-attachment-bg)',
                border: '0.5px solid var(--c-attachment-badge-border)',
                color: 'var(--c-text-secondary)',
                fontSize: '10px',
                fontWeight: 500,
              }}>
                PASTED
              </span>
            </div>
          </>
        )}
      </div>

      <button
        type="button"
        className="attachment-close-btn"
        onClick={(e) => { e.stopPropagation(); onRemove() }}
        style={{
          position: 'absolute',
          top: '-5px',
          left: '-5px',
          width: '18px',
          height: '18px',
          borderRadius: '50%',
          background: 'var(--c-bg-input)',
          border: '0.5px solid var(--c-attachment-close-border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          cursor: 'pointer',
          opacity: cardHovered ? 1 : 0,
          transition: 'opacity 0.15s ease',
          pointerEvents: cardHovered ? 'auto' : 'none',
          zIndex: 1,
        }}
      >
        <X size={9} />
      </button>
    </div>
  )
}
