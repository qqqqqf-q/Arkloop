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
  appMode,
  availableModes,
  browserPanelOpen = false,
  onSetAppMode,
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
      <div
        className="flex min-w-0 flex-1 items-center"
      >
        <div
          className="flex h-9 w-fit max-w-full min-w-0 items-center gap-1 overflow-x-auto px-1 py-0.5"
        >
        {availableModes.map((mode) => {
          const active = appMode === mode
          const label = mode === 'work' ? t.modeWork : t.modeChat
          return (
            <button
              key={mode}
              type="button"
              onClick={() => onSetAppMode(mode)}
              className="relative flex h-8 shrink-0 items-center justify-center rounded-[10px] px-3.5 text-[12.5px] leading-[18px] transition-colors"
              style={{
                background: active ? 'var(--c-mode-switch-pill)' : 'transparent',
                border: active ? '0.5px solid var(--c-mode-switch-border)' : '0.5px solid transparent',
                color: active ? 'var(--c-mode-switch-active-text)' : 'var(--c-mode-switch-inactive-text)',
              }}
            >
              {label}
            </button>
          )
        })}
        </div>
      </div>
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
