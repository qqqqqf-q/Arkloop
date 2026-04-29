import { forwardRef, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  Copy,
  Glasses,
  Minus,
  PanelLeftClose,
  PanelLeftOpen,
  Square,
  X,
} from 'lucide-react'
import { getDesktopApi, getDesktopPlatform, isDesktop } from '@arkloop/shared/desktop'
import type { AppUpdaterState } from '@arkloop/shared/desktop'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { Button } from '@arkloop/shared'
import { ModeSwitch } from './ModeSwitch'
import { useLocale } from '../contexts/LocaleContext'
import type { AppMode } from '../storage'
import type { SettingsTab } from './SettingsModal'
import { openExternal } from '../openExternal'
import { beginPerfTrace, endPerfTrace } from '../perfDebug'
import { secondaryButtonSmCls, secondaryButtonBorderStyle } from './buttonStyles'

export const DESKTOP_TITLEBAR_HEIGHT = 44
const WINDOWS_TITLEBAR_HEIGHT = 44

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
  onOpenSettings?: (tab?: SettingsTab | 'voice') => void
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
  onOpenSettings,
}: Props) {
  const { t } = useLocale()
  const sidebarToggleTrace = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const updateBtnRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const [updatePopoverOpen, setUpdatePopoverOpen] = useState(false)
  const [updatePopoverPosition, setUpdatePopoverPosition] = useState<{ top: number; right: number }>({ top: 50, right: 12 })
  const [windowMaximized, setWindowMaximized] = useState(false)
  const desktopPlatform = getDesktopPlatform()
  const platformName = (desktopPlatform ?? navigator.platform).toLowerCase()
  const isMac = desktopPlatform === 'darwin' || (!desktopPlatform && platformName.includes('mac'))
  const isWindows = desktopPlatform === 'win32' || (!desktopPlatform && platformName.includes('win'))
  const titleBarHeight = isWindows ? WINDOWS_TITLEBAR_HEIGHT : DESKTOP_TITLEBAR_HEIGHT

  useEffect(() => {
    if (!isWindows) return
    const api = getDesktopApi()
    void api?.window?.isMaximized().then(setWindowMaximized).catch(() => {})
    return api?.window?.onMaximizedChanged?.(setWindowMaximized)
  }, [isWindows])

  const handleWindowMinimize = useCallback(() => {
    const request = getDesktopApi()?.window?.minimize()
    void request?.catch(() => {})
  }, [])

  const handleWindowMaximize = useCallback(() => {
    const request = getDesktopApi()?.window?.toggleMaximize()
    void request
      ?.then((result) => setWindowMaximized(result.maximized))
      .catch(() => {})
  }, [])

  const handleWindowClose = useCallback(() => {
    const request = getDesktopApi()?.window?.close()
    void request?.catch(() => {})
  }, [])

  // 检查是否跳过了当前版本
  const isVersionSkipped = useMemo(() => {
    try {
      // sessionStorage: 本次会话内跳过
      if (sessionStorage.getItem('arkloop:skip_update_once')) return true
      // localStorage: 永久跳过当前版本
      const skippedVersion = localStorage.getItem('arkloop:skip_version')
      if (!skippedVersion || !appUpdateState?.latestVersion) return false
      return skippedVersion === appUpdateState.latestVersion
    } catch {
      return false
    }
  }, [appUpdateState?.latestVersion])

  const togglePopover = useCallback(() => {
    setUpdatePopoverOpen((prev) => {
      if (!prev) {
        const rect = updateBtnRef.current?.getBoundingClientRect()
        setUpdatePopoverPosition(
          rect
            ? { top: rect.bottom + 6, right: window.innerWidth - rect.right }
            : { top: 50, right: 12 },
        )
        onCheckAppUpdate?.()
      }
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

  const btnCls = [
    'flex h-8 w-8 items-center justify-center rounded-md',
    'text-[var(--c-text-tertiary)] transition-colors',
    isWindows
      ? 'hover:bg-[var(--title-btn-hover)] hover:text-[var(--c-text-primary)]'
      : 'hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
  ].join(' ')

  return (
    <div
      className="relative grid shrink-0 items-center"
      style={{
        height: titleBarHeight,
        gridTemplateColumns: 'minmax(0, 1fr) auto minmax(0, 1fr)',
        paddingLeft: isMac ? '76px' : isWindows ? '12px' : '16px',
        paddingRight: isWindows ? 0 : '12px',
        background: isWindows
          ? 'color-mix(in srgb, var(--c-bg-sidebar) 92%, var(--c-bg-page))'
          : 'var(--c-bg-sidebar)',
        borderBottom: '0.5px solid var(--c-border-subtle)',
        WebkitAppRegion: 'drag',
      } as React.CSSProperties}
    >
      {/* sidebar and history controls */}
      <div
        className={isWindows ? 'flex min-w-0 items-center gap-1.5 justify-self-start' : 'flex min-w-0 items-center gap-1 self-start justify-self-start pt-[6px]'}
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

      {/* centered mode switch */}
      <div
        className="min-w-0 translate-y-px justify-self-center"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      >
        <ModeSwitch
          mode={appMode}
          onChange={onSetAppMode}
          labels={{ chat: t.modeChat, work: t.modeWork }}
          availableModes={availableModes}
        />
      </div>

      {/* app actions and window controls */}
      <div
        className={isWindows ? 'flex min-w-0 items-stretch justify-end self-stretch justify-self-end' : 'flex min-w-0 items-center justify-end justify-self-end'}
        style={{
          WebkitAppRegion: 'no-drag',
        } as React.CSSProperties}
      >
        <div className={isWindows ? 'flex items-center justify-end gap-1 pr-2' : 'flex items-center justify-end'}>
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
              title={isVersionSkipped ? t.updateSkipped : t.componentUpdatesAvailable}
              className={`relative flex h-8 w-8 items-center justify-center rounded-md transition-colors hover:bg-[var(--c-bg-deep)] ${
                isVersionSkipped ? 'text-[var(--c-text-muted)]' : 'text-[var(--c-accent)]'
              }`}
            >
              <ArrowUp size={16} />
              {!isVersionSkipped && (
                <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-[var(--c-accent)]" />
              )}
            </button>
          )}
        </div>
        {isWindows && (
          <WindowsWindowControls
            maximized={windowMaximized}
            onMinimize={handleWindowMinimize}
            onMaximize={handleWindowMaximize}
            onClose={handleWindowClose}
          />
        )}
        {updatePopoverOpen && <UpdatePopover
          ref={popoverRef}
          position={updatePopoverPosition}
          state={appUpdateState ?? null}
          onDownload={onDownloadApp}
          onInstall={onInstallApp}
          onOpenSettings={onOpenSettings}
          onClose={() => setUpdatePopoverOpen(false)}
        />}
      </div>
    </div>
  )
}

type WindowsWindowControlsProps = {
  maximized: boolean
  onMinimize: () => void
  onMaximize: () => void
  onClose: () => void
}

function WindowsWindowControls({
  maximized,
  onMinimize,
  onMaximize,
  onClose,
}: WindowsWindowControlsProps) {
  const buttonCls = [
    'flex h-full w-[46px] items-center justify-center',
    'text-[var(--c-text-secondary)] transition-colors',
    'hover:bg-[var(--title-btn-hover)] hover:text-[var(--c-text-primary)]',
  ].join(' ')

  return (
    <div className="flex h-full items-stretch">
      <button
        type="button"
        title="Minimize"
        aria-label="Minimize"
        className={buttonCls}
        onClick={onMinimize}
      >
        <Minus size={15} strokeWidth={1.8} />
      </button>
      <button
        type="button"
        title={maximized ? 'Restore' : 'Maximize'}
        aria-label={maximized ? 'Restore' : 'Maximize'}
        className={buttonCls}
        onClick={onMaximize}
      >
        {maximized ? (
          <Copy size={13} strokeWidth={1.7} />
        ) : (
          <Square size={12} strokeWidth={1.8} />
        )}
      </button>
      <button
        type="button"
        title="Close"
        aria-label="Close"
        className={[
          'flex h-full w-[46px] items-center justify-center',
          'text-[var(--c-text-secondary)] transition-colors',
          'hover:bg-[var(--c-window-close-hover)] hover:text-white',
        ].join(' ')}
        onClick={onClose}
      >
        <X size={15} strokeWidth={1.8} />
      </button>
    </div>
  )
}

const GITHUB_RELEASES_URL = 'https://github.com/qqqqqf-q/Arkloop/releases/latest'

type UpdatePopoverProps = {
  position: { top: number; right: number }
  state: AppUpdaterState | null
  onDownload?: () => void
  onInstall?: () => void
  onOpenSettings?: (tab?: SettingsTab | 'voice') => void
  onClose?: () => void
}

const UpdatePopover = forwardRef<HTMLDivElement, UpdatePopoverProps>(function UpdatePopover(
  { position, state, onDownload, onInstall, onOpenSettings, onClose },
  ref,
) {
  const { t } = useLocale()
  const ds = (t as unknown as Record<string, unknown>).desktopSettings as Record<string, string> | undefined
  const isMac = navigator.platform.toLowerCase().includes('mac')

  const phase = state?.phase === 'unsupported' ? 'not-available' : (state?.phase ?? 'idle')

  const handleSkipOnce = useCallback(() => {
    try {
      sessionStorage.setItem('arkloop:skip_update_once', '1')
    } catch {
      // Browser storage can be unavailable in private or embedded contexts.
    }
    onClose?.()
  }, [onClose])

  const latestVersion = state?.latestVersion
  const handleSkipVersion = useCallback(() => {
    if (latestVersion) {
      try {
        localStorage.setItem('arkloop:skip_version', latestVersion)
      } catch {
        // Browser storage can be unavailable in private or embedded contexts.
      }
    }
    onClose?.()
  }, [latestVersion, onClose])

  const renderContent = () => {
    switch (phase) {
      case 'idle':
      case 'not-available':
        return (
          <div className="flex flex-col gap-3">
            <div>
              <p className="text-sm text-[var(--c-text-secondary)]">{ds?.appUpdateLatest ?? 'Up to date'}</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                {ds?.appUpdateTitle ?? 'Desktop App'} v{state?.currentVersion ?? ''}
              </p>
            </div>
            <Button variant="primary" size="md" onClick={() => onOpenSettings?.('updates')}>
              {ds?.appUpdateViewDetails ?? 'View update details'}
            </Button>
            <div className="flex gap-2">
              <button type="button" className={`${secondaryButtonSmCls} flex-1`} style={secondaryButtonBorderStyle} onClick={handleSkipOnce}>
                {ds?.appUpdateSkipOnce ?? 'Skip for now'}
              </button>
              <button type="button" className={`${secondaryButtonSmCls} flex-1`} style={secondaryButtonBorderStyle} onClick={handleSkipVersion}>
                {ds?.appUpdateSkipVersion ?? 'Skip until next version'}
              </button>
            </div>
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
            <div className="flex gap-2">
              <button type="button" className={`${secondaryButtonSmCls} flex-1`} style={secondaryButtonBorderStyle} onClick={handleSkipOnce}>
                {ds?.appUpdateSkipOnce ?? 'Skip for now'}
              </button>
              <button type="button" className={`${secondaryButtonSmCls} flex-1`} style={secondaryButtonBorderStyle} onClick={handleSkipVersion}>
                {ds?.appUpdateSkipVersion ?? 'Skip until next version'}
              </button>
            </div>
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

      default:
        return null
    }
  }

  return (
    <div
      ref={ref}
      style={{
        position: 'fixed',
        top: `${position.top}px`,
        right: `${position.right}px`,
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
