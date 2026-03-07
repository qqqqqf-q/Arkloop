import { FileText } from 'lucide-react'
import type { ArtifactRef } from '../storage'

type Props = {
  artifact: ArtifactRef
  onClick: () => void
  active?: boolean
}

export function DocumentCard({ artifact, onClick, active }: Props) {
  return (
    <button
      onClick={onClick}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '12px',
        padding: '10px 14px',
        borderRadius: '12px',
        border: active
          ? '0.5px solid var(--c-border-mid)'
          : '0.5px solid var(--c-border-subtle)',
        background: active ? 'var(--c-bg-deep)' : 'var(--c-bg-sub)',
        cursor: 'pointer',
        fontFamily: 'inherit',
        transition: 'background 150ms, border-color 150ms',
        maxWidth: '320px',
        textAlign: 'left',
      }}
      onMouseEnter={(e) => {
        if (!active) (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)'
      }}
      onMouseLeave={(e) => {
        if (!active) (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-sub)'
      }}
    >
      <div
        style={{
          width: '36px',
          height: '36px',
          borderRadius: '8px',
          background: 'var(--c-bg-page)',
          border: '0.5px solid var(--c-border-subtle)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          flexShrink: 0,
        }}
      >
        <FileText size={18} style={{ color: 'var(--c-text-icon)' }} />
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '2px', minWidth: 0 }}>
        <span
          style={{
            fontSize: '13px',
            fontWeight: 500,
            color: 'var(--c-text-primary)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            lineHeight: '16px',
          }}
        >
          {artifact.filename}
        </span>
        <span
          style={{
            fontSize: '11px',
            color: 'var(--c-text-muted)',
            lineHeight: '14px',
          }}
        >
          Document
        </span>
      </div>
    </button>
  )
}
