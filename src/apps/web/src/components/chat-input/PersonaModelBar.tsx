import { useRef, useState, useCallback, useEffect, useLayoutEffect } from 'react'
import { createPortal } from 'react-dom'
import { Plus, Paperclip, BookOpen, Folder, FolderOpen, X, Check, ListTodo } from 'lucide-react'
import { updateThreadSidebarState } from '../../api'
import { ModelPicker } from '../ModelPicker'
import type { SettingsTab } from '../SettingsModal'
import { getDesktopApi, isDesktop } from '@arkloop/shared/desktop'
import {
  readWorkFolder,
  writeWorkFolder,
  clearWorkFolder,
  readWorkRecentFolders,
  readThreadWorkFolder,
  writeThreadWorkFolder,
  clearThreadWorkFolder,
} from '../../storage'
import type { AppMode } from '../../storage'
import { useLocale } from '../../contexts/LocaleContext'
import { useThreadList } from '../../contexts/thread-list'

type Props = {
  selectedModel: string | null
  onModelChange: (model: string | null) => void
  thinkingEnabled: string
  onThinkingChange: (mode: string) => void
  onOpenSettings?: (tab: SettingsTab) => void
  onFileInputClick: () => void
  accessToken?: string
  variant?: 'welcome' | 'chat'
  appMode?: AppMode
  threadHasMessages?: boolean
  threadMessagesLoading?: boolean
  workThreadId?: string
  hideWorkFolderPicker?: boolean
  hideModelPicker?: boolean
  onMenuOpenChange?: (open: boolean) => void
  planMode?: boolean
  onTogglePlanMode?: (currentMode: boolean) => Promise<void>
  learningModeEnabled?: boolean
  onToggleLearningMode?: (currentMode: boolean) => Promise<void>
}

type FolderMenuPosition = {
  left: number
  top?: number
  bottom?: number
  maxHeight: number
  placement: 'up' | 'down'
}

function readActiveWorkFolder(threadId?: string): string | null {
  return threadId ? readThreadWorkFolder(threadId) : readWorkFolder()
}

