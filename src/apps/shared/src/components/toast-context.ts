import { createContext } from 'react'

export type ToastVariant = 'success' | 'error' | 'neutral'

export type ToastContextValue = {
  addToast: (message: string, variant?: ToastVariant) => void
}

export const ToastContext = createContext<ToastContextValue | null>(null)
