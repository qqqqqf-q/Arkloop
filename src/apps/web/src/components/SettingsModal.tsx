import { useState, useRef, useEffect } from 'react'
import type { LucideIcon } from 'lucide-react'
import {
  X,
  User,
  Settings,
  BarChart2,
  CalendarClock,
  Mail,
  Database,
  Globe,
  Sliders,
  Zap,
  Cable,
  Layers,
  HelpCircle,
  LogOut,
  ArrowUpRight,
  ChevronDown,
  Monitor,
  Sun,
  Moon,
} from 'lucide-react'
import type { MeResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { useTheme } from '../contexts/ThemeContext'
import type { Locale } from '../locales'
import type { Theme } from '../storage'


export type SettingsTab =
  | 'account' | 'settings' | 'usage' | 'scheduled'
  | 'mail' | 'data' | 'browser' | 'personal'
  | 'skills' | 'connectors' | 'integrations'

type NavItem = { key: SettingsTab; icon: LucideIcon }

const NAV_ITEMS: NavItem[] = [
  { key: 'account',      icon: User         },
  { key: 'settings',     icon: Settings     },
  { key: 'usage',        icon: BarChart2    },
  { key: 'scheduled',    icon: CalendarClock },
  { key: 'mail',         icon: Mail         },
  { key: 'data',         icon: Database     },
  { key: 'browser',      icon: Globe        },
  { key: 'personal',     icon: Sliders      },
  { key: 'skills',       icon: Zap          },
  { key: 'connectors',   icon: Cable        },
  { key: 'integrations', icon: Layers       },
]

type Props = {
  me: MeResponse | null
  initialTab?: SettingsTab
  onClose: () => void
  onLogout: () => void
}

export function SettingsModal({ me, initialTab = 'account', onClose, onLogout }: Props) {
  const { t, locale, setLocale } = useLocale()
  const { theme, setTheme } = useTheme()
  const [activeKey, setActiveKey] = useState<SettingsTab>(initialTab)
  const userInitial = me?.display_name?.charAt(0).toUpperCase() ?? '?'
  const activeLabel = t.nav[activeKey as keyof typeof t.nav] ?? t.nav.account

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-[2px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="flex h-[624px] w-[832px] overflow-hidden rounded-2xl shadow-2xl bg-[var(--c-bg-page)]"
        style={{ boxShadow: 'inset 0 0 0 0.5px var(--c-modal-ring)' }}
      >
        {/* 左侧导航 */}
        <div
          className="flex w-[200px] shrink-0 flex-col py-4 bg-[var(--c-bg-sidebar)]"
          style={{ borderRight: '0.5px solid rgba(0,0,0,0.14)' }}
        >
          <div className="mb-2 px-4 py-1">
            <span className="text-sm font-semibold text-[var(--c-text-heading)]">Arkloop</span>
          </div>

          <nav className="flex flex-col gap-[2px] px-2">
            {NAV_ITEMS.map(({ key, icon: Icon }) => (
              <button
                key={key}
                onClick={() => setActiveKey(key)}
                className={[
                  'flex h-8 items-center gap-2 rounded-md px-2 text-sm transition-colors',
                  activeKey === key
                    ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                    : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
                ].join(' ')}
              >
                <Icon size={15} />
                <span>{t.nav[key as keyof typeof t.nav]}</span>
              </button>
            ))}
          </nav>

          <div className="mt-auto px-2">
            <div style={{ borderTop: '0.5px solid var(--c-border-subtle)', marginBottom: '8px' }} />
            <button
              className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
            >
              <HelpCircle size={15} />
              <span>{t.getHelp}</span>
              <ArrowUpRight size={12} style={{ marginLeft: 'auto' }} />
            </button>
          </div>
        </div>

        {/* 右侧内容 */}
        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            <h2 className="text-base font-medium text-[var(--c-text-heading)]">{activeLabel}</h2>
            <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              <X size={16} />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {activeKey === 'account' && (
              <AccountContent
                me={me}
                userInitial={userInitial}
                onLogout={() => { onLogout(); onClose() }}
              />
            )}
            {activeKey === 'settings' && (
              <div className="flex flex-col gap-6">
                <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
                <ThemeContent theme={theme} setTheme={setTheme} label={t.appearance} t={t} />
              </div>
            )}
            {activeKey !== 'account' && activeKey !== 'settings' && (
              <div className="flex h-full items-center justify-center text-sm text-[var(--c-text-muted)]">
                {t.comingSoon}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function AccountContent({
  me,
  userInitial,
  onLogout,
}: {
  me: MeResponse | null
  userInitial: string
  onLogout: () => void
}) {
  const { t } = useLocale()

  return (
    <div className="flex flex-col gap-3">
      <div
        className="flex items-center gap-3 rounded-xl p-4 bg-[var(--c-bg-menu)]"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-lg font-medium"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-sm font-medium text-[var(--c-text-heading)]">
            {me?.display_name ?? t.loading}
          </span>
        </div>
        <button
          onClick={onLogout}
          className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          title={t.logout}
        >
          <LogOut size={15} />
        </button>
      </div>

      <div
        className="rounded-xl p-4 bg-[var(--c-bg-menu)]"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.enterprisePlan}</span>
        </div>
      </div>
    </div>
  )
}

const LOCALE_OPTIONS: { value: Locale; label: string }[] = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
]

function LanguageContent({
  locale,
  setLocale,
  label,
}: {
  locale: Locale
  setLocale: (l: Locale) => void
  label: string
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const currentLabel = LOCALE_OPTIONS.find(o => o.value === locale)?.label ?? locale

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node)
      ) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      <div className="relative">
        <button
          ref={btnRef}
          type="button"
          onClick={() => setOpen(v => !v)}
          className="flex h-9 w-[240px] items-center justify-between rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          <span>{currentLabel}</span>
          <ChevronDown size={13} />
        </button>
        {open && (
          <div
            ref={menuRef}
            className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              width: '240px',
              boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
            }}
          >
            {LOCALE_OPTIONS.map(({ value, label: optLabel }) => (
              <button
                key={value}
                type="button"
                onClick={() => { setLocale(value); setOpen(false) }}
                className="flex w-full items-center px-3 py-2 text-sm transition-colors duration-100"
                style={{
                  borderRadius: '8px',
                  fontWeight: locale === value ? 600 : 400,
                  color: locale === value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                  background: 'transparent',
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--c-bg-deep)')}
                onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
              >
                {optLabel}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

type ThemeOption = { value: Theme; label: string; icon: LucideIcon }

function ThemeContent({
  theme,
  setTheme,
  label,
  t,
}: {
  theme: Theme
  setTheme: (t: Theme) => void
  label: string
  t: { themeSystem: string; themeLight: string; themeDark: string }
}) {
  const options: ThemeOption[] = [
    { value: 'system', label: t.themeSystem, icon: Monitor },
    { value: 'light',  label: t.themeLight,  icon: Sun     },
    { value: 'dark',   label: t.themeDark,   icon: Moon    },
  ]

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      <div
        className="flex w-[240px] rounded-lg p-[3px]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        {options.map(({ value, label: optLabel, icon: Icon }) => {
          const active = theme === value
          return (
            <button
              key={value}
              type="button"
              onClick={() => setTheme(value)}
              className="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1.5 text-xs transition-colors duration-100"
              style={{
                background: active ? 'var(--c-bg-deep)' : 'transparent',
                color: active ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                fontWeight: active ? 500 : 400,
              }}
            >
              <Icon size={13} />
              <span>{optLabel}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
