import type { ReactNode } from 'react'

type Variant = 'success' | 'warning' | 'error' | 'neutral'

type Props = {
  variant: Variant
  children: ReactNode
}

const styles: Record<Variant, string> = {
  success: 'bg-green-500/10 text-green-400',
  warning: 'bg-amber-500/10 text-amber-400',
  error:   'bg-rose-500/10 text-rose-400',
  neutral: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',
}

export function SettingsStatusBadge({ variant, children }: Props) {
  return (
    <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${styles[variant]}`}>
      {children}
    </span>
  )
}
