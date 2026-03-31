import { useEffect, useCallback, useRef, type ReactNode, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'

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
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    >
      <div
        className="modal-enter flex max-h-[85vh] flex-col rounded-[14px]"
        style={{
          background: 'var(--c-bg-page)',
          border: '0.5px solid var(--c-border-subtle)',
          width: `min(${width}, calc(100vw - 40px))`,
        }}
      >
        {title && (
          <div className="flex items-center justify-between px-5 py-4" style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}>
            <h3 className="text-[15px] font-semibold text-[var(--c-text-heading)]">{title}</h3>
          </div>
        )}
        <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>,
    document.body,
  )
}
