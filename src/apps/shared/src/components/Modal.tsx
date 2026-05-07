import { useEffect, useCallback, useRef, type ReactNode, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'

type Props = {
  open: boolean
  onClose: () => void
  title?: string
  children: ReactNode
  width?: string
}

export function Modal({ open, onClose, title, children, width = '480px' }: Props) {
  const overlayRef = useRef<HTMLDivElement>(null)

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    },
    [onClose],
  )

  useEffect(() => {
    if (!open) return
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, handleKeyDown])

  const handleOverlayClick = useCallback(
    (e: MouseEvent<HTMLDivElement>) => {
      if (e.target === overlayRef.current) onClose()
    },
    [onClose],
  )

  if (!open) return null

  return createPortal(
    <div
      ref={overlayRef}
      onClick={handleOverlayClick}
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'rgba(0, 0, 0, 0.58)' }}
    >
      <div
        className="design-token-modal-enter flex max-h-[85vh] flex-col rounded-[17px] p-7"
        style={{
          background: 'var(--c-bg-menu)',
          border: '0.5px solid var(--c-border-subtle)',
          width: `min(${width}, calc(100vw - 40px))`,
        }}
      >
        {title && (
          <div className="flex items-center justify-between">
            <h3 className="text-[19px] font-semibold leading-none text-[var(--c-text-heading)]">{title}</h3>
            <button
              type="button"
              aria-label="Close"
              onClick={onClose}
              className="-mr-2 flex h-8 w-8 shrink-0 items-center justify-center rounded-[7px] text-[color-mix(in_srgb,var(--c-border)_72%,var(--c-text-primary)_28%)] transition-colors duration-[160ms] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]"
            >
              <X size={16} />
            </button>
          </div>
        )}
        <div className={`${title ? 'mt-7' : ''} flex-1 overflow-y-auto`}>{children}</div>
      </div>
    </div>,
    document.body,
  )
}
