import type { ReactNode } from 'react'

type Props = {
  icon: ReactNode
  label: string
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
}

export function DropdownAction({ icon, label, onClick, disabled, destructive }: Props) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={[
        'flex w-full items-center gap-2 rounded-[8px] bg-[var(--c-bg-menu)] px-3 py-2 text-sm font-[450] transition-colors duration-100 disabled:cursor-not-allowed disabled:opacity-40',
        destructive
          ? 'text-[var(--c-status-error-text)] [&:not(:disabled)]:hover:bg-[var(--c-error-bg)]'
          : 'text-[var(--c-text-secondary)] [&:not(:disabled)]:hover:bg-[var(--c-bg-deep)] [&:not(:disabled)]:hover:text-[var(--c-text-primary)]',
      ].join(' ')}
    >
      {icon}
      {label}
    </button>
  )
}
