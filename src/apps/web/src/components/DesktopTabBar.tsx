import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { MoreHorizontal, Pencil, Star, Trash2, PanelRightOpen } from 'lucide-react'
import { ConfirmDialog } from '@arkloop/shared'
import { isDesktop } from '@arkloop/shared/desktop'
import { useLocale } from '../contexts/LocaleContext'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import {
  starThread,
  unstarThread,
  updateThreadTitle,
  deleteThread,
  listStarredThreadIds,
} from '../api'
import type { ThreadResponse } from '../api'
import type { AppMode } from '../storage'

type Props = {
  appMode: AppMode
  availableModes: AppMode[]
  browserPanelOpen?: boolean
  onSetAppMode: (mode: AppMode) => void
  onToggleBrowserPanel?: () => void
  currentThread?: ThreadResponse | null
}

export function DesktopTabBar({
  browserPanelOpen = false,
  onToggleBrowserPanel,
  currentThread,
}: Props) {
  const { t } = useLocale()
  const { accessToken } = useAuth()
  const threadList = useThreadList()
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

  const threadId = currentThread?.id ?? null
  const currentTitle = currentThread
    ? ((currentThread.title ?? '').trim() || t.untitled)
    : null

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

  return (
    <>
      <div
        className="flex h-12 shrink-0 items-center gap-2 px-3"
        style={{
          borderBottom: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-page)',
        }}
      >
        <div className="flex min-w-0 flex-1 items-center">
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
                className="flex items-center gap-1"
              >
                <button
                  ref={titleChevronRef}
                  onClick={openTitleMenu}
                  className="flex items-center gap-1 rounded-lg px-2 py-1 text-sm font-medium text-[var(--c-text-primary)] hover:bg-[var(--c-bg-deep)] transition-colors"
                  style={{ maxWidth: '280px' }}
                >
                  <span className="truncate">{currentTitle}</span>
                  <MoreHorizontal size={14} className="shrink-0" />
                </button>
              </div>
            )
          )}
        </div>
        {isDesktop() && !browserPanelOpen && (
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
