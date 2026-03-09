import { useEffect, useState, useCallback, useRef, useMemo, type ReactNode } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Play, ClipboardList, AlertTriangle,
  KeyRound, Bot, Plug, Sparkles,
  Key, Webhook,
  ShieldCheck,
  Users, UsersRound, FolderOpen, UserPlus,
  Package, Receipt, BadgeCheck, BarChart3,
  Flag, Ticket, Gift, Coins, Megaphone, Mic, Mail, AlignLeft,
  PanelLeftClose, PanelLeftOpen, ChevronDown,
  Settings, ScrollText,
  Wrench, SlidersHorizontal,
} from 'lucide-react'
import { getMe, logout, isApiError, type MeResponse } from '../api'
import { ConsoleSettingsModal } from '../components/SettingsModal'
import { useLocale } from '../contexts/LocaleContext'
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

type NavGroup = {
  id: string
  label: string
  items: NavItem[]
}

function buildNavGroups(t: LocaleStrings): NavGroup[] {
  return [
    {
      id: 'operations',
      label: t.groups.operations,
      items: [
        { label: t.nav.dashboard, path: '/dashboard', icon: <LayoutDashboard size={17} /> },
        { label: t.nav.runs,      path: '/runs',  icon: <Play size={17} /> },
        { label: t.nav.auditLogs, path: '/audit', icon: <ClipboardList size={17} /> },
        { label: t.nav.reports,   path: '/reports', icon: <AlertTriangle size={17} /> },
      ],
    },
    {
      id: 'platform',
      label: t.groups.platform,
      items: [
        { label: t.nav.featureFlags, path: '/feature-flags', icon: <Flag size={17} /> },
        { label: t.nav.users,        path: '/users',         icon: <Users size={17} /> },
        { label: t.nav.registration, path: '/registration',  icon: <UserPlus size={17} /> },
        { label: t.nav.inviteCodes,   path: '/invite-codes',  icon: <Ticket size={17} /> },
        { label: t.nav.redemptionCodes, path: '/redemption-codes', icon: <Gift size={17} /> },
        { label: t.nav.creditsAdmin, path: '/credits-admin', icon: <Coins size={17} /> },
        { label: t.nav.broadcasts, path: '/broadcasts', icon: <Megaphone size={17} /> },
        { label: t.nav.email, path: '/email', icon: <Mail size={17} /> },
      ],
    },
    {
      id: 'configuration',
      label: t.groups.configuration,
      items: [
        { label: t.nav.credentials,      path: '/providers',       icon: <KeyRound size={17} /> },
        { label: t.nav.tools,            path: '/tools',            icon: <Wrench size={17} /> },
        { label: t.nav.mcpConfigs,       path: '/mcp-configs',      icon: <Plug size={17} /> },
        { label: t.nav.agents,           path: '/personas',           icon: <Sparkles size={17} /> },
        { label: t.nav.asrCredentials,   path: '/asr-credentials',  icon: <Mic size={17} /> },
        { label: t.nav.titleSummarizer,  path: '/title-summarizer', icon: <AlignLeft size={17} /> },

        { label: t.nav.executionGovernance, path: '/execution-governance', icon: <SlidersHorizontal size={17} /> },
      ],
    },
    {
      id: 'billing',
      label: t.groups.billing,
      items: [
        { label: t.nav.usage,         path: '/usage',         icon: <BarChart3 size={17} /> },
        { label: t.nav.plans,         path: '/plans',         icon: <Package size={17} /> },
        { label: t.nav.subscriptions, path: '/subscriptions', icon: <Receipt size={17} /> },
        { label: t.nav.entitlements,  path: '/entitlements',  icon: <BadgeCheck size={17} /> },
      ],
    },
    {
      id: 'security',
      label: t.groups.security,
      items: [
        { label: t.nav.ipRules,       path: '/ip-rules',       icon: <ShieldCheck size={17} /> },
        { label: t.nav.captcha,       path: '/captcha',        icon: <Bot size={17} /> },
        { label: t.nav.gatewayConfig, path: '/gateway-config', icon: <Settings size={17} /> },
        { label: t.nav.accessLog,    path: '/access-log',    icon: <ScrollText size={17} /> },
      ],
    },
    {
      id: 'integration',
      label: t.groups.integration,
      items: [
        { label: t.nav.apiKeys,   path: '/api-keys',  icon: <Key size={17} /> },
        { label: t.nav.webhooks,  path: '/webhooks',  icon: <Webhook size={17} /> },
      ],
    },
    {
      id: 'organization',
      label: t.groups.organization,
      items: [
        { label: t.nav.members,  path: '/members',  icon: <Users size={17} /> },
        { label: t.nav.teams,    path: '/teams',    icon: <UsersRound size={17} /> },
        { label: t.nav.projects, path: '/projects', icon: <FolderOpen size={17} /> },
      ],
    },
  ]
}

export type ConsoleOutletContext = {
  accessToken: string
  onLoggedOut: () => void
  me: MeResponse | null
}

export function ConsoleLayout({ accessToken, onLoggedOut }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useLocale()
  const [me, setMe] = useState<MeResponse | null>(null)
  const [meLoaded, setMeLoaded] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set())
  const [settingsOpen, setSettingsOpen] = useState(false)
  const mountedRef = useRef(true)

  const navGroups = useMemo(() => buildNavGroups(t), [t])

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

  const toggleGroup = useCallback((groupId: string) => {
    setCollapsedGroups((prev) => {
      const next = new Set(prev)
      if (next.has(groupId)) {
        next.delete(groupId)
      } else {
        next.add(groupId)
      }
      return next
    })
  }, [])

  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'
  const context: ConsoleOutletContext = { accessToken, onLoggedOut, me }

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

        <nav className="flex-1 overflow-y-auto p-2">
          {navGroups.map((group, groupIdx) => {
            const collapsed = collapsedGroups.has(group.id)
            return (
              <div key={group.id}>
                {groupIdx > 0 && (
                  <div className="mx-2 my-2 border-t border-[var(--c-border-console)]" />
                )}
                <button
                  onClick={() => toggleGroup(group.id)}
                  className="flex w-full items-center justify-between rounded px-2 py-1.5 transition-colors hover:bg-[var(--c-bg-sub)]"
                >
                  <span className="text-[10px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
                    {group.label}
                  </span>
                  <ChevronDown
                    size={12}
                    className={[
                      'text-[var(--c-text-muted)] transition-transform duration-200',
                      collapsed ? '-rotate-90' : 'rotate-0',
                    ].join(' ')}
                  />
                </button>
                <div className={`nav-group-content${collapsed ? ' nav-group-collapsed' : ''}`}>
                  <div className="flex flex-col gap-[3px]" style={{ overflow: 'hidden', minHeight: 0 }}>
                    {group.items.map((item) => {
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
                  </div>
                </div>
              </div>
            )
          })}
        </nav>

        {/* 用户信息 */}
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
        <ConsoleSettingsModal
          me={me}
          onClose={() => setSettingsOpen(false)}
          onLogout={handleLogout}
        />
      )}
    </div>
  )
}
