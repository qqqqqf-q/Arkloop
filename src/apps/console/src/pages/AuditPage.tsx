import { useOutletContext } from 'react-router-dom'
import type { ConsoleOutletContext } from '../layouts/ConsoleLayout'

export function AuditPage() {
  useOutletContext<ConsoleOutletContext>()

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <header className="flex min-h-[46px] items-center border-b border-[var(--c-border-console)] px-6">
        <h2 className="text-sm font-medium text-[var(--c-text-secondary)]">Audit Logs</h2>
      </header>
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-[var(--c-text-muted)]">P41</p>
      </div>
    </div>
  )
}
