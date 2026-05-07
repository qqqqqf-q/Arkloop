import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2, Send } from 'lucide-react'
import {
  type ChannelResponse,
  type LlmProvider,
  type Persona,
  listChannelPersonas,
  listChannels,
  listLlmProviders,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { DesktopDiscordSettingsPanel } from './DesktopDiscordSettingsPanel'
import { DesktopFeishuSettingsPanel } from './DesktopFeishuSettingsPanel'
import { DesktopQQBotSettingsPanel } from './DesktopQQBotSettingsPanel'
import { DesktopQQSettingsPanel } from './DesktopQQSettingsPanel'
import { DesktopTelegramSettingsPanel } from './DesktopTelegramSettingsPanel'
import { DesktopWeixinSettingsPanel } from './DesktopWeixinSettingsPanel'
import { SettingsModalFrame } from './_SettingsModalFrame'

type Props = {
  accessToken: string
}

type IntegrationTab = 'telegram' | 'discord' | 'feishu' | 'qqbot' | 'qq' | 'weixin'
type ChannelsCache = {
  channels: ChannelResponse[]
  personas: Persona[]
  providers: LlmProvider[]
}

function TelegramIcon({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z" />
    </svg>
  )
}

function DiscordIcon({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M20.317 4.3698a19.7913 19.7913 0 00-4.8851-1.5152.0741.0741 0 00-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 00-.0785-.037 19.7363 19.7363 0 00-4.8852 1.515.0699.0699 0 00-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 00.0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 00.0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 00-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 01-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 01.0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 01.0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 01-.0066.1276 12.2986 12.2986 0 01-1.873.8914.0766.0766 0 00-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 00.0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 00.0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 00-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189Z" />
    </svg>
  )
}

function QQIcon({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M21.395 15.035a40 40 0 0 0-.803-2.264l-1.079-2.695c.001-.032.014-.562.014-.836C19.526 4.632 17.351 0 12 0S4.474 4.632 4.474 9.241c0 .274.013.804.014.836l-1.08 2.695a39 39 0 0 0-.802 2.264c-1.021 3.283-.69 4.643-.438 4.673.54.065 2.103-2.472 2.103-2.472 0 1.469.756 3.387 2.394 4.771-.612.188-1.363.479-1.845.835-.434.32-.379.646-.301.778.343.578 5.883.369 7.482.189 1.6.18 7.14.389 7.483-.189.078-.132.132-.458-.301-.778-.483-.356-1.233-.646-1.846-.836 1.637-1.384 2.393-3.302 2.393-4.771 0 0 1.563 2.537 2.103 2.472.251-.03.581-1.39-.438-4.673" />
    </svg>
  )
}

function WeixinIcon({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M8.691 2.188C3.891 2.188 0 5.476 0 9.53c0 2.212 1.17 4.203 3.002 5.55a.59.59 0 0 1 .213.665l-.39 1.48c-.019.07-.048.141-.048.213 0 .163.13.295.29.295a.326.326 0 0 0 .167-.054l1.903-1.114a.864.864 0 0 1 .717-.098 10.16 10.16 0 0 0 2.837.403c.276 0 .543-.027.811-.05-.857-2.578.157-4.972 1.932-6.446 1.703-1.415 3.882-1.98 5.853-1.838-.576-3.583-4.196-6.348-8.596-6.348zM5.785 5.991c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 0 1-1.162 1.178A1.17 1.17 0 0 1 4.623 7.17c0-.651.52-1.18 1.162-1.18zm5.813 0c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 0 1-1.162 1.178 1.17 1.17 0 0 1-1.162-1.178c0-.651.52-1.18 1.162-1.18zm5.34 2.867c-3.95-.093-7.332 2.836-7.332 6.547 0 3.622 3.263 6.572 7.242 6.572a7.1 7.1 0 0 0 2.07-.296.592.592 0 0 1 .518.074l1.388.812a.23.23 0 0 0 .12.039.215.215 0 0 0 .212-.215c0-.051-.02-.1-.035-.155l-.285-1.08a.43.43 0 0 1 .155-.484C22.048 19.708 23.2 18.158 23.2 16.41c0-3.622-2.855-6.434-6.262-7.552zm-3.215 3.98c.468 0 .848.386.848.86a.854.854 0 0 1-.848.86.854.854 0 0 1-.848-.86c0-.475.38-.86.848-.86zm4.804 0c.468 0 .848.386.848.86a.854.854 0 0 1-.848.86.854.854 0 0 1-.848-.86c0-.475.38-.86.848-.86z" />
    </svg>
  )
}

const PLATFORM_ICONS: Record<IntegrationTab, ReactNode> = {
  telegram: <TelegramIcon size={24} />,
  discord: <DiscordIcon size={24} />,
  feishu: <Send size={24} />,
  qqbot: <QQIcon size={24} />,
  qq: <QQIcon size={24} />,
  weixin: <WeixinIcon size={24} />,
}

const PLATFORM_COLORS: Record<IntegrationTab, { color: string; background: string }> = {
  telegram: { color: '#2aabee', background: 'rgba(42,171,238,0.14)' },
  discord: { color: '#5865f2', background: 'rgba(88,101,242,0.14)' },
  feishu: { color: '#3370ff', background: 'rgba(51,112,255,0.14)' },
  qqbot: { color: '#12b7f5', background: 'rgba(18,183,245,0.14)' },
  qq: { color: '#12b7f5', background: 'rgba(18,183,245,0.14)' },
  weixin: { color: '#07c160', background: 'rgba(7,193,96,0.14)' },
}

let cachedChannelsData: ChannelsCache | null = null

function channelPersonaName(channel: ChannelResponse | null, personas: Persona[], defaultLabel: string) {
  if (!channel?.persona_id) return defaultLabel
  const persona = personas.find((item) => item.id === channel.persona_id)
  return persona?.display_name || persona?.persona_key || defaultLabel
}

function channelModelName(channel: ChannelResponse | null, defaultLabel: string) {
  const value = channel?.config_json?.default_model
  return typeof value === 'string' && value.trim() ? value.trim() : defaultLabel
}

function ChannelSummaryCard({
  item,
  personas,
  active,
  onOpen,
  activeLabel,
  inactiveLabel,
  labels,
}: {
  item: { key: IntegrationTab; label: string; channel: ChannelResponse | null }
  personas: Persona[]
  active: boolean
  onOpen: () => void
  activeLabel: string
  inactiveLabel: string
  labels: {
    persona: string
    model: string
    default: string
  }
}) {
  const enabled = item.channel?.is_active === true
  const persona = channelPersonaName(item.channel, personas, labels.default)
  const model = channelModelName(item.channel, labels.default)
  const colors = PLATFORM_COLORS[item.key]

  return (
    <button
      type="button"
      onClick={onOpen}
      className={[
        'group relative min-h-[88px] rounded-xl border bg-[var(--c-bg-menu)] p-4 text-left transition-[border-color,background-color] duration-150',
        active
          ? 'border-[var(--c-btn-bg)]'
          : 'border-[var(--c-border-subtle)] hover:border-[color-mix(in_srgb,var(--c-border)_76%,var(--c-text-primary)_24%)] hover:bg-[var(--c-bg-sub)]',
      ].join(' ')}
    >
      <div className="flex items-center gap-3">
        <span
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl"
          style={{ color: colors.color, background: colors.background }}
        >
          {PLATFORM_ICONS[item.key]}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center justify-between gap-3">
            <h3 className="truncate text-[15px] font-semibold leading-tight text-[var(--c-text-primary)]">{item.label}</h3>
            <span
              className="shrink-0 rounded-md px-1.5 py-0.5 text-[10px] font-medium leading-tight"
              style={{
                background: enabled ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))' : 'var(--c-bg-deep)',
                color: enabled ? 'var(--c-status-success, #22c55e)' : 'var(--c-text-muted)',
              }}
            >
              {enabled ? activeLabel : inactiveLabel}
            </span>
          </div>
          <div className="mt-2 flex min-w-0 items-center gap-2 text-[12px] font-medium text-[var(--c-text-muted)]">
            <span className="truncate">{labels.persona}: {persona}</span>
            <span className="shrink-0 text-[var(--c-text-muted)]">/</span>
            <span className="truncate">{labels.model}: {model}</span>
          </div>
        </div>
      </div>
    </button>
  )
}

