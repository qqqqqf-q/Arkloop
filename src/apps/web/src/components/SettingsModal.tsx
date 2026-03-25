import { useState } from 'react'
import type { LucideIcon } from 'lucide-react'
import {
  X,
  User,
  Settings,
  ChevronLeft,
  Coins,
  Puzzle,
  Cpu,
  Bot,
  Radio,
  Wifi,
  Palette,
  RefreshCw,
} from 'lucide-react'
import type { MeResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { useTheme } from '../contexts/ThemeContext'
import { SkillsSettingsContent } from './SkillsSettingsContent'
import { ModelConfigContent } from './ModelConfigContent'
import { AgentSettingsContent } from './AgentSettingsContent'
import { ChannelsSettingsContent } from './ChannelsSettingsContent'
import { ConnectionSettingsContent } from './ConnectionSettingsContent'
import { AccountContent, ProfileContent } from './settings/AccountSettings'
import { AppearanceContent, LanguageContent, ThemeContent } from './settings/AppearanceSettings'
import { InviteCodeContent } from './settings/InviteSettings'
import { HelpContent, ReportFeedbackContent } from './settings/HelpSettings'
import { CreditsContent } from './settings/CreditsSettings'
import { UpdateSettingsContent } from './settings/UpdateSettings'
import { isDesktop, isLocalMode } from '@arkloop/shared/desktop'

export type SettingsTab = 'account' | 'appearance' | 'settings' | 'skills' | 'credits' | 'models' | 'agents' | 'channels' | 'connection' | 'updates'

type NavItem = { key: SettingsTab; icon: LucideIcon }

const BASE_NAV_ITEMS: NavItem[] = [
  { key: 'account',    icon: User },
  { key: 'appearance', icon: Palette },
  { key: 'settings',   icon: Settings },
  { key: 'skills',     icon: Puzzle },
  { key: 'models',     icon: Cpu },
  { key: 'agents',     icon: Bot },
  { key: 'channels',   icon: Radio },
  { key: 'credits',    icon: Coins },
]

const DESKTOP_NAV_ITEMS: NavItem[] = [
  ...BASE_NAV_ITEMS,
  { key: 'updates',    icon: RefreshCw },
  { key: 'connection', icon: Wifi },
]

type Props = {
  me: MeResponse | null
  accessToken: string
  initialTab?: SettingsTab
  onClose: () => void
  onLogout: () => void
  onCreditsChanged?: (balance: number) => void
  onMeUpdated?: (me: MeResponse) => void
  onTrySkill?: (prompt: string) => void
}

export function SettingsModal({ me, accessToken, initialTab = 'account', onClose, onLogout, onCreditsChanged, onMeUpdated, onTrySkill }: Props) {
  const { t, locale, setLocale } = useLocale()
  const { theme, setTheme } = useTheme()
  const [activeKey, setActiveKey] = useState<SettingsTab>(initialTab)
  const [profileView, setProfileView] = useState(false)
  const localMode = isLocalMode()
  const navItems = (isDesktop() ? DESKTOP_NAV_ITEMS : BASE_NAV_ITEMS)
    .filter(item => !(localMode && item.key === 'credits'))
  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'
  const activeLabel = t.nav[activeKey as keyof typeof t.nav] ?? t.nav.account

  const handleTabChange = (key: SettingsTab) => {
    setActiveKey(key)
    if (key !== 'account') setProfileView(false)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-[2px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex overflow-hidden rounded-2xl shadow-2xl bg-[var(--c-bg-page)]"
        style={{
          width: '832px',
          height: '624px',
          boxShadow: 'inset 0 0 0 0.5px var(--c-modal-ring)',
        }}
      >
        {/* nav */}
        <div
          className="flex w-[200px] shrink-0 flex-col py-4 bg-[var(--c-bg-sidebar)]"
          style={{ borderRight: '0.5px solid rgba(0,0,0,0.14)' }}
        >
          <div className="mb-2 px-4 py-1">
            <span className="text-sm font-semibold text-[var(--c-text-heading)]">Arkloop</span>
          </div>

          <nav className="flex flex-col gap-[2px] px-2">
            {navItems.map(({ key, icon: Icon }) => (
              <button
                key={key}
                onClick={() => handleTabChange(key)}
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
        </div>

        {/* content */}
        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            {profileView ? (
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setProfileView(false)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                >
                  <ChevronLeft size={16} />
                </button>
                <h2 className="text-base font-medium text-[var(--c-text-heading)]">{t.profileTitle}</h2>
              </div>
            ) : (
              <h2 className="text-base font-medium text-[var(--c-text-heading)]">{activeLabel}</h2>
            )}
            <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              <X size={16} />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {activeKey === 'account' && !profileView && (
              <AccountContent
                me={me}
                userInitial={userInitial}
                onLogout={() => { onLogout(); onClose() }}
                onEditProfile={() => setProfileView(true)}
              />
            )}
            {activeKey === 'account' && profileView && (
              <ProfileContent
                me={me}
                accessToken={accessToken}
                userInitial={userInitial}
                onMeUpdated={onMeUpdated}
              />
            )}
            {activeKey === 'appearance' && (
              <AppearanceContent />
            )}
            {activeKey === 'settings' && (
              <div className="flex flex-col gap-6">
                <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
                <ThemeContent theme={theme} setTheme={setTheme} label={t.appearance} t={t} />
                <InviteCodeContent accessToken={accessToken} />
                <div className="flex flex-col gap-2">
                  <HelpContent label={t.getHelp} />
                  <ReportFeedbackContent accessToken={accessToken} />
                </div>
              </div>
            )}
            {activeKey === 'skills' && (
              <SkillsSettingsContent accessToken={accessToken} onTrySkill={(prompt) => { onClose(); onTrySkill?.(prompt) }} />
            )}
            {activeKey === 'credits' && !localMode && (
              <CreditsContent accessToken={accessToken} onCreditsChanged={onCreditsChanged} />
            )}
            {activeKey === 'models' && (
              <ModelConfigContent accessToken={accessToken} />
            )}
            {activeKey === 'agents' && (
              <AgentSettingsContent accessToken={accessToken} />
            )}
            {activeKey === 'channels' && (
              <ChannelsSettingsContent accessToken={accessToken} />
            )}
            {activeKey === 'connection' && (
              <ConnectionSettingsContent />
            )}
            {activeKey === 'updates' && isDesktop() && (
              <UpdateSettingsContent />
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
