import { useState } from 'react'
import {
  ChevronLeft,
  Settings,
  Cpu,
  Bot,
  Puzzle,
  Server,
  Plug,
  Wifi,
  Blocks,
  Bug,
} from 'lucide-react'
import type { MeResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import {
  GeneralSettings,
  ProvidersSettings,
  PersonasSettings,
  SkillsSettings,
  MCPSettings,
  ConnectorsSettings,
  ConnectionSettings,
  ExtensionsSettings,
  DeveloperSettings,
} from './settings'

export type DesktopSettingsKey =
  | 'general'
  | 'providers'
  | 'personas'
  | 'skills'
  | 'mcp'
  | 'connectors'
  | 'connection'
  | 'extensions'
  | 'developer'

type NavItem = {
  key: DesktopSettingsKey
  icon: typeof Settings
}

const MAIN_NAV: NavItem[] = [
  { key: 'general', icon: Settings },
  { key: 'providers', icon: Cpu },
  { key: 'personas', icon: Bot },
  { key: 'skills', icon: Puzzle },
  { key: 'mcp', icon: Server },
  { key: 'connectors', icon: Plug },
]

const DESKTOP_NAV: NavItem[] = [
  { key: 'connection', icon: Wifi },
  { key: 'extensions', icon: Blocks },
  { key: 'developer', icon: Bug },
]

type Props = {
  me: MeResponse | null
  accessToken: string
  initialSection?: DesktopSettingsKey
  onClose: () => void
  onLogout: () => void
  onMeUpdated?: (me: MeResponse) => void
  onTrySkill?: (prompt: string) => void
}

export function DesktopSettings({
  me,
  accessToken,
  initialSection = 'general',
  onClose,
  onLogout,
  onMeUpdated,
  onTrySkill,
}: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const [activeKey, setActiveKey] = useState<DesktopSettingsKey>(initialSection)

  const renderNav = (items: NavItem[]) =>
    items.map(({ key, icon: Icon }) => (
      <button
        key={key}
        onClick={() => setActiveKey(key)}
        className={[
          'flex h-8 items-center gap-2.5 rounded-lg px-2.5 text-[13px] font-medium transition-colors',
          activeKey === key
            ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
            : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
        ].join(' ')}
      >
        <Icon size={15} />
        <span>{ds[key as keyof typeof ds]}</span>
      </button>
    ))

  const renderContent = () => {
    switch (activeKey) {
      case 'general':
        return (
          <GeneralSettings
            me={me}
            accessToken={accessToken}
            onLogout={onLogout}
            onMeUpdated={onMeUpdated}
          />
        )
      case 'providers':
        return <ProvidersSettings accessToken={accessToken} />
      case 'personas':
        return <PersonasSettings accessToken={accessToken} />
      case 'skills':
        return <SkillsSettings accessToken={accessToken} onTrySkill={onTrySkill} />
      case 'mcp':
        return <MCPSettings />
      case 'connectors':
        return <ConnectorsSettings />
      case 'connection':
        return <ConnectionSettings />
      case 'extensions':
        return <ExtensionsSettings />
      case 'developer':
        return <DeveloperSettings />
      default:
        return null
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-1">
      {/* Left navigation sidebar */}
      <div
        className="flex w-[220px] shrink-0 flex-col overflow-y-auto py-4"
        style={{ borderRight: '0.5px solid var(--c-border-subtle)' }}
      >
        {/* Back button / header */}
        <div className="mb-4 px-3">
          <button
            onClick={onClose}
            className="flex items-center gap-1.5 rounded-lg px-1 py-1 text-sm font-semibold text-[var(--c-text-heading)] transition-colors hover:text-[var(--c-text-primary)]"
          >
            <ChevronLeft size={16} className="text-[var(--c-text-muted)]" />
            {ds.settingsTitle}
          </button>
        </div>

        {/* Platform section */}
        <div className="px-2">
          <div className="mb-1 px-2.5 text-[11px] font-semibold uppercase tracking-wider text-[var(--c-text-muted)]">
            {ds.mainSection}
          </div>
          <div className="flex flex-col gap-[2px]">{renderNav(MAIN_NAV)}</div>
        </div>

        {/* Desktop section */}
        <div className="mt-5 px-2">
          <div className="mb-1 px-2.5 text-[11px] font-semibold uppercase tracking-wider text-[var(--c-text-muted)]">
            {ds.desktopSection}
          </div>
          <div className="flex flex-col gap-[2px]">{renderNav(DESKTOP_NAV)}</div>
        </div>
      </div>

      {/* Right content area */}
      <div className="flex min-w-0 flex-1 flex-col overflow-y-auto p-6">
        {renderContent()}
      </div>
    </div>
  )
}
