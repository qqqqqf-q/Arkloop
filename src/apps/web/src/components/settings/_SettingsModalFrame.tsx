import { useEffect, type MouseEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'

type Props = {
  open: boolean
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  width?: number
}

export function SettingsModalFrame({
  open,
  title,
  onClose,
  children,
  footer,
  width = 510,
}: Props) {
  useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose, open])

  if (!open) return null

  const handleOverlayClick = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target === event.currentTarget) onClose()
  }

  return createPortal(
    <div
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'rgba(0, 0, 0, 0.58)' }}
      onClick={handleOverlayClick}
    >
      <div
        className="design-token-modal-enter flex flex-col rounded-[17px] p-7"
        style={{
          width: `min(${width}px, calc(100vw - 40px))`,
          background: 'var(--c-bg-menu)',
          border: '0.5px solid var(--c-border-subtle)',
        }}
      >
        <div className="flex w-full items-center justify-between">
          <h3 className="text-[19px] font-semibold leading-none text-[var(--c-text-heading)]">{title}</h3>
          <button
            type="button"
            aria-label="关闭"
            onClick={onClose}
            className="-mr-2 flex h-8 w-8 items-center justify-center rounded-[7px] text-[color-mix(in_srgb,var(--c-border)_72%,var(--c-text-primary)_28%)] transition-colors duration-[160ms] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]"
          >
            <X size={16} />
          </button>
        </div>
        {children}
        {footer && <div className="mt-7 flex items-center justify-end gap-2">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}