export function PersonaModelBar({
  selectedModel,
  onModelChange,
  thinkingEnabled,
  onThinkingChange,
  onOpenSettings,
  onFileInputClick,
  accessToken,
  variant,
  appMode,
  threadHasMessages,
  threadMessagesLoading,
  workThreadId,
  hideWorkFolderPicker,
  hideModelPicker,
  onMenuOpenChange,
  planMode = false,
  onTogglePlanMode,
  learningModeEnabled = false,
  onToggleLearningMode,
}: Props) {
  const { t } = useLocale()
  const { threads, upsertThread } = useThreadList()
  const menuRef = useRef<HTMLDivElement>(null)
  const plusBtnRef = useRef<HTMLButtonElement>(null)
  const folderMenuRef = useRef<HTMLDivElement>(null)
  const folderBtnRef = useRef<HTMLButtonElement>(null)

  const [menuOpen, setMenuOpen] = useState(false)
  const [folderMenuOpen, setFolderMenuOpen] = useState(false)
  const [folderMenuPosition, setFolderMenuPosition] = useState<FolderMenuPosition | null>(null)
  const [workFolder, setWorkFolder] = useState<string | null>(() => readActiveWorkFolder(workThreadId))
  const [recentFolders, setRecentFolders] = useState<string[]>(() => readWorkRecentFolders())
  const isWorkMode = appMode === 'work'
  const showWorkFolderPicker = isWorkMode && isDesktop() && !hideWorkFolderPicker && !threadMessagesLoading && !threadHasMessages
  const effectiveFolderMenuOpen = folderMenuOpen && showWorkFolderPicker
  const currentThread = workThreadId ? threads.find((thread) => thread.id === workThreadId) ?? null : null
  const showLearningMode = learningModeEnabled || Boolean(onToggleLearningMode)
  const showPlanModeMenuItem = isWorkMode && Boolean(onTogglePlanMode)
  const showPlanMode = showPlanModeMenuItem && planMode
  const learningModeDisabled = !onToggleLearningMode
  const togglePlanMode = useCallback(() => {
    if (!onTogglePlanMode) return
    void onTogglePlanMode(planMode)
    setMenuOpen(false)
  }, [onTogglePlanMode, planMode])
  const toggleLearningMode = useCallback(() => {
    if (learningModeDisabled) return
    void onToggleLearningMode?.(learningModeEnabled)
    setMenuOpen(false)
  }, [learningModeDisabled, learningModeEnabled, onToggleLearningMode])
  const updateFolderMenuPosition = useCallback(() => {
    const button = folderBtnRef.current
    if (!button) return
    const rect = button.getBoundingClientRect()
    const gap = 8
    const viewportPadding = 8
    const menuMinWidth = 220
    const spaceBelow = window.innerHeight - rect.bottom - gap - viewportPadding
    const spaceAbove = rect.top - gap - viewportPadding
    const placement: FolderMenuPosition['placement'] = spaceBelow >= 220 || spaceBelow >= spaceAbove ? 'down' : 'up'
    const maxHeight = Math.max(0, placement === 'up' ? spaceAbove : spaceBelow)
    const left = Math.min(
      Math.max(viewportPadding, rect.left),
      Math.max(viewportPadding, window.innerWidth - menuMinWidth - viewportPadding),
    )

    setFolderMenuPosition(placement === 'up'
      ? { left, bottom: window.innerHeight - rect.top + gap, maxHeight, placement }
      : { left, top: rect.bottom + gap, maxHeight, placement })
  }, [])

  // close plus menu on outside click
  useEffect(() => {
    if (!menuOpen) return
    const handleClick = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        plusBtnRef.current?.contains(e.target as Node)
      ) return
      setMenuOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [menuOpen])

  // close folder menu on outside click
  useEffect(() => {
    if (!effectiveFolderMenuOpen) return
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (folderBtnRef.current?.contains(target)) return
      if (folderMenuRef.current && !folderMenuRef.current.contains(target)) {
        setFolderMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [effectiveFolderMenuOpen])

  useLayoutEffect(() => {
    if (!effectiveFolderMenuOpen) return
    updateFolderMenuPosition()
    window.addEventListener('resize', updateFolderMenuPosition)
    window.addEventListener('scroll', updateFolderMenuPosition, true)
    return () => {
      window.removeEventListener('resize', updateFolderMenuPosition)
      window.removeEventListener('scroll', updateFolderMenuPosition, true)
    }
  }, [effectiveFolderMenuOpen, updateFolderMenuPosition])

  // notify parent of menu open/close state changes (for focus management)
  useEffect(() => {
    onMenuOpenChange?.(menuOpen)
  }, [menuOpen, onMenuOpenChange])

  useEffect(() => {
    onMenuOpenChange?.(effectiveFolderMenuOpen)
  }, [effectiveFolderMenuOpen, onMenuOpenChange])

  useEffect(() => {
    const syncWorkFolder = () => {
      setWorkFolder(currentThread?.sidebar_work_folder ?? readActiveWorkFolder(workThreadId))
      setRecentFolders(readWorkRecentFolders())
    }
    syncWorkFolder()
    window.addEventListener('arkloop:work-folder-changed', syncWorkFolder)
    return () => window.removeEventListener('arkloop:work-folder-changed', syncWorkFolder)
  }, [currentThread?.sidebar_work_folder, workThreadId])

  const patchThreadWorkFolder = useCallback((threadId: string, folder: string | null, previous: string | null) => {
    if (!accessToken) return
    const thread = threads.find((item) => item.id === threadId)
    if (thread) upsertThread({ ...thread, sidebar_work_folder: folder })
    void updateThreadSidebarState(accessToken, threadId, { sidebar_work_folder: folder }).then((updated) => {
      upsertThread(updated)
    }).catch(() => {
      if (previous) writeThreadWorkFolder(threadId, previous)
      else clearThreadWorkFolder(threadId)
      if (thread) upsertThread(thread)
    })
  }, [accessToken, threads, upsertThread])

  const handleSelectFolder = useCallback(async (path?: string) => {
    let folder = path
    if (!folder) {
      const api = getDesktopApi()
      if (api?.dialog) {
        folder = (await api.dialog.openFolder()) ?? undefined
      }
    }
    if (!folder) return
    if (workThreadId) {
      const previous = readThreadWorkFolder(workThreadId)
      writeThreadWorkFolder(workThreadId, folder)
      patchThreadWorkFolder(workThreadId, folder, previous)
    } else {
      writeWorkFolder(folder)
    }
    setWorkFolder(folder)
    setRecentFolders(readWorkRecentFolders())
    setFolderMenuOpen(false)
  }, [patchThreadWorkFolder, workThreadId])

  return (
    <>
      {/* work folder picker -- desktop only, hidden in compact input or once thread has messages */}
      {showWorkFolderPicker && (
        <div
          className="relative -ml-1.5"
          style={{
            marginRight: '2px',
            animation: 'chip-enter 0.18s cubic-bezier(0.16, 1, 0.3, 1) both',
          }}
        >
          <button
            ref={folderBtnRef}
            type="button"
            onClick={() => setFolderMenuOpen((v) => !v)}
            className="flex h-[33.5px] items-center gap-1.5 rounded-lg px-2 text-[var(--c-text-secondary)] transition-[background] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
            style={{ maxWidth: '160px' }}
          >
            {workFolder
              ? <FolderOpen size={15} strokeWidth={1.5} style={{ flexShrink: 0 }} />
              : <Folder size={15} strokeWidth={1.5} style={{ flexShrink: 0 }} />
            }
            <span
              className="text-[12px] truncate"
              style={{ fontWeight: 400, maxWidth: '120px', color: workFolder ? 'var(--c-text-primary)' : 'var(--c-text-secondary)' }}
            >
              {workFolder
                ? workFolder.split('/').pop() || workFolder
                : 'Work in a folder'
              }
            </span>
          </button>

          {effectiveFolderMenuOpen && folderMenuPosition && createPortal((
            <div
              ref={folderMenuRef}
              className={`fixed z-50 ${folderMenuPosition.placement === 'down' ? 'dropdown-menu' : 'dropdown-menu-up'}`}
              style={{
                left: `${folderMenuPosition.left}px`,
                top: folderMenuPosition.top === undefined ? undefined : `${folderMenuPosition.top}px`,
                bottom: folderMenuPosition.bottom === undefined ? undefined : `${folderMenuPosition.bottom}px`,
                border: '0.5px solid var(--c-border-subtle)',
                borderRadius: '10px',
                padding: '4px',
                background: 'var(--c-bg-menu)',
                minWidth: '220px',
                maxHeight: `${folderMenuPosition.maxHeight}px`,
                overflowY: 'auto',
                boxShadow: 'var(--c-dropdown-shadow)',
              }}
            >
              <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                {recentFolders.length > 0 && (
                  <>
                    <div style={{ padding: '4px 12px 2px', fontSize: '11px', fontWeight: 500, color: 'var(--c-text-muted)', letterSpacing: '0.3px', textTransform: 'uppercase' }}>
                      Recent
                    </div>
                    {recentFolders.map((folder) => (
                      <button
                        key={folder}
                        type="button"
                        onClick={() => { void handleSelectFolder(folder) }}
                        className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                      >
                        <Folder size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                        <span className="truncate" style={{ flex: 1, textAlign: 'left' }}>
                          {folder.split('/').pop() || folder}
                        </span>
                        {workFolder === folder ? (
                          <Check size={12} style={{ flexShrink: 0, color: 'var(--c-accent)' }} />
                        ) : null}
                      </button>
                    ))}
                    <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                  </>
                )}

                <button
                  type="button"
                  onClick={() => { void handleSelectFolder() }}
                  className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                >
                  <FolderOpen size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                  Choose a different folder
                </button>

                {workFolder && (
                  <button
                    type="button"
                    onClick={() => {
                      if (workThreadId) {
                        const previous = readThreadWorkFolder(workThreadId)
                        clearThreadWorkFolder(workThreadId)
                        patchThreadWorkFolder(workThreadId, null, previous)
                      } else {
                        clearWorkFolder()
                      }
                      setWorkFolder(null)
                      setFolderMenuOpen(false)
                    }}
                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                  >
                    <X size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                    清除工作目录
                  </button>
                )}
              </div>
            </div>
          ), document.body)}
        </div>
      )}

      {/* + button and menu */}
      <div className="relative -ml-1.5">
        <button
          ref={plusBtnRef}
          type="button"
          onClick={() => setMenuOpen((v) => !v)}
          className={[
            'flex items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[background] duration-[60ms] hover:bg-[var(--c-bg-deep)] h-[33.5px] w-[33.5px]',
          ].join(' ')}
        >
          <Plus size={20} strokeWidth={1.5} />
        </button>

        {menuOpen && (
          <div
            ref={menuRef}
            className={`absolute left-0 z-50 ${variant === 'welcome' ? 'dropdown-menu' : 'dropdown-menu-up'}`}
            style={{
              ...(variant === 'welcome'
                ? { top: 'calc(100% + 8px)' }
                : { bottom: 'calc(100% + 8px)' }),
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              minWidth: '200px',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
              {isWorkMode ? (
                <>
                  <button
                    type="button"
                    onClick={() => { onFileInputClick(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                  >
                    <Paperclip size={14} style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }} />
                    {t.addFromLocal}
                  </button>
                  {(showPlanModeMenuItem || showLearningMode) && (
                    <>
                      <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                      {showPlanModeMenuItem && (
                        <button
                          type="button"
                          onClick={togglePlanMode}
                          className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                          style={{
                            color: planMode ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                            fontWeight: planMode ? 500 : 400,
                          }}
                        >
                          <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                            <ListTodo size={14} style={{ flexShrink: 0 }} />
                            {t.planMode}
                          </span>
                          {planMode && (
                            <Check size={13} style={{ color: '#4691F6', flexShrink: 0 }} />
                          )}
                        </button>
                      )}
                      {showLearningMode && (
                        <button
                          type="button"
                          onClick={toggleLearningMode}
                          disabled={learningModeDisabled}
                          className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                          style={{
                            color: learningModeEnabled ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                            fontWeight: learningModeEnabled ? 500 : 400,
                            opacity: learningModeDisabled ? 0.55 : undefined,
                          }}
                        >
                          <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                            <BookOpen size={14} style={{ flexShrink: 0 }} />
                            {t.learningMode}
                          </span>
                          {learningModeEnabled && (
                            <Check size={13} style={{ color: '#4691F6', flexShrink: 0 }} />
                          )}
                        </button>
                      )}
                    </>
                  )}
                </>
              ) : (
                <>
                  <button
                    type="button"
                    onClick={() => { onFileInputClick(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                  >
                    <Paperclip size={14} style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }} />
                    {t.addFromLocal}
                  </button>
                  {showLearningMode && (
                    <>
                      <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                      <button
                      type="button"
                      onClick={toggleLearningMode}
                      disabled={learningModeDisabled}
                      className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                      style={{
                        color: learningModeEnabled ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                        fontWeight: learningModeEnabled ? 500 : 400,
                        opacity: learningModeDisabled ? 0.55 : undefined,
                      }}
                    >
                      <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <BookOpen size={14} style={{ flexShrink: 0 }} />
                        {t.learningMode}
                      </span>
                      {learningModeEnabled && (
                        <Check size={13} style={{ color: '#4691F6', flexShrink: 0 }} />
                      )}
                    </button>
                    </>
                  )}
                </>
              )}
            </div>
          </div>
        )}
      </div>

      {/* active mode chip */}
      {showPlanMode && (
        <button
          type="button"
          onClick={togglePlanMode}
          className="plan-mode-chip"
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '2px',
            height: '33.5px',
            padding: '0 8px 0 9px',
            borderRadius: '8px',
            background: 'transparent',
            border: 'none',
            flexShrink: 0,
            cursor: 'pointer',
          }}
        >
          <span
            style={{
              width: '14px',
              height: '14px',
              display: 'grid',
              placeItems: 'center',
              flexShrink: 0,
            }}
          >
            <ListTodo className="plan-mode-icon plan-mode-icon-default" size={14} style={{ color: 'var(--c-plan-icon)' }} />
            <X className="plan-mode-icon plan-mode-icon-close" size={14} style={{ color: 'var(--c-plan-icon)' }} />
          </span>
          <span style={{
            fontSize: '13px',
            color: 'var(--c-plan-text)',
            fontWeight: 450,
            whiteSpace: 'nowrap',
            margin: '0 4px',
          }}>
            {t.planMode}
          </span>
        </button>
      )}

      {showLearningMode && learningModeEnabled && (
        <button
          type="button"
          onClick={toggleLearningMode}
          disabled={learningModeDisabled}
          className="learning-mode-chip"
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '2px',
            height: '33.5px',
            padding: '0 8px 0 9px',
            borderRadius: '8px',
            background: 'transparent',
            border: 'none',
            flexShrink: 0,
            cursor: 'pointer',
            opacity: learningModeDisabled ? 0.55 : undefined,
          }}
        >
          <span
            style={{
              width: '14px',
              height: '14px',
              display: 'grid',
              placeItems: 'center',
              flexShrink: 0,
            }}
          >
            <BookOpen className="learning-mode-icon learning-mode-icon-default" size={14} style={{ color: 'var(--c-learning-icon)' }} />
            <X className="learning-mode-icon learning-mode-icon-close" size={14} style={{ color: 'var(--c-learning-icon)' }} />
          </span>
          <span style={{
            fontSize: '13px',
            color: 'var(--c-learning-text)',
            fontWeight: 450,
            whiteSpace: 'nowrap',
            margin: '0 4px',
          }}>
            {t.learningMode}
          </span>
        </button>
      )}

      {/* model picker + spacer */}
      {!hideModelPicker && (
        <div style={{ marginLeft: 'auto', marginRight: '4px', display: 'flex', alignItems: 'center', gap: '14px', position: 'relative' }}>
          <ModelPicker
            accessToken={accessToken}
            value={selectedModel}
            onChange={onModelChange}
            onAddModel={() => onOpenSettings?.('models')}
            variant={variant}
            thinkingEnabled={thinkingEnabled}
            onThinkingChange={onThinkingChange}
            onOpenChange={onMenuOpenChange}
          />
        </div>
      )}
    </>
  )
}
