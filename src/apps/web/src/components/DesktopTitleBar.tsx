import { useCallback, useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, PanelLeftClose, PanelLeftOpen, Glasses, ArrowUp } from 'lucide-react'
import { isDesktop } from '@arkloop/shared/desktop'
import type { AppUpdaterState } from '@arkloop/shared/desktop'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { Button } from '@arkloop/shared'
import { ModeSwitch } from './ModeSwitch'
import { useLocale } from '../contexts/LocaleContext'
import type { AppMode } from '../storage'
import { openExternal } from '../openExternal'
import { beginPerfTrace, endPerfTrace } from '../perfDebug'

export const DESKTOP_TITLEBAR_HEIGHT = 44

type Props = {
  sidebarCollapsed: boolean
  onToggleSidebar: () => void
  appMode: AppMode
  onSetAppMode: (mode: AppMode) => void
  availableModes: AppMode[]
  showIncognitoToggle?: boolean
  isPrivateMode?: boolean
  onTogglePrivateMode?: () => void
  hasComponentUpdates?: boolean
  appUpdateState?: AppUpdaterState | null
  onCheckAppUpdate?: () => void
  onDownloadApp?: () => void
  onInstallApp?: () => void
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
  appUpdateState,
  onCheckAppUpdate,
  onDownloadApp,
  onInstallApp,
}: Props) {
  const { t } = useLocale()
  const sidebarToggleTrace = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const updateBtnRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const [updatePopoverOpen, setUpdatePopoverOpen] = useState(false)

  const togglePopover = useCallback(() => {
    setUpdatePopoverOpen((prev) => {
      if (!prev) onCheckAppUpdate?.()
      return !prev
    })
  }, [onCheckAppUpdate])

  // click outside to close
  useEffect(() => {
    if (!updatePopoverOpen) return
    const handler = (e: MouseEvent) => {
      const target = e.target as Node
      if (updateBtnRef.current?.contains(target) || popoverRef.current?.contains(target)) return
      setUpdatePopoverOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [updatePopoverOpen])

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
        {hasComponentUpdates && onCheckAppUpdate && (
          <button
            ref={updateBtnRef}
            onClick={togglePopover}
            title={t.componentUpdatesAvailable}
            className="relative flex h-8 w-8 items-center justify-center rounded-md text-[var(--c-accent)] transition-colors hover:bg-[var(--c-bg-deep)]"
          >
            <ArrowUp size={16} />
            <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-[var(--c-accent)]" />
          </button>
        )}
        {updatePopoverOpen && <UpdatePopover
          ref={popoverRef}
          btnRef={updateBtnRef}
          state={appUpdateState ?? null}
          onDownload={onDownloadApp}
          onInstall={onInstallApp}
        />}
      </div>
    </div>
  )
}

const GITHUB_RELEASES_URL = 'https://github.com/qqqqqf-q/Arkloop/releases/latest'

type UpdatePopoverProps = {
  btnRef: React.RefObject<HTMLButtonElement | null>
  state: AppUpdaterState | null
  onDownload?: () => void
  onInstall?: () => void
}

import { forwardRef } from 'react'

const UpdatePopover = forwardRef<HTMLDivElement, UpdatePopoverProps>(function UpdatePopover(
  { btnRef, state, onDownload, onInstall },
  ref,
) {
  const { t } = useLocale()
  const ds = (t as unknown as Record<string, unknown>).desktopSettings as Record<string, string> | undefined
  const isMac = navigator.platform.toLowerCase().includes('mac')

  // position from button rect
  const rect = btnRef.current?.getBoundingClientRect()
  const top = rect ? rect.bottom + 6 : 50
  const right = rect ? window.innerWidth - rect.right : 12

  const phase = state?.phase ?? 'idle'

  const renderContent = () => {
    switch (phase) {
      case 'idle':
      case 'not-available':
        return (
          <div>
            <p className="text-sm text-[var(--c-text-secondary)]">{ds?.appUpdateLatest ?? 'Up to date'}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
              {ds?.appUpdateTitle ?? 'Desktop App'} v{state?.currentVersion ?? ''}
            </p>
          </div>
        )

      case 'checking':
        return (
          <div className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <SpinnerIcon />
            <span>{ds?.appUpdateChecking ?? 'Checking...'}</span>
          </div>
        )

      case 'available':
        return (
          <div className="flex flex-col gap-3">
            <div>
              <p className="text-sm font-medium text-[var(--c-text-primary)]">
                {ds?.appUpdateAvailable ?? 'Update available'}
                {state?.latestVersion && (
                  <span className="ml-1.5 rounded-full px-1.5 py-0.5 text-xs font-medium" style={{ background: 'color-mix(in srgb, var(--c-accent) 15%, transparent)', color: 'var(--c-accent)' }}>
                    v{state.latestVersion}
                  </span>
                )}
              </p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                {ds?.appUpdateTitle ?? 'Desktop App'} v{state?.currentVersion ?? ''}
              </p>
            </div>
            {isMac ? (
              <Button variant="primary" size="md" onClick={() => openExternal(GITHUB_RELEASES_URL)}>
                {t.goToDownload}
              </Button>
            ) : (
              <Button variant="primary" size="md" onClick={onDownload}>
                {ds?.appUpdateDownload ?? 'Download'}
              </Button>
            )}
          </div>
        )

      case 'downloading':
        return (
          <div className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <SpinnerIcon />
            <span>{ds?.appUpdateDownloading ?? 'Downloading'} {state?.progressPercent ?? 0}%</span>
          </div>
        )

      case 'downloaded':
        return (
          <div className="flex flex-col gap-2">
            <p className="text-sm text-[var(--c-text-primary)]">{ds?.appUpdateReady ?? 'Ready to install'}</p>
            <Button variant="primary" size="sm" onClick={onInstall}>
              {ds?.appUpdateInstall ?? 'Install'}
            </Button>
          </div>
        )

      case 'error':
        return (
          <p className="text-sm text-[var(--c-status-error-text)]">
            {state?.error ?? (ds?.appUpdateError ?? 'Update failed')}
          </p>
        )

      case 'unsupported':
        return <p className="text-sm text-[var(--c-text-tertiary)]">{ds?.appUpdateUnsupported ?? 'Available in packaged builds only'}</p>

      default:
        return null
    }
  }

  return (
    <div
      ref={ref}
      style={{
        position: 'fixed',
        top: `${top}px`,
        right: `${right}px`,
        width: 280,
        zIndex: 1000,
        background: 'var(--c-bg-page)',
        border: '0.5px solid var(--c-border-mid)',
        borderRadius: 12,
        boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
        padding: 14,
        animation: 'updatePopoverIn 150ms ease-out',
      }}
    >
      {renderContent()}
      <style>{`@keyframes updatePopoverIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }`}</style>
    </div>
  )
})