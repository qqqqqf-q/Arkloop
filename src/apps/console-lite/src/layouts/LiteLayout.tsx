import { useEffect, useState, useCallback, useRef, useMemo, type ReactNode } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Sparkles,
  KeyRound,
  Wrench,
  Play,
  Blocks,
  Settings,
  ShieldCheck,
  ShieldAlert,
  Loader2,
} from 'lucide-react'
import { getMe, logout, isApiError, type MeResponse } from '../api'
import { LiteSettingsModal } from '../components/SettingsModal'
import { OperationHistoryModal } from '../components/OperationHistoryModal'
import { useLocale } from '../contexts/LocaleContext'
import { useOperations } from '../contexts/OperationContext'
import type { LocaleStrings } from '../locales'

type Props = {
  accessToken: string
  onLoggedOut: () => void
}

type NavItem = {
  label: string
  path: string
  icon: ReactNode
}

function buildNavItems(t: LocaleStrings): NavItem[] {
  return [
    { label: t.nav.dashboard, path: '/dashboard', icon: <LayoutDashboard size={17} /> },
    { label: t.nav.agents,    path: '/agents',    icon: <Sparkles size={17} /> },
    { label: t.nav.models,    path: '/models',    icon: <KeyRound size={17} /> },
    { label: t.nav.tools,     path: '/tools?group=',          icon: <Wrench size={17} /> },
    { label: t.nav.runs,      path: '/runs',                  icon: <Play size={17} /> },
    { label: t.nav.modules,   path: '/modules?cat=memory',    icon: <Blocks size={17} /> },
    { label: t.nav.security,  path: '/security',              icon: <ShieldAlert size={17} /> },
    { label: t.nav.settings,  path: '/settings?section=general', icon: <Settings size={17} /> },
  ]
}

export type LiteOutletContext = {
  accessToken: string
  onLoggedOut: () => void
  me: MeResponse | null
}

export function LiteLayout({ accessToken, onLoggedOut }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useLocale()
  const [me, setMe] = useState<MeResponse | null>(null)
  const [meLoaded, setMeLoaded] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const { operations, activeCount, historyOpen, setHistoryOpen } = useOperations()
  const mountedRef = useRef(true)

  const navItems = useMemo(() => buildNavItems(t), [t])

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
      } finally {
        if (mountedRef.current) setMeLoaded(true)
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

  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'
  const context: LiteOutletContext = { accessToken, onLoggedOut, me }

  if (!meLoaded) {
    return (
      <div className="flex h-screen items-center justify-center bg-[var(--c-bg-page)]">
        <span className="text-sm text-[var(--c-text-muted)]">{t.loading}</span>
      </div>
    )
  }

  if (!me?.permissions?.includes('platform.admin')) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-3 bg-[var(--c-bg-page)]">
        <ShieldCheck size={32} className="text-[var(--c-text-muted)]" />
        <p className="text-sm font-medium text-[var(--c-text-secondary)]">{t.accessDenied}</p>
        <p className="text-xs text-[var(--c-text-muted)]">{t.noAdminAccess}</p>
        <button
          onClick={onLoggedOut}
          className="mt-2 text-xs text-[var(--c-text-muted)] underline hover:opacity-70"
        >
          {t.signOut}
        </button>
      </div>
    )
  }

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--c-bg-page)]">
      <aside className="flex h-full w-[220px] shrink-0 flex-col border-r border-[var(--c-border-console)] bg-[var(--c-bg-sidebar)]">
        <div className="flex min-h-[46px] items-center px-4 py-3">
          <div className="flex items-center gap-2">
            <h1 className="text-sm font-semibold tracking-wide text-[var(--c-text-primary)]">Arkloop</h1>
            <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
              Lite
            </span>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto p-2">
          <div className="flex flex-col gap-[3px]">
            {navItems.map((item) => {
              const basePath = item.path.split('?')[0]
              const active = location.pathname === basePath || location.pathname.startsWith(basePath + '/')
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
          </div>
        </nav>

        {operations.length > 0 && (
          <div className="border-t border-[var(--c-border-console)] px-2 py-2">
            <button
              onClick={() => setHistoryOpen(true)}
              className="flex w-full items-center gap-3 rounded-xl px-3 py-[10px] transition-colors hover:bg-[var(--c-bg-sub)]"
              style={{ border: '0.5px solid var(--c-border-console)' }}
            >
              <div className="flex h-[32px] w-[32px] shrink-0 items-center justify-center rounded-full bg-[var(--c-bg-tag)]">
                {activeCount > 0
                  ? <Loader2 size={15} className="animate-spin text-amber-500" />
                  : <Blocks size={15} className="text-[var(--c-text-muted)]" />
                }
              </div>
              <div className="flex min-w-0 flex-1 flex-col gap-[2px] text-left">
                <div className="truncate text-xs font-medium text-[var(--c-text-secondary)]">
                  {t.nav.installTasks}
                </div>
                <div className="text-[10px] font-normal text-[var(--c-text-tertiary)]">
                  {activeCount > 0
                    ? `${activeCount} running · ${operations.length} total`
                    : `${operations.length} completed`
                  }
                </div>
              </div>
            </button>
          </div>
        )}

        <div className="mt-auto border-t border-[var(--c-border-console)] px-3 py-3">
          <div className="flex items-center gap-2">
            <div
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-medium text-[var(--c-text-secondary)]"
              style={{ background: 'var(--c-avatar-console-bg)' }}
            >
              {userInitial}
            </div>
            <div className="min-w-0 flex-1 truncate text-sm font-medium text-[var(--c-text-secondary)]">
              {me?.username ?? '...'}
            </div>
            <button
              onClick={() => setSettingsOpen(true)}
              className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
              title={t.settings}
            >
              <Settings size={14} />
            </button>
          </div>
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={context} />
      </main>

      {settingsOpen && (
        <LiteSettingsModal
          me={me}
          onClose={() => setSettingsOpen(false)}
          onLogout={handleLogout}
        />
      )}

      {historyOpen && (
        <OperationHistoryModal onClose={() => setHistoryOpen(false)} />
      )}
    </div>
  )
}
