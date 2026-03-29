import type { ReactNode } from 'react'

type Props = {
  children: ReactNode
  action?: ReactNode
  className?: string
}

export function SettingsCardRow({ children, action, className }: Props) {
  return (
    <div
      className={`flex items-center justify-between gap-4 px-4 py-4${className ? ` ${className}` : ''}`}
    >
      <div className="min-w-0 flex-1 pr-2">{children}</div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}
