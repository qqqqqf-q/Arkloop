import { useState, useCallback, useEffect, useRef, type ReactNode } from 'react'
import {
  Globe,
  Search,
  Loader2,
  Eye,
  EyeOff,
  Zap,
  Key,
  Link,
} from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import { listToolProviders } from '../../api-admin'
import { getDesktopAccessToken, getDesktopApi } from '@arkloop/shared/desktop'
import type { ConnectorsConfig, FetchProvider, SearchProvider } from '@arkloop/shared/desktop'
import { useToast } from '@arkloop/shared'

// ---------------------------------------------------------------------------
// Shared styles — all colours use CSS variables so they adapt to dark/light
// ---------------------------------------------------------------------------

import { settingsInputCls } from './_SettingsInput'
import { settingsLabelCls } from './_SettingsLabel'

const inputCls =
  settingsInputCls('md') + ' transition-colors duration-150'

const labelCls = settingsLabelCls('md')

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Status badge
// ---------------------------------------------------------------------------

type BadgeVariant = 'free' | 'configured' | 'always' | 'missing'

const BADGE: Record<BadgeVariant, { cls: string; label: (t: BadgeT) => string }> = {
  free:       { cls: 'bg-blue-500/15 text-blue-400',                     label: (t) => t.connectorFreeTier },
  configured: { cls: 'bg-green-500/15 text-green-400',                   label: (t) => t.connectorConfigured },
  always:     { cls: 'bg-green-500/15 text-green-400',                   label: (t) => t.connectorConfigured },
  missing:    { cls: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]', label: (t) => t.connectorNotConfigured },
}

type BadgeT = { connectorFreeTier: string; connectorConfigured: string; connectorNotConfigured: string }

