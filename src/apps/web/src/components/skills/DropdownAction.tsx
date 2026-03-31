import type { ReactNode } from 'react'

export function DropdownAction({ icon, label, onClick, disabled, destructive }: {
  icon: ReactNode
  label: string
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`flex w-full items-center gap-2 px-3 py-2 text-sm transition-colors duration-100 disabled:cursor-not-allowed disabled:opacity-40 bg-[var(--c-bg-menu)] ${destructive ? '[&:not(:disabled)]:hover:bg-[var(--c-error-bg)]' : '[&:not(:disabled)]:hover:bg-[var(--c-bg-deep)]'}`}
      style={{
        borderRadius: '8px',
        color: destructive ? 'var(--c-status-error-text)' : 'var(--c-text-secondary)',
      }}
    >
      {icon}
      {label}
    </button>
  )
}
