import { forwardRef, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import {
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  Copy,
  Glasses,
  Minus,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  Square,
  SquarePen,
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
import { ActionIconButton } from './ActionIconButton'
import { SHORTCUTS } from '../shortcuts'
import { formatDesktopAppVersion } from '../desktopVersion'
import type { ThreadResponse } from '../api'
import { useThreadLiveState } from '../contexts/thread-list'

export const DESKTOP_TITLEBAR_HEIGHT = 44
const WINDOWS_TITLEBAR_HEIGHT = 44
const MAC_TITLEBAR_LEFT_PADDING = 76
const DESKTOP_ICON_RAIL_LEFT_PADDING = 12
const PINNED_MENU_OPEN_DELAY_MS = 70
const PINNED_MENU_GAP = 4

type Props = {
  sidebarCollapsed: boolean
  onToggleSidebar: () => void
  onNewThread?: () => void
  appMode: AppMode
  onSetAppMode: (mode: AppMode) => void
  availableModes: AppMode[]
  showIncognitoToggle?: boolean
  isPrivateMode?: boolean
  onTogglePrivateMode?: () => void
  rightPanelOpen?: boolean
  onToggleRightPanel?: () => void
  showTitleBarNewThread?: boolean
  hasAppUpdate?: boolean
  appUpdateState?: AppUpdaterState | null
  onCheckAppUpdate?: () => void
  onDownloadApp?: () => void
  onInstallApp?: () => void
  onOpenSettings?: (tab?: SettingsTab | 'voice') => void
  pinnedThreads?: ThreadResponse[]
  activeThreadId?: string | null
  onSelectPinnedThread?: (threadId: string) => void
}

export function DesktopTitleBar({
  sidebarCollapsed,
  onToggleSidebar,
  onNewThread,
  appMode,
  onSetAppMode,
  availableModes,
  showIncognitoToggle = true,
  isPrivateMode,
  onTogglePrivateMode,
  rightPanelOpen = false,
  onToggleRightPanel,
  showTitleBarNewThread = false,
  hasAppUpdate = false,
  appUpdateState,
  onCheckAppUpdate,
  onDownloadApp,
  onInstallApp,
  onOpenSettings,
  pinnedThreads = [],
  activeThreadId,
  onSelectPinnedThread,
}: Props) {
  const { t } = useLocale()
  const sidebarToggleTrace = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const updateBtnRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const pinnedMenuAnchorRef = useRef<HTMLDivElement>(null)
  const pinnedMenuOpenTimerRef = useRef<number | null>(null)
  const pinnedMenuCloseTimerRef = useRef<number | null>(null)
  const pinnedMenuSuppressedRef = useRef(false)
  const [updatePopoverOpen, setUpdatePopoverOpen] = useState(false)
  const [updatePopoverPosition, setUpdatePopoverPosition] = useState<{ top: number; right: number }>({ top: 50, right: 12 })
  const [pinnedMenuOpen, setPinnedMenuOpen] = useState(false)
  const [pinnedMenuPosition, setPinnedMenuPosition] = useState<{ left: number; top: number }>({ left: 0, top: 0 })
  const [windowMaximized, setWindowMaximized] = useState(false)
  const desktopPlatform = getDesktopPlatform()
  const isMac = desktopPlatform === 'darwin'
  const isWindows = desktopPlatform === 'win32'
  const titleBarHeight = isWindows ? WINDOWS_TITLEBAR_HEIGHT : DESKTOP_TITLEBAR_HEIGHT
  const hasActionableAppUpdate =
    appUpdateState?.phase === 'available' ||
    appUpdateState?.phase === 'downloaded'

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
      if (appUpdateState?.phase !== 'available') return false
      // sessionStorage: 本次会话内跳过
      if (sessionStorage.getItem('arkloop:skip_update_once')) return true
      // localStorage: 永久跳过当前版本
      const skippedVersion = localStorage.getItem('arkloop:skip_version')
      if (!skippedVersion || !appUpdateState?.latestVersion) return false
      return skippedVersion === appUpdateState.latestVersion
    } catch {
      return false
    }
  }, [appUpdateState?.latestVersion, appUpdateState?.phase])

  const updateButtonTitle = isVersionSkipped
    ? t.updateSkipped
    : appUpdateState?.phase === 'downloaded'
      ? t.desktopSettings.appUpdateReady
      : t.desktopSettings.appUpdateAvailable
  const newThreadLabel = appMode === 'work' ? t.newTask : t.newChat
  const showPinnedSidebarPicker = sidebarCollapsed && pinnedThreads.length > 0

  const clearPinnedMenuTimers = useCallback(() => {
    if (pinnedMenuOpenTimerRef.current !== null) {
      window.clearTimeout(pinnedMenuOpenTimerRef.current)
      pinnedMenuOpenTimerRef.current = null
    }
    if (pinnedMenuCloseTimerRef.current !== null) {
      window.clearTimeout(pinnedMenuCloseTimerRef.current)
      pinnedMenuCloseTimerRef.current = null
    }
  }, [])

  const openPinnedMenu = useCallback(() => {
    if (!showPinnedSidebarPicker || pinnedMenuSuppressedRef.current) return
    if (pinnedMenuOpenTimerRef.current !== null) window.clearTimeout(pinnedMenuOpenTimerRef.current)
    if (pinnedMenuCloseTimerRef.current !== null) {
      window.clearTimeout(pinnedMenuCloseTimerRef.current)
      pinnedMenuCloseTimerRef.current = null
    }
    const rect = pinnedMenuAnchorRef.current?.getBoundingClientRect()
    if (rect) setPinnedMenuPosition({ left: rect.left, top: rect.bottom + PINNED_MENU_GAP })
    pinnedMenuOpenTimerRef.current = window.setTimeout(() => {
      pinnedMenuOpenTimerRef.current = null
      setPinnedMenuOpen(true)
    }, PINNED_MENU_OPEN_DELAY_MS)
  }, [showPinnedSidebarPicker])

  const handlePinnedMenuAnchorEnter = useCallback(() => {
    pinnedMenuSuppressedRef.current = false
    openPinnedMenu()
  }, [openPinnedMenu])

  const schedulePinnedMenuClose = useCallback(() => {
    clearPinnedMenuTimers()
    pinnedMenuCloseTimerRef.current = window.setTimeout(() => {
      pinnedMenuCloseTimerRef.current = null
      setPinnedMenuOpen(false)
    }, 120)
  }, [clearPinnedMenuTimers])

  const suppressPinnedMenuForHoverCycle = useCallback(() => {
    pinnedMenuSuppressedRef.current = true
    clearPinnedMenuTimers()
    setPinnedMenuOpen(false)
  }, [clearPinnedMenuTimers])

  useEffect(() => {
    return () => {
      clearPinnedMenuTimers()
    }
  }, [clearPinnedMenuTimers])

  const togglePopover = useCallback(() => {
    setUpdatePopoverOpen((prev) => {
      if (!prev) {
        const rect = updateBtnRef.current?.getBoundingClientRect()
        setUpdatePopoverPosition(
          rect
            ? { top: rect.bottom + 6, right: window.innerWidth - rect.right }
            : { top: 50, right: 12 },
        )
        if (!hasActionableAppUpdate) onCheckAppUpdate?.()
      }
      return !prev
    })
  }, [hasActionableAppUpdate, onCheckAppUpdate])

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
      className="relative shrink-0"
      style={{
        height: titleBarHeight,
        paddingLeft: `${isMac ? MAC_TITLEBAR_LEFT_PADDING : DESKTOP_ICON_RAIL_LEFT_PADDING}px`,
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
        className={isWindows ? 'flex items-center gap-1.5' : 'flex items-center gap-1'}
        style={{
          position: 'absolute',
          left: isMac ? MAC_TITLEBAR_LEFT_PADDING : DESKTOP_ICON_RAIL_LEFT_PADDING,
          top: isWindows ? 0 : 6,
          height: isWindows ? '100%' : undefined,
          zIndex: 2,
          WebkitAppRegion: 'no-drag',
        } as React.CSSProperties}
      >
        <div
          ref={pinnedMenuAnchorRef}
          className="relative inline-flex"
          onMouseEnter={handlePinnedMenuAnchorEnter}
          onMouseLeave={schedulePinnedMenuClose}
        >
          <ActionIconButton
            onClick={() => {
              suppressPinnedMenuForHoverCycle()
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
            tooltip={showPinnedSidebarPicker ? undefined : sidebarCollapsed ? t.showSidebarAction : t.hideSidebarAction}
            shortcut={showPinnedSidebarPicker ? undefined : SHORTCUTS.toggleSidebar.binding}
            className={btnCls}
          >
            {sidebarCollapsed ? <PanelLeftOpen size={17} /> : <PanelLeftClose size={17} />}
          </ActionIconButton>
          {showPinnedSidebarPicker && pinnedMenuOpen && createPortal(
            <TitleBarPinnedThreadsHoverArea
              position={pinnedMenuPosition}
              onMouseEnter={openPinnedMenu}
              onMouseLeave={schedulePinnedMenuClose}
            >
              <TitleBarPinnedThreadsMenu
                threads={pinnedThreads}
                activeThreadId={activeThreadId ?? null}
                onSelect={(threadId) => {
                  onSelectPinnedThread?.(threadId)
                  setPinnedMenuOpen(false)
                }}
              />
            </TitleBarPinnedThreadsHoverArea>,
            document.body,
          )}
        </div>
        <ActionIconButton onClick={() => window.history.back()} tooltip={t.browserPanel.back} className={btnCls}>
          <ChevronLeft size={17} />
        </ActionIconButton>
        <ActionIconButton onClick={() => window.history.forward()} tooltip={t.browserPanel.forward} className={btnCls}>
          <ChevronRight size={17} />
        </ActionIconButton>
        {showTitleBarNewThread && onNewThread && (
          <ActionIconButton
            onClick={onNewThread}
            aria-label={newThreadLabel}
            tooltip={newThreadLabel}
            className={btnCls}
          >
            <SquarePen size={17} />
          </ActionIconButton>
        )}
      </div>

      {/* centered mode switch */}
      <div
        className="min-w-0"
        style={{
          position: 'absolute',
          left: '50%',
          top: '50%',
          transform: 'translate(-50%, -50%) translateY(1px)',
          zIndex: 1,
          WebkitAppRegion: 'no-drag',
        } as React.CSSProperties}
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
        className={isWindows ? 'flex items-stretch' : 'flex items-center'}
        style={{
          position: 'absolute',
          right: isWindows ? 0 : 12,
          top: 0,
          bottom: 0,
          zIndex: 2,
          WebkitAppRegion: 'no-drag',
        } as React.CSSProperties}
      >
        <div className={isWindows ? 'flex items-center justify-end gap-1 pr-2' : 'flex items-center justify-end'}>
          {showIncognitoToggle && onTogglePrivateMode && (
            <ActionIconButton
              onClick={onTogglePrivateMode}
              tooltip={t.toggleIncognito}
              className={[
                'flex h-8 w-8 items-center justify-center rounded-md transition-colors',
                isPrivateMode
                  ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
              ].join(' ')}
            >
              <Glasses size={17} />
            </ActionIconButton>
          )}
          {onToggleRightPanel && (
            <ActionIconButton
              onClick={onToggleRightPanel}
              tooltip={t.rightPanel.toggle}
              shortcut={SHORTCUTS.toggleRightPanel.binding}
              className="flex h-8 w-8 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
            >
              {rightPanelOpen ? <PanelRightClose size={17} /> : <PanelRightOpen size={17} />}
            </ActionIconButton>
          )}
          {hasAppUpdate && (
            <button
              ref={updateBtnRef}
              onClick={togglePopover}
              title={updateButtonTitle}
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
        {updatePopoverOpen && createPortal(
          <UpdatePopover
            ref={popoverRef}
            position={updatePopoverPosition}
            state={appUpdateState ?? null}
            onDownload={onDownloadApp}
            onInstall={onInstallApp}
            onOpenSettings={onOpenSettings}
            onClose={() => setUpdatePopoverOpen(false)}
          />,
          document.body,
        )}
      </div>
    </div>
  )
}

type TitleBarPinnedThreadsMenuProps = {
  threads: ThreadResponse[]
  activeThreadId: string | null
  onSelect: (threadId: string) => void
}

type TitleBarPinnedThreadsHoverAreaProps = {
  position: { left: number; top: number }
  children: ReactNode
  onMouseEnter: () => void
  onMouseLeave: () => void
}

function titleBarThreadTitle(thread: ThreadResponse, untitled: string): string {
  const title = (thread.title ?? '').trim()
  return title.length > 0 ? title : untitled
}

function TitleBarPinnedThreadsMenu({
  threads,
  activeThreadId,
  onSelect,
}: TitleBarPinnedThreadsMenuProps) {
  const { t } = useLocale()
  const { runningThreadIds, completedUnreadThreadIds } = useThreadLiveState()

  return (
    <div
      role="menu"
      className="dropdown-menu"
      style={{
        width: 260,
        maxHeight: 'min(320px, calc(100vh - 72px))',
        overflowY: 'auto',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        padding: '4px',
        background: 'var(--c-bg-menu)',
        boxShadow: 'var(--c-dropdown-shadow)',
        WebkitAppRegion: 'no-drag',
      } as React.CSSProperties}
    >
      <div className="flex flex-col gap-0.5">
        {threads.map((thread) => {
          const isActive = thread.id === activeThreadId
          const isRunning = runningThreadIds.has(thread.id)
          const isCompletedUnread = completedUnreadThreadIds.has(thread.id)

          return (
            <button
              key={thread.id}
              type="button"
              role="menuitem"
              onClick={() => onSelect(thread.id)}
              className={[
                'flex h-[34px] w-full items-center gap-2 rounded-[6px] px-2 text-left text-[13.5px] leading-[20px]',
                isActive
                  ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
              ].join(' ')}
              style={{ fontWeight: 'var(--c-sidebar-thread-weight)' }}
            >
              <span className="flex h-[14px] w-[16px] shrink-0 items-center justify-center" aria-hidden="true">
                {isRunning ? (
                  <span className="sidebar-running-dots">
                    <span className="sidebar-running-dot" />
                    <span className="sidebar-running-dot" />
                    <span className="sidebar-running-dot" />
                  </span>
                ) : isCompletedUnread ? (
                  <span className="sidebar-completed-unread-dot" />
                ) : (
                  <span className="h-[6px] w-[6px] rounded-full border border-[var(--c-text-muted)] opacity-40" />
                )}
              </span>
              <span className="min-w-0 flex-1 truncate">{titleBarThreadTitle(thread, t.untitled)}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function TitleBarPinnedThreadsHoverArea({
  position,
  children,
  onMouseEnter,
  onMouseLeave,
}: TitleBarPinnedThreadsHoverAreaProps) {
  return (
    <div
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      style={{
        position: 'fixed',
        left: position.left,
        top: position.top - PINNED_MENU_GAP,
        zIndex: 9999,
        paddingTop: PINNED_MENU_GAP,
        WebkitAppRegion: 'no-drag',
      } as React.CSSProperties}
    >
      {children}
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

  const handleOpenDetails = useCallback(() => {
    onOpenSettings?.('updates')
    onClose?.()
  }, [onClose, onOpenSettings])

  const renderContent = () => {
    switch (phase) {
      case 'idle':
      case 'not-available':
        return (
          <div className="flex flex-col gap-3">
            <div>
              <p className="text-sm text-[var(--c-text-secondary)]">{ds?.appUpdateLatest ?? 'Up to date'}</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                {ds?.appUpdateTitle ?? 'Desktop App'} v{formatDesktopAppVersion(state?.currentVersion)}
              </p>
            </div>
            <Button variant="primary" size="md" onClick={handleOpenDetails}>
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
                    v{formatDesktopAppVersion(state.latestVersion)}
                  </span>
                )}
              </p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                {ds?.appUpdateTitle ?? 'Desktop App'} v{formatDesktopAppVersion(state?.currentVersion)}
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
            <Button variant="outline" size="md" className="justify-center" onClick={handleOpenDetails}>
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
            <Button variant="outline" size="sm" className="justify-center" onClick={handleOpenDetails}>
              {ds?.appUpdateViewDetails ?? 'View update details'}
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
        zIndex: 9999,
        background: 'var(--c-bg-page)',
        border: '0.5px solid var(--c-border-mid)',
        borderRadius: 12,
        boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
        padding: 14,
        animation: 'updatePopoverIn 150ms ease-out',
        WebkitAppRegion: 'no-drag',
      } as React.CSSProperties}
    >
      {renderContent()}
      <style>{`@keyframes updatePopoverIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }`}</style>
    </div>
  )
})