function StatusBadge({ variant, t }: { variant: BadgeVariant; t: BadgeT }) {
  const s = BADGE[variant]
  return (
    <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${s.cls}`}>
      {s.label(t)}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Animated expand panel (grid-template-rows trick — no height guessing)
// ---------------------------------------------------------------------------

function ExpandPanel({ open, children }: { open: boolean; children: React.ReactNode }) {
  return (
    <div
      className="overflow-hidden transition-[grid-template-rows] duration-200 ease-in-out"
      style={{ display: 'grid', gridTemplateRows: open ? '1fr' : '0fr' }}
    >
      <div className="overflow-hidden">
        <div className="border-t border-[var(--c-border-subtle)] px-4 pb-4 pt-3">
          {children}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Provider card
// ---------------------------------------------------------------------------

interface ProviderCardProps {
  icon: React.ReactNode
  title: string
  description: string
  badge: BadgeVariant
  selected: boolean
  onSelect: () => void
  children?: React.ReactNode
  status?: ReactNode
  t: BadgeT
}

function ProviderCard({ icon, title, description, badge, selected, onSelect, children, status, t }: ProviderCardProps) {
  return (
    <div
      className="rounded-xl transition-[border-color] duration-150"
      style={{
        border: selected
          ? '1.5px solid var(--c-accent)'
          : '1px solid var(--c-border-subtle)',
        background: 'var(--c-bg-menu)',
      }}
    >
      <button
        type="button"
        onClick={onSelect}
        className="flex w-full items-start gap-3 rounded-xl p-4 text-left transition-colors duration-100 hover:bg-[var(--c-bg-deep)]/40"
      >
        {/* Icon — no background box, colour follows selection state via CSS variable */}
        <div
          className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center transition-colors duration-150"
          style={{ color: selected ? 'var(--c-accent)' : 'var(--c-text-muted)' }}
        >
          {icon}
        </div>

        {/* Text */}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium text-[var(--c-text-heading)]">{title}</span>
            <StatusBadge variant={badge} t={t} />
          </div>
          <p className="mt-0.5 text-xs leading-relaxed text-[var(--c-text-muted)]">{description}</p>
          {status && <div className="mt-1">{status}</div>}
        </div>

        {/* Radio knob */}
        <div
          className="mt-0.5 h-4 w-4 shrink-0 rounded-full transition-[border-width,border-color] duration-150"
          style={{
            border: selected ? '5px solid var(--c-accent)' : '1.5px solid var(--c-border-subtle)',
          }}
        />
      </button>

      <ExpandPanel open={!!(selected && children)}>
        {children}
      </ExpandPanel>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Section header
// ---------------------------------------------------------------------------

function Section({ icon, title, subtitle, children }: {
  icon: React.ReactNode
  title: string
  subtitle: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="text-[var(--c-text-secondary)]">{icon}</span>
        <div>
          <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{title}</h4>
          <p className="text-xs text-[var(--c-text-muted)]">{subtitle}</p>
        </div>
      </div>
      <div className="space-y-2">{children}</div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Password input
// ---------------------------------------------------------------------------

function PasswordInput({ value, onChange, placeholder }: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  const [show, setShow] = useState(false)
  return (
    <div className="relative">
      <input
        type={show ? 'text' : 'password'}
        className={inputCls}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <button
        type="button"
        onClick={() => setShow((v) => !v)}
        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
      >
        {show ? <EyeOff size={13} /> : <Eye size={13} />}
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function SearchFetchSettings() {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const { addToast } = useToast()

  const [config, setConfig] = useState<ConnectorsConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [savedAt, setSavedAt] = useState(0)
  const [runtimeProviders, setRuntimeProviders] = useState<
    Record<string, { runtime_state?: string; runtime_reason?: string }>
  >({})

  const savedConfigRef = useRef<ConnectorsConfig | null>(null)
  const initializedRef = useRef(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const api = getDesktopApi()

  useEffect(() => {
    if (!api?.connectors) { setLoading(false); return }
    void api.connectors.get().then((c) => {
      setConfig(c)
      savedConfigRef.current = c
      setLoading(false)
      initializedRef.current = true
    }).catch(() => setLoading(false))
  }, [api])

  useEffect(() => {
    let canceled = false
    const load = async () => {
      const accessToken = getDesktopAccessToken()
      if (!accessToken) {
        if (!canceled) setRuntimeProviders({})
        return
      }
      try {
        const groups = await listToolProviders(accessToken)
        if (canceled) return
        const next: Record<string, { runtime_state?: string; runtime_reason?: string }> = {}
        groups.forEach((group) => {
          group.providers.forEach((provider) => {
            next[provider.provider_name] = {
              runtime_state: provider.runtime_state,
              runtime_reason: provider.runtime_reason,
            }
          })
        })
        setRuntimeProviders(next)
      } catch {
        if (!canceled) setRuntimeProviders({})
      }
    }
    void load()
    return () => { canceled = true }
  }, [savedAt])

  const runtimeStatusForName = (providerName?: string, fallbackReason?: string) => {
    const runtime = providerName ? runtimeProviders[providerName] : undefined
    if (runtime && (runtime.runtime_state || runtime.runtime_reason)) {
      return runtime
    }
    return {
      runtime_state: 'inactive',
      runtime_reason: fallbackReason,
    }
  }

  const handleSave = useCallback(async (cfg: ConnectorsConfig) => {
    if (!api?.connectors) return
    try {
      await api.connectors.set(cfg)
      savedConfigRef.current = cfg
      setSavedAt(Date.now())
      addToast(ds.connectorSaved, 'success')
    } catch {
      addToast('Save failed', 'error')
    }
  }, [api, addToast, ds.connectorSaved])

  const scheduleAutoSave = useCallback((cfg: ConnectorsConfig) => {
    if (!initializedRef.current) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      void handleSave(cfg)
    }, 500)
  }, [handleSave])

  const patchFetch = useCallback((patch: Partial<ConnectorsConfig['fetch']>) => {
    setConfig((prev) => {
      if (!prev) return prev
      const next = { ...prev, fetch: { ...prev.fetch, ...patch } }
      scheduleAutoSave(next)
      return next
    })
  }, [scheduleAutoSave])

  const patchSearch = useCallback((patch: Partial<ConnectorsConfig['search']>) => {
    setConfig((prev) => {
      if (!prev) return prev
      const next = { ...prev, search: { ...prev.search, ...patch } }
      scheduleAutoSave(next)
      return next
    })
  }, [scheduleAutoSave])

  const fetchRuntimeStatus = {
    jina: runtimeStatusForName('web_fetch.jina'),
    basic: runtimeStatusForName('web_fetch.basic'),
    firecrawl: runtimeStatusForName('web_fetch.firecrawl'),
  }
  const searchRuntimeStatus = {
    duckduckgo: runtimeStatusForName('web_search.duckduckgo'),
    tavily: runtimeStatusForName('web_search.tavily'),
    searxng: runtimeStatusForName('web_search.searxng'),
  }

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader ds={ds} />
        <div className="flex items-center justify-center py-20">
          <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      </div>
    )
  }

  if (!config || !api?.connectors) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader ds={ds} />
        <div
          className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <p className="text-sm text-[var(--c-text-muted)]">Not available outside Desktop mode.</p>
        </div>
      </div>
    )
  }

  const fetchP = config.fetch.provider
  const searchP = config.search.provider

  const badgeT: BadgeT = {
    connectorFreeTier: ds.connectorFreeTier,
    connectorConfigured: ds.connectorConfigured,
    connectorNotConfigured: ds.connectorNotConfigured,
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader ds={ds} />

      {/* ── Fetch ── */}
      <Section icon={<Globe size={16} />} title={ds.fetchConnectorTitle} subtitle={ds.fetchConnectorDesc}>
        <ProviderCard
          icon={<Zap size={14} />}
          title={ds.fetchProviderJina}
          description={ds.fetchProviderJinaDesc}
          badge={config.fetch.jinaApiKey ? 'configured' : 'free'}
          selected={fetchP === 'jina'}
          onSelect={() => patchFetch({ provider: 'jina' as FetchProvider })}
          status={<RuntimeStatusLabel state={fetchRuntimeStatus.jina.runtime_state} reason={fetchRuntimeStatus.jina.runtime_reason} />}
          t={badgeT}
        >
          <div>
            <label className={labelCls}><span className="flex items-center gap-1.5"><Key size={11} />{ds.apiKeyOptionalLabel}</span></label>
            <PasswordInput
              value={config.fetch.jinaApiKey ?? ''}
              onChange={(v) => patchFetch({ jinaApiKey: v || undefined })}
              placeholder="jina_..."
            />
          </div>
        </ProviderCard>

        <ProviderCard
          icon={<Globe size={14} />}
          title={ds.fetchProviderBasic}
          description={ds.fetchProviderBasicDesc}
          badge="always"
          selected={fetchP === 'basic'}
          onSelect={() => patchFetch({ provider: 'basic' as FetchProvider })}
          status={<RuntimeStatusLabel state={fetchRuntimeStatus.basic.runtime_state} reason={fetchRuntimeStatus.basic.runtime_reason} />}
          t={badgeT}
        />

        <ProviderCard
          icon={<Link size={14} />}
          title={ds.fetchProviderFirecrawl}
          description={ds.fetchProviderFirecrawlDesc}
          badge={fetchP === 'firecrawl' ? (config.fetch.firecrawlApiKey ? 'configured' : 'missing') : 'missing'}
          selected={fetchP === 'firecrawl'}
          onSelect={() => patchFetch({ provider: 'firecrawl' as FetchProvider })}
          status={<RuntimeStatusLabel state={fetchRuntimeStatus.firecrawl.runtime_state} reason={fetchRuntimeStatus.firecrawl.runtime_reason} />}
          t={badgeT}
        >
          <div className="space-y-3">
            <div>
              <label className={labelCls}><span className="flex items-center gap-1.5"><Key size={11} />{ds.apiKeyLabel}</span></label>
              <PasswordInput
                value={config.fetch.firecrawlApiKey ?? ''}
                onChange={(v) => patchFetch({ firecrawlApiKey: v || undefined })}
                placeholder="fc-..."
              />
            </div>
            <div>
              <label className={labelCls}><span className="flex items-center gap-1.5"><Link size={11} />{ds.baseUrlLabel}</span></label>
              <input type="text" className={inputCls} placeholder="https://api.firecrawl.dev"
                value={config.fetch.firecrawlBaseUrl ?? ''}
                onChange={(e) => patchFetch({ firecrawlBaseUrl: e.target.value || undefined })}
              />
            </div>
          </div>
        </ProviderCard>
      </Section>

      <div className="border-t border-[var(--c-border-subtle)]" />

      {/* ── Search ── */}
      <Section icon={<Search size={16} />} title={ds.searchConnectorTitle} subtitle={ds.searchConnectorDesc}>
        <ProviderCard
          icon={<Zap size={14} />}
          title={ds.searchProviderDuckduckgo}
          description={ds.searchProviderDuckduckgoDesc}
          badge="free"
          selected={searchP === 'duckduckgo'}
          onSelect={() => patchSearch({ provider: 'duckduckgo' as SearchProvider })}
          status={<RuntimeStatusLabel state={searchRuntimeStatus.duckduckgo.runtime_state} reason={searchRuntimeStatus.duckduckgo.runtime_reason} />}
          t={badgeT}
        />

        <ProviderCard
          icon={<Search size={14} />}
          title={ds.searchProviderTavily}
          description={ds.searchProviderTavilyDesc}
          badge={searchP === 'tavily' ? (config.search.tavilyApiKey ? 'configured' : 'missing') : 'missing'}
          selected={searchP === 'tavily'}
          onSelect={() => patchSearch({ provider: 'tavily' as SearchProvider })}
          status={<RuntimeStatusLabel state={searchRuntimeStatus.tavily.runtime_state} reason={searchRuntimeStatus.tavily.runtime_reason} />}
          t={badgeT}
        >
          <div>
            <label className={labelCls}><span className="flex items-center gap-1.5"><Key size={11} />{ds.apiKeyLabel}</span></label>
            <PasswordInput
              value={config.search.tavilyApiKey ?? ''}
              onChange={(v) => patchSearch({ tavilyApiKey: v || undefined })}
              placeholder="tvly-..."
            />
          </div>
        </ProviderCard>

        <ProviderCard
          icon={<Globe size={14} />}
          title={ds.searchProviderSearxng}
          description={ds.searchProviderSearxngDesc}
          badge={searchP === 'searxng' ? (config.search.searxngBaseUrl ? 'configured' : 'missing') : 'missing'}
          selected={searchP === 'searxng'}
          onSelect={() => patchSearch({ provider: 'searxng' as SearchProvider })}
          status={<RuntimeStatusLabel state={searchRuntimeStatus.searxng.runtime_state} reason={searchRuntimeStatus.searxng.runtime_reason} />}
          t={badgeT}
        >
          <div>
            <label className={labelCls}><span className="flex items-center gap-1.5"><Link size={11} />{ds.baseUrlLabel}</span></label>
            <input type="text" className={inputCls} placeholder="http://localhost:4000"
              value={config.search.searxngBaseUrl ?? ''}
              onChange={(e) => patchSearch({ searxngBaseUrl: e.target.value || undefined })}
            />
          </div>
        </ProviderCard>
      </Section>
    </div>
  )
}

import { SettingsSectionHeader } from './_SettingsSectionHeader'

function PageHeader({ ds }: { ds: { desktopConnectorsTitle: string; desktopConnectorsDesc: string } }) {
  return <SettingsSectionHeader title={ds.desktopConnectorsTitle} description={ds.desktopConnectorsDesc} />
}

function RuntimeStatusLabel({ state, reason }: { state?: string; reason?: string }) {
  const info = runtimeStateInfo(state)
  return (
    <span className={`inline-flex items-center gap-1 text-[10px] font-medium ${info.text}`}>
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${info.dot}`} />
      <span>{info.label}</span>
      {reason ? <span className="text-[var(--c-text-muted)]">({formatRuntimeReason(reason)})</span> : null}
    </span>
  )
}

function runtimeStateInfo(state?: string) {
  const normalized = state ?? 'inactive'
  switch (normalized) {
  case 'ready':
    return { label: 'Ready', dot: 'bg-green-400', text: 'text-green-400' }
  case 'missing_config':
    return { label: 'Missing config', dot: 'bg-amber-400', text: 'text-amber-400' }
  case 'decrypt_failed':
    return { label: 'Decrypt failed', dot: 'bg-rose-400', text: 'text-rose-400' }
  case 'invalid_config':
    return { label: 'Invalid config', dot: 'bg-rose-400', text: 'text-rose-400' }
  default:
    return { label: 'Inactive', dot: 'bg-[var(--c-text-muted)]', text: 'text-[var(--c-text-muted)]' }
  }
}

function formatRuntimeReason(reason: string) {
  return reason
    .split('_')
    .map((segment) => segment.charAt(0).toUpperCase() + segment.slice(1))
    .join(' ')
}
