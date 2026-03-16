import { useState, useCallback, useEffect, useRef } from 'react'
import {
  Globe,
  Search,
  Check,
  Loader2,
  Eye,
  EyeOff,
  Zap,
  Key,
  Link,
} from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { ConnectorsConfig, FetchProvider, SearchProvider } from '@arkloop/shared/desktop'

// ---------------------------------------------------------------------------
// Shared styles — all colours use CSS variables so they adapt to dark/light
// ---------------------------------------------------------------------------

const inputCls =
  'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm ' +
  'text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] ' +
  'focus:border-[var(--c-border)] transition-colors duration-150'

const labelCls = 'mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function configEqual(a: ConnectorsConfig, b: ConnectorsConfig): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

// ---------------------------------------------------------------------------
// Status badge
// ---------------------------------------------------------------------------

type BadgeVariant = 'free' | 'configured' | 'always' | 'missing'

const BADGE: Record<BadgeVariant, { cls: string; dot: string; label: (t: BadgeT) => string }> = {
  free:       { cls: 'bg-blue-500/15 text-blue-400',   dot: 'bg-blue-400',   label: (t) => t.connectorFreeTier },
  configured: { cls: 'bg-green-500/15 text-green-400', dot: 'bg-green-400', label: (t) => t.connectorConfigured },
  always:     { cls: 'bg-green-500/15 text-green-400', dot: 'bg-green-400', label: (t) => t.connectorConfigured },
  missing:    { cls: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',   dot: 'bg-[var(--c-text-muted)]',       label: (t) => t.connectorNotConfigured },
}

type BadgeT = { connectorFreeTier: string; connectorConfigured: string; connectorNotConfigured: string }

function StatusBadge({ variant, t }: { variant: BadgeVariant; t: BadgeT }) {
  const s = BADGE[variant]
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${s.cls}`}>
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${s.dot}`} />
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
  t: BadgeT
}

function ProviderCard({ icon, title, description, badge, selected, onSelect, children, t }: ProviderCardProps) {
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

  const [config, setConfig] = useState<ConnectorsConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  // Track the last-saved config so dirty = current !== saved
  const savedConfigRef = useRef<ConnectorsConfig | null>(null)

  const api = getDesktopApi()

  useEffect(() => {
    if (!api?.connectors) { setLoading(false); return }
    void api.connectors.get().then((c) => {
      setConfig(c)
      savedConfigRef.current = c
      setLoading(false)
    }).catch(() => setLoading(false))
  }, [api])

  // dirty = config differs from the last saved snapshot
  const dirty = config !== null
    && savedConfigRef.current !== null
    && !configEqual(config, savedConfigRef.current)

  const handleSave = useCallback(async () => {
    if (!config || !api?.connectors) return
    setSaving(true)
    try {
      await api.connectors.set(config)
      savedConfigRef.current = config
      setSaved(true)
      setTimeout(() => setSaved(false), 3000)
    } finally {
      setSaving(false)
    }
  }, [config, api])

  const patchFetch = useCallback((patch: Partial<ConnectorsConfig['fetch']>) => {
    setSaved(false)
    setConfig((prev) => prev ? { ...prev, fetch: { ...prev.fetch, ...patch } } : prev)
  }, [])

  const patchSearch = useCallback((patch: Partial<ConnectorsConfig['search']>) => {
    setSaved(false)
    setConfig((prev) => prev ? { ...prev, search: { ...prev.search, ...patch } } : prev)
  }, [])

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
          t={badgeT}
        />

        <ProviderCard
          icon={<Link size={14} />}
          title={ds.fetchProviderFirecrawl}
          description={ds.fetchProviderFirecrawlDesc}
          badge={fetchP === 'firecrawl' ? (config.fetch.firecrawlApiKey ? 'configured' : 'missing') : 'missing'}
          selected={fetchP === 'firecrawl'}
          onSelect={() => patchFetch({ provider: 'firecrawl' as FetchProvider })}
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
          title={ds.searchProviderBrowser}
          description={ds.searchProviderBrowserDesc}
          badge="free"
          selected={searchP === 'browser'}
          onSelect={() => patchSearch({ provider: 'browser' as SearchProvider })}
          t={badgeT}
        />

        <ProviderCard
          icon={<Search size={14} />}
          title={ds.searchProviderTavily}
          description={ds.searchProviderTavilyDesc}
          badge={searchP === 'tavily' ? (config.search.tavilyApiKey ? 'configured' : 'missing') : 'missing'}
          selected={searchP === 'tavily'}
          onSelect={() => patchSearch({ provider: 'tavily' as SearchProvider })}
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

      {/* ── Save bar ── */}
      <div className="flex items-center gap-3 border-t border-[var(--c-border-subtle)] pt-4">
        <button
          onClick={() => void handleSave()}
          disabled={saving || !dirty}
          className="inline-flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-[background,color,opacity] duration-150 disabled:pointer-events-none disabled:opacity-40"
          style={{
            background: dirty ? 'var(--c-accent)' : 'var(--c-bg-deep)',
            color: dirty ? 'var(--c-accent-fg)' : 'var(--c-text-muted)',
          }}
        >
          {saving && <Loader2 size={13} className="animate-spin" />}
          {!saving && saved && <Check size={13} />}
          {saving ? ds.connectorSaving : ds.connectorSaveBtn}
        </button>
        {saved && !dirty && (
          <span className="flex items-center gap-1 text-xs text-green-400">
            <Check size={11} />
            {ds.connectorSaved}
          </span>
        )}
      </div>
    </div>
  )
}

import { SettingsSectionHeader } from './_SettingsSectionHeader'

function PageHeader({ ds }: { ds: { desktopConnectorsTitle: string; desktopConnectorsDesc: string } }) {
  return <SettingsSectionHeader title={ds.desktopConnectorsTitle} description={ds.desktopConnectorsDesc} />
}
