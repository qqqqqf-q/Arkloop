import { Server } from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'

export function MCPSettings() {
  const { t } = useLocale()
  const ds = t.desktopSettings

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
          {ds.mcpTitle}
        </h3>
        <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
          {ds.mcpDesc}
        </p>
      </div>
      <div
        className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <Server size={32} className="mb-3 text-[var(--c-text-muted)]" />
        <p className="text-sm text-[var(--c-text-muted)]">{t.comingSoon}</p>
      </div>
    </div>
  )
}
