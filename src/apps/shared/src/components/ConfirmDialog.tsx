import { Modal } from './Modal'
import { Button } from './Button'

type Props = {
  open: boolean
  onClose: () => void
  onConfirm: () => void
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  loading?: boolean
}

export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title = 'Confirm',
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  loading = false,
}: Props) {
  return (
    <Modal open={open} onClose={onClose} title={title} width="400px">
      <p className="text-sm text-[var(--c-text-secondary)]">{message}</p>
      <div className="mt-7 flex justify-end gap-2">
        <Button
          variant="outline"
          size="md"
          onClick={onClose}
          disabled={loading}
        >
          {cancelLabel}
        </Button>
        <Button
          variant="danger"
          size="md"
          onClick={onConfirm}
          disabled={loading}
        >
          {loading ? '…' : confirmLabel}
        </Button>
      </div>
    </Modal>
  )
}
