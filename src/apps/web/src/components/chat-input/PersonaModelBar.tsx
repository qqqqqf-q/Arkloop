import { useRef, useState, useCallback, useEffect } from 'react'
import { Plus, Paperclip, BookOpen, Search, Folder, FolderOpen, X, Check } from 'lucide-react'
import type { SelectablePersona } from '../../api'
import { ModelPicker } from '../ModelPicker'
import type { SettingsTab } from '../SettingsModal'
import { getDesktopApi, isDesktop } from '@arkloop/shared/desktop'
import {
  SEARCH_PERSONA_KEY,
  LEARNING_PERSONA_KEY,
  readWorkFolder,
  writeWorkFolder,
  clearWorkFolder,
  readWorkRecentFolders,
  writeThreadWorkFolder,
  clearThreadWorkFolder,
} from '../../storage'
import type { AppMode } from '../../storage'
import { useLocale } from '../../contexts/LocaleContext'

type Props = {
  personas: SelectablePersona[]
  selectedPersonaKey: string
  selectedModel: string | null
  chipExiting: boolean
  isNonDefaultMode: boolean
  selectedPersona: SelectablePersona | null
  onModeSelect: (personaKey: string) => void
  onDeactivateMode: () => void
  onModelChange: (model: string | null) => void
  thinkingEnabled: boolean
  onThinkingChange: (v: boolean) => void
  onOpenSettings?: (tab: SettingsTab) => void
  onFileInputClick: () => void
  accessToken?: string
  variant?: 'welcome' | 'chat'
  appMode?: AppMode
  threadHasMessages?: boolean
  workThreadId?: string
}

export function PersonaModelBar({
  personas,
  selectedPersonaKey,
  selectedModel,
  chipExiting,
  isNonDefaultMode,
  selectedPersona,
  onModeSelect,
  onDeactivateMode,
  onModelChange,
  thinkingEnabled,
  onThinkingChange,
  onOpenSettings,
  onFileInputClick,
  accessToken,
  variant,
  appMode,
  threadHasMessages,
  workThreadId,
}: Props) {
  const { t } = useLocale()
  const menuRef = useRef<HTMLDivElement>(null)
  const plusBtnRef = useRef<HTMLButtonElement>(null)
  const folderMenuRef = useRef<HTMLDivElement>(null)
  const folderBtnRef = useRef<HTMLButtonElement>(null)

  const [menuOpen, setMenuOpen] = useState(false)
  const [folderMenuOpen, setFolderMenuOpen] = useState(false)
  const [workFolder, setWorkFolder] = useState<string | null>(() => readWorkFolder())
  const [recentFolders, setRecentFolders] = useState<string[]>(() => readWorkRecentFolders())

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
    if (!folderMenuOpen) return
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (folderBtnRef.current?.contains(target)) return
      if (folderMenuRef.current && !folderMenuRef.current.contains(target)) {
        setFolderMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [folderMenuOpen])

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
      writeThreadWorkFolder(workThreadId, folder)
    } else {
      writeWorkFolder(folder)
    }
    setWorkFolder(folder)
    setRecentFolders(readWorkRecentFolders())
    setFolderMenuOpen(false)
  }, [workThreadId])

  return (
    <>
      {/* work folder picker -- desktop only, hidden once thread has messages */}
      {appMode === 'work' && isDesktop() && !threadHasMessages && (
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

          {folderMenuOpen && (
            <div
              ref={folderMenuRef}
              className={`absolute left-0 z-50 ${variant === 'welcome' ? 'dropdown-menu' : 'dropdown-menu-up'}`}
              style={{
                ...(variant === 'welcome'
                  ? { top: 'calc(100% + 8px)' }
                  : { bottom: 'calc(100% + 8px)' }),
                border: '0.5px solid var(--c-border-subtle)',
                borderRadius: '10px',
                padding: '4px',
                background: 'var(--c-bg-menu)',
                minWidth: '220px',
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
                          <Check size={12} style={{ flexShrink: 0, color: '#4691F6' }} />
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
                        clearThreadWorkFolder(workThreadId)
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
          )}
        </div>
      )}

      {/* + button and menu */}
      <div className="relative -ml-1.5">
        <button
          ref={plusBtnRef}
          type="button"
          onClick={() => setMenuOpen((v) => !v)}
          className="flex h-[33.5px] w-[33.5px] items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[background] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
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
              <button
                type="button"
                onClick={() => { onFileInputClick(); setMenuOpen(false) }}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
              >
                <Paperclip size={14} style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }} />
                {t.addFromLocal}
              </button>
              <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
              {personas.map((persona) => {
                const isActive = selectedPersonaKey === persona.persona_key
                const icon = persona.persona_key === LEARNING_PERSONA_KEY
                  ? <BookOpen size={14} style={{ flexShrink: 0 }} />
                  : persona.persona_key === SEARCH_PERSONA_KEY
                    ? <Search size={14} style={{ flexShrink: 0 }} />
                    : null
                return (
                  <button
                    key={persona.persona_key}
                    type="button"
                    onClick={() => { onModeSelect(persona.persona_key); setMenuOpen(false) }}
                    className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                    style={{
                      color: isActive ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                      fontWeight: isActive ? 500 : 400,
                    }}
                  >
                    <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      {icon}
                      {persona.selector_name}
                    </span>
                    {(isActive || (chipExiting && selectedPersonaKey === persona.persona_key)) && (
                      <Check size={13} style={{ color: '#4691F6', flexShrink: 0 }} />
                    )}
                  </button>
                )
              })}
            </div>
          </div>
        )}
      </div>

      {/* active mode chip */}
      {(isNonDefaultMode || chipExiting) && (
        <button
          type="button"
          onClick={onDeactivateMode}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '2px',
            height: '33.5px',
            padding: '0 8px 0 9px',
            borderRadius: '8px',
            background: 'var(--c-chip-active-bg)',
            border: '0.5px solid var(--c-border-subtle)',
            flexShrink: 0,
            marginLeft: '4px',
            cursor: 'pointer',
            animation: chipExiting
              ? 'chip-exit 0.12s cubic-bezier(0.4, 0, 1, 1) both'
              : 'chip-enter 0.14s cubic-bezier(0.16, 1, 0.3, 1) both',
          }}
        >
          {selectedPersonaKey === LEARNING_PERSONA_KEY && (
            <BookOpen size={12} style={{ color: 'var(--c-chip-active-text)', flexShrink: 0 }} />
          )}
          {selectedPersonaKey === SEARCH_PERSONA_KEY && (
            <Search size={12} style={{ color: '#4691F6', flexShrink: 0 }} />
          )}
          <span style={{
            fontSize: '13px',
            color: selectedPersonaKey === SEARCH_PERSONA_KEY ? '#4691F6' : 'var(--c-chip-active-text)',
            fontWeight: 450,
            whiteSpace: 'nowrap',
            margin: '0 4px',
          }}>
            {selectedPersona?.selector_name ?? selectedPersonaKey}
          </span>
          <X size={9} style={{ color: 'var(--c-chip-active-text)', opacity: 0.5, flexShrink: 0 }} />
        </button>
      )}

      {/* model picker + spacer */}
      <div style={{ marginLeft: 'auto', marginRight: '4px', display: 'flex', alignItems: 'center', gap: '14px', position: 'relative' }}>
        <ModelPicker
          accessToken={accessToken}
          value={selectedModel}
          onChange={onModelChange}
          onAddApiKey={() => onOpenSettings?.('models')}
          variant={variant}
          thinkingEnabled={thinkingEnabled}
          onThinkingChange={onThinkingChange}
        />
      </div>
    </>
  )
}
