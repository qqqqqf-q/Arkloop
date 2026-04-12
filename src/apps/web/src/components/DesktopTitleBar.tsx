import { useRef } from 'react'
import { ChevronLeft, ChevronRight, PanelLeftClose, PanelLeftOpen, Glasses, ArrowUp } from 'lucide-react'
import { isDesktop } from '@arkloop/shared/desktop'
import { ModeSwitch } from './ModeSwitch'
import { useLocale } from '../contexts/LocaleContext'
import type { AppMode } from '../storage'
import { beginPerfTrace, endPerfTrace } from '../perfDebug'

export const DESKTOP_TITLEBAR_HEIGHT = 44

type Props = {
  sidebarCollapsed: boolean
  onToggleSidebar: () => void
  appMode: AppMode
  onSetAppMode: (mode: AppMode) => void
  availableModes: AppMode[]
  /** chat 模式下显示；work 下隐藏。线程内点击由 ChatPage 注册到 layout，与欢迎页共用同一按钮 */
  showIncognitoToggle?: boolean
  isPrivateMode?: boolean
  onTogglePrivateMode?: () => void
  hasComponentUpdates?: boolean
  onOpenUpdates?: () => void
}

export function DesktopTitleBar({
  sidebarCollapsed,
  onToggleSidebar,
  appMode,
  onSetAppMode,
  availableModes,
  showIncognitoToggle = true,
  isPrivateMode,
  onTogglePrivateMode,
  hasComponentUpdates,
  onOpenUpdates,
}: Props) {
  const { t } = useLocale()
  const sidebarToggleTrace = useRef<ReturnType<typeof beginPerfTrace>>(null)

  if (!isDesktop()) return null

  const isMac = navigator.platform.toLowerCase().includes('mac')

  const btnCls = [
    'flex h-8 w-8 items-center justify-center rounded-md',
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
        className="flex items-center gap-1 self-start pt-[6px]"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      >
        <button
          onClick={() => {
            endPerfTrace(sidebarToggleTrace.current, {
              phase: 'click',
              collapsed: sidebarCollapsed,
              appMode,
            })
            sidebarToggleTrace.current = null
            onToggleSidebar()
          }}
          onPointerDown={() => {
            sidebarToggleTrace.current = beginPerfTrace('desktop_titlebar_sidebar_interaction', {
              phase: 'pointerdown',
              collapsed: sidebarCollapsed,
              appMode,
            })
          }}
          onPointerLeave={() => {
            sidebarToggleTrace.current = null
          }}
          className={btnCls}
        >
          {sidebarCollapsed ? <PanelLeftOpen size={17} /> : <PanelLeftClose size={17} />}
        </button>
        <button onClick={() => window.history.back()} className={btnCls}>
          <ChevronLeft size={17} />
        </button>
        <button onClick={() => window.history.forward()} className={btnCls}>
          <ChevronRight size={17} />
        </button>
      </div>

      {/* ModeSwitch centered */}
      <div
        className="absolute left-1/2 -translate-x-1/2 translate-y-px"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      >
        <ModeSwitch
          mode={appMode}
          onChange={onSetAppMode}
          labels={{ chat: t.modeChat, work: t.modeWork }}
          availableModes={availableModes}
        />
      </div>

      {/* Right side: always no-drag to prevent drag region from blocking right-side panels */}
      <div
        className="ml-auto flex items-center justify-end"
        style={{ WebkitAppRegion: 'no-drag', minWidth: '300px' } as React.CSSProperties}
      >
        {showIncognitoToggle && onTogglePrivateMode && (
          <button
            onClick={onTogglePrivateMode}
            title={t.toggleIncognito}
            className={[
              'flex h-8 w-8 items-center justify-center rounded-md transition-colors',
              isPrivateMode
                ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
            ].join(' ')}
          >
            <Glasses size={17} />
          </button>
        )}
        {hasComponentUpdates && onOpenUpdates && (
          <button
            onClick={onOpenUpdates}
            title={t.componentUpdatesAvailable}
            className="relative flex h-8 w-8 items-center justify-center rounded-md text-[var(--c-accent)] transition-colors hover:bg-[var(--c-bg-deep)]"
          >
            <ArrowUp size={16} />
            <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-[var(--c-accent)]" />
          </button>
        )}
      </div>
    </div>
  )
}
