import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown, Glasses, Pencil, Share2, Star, Trash2 } from 'lucide-react'
import { isDesktop } from '@arkloop/shared/desktop'
import { ConfirmDialog } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { useChatSession } from '../contexts/chat-session'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useMessageStore } from '../contexts/message-store'
import {
  useAppModeUI,
  useNotificationsUI,
  useTitleBarIncognitoUI,
} from '../contexts/app-ui'
import { usePanels } from '../contexts/panels'
import {
  starThread,
  unstarThread,
  updateThreadTitle,
  deleteThread,
  listStarredThreadIds,
} from '../api'
import { ModeSwitch } from './ModeSwitch'
import { NotificationBell } from './NotificationBell'

export function ChatTitleMenu() {
  const { threadId } = useChatSession()
  return <ChatTitleMenuContent key={threadId ?? '__no_thread__'} threadId={threadId} />
}

function ChatTitleMenuContent({ threadId }: { threadId: string | null }) {
  const { accessToken } = useAuth()
  const { t } = useLocale()
  const threadList = useThreadList()
  const msgs = useMessageStore()
  const { appMode, availableAppModes, setAppMode } = useAppModeUI()
  const { openNotifications, notificationVersion } = useNotificationsUI()
  const { setTitleBarIncognitoClick } = useTitleBarIncognitoUI()
  const panels = usePanels()

  const [titleMenuOpen, setTitleMenuOpen] = useState(false)
  const [titleMenuPos, setTitleMenuPos] = useState({ x: 0, y: 0 })
  const [starredIds, setStarredIds] = useState<string[]>([])
  const [editingTitle, setEditingTitle] = useState<string | null>(null)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const titleMenuRef = useRef<HTMLDivElement>(null)
  const titleContainerRef = useRef<HTMLDivElement>(null)
  const titleChevronRef = useRef<HTMLButtonElement>(null)
  const editTitleInputRef = useRef<HTMLInputElement>(null)
  const renameCancelledRef = useRef(false)

  const currentThread = threadId
    ? threadList.threads.find((th) => th.id === threadId)
    : undefined
  const currentTitle = currentThread
    ? ((currentThread.title ?? '').trim() || t.untitled)
    : null
  const effectiveAppMode = currentThread?.mode === 'work' ? 'work' : currentThread?.mode === 'chat' ? 'chat' : appMode
  const privateThreadIds = threadList.privateThreadIds

  // load starred ids
  useEffect(() => {
    listStarredThreadIds(accessToken)
      .then((ids) => setStarredIds(ids))
      .catch(() => {})
  }, [accessToken])

  // close menu on outside click
  useEffect(() => {
    if (!titleMenuOpen) return
    const handler = (e: MouseEvent) => {
      if (
        titleMenuRef.current && !titleMenuRef.current.contains(e.target as Node) &&
        titleContainerRef.current && !titleContainerRef.current.contains(e.target as Node)
      ) {
        setTitleMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [titleMenuOpen])

  // auto-focus rename input
  useEffect(() => {
    if (editingTitle !== null && editTitleInputRef.current) {
      editTitleInputRef.current.focus()
      editTitleInputRef.current.select()
    }
  }, [editingTitle])

  const openTitleMenu = useCallback(() => {
    if (titleChevronRef.current) {
      const rect = titleChevronRef.current.getBoundingClientRect()
      setTitleMenuPos({ x: rect.right, y: rect.bottom + 4 })
    }
    setTitleMenuOpen((prev) => !prev)
  }, [])

  const toggleStar = useCallback(() => {
    if (!threadId) return
    const wasStarred = starredIds.includes(threadId)
    setStarredIds((prev) =>
      wasStarred ? prev.filter((x) => x !== threadId) : [threadId, ...prev],
    )
    setTitleMenuOpen(false)
    const req = wasStarred
      ? unstarThread(accessToken, threadId)
      : starThread(accessToken, threadId)
    req.catch(() => {
      setStarredIds((prev) =>
        wasStarred ? [threadId, ...prev] : prev.filter((x) => x !== threadId),
      )
    })
  }, [accessToken, threadId, starredIds])

  const startRename = useCallback(() => {
    if (!currentThread) return
    setTitleMenuOpen(false)
    const title = (currentThread.title ?? '').trim()
    setEditingTitle(title || '')
  }, [currentThread])

  const commitRename = useCallback(async (newTitle: string) => {
    if (!threadId) return
    setEditingTitle(null)
    const trimmed = newTitle.trim()
    if (!trimmed) return
    try {
      await updateThreadTitle(accessToken, threadId, trimmed)
      threadList.updateTitle(threadId, trimmed)
    } catch {
      // ignore rename failure
    }
  }, [accessToken, threadId, threadList])

  const confirmDelete = useCallback(() => {
    setTitleMenuOpen(false)
    setDeleteConfirmOpen(true)
  }, [])

  const handleDeleteThread = useCallback(async () => {
    if (!threadId) return
    setDeleteConfirmOpen(false)
    try {
      await deleteThread(accessToken, threadId)
      threadList.removeThread(threadId)
    } catch {
      // ignore
    }
  }, [accessToken, threadId, threadList])

  const handleShareFromMenu = useCallback(() => {
    setTitleMenuOpen(false)
    panels.openShareModal()
  }, [panels])

  const pendingIncognito = msgs.pendingIncognito

  const handleIncognitoClick = useCallback(() => {
    if (!threadId) return
    if (privateThreadIds.has(threadId)) return
    if (pendingIncognito) {
      msgs.setPendingIncognito(false)
      return
    }
    if (msgs.messages.length > 0) {
      msgs.setPendingIncognito(true)
      return
    }
    threadList.togglePrivateMode()
  }, [threadId, privateThreadIds, pendingIncognito, msgs, threadList])

  useLayoutEffect(() => {
    if (!isDesktop()) return
    setTitleBarIncognitoClick(handleIncognitoClick)
    return () => { setTitleBarIncognitoClick(null) }
  }, [handleIncognitoClick, setTitleBarIncognitoClick])

  return (
    <>
      {/* header bar */}
      <div className="flex min-h-[60px] items-center justify-between gap-2 px-[15px] py-[8px]">
        {/* left: title */}
        <div className="flex min-w-0 flex-1 items-center pl-[5px]">
          {threadId && currentTitle && (
            editingTitle !== null ? (
              <input
                ref={editTitleInputRef}
                value={editingTitle}
                onChange={(e) => setEditingTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    renameCancelledRef.current = false
                    void commitRename(editingTitle)
                  } else if (e.key === 'Escape') {
                    renameCancelledRef.current = true
                    setEditingTitle(null)
                  }
                }}
                onBlur={() => {
                  if (!renameCancelledRef.current) {
                    void commitRename(editingTitle)
                  }
                  renameCancelledRef.current = false
                }}
                style={{
                  fontSize: '14px',
                  fontWeight: 450,
                  color: 'var(--c-text-primary)',
                  background: 'var(--c-bg-deep)',
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '8px',
                  padding: '5px 10px',
                  outline: 'none',
                  minWidth: 0,
                  maxWidth: '320px',
                  width: '100%',
                }}
              />
            ) : (
              <div
                ref={titleContainerRef}
                className="title-group flex items-stretch gap-[3px]"
                style={{ transform: 'translateY(-3px)' }}
              >
                <button
                  onClick={openTitleMenu}
                  className="title-part"
                  style={{
                    borderRadius: '7px 0 0 7px',
                    padding: '5px 10px',
                    fontSize: '14px',
                    fontWeight: 350,
                    maxWidth: '280px',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {currentTitle}
                </button>
                <button
                  ref={titleChevronRef}
                  onClick={openTitleMenu}
                  className="title-part"
                  style={{
                    borderRadius: '0 7px 7px 0',
                    padding: '5px 8px',
                    display: 'flex',
                    alignItems: 'center',
                  }}
                >
                  <ChevronDown size={14} style={{ flexShrink: 0 }} />
                </button>
              </div>
            )
          )}
        </div>

        {/* right: actions */}
        <div className="flex items-center gap-2">
          {!isDesktop() && (
            <ModeSwitch
              mode={effectiveAppMode}
              onChange={setAppMode}
              labels={{ chat: t.modeChat, work: t.modeWork }}
              availableModes={availableAppModes}
            />
          )}
          {threadId && privateThreadIds.has(threadId) && (
            <span className="text-xs font-medium text-[var(--c-text-muted)]">{t.incognitoLabel}</span>
          )}
          {!isDesktop() && (
            <NotificationBell
              accessToken={accessToken}
              onClick={openNotifications}
              refreshKey={notificationVersion}
              title={t.notificationsTitle}
            />
          )}
          {!isDesktop() && threadId && !privateThreadIds.has(threadId) && (
            <button
              onClick={() => panels.openShareModal()}
              title={t.shareTitle}
              className="flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Share2 size={18} />
            </button>
          )}
          {!isDesktop() && (
            <button
              onClick={
                threadId && privateThreadIds.has(threadId)
                  ? undefined
                  : handleIncognitoClick
              }
              title={threadId && privateThreadIds.has(threadId) ? t.thisThreadIsIncognito : t.toggleIncognito}
              className={[
                'flex h-8 w-8 items-center justify-center rounded-lg transition-colors',
                threadId && privateThreadIds.has(threadId) || pendingIncognito
                  ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
                threadId && privateThreadIds.has(threadId) ? 'cursor-default' : 'cursor-pointer',
              ].join(' ')}
            >
              <Glasses size={18} />
            </button>
          )}
        </div>
      </div>

      {/* title dropdown menu */}
      {titleMenuOpen && threadId && createPortal(
        <div
          ref={titleMenuRef}
          className="dropdown-menu"
          style={{
            position: 'fixed',
            right: `calc(100vw - ${titleMenuPos.x}px)`,
            top: titleMenuPos.y,
            zIndex: 9999,
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            minWidth: '140px',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
            <button
              onClick={startRename}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Pencil size={13} style={{ flexShrink: 0 }} />
              {t.renameThread}
            </button>
            <button
              onClick={toggleStar}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Star
                size={13}
                style={{
                  flexShrink: 0,
                  fill: starredIds.includes(threadId) ? 'var(--c-text-secondary)' : 'none',
                }}
              />
              {starredIds.includes(threadId) ? t.unstarThread : t.starThread}
            </button>
            {!isDesktop() && !privateThreadIds.has(threadId) && (
              <button
                onClick={handleShareFromMenu}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
              >
                <Share2 size={13} style={{ flexShrink: 0 }} />
                {t.shareThread}
              </button>
            )}
            <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 0' }} />
            <button
              onClick={confirmDelete}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[#ef4444] hover:bg-[rgba(239,68,68,0.08)] hover:text-[#f87171]"
            >
              <Trash2 size={13} style={{ flexShrink: 0 }} />
              {t.deleteThread}
            </button>
          </div>
        </div>,
        document.body,
      )}

      <ConfirmDialog
        open={deleteConfirmOpen}
        title={t.deleteThreadConfirmTitle}
        message={t.deleteThreadConfirmBody}
        confirmLabel={t.deleteThreadConfirm}
        cancelLabel={t.deleteThreadCancel}
        onClose={() => setDeleteConfirmOpen(false)}
        onConfirm={() => void handleDeleteThread()}
      />
    </>
  )
}
