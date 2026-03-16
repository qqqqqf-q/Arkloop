import { Blocks } from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'

export function ExtensionsSettings() {
  const { t } = useLocale()
  const ds = t.desktopSettings

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
          {ds.extensionsTitle}
        </h3>
        <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
          {ds.extensionsDesc}
        </p>
      </div>
      <div
        className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <Blocks size={32} className="mb-3 text-[var(--c-text-muted)]" />
        <p className="text-sm text-[var(--c-text-muted)]">{t.comingSoon}</p>
      </div>
    </div>
  )
}
