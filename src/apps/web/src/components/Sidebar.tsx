import { memo, useState, useRef, useEffect, useCallback, useMemo, useLayoutEffect } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate, useParams, useLocation } from 'react-router-dom'
import {
  SquarePen,
  Search,
  Clock,
  PanelLeftClose,
  Bolt,
  Glasses,
  MoreHorizontal,
  Star,
  Share2,
  Pencil,
  Trash2,
} from 'lucide-react'
import type { ThreadResponse } from '../api'
import { listStarredThreadIds, starThread, unstarThread, updateThreadTitle, deleteThread } from '../api'
import { isLocalMode, isDesktop } from '@arkloop/shared/desktop'
import { useLocale } from '../contexts/LocaleContext'
import { ShareModal } from './ShareModal'
import { beginPerfTrace, endPerfTrace, isPerfDebugEnabled, recordPerfValue } from '../perfDebug'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useAppModeUI, useSearchUI, useSettingsUI, useSidebarUI } from '../contexts/app-ui'

type Props = {
  threads: ThreadResponse[]
  onNewThread: () => void
  onThreadDeleted: (threadId: string) => void
  /** 点到历史会话时先收起设置等全屏层；否则同 URL 的 navigate 不会触发，桌面端无法回到聊天 */
  beforeNavigateToThread?: () => void
}

function threadTitle(thread: ThreadResponse, untitled: string): string {
  const title = (thread.title ?? '').trim()
  return title.length > 0 ? title : untitled
}

type SidebarThreadListProps = {
  starredThreads: ThreadResponse[]
  regularThreads: ThreadResponse[]
  starredSet: Set<string>
  runningThreadIds: Set<string>
  menuThreadId: string | null
  editingThreadId: string | null
  editingTitle: string
  activeThreadId?: string
  untitled: string
  editInputRef: React.RefObject<HTMLInputElement | null>
  setEditingTitle: React.Dispatch<React.SetStateAction<string>>
  setEditingThreadId: React.Dispatch<React.SetStateAction<string | null>>
  commitRename: (id: string, newTitle: string) => void
  beforeNavigateToThread?: () => void
  navigate: ReturnType<typeof useNavigate>
  openMenu: (event: React.MouseEvent, id: string) => void
}

