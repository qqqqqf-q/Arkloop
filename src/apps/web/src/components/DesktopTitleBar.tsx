import { ChevronLeft, ChevronRight, PanelLeftClose, PanelLeftOpen, Glasses } from 'lucide-react'
import { isDesktop } from '@arkloop/shared/desktop'
import { ModeSwitch } from './ModeSwitch'
import { useLocale } from '../contexts/LocaleContext'
import type { AppMode } from '../storage'

export const DESKTOP_TITLEBAR_HEIGHT = 38

type Props = {
  sidebarCollapsed: boolean
  onToggleSidebar: () => void
  appMode: AppMode
  onSetAppMode: (mode: AppMode) => void
  availableModes: AppMode[]
  isPrivateMode?: boolean
  onTogglePrivateMode?: () => void
}

export function DesktopTitleBar({ sidebarCollapsed, onToggleSidebar, appMode, onSetAppMode, availableModes, isPrivateMode, onTogglePrivateMode }: Props) {
  const { t } = useLocale()

  if (!isDesktop()) return null

  const isMac = navigator.platform.toLowerCase().includes('mac')

  const btnCls = [
    'flex h-7 w-7 items-center justify-center rounded-md',
    'text-[var(--c-text-tertiary)] transition-colors',
    'hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
  ].join(' ')

  return (
    <div
      className="relative flex shrink-0 items-center"
      style={{
        height: DESKTOP_TITLEBAR_HEIGHT,
        paddingLeft: isMac ? '76px' : '16px',
        paddingRight: '12px',
        background: 'var(--c-bg-sidebar)',
        borderBottom: '0.5px solid var(--c-border-subtle)',
        WebkitAppRegion: 'drag',
      } as React.CSSProperties}
    >
      {/* sidebar toggle + back/forward — nudged 1px down to align with macOS traffic lights */}
      <div
        className="flex items-center gap-1 translate-y-px"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      >
        <button onClick={onToggleSidebar} className={btnCls}>
          {sidebarCollapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
        </button>
        <button onClick={() => window.history.back()} className={btnCls}>
          <ChevronLeft size={16} />
        </button>
        <button onClick={() => window.history.forward()} className={btnCls}>
          <ChevronRight size={16} />
        </button>
      </div>

      {/* ModeSwitch centered */}
      <div
        className="absolute left-1/2 -translate-x-1/2"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      >
        <ModeSwitch
          mode={appMode}
          onChange={onSetAppMode}
          labels={{ chat: t.modeChat, claw: t.modeClaw }}
          availableModes={availableModes}
        />
      </div>

      {/* Right side: private mode toggle (hidden in claw mode) */}
      {appMode !== 'claw' && onTogglePrivateMode && (
        <div
          className="ml-auto flex items-center"
          style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
        >
          <button
            onClick={onTogglePrivateMode}
            title={t.toggleIncognito}
            className={[
              'flex h-7 w-7 items-center justify-center rounded-md transition-colors',
              isPrivateMode
                ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
            ].join(' ')}
          >
            <Glasses size={15} />
          </button>
        </div>
      )}
    </div>
  )
}
