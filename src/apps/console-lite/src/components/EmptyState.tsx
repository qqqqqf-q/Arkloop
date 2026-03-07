import type { ReactNode } from 'react'
import { Inbox } from 'lucide-react'

type Props = {
  icon?: ReactNode
  message?: string
}

export function EmptyState({ icon, message = 'No data' }: Props) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-[var(--c-text-muted)]">
      <span className="opacity-40">{icon ?? <Inbox size={32} />}</span>
      <p className="text-sm">{message}</p>
    </div>
  )
}
