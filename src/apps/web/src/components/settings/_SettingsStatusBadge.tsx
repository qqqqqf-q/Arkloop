import type { ReactNode } from 'react'

type Variant = 'success' | 'warning' | 'error' | 'neutral'

type Props = {
  variant: Variant
  children: ReactNode
}

const styles: Record<Variant, { bg: string; dot: string }> = {
  success: { bg: 'bg-green-500/10 text-green-400', dot: 'bg-green-400' },
  warning: { bg: 'bg-amber-500/10 text-amber-400', dot: 'bg-amber-400' },
  error:   { bg: 'bg-rose-500/10 text-rose-400',   dot: 'bg-rose-400' },
  neutral: { bg: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]', dot: 'bg-[var(--c-text-muted)]' },
}

export function SettingsStatusBadge({ variant, children }: Props) {
  const s = styles[variant]
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${s.bg}`}>
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${s.dot}`} />
      {children}
    </span>
  )
}
