import { useState, useEffect, useCallback, useRef, useLayoutEffect } from 'react'
import { FileText, RefreshCw, Settings, Database, ChevronRight } from 'lucide-react'
import { PillToggle } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { MemoryConfig, SnapshotHit } from '@arkloop/shared/desktop'
import { checkBridgeAvailable, bridgeClient, type ModuleStatus } from '../../api-bridge'
import { secondaryButtonSmCls, secondaryButtonBorderStyle } from '../buttonStyles'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { MemoryConfigModal } from './MemoryConfigModal'

// ---------------------------------------------------------------------------
// Status dot — shows health on the provider card
// ---------------------------------------------------------------------------

type HealthStatus = 'ok' | 'warning' | 'error' | 'checking'

function statusDotColor(s: HealthStatus): string {
  switch (s) {
    case 'ok': return '#22c55e'
    case 'warning': return '#f59e0b'
    case 'error': return '#ef4444'
    default: return 'var(--c-text-muted)'
  }
}

// ---------------------------------------------------------------------------
// SnapshotView — memory hits displayed as cards (matches Notebook EntryCard style)
// ---------------------------------------------------------------------------

function scoreColor(score: number): string {
  if (score >= 0.85) return 'bg-green-500/15 text-green-400'
  if (score >= 0.6) return 'bg-amber-500/15 text-amber-400'
  return 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]'
}

const CONTENT_MAX_LINES = 6
const CONTENT_LINE_HEIGHT = 20
const CONTENT_COLLAPSED_HEIGHT = CONTENT_MAX_LINES * CONTENT_LINE_HEIGHT
const CONTENT_FADE_HEIGHT = CONTENT_LINE_HEIGHT * 2

type MemoryLayerBlockProps = {
  label: string
  content: string | null
  ref?: React.Ref<HTMLPreElement>
  collapsed?: boolean
  collapsedHeight?: number
  expandedHeight?: number | null
  fadeMask?: string
  needsTruncation?: boolean
  contentExpanded?: boolean
  onToggleExpand?: () => void
}

function MemoryLayerBlock({
  label,
  content,
  ref,
  collapsed,
  collapsedHeight,
  expandedHeight,
  fadeMask,
  needsTruncation,
  contentExpanded,
  onToggleExpand,
}: MemoryLayerBlockProps) {
  const resolvedMaxHeight = collapsed
    ? `${collapsedHeight}px`
    : expandedHeight != null
      ? `${expandedHeight}px`
      : undefined

  if (!content) {
    return (
      <p className="text-xs text-[var(--c-text-muted)]">
        <span className="font-medium">{label}</span>
        <span className="ml-1.5">not available</span>
      </p>
    )
  }

  return (
    <div>
      <p className="mb-1.5 text-[11px] font-medium text-[var(--c-text-muted)]">{label}</p>
      <pre
        ref={ref}
        className="rounded-lg p-3 text-xs leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
        style={{
          background: 'var(--c-bg-input)',
          overflow: 'hidden',
          transition: 'max-height 0.3s cubic-bezier(0.25,0.1,0.25,1), mask-image 0.25s ease, -webkit-mask-image 0.25s ease',
          willChange: 'max-height',
          maxHeight: resolvedMaxHeight,
          ...(collapsed
            ? { WebkitMaskImage: fadeMask, maskImage: fadeMask }
            : { WebkitMaskImage: 'none', maskImage: 'none' }),
        }}
      >
        {content}
      </pre>
      {needsTruncation && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onToggleExpand?.() }}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); onToggleExpand?.() } }}
          style={{
            display: 'inline-block',
            marginTop: 6,
            fontSize: 11,
            color: 'var(--c-text-muted)',
            background: 'none',
            border: 'none',
            padding: 0,
            cursor: 'pointer',
            userSelect: 'none',
            WebkitUserSelect: 'none',
            transition: 'color 150ms ease',
          }}
          className="hover:text-[var(--c-text-primary)]"
        >
          {contentExpanded ? 'Show less' : 'Show more'}
        </button>
      )}
    </div>
  )
}

