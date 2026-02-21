import { useNavigate, useParams } from 'react-router-dom'
import {
  Plus,
  MessagesSquare,
  FolderKanban,
  SearchCheck,
  Scale,
  PanelLeftClose,
  ChevronsUpDown,
} from 'lucide-react'
import type { ThreadResponse, MeResponse } from '../api'

type Props = {
  me: MeResponse | null
  threads: ThreadResponse[]
  runningThreadIds: Set<string>
  onNewThread: () => void
  onLogout: () => void
  collapsed: boolean
  onToggleCollapse: () => void
}

function threadTitle(thread: ThreadResponse): string {
  const title = (thread.title ?? '').trim()
  return title.length > 0 ? title : '未命名会话'
}

export function Sidebar({
  me,
  threads,
  runningThreadIds,
  onNewThread,
  onLogout,
  collapsed,
  onToggleCollapse,
}: Props) {
  const navigate = useNavigate()
  const { threadId } = useParams<{ threadId: string }>()

  const userInitial = me?.display_name?.charAt(0).toUpperCase() ?? '?'

  return (
    <aside
      className={[
        'flex h-full shrink-0 flex-col border-r border-[var(--c-border)] bg-[var(--c-bg-sidebar)] transition-all duration-300',
        collapsed ? 'w-0 overflow-hidden border-r-0' : 'w-[288px]',
      ].join(' ')}
    >
      {/* 顶部标题栏 */}
      <div className="flex min-h-[46px] items-center justify-between px-[18px] py-3">
        <h1 className="text-lg font-medium text-[var(--c-text-primary)]">Arkloop</h1>
        <button
          onClick={onToggleCollapse}
          className="flex h-5 w-5 items-center justify-center text-[var(--c-text-tertiary)] transition-opacity hover:opacity-70"
        >
          <PanelLeftClose size={18} />
        </button>
      </div>

      {/* 导航 */}
      <nav className="flex flex-col gap-[3px] p-2">
        <button
          onClick={onNewThread}
          className="flex h-[30px] items-center gap-[11px] rounded-[5px] px-2 py-[7px] text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
        >
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
            <span className="flex h-[22px] w-[22px] items-center justify-center rounded-full bg-[var(--c-bg-plus)]">
              <Plus size={12} />
            </span>
          </span>
          <span>New chat</span>
        </button>

        <button
          onClick={() => navigate('/')}
          className="flex h-[30px] items-center gap-[11px] rounded-[5px] px-2 py-[7px] text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
        >
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
            <MessagesSquare size={17} />
          </span>
          <span>Chats</span>
        </button>

        <button className="flex h-[30px] items-center gap-[11px] rounded-[5px] px-2 py-[7px] text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]">
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
            <FolderKanban size={17} />
          </span>
          <span>Projects</span>
        </button>

        <button className="flex h-[30px] items-center gap-[11px] rounded-[5px] px-2 py-[7px] text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]">
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
            <SearchCheck size={17} />
          </span>
          <span>Retrieve</span>
        </button>

        <button className="flex h-[30px] items-center gap-[11px] rounded-[5px] px-2 py-[7px] text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]">
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
            <Scale size={17} />
          </span>
          <span>Legal</span>
        </button>
      </nav>

      {/* 最近会话 */}
      <div className="mt-6 flex min-h-0 flex-1 flex-col overflow-y-auto px-2">
        <h3 className="mb-[12px] shrink-0 px-2 text-xs font-normal tracking-[0.5px] text-[var(--c-text-muted)]">Recents</h3>
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
              <span className="min-w-0 flex-1 truncate">{threadTitle(thread)}</span>
              {runningThreadIds.has(thread.id) && (
                <span className="shrink-0 h-3 w-3 animate-spin rounded-full border border-[var(--c-text-muted)] border-t-transparent" />
              )}
            </button>
          ))}
        </div>
      </div>

      {/* 用户信息 */}
      <div className="mt-auto flex min-h-[62px] items-center gap-3 border-t border-[var(--c-border)] bg-[var(--c-bg-sidebar)] px-4 py-[14px]">
        <div
          className="flex h-[37px] w-[37px] shrink-0 items-center justify-center rounded-full text-[15px] font-medium"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-[2px]">
          <div className="truncate text-sm font-medium text-[var(--c-text-secondary)]">
            {me?.display_name ?? '加载中...'}
          </div>
          <button
            onClick={onLogout}
            className="text-left text-xs font-normal text-[var(--c-text-tertiary)] transition-opacity hover:opacity-70"
          >
            退出登录
          </button>
        </div>
        <button className="flex h-4 w-4 shrink-0 items-center justify-center text-[var(--c-text-icon2)] transition-opacity hover:opacity-70">
          <ChevronsUpDown size={16} />
        </button>
      </div>
    </aside>
  )
}
