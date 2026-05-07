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
  Archive,
  Pin,
  Inbox,
  CheckCircle,
  ChevronRight,
  Plus,
} from 'lucide-react'
import type { ThreadGtdBucket, ThreadResponse, UpdateThreadSidebarRequest } from '../api'
import { listStarredThreadIds, starThread, unstarThread, updateThreadTitle, deleteThread, updateThreadSidebarState } from '../api'
import { isLocalMode, isDesktop } from '@arkloop/shared/desktop'
import { ConfirmDialog } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { ShareModal } from './ShareModal'
import { beginPerfTrace, endPerfTrace, isPerfDebugEnabled, recordPerfValue } from '../perfDebug'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useAppModeUI, useSearchUI, useSettingsUI, useSidebarUI } from '../contexts/app-ui'
import {
  readGtdInboxThreadIds, writeGtdInboxThreadIds,
  readGtdTodoThreadIds, writeGtdTodoThreadIds,
  readGtdWaitingThreadIds, writeGtdWaitingThreadIds,
  readGtdSomedayThreadIds, writeGtdSomedayThreadIds,
  readGtdArchivedThreadIds, writeGtdArchivedThreadIds,
  readPinnedThreadIds, writePinnedThreadIds,
  readGtdEnabled, readExpandedProjectPaths, writeExpandedProjectPaths,
  clearThreadWorkFolder, readThreadWorkFolder, writeThreadWorkFolder, clearWorkFolder, writeWorkFolder,
} from '../storage'

type Props = {
  threads: ThreadResponse[]
  onNewThread: () => void
  onThreadDeleted: (threadId: string) => void
  /** 点到历史会话时先收起设置等全屏层；否则同 URL 的 navigate 不会触发，桌面端无法回到聊天 */
  beforeNavigateToThread?: () => void
}

type ProjectGroup = { path: string; label: string; threads: ThreadResponse[] }
type GtdBucket = ThreadGtdBucket
type GtdGroup = { bucket: GtdBucket; label: string; threads: ThreadResponse[] }

const PROJECT_GROUP_PAGE_SIZE = 8
const PROJECT_GROUP_SECONDARY_PAGE_SIZE = 2
const PROJECT_GROUP_LABEL_WEIGHT = 'var(--c-sidebar-thread-weight)'
const SIDEBAR_ROW_TEXT_SIZE = '13.5px'
const SIDEBAR_ROW_LINE_HEIGHT = '20px'
const GTD_ENABLED_STORAGE_KEY = 'arkloop:web:gtd_enabled'
const DRAG_START_DISTANCE_PX = 3
const DRAG_LONG_PRESS_DELAY_MS = 180
const GTD_BUCKETS: readonly GtdBucket[] = ['inbox', 'todo', 'waiting', 'someday', 'archived']

type SidebarDragState =
  | { kind: 'work'; threadId: string; title: string; x: number; y: number; fromPinned: boolean; sourcePath: string }
  | { kind: 'gtd'; threadId: string; title: string; x: number; y: number; sourceBucket: GtdBucket }

function projectGroupLimit(value: number | undefined, fallback: number): number {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) return Math.floor(value)
  return fallback
}

function threadTitle(thread: ThreadResponse, untitled: string): string {
  const title = (thread.title ?? '').trim()
  return title.length > 0 ? title : untitled
}

function defaultGtdExpandedBuckets(): Set<GtdBucket> {
  return new Set(GTD_BUCKETS)
}

function areStringSetsEqual(left: Set<string>, right: Set<string>): boolean {
  if (left.size !== right.size) return false
  for (const value of left) {
    if (!right.has(value)) return false
  }
  return true
}

function normalizeGtdBucket(value: ThreadResponse['sidebar_gtd_bucket']): GtdBucket | null {
  return value && GTD_BUCKETS.includes(value) ? value : null
}

function withSidebarPatch(thread: ThreadResponse, patch: UpdateThreadSidebarRequest): ThreadResponse {
  const next: ThreadResponse = { ...thread }
  if ('sidebar_work_folder' in patch) {
    const folder = (patch.sidebar_work_folder ?? '').trim()
    next.sidebar_work_folder = folder.length > 0 ? folder : null
  }
  if ('sidebar_pinned' in patch) {
    next.sidebar_pinned_at = patch.sidebar_pinned ? new Date().toISOString() : null
  }
  if ('sidebar_gtd_bucket' in patch) {
    next.sidebar_gtd_bucket = patch.sidebar_gtd_bucket ?? null
  }
  return next
}

type SidebarThreadItemProps = {
  thread: ThreadResponse
  section: 'starred' | 'regular'
  isRunning: boolean
  isCompletedUnread: boolean
  isMenuOpen: boolean
  isEditing: boolean
  isActive: boolean
  isStarred: boolean
  showStatusDot?: boolean
  isDragging?: boolean
  editingTitle: string
  untitled: string
  editInputRef: React.RefObject<HTMLInputElement | null>
  setEditingTitle: React.Dispatch<React.SetStateAction<string>>
  setEditingThreadId: React.Dispatch<React.SetStateAction<string | null>>
  commitRename: (id: string, newTitle: string) => void
  markCompletionRead: (threadId: string) => void
  beforeNavigateToThread?: () => void
  navigate: ReturnType<typeof useNavigate>
  openMenu: (event: React.MouseEvent, id: string) => void
}

