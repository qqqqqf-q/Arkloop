import type { KeyboardEvent, ReactNode } from 'react'
import { motion } from 'framer-motion'

export function SettingsSummaryCard({
  children,
  onClick,
  className,
  minHeightClass = 'min-h-[138px]',
}: {
  children: ReactNode
  onClick?: () => void
  className?: string
  minHeightClass?: string
}) {
  const interactive = onClick !== undefined

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!interactive) return
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    onClick?.()
  }

  return (
    <motion.div
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      whileTap={interactive ? { scale: 0.96 } : undefined}
      transition={interactive ? { type: 'spring', stiffness: 620, damping: 22, mass: 0.42 } : undefined}
      className={[
        'group relative flex flex-col rounded-xl bg-[var(--c-bg-input)] p-4 text-left outline-none transition-[border-color,box-shadow,background-color] duration-[120ms]',
        minHeightClass,
        interactive ? 'cursor-pointer focus-visible:[box-shadow:0_0_0_1px_var(--c-input-border-color-hover)]' : '',
        'hover:[box-shadow:0_0_0_0.35px_var(--c-input-border-color-hover)]',
        className,
      ].filter(Boolean).join(' ')}
      style={{ border: '0.5px solid var(--c-input-border-color)' }}
    >
      {children}
    </motion.div>
  )
}

export function SettingsSummaryCardBadge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-md bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">
      {children}
    </span>
  )
}

export function SettingsSummaryCardLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">{label}</div>
      <div className="mt-0.5 truncate text-[12px] font-medium leading-tight text-[var(--c-text-secondary)]">{value}</div>
    </div>
  )
}
