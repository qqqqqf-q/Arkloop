import { FileText } from 'lucide-react'
import type { ArtifactRef } from '../storage'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  artifact: ArtifactRef
  onClick: (trigger: HTMLButtonElement) => void
  active?: boolean
}

type DocumentResourceCardProps = {
  title: string
  kindLabel?: string
  onClick: (trigger: HTMLButtonElement) => void
  active?: boolean
}

export function DocumentResourceCard({ title, kindLabel = 'Document', onClick, active }: DocumentResourceCardProps) {
  const { locale } = useLocale()
  const restingBackground = active ? 'var(--c-bg-page)' : 'var(--c-bg-sub)'
  const iconBackground = active ? 'transparent' : 'var(--c-bg-page)'
  const iconBorder = active ? '0.5px solid transparent' : '0.5px solid var(--c-border-subtle)'
  const ring = active ? 'inset 0 0 0 1px var(--c-border-subtle)' : 'none'

  return (
    <button
      type="button"
      onClick={(event) => onClick(event.currentTarget)}
      aria-pressed={active}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '12px',
        padding: '10px 14px',
        borderRadius: '12px',
        border: '0.5px solid var(--c-border-subtle)',
        background: restingBackground,
        cursor: 'pointer',
        fontFamily: 'inherit',
        transition: 'background 150ms, box-shadow 150ms, border-color 150ms',
        maxWidth: '320px',
        textAlign: 'left',
        boxShadow: ring,
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.background = 'var(--c-bg-deep)'
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.background = restingBackground
      }}
    >
      <div
        style={{
          width: '36px',
          height: '36px',
          borderRadius: '8px',
          background: iconBackground,
          border: iconBorder,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          flexShrink: 0,
          transition: 'background 150ms, border-color 150ms',
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
          {title}
        </span>
        <span
          style={{
            fontSize: '11px',
            color: 'var(--c-text-muted)',
            lineHeight: '14px',
          }}
        >
          {kindLabel === 'Document' && locale === 'zh' ? '文档' : kindLabel}
        </span>
      </div>
    </button>
  )
}

export function DocumentCard({ artifact, onClick, active }: Props) {
  return (
    <DocumentResourceCard
      title={artifact.title || artifact.filename}
      onClick={onClick}
      active={active}
    />
  )
}
