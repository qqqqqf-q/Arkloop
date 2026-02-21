import { useState } from 'react'
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
} from 'lucide-react'
import type { MeResponse } from '../api'

export type SettingsTab =
  | 'account' | 'settings' | 'usage' | 'scheduled'
  | 'mail' | 'data' | 'browser' | 'personal'
  | 'skills' | 'connectors' | 'integrations'

type NavItem = { key: SettingsTab; label: string; icon: LucideIcon }

const NAV_ITEMS: NavItem[] = [
  { key: 'account',      label: '账户',    icon: User         },
  { key: 'settings',     label: '设置',    icon: Settings     },
  { key: 'usage',        label: '使用情况', icon: BarChart2    },
  { key: 'scheduled',    label: '定时任务', icon: CalendarClock },
  { key: 'mail',         label: 'Mail',   icon: Mail         },
  { key: 'data',         label: '数据控制', icon: Database     },
  { key: 'browser',      label: '云浏览器', icon: Globe        },
  { key: 'personal',     label: '个性化',  icon: Sliders      },
  { key: 'skills',       label: '技能',    icon: Zap          },
  { key: 'connectors',   label: '连接器',  icon: Cable        },
  { key: 'integrations', label: '集成',    icon: Layers       },
]

type Props = {
  me: MeResponse | null
  initialTab?: SettingsTab
  onClose: () => void
  onLogout: () => void
}

export function SettingsModal({ me, initialTab = 'account', onClose, onLogout }: Props) {
  const [activeKey, setActiveKey] = useState<SettingsTab>(initialTab)
  const userInitial = me?.display_name?.charAt(0).toUpperCase() ?? '?'
  const activeLabel = NAV_ITEMS.find((i) => i.key === activeKey)?.label ?? '账户'

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/20 backdrop-blur-[2px]"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="flex h-[624px] w-[832px] overflow-hidden rounded-2xl shadow-2xl"
        style={{ background: '#FAFAF8' }}
      >
        {/* 左侧导航 */}
        <div
          className="flex w-[200px] shrink-0 flex-col py-4"
          style={{ background: '#F2F2F0', borderRight: '0.5px solid #E2E2E0' }}
        >
          <div className="mb-2 px-4 py-1">
            <span className="text-sm font-semibold" style={{ color: '#1A1A18' }}>Arkloop</span>
          </div>

          <nav className="flex flex-col gap-[2px] px-2">
            {NAV_ITEMS.map(({ key, label, icon: Icon }) => (
              <button
                key={key}
                onClick={() => setActiveKey(key)}
                className="flex h-8 items-center gap-2 rounded-md px-2 text-sm transition-colors"
                style={{
                  background: activeKey === key ? '#E4E4E2' : 'transparent',
                  color: activeKey === key ? '#1A1A18' : '#4A4A48',
                }}
              >
                <Icon size={15} />
                <span>{label}</span>
              </button>
            ))}
          </nav>

          <div className="mt-auto px-2">
            <div style={{ borderTop: '0.5px solid #E2E2E0', marginBottom: '8px' }} />
            <button
              className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm transition-colors"
              style={{ color: '#4A4A48' }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = '#E4E4E2' }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
            >
              <HelpCircle size={15} />
              <span>获取帮助</span>
              <ArrowUpRight size={12} style={{ marginLeft: 'auto' }} />
            </button>
          </div>
        </div>

        {/* 右侧内容 */}
        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid #E2E2E0' }}
          >
            <h2 className="text-base font-medium" style={{ color: '#1A1A18' }}>{activeLabel}</h2>
            <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md transition-colors"
              style={{ color: '#6B6B68' }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = '#E4E4E2' }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
            >
              <X size={16} />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {activeKey === 'account' ? (
              <AccountContent
                me={me}
                userInitial={userInitial}
                onLogout={() => { onLogout(); onClose() }}
              />
            ) : (
              <div
                className="flex h-full items-center justify-center text-sm"
                style={{ color: '#8C8C8A' }}
              >
                暂未开放
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
  return (
    <div className="flex flex-col gap-3">
      {/* 用户信息卡片 */}
      <div
        className="flex items-center gap-3 rounded-xl p-4"
        style={{ background: '#F2F2F0', border: '0.5px solid #E2E2E0' }}
      >
        <div
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-lg font-medium"
          style={{ background: '#c2c0b6', color: '#242422' }}
        >
          {userInitial}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-sm font-medium" style={{ color: '#1A1A18' }}>
            {me?.display_name ?? '加载中...'}
          </span>
        </div>
        <button
          onClick={onLogout}
          className="flex h-7 w-7 items-center justify-center rounded-md transition-colors"
          style={{ color: '#6B6B68' }}
          title="退出登录"
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = '#E4E4E2' }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
        >
          <LogOut size={15} />
        </button>
      </div>

      {/* 方案信息 */}
      <div
        className="rounded-xl p-4"
        style={{ background: '#F2F2F0', border: '0.5px solid #E2E2E0' }}
      >
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium" style={{ color: '#1A1A18' }}>Enterprise plan</span>
        </div>
      </div>
    </div>
  )
}
