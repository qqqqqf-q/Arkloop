import { useNavigate, useParams, useLocation } from 'react-router-dom'
import {
  SquarePen,
  Search,
  FolderKanban,
  SearchCheck,
  Scale,
  PanelLeftClose,
  Bolt,
} from 'lucide-react'
import type { SettingsTab } from './SettingsModal'
import type { ThreadResponse, MeResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  me: MeResponse | null
  threads: ThreadResponse[]
  runningThreadIds: Set<string>
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

  const userInitial = me?.display_name?.charAt(0).toUpperCase() ?? '?'

  return (
    <aside
      className={[
        'flex h-full shrink-0 flex-col overflow-hidden bg-[var(--c-bg-sidebar)] transition-all duration-300',
        collapsed ? 'w-0' : 'w-[288px]',
      ].join(' ')}
      style={collapsed ? undefined : { borderRight: '0.5px solid rgba(0,0,0,0.16)' }}
    >
      <div
        className={[
          'flex min-h-0 min-w-[288px] flex-1 flex-col transition-opacity',
          collapsed ? 'opacity-0 duration-100' : 'opacity-100 delay-150 duration-200',
        ].join(' ')}
      >
      {/* 顶部标题栏 */}
      <div className="flex min-h-[52px] items-center justify-between px-4 py-3">
        <h1 className="text-[15px] font-semibold tracking-tight text-[var(--c-text-primary)]">Arkloop</h1>
        <button
          onClick={onToggleCollapse}
          className="flex h-6 w-6 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
        >
          <PanelLeftClose size={16} />
        </button>
      </div>

      {/* 导航 */}
      <nav className="flex flex-col gap-px px-2">
        <button
          onClick={onNewThread}
          className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <SquarePen size={15} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.newChat}</span>
        </button>

        <button
          onClick={() => {
            const basePath = location.pathname.replace(/\/search$/, '') || '/'
            const searchPath = basePath.endsWith('/') ? `${basePath}search` : `${basePath}/search`
            navigate(searchPath)
          }}
          className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <Search size={15} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.chats}</span>
        </button>

        <button className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <FolderKanban size={15} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.projects}</span>
        </button>

        <button className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <SearchCheck size={15} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.retrieve}</span>
        </button>

        <button className="group flex h-9 items-center gap-2.5 rounded-lg px-2 text-[15px] text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <Scale size={15} className="shrink-0 transition-transform duration-200 group-hover:scale-[1.1]" />
          <span>{t.legal}</span>
        </button>
      </nav>

      {/* 最近会话 */}
      <div className="mt-6 flex min-h-0 flex-1 flex-col overflow-y-auto px-2">
        <h3 className="mb-[12px] shrink-0 px-2 text-sm font-medium tracking-[0.3px] text-[var(--c-text-muted)]">{t.recents}</h3>
        <div className="flex flex-col gap-[2px]">
          {threads.map((thread) => (
            <button
              key={thread.id}
              onClick={() => navigate(`/t/${thread.id}`)}
              className={[
                'flex items-center gap-2 rounded-[5px] px-2 py-[8px] text-left text-sm font-[350] transition-colors',
                thread.id === threadId
                  ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)]',
              ].join(' ')}
            >
              <span className="min-w-0 flex-1 truncate">{threadTitle(thread, t.untitled)}</span>
              {runningThreadIds.has(thread.id) && (
                <span className="shrink-0 h-3 w-3 animate-spin rounded-full border border-[var(--c-text-muted)] border-t-transparent" />
              )}
            </button>
          ))}
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
            className="flex h-[37px] w-[37px] shrink-0 items-center justify-center rounded-full text-[15px] font-medium"
            style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
          >
            {userInitial}
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-[2px] text-left">
            <div className="truncate text-sm font-medium text-[var(--c-text-secondary)]">
              {me?.display_name ?? t.loading}
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
  )
}