export function DesktopChannelsSettings({ accessToken }: Props) {
  const { t, locale } = useLocale()
  const ct = t.channels
  const ds = t.desktopSettings
  const [activeTab, setActiveTab] = useState<IntegrationTab | null>(null)
  const [loading, setLoading] = useState(() => cachedChannelsData === null)
  const [channels, setChannels] = useState<ChannelResponse[]>(() => cachedChannelsData?.channels ?? [])
  const [personas, setPersonas] = useState<Persona[]>(() => cachedChannelsData?.personas ?? [])
  const [providers, setProviders] = useState<LlmProvider[]>(() => cachedChannelsData?.providers ?? [])

  const load = useCallback(async () => {
    if (cachedChannelsData === null) setLoading(true)
    try {
      const [allChannels, allPersonas] = await Promise.all([
        listChannels(accessToken),
        listChannelPersonas(accessToken).catch(() => [] as Persona[]),
      ])
      cachedChannelsData = {
        channels: allChannels,
        personas: allPersonas,
        providers: cachedChannelsData?.providers ?? [],
      }
      setChannels(allChannels)
      setPersonas(allPersonas)
    } finally {
      setLoading(false)
    }
  }, [accessToken])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    listLlmProviders(accessToken).then((nextProviders) => {
      cachedChannelsData = {
        channels: cachedChannelsData?.channels ?? channels,
        personas: cachedChannelsData?.personas ?? personas,
        providers: nextProviders,
      }
      setProviders(nextProviders)
    }).catch(() => {})
  }, [accessToken])

  const telegramChannel = useMemo(
    () => channels.find((channel) => channel.channel_type === 'telegram') ?? null,
    [channels],
  )
  const discordChannel = useMemo(
    () => channels.find((channel) => channel.channel_type === 'discord') ?? null,
    [channels],
  )
  const qqChannel = useMemo(
    () => channels.find((channel) => channel.channel_type === 'qq') ?? null,
    [channels],
  )
  const feishuChannel = useMemo(
    () => channels.find((channel) => channel.channel_type === 'feishu') ?? null,
    [channels],
  )
  const qqBotChannel = useMemo(
    () => channels.find((channel) => channel.channel_type === 'qqbot') ?? null,
    [channels],
  )
  const wxChannel = useMemo(
    () => channels.find((channel) => channel.channel_type === 'weixin') ?? null,
    [channels],
  )

  const tabItems: { key: IntegrationTab; label: string; channel: ChannelResponse | null }[] = [
    { key: 'telegram', label: ct.telegram, channel: telegramChannel },
    { key: 'discord', label: ct.discord, channel: discordChannel },
    { key: 'feishu', label: ct.feishu, channel: feishuChannel },
    { key: 'qqbot', label: ct.qq, channel: qqBotChannel },
    { key: 'qq', label: ct.qqOneBot, channel: qqChannel },
    { key: 'weixin', label: ct.weixin, channel: wxChannel },
  ]
  const cardLabels = locale === 'zh'
    ? {
        persona: '智能体',
        model: '模型',
        default: '默认',
      }
    : {
        persona: 'Persona',
        model: 'Model',
        default: 'Default',
      }
  const selectedItem = tabItems.find((item) => item.key === activeTab) ?? null

  const detailPanel = selectedItem === null ? null
    : selectedItem.key === 'telegram' ? (
      <DesktopTelegramSettingsPanel
        accessToken={accessToken}
        channel={telegramChannel}
        personas={personas}
        providers={providers}
        reload={load}
      />
    ) : selectedItem.key === 'discord' ? (
      <DesktopDiscordSettingsPanel
        accessToken={accessToken}
        channel={discordChannel}
        personas={personas}
        providers={providers}
        reload={load}
      />
    ) : selectedItem.key === 'feishu' ? (
      <DesktopFeishuSettingsPanel
        accessToken={accessToken}
        channel={feishuChannel}
        personas={personas}
        providers={providers}
        reload={load}
      />
    ) : selectedItem.key === 'qqbot' ? (
      <DesktopQQBotSettingsPanel
        accessToken={accessToken}
        channel={qqBotChannel}
        personas={personas}
        providers={providers}
        reload={load}
      />
    ) : selectedItem.key === 'weixin' ? (
      <DesktopWeixinSettingsPanel
        accessToken={accessToken}
        channel={wxChannel}
        personas={personas}
        providers={providers}
        reload={load}
      />
    ) : (
      <DesktopQQSettingsPanel
        accessToken={accessToken}
        channel={qqChannel}
        personas={personas}
        providers={providers}
        reload={load}
      />
    )

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-6 px-1 pb-8">
      <div>
        <h2 className="text-[24px] font-semibold leading-tight tracking-normal text-[var(--c-text-heading)]">{ds.channels}</h2>
        <p className="mt-2 text-[13px] text-[var(--c-text-muted)]">{ct.subtitle}</p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-16 text-[var(--c-text-muted)]">
          <Loader2 size={18} className="animate-spin" />
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {tabItems.map((item) => (
            <ChannelSummaryCard
              key={item.key}
              item={item}
              personas={personas}
              active={activeTab === item.key}
              onOpen={() => setActiveTab(item.key)}
              activeLabel={ct.active}
              inactiveLabel={ct.inactive}
              labels={cardLabels}
            />
          ))}
        </div>
      )}

      {selectedItem && detailPanel && (
        <SettingsModalFrame
          open
          title={selectedItem.label}
          onClose={() => setActiveTab(null)}
          width={760}
        >
          <div className="mt-6 max-h-[min(78vh,820px)] overflow-y-auto pr-1">
            {detailPanel}
          </div>
        </SettingsModalFrame>
      )}
    </div>
  )
}
