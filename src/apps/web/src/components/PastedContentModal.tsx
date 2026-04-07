import { X } from 'lucide-react'
import { createPortal } from 'react-dom'
import { useLocale } from '../contexts/LocaleContext'

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

type Props = {
  text: string
  size: number
  lineCount: number
  onClose: () => void
  title?: string
  subtitle?: string
}

export function PastedContentModal({ text, size, lineCount, onClose, title, subtitle }: Props) {
  const { t } = useLocale()

  const displayTitle = title ?? t.pastedContent
  const displaySubtitle = subtitle ?? `${formatSize(size)} · ${t.pastedLines(lineCount)}`

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-[2px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex flex-col rounded-2xl"
        style={{
          width: '680px',
          maxWidth: 'calc(100vw - 48px)',
          maxHeight: 'calc(100vh - 96px)',
          background: 'var(--c-bg-page)',
          border: '0.5px solid var(--c-border-subtle)',
          boxShadow: '0 24px 48px -12px rgba(0,0,0,0.25)',
        }}
      >
        {/* header */}
        <div style={{
          display: 'flex',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          padding: '20px 24px 0',
        }}>
          <div>
            <h2 style={{
              fontSize: '18px',
              fontWeight: 600,
              color: 'var(--c-text-heading)',
              lineHeight: 1.3,
              margin: 0,
            }}>
              {displayTitle}
            </h2>
            <p style={{
              fontSize: '13px',
              color: 'var(--c-text-muted)',
              marginTop: '4px',
            }}>
              {displaySubtitle}
            </p>
          </div>
          <button
            onClick={onClose}
            style={{
              width: '32px',
              height: '32px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '8px',
              border: 'none',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              flexShrink: 0,
            }}
            className="hover:bg-[var(--c-bg-deep)]"
          >
            <X size={18} />
          </button>
        </div>

        {/* content */}
        <div style={{
          flex: 1,
          overflow: 'auto',
          padding: '16px 24px 24px',
          minHeight: 0,
        }}>
          <div style={{
            background: 'var(--c-bg-input)',
            borderRadius: '10px',
            padding: '16px',
            border: '0.5px solid var(--c-border-subtle)',
          }}>
            <pre style={{
              margin: 0,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              fontSize: '14px',
              lineHeight: 1.6,
              color: 'var(--c-text-primary)',
              fontFamily: 'inherit',
            }}>
              {text}
            </pre>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  )
}