const SidebarThreadList = memo(function SidebarThreadList({
  starredThreads,
  regularThreads,
  starredSet,
  runningThreadIds,
  menuThreadId,
  editingThreadId,
  editingTitle,
  activeThreadId,
  untitled,
  editInputRef,
  setEditingTitle,
  setEditingThreadId,
  commitRename,
  beforeNavigateToThread,
  navigate,
  openMenu,
}: SidebarThreadListProps) {
  useEffect(() => {
    if (!isPerfDebugEnabled()) return
    recordPerfValue('sidebar_thread_list_render_count', 1, 'count', {
      starredCount: starredThreads.length,
      regularCount: regularThreads.length,
      runningCount: runningThreadIds.size,
      activeThreadId: activeThreadId ?? null,
    })
  })

  const renderThread = (thread: ThreadResponse, section: 'starred' | 'regular') => {
    const isRunning = runningThreadIds.has(thread.id)
    const isMenuOpen = menuThreadId === thread.id
    const isEditing = editingThreadId === thread.id
    return (
      <div
        key={`${thread.id}-${section}`}
        className={[
          'group relative flex w-full items-center rounded-[6px]',
          thread.id === activeThreadId || isMenuOpen
            ? 'bg-[var(--c-bg-deep)]'
            : 'hover:bg-[var(--c-bg-deep)]',
        ].join(' ')}
      >
        {isEditing ? (
          <input
            ref={editInputRef}
            value={editingTitle}
            onChange={(e) => setEditingTitle(e.target.value)}
            onBlur={() => commitRename(thread.id, editingTitle)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                commitRename(thread.id, editingTitle)
              } else if (e.key === 'Escape') {
                setEditingThreadId(null)
              }
            }}
            className="min-w-0 flex-1 bg-transparent px-2 py-[7px] text-[13px] text-[var(--c-text-primary)] outline-none"
            style={{ border: 'none', fontWeight: 'var(--c-sidebar-thread-weight)' }}
            maxLength={200}
          />
        ) : (
          <button
            onClick={() => {
              beforeNavigateToThread?.()
              navigate(`/t/${thread.id}`)
            }}
            className={[
              'flex min-w-0 flex-1 items-center gap-2 px-2 py-[7px] text-left text-[14px] group-hover:text-[var(--c-text-primary)]',
              thread.id === activeThreadId
                ? 'text-[var(--c-text-primary)]'
                : 'text-[var(--c-text-secondary)]',
            ].join(' ')}
            style={{ fontWeight: 'var(--c-sidebar-thread-weight)' }}
          >
            {starredSet.has(thread.id) && (
              <Star size={11} className="shrink-0 fill-[var(--c-text-muted)] text-[var(--c-text-muted)] opacity-70" />
            )}
            <span className="min-w-0 flex-1 truncate">{threadTitle(thread, untitled)}</span>
          </button>
        )}

        {!isEditing && (
          <div className="mr-1 flex shrink-0 items-center">
            {isRunning && (
              <span className="mr-1 h-3 w-3 shrink-0 animate-spin rounded-full border border-[var(--c-text-muted)] border-t-transparent" />
            )}
            <div
              className={[
                'shrink-0',
                isRunning
                  ? `overflow-hidden transition-[width] duration-150 ${isMenuOpen ? 'w-6' : 'w-0 group-hover:w-6'}`
                  : 'w-6',
              ].join(' ')}
            >
              <button
                data-menu-button={thread.id}
                onClick={(e) => openMenu(e, thread.id)}
                className={[
                  'flex h-6 w-6 shrink-0 items-center justify-center rounded-md transition-transform duration-[80ms] active:scale-[0.96]',
                  isMenuOpen
                    ? 'opacity-100 bg-[var(--c-sidebar-btn-hover)] text-[var(--c-text-primary)]'
                    : 'opacity-0 group-hover:opacity-100 text-[var(--c-text-muted)] hover:bg-[var(--c-sidebar-btn-hover)] hover:text-[var(--c-text-primary)]',
                ].join(' ')}
              >
                <MoreHorizontal size={14} />
              </button>
            </div>
          </div>
        )}
      </div>
    )
  }

  return (
    <>
      {starredThreads.map((thread) => renderThread(thread, 'starred'))}
      {starredThreads.length > 0 && regularThreads.length > 0 && (
        <div className="my-1 mx-2 h-px bg-[var(--c-border-subtle)]" />
      )}
      {regularThreads.map((thread) => renderThread(thread, 'regular'))}
    </>
  )
})

