import type { ReactNode } from 'react'

export type BadgeVariant = 'success' | 'warning' | 'error' | 'neutral'

const variantStyles: Record<BadgeVariant, string> = {
  success: 'bg-[var(--c-status-success-bg)] text-[var(--c-status-success-text)]',
  warning: 'bg-[var(--c-status-warning-bg)] text-[var(--c-status-warning-text)]',
  error: 'bg-[var(--c-status-error-bg)] text-[var(--c-status-error-text)]',
  neutral: 'bg-[var(--c-bg-tag)] text-[var(--c-text-tertiary)]',
}

type Props = {
  variant?: BadgeVariant
  children: ReactNode
}

export function Badge({ variant = 'neutral', children }: Props) {
  return (
    <span className={`inline-flex items-center rounded-md px-1.5 py-0.5 text-xs font-medium ${variantStyles[variant]}`}>
      {children}
    </span>
  )
}
