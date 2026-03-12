import { useState, useCallback, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { ToastContext, type ToastVariant } from './toast-context'

type Toast = {
  id: string
  message: string
  variant: ToastVariant
}

const variantText: Record<ToastVariant, string> = {
  success: 'text-[var(--c-status-success-text)]',
  error: 'text-[var(--c-status-error-text)]',
  neutral: 'text-[var(--c-text-secondary)]',
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-4 py-2.5 shadow-lg">
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

  const addToast = useCallback((message: string, variant: ToastVariant = 'neutral') => {
    const id = crypto.randomUUID()
    setToasts((prev) => [...prev, { id, message, variant }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 4000)
  }, [])

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

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
