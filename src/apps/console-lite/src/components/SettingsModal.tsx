import { useState, useRef, useEffect } from 'react'
import {
  X,
  User,
  Settings,
  Monitor,
  Sun,
  Moon,
  LogOut,
  ChevronDown,
  type LucideIcon,
} from 'lucide-react'
import { useTheme } from '../contexts/ThemeContext'
import { useLocale } from '../contexts/LocaleContext'
import type { Theme } from '../storage'
import type { Locale } from '../locales'
import type { MeResponse } from '../api'

type Tab = 'account' | 'settings'

type Props = {
  me: MeResponse | null
  onClose: () => void
  onLogout: () => void
}

export function LiteSettingsModal({ me, onClose, onLogout }: Props) {
  const { t } = useLocale()
  const [active, setActive] = useState<Tab>('account')
  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'

  const NAV_ITEMS: { key: Tab; label: string; icon: LucideIcon }[] = [
    { key: 'account', label: t.account, icon: User },
    { key: 'settings', label: t.settings, icon: Settings },
  ]

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-[2px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex h-[480px] w-[720px] overflow-hidden rounded-2xl shadow-2xl bg-[var(--c-bg-page)]"
        style={{ boxShadow: 'inset 0 0 0 0.5px var(--c-modal-ring)' }}
      >
        <div
          className="flex w-[180px] shrink-0 flex-col py-4 bg-[var(--c-bg-sidebar)]"
          style={{ borderRight: '0.5px solid var(--c-border-console)' }}
        >
          <div className="mb-3 px-4">
            <span className="text-sm font-semibold text-[var(--c-text-heading)]">Arkloop</span>
            <span className="ml-1.5 rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
              Lite
            </span>
          </div>

          <nav className="flex flex-col gap-[2px] px-2">
            {NAV_ITEMS.map(({ key, label, icon: Icon }) => (
              <button
                key={key}
                onClick={() => setActive(key)}
                className={[
                  'flex h-8 items-center gap-2 rounded-md px-2 text-sm transition-colors',
                  active === key
                    ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                    : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                ].join(' ')}
              >
                <Icon size={14} />
                <span>{label}</span>
              </button>
            ))}
          </nav>
        </div>

        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            <h2 className="text-sm font-medium text-[var(--c-text-heading)]">
              {active === 'account' ? t.account : t.settings}
            </h2>
            <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              <X size={15} />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {active === 'account' && (
              <AccountTab me={me} userInitial={userInitial} onLogout={() => { onLogout(); onClose() }} />
            )}
            {active === 'settings' && <SettingsTab />}
          </div>
        </div>
      </div>
    </div>
  )
}

function AccountTab({
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
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-base font-medium"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium text-[var(--c-text-heading)]">
            {me?.username ?? '...'}
          </div>
        </div>
        <button
          onClick={onLogout}
          className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          title={t.signOut}
        >
          <LogOut size={14} />
        </button>
      </div>
    </div>
  )
}

const LOCALE_OPTIONS: { value: Locale; label: string }[] = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
]

type ThemeOption = { value: Theme; label: string; icon: LucideIcon }

function SettingsTab() {
  const { theme, setTheme } = useTheme()
  const { t, locale, setLocale } = useLocale()
  const [langOpen, setLangOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!langOpen) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node)
      ) return
      setLangOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [langOpen])

  const themeOptions: ThemeOption[] = [
    { value: 'system', label: t.themeSystem, icon: Monitor },
    { value: 'light', label: t.themeLight, icon: Sun },
    { value: 'dark', label: t.themeDark, icon: Moon },
  ]

  const currentLocaleLabel = LOCALE_OPTIONS.find(o => o.value === locale)?.label ?? locale

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.language}</span>
        <div className="relative">
          <button
            ref={btnRef}
            type="button"
            onClick={() => setLangOpen(v => !v)}
            className="flex h-9 w-[200px] items-center justify-between rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            <span>{currentLocaleLabel}</span>
            <ChevronDown size={13} />
          </button>
          {langOpen && (
            <div
              ref={menuRef}
              className="absolute left-0 top-[calc(100%+4px)] z-50 rounded-[10px] p-1"
              style={{
                border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-menu)',
                width: '200px',
                boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
              }}
            >
              {LOCALE_OPTIONS.map(({ value, label }) => (
                <button
                  key={value}
                  type="button"
                  onClick={() => { setLocale(value); setLangOpen(false) }}
                  className="flex w-full items-center rounded-lg px-3 py-2 text-sm transition-colors duration-100"
                  style={{
                    fontWeight: locale === value ? 600 : 400,
                    color: locale === value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                    background: 'transparent',
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--c-bg-sub)')}
                  onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.appearance}</span>
        <div
          className="flex w-[240px] rounded-lg p-[3px]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          {themeOptions.map(({ value, label, icon: Icon }) => {
            const isActive = theme === value
            return (
              <button
                key={value}
                type="button"
                onClick={() => setTheme(value)}
                className="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1.5 text-xs transition-colors duration-100"
                style={{
                  background: isActive ? 'var(--c-bg-sub)' : 'transparent',
                  color: isActive ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                  fontWeight: isActive ? 500 : 400,
                }}
              >
                <Icon size={13} />
                <span>{label}</span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
