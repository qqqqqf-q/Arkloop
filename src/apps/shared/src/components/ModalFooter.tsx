import type { ReactNode } from 'react'
import { Button } from './Button'

type Props = {
  onCancel: () => void
  onConfirm: () => void
  cancelLabel?: string
  confirmLabel?: string
  loading?: boolean
  confirmDisabled?: boolean
  confirmVariant?: 'primary' | 'destructive'
  children?: ReactNode
}

export function ModalFooter({
  onCancel,
  onConfirm,
  cancelLabel = 'Cancel',
  confirmLabel = 'Save',
  loading = false,
  confirmDisabled = false,
  confirmVariant = 'primary',
  children,
}: Props) {
  return (
    <div className="flex items-center justify-end gap-2 border-t border-[var(--c-border)] pt-3">
      {children}
      <Button variant="outline" size="sm" onClick={onCancel} disabled={loading}>
        {cancelLabel}
      </Button>
      <Button
        variant={confirmVariant === 'destructive' ? 'danger' : 'primary'}
        size="sm"
        onClick={onConfirm}
        disabled={confirmDisabled}
        loading={loading}
      >
        {confirmLabel}
      </Button>
    </div>
  )
}
