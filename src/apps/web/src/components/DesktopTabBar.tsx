import { PanelRightOpen } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import type { AppMode } from '../storage'

type Props = {
  appMode: AppMode
  availableModes: AppMode[]
  browserPanelOpen?: boolean
  onSetAppMode: (mode: AppMode) => void
  onToggleBrowserPanel?: () => void
}

export function DesktopTabBar({
  browserPanelOpen = false,
  onToggleBrowserPanel,
}: Props) {
  const { t } = useLocale()

  return (
    <div
      className="flex h-12 shrink-0 items-center gap-2 px-3"
      style={{
        borderBottom: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-page)',
      }}
    >
      <div className="flex min-w-0 flex-1 items-center" />
      {!browserPanelOpen && (
        <button
          type="button"
          onClick={onToggleBrowserPanel}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          title={t.browserPanelExpand}
        >
          <PanelRightOpen size={16} />
        </button>
      )}
    </div>
  )
}