export function Sidebar({
  threads,
  onNewThread,
  onThreadDeleted,
  beforeNavigateToThread,
}: Props) {
  const { me, accessToken } = useAuth()
  const { runningThreadIds, isPrivateMode, pendingIncognitoMode, updateTitle: onThreadTitleUpdated } = useThreadList()
  const { sidebarCollapsed: collapsed, toggleSidebar: onToggleCollapse, rightPanelOpen: narrow } = useSidebarUI()
  const { openSearchOverlay: onOpenSearchOverlay } = useSearchUI()
  const { settingsOpen: suppressActiveThreadHighlight, openSettings: onOpenSettings } = useSettingsUI()
  const { appMode } = useAppModeUI()
  const desktopMode = isDesktop()
  const isPrivateModeEffective = isPrivateMode || pendingIncognitoMode
  const isWorkMode = appMode === 'work'
  const navigate = useNavigate()
  const location = useLocation()
  const { threadId } = useParams<{ threadId: string }>()
  const activeThreadId = suppressActiveThreadHighlight ? undefined : threadId
  const { t } = useLocale()

  const [starredIds, setStarredIds] = useState<string[]>([])
  const [menuThreadId, setMenuThreadId] = useState<string | null>(null)
  const [shareModalThreadId, setShareModalThreadId] = useState<string | null>(null)
  const [menuPos, setMenuPos] = useState<{ x: number; y: number }>({ x: 0, y: 0 })
  const menuRef = useRef<HTMLDivElement>(null)
  const asideRef = useRef<HTMLElement>(null)
  const toggleStartedRef = useRef<{ startedAt: number; sample?: Record<string, string | number | boolean | null | undefined> } | null>(null)
  const toggleCommittedAtRef = useRef<number | null>(null)
  const [editingThreadId, setEditingThreadId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState<string>('')
  const editInputRef = useRef<HTMLInputElement>(null)
  const [deleteConfirmThreadId, setDeleteConfirmThreadId] = useState<string | null>(null)
  const settingsPointerTraceRef = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const collapsePointerTraceRef = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const searchPointerTraceRef = useRef<ReturnType<typeof beginPerfTrace>>(null)
  const { starredSet, starredThreads, regularThreads } = useMemo(() => {
    const nextStarredSet = new Set(starredIds)
    const threadsById = new Map(threads.map((thread) => [thread.id, thread] as const))
    const next = {
      starredSet: nextStarredSet,
      starredThreads: starredIds
        .map((id) => threadsById.get(id))
        .filter((t): t is ThreadResponse => t !== undefined),
      regularThreads: threads.filter((t) => !nextStarredSet.has(t.id)),
    }
    return next
  }, [appMode, starredIds, threads])

  // 初始化时从服务端拉取收藏列表
  useEffect(() => {
    listStarredThreadIds(accessToken)
      .then((ids) => setStarredIds(ids))
      .catch(() => {})
  }, [accessToken])

  const toggleStar = useCallback((id: string) => {
    const wasStarred = starredIds.includes(id)
    // 乐观更新：新收藏插到最前，取消收藏直接移除
    setStarredIds((prev) =>
      wasStarred ? prev.filter((x) => x !== id) : [id, ...prev.filter((x) => x !== id)]
    )
    setMenuThreadId(null)
    // API 调用失败时回滚
    const req = wasStarred ? unstarThread(accessToken, id) : starThread(accessToken, id)
    req.catch(() => {
      setStarredIds((prev) =>
        wasStarred ? [id, ...prev.filter((x) => x !== id)] : prev.filter((x) => x !== id)
      )
    })
  }, [accessToken, starredIds])

  const openMenu = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    setMenuPos({ x: rect.right, y: rect.bottom + 4 })
    setMenuThreadId((prev) => (prev === id ? null : id))
  }, [])

  const startRename = useCallback((id: string, currentTitle: string) => {
    setMenuThreadId(null)
    setEditingThreadId(id)
    setEditingTitle(currentTitle)
  }, [])

  const commitRename = useCallback(async (id: string, newTitle: string) => {
    const trimmed = newTitle.trim()
    setEditingThreadId(null)
    if (!trimmed) return
    try {
      await updateThreadTitle(accessToken, id, trimmed)
      onThreadTitleUpdated(id, trimmed)
    } catch {
      // 失败静默，保持旧标题
    }
  }, [accessToken, onThreadTitleUpdated])

  const handleDelete = useCallback(async (id: string) => {
    setDeleteConfirmThreadId(null)
    try {
      await deleteThread(accessToken, id)
      onThreadDeleted(id)
    } catch {
      // 失败静默
    }
  }, [accessToken, onThreadDeleted])

  // 进入编辑模式后自动聚焦 input
  useEffect(() => {
    if (editingThreadId && editInputRef.current) {
      editInputRef.current.focus()
      editInputRef.current.select()
    }
  }, [editingThreadId])

  // 点击外部关闭菜单（排除触发按钮本身，否则 mousedown 会先关闭再被 click 重新打开）
  useEffect(() => {
    if (!menuThreadId) return
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (target.closest('[data-menu-button]')) return
      if (menuRef.current && !menuRef.current.contains(target)) {
        setMenuThreadId(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menuThreadId])

  // deleteConfirm 时 Escape 关闭
  useEffect(() => {
    if (!deleteConfirmThreadId) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') setDeleteConfirmThreadId(null) }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [deleteConfirmThreadId])

  useEffect(() => {
    if (!isPerfDebugEnabled()) return
    recordPerfValue('sidebar_render_count', 1, 'count', {
      collapsed,
      desktopMode: !!desktopMode,
      narrow: !!narrow,
      isPrivateMode: isPrivateModeEffective,
      threadCount: threads.length,
      starredCount: starredIds.length,
      runningCount: runningThreadIds.size,
      menuOpen: menuThreadId !== null,
      editing: editingThreadId !== null,
      deleting: deleteConfirmThreadId !== null,
      appMode: appMode ?? 'chat',
      pathname: location.pathname,
    })
    recordPerfValue('sidebar_thread_partition_count', 1, 'count', {
      collapsed,
      threadCount: threads.length,
      starredCount: starredIds.length,
      starredResolvedCount: starredThreads.length,
      regularCount: regularThreads.length,
      runningCount: runningThreadIds.size,
      appMode: appMode ?? 'chat',
    })
  })

  useEffect(() => {
    if (!isPerfDebugEnabled()) return
    const startedAt = performance.now()
    const timer = window.setTimeout(() => {
      recordPerfValue('sidebar_collapse_animation', performance.now() - startedAt, 'ms', {
        collapsed,
        threadCount: threads.length,
      })
    }, 280)
    return () => window.clearTimeout(timer)
  }, [collapsed, threads.length])

  useEffect(() => {
    const handleToggleStarted = (event: Event) => {
      const detail = (event as CustomEvent<{ startedAt: number; sample?: Record<string, string | number | boolean | null | undefined> }>).detail
      if (!detail || typeof detail.startedAt !== 'number') return
      toggleStartedRef.current = detail
      toggleCommittedAtRef.current = null
    }
    window.addEventListener('arkloop:sidebar-toggle-started', handleToggleStarted as EventListener)
    return () => window.removeEventListener('arkloop:sidebar-toggle-started', handleToggleStarted as EventListener)
  }, [])

  useLayoutEffect(() => {
    const current = toggleStartedRef.current
    if (!current || !isPerfDebugEnabled() || typeof performance === 'undefined') return
    const committedAt = performance.now()
    toggleCommittedAtRef.current = committedAt
    recordPerfValue('sidebar_component_commit', committedAt - current.startedAt, 'ms', {
      ...current.sample,
      threadCount: threads.length,
      pathname: location.pathname,
    })
    const frameId = requestAnimationFrame(() => {
      recordPerfValue('sidebar_component_first_frame', performance.now() - current.startedAt, 'ms', {
        ...current.sample,
        threadCount: threads.length,
        pathname: location.pathname,
      })
    })
    return () => cancelAnimationFrame(frameId)
  }, [collapsed, threads.length, location.pathname])

  useEffect(() => {
    const aside = asideRef.current
    if (!aside || !isPerfDebugEnabled()) return
    const handleTransitionStart = (event: TransitionEvent) => {
      if (event.propertyName !== 'width') return
      const current = toggleStartedRef.current
      if (!current || typeof performance === 'undefined') return
      recordPerfValue('sidebar_collapse_transition_start_delay', performance.now() - current.startedAt, 'ms', {
        ...current.sample,
        threadCount: threads.length,
        pathname: location.pathname,
      })
      if (toggleCommittedAtRef.current !== null) {
        recordPerfValue('sidebar_commit_to_transition_start_gap', performance.now() - toggleCommittedAtRef.current, 'ms', {
          ...current.sample,
          threadCount: threads.length,
          pathname: location.pathname,
        })
      }
      toggleStartedRef.current = null
      toggleCommittedAtRef.current = null
    }
    aside.addEventListener('transitionstart', handleTransitionStart)
    return () => aside.removeEventListener('transitionstart', handleTransitionStart)
  }, [threads.length, location.pathname])

  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'
  const recordSearchOpenStart = useCallback(() => {
    if (!isPerfDebugEnabled() || typeof performance === 'undefined') return
    const sample = {
      source: 'sidebar',
      collapsed,
      threadCount: threads.length,
      appMode: appMode ?? 'chat',
      pathname: location.pathname,
    }
    ;(window as Window & {
      __arkloopSearchOpenStarted?: {
        startedAt: number
        sample: Record<string, string | number | boolean | null | undefined>
      }
    }).__arkloopSearchOpenStarted = {
      startedAt: performance.now(),
      sample,
    }
    recordPerfValue('desktop_search_open_request', 0, 'ms', sample)
  }, [appMode, collapsed, location.pathname, threads.length])
  const menuPortal = menuThreadId !== null ? createPortal(
    <div
      ref={menuRef}
      style={{
        position: 'fixed',
        right: `calc(100vw - ${menuPos.x}px)`,
        top: menuPos.y,
        zIndex: 9999,
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        padding: '4px',
        background: 'var(--c-bg-menu)',
        minWidth: '120px',
        boxShadow: 'var(--c-dropdown-shadow)',
      }}
      className="dropdown-menu"
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
        <button
          onClick={() => {
            const thread = threads.find((th) => th.id === menuThreadId)
            const currentTitle = thread ? threadTitle(thread, t.untitled) : ''
            startRename(menuThreadId, currentTitle === t.untitled ? '' : currentTitle)
          }}
          className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <Pencil size={13} style={{ flexShrink: 0 }} />
          {t.renameThread}
        </button>
        <button
          onClick={() => toggleStar(menuThreadId)}
          className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <Star
            size={13}
            style={{
              flexShrink: 0,
              color: 'var(--c-text-secondary)',
              fill: starredIds.includes(menuThreadId) ? 'var(--c-text-secondary)' : 'none',
            }}
          />
          {starredIds.includes(menuThreadId) ? t.unstarThread : t.starThread}
        </button>
        {!isDesktop() && (
          <button
            onClick={() => {
              const id = menuThreadId
              setMenuThreadId(null)
              setShareModalThreadId(id)
            }}
            className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          >
            <Share2 size={13} style={{ flexShrink: 0 }} />
            {t.shareThread}
          </button>
        )}
        <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 0' }} />
        <button
          onClick={() => {
            const id = menuThreadId
            setMenuThreadId(null)
            setDeleteConfirmThreadId(id)
          }}
          className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[#ef4444] hover:bg-[rgba(239,68,68,0.08)] hover:text-[#f87171]"
        >
          <Trash2 size={13} style={{ flexShrink: 0 }} />
          {t.deleteThread}
        </button>
      </div>
    </div>,
    document.body,
  ) : null
  const shareModal = shareModalThreadId ? (
    <ShareModal
      accessToken={accessToken}
      threadId={shareModalThreadId}
      open={shareModalThreadId !== null}
      onClose={() => setShareModalThreadId(null)}
    />
  ) : null
  const deleteConfirmPortal = deleteConfirmThreadId !== null ? createPortal(
    <div
      className="overlay-fade-in fixed inset-0 flex items-center justify-center"
      style={{ zIndex: 10000, background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
      onClick={(e) => { if (e.target === e.currentTarget) setDeleteConfirmThreadId(null) }}
    >
      <div
        className="modal-enter"
        style={{
          background: 'var(--c-bg-page)',
          border: '0.5px solid var(--c-border-subtle)',
          borderRadius: '16px',
          padding: '24px',
          width: '320px',
          boxShadow: 'var(--c-dropdown-shadow)',
        }}
      >
        <p style={{ fontSize: '15px', fontWeight: 600, color: 'var(--c-text-primary)', marginBottom: '8px' }}>
          {t.deleteThreadConfirmTitle}
        </p>
        <p style={{ fontSize: '13px', color: 'var(--c-text-secondary)', lineHeight: 1.55, marginBottom: '20px' }}>
          {t.deleteThreadConfirmBody}
        </p>
        <div style={{ display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
          <button
            onClick={() => setDeleteConfirmThreadId(null)}
            className="hover:bg-[var(--c-bg-deep)]"
            style={{
              padding: '7px 16px',
              borderRadius: '8px',
              fontSize: '13px',
              fontWeight: 500,
              color: 'var(--c-text-secondary)',
              background: 'transparent',
              border: '0.5px solid var(--c-border-subtle)',
              cursor: 'pointer',
            }}
          >
            {t.deleteThreadCancel}
          </button>
          <button
            onClick={() => handleDelete(deleteConfirmThreadId)}
            className="hover:opacity-85 active:opacity-70"
            style={{
              padding: '7px 16px',
              borderRadius: '8px',
              fontSize: '13px',
              fontWeight: 500,
              color: '#fff',
              background: '#ef4444',
              border: 'none',
              cursor: 'pointer',
            }}
          >
            {t.deleteThreadConfirm}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  ) : null

  return (
    <>
      <aside
      ref={asideRef}
      className={[
        'flex h-full shrink-0 flex-col overflow-hidden bg-[var(--c-bg-sidebar)]',
        collapsed ? 'w-[48px]' : narrow ? 'w-[224px]' : desktopMode ? 'w-[284px]' : 'w-[304px]',
      ].join(' ')}
      style={{
        transition: 'width 280ms cubic-bezier(0.16,1,0.3,1)',
        willChange: 'width',
        borderRight: '0.5px solid var(--c-border)',
      }}
    >
      {/* Desktop title bar spacer */}
      {desktopMode && <div className="h-3" />}

      {/* Non-desktop title bar or spacer */}
      {!desktopMode && (
        collapsed ? (
          <div className="h-3" />
        ) : (
          <div className="flex min-h-[56px] items-center justify-between px-4 py-3">
            <div className="flex items-center gap-2 overflow-hidden">
              <h1 className="text-[16px] font-semibold tracking-tight text-[var(--c-text-primary)] shrink-0">Arkloop</h1>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  opacity: isPrivateModeEffective ? 1 : 0,
                  transform: isPrivateModeEffective ? 'translateX(0)' : 'translateX(14px)',
                  transition: 'opacity 0.18s ease, transform 0.18s ease',
                  pointerEvents: 'none',
                }}
              >
                <span className="h-[5px] w-[5px] shrink-0 rounded-full bg-[var(--c-text-tertiary)]" style={{ opacity: 0.5 }} />
                <span className="text-[12px] font-medium text-[var(--c-text-tertiary)] whitespace-nowrap">{t.incognitoMode}</span>
              </div>
            </div>
            <button
              onClick={() => {
                endPerfTrace(collapsePointerTraceRef.current, {
                  phase: 'click',
                  collapsed,
                  threadCount: threads.length,
                  starredCount: starredIds.length,
                })
                collapsePointerTraceRef.current = null
                onToggleCollapse('sidebar')
              }}
              onPointerDown={() => {
                collapsePointerTraceRef.current = beginPerfTrace('sidebar_collapse_interaction', {
                  phase: 'pointerdown',
                  collapsed,
                  threadCount: threads.length,
                  starredCount: starredIds.length,
                  runningCount: runningThreadIds.size,
                  appMode: appMode ?? 'chat',
                  pathname: location.pathname,
                })
              }}
              onPointerLeave={() => {
                collapsePointerTraceRef.current = null
              }}
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[var(--c-text-secondary)] transition-[background-color,color,transform] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)] active:scale-[0.96]"
            >
              <PanelLeftClose size={17} />
            </button>
          </div>
        )
      )}

      {/* Nav buttons — always rendered, text clips when sidebar narrows */}
      <nav className="flex flex-col gap-px px-2 pt-1">
        <button
          onClick={onNewThread}
          className="group flex h-9 items-center gap-2.5 overflow-hidden whitespace-nowrap rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-[background-color,color] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          style={{ fontWeight: 'var(--c-sidebar-nav-weight)' }}
        >
          <SquarePen size={16} className="shrink-0 transition-transform duration-100 group-hover:scale-[1.05]" />
          <span style={{ overflow: 'hidden', maxWidth: collapsed ? 0 : '200px', opacity: collapsed ? 0 : 1, transition: 'max-width 280ms cubic-bezier(0.16,1,0.3,1), opacity 150ms ease', whiteSpace: 'nowrap' }}>{isWorkMode ? t.newTask : t.newChat}</span>
        </button>

        <button
          onClick={() => {
            endPerfTrace(searchPointerTraceRef.current, {
              phase: 'click',
              collapsed,
              threadCount: threads.length,
              appMode: appMode ?? 'chat',
              pathname: location.pathname,
            })
            searchPointerTraceRef.current = null
            recordSearchOpenStart()
            onOpenSearchOverlay()
          }}
          onPointerDown={() => {
            searchPointerTraceRef.current = beginPerfTrace('sidebar_search_interaction', {
              phase: 'pointerdown',
              collapsed,
              threadCount: threads.length,
              appMode: appMode ?? 'chat',
              pathname: location.pathname,
            })
          }}
          onPointerLeave={() => {
            searchPointerTraceRef.current = null
          }}
          className="group flex h-9 items-center gap-2.5 overflow-hidden whitespace-nowrap rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-[background-color,color] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          style={{ fontWeight: 'var(--c-sidebar-nav-weight)' }}
        >
          <Search size={16} className="shrink-0 transition-transform duration-100 group-hover:scale-[1.05]" />
          <span style={{ overflow: 'hidden', maxWidth: collapsed ? 0 : '200px', opacity: collapsed ? 0 : 1, transition: 'max-width 280ms cubic-bezier(0.16,1,0.3,1), opacity 150ms ease', whiteSpace: 'nowrap' }}>{isWorkMode ? t.searchTasks : t.searchChats}</span>
        </button>

        <button
          onClick={() => navigate('/scheduled-jobs')}
          className="group flex h-9 items-center gap-2.5 overflow-hidden whitespace-nowrap rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-[background-color,color] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          style={{ fontWeight: 'var(--c-sidebar-nav-weight)' }}
        >
          <Clock size={16} className="shrink-0 transition-transform duration-100 group-hover:scale-[1.05]" />
          <span style={{ overflow: 'hidden', maxWidth: collapsed ? 0 : '200px', opacity: collapsed ? 0 : 1, transition: 'max-width 280ms cubic-bezier(0.16,1,0.3,1), opacity 150ms ease', whiteSpace: 'nowrap' }}>{t.scheduledJobs}</span>
        </button>

      </nav>

      {/* Thread list — hidden when collapsed */}
      <div
        className={[
          'mt-6 flex min-h-0 flex-1 flex-col overflow-y-auto px-2',
          collapsed ? 'opacity-0' : 'opacity-100',
        ].join(' ')}
        style={{ transition: 'opacity 150ms ease' }}
        inert={collapsed || undefined}
      >
          <div className="mb-[12px] mt-1 flex shrink-0 items-center gap-2 px-2">
            <h3
              className="text-[11px] tracking-[0.3px] text-[var(--c-text-tertiary)]"
              style={{ fontWeight: 'var(--c-sidebar-section-weight)' }}
            >
              {t.recents}
            </h3>
          </div>
          <div className="flex flex-col gap-[2px]">
            {/* incognito placeholder */}
            <div
              style={{
                display: 'grid',
                gridTemplateRows: isPrivateModeEffective ? '1fr' : '0fr',
                opacity: isPrivateModeEffective ? 1 : 0,
                overflow: 'hidden',
                transition: 'grid-template-rows 0.15s ease, opacity 0.12s ease',
              }}
            >
              <div style={{ minHeight: 0 }}>
                <div
                  className="flex items-center gap-2 rounded-lg px-3 py-2.5"
                  style={{
                    border: '1px dashed var(--c-border-subtle)',
                    color: 'var(--c-text-muted)',
                  }}
                >
                  <Glasses size={14} strokeWidth={1.5} style={{ opacity: 0.6, flexShrink: 0 }} />
                  <p className="text-[12px] leading-snug">{t.incognitoHistoryNote}</p>
                </div>
              </div>
            </div>

            <div
              key={appMode}
              className="flex w-full flex-col gap-[2px]"
              style={{
                opacity: isPrivateModeEffective ? 0 : 1,
                transition: 'opacity 0.15s ease',
                pointerEvents: isPrivateModeEffective ? 'none' : 'auto',
              }}
            >
              {threads.length === 0 ? (
                <p className="overflow-hidden whitespace-nowrap px-2 py-1 text-[12px] text-[var(--c-text-muted)]">{t.recentsEmpty}</p>
              ) : (
                <SidebarThreadList
                  starredThreads={starredThreads}
                  regularThreads={regularThreads}
                  starredSet={starredSet}
                  runningThreadIds={runningThreadIds}
                  menuThreadId={menuThreadId}
                  editingThreadId={editingThreadId}
                  editingTitle={editingTitle}
                  activeThreadId={activeThreadId}
                  untitled={t.untitled}
                  editInputRef={editInputRef}
                  setEditingTitle={setEditingTitle}
                  setEditingThreadId={setEditingThreadId}
                  commitRename={commitRename}
                  beforeNavigateToThread={beforeNavigateToThread}
                  navigate={navigate}
                  openMenu={openMenu}
                />
              )}
            </div>
          </div>
        </div>

      {/* Bottom area */}
      <div
        className="mt-auto px-2 pb-2 pt-1"
        style={{
          borderTop: '1px solid var(--c-border)',
          borderTopColor: collapsed ? 'transparent' : 'var(--c-border)',
          transition: 'border-top-color 280ms cubic-bezier(0.16,1,0.3,1)',
        }}
      >
        {!collapsed && !isLocalMode() && (
          <button
            onClick={() => onOpenSettings('account')}
            className="flex w-full items-center gap-3 rounded-xl px-3 py-[10px] transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div
              className="flex h-[39px] w-[39px] shrink-0 items-center justify-center rounded-full text-[15px] font-medium"
              style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
            >
              {userInitial}
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-[2px] text-left">
              <div className="truncate text-sm font-medium text-[var(--c-text-secondary)]">
                {me?.username ?? t.loading}
              </div>
              <div className="text-xs font-normal text-[var(--c-text-tertiary)]">
                {t.enterprisePlan}
              </div>
            </div>
          </button>
        )}

        {/* Settings button: fixed pl-1 so the icon x-position never
            changes during sidebar collapse/expand — no justifyContent flip. */}
        <div className="mt-0.5 pl-1">
          <button
            onClick={() => {
              endPerfTrace(settingsPointerTraceRef.current, {
                phase: 'click',
                collapsed,
                threadCount: threads.length,
                starredCount: starredIds.length,
                runningCount: runningThreadIds.size,
                appMode: appMode ?? 'chat',
                pathname: location.pathname,
              })
              settingsPointerTraceRef.current = null
              onOpenSettings('settings')
            }}
            onPointerDown={() => {
              settingsPointerTraceRef.current = beginPerfTrace('sidebar_settings_interaction', {
                phase: 'pointerdown',
                collapsed,
                threadCount: threads.length,
                starredCount: starredIds.length,
                runningCount: runningThreadIds.size,
                appMode: appMode ?? 'chat',
                pathname: location.pathname,
              })
            }}
            onPointerLeave={() => {
              settingsPointerTraceRef.current = null
            }}
            className="flex h-8 w-8 items-center justify-center rounded-md text-[var(--c-text-icon)] transition-[background-color,color,transform] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)] active:scale-[0.96]"
          >
            <Bolt size={18} />
          </button>
        </div>
      </div>

      </aside>

      {menuPortal}
      {shareModal}
      {deleteConfirmPortal}
    </>
  )
}
