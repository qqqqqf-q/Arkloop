import type { ReactNode } from 'react'

type Props = {
  title?: string
  children: ReactNode
  className?: string
}

const base = 'rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] p-5'

// eslint-disable-next-line react-refresh/only-export-components
export const settingsSectionCls = base

export function SettingsSection({ title, children, className }: Props) {
  return (
    <div className={`${base}${className ? ` ${className}` : ''}`}>
      {title && (
        <div className="mb-3 text-sm font-semibold text-[var(--c-text-heading)]">
          {title}
        </div>
      )}
      {children}
    </div>
  )
}