const SidebarThreadItem = memo(function SidebarThreadItem({
  thread,
  section,
  isRunning,
  isCompletedUnread,
  isMenuOpen,
  isEditing,
  isActive,
  isStarred,
  showStatusDot = false,
  isDragging = false,
  editingTitle,
  untitled,
  editInputRef,
  setEditingTitle,
  setEditingThreadId,
  commitRename,
  markCompletionRead,
  beforeNavigateToThread,
  navigate,
  openMenu,
}: SidebarThreadItemProps) {
  return (
    <div
      key={`${thread.id}-${section}`}
      data-sidebar-thread-item="thread"
      data-thread-id={thread.id}
      aria-hidden={isDragging || undefined}
      className={[
        'group relative isolate flex w-full items-center rounded-[6px] before:pointer-events-none before:absolute before:inset-x-0 before:inset-y-px before:-z-10 before:rounded-[6px] before:content-[""]',
        isDragging ? 'pointer-events-none invisible' : '',
        isActive || isMenuOpen
          ? 'before:bg-[var(--c-bg-deep)]'
          : 'hover:before:bg-[var(--c-bg-deep)]',
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
          className="h-[34px] min-w-0 flex-1 bg-transparent px-2 py-0 text-[13.5px] leading-[20px] text-[var(--c-text-primary)] outline-none"
          style={{ border: 'none', fontWeight: 'var(--c-sidebar-thread-weight)' }}
          maxLength={200}
        />
      ) : (
        <button
          onClick={() => {
            markCompletionRead(thread.id)
            beforeNavigateToThread?.()
            navigate(`/t/${thread.id}`)
          }}
          className={[
            'flex h-[34px] min-w-0 flex-1 items-center gap-2 px-2 py-0 text-left text-[13.5px] leading-[20px] group-hover:text-[var(--c-text-primary)]',
            isActive
              ? 'text-[var(--c-text-primary)]'
              : 'text-[var(--c-text-secondary)]',
          ].join(' ')}
          style={{ fontWeight: 'var(--c-sidebar-thread-weight)' }}
        >
          {showStatusDot && (
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
          )}
          {isStarred && (
            <Star size={11} className="shrink-0 fill-[var(--c-text-muted)] text-[var(--c-text-muted)] opacity-70" />
          )}
          <span className="min-w-0 flex-1 truncate">{threadTitle(thread, untitled)}</span>
        </button>
      )}

      {!isEditing && (
        <div className="mr-1 flex shrink-0 items-center">
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
})

export function Sidebar({
  threads,
  onNewThread,
  onThreadDeleted,
  beforeNavigateToThread,
}: Props) {
  const { me, accessToken } = useAuth()
  const {
    runningThreadIds,
    completedUnreadThreadIds,
    isPrivateMode,
    pendingIncognitoMode,
    updateTitle: onThreadTitleUpdated,
    upsertThread,
    markCompletionRead,
  } = useThreadList()
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
  const [gtdEnabled, setGtdEnabled] = useState(() => readGtdEnabled())
  const [gtdInboxIds, setGtdInboxIds] = useState<Set<string>>(() => readGtdInboxThreadIds())
  const [gtdTodoIds, setGtdTodoIds] = useState<Set<string>>(() => readGtdTodoThreadIds())
  const [gtdWaitingIds, setGtdWaitingIds] = useState<Set<string>>(() => readGtdWaitingThreadIds())
  const [gtdSomedayIds, setGtdSomedayIds] = useState<Set<string>>(() => readGtdSomedayThreadIds())
  const [gtdArchivedIds, setGtdArchivedIds] = useState<Set<string>>(() => readGtdArchivedThreadIds())
  const [pinnedIds, setPinnedIds] = useState<Set<string>>(() => readPinnedThreadIds())
  const pinnedIdsRef = useRef(pinnedIds)
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(() => readExpandedProjectPaths())
  const [expandedLimits, setExpandedLimits] = useState<Record<string, number>>({})
  const [expandedGtdBuckets, setExpandedGtdBuckets] = useState<Set<GtdBucket>>(() => defaultGtdExpandedBuckets())
  const [gtdExpandedLimits, setGtdExpandedLimits] = useState<Record<GtdBucket, number>>({ inbox: 8, todo: 8, waiting: 8, someday: 8, archived: 8 })
  const [workFolderVersion, setWorkFolderVersion] = useState(0)
  const [pinnedExpanded, setPinnedExpanded] = useState(true)
  const [dragState, setDragState] = useState<SidebarDragState | null>(null)
  const [dragOverPinned, setDragOverPinned] = useState(false)
  const [dragOverProjectPath, setDragOverProjectPath] = useState<string | null>(null)
  const [dragOverGtdBucket, setDragOverGtdBucket] = useState<GtdBucket | null>(null)
  const pinnedDropRef = useRef<HTMLDivElement>(null)
  const projectGroupRefs = useRef<Map<string, HTMLDivElement>>(new Map())
  const gtdGroupRefs = useRef<Map<GtdBucket, HTMLDivElement>>(new Map())
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
  }, [starredIds, threads])

  const effectivePinnedIds = useMemo(() => {
    const next = new Set(pinnedIds)
    for (const thread of threads) {
      if (thread.sidebar_pinned_at) next.add(thread.id)
    }
    return next
  }, [pinnedIds, threads])

  const pinnedWorkThreads = useMemo(() => {
    if (!isWorkMode) return []
    return threads.filter(th => effectivePinnedIds.has(th.id))
  }, [isWorkMode, threads, effectivePinnedIds])

  // 初始化时从服务端拉取收藏列表
  useEffect(() => {
    listStarredThreadIds(accessToken)
      .then((ids) => setStarredIds(ids))
      .catch(() => {})
  }, [accessToken])

  useEffect(() => {
    if (!activeThreadId || !completedUnreadThreadIds.has(activeThreadId)) return
    markCompletionRead(activeThreadId)
  }, [activeThreadId, completedUnreadThreadIds, markCompletionRead])

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

  const patchSidebarState = useCallback((id: string, patch: UpdateThreadSidebarRequest, rollbackLocal?: () => void) => {
    const current = threads.find((thread) => thread.id === id)
    if (current) upsertThread(withSidebarPatch(current, patch))
    void updateThreadSidebarState(accessToken, id, patch).then((updated) => {
      upsertThread(updated)
    }).catch(() => {
      rollbackLocal?.()
      if (current) upsertThread(current)
    })
  }, [accessToken, threads, upsertThread])

  // -- 分组逻辑 --

  const projectGroups = useMemo(() => {
    void workFolderVersion
    const groups = new Map<string, ThreadResponse[]>()

    for (const t of threads) {
      const wf = (t.sidebar_work_folder ?? readThreadWorkFolder(t.id) ?? '').trim()
      const key = wf || '__unassigned__'
      if (!groups.has(key)) groups.set(key, [])
      if (!effectivePinnedIds.has(t.id)) groups.get(key)!.push(t)
    }

    const result: ProjectGroup[] = []
    for (const [path, groupThreads] of groups) {
      const sorted = [...groupThreads].sort((a, b) => {
        const aStar = starredIds.includes(a.id) ? 1 : 0
        const bStar = starredIds.includes(b.id) ? 1 : 0
        if (aStar !== bStar) return bStar - aStar
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      })

      const label = path === '__unassigned__' ? t.projectUnassigned : path.split('/').pop() || path
      result.push({ path, label, threads: sorted })
    }

    result.sort((a, b) => {
      if (a.path === '__unassigned__') return -1
      if (b.path === '__unassigned__') return 1
      return a.path.localeCompare(b.path)
    })

    return result
  }, [threads, effectivePinnedIds, starredIds, t, workFolderVersion])

  const localGtdBucketForThread = useCallback((id: string): GtdBucket | null => {
    if (gtdInboxIds.has(id)) return 'inbox'
    if (gtdTodoIds.has(id)) return 'todo'
    if (gtdWaitingIds.has(id)) return 'waiting'
    if (gtdSomedayIds.has(id)) return 'someday'
    if (gtdArchivedIds.has(id)) return 'archived'
    return null
  }, [gtdArchivedIds, gtdInboxIds, gtdSomedayIds, gtdTodoIds, gtdWaitingIds])

  const effectiveGtdBucketForThread = useCallback((thread: ThreadResponse): GtdBucket | null => {
    return normalizeGtdBucket(thread.sidebar_gtd_bucket) ?? localGtdBucketForThread(thread.id)
  }, [localGtdBucketForThread])

  const gtdGroups = useMemo(() => {
    const buckets: GtdGroup[] = [
      { bucket: 'inbox', label: t.gtdInbox, threads: [] },
      { bucket: 'todo', label: t.gtdTodo, threads: [] },
      { bucket: 'waiting', label: t.gtdWaiting, threads: [] },
      { bucket: 'someday', label: t.gtdSomeday, threads: [] },
      { bucket: 'archived', label: t.gtdArchived, threads: [] },
    ]

    for (const thread of threads) {
      const bucketName = effectiveGtdBucketForThread(thread) ?? 'archived'
      const bucket = buckets.find((item) => item.bucket === bucketName) ?? buckets[4]
      bucket.threads.push(thread)
    }

    for (const bucket of buckets) {
      bucket.threads.sort((a, b) => {
        const aPin = effectivePinnedIds.has(a.id) ? 1 : 0
        const bPin = effectivePinnedIds.has(b.id) ? 1 : 0
        if (aPin !== bPin) return bPin - aPin
        const aStar = starredIds.includes(a.id) ? 1 : 0
        const bStar = starredIds.includes(b.id) ? 1 : 0
        if (aStar !== bStar) return bStar - aStar
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      })
    }

    return buckets
  }, [threads, effectiveGtdBucketForThread, effectivePinnedIds, starredIds, t])

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

  // -- 渲染辅助 --

  const draggingThreadId = dragState?.threadId ?? null
  const draggingToPinned = dragState?.kind === 'work' && !dragState.fromPinned

  const setProjectGroupNode = useCallback((path: string, node: HTMLDivElement | null) => {
    if (node) {
      projectGroupRefs.current.set(path, node)
    } else {
      projectGroupRefs.current.delete(path)
    }
  }, [])

  const setGtdGroupNode = useCallback((bucket: GtdBucket, node: HTMLDivElement | null) => {
    if (node) {
      gtdGroupRefs.current.set(bucket, node)
    } else {
      gtdGroupRefs.current.delete(bucket)
    }
  }, [])

  const renderThread = useCallback((thread: ThreadResponse, options?: { showStatusDot?: boolean }) => {
    return (
      <SidebarThreadItem
        key={thread.id}
        thread={thread}
        section="regular"
        isRunning={runningThreadIds.has(thread.id)}
        isCompletedUnread={completedUnreadThreadIds.has(thread.id)}
        isMenuOpen={menuThreadId === thread.id}
        isEditing={editingThreadId === thread.id}
        isActive={thread.id === activeThreadId}
        isStarred={starredSet.has(thread.id)}
        showStatusDot={options?.showStatusDot}
        isDragging={thread.id === draggingThreadId}
        editingTitle={editingTitle}
        untitled={t.untitled}
        editInputRef={editInputRef}
        setEditingTitle={setEditingTitle}
        setEditingThreadId={setEditingThreadId}
        commitRename={commitRename}
        markCompletionRead={markCompletionRead}
        beforeNavigateToThread={beforeNavigateToThread}
        navigate={navigate}
        openMenu={openMenu}
      />
    )
  }, [runningThreadIds, completedUnreadThreadIds, menuThreadId, editingThreadId, activeThreadId, starredSet, draggingThreadId, editingTitle, t.untitled, editInputRef, setEditingTitle, setEditingThreadId, commitRename, markCompletionRead, beforeNavigateToThread, navigate, openMenu])

  const renderDropRow = (icon: React.ReactNode, label: string, active: boolean) => (
    <div
      className={[
        'relative isolate flex h-[34px] w-full items-center gap-2 rounded-[6px] px-2 py-0 text-[13.5px] leading-[20px] before:pointer-events-none before:absolute before:inset-x-0 before:inset-y-px before:-z-10 before:rounded-[6px] before:content-[""]',
        active
          ? 'text-[var(--c-text-primary)] before:bg-[var(--c-bg-deep)]'
          : 'text-[var(--c-text-muted)]',
      ].join(' ')}
      style={{ fontWeight: 'var(--c-sidebar-thread-weight)' }}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
    </div>
  )

  // -- 视图组件 --

  const ProjectSidebarView = (
    <>
      {projectGroups.map(group => {
        const isExpanded = expandedPaths.has(group.path)
        const limit = projectGroupLimit(expandedLimits[group.path], PROJECT_GROUP_PAGE_SIZE)
        const visible = isExpanded ? group.threads.slice(0, limit) : []
        const hasMore = isExpanded && group.threads.length > limit

        const toggleExpand = () => {
          const willExpand = !expandedPaths.has(group.path)
          const initialLimit = expandedPaths.size > 0 ? PROJECT_GROUP_SECONDARY_PAGE_SIZE : PROJECT_GROUP_PAGE_SIZE
          setExpandedPaths(prev => {
            const next = new Set(prev)
            if (next.has(group.path)) {
              next.delete(group.path)
            } else {
              next.add(group.path)
            }
            writeExpandedProjectPaths(next)
            return next
          })
          if (willExpand) {
            setExpandedLimits(prev => ({ ...prev, [group.path]: initialLimit }))
          }
        }

        return (
          <div
            key={group.path}
            ref={(node) => setProjectGroupNode(group.path, node)}
            className={[
              'rounded-[6px]',
              dragOverProjectPath === group.path ? 'bg-[var(--c-bg-deep)]' : '',
            ].join(' ')}
          >
            {/* folder header — click anywhere to toggle */}
            <div
              className="group/folder flex h-[34px] w-full cursor-pointer select-none items-center px-2 py-0"
              onMouseDown={(e) => { if (e.detail > 1) e.preventDefault() }}
              onClick={toggleExpand}
            >
              <span
                className="min-w-0 shrink select-none truncate text-left text-[13.5px] leading-[20px] text-[var(--c-text-muted)] transition-colors duration-[80ms] group-hover/folder:text-[var(--c-text-tertiary)]"
                style={{ fontWeight: PROJECT_GROUP_LABEL_WEIGHT }}
              >
                {group.label}
              </span>
              {/* chevron — right of text, hover only */}
              <span className="ml-1 shrink-0 opacity-0 group-hover/folder:opacity-100 text-[var(--c-text-muted)] transition-opacity duration-[80ms]">
                <ChevronRight size={12} className={['transition-transform duration-150', isExpanded ? 'rotate-90' : 'rotate-0'].join(' ')} />
              </span>
              <span className="flex-1" />
              {/* plus — far right, hover only */}
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  if (group.path === '__unassigned__') {
                    clearWorkFolder()
                  } else {
                    writeWorkFolder(group.path)
                  }
                  onNewThread()
                }}
                className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md opacity-0 group-hover/folder:opacity-100 text-[var(--c-text-muted)] hover:bg-[var(--c-sidebar-btn-hover)] hover:text-[var(--c-text-primary)] transition-[opacity,background-color,color,transform] duration-[80ms] active:scale-[0.92]"
              >
                <Plus size={14} />
              </button>
            </div>
            {visible.map(th => renderThread(th, { showStatusDot: true }))}
            {/* More — styled like a session item */}
            {hasMore && (
              <button
                onClick={() => setExpandedLimits(prev => ({ ...prev, [group.path]: limit + PROJECT_GROUP_PAGE_SIZE }))}
                className="group relative isolate flex h-[34px] w-full items-center rounded-[6px] px-2 py-0 before:pointer-events-none before:absolute before:inset-x-0 before:inset-y-px before:-z-10 before:rounded-[6px] before:content-[''] hover:before:bg-[var(--c-bg-deep)]"
              >
                <div className="flex items-center gap-2 text-[13.5px] leading-[20px] text-[var(--c-text-muted)]" style={{ fontWeight: 'var(--c-sidebar-thread-weight)' }}>
                  <MoreHorizontal size={14} className="shrink-0" />
                  <span>{t.folderMore}</span>
                </div>
              </button>
            )}
          </div>
        )
      })}
    </>
  )

  const GtdSidebarView = (
    <>
      {gtdGroups.map(group => {
        const isExpanded = expandedGtdBuckets.has(group.bucket)
        const limit = projectGroupLimit(gtdExpandedLimits[group.bucket], PROJECT_GROUP_PAGE_SIZE)
        const visible = isExpanded ? group.threads.slice(0, limit) : []
        const hasMore = isExpanded && group.threads.length > limit

        const toggleExpand = () => {
          setExpandedGtdBuckets(prev => {
            const next = new Set(prev)
            if (next.has(group.bucket)) {
              next.delete(group.bucket)
            } else {
              next.add(group.bucket)
            }
            return next
          })
        }

        return (
          <div
            key={group.bucket}
            ref={(node) => setGtdGroupNode(group.bucket, node)}
            className={[
              'rounded-[6px]',
              dragOverGtdBucket === group.bucket ? 'bg-[var(--c-bg-deep)]' : '',
            ].join(' ')}
          >
            <div
              className="group/folder flex h-[34px] w-full cursor-pointer select-none items-center px-2 py-0"
              onMouseDown={(e) => { if (e.detail > 1) e.preventDefault() }}
              onClick={toggleExpand}
            >
              <span
                className="min-w-0 shrink select-none truncate text-left text-[13.5px] leading-[20px] text-[var(--c-text-muted)] transition-colors duration-[80ms] group-hover/folder:text-[var(--c-text-tertiary)]"
                style={{ fontWeight: PROJECT_GROUP_LABEL_WEIGHT }}
              >
                {group.label}
              </span>
              <span className="ml-1 shrink-0 opacity-0 group-hover/folder:opacity-100 text-[var(--c-text-muted)] transition-opacity duration-[80ms]">
                <ChevronRight size={12} className={['transition-transform duration-150', isExpanded ? 'rotate-90' : 'rotate-0'].join(' ')} />
              </span>
            </div>
            {visible.map(th => renderThread(th, { showStatusDot: true }))}
            {hasMore && (
              <button
                onClick={() => setGtdExpandedLimits(prev => ({ ...prev, [group.bucket]: limit + PROJECT_GROUP_PAGE_SIZE }))}
                className="group relative isolate flex h-[34px] w-full items-center rounded-[6px] px-2 py-0 before:pointer-events-none before:absolute before:inset-x-0 before:inset-y-px before:-z-10 before:rounded-[6px] before:content-[''] hover:before:bg-[var(--c-bg-deep)]"
              >
                <div className="flex items-center gap-2 text-[13.5px] leading-[20px] text-[var(--c-text-muted)]" style={{ fontWeight: 'var(--c-sidebar-thread-weight)' }}>
                  <MoreHorizontal size={14} className="shrink-0" />
                  <span>{t.folderMore}</span>
                </div>
              </button>
            )}
          </div>
        )
      })}
    </>
  )

  // GTD / Pin 操作
  const applyGtdBucketLocal = useCallback((id: string, bucket: GtdBucket | null) => {
    setGtdInboxIds((prev: Set<string>) => {
      const next = new Set(prev)
      if (bucket === 'inbox') next.add(id); else next.delete(id)
      writeGtdInboxThreadIds(next)
      return next
    })
    setGtdTodoIds((prev: Set<string>) => {
      const next = new Set(prev)
      if (bucket === 'todo') next.add(id); else next.delete(id)
      writeGtdTodoThreadIds(next)
      return next
    })
    setGtdWaitingIds((prev: Set<string>) => {
      const next = new Set(prev)
      if (bucket === 'waiting') next.add(id); else next.delete(id)
      writeGtdWaitingThreadIds(next)
      return next
    })
    setGtdSomedayIds((prev: Set<string>) => {
      const next = new Set(prev)
      if (bucket === 'someday') next.add(id); else next.delete(id)
      writeGtdSomedayThreadIds(next)
      return next
    })
    setGtdArchivedIds((prev: Set<string>) => {
      const next = new Set(prev)
      if (bucket === 'archived') next.add(id); else next.delete(id)
      writeGtdArchivedThreadIds(next)
      return next
    })
  }, [])

  const currentGtdBucket = useCallback((id: string): GtdBucket | null => {
    const thread = threads.find((item) => item.id === id)
    return thread ? effectiveGtdBucketForThread(thread) : localGtdBucketForThread(id)
  }, [effectiveGtdBucketForThread, localGtdBucketForThread, threads])

  const moveGtdThread = useCallback((id: string, bucket: GtdBucket) => {
    const previous = currentGtdBucket(id)
    applyGtdBucketLocal(id, bucket)
    patchSidebarState(id, { sidebar_gtd_bucket: bucket }, () => applyGtdBucketLocal(id, previous))
  }, [applyGtdBucketLocal, currentGtdBucket, patchSidebarState])

  const markGtdInbox = useCallback((id: string) => moveGtdThread(id, 'inbox'), [moveGtdThread])
  const markGtdTodo = useCallback((id: string) => moveGtdThread(id, 'todo'), [moveGtdThread])
  const markGtdWaiting = useCallback((id: string) => moveGtdThread(id, 'waiting'), [moveGtdThread])
  const markGtdSomeday = useCallback((id: string) => moveGtdThread(id, 'someday'), [moveGtdThread])
  const archiveGtdThread = useCallback((id: string) => moveGtdThread(id, 'archived'), [moveGtdThread])

  const replacePinnedIds = useCallback((next: Set<string>) => {
    pinnedIdsRef.current = next
    writePinnedThreadIds(next)
    setPinnedIds(next)
  }, [])

  const applyPinnedLocal = useCallback((id: string, pinned: boolean) => {
    const next = new Set(pinnedIdsRef.current)
    if (pinned) next.add(id)
    else next.delete(id)
    replacePinnedIds(next)
  }, [replacePinnedIds])

  const togglePin = useCallback((id: string) => {
    const nextPinned = !effectivePinnedIds.has(id)
    applyPinnedLocal(id, nextPinned)
    patchSidebarState(id, { sidebar_pinned: nextPinned }, () => applyPinnedLocal(id, !nextPinned))
  }, [applyPinnedLocal, effectivePinnedIds, patchSidebarState])

  const pinThread = useCallback((id: string) => {
    if (effectivePinnedIds.has(id)) return
    applyPinnedLocal(id, true)
    patchSidebarState(id, { sidebar_pinned: true }, () => applyPinnedLocal(id, false))
  }, [applyPinnedLocal, effectivePinnedIds, patchSidebarState])

  const dragOverPinnedRef = useRef(false)
  const dragOverProjectPathRef = useRef<string | null>(null)
  const dragOverGtdBucketRef = useRef<GtdBucket | null>(null)
  const setThreadWorkFolder = useCallback((id: string, folder: string | null) => {
    const previous = readThreadWorkFolder(id)
    if (folder && folder !== '__unassigned__') {
      writeThreadWorkFolder(id, folder)
    } else {
      clearThreadWorkFolder(id)
    }
    patchSidebarState(id, { sidebar_work_folder: folder === '__unassigned__' ? null : folder }, () => {
      if (previous) writeThreadWorkFolder(id, previous)
      else clearThreadWorkFolder(id)
    })
  }, [patchSidebarState])

  const movePinnedThreadToFolder = useCallback((id: string, folder: string | null) => {
    const normalizedFolder = folder === '__unassigned__' ? null : folder
    const thread = threads.find((item) => item.id === id)
    const previousFolder = thread?.sidebar_work_folder ?? readThreadWorkFolder(id)
    const previousPinned = effectivePinnedIds.has(id)

    if (normalizedFolder) writeThreadWorkFolder(id, normalizedFolder)
    else clearThreadWorkFolder(id)
    applyPinnedLocal(id, false)

    patchSidebarState(
      id,
      { sidebar_work_folder: normalizedFolder, sidebar_pinned: false },
      () => {
        if (previousFolder) writeThreadWorkFolder(id, previousFolder)
        else clearThreadWorkFolder(id)
        applyPinnedLocal(id, previousPinned)
      },
    )
  }, [applyPinnedLocal, effectivePinnedIds, patchSidebarState, threads])

  useEffect(() => {
    dragOverPinnedRef.current = dragOverPinned
  }, [dragOverPinned])
  useEffect(() => {
    dragOverProjectPathRef.current = dragOverProjectPath
  }, [dragOverProjectPath])
  useEffect(() => {
    dragOverGtdBucketRef.current = dragOverGtdBucket
  }, [dragOverGtdBucket])

  useEffect(() => {
    if (!isWorkMode) return
    let timer: number | null = null
    let startX = 0
    let startY = 0
    let candidate: { id: string; title: string; fromPinned: boolean; sourcePath: string } | null = null
    let dragging: { id: string; title: string; fromPinned: boolean; sourcePath: string } | null = null
    let suppressClick = false

    const clearTimer = () => {
      if (timer) {
        window.clearTimeout(timer)
        timer = null
      }
    }

    const setPinnedHover = (over: boolean) => {
      dragOverPinnedRef.current = over
      setDragOverPinned(prev => prev === over ? prev : over)
    }

    const setProjectHover = (path: string | null) => {
      dragOverProjectPathRef.current = path
      setDragOverProjectPath(prev => prev === path ? prev : path)
    }

    const findProjectTarget = (e: PointerEvent, sourcePath: string, fromPinned: boolean): string | null => {
      for (const [path, node] of projectGroupRefs.current) {
        const rect = node.getBoundingClientRect()
        const inside = e.clientX >= rect.left && e.clientX <= rect.right && e.clientY >= rect.top && e.clientY <= rect.bottom
        if (!inside) continue
        if (!fromPinned && path === sourcePath) return null
        return path
      }
      return null
    }

    const updateDropTargets = (e: PointerEvent, current: { fromPinned: boolean; sourcePath: string }) => {
      const projectTarget = findProjectTarget(e, current.sourcePath, current.fromPinned)
      setProjectHover(projectTarget)

      if (!pinnedDropRef.current) {
        setPinnedHover(false)
        return
      }
      const rect = pinnedDropRef.current.getBoundingClientRect()
      const overPinned = e.clientX >= rect.left && e.clientX <= rect.right && e.clientY >= rect.top && e.clientY <= rect.bottom
      setPinnedHover(!current.fromPinned && overPinned)
    }

    const beginDrag = (e: PointerEvent) => {
      if (!candidate || dragging) return
      dragging = candidate
      suppressClick = true
      clearTimer()
      setDragState({ kind: 'work', threadId: candidate.id, title: candidate.title, x: e.clientX, y: e.clientY, fromPinned: candidate.fromPinned, sourcePath: candidate.sourcePath })
      updateDropTargets(e, candidate)
    }

    const onPointerDown = (e: PointerEvent) => {
      const el = (e.target as HTMLElement).closest('[data-sidebar-thread-item="thread"]')
      if (!el) return
      const threadId = el.getAttribute('data-thread-id')
      if (!threadId) return
      const thread = threads.find(t => t.id === threadId)
      if (!thread) return
      startX = e.clientX
      startY = e.clientY
      candidate = {
        id: threadId,
        title: threadTitle(thread, t.untitled),
        fromPinned: effectivePinnedIds.has(threadId),
        sourcePath: (thread.sidebar_work_folder ?? readThreadWorkFolder(threadId)) || '__unassigned__',
      }
      dragging = null
      if (!candidate.fromPinned) {
        timer = window.setTimeout(() => {
          beginDrag(e)
        }, DRAG_LONG_PRESS_DELAY_MS)
      }
    }

    const onPointerMove = (e: PointerEvent) => {
      if (dragging) {
        e.preventDefault()
        setDragState(prev => prev ? { ...prev, x: e.clientX, y: e.clientY } : null)
        updateDropTargets(e, dragging)
        return
      }
      if (!candidate) return
      const dx = e.clientX - startX
      const dy = e.clientY - startY
      if (Math.sqrt(dx * dx + dy * dy) < DRAG_START_DISTANCE_PX) return
      beginDrag(e)
    }

    const onPointerUp = () => {
      clearTimer()
      if (dragging) {
        const projectTarget = dragOverProjectPathRef.current
        if (dragging.fromPinned && projectTarget) {
          movePinnedThreadToFolder(dragging.id, projectTarget === '__unassigned__' ? null : projectTarget)
        } else if (!dragging.fromPinned && dragOverPinnedRef.current) {
          pinThread(dragging.id)
          setPinnedExpanded(true)
        } else if (!dragging.fromPinned && projectTarget && projectTarget !== dragging.sourcePath) {
          setThreadWorkFolder(dragging.id, projectTarget === '__unassigned__' ? null : projectTarget)
        }
        dragging = null
        setDragState(null)
        setPinnedHover(false)
        setProjectHover(null)
        window.setTimeout(() => { suppressClick = false }, 100)
      }
      candidate = null
    }

    const onClick = (e: MouseEvent) => {
      if (!suppressClick) return
      const el = (e.target as HTMLElement).closest('[data-sidebar-thread-item="thread"]')
      if (!el) return
      e.preventDefault()
      e.stopPropagation()
    }

    document.addEventListener('pointerdown', onPointerDown, true)
    document.addEventListener('pointermove', onPointerMove)
    document.addEventListener('pointerup', onPointerUp)
    document.addEventListener('pointercancel', onPointerUp)
    document.addEventListener('click', onClick, true)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true)
      document.removeEventListener('pointermove', onPointerMove)
      document.removeEventListener('pointerup', onPointerUp)
      document.removeEventListener('pointercancel', onPointerUp)
      document.removeEventListener('click', onClick, true)
      clearTimer()
    }
  }, [isWorkMode, threads, effectivePinnedIds, t.untitled, movePinnedThreadToFolder, pinThread, setThreadWorkFolder])

  useEffect(() => {
    if (isWorkMode || !gtdEnabled) return
    let timer: number | null = null
    let startX = 0
    let startY = 0
    let candidate: { id: string; title: string; sourceBucket: GtdBucket } | null = null
    let dragging: { id: string; title: string; sourceBucket: GtdBucket } | null = null
    let suppressClick = false

    const clearTimer = () => {
      if (timer) {
        window.clearTimeout(timer)
        timer = null
      }
    }

    const setGtdHover = (bucket: GtdBucket | null) => {
      dragOverGtdBucketRef.current = bucket
      setDragOverGtdBucket(prev => prev === bucket ? prev : bucket)
    }

    const currentBucket = (thread: ThreadResponse): GtdBucket => {
      return effectiveGtdBucketForThread(thread) ?? 'archived'
    }

    const findGtdTarget = (e: PointerEvent, sourceBucket: GtdBucket): GtdBucket | null => {
      for (const [bucket, node] of gtdGroupRefs.current) {
        const rect = node.getBoundingClientRect()
        const inside = e.clientX >= rect.left && e.clientX <= rect.right && e.clientY >= rect.top && e.clientY <= rect.bottom
        if (!inside) continue
        if (bucket === sourceBucket) return null
        return bucket
      }
      return null
    }

    const updateDropTargets = (e: PointerEvent, sourceBucket: GtdBucket) => {
      setGtdHover(findGtdTarget(e, sourceBucket))
    }

    const beginDrag = (e: PointerEvent) => {
      if (!candidate || dragging) return
      dragging = candidate
      suppressClick = true
      clearTimer()
      setDragState({ kind: 'gtd', threadId: candidate.id, title: candidate.title, x: e.clientX, y: e.clientY, sourceBucket: candidate.sourceBucket })
      updateDropTargets(e, candidate.sourceBucket)
    }

    const onPointerDown = (e: PointerEvent) => {
      const el = (e.target as HTMLElement).closest('[data-sidebar-thread-item="thread"]')
      if (!el) return
      const threadId = el.getAttribute('data-thread-id')
      if (!threadId) return
      const thread = threads.find(t => t.id === threadId)
      if (!thread) return
      startX = e.clientX
      startY = e.clientY
      candidate = {
        id: threadId,
        title: threadTitle(thread, t.untitled),
        sourceBucket: currentBucket(thread),
      }
      dragging = null
      timer = window.setTimeout(() => {
        beginDrag(e)
      }, DRAG_LONG_PRESS_DELAY_MS)
    }

    const onPointerMove = (e: PointerEvent) => {
      if (dragging) {
        e.preventDefault()
        setDragState(prev => prev ? { ...prev, x: e.clientX, y: e.clientY } : null)
        updateDropTargets(e, dragging.sourceBucket)
        return
      }
      if (!candidate) return
      const dx = e.clientX - startX
      const dy = e.clientY - startY
      if (Math.sqrt(dx * dx + dy * dy) < DRAG_START_DISTANCE_PX) return
      beginDrag(e)
    }

    const onPointerUp = () => {
      clearTimer()
      if (dragging) {
        const target = dragOverGtdBucketRef.current
        if (target) {
          moveGtdThread(dragging.id, target)
        }
        dragging = null
        setDragState(null)
        setGtdHover(null)
        window.setTimeout(() => { suppressClick = false }, 100)
      }
      candidate = null
    }

    const onClick = (e: MouseEvent) => {
      if (!suppressClick) return
      const el = (e.target as HTMLElement).closest('[data-sidebar-thread-item="thread"]')
      if (!el) return
      e.preventDefault()
      e.stopPropagation()
    }

    document.addEventListener('pointerdown', onPointerDown, true)
    document.addEventListener('pointermove', onPointerMove)
    document.addEventListener('pointerup', onPointerUp)
    document.addEventListener('pointercancel', onPointerUp)
    document.addEventListener('click', onClick, true)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true)
      document.removeEventListener('pointermove', onPointerMove)
      document.removeEventListener('pointerup', onPointerUp)
      document.removeEventListener('pointercancel', onPointerUp)
      document.removeEventListener('click', onClick, true)
      clearTimer()
    }
  }, [isWorkMode, gtdEnabled, threads, effectiveGtdBucketForThread, t.untitled, moveGtdThread])

  const handleDelete = useCallback(async (id: string) => {
    setDeleteConfirmThreadId(null)
    try {
      await deleteThread(accessToken, id)
      // 清理 GTD 和 Pin 的本地状态
      applyGtdBucketLocal(id, null)
      if (pinnedIdsRef.current.has(id)) {
        const next = new Set(pinnedIdsRef.current)
        next.delete(id)
        replacePinnedIds(next)
      }
      onThreadDeleted(id)
    } catch {
      // 失败静默
    }
  }, [accessToken, applyGtdBucketLocal, onThreadDeleted, replacePinnedIds])

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
    const handler = (e: Event) => {
      const enabled = (e as CustomEvent<boolean>).detail
      setGtdEnabled(enabled)
    }
    const storageHandler = (e: StorageEvent) => {
      if (e.key !== GTD_ENABLED_STORAGE_KEY) return
      setGtdEnabled(e.newValue === 'true')
    }
    window.addEventListener('arkloop:gtd-enabled-changed', handler)
    window.addEventListener('storage', storageHandler)
    return () => {
      window.removeEventListener('arkloop:gtd-enabled-changed', handler)
      window.removeEventListener('storage', storageHandler)
    }
  }, [])

  // 监听 work folder 变更，触发重新渲染
  useEffect(() => {
    const handler = () => setWorkFolderVersion(v => v + 1)
    window.addEventListener('arkloop:work-folder-changed', handler)
    return () => window.removeEventListener('arkloop:work-folder-changed', handler)
  }, [])

  // 新建线程时 addThread 会更新 localStorage 中的 GTD Inbox，
  // 但 Sidebar 的 gtdInboxIds state 不会感知，需要在线程列表变化时同步。
  useEffect(() => {
    let cancelled = false
    window.queueMicrotask(() => {
      if (cancelled) return
      setGtdInboxIds(readGtdInboxThreadIds())
      setGtdTodoIds(readGtdTodoThreadIds())
      setGtdWaitingIds(readGtdWaitingThreadIds())
      setGtdSomedayIds(readGtdSomedayThreadIds())
      setGtdArchivedIds(readGtdArchivedThreadIds())
    })
    return () => { cancelled = true }
  }, [threads])

  useEffect(() => {
    let cancelled = false
    window.queueMicrotask(() => {
      if (cancelled) return
      const storedPinnedIds = readPinnedThreadIds()
      const next = new Set(pinnedIdsRef.current)
      for (const thread of threads) {
        if (thread.sidebar_pinned_at || storedPinnedIds.has(thread.id)) {
          next.add(thread.id)
        } else {
          next.delete(thread.id)
        }
      }
      if (!areStringSetsEqual(next, pinnedIdsRef.current)) replacePinnedIds(next)
    })
    return () => { cancelled = true }
  }, [replacePinnedIds, threads])

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
      gtdEnabled,
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
      gtdEnabled,
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
        <button
          onClick={() => togglePin(menuThreadId)}
          className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <Pin
            size={13}
            style={{
              flexShrink: 0,
              color: 'var(--c-text-secondary)',
              fill: effectivePinnedIds.has(menuThreadId) ? 'var(--c-text-secondary)' : 'none',
            }}
          />
          {effectivePinnedIds.has(menuThreadId) ? t.unpinThread : t.pinThread}
        </button>
        {gtdEnabled && !isWorkMode && (
          <>
            <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 0' }} />
            <button
              onClick={() => { markGtdInbox(menuThreadId); setMenuThreadId(null) }}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Inbox size={13} style={{ flexShrink: 0 }} />
              {t.gtdMoveToInbox}
            </button>
            <button
              onClick={() => { markGtdTodo(menuThreadId); setMenuThreadId(null) }}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <CheckCircle size={13} style={{ flexShrink: 0 }} />
              {t.gtdMoveToTodo}
            </button>
            <button
              onClick={() => { markGtdWaiting(menuThreadId); setMenuThreadId(null) }}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Clock size={13} style={{ flexShrink: 0 }} />
              {t.gtdMoveToWaiting}
            </button>
            <button
              onClick={() => { markGtdSomeday(menuThreadId); setMenuThreadId(null) }}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Star size={13} style={{ flexShrink: 0 }} />
              {t.gtdMoveToSomeday}
            </button>
            <button
              onClick={() => { archiveGtdThread(menuThreadId); setMenuThreadId(null) }}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Archive size={13} style={{ flexShrink: 0 }} />
              {t.gtdMoveToArchived}
            </button>
          </>
        )}
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
  const deleteConfirmDialog = (
    <ConfirmDialog
      open={deleteConfirmThreadId !== null}
      title={t.deleteThreadConfirmTitle}
      message={t.deleteThreadConfirmBody}
      confirmLabel={t.deleteThreadConfirm}
      cancelLabel={t.deleteThreadCancel}
      onClose={() => setDeleteConfirmThreadId(null)}
      onConfirm={() => {
        if (deleteConfirmThreadId) void handleDelete(deleteConfirmThreadId)
      }}
    />
  )

  const dragPortal = dragState ? createPortal(
    <div
      style={{
        position: 'fixed',
        left: dragState.x,
        top: dragState.y,
        transform: 'translate(-50%, -50%)',
        zIndex: 9999,
        pointerEvents: 'none',
        padding: '6px 12px',
        borderRadius: '6px',
        background: 'var(--c-bg-page)',
        border: '0.5px solid var(--c-border-subtle)',
        boxShadow: 'var(--c-dropdown-shadow)',
        fontSize: SIDEBAR_ROW_TEXT_SIZE,
        lineHeight: SIDEBAR_ROW_LINE_HEIGHT,
        fontWeight: 'var(--c-sidebar-thread-weight)',
        color: 'var(--c-text-primary)',
        maxWidth: '200px',
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      }}
    >
      {dragState.title}
    </div>,
    document.body,
  ) : null
  const navButtonClass = 'group flex h-[32px] w-full items-center gap-[10px] overflow-hidden whitespace-nowrap rounded-lg px-[8px] text-[15px] text-[var(--c-text-secondary)] transition-colors duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]'
  const navButtonStyle = { fontWeight: 'var(--c-sidebar-nav-weight)' }
  const navLabelClass = 'min-w-0 overflow-hidden whitespace-nowrap transition-opacity duration-100'
  const newThreadNavLabel = isWorkMode ? t.newTask : t.newChat
  const searchNavLabel = isWorkMode ? t.searchTasks : t.searchChats

  return (
    <>
      <aside
      ref={asideRef}
      className={[
        'theme-surface-sidebar flex h-full shrink-0 flex-col overflow-hidden bg-[var(--c-bg-sidebar)]',
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
      <nav className="flex flex-col items-start gap-px pl-[8px] pr-[7px] pt-1">
        <button
          onClick={onNewThread}
          aria-label={newThreadNavLabel}
          className={navButtonClass}
          style={navButtonStyle}
        >
          <span className="flex h-[16px] w-[16px] shrink-0 items-center justify-center">
            <SquarePen size={16} className="shrink-0 transition-transform duration-100 group-hover:scale-[1.05]" />
          </span>
          <span className={navLabelClass}>{newThreadNavLabel}</span>
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
          aria-label={searchNavLabel}
          className={navButtonClass}
          style={navButtonStyle}
        >
          <span className="flex h-[16px] w-[16px] shrink-0 items-center justify-center">
            <Search size={16} className="shrink-0 transition-transform duration-100 group-hover:scale-[1.05]" />
          </span>
          <span className={navLabelClass}>{searchNavLabel}</span>
        </button>

        <button
          onClick={() => navigate('/scheduled-jobs')}
          aria-label={t.scheduledJobs}
          className={navButtonClass}
          style={navButtonStyle}
        >
          <span className="flex h-[16px] w-[16px] shrink-0 items-center justify-center">
            <Clock size={16} className="shrink-0 transition-transform duration-100 group-hover:scale-[1.05]" />
          </span>
          <span className={navLabelClass}>{t.scheduledJobs}</span>
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
          {!isWorkMode && !gtdEnabled && (
            <div className="mb-[12px] mt-1 flex shrink-0 items-center gap-2 px-2">
              <h3
                className="text-[11px] tracking-[0.3px] text-[var(--c-text-tertiary)]"
                style={{ fontWeight: 'var(--c-sidebar-section-weight)' }}
              >
                {t.recents}
              </h3>
            </div>
          )}
          <div className="flex flex-col gap-[2px] flex-1 min-h-0">
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
              className="flex w-full flex-1 flex-col gap-[2px] min-h-0"
              style={{
                opacity: isPrivateModeEffective ? 0 : 1,
                transition: 'opacity 0.15s ease',
                pointerEvents: isPrivateModeEffective ? 'none' : 'auto',
              }}
            >
              {threads.length === 0 ? (
                <p className="overflow-hidden whitespace-nowrap px-2 py-1 text-[12px] text-[var(--c-text-muted)]">{t.recentsEmpty}</p>
              ) : isWorkMode ? (
                <>
                  {/* Pinned section */}
                  <div
                    ref={pinnedDropRef}
                    className={[
                      'rounded-[6px]',
                      dragOverPinned && draggingToPinned ? 'bg-[var(--c-bg-deep)]' : '',
                    ].join(' ')}
                  >
                    <div
                      className="group/pinned flex h-[34px] w-full cursor-pointer select-none items-center px-2 py-0"
                      onMouseDown={(e) => { if (e.detail > 1) e.preventDefault() }}
                      onClick={() => setPinnedExpanded(v => !v)}
                    >
                      <span
                        className="select-none text-[13.5px] leading-[20px] text-[var(--c-text-muted)] transition-colors duration-[80ms] group-hover/pinned:text-[var(--c-text-tertiary)]"
                        style={{ fontWeight: PROJECT_GROUP_LABEL_WEIGHT }}
                      >
                        {t.pinnedSection}
                      </span>
                      <span className="ml-1 shrink-0 opacity-0 group-hover/pinned:opacity-100 text-[var(--c-text-muted)] transition-opacity duration-[80ms]">
                        <ChevronRight size={12} className={['transition-transform duration-150', pinnedExpanded ? 'rotate-90' : 'rotate-0'].join(' ')} />
                      </span>
                    </div>
                    {pinnedExpanded && pinnedWorkThreads.length === 0 && (
                      renderDropRow(
                        <Pin size={14} className="shrink-0 opacity-50" />,
                        dragOverPinned && draggingToPinned ? t.letGo : t.dragToPin,
                        dragOverPinned && draggingToPinned,
                      )
                    )}
                    {pinnedExpanded && pinnedWorkThreads
                      .map(th => renderThread(th, { showStatusDot: true }))}
                  </div>
                  {ProjectSidebarView}
                </>
              ) : gtdEnabled ? (
                GtdSidebarView
              ) : (
                <>
                  {starredThreads.map(thread => renderThread(thread))}
                  {regularThreads.map(thread => renderThread(thread))}
                </>
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
      {deleteConfirmDialog}
      {dragPortal}
    </>
  )
}
