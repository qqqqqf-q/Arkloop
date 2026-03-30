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
  success: 'text-[var(--c-status-success-text)]',
  error: 'text-[var(--c-status-error-text)]',
  neutral: 'text-[var(--c-text-secondary)]',
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  return (
    <div
      className={[
        'flex items-center gap-2 rounded-lg px-4 py-2.5',
        toast.exiting ? 'toast-exit' : 'toast-enter',
      ].join(' ')}
      style={{
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-menu)',
      }}
    >
      <span className={`flex-1 text-sm ${variantText[toast.variant]}`}>{toast.message}</span>
      <button
        onClick={onDismiss}
        className="shrink-0 text-[var(--c-text-muted)] transition-opacity hover:opacity-70"
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
        <div className="fixed bottom-4 right-4 z-[60] flex flex-col gap-2">
          {toasts.map((t) => (
            <ToastItem key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
          ))}
        </div>,
        document.body,
      )}
    </ToastContext.Provider>
  )
}
