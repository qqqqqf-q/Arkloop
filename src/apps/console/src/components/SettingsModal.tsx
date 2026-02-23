import { useState } from 'react'
import {
  X,
  User,
  Settings,
  Monitor,
  Sun,
  Moon,
  LogOut,
  type LucideIcon,
} from 'lucide-react'
import { useTheme } from '../contexts/ThemeContext'
import type { Theme } from '../storage'
import type { MeResponse } from '../api'

type Tab = 'account' | 'settings'

type Props = {
  me: MeResponse | null
  onClose: () => void
  onLogout: () => void
}

const NAV_ITEMS: { key: Tab; label: string; icon: LucideIcon }[] = [
  { key: 'account',  label: 'Account',  icon: User     },
  { key: 'settings', label: 'Settings', icon: Settings },
]

export function ConsoleSettingsModal({ me, onClose, onLogout }: Props) {
  const [active, setActive] = useState<Tab>('account')
  const userInitial = me?.display_name?.charAt(0).toUpperCase() ?? '?'

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
        {/* 左侧导航 */}
        <div
          className="flex w-[180px] shrink-0 flex-col py-4 bg-[var(--c-bg-sidebar)]"
          style={{ borderRight: '0.5px solid var(--c-border-console)' }}
        >
          <div className="mb-3 px-4">
            <span className="text-sm font-semibold text-[var(--c-text-heading)]">Arkloop</span>
            <span className="ml-1.5 rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
              Console
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

        {/* 右侧内容 */}
        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            <h2 className="text-sm font-medium text-[var(--c-text-heading)]">
              {active === 'account' ? 'Account' : 'Settings'}
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
            {me?.display_name ?? '...'}
          </div>
          <div className="text-xs text-[var(--c-text-muted)]">Platform Admin</div>
        </div>
        <button
          onClick={onLogout}
          className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          title="Sign out"
        >
          <LogOut size={14} />
        </button>
      </div>
    </div>
  )
}

type ThemeOption = { value: Theme; label: string; icon: LucideIcon }

function SettingsTab() {
  const { theme, setTheme } = useTheme()

  const options: ThemeOption[] = [
    { value: 'system', label: 'System', icon: Monitor },
    { value: 'light',  label: 'Light',  icon: Sun     },
    { value: 'dark',   label: 'Dark',   icon: Moon    },
  ]

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">Appearance</span>
      <div
        className="flex w-[240px] rounded-lg p-[3px]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        {options.map(({ value, label, icon: Icon }) => {
          const active = theme === value
          return (
            <button
              key={value}
              type="button"
              onClick={() => setTheme(value)}
              className="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1.5 text-xs transition-colors duration-100"
              style={{
                background: active ? 'var(--c-bg-sub)' : 'transparent',
                color: active ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                fontWeight: active ? 500 : 400,
              }}
            >
              <Icon size={13} />
              <span>{label}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
