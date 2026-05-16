import { ClipboardList, FileText } from 'lucide-react'
import type { ArtifactRef } from '../storage'
import { useLocale } from '../contexts/LocaleContext'
import { isPlanMarkdownPath } from '../planMetadata'

type Props = {
  artifact: ArtifactRef
  onClick: (trigger: HTMLButtonElement) => void
  active?: boolean
}

type DocumentResourceCardProps = {
  title: string
  kindLabel?: string
  isPlan?: boolean
  onClick: (trigger: HTMLButtonElement) => void
  active?: boolean
}

export function DocumentResourceCard({ title, kindLabel = 'Document', isPlan = false, onClick, active }: DocumentResourceCardProps) {
  const { locale } = useLocale()
  const restingBackground = active ? 'var(--c-bg-menu)' : 'var(--c-bg-menu)'
  const iconBackground = active ? 'var(--c-bg-input)' : 'var(--c-bg-input)'
  const iconBorder = '0.5px solid var(--c-border-subtle)'
  const ring = active ? 'inset 0 0 0 1px var(--c-border-subtle)' : 'none'
  const Icon = isPlan ? ClipboardList : FileText
  const label = isPlan
    ? locale === 'zh' ? '计划文档' : 'Plan document'
    : kindLabel === 'Document' && locale === 'zh' ? '文档' : kindLabel

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
        e.currentTarget.style.background = 'color-mix(in srgb, var(--c-bg-deep) 28%, var(--c-bg-menu) 72%)'
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
        <Icon size={18} style={{ color: 'var(--c-text-icon)' }} />
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
          {label}
        </span>
      </div>
    </button>
  )
}

export function DocumentCard({ artifact, onClick, active }: Props) {
  const isPlan = isPlanMarkdownPath(artifact.filename)
  return (
    <DocumentResourceCard
      title={artifact.title || artifact.filename}
      isPlan={isPlan}
      onClick={onClick}
      active={active}
    />
  )
}
