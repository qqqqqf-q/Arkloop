import { useState, useCallback, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { ToastContext, type ToastVariant } from './toast-context'

type Toast = {
  id: string
  message: string
  variant: ToastVariant
  exiting?: boolean
}

const variantText: Record<ToastVariant, string> = {
  success: 'text-[var(--c-status-ok-text)]',
  error: 'text-[var(--c-danger-action-text)]',
  warn: 'text-[var(--c-status-warn-text)]',
  neutral: 'text-[var(--c-text-secondary)]',
}

const toastFrameBorder = 'color-mix(in srgb, var(--c-border) 91%, var(--c-text-primary) 9%)'

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  return (
    <div
      className={[
        'flex min-h-[32px] items-center gap-2 rounded-[6.5px] px-3.5 py-2 [background-clip:padding-box]',
        toast.exiting ? 'toast-exit' : 'toast-enter',
      ].join(' ')}
      style={{
        border: `0.65px solid ${toastFrameBorder}`,
        background: 'var(--c-bg-input)',
      }}
    >
      <span className={`flex-1 text-sm ${variantText[toast.variant]}`}>{toast.message}</span>
      <button
        type="button"
        onClick={onDismiss}
        className="-mr-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-[6px] text-[color-mix(in_srgb,var(--c-border)_72%,var(--c-text-primary)_28%)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]"
      >
        <X size={14} />
      </button>
    </div>
  )
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const dismiss = useCallback((id: string) => {
    // mark exiting first, then remove after animation
    setToasts((prev) => prev.map((t) => t.id === id ? { ...t, exiting: true } : t))
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 200)
  }, [])

  const addToast = useCallback((message: string, variant: ToastVariant = 'neutral') => {
    const id = crypto.randomUUID()
    setToasts((prev) => [...prev, { id, message, variant }])
    setTimeout(() => {
      dismiss(id)
    }, 4000)
  }, [dismiss])

  return (
    <ToastContext.Provider value={{ addToast }}>
      {children}
      {createPortal(
        <div className="fixed right-4 top-4 z-[60] flex flex-col gap-2">
          {toasts.map((t) => (
            <ToastItem key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
          ))}
        </div>,
        document.body,
      )}
    </ToastContext.Provider>
  )
}
