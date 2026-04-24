import type { ReactNode } from 'react'

type Props = {
  title?: string
  children: ReactNode
  className?: string
  overflow?: 'hidden' | 'visible'
}

const base = 'min-w-0 rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] p-5'

export const settingsSectionCls = `${base} overflow-hidden`

export function SettingsSection({ title, children, className, overflow = 'hidden' }: Props) {
  return (
    <div className={`${base} ${overflow === 'hidden' ? 'overflow-hidden' : 'overflow-visible'}${className ? ` ${className}` : ''}`}>
      {title && (
        <div className="mb-3 text-sm font-semibold text-[var(--c-text-heading)]">
          {title}
        </div>
      )}
      {children}
    </div>
  )
}
