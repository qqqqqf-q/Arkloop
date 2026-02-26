import { useState, useRef, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate, useParams, useLocation } from 'react-router-dom'
import { motion, AnimatePresence } from 'framer-motion'
import {
  SquarePen,
  Search,
  FolderKanban,
  SearchCheck,
  Scale,
  PanelLeftClose,
  Bolt,
  Glasses,
  MoreHorizontal,
  Star,
  Share2,
} from 'lucide-react'
import type { SettingsTab } from './SettingsModal'
import type { ThreadResponse, MeResponse } from '../api'
import { listStarredThreadIds, starThread, unstarThread } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  me: MeResponse | null
  threads: ThreadResponse[]
  runningThreadIds: Set<string>
  isPrivateMode: boolean
  accessToken: string
  onNewThread: () => void
  onLogout: () => void
  onOpenSettings: (tab?: SettingsTab) => void
  collapsed: boolean
  onToggleCollapse: () => void
}

function threadTitle(thread: ThreadResponse, untitled: string): string {
  const title = (thread.title ?? '').trim()
  return title.length > 0 ? title : untitled
}

export function Sidebar({
  me,
  threads,
  runningThreadIds,
  isPrivateMode,
  accessToken,
  onNewThread,
  onLogout: _onLogout,
  onOpenSettings,
  collapsed,
  onToggleCollapse,
}: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const { threadId } = useParams<{ threadId: string }>()
  const { t } = useLocale()

  const [starredIds, setStarredIds] = useState<string[]>([])
  const [menuThreadId, setMenuThreadId] = useState<string | null>(null)
  const [menuPos, setMenuPos] = useState<{ x: number; y: number }>({ x: 0, y: 0 })
  const menuRef = useRef<HTMLDivElement>(null)

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

  // 点击外部关闭菜单
  useEffect(() => {
    if (!menuThreadId) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuThreadId(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menuThreadId])

  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'

  return (
    <>
    <aside
      className={[
        'flex h-full shrink-0 flex-col overflow-hidden bg-[var(--c-bg-sidebar)] transition-all duration-300',
        collapsed ? 'w-0' : 'w-[304px]',
      ].join(' ')}
      style={collapsed ? undefined : { borderRight: '0.5px solid rgba(0,0,0,0.16)' }}
    >
      <div
        className={[
          'flex min-h-0 min-w-[304px] flex-1 flex-col transition-opacity',
          collapsed ? 'opacity-0 duration-100' : 'opacity-100 delay-150 duration-200',
        ].join(' ')}
      >
      {/* 顶部标题栏 */}
      <div className="flex min-h-[56px] items-center justify-between px-4 py-3">
        <div className="flex items-center gap-2 overflow-hidden">
          <h1 className="text-[16px] font-semibold tracking-tight text-[var(--c-text-primary)] shrink-0">Arkloop</h1>
          {/* 从右滑入 */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
              opacity: isPrivateMode ? 1 : 0,
              transform: isPrivateMode ? 'translateX(0)' : 'translateX(14px)',
              transition: 'opacity 0.18s ease, transform 0.18s ease',
              pointerEvents: 'none',
            }}
          >
            <span className="h-[5px] w-[5px] shrink-0 rounded-full bg-[var(--c-text-tertiary)]" style={{ opacity: 0.5 }} />
            <span className="text-[12px] font-medium text-[var(--c-text-tertiary)] whitespace-nowrap">{t.incognitoMode}</span>
          </div>
        </div>
        <button
          onClick={onToggleCollapse}
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
        >
          <PanelLeftClose size={16} />
        </button>
      </div>

      {/* 导航 */}
      <nav className="flex flex-col gap-px px-2">
        <button
          onClick={onNewThread}
          className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[16px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <SquarePen size={16} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.newChat}</span>
        </button>

        <button
          onClick={() => {
            const basePath = location.pathname.replace(/\/search$/, '') || '/'
            const searchPath = basePath.endsWith('/') ? `${basePath}search` : `${basePath}/search`
            navigate(searchPath)
          }}
          className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[16px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <Search size={16} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.chats}</span>
        </button>

        <button className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[16px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <FolderKanban size={16} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.projects}</span>
        </button>

        <button className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[16px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <SearchCheck size={16} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.retrieve}</span>
        </button>

        <button className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[16px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <Scale size={16} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.legal}</span>
        </button>
      </nav>

      {/* 最近会话 */}
      <div className="mt-6 flex min-h-0 flex-1 flex-col overflow-y-auto px-2">
        <div className="mb-[12px] flex shrink-0 items-center gap-2 px-2">
          <h3 className="text-[14px] font-medium tracking-[0.3px] text-[var(--c-text-muted)]">
            {t.recents}
          </h3>
        </div>
        <div className="flex flex-col gap-[2px]">
          {/* incognito 占位：平滑展开/收起 */}
          <div
            style={{
              display: 'grid',
              gridTemplateRows: isPrivateMode ? '1fr' : '0fr',
              opacity: isPrivateMode ? 1 : 0,
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

          {/* 线程列表：始终渲染，淡入避免位移感 */}
          <div
            className="w-full flex flex-col gap-[2px]"
            style={{
              opacity: isPrivateMode ? 0 : 1,
              transition: 'opacity 0.15s ease',
              pointerEvents: isPrivateMode ? 'none' : 'auto',
            }}
          >
            {threads.length === 0 ? (
              <p className="px-2 py-1 text-[12px] text-[var(--c-text-muted)]">{t.recentsEmpty}</p>
            ) : (() => {
              const starredSet = new Set(starredIds)
              const starredThreads = starredIds
                .map((id) => threads.find((t) => t.id === id))
                .filter((t): t is ThreadResponse => t !== undefined)
              const regularThreads = threads.filter((t) => !starredSet.has(t.id))

              const renderThread = (thread: ThreadResponse, section: 'starred' | 'regular') => (
                <motion.div
                  key={`${thread.id}-${section}`}
                  initial={{ opacity: 0, scale: 0.97 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.97 }}
                  transition={{ duration: 0.15, ease: 'easeOut' }}
                  className={[
                    'group relative flex w-full items-center rounded-[6px]',
                    thread.id === threadId
                      ? 'bg-[var(--c-bg-deep)]'
                      : 'hover:bg-[var(--c-bg-deep)]',
                  ].join(' ')}
                >
                  <button
                    onClick={() => navigate(`/t/${thread.id}`)}
                    className={[
                      'flex min-w-0 flex-1 items-center gap-2 px-2 py-[9px] text-left text-[13px] font-[350]',
                      thread.id === threadId
                        ? 'text-[var(--c-text-primary)]'
                        : 'text-[var(--c-text-secondary)]',
                    ].join(' ')}
                  >
                    {starredSet.has(thread.id) && (
                      <Star size={11} className="shrink-0 fill-[var(--c-text-muted)] text-[var(--c-text-muted)] opacity-70" />
                    )}
                    <span className="min-w-0 flex-1 truncate">{threadTitle(thread, t.untitled)}</span>
                    {runningThreadIds.has(thread.id) && (
                      <span className="shrink-0 h-3 w-3 animate-spin rounded-full border border-[var(--c-text-muted)] border-t-transparent" />
                    )}
                  </button>
                  {/* hover 时显示三点按钮 */}
                  <button
                    onClick={(e) => openMenu(e, thread.id)}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = 'rgba(0,0,0,0.18)' }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = 'transparent' }}
                    className={[
                      'mr-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-md',
                      'text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]',
                      'opacity-0 group-hover:opacity-100',
                      menuThreadId === thread.id ? '!opacity-100 !bg-[rgba(0,0,0,0.18)]' : '',
                    ].join(' ')}
                    style={{ background: 'transparent' }}
                  >
                    <MoreHorizontal size={14} />
                  </button>
                </motion.div>
              )

              return (
                <AnimatePresence initial={false}>
                  {starredThreads.map((t) => renderThread(t, 'starred'))}
                  {starredThreads.length > 0 && regularThreads.length > 0 && (
                    <motion.div
                      key="__divider__"
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      exit={{ opacity: 0 }}
                      transition={{ duration: 0.15 }}
                      className="my-1 mx-2 h-px bg-[var(--c-bg-deep)]"
                    />
                  )}
                  {regularThreads.map((t) => renderThread(t, 'regular'))}
                </AnimatePresence>
              )
            })()}
          </div>
        </div>
      </div>

      {/* 用户信息 */}
      <div className="mt-auto p-2" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
        <button
          onClick={() => onOpenSettings('account')}
          className="flex w-full items-center gap-3 rounded-xl px-3 py-[10px] transition-colors hover:bg-[var(--c-bg-deep)]"
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

        {/* 底部快捷图标 */}
        <div className="mt-1 flex items-center gap-[2px] px-1">
          <button
            onClick={() => onOpenSettings('settings')}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-icon)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
          >
            <Bolt size={15} />
          </button>
        </div>
      </div>
      </div>
    </aside>

    {/* 三点菜单 - portal 挂到 body 避免被 overflow 裁切 */}
    {menuThreadId !== null && createPortal(
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
            onClick={() => toggleStar(menuThreadId)}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-[13px] transition-colors duration-100"
            style={{ color: 'var(--c-text-secondary)', background: 'var(--c-bg-menu)', borderRadius: '8px' }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = 'var(--c-bg-deep)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = 'var(--c-bg-menu)' }}
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
            disabled
            className="flex w-full items-center gap-2 px-3 py-1.5 text-[13px]"
            style={{ color: 'var(--c-text-muted)', background: 'var(--c-bg-menu)', borderRadius: '8px', opacity: 0.4, cursor: 'not-allowed' }}
          >
            <Share2 size={13} style={{ flexShrink: 0 }} />
            {t.shareThread}
          </button>
        </div>
      </div>,
      document.body,
    )}
    </>
  )
}
