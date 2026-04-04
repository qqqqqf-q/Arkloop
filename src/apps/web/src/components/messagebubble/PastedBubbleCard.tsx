import { useState } from 'react'
import { PastedContentModal } from '../PastedContentModal'
import { formatSize } from './utils'

type Props = {
  preview: string
  fullText: string
  size: number
}

export function PastedBubbleCard({ preview, fullText, size }: Props) {
  const [hovered, setHovered] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const lineCount = fullText.split('\n').length

  return (
    <>
      <div
        onClick={() => setModalOpen(true)}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          width: '120px',
          height: '120px',
          borderRadius: '10px',
          background: 'var(--c-bg-input)',
          overflow: 'hidden',
          borderWidth: '0.7px',
          borderStyle: 'solid',
          borderColor: hovered ? 'var(--c-attachment-border-hover)' : 'var(--c-border)',
          boxShadow: '0 1px 3px rgba(242, 247, 250, 0.35)',
          transition: 'border-color 0.2s ease',
          cursor: 'pointer',
          padding: '10px',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{
          color: 'var(--c-text-heading)',
          fontSize: '11px',
          lineHeight: '1.4',
          display: '-webkit-box',
          WebkitLineClamp: 4,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
          wordBreak: 'break-all',
          flex: 1,
        }}>
          {preview}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '4px' }}>
          <span style={{ fontSize: '9px', color: 'var(--c-text-muted)', whiteSpace: 'nowrap' }}>
            {formatSize(size)}
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
      </div>

      {modalOpen && (
        <PastedContentModal
          text={fullText}
          size={size}
          lineCount={lineCount}
          onClose={() => setModalOpen(false)}
        />
      )}
    </>
  )
}
