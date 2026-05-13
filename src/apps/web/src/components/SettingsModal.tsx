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
  Radio,
  Wifi,
  Wrench,
  Palette,
  RefreshCw,
} from 'lucide-react'
import type { MeResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { useTheme } from '../contexts/ThemeContext'
import { SkillsSettingsContent } from './SkillsSettingsContent'
import { ProvidersSettings } from './settings/ProvidersSettings'
import { ChannelsSettingsContent } from './ChannelsSettingsContent'
import { ConnectionSettingsContent } from './ConnectionSettingsContent'
import { AccountContent, ProfileContent } from './settings/AccountSettings'
import { AppearanceContent, LanguageContent, ThemeContent } from './settings/AppearanceSettings'
import { InviteCodeContent } from './settings/InviteSettings'
import { HelpContent, ReportFeedbackContent } from './settings/HelpSettings'
import { CreditsContent } from './settings/CreditsSettings'
import { UpdateSettingsContent } from './settings/UpdateSettings'
import { ToolsSettings } from './settings/ToolsSettings'
import { TimeZoneSettings } from './settings/TimeZoneSettings'
import { isDesktop, isLocalMode } from '@arkloop/shared/desktop'

export type SettingsTab = 'account' | 'appearance' | 'settings' | 'tools' | 'skills' | 'credits' | 'models' | 'channels' | 'connection' | 'updates'

type NavItem = { key: SettingsTab; icon: LucideIcon }

const BASE_NAV_ITEMS: NavItem[] = [
  { key: 'account',    icon: User },
  { key: 'appearance', icon: Palette },
  { key: 'settings',   icon: Settings },
  { key: 'tools',      icon: Wrench },
  { key: 'skills',     icon: Puzzle },
  { key: 'models',     icon: Cpu },
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
        className="theme-surface-page modal-enter flex overflow-hidden rounded-2xl bg-[var(--c-bg-page)] shadow-2xl"
        style={{
          width: '832px',
          height: '624px',
          boxShadow: 'inset 0 0 0 0.5px var(--c-modal-ring)',
        }}
      >
        {/* nav */}
        <div
          className="theme-surface-sidebar flex w-[200px] shrink-0 flex-col bg-[var(--c-bg-sidebar)] py-4"
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
                  'flex h-8 items-center gap-2 rounded-md px-2 text-sm transition-[colors,transform] duration-100 active:scale-95',
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

          <div className="relative flex-1 overflow-hidden">
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'account' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              {!profileView
                ? <AccountContent me={me} userInitial={userInitial} onLogout={() => { onLogout(); onClose() }} onEditProfile={() => setProfileView(true)} />
                : <ProfileContent me={me} accessToken={accessToken} userInitial={userInitial} onMeUpdated={onMeUpdated} />
              }
            </div>
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'appearance' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              <AppearanceContent />
            </div>
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'settings' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              <div className="flex flex-col gap-6">
                <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
                <TimeZoneSettings me={me} accessToken={accessToken} onMeUpdated={onMeUpdated} />
                <ThemeContent theme={theme} setTheme={setTheme} label={t.appearance} t={t} />
                <InviteCodeContent accessToken={accessToken} />
                <div className="flex flex-col gap-2">
                  <HelpContent label={t.getHelp} />
                  <ReportFeedbackContent accessToken={accessToken} />
                </div>
              </div>
            </div>
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'tools' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              <ToolsSettings accessToken={accessToken} />
            </div>
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'skills' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              <SkillsSettingsContent accessToken={accessToken} onTrySkill={(prompt) => { onClose(); onTrySkill?.(prompt) }} />
            </div>
            {!localMode && (
              <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'credits' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
                <CreditsContent accessToken={accessToken} onCreditsChanged={onCreditsChanged} />
              </div>
            )}
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'models' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              <ProvidersSettings accessToken={accessToken} />
            </div>
            <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'channels' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
              <ChannelsSettingsContent accessToken={accessToken} />
            </div>
            {isDesktop() && (
              <>
                <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'connection' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
                  <ConnectionSettingsContent />
                </div>
                <div className={['absolute inset-0 overflow-y-auto p-6', activeKey === 'updates' ? '' : 'hidden'].join(' ')} style={{ scrollbarGutter: 'stable' }}>
                  <UpdateSettingsContent />
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
