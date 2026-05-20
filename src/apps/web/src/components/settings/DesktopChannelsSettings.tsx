import { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2 } from 'lucide-react'
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
import {
  SETTINGS_INTERACTIVE_CARD_BASE_CLASS,
  SETTINGS_INTERACTIVE_CARD_CLASS,
} from './_SettingsLayout'
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







const PLATFORM_ICONS: Record<IntegrationTab, string> = {
  telegram: `${import.meta.env.BASE_URL}channel-icons/telegram.png`,
  discord: `${import.meta.env.BASE_URL}channel-icons/discord.png`,
  feishu: `${import.meta.env.BASE_URL}channel-icons/feishu.png`,
  qqbot: `${import.meta.env.BASE_URL}channel-icons/qqbot.png`,
  qq: `${import.meta.env.BASE_URL}channel-icons/qq.png`,
  weixin: `${import.meta.env.BASE_URL}channel-icons/weixin.png`,
}

let cachedChannelsData: ChannelsCache | null = null

function channelPersonaName(channel: ChannelResponse | null, personas: Persona[], defaultLabel: string) {
  if (!channel?.persona_id) return defaultLabel
  const persona = personas.find((item) => item.id === channel.persona_id)
  return persona?.display_name || persona?.persona_key || defaultLabel
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
    default: string
  }
}) {
  const enabled = item.channel?.is_active === true
  const persona = channelPersonaName(item.channel, personas, labels.default)

  return (
    <button
      type="button"
      onClick={onOpen}
      className={[
        'group relative min-h-[88px] p-4 text-left',
        active
          ? `${SETTINGS_INTERACTIVE_CARD_BASE_CLASS} cursor-pointer border-[var(--c-btn-bg)] bg-[color-mix(in_srgb,var(--c-bg-deep)_30%,var(--c-bg-menu)_70%)] focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]`
          : SETTINGS_INTERACTIVE_CARD_CLASS,
      ].join(' ')}
    >
      <div className="flex items-center gap-3">
        <img
          src={PLATFORM_ICONS[item.key]}
          alt={item.label}
          className="h-12 w-12 shrink-0 rounded-xl object-cover"
          style={{ boxShadow: '0 1px 2px rgba(0,0,0,0.06), 0 4px 12px rgba(0,0,0,0.08)' }}
          draggable={false}
        />
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
        default: '默认',
      }
    : {
        persona: 'Persona',
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
          width={640}
        >
          <div className="mt-6 max-h-[min(78vh,820px)] overflow-y-auto pr-1">
            {detailPanel}
          </div>
        </SettingsModalFrame>
      )}
    </div>
  )
}
