import { useEffect, useState, useCallback, useRef } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { ClipboardList, Play, Cpu, Building2, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { getMe, logout, isApiError, type MeResponse } from '../api'

type Props = {
  accessToken: string
  onLoggedOut: () => void
}

type NavItem = {
  label: string
  path: string
  icon: React.ReactNode
}

const NAV_ITEMS: NavItem[] = [
  { label: 'Audit', path: '/audit', icon: <ClipboardList size={17} /> },
  { label: 'Runs', path: '/runs', icon: <Play size={17} /> },
  { label: 'Providers', path: '/providers', icon: <Cpu size={17} /> },
  { label: 'Orgs', path: '/orgs', icon: <Building2 size={17} /> },
]

export type ConsoleOutletContext = {
  accessToken: string
  onLoggedOut: () => void
  me: MeResponse | null
}

export function ConsoleLayout({ accessToken, onLoggedOut }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const [me, setMe] = useState<MeResponse | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    void (async () => {
      try {
        const meResp = await getMe(accessToken)
        if (!mountedRef.current) return
        setMe(meResp)
      } catch (err) {
        if (!mountedRef.current) return
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
        }
      }
    })()
  }, [accessToken, onLoggedOut])

  const handleLogout = useCallback(async () => {
    try {
      await logout(accessToken)
    } catch (err) {
      if (isApiError(err) && err.status !== 401) return
    }
    onLoggedOut()
  }, [accessToken, onLoggedOut])

  const userInitial = me?.display_name?.charAt(0).toUpperCase() ?? '?'

  const context: ConsoleOutletContext = { accessToken, onLoggedOut, me }

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--c-bg-page)]">
      {sidebarCollapsed && (
        <button
          onClick={() => setSidebarCollapsed(false)}
          className="fixed left-3 top-3 z-40 flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
        >
          <PanelLeftOpen size={18} />
        </button>
      )}

      <aside
        className={[
          'flex h-full shrink-0 flex-col border-r border-[var(--c-border-console)] bg-[var(--c-bg-sidebar)] transition-all duration-300',
          sidebarCollapsed ? 'w-0 overflow-hidden border-r-0' : 'w-[240px]',
        ].join(' ')}
      >
        {/* 标题栏 */}
        <div className="flex min-h-[46px] items-center justify-between px-4 py-3">
          <div className="flex items-center gap-2">
            <h1 className="text-sm font-semibold tracking-wide text-[var(--c-text-primary)]">Arkloop</h1>
            <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
              Console
            </span>
          </div>
          <button
            onClick={() => setSidebarCollapsed(true)}
            className="flex h-5 w-5 items-center justify-center text-[var(--c-text-tertiary)] transition-opacity hover:opacity-70"
          >
            <PanelLeftClose size={18} />
          </button>
        </div>

        {/* 导航 */}
        <nav className="flex flex-col gap-[3px] p-2">
          {NAV_ITEMS.map((item) => {
            const active = location.pathname.startsWith(item.path)
            return (
              <button
                key={item.path}
                onClick={() => navigate(item.path)}
                className={[
                  'flex h-[30px] items-center gap-[11px] rounded-[5px] px-2 py-[7px] text-sm font-medium transition-colors',
                  active
                    ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                ].join(' ')}
              >
                <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
                  {item.icon}
                </span>
                <span>{item.label}</span>
              </button>
            )
          })}
        </nav>

        {/* 用户信息 */}
        <div className="mt-auto flex min-h-[56px] items-center gap-3 border-t border-[var(--c-border-console)] px-4 py-3">
          <div
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-medium text-[var(--c-text-secondary)]"
            style={{ background: 'var(--c-avatar-console-bg)' }}
          >
            {userInitial}
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-[2px]">
            <div className="truncate text-sm font-medium text-[var(--c-text-secondary)]">
              {me?.display_name ?? '...'}
            </div>
            <button
              onClick={handleLogout}
              className="text-left text-xs font-normal text-[var(--c-text-muted)] transition-opacity hover:opacity-70"
            >
              退出登录
            </button>
          </div>
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={context} />
      </main>
    </div>
  )
}