function HitCard({ hit, onLoadContent }: {
  hit: SnapshotHit
  onLoadContent: (uri: string, layer: 'overview' | 'read') => Promise<string>
}) {
  const [expanded, setExpanded] = useState(false)
  const [overview, setOverview] = useState<string | null>(null)
  const [fullText, setFullText] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [fullExpanded, setFullExpanded] = useState(false)
  const [needsTruncation, setNeedsTruncation] = useState(false)
  const [fullHeight, setFullHeight] = useState<number | null>(null)
  const fullRef = useRef<HTMLPreElement>(null)

  const loadBoth = useCallback(async () => {
    if (overview !== null || loading) return
    setLoading(true)
    try {
      if (hit.is_leaf) {
        // leaf node: overview 接口不支持文件，只请求 read
        setOverview('')
        const rd = await onLoadContent(hit.uri, 'read').catch(() => '')
        setFullText(rd || '')
      } else {
        const [ov, rd] = await Promise.all([
          onLoadContent(hit.uri, 'overview').catch(() => ''),
          onLoadContent(hit.uri, 'read').catch(() => ''),
        ])
        setOverview(ov || '')
        setFullText(rd || '')
      }
    } catch {
      setOverview('')
      setFullText('')
    } finally {
      setLoading(false)
    }
  }, [overview, loading, hit.uri, hit.is_leaf, onLoadContent])

  const handleToggle = () => {
    const next = !expanded
    setExpanded(next)
    if (next) void loadBoth()
  }

  useLayoutEffect(() => {
    const el = fullRef.current
    if (!el) return
    const prev = el.style.maxHeight
    el.style.maxHeight = 'none'
    const h = el.scrollHeight
    el.style.maxHeight = prev
    if (h > CONTENT_COLLAPSED_HEIGHT + 1) {
      setNeedsTruncation(true)
      setFullHeight(h)
    } else {
      setNeedsTruncation(false)
      setFullHeight(null)
    }
  }, [fullText])

  const shortUri = hit.uri.replace(/^viking:\/\/user\/[^/]+\//, '')
  const isCollapsed = needsTruncation && !fullExpanded
  const fadeMask = `linear-gradient(to bottom, black calc(100% - ${CONTENT_FADE_HEIGHT}px), transparent)`

  return (
    <div
      className="group rounded-xl"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
    >
      <div
        role="button"
        tabIndex={0}
        className="flex cursor-pointer items-start gap-3 px-4 py-3 outline-none transition-colors hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]"
        style={{ borderRadius: expanded ? '0.75rem 0.75rem 0 0' : '0.75rem' }}
        onClick={handleToggle}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleToggle() } }}
      >
        <span className="mt-0.5 shrink-0 text-[var(--c-text-muted)] transition-transform" style={{ transform: expanded ? 'rotate(90deg)' : undefined }}>
          <ChevronRight size={14} />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${scoreColor(hit.score)}`}>
              {Math.round(hit.score * 100)}%
            </span>
            {!hit.is_leaf && (
              <span className="inline-flex items-center rounded-md bg-blue-500/15 px-2 py-0.5 text-xs font-medium text-blue-400">
                topic
              </span>
            )}
            <span className="text-[10px] text-[var(--c-text-muted)]">{shortUri}</span>
          </div>
          <p className="text-sm text-[var(--c-text-primary)]">{hit.abstract || hit.uri}</p>
        </div>
      </div>

      {expanded && (
        <div className="border-t border-[var(--c-border-subtle)] px-4 py-3">
          {loading ? (
            <div className="flex justify-center py-4"><SpinnerIcon /></div>
          ) : overview !== null && (
            <div className="flex flex-col gap-3">
              <MemoryLayerBlock label="L1 Overview" content={overview} />
              <MemoryLayerBlock
                label="L2 Overview"
                content={fullText}
                ref={fullRef}
                collapsed={isCollapsed}
                collapsedHeight={CONTENT_COLLAPSED_HEIGHT}
                expandedHeight={fullHeight}
                fadeMask={fadeMask}
                needsTruncation={needsTruncation}
                contentExpanded={fullExpanded}
                onToggleExpand={() => setFullExpanded(prev => !prev)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function SnapshotView({ snapshot, hits, onLoadContent }: {
  snapshot: string
  hits: SnapshotHit[]
  onLoadContent: (uri: string, layer: 'overview' | 'read') => Promise<string>
}) {
  if (!snapshot && hits.length === 0) {
    return (
      <div
        className="flex flex-col items-center justify-center rounded-xl py-14"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <FileText size={28} className="mb-3 text-[var(--c-text-muted)]" />
        <p className="text-sm text-[var(--c-text-muted)]">No memory snapshot available yet.</p>
      </div>
    )
  }

  if (hits.length > 0) {
    return (
      <div className="flex flex-col gap-2">
        {hits.map((hit, i) => (
          <HitCard key={hit.uri + i} hit={hit} onLoadContent={onLoadContent} />
        ))}
      </div>
    )
  }

  return (
    <pre
      className="overflow-auto rounded-xl p-4 text-xs leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)', maxHeight: 360 }}
    >
      {snapshot}
    </pre>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

type Props = { accessToken?: string }

export function MemorySettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings

  const [memConfig, setMemConfigState] = useState<MemoryConfig | null>(null)
  const [snapshot, setSnapshot] = useState<string>('')
  const [hits, setHits] = useState<SnapshotHit[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [configModalOpen, setConfigModalOpen] = useState(false)

  // Runtime health probe (lightweight — no full Bridge UI, just status)
  const [health, setHealth] = useState<HealthStatus>('checking')
  const [healthLabel, setHealthLabel] = useState('')

  const api = getDesktopApi()

  const probeHealth = useCallback(async (cfg: MemoryConfig | null) => {
    const isConfigured = Boolean(cfg?.openviking?.vlmModel && cfg?.openviking?.embeddingModel)
    if (!isConfigured) {
      setHealth('error')
      setHealthLabel(ds.memoryNotConfiguredHint)
      return
    }
    try {
      const online = await checkBridgeAvailable()
      if (!online) {
        setHealth('error')
        setHealthLabel('Bridge Offline')
        return
      }
      const list = await bridgeClient.listModules()
      const ov = list.find((m) => m.id === 'openviking')
      if (!ov) {
        setHealth('warning')
        setHealthLabel(ds.memoryModuleNotInstalled)
        return
      }
      const bad: ModuleStatus[] = ['error', 'stopped', 'installed_disconnected']
      if (bad.includes(ov.status)) {
        setHealth(ov.status === 'error' ? 'error' : 'warning')
        switch (ov.status) {
          case 'error': setHealthLabel(ds.memoryModuleError); break
          case 'stopped': setHealthLabel(ds.memoryModuleStopped); break
          case 'installed_disconnected': setHealthLabel(ds.memoryModuleDisconnected); break
        }
        return
      }
      if (ov.status === 'running') {
        setHealth('ok')
        setHealthLabel(ds.memoryModuleRunning)
        return
      }
      setHealth('checking')
      setHealthLabel(ds.memoryModuleChecking)
    } catch {
      setHealth('error')
      setHealthLabel('Bridge Offline')
    }
  }, [ds])

  const loadData = useCallback(async (quiet = false) => {
    if (!api?.memory) { setLoading(false); return }
    if (!quiet) setLoading(true); else setRefreshing(true)
    try {
      const cfg = await api.memory.getConfig()
      setMemConfigState(cfg)
      void probeHealth(cfg)
      if (cfg.enabled) {
        const snap = await api.memory.getSnapshot()
        setSnapshot(snap.memory_block ?? '')
        setHits(snap.hits ?? [])
      }
    } catch { /* ignore */ } finally {
      setLoading(false); setRefreshing(false)
    }
  }, [api, probeHealth])

  useEffect(() => { void loadData() }, [loadData])

  const saveConfig = useCallback(async (next: MemoryConfig) => {
    if (!api?.memory) return
    await api.memory.setConfig(next)
    setMemConfigState(next)
  }, [api])

  const loadContent = useCallback(async (uri: string, layer: 'overview' | 'read'): Promise<string> => {
    if (!api?.memory?.getContent) return ''
    const resp = await api.memory.getContent(uri, layer)
    return resp.content ?? ''
  }, [api])

  // ---------------------------------------------------------------------------

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />
        <div className="flex items-center justify-center py-20"><SpinnerIcon /></div>
      </div>
    )
  }

  if (!api?.memory) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />
        <div
          className="rounded-xl bg-[var(--c-bg-menu)] py-16 text-center text-sm text-[var(--c-text-muted)]"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          Not available outside Desktop mode.
        </div>
      </div>
    )
  }

  const enabled = memConfig?.enabled ?? true
  const isConfigured = Boolean(memConfig?.openviking?.vlmModel && memConfig?.openviking?.embeddingModel)

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />

      {/* Enable Memory toggle */}
      <div
        className="flex items-center justify-between rounded-xl px-4 py-3"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <div className="flex-1 pr-4">
          <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryEnabled}</p>
          <p className="text-xs text-[var(--c-text-muted)]">{ds.memoryEnabledDesc}</p>
        </div>
        <PillToggle
          checked={enabled}
          onChange={(next) => { if (memConfig) void saveConfig({ ...memConfig, enabled: next }) }}
        />
      </div>

      {enabled && memConfig && (
        <>
          {/* OpenViking provider card */}
          <div
            className="rounded-xl transition-[border-color] duration-150"
            style={{
              border: '1.5px solid var(--c-accent)',
              background: 'var(--c-bg-menu)',
            }}
          >
            <div className="flex w-full items-start gap-3 rounded-xl p-4 text-left">
              <div
                className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center"
                style={{ color: 'var(--c-accent)' }}
              >
                <Database size={18} />
              </div>
              <div className="min-w-0 flex-1">
                <span className="text-sm font-medium text-[var(--c-text-heading)]">OpenViking</span>
                <p className="mt-0.5 text-xs leading-relaxed text-[var(--c-text-muted)]">
                  {ds.memoryOpenvikingProviderDesc}
                </p>
              </div>
            </div>
            <div className="flex items-center justify-end gap-3 border-t border-[var(--c-border-subtle)] px-4 py-3">
              <div className="flex items-center gap-2">
                <div
                  className="h-2 w-2 shrink-0 rounded-full"
                  style={{ background: statusDotColor(health) }}
                />
                <span
                  className="text-xs"
                  style={{ color: health === 'ok' ? 'var(--c-text-muted)' : statusDotColor(health) }}
                >
                  {healthLabel}
                </span>
              </div>
              <button
                type="button"
                onClick={() => setConfigModalOpen(true)}
                className={secondaryButtonSmCls}
                style={secondaryButtonBorderStyle}
              >
                <Settings size={14} />
                {ds.memoryConfigureButton}
              </button>
            </div>
          </div>

          {/* Auto-summarize toggle */}
          <div
            className="flex items-center justify-between rounded-xl px-4 py-3"
            style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
          >
            <div className="flex-1 pr-4">
              <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryAutoSummarizeLabel}</p>
              <p className="text-xs text-[var(--c-text-muted)]">{ds.memoryAutoSummarizeDesc}</p>
            </div>
            <PillToggle
              checked={memConfig.memoryCommitEachTurn !== false}
              onChange={(next) => void saveConfig({ ...memConfig, memoryCommitEachTurn: next })}
            />
          </div>

          <div className="border-t border-[var(--c-border-subtle)]" />

          {/* Snapshot section header */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileText size={15} className="text-[var(--c-text-secondary)]" />
              <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{ds.memorySnapshotTitle}</h4>
            </div>
            <button
              onClick={() => void loadData(true)}
              disabled={refreshing}
              className="shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-40"
            >
              <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
            </button>
          </div>

          {isConfigured && <SnapshotView snapshot={snapshot} hits={hits} onLoadContent={loadContent} />}
        </>
      )}

      <MemoryConfigModal
        open={configModalOpen}
        onClose={() => setConfigModalOpen(false)}
        accessToken={accessToken}
        memConfig={memConfig}
        onConfigSaved={(cfg) => { setMemConfigState(cfg); void probeHealth(cfg); void loadData(true) }}
      />
    </div>
  )
}
