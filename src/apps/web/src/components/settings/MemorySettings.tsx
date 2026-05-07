import { useState, useEffect, useCallback, useRef, useLayoutEffect } from 'react'
import { FileText, RefreshCw, Settings, AlertTriangle, Brain, Check, ChevronRight } from 'lucide-react'
import { Modal } from '@arkloop/shared'
import { ProviderSelectCard } from './ProviderSelectCard'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { formatDateTime } from '@arkloop/shared'
import type { MemoryConfig, SnapshotHit } from '@arkloop/shared/desktop'
import { checkBridgeAvailable, bridgeClient, type ModuleStatus } from '../../api-bridge'
import { secondaryButtonSmCls, secondaryButtonXsCls, secondaryButtonBorderStyle } from '../buttonStyles'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { MemoryConfigModal } from './MemoryConfigModal'
import { listMemoryErrors, type MemoryErrorEvent } from '../../api'
import { PastedContentModal } from '../PastedContentModal'
import { getDesktopMemoryApi } from '../../desktopMemoryApi'
import { SettingsSwitch } from './_SettingsSwitch'

// ---------------------------------------------------------------------------
// Status dot — shows health on the provider card
// ---------------------------------------------------------------------------

type HealthStatus = 'ok' | 'warning' | 'error' | 'checking'

const memoryUiActionState = {
  rebuildingSnapshot: false,
  rebuildingImpression: false,
}

let memoryConfigCache: MemoryConfig | null = null

function statusDotColor(s: HealthStatus): string {
  switch (s) {
    case 'ok': return '#22c55e'
    case 'warning': return '#f59e0b'
    case 'error': return '#ef4444'
    default: return 'var(--c-text-muted)'
  }
}

// ---------------------------------------------------------------------------
// Impression card — Claude-style preview card with hover animations
// ---------------------------------------------------------------------------

function formatTimeAgo(dateStr: string | undefined, lang: 'zh' | 'en'): string {
  if (!dateStr) return ''
  const normalized = dateStr.includes('T') || dateStr.includes('Z') || dateStr.includes('+')
    ? dateStr
    : dateStr.replace(' ', 'T') + 'Z'
  const then = new Date(normalized).getTime()
  if (Number.isNaN(then)) return ''
  const diffMs = Date.now() - then
  const minutes = Math.floor(diffMs / 60_000)
  if (minutes < 1) return lang === 'zh' ? '刚刚' : 'just now'
  if (minutes < 60) return lang === 'zh' ? `${minutes} 分钟前` : `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return lang === 'zh' ? `${hours} 小时前` : `${hours}h ago`
  const days = Math.floor(hours / 24)
  return lang === 'zh' ? `${days} 天前` : `${days}d ago`
}

function ImpressionCard({
  impression,
  updatedAt,
  loading,
  onRebuild,
  rebuilding,
  rebuildDone,
  titles,
}: {
  impression: string
  updatedAt?: string
  loading?: boolean
  onRebuild: () => void
  rebuilding: boolean
  rebuildDone: boolean
  titles: {
    title: string
    updatedAgo: string
    empty: string
    viewEdit: string
    rebuild: string
    modalTitle: string
  }
}) {
  const { locale } = useLocale()
  const [hovered, setHovered] = useState(false)
  const [miniHovered, setMiniHovered] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const timeAgo = formatTimeAgo(updatedAt, locale)
  const hasContent = impression.trim().length > 0

  const timeLabel = timeAgo
    ? titles.updatedAgo.replace('{time}', timeAgo)
    : ''

  const lineCount = impression.split('\n').length
  const byteSize = new TextEncoder().encode(impression).length

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Brain size={15} className="text-[var(--c-text-secondary)]" />
          <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">Impression</h4>
        </div>
        <button
          type="button"
          onClick={onRebuild}
          disabled={rebuilding}
          className={secondaryButtonXsCls}
          style={secondaryButtonBorderStyle}
        >
          {rebuildDone ? <Check size={13} /> : <RefreshCw size={13} className={rebuilding ? 'animate-spin' : ''} />}
          {rebuildDone ? 'Done' : titles.rebuild}
        </button>
      </div>

      {loading && !hasContent ? (
        <div
          className="flex flex-col items-center justify-center rounded-xl py-10"
          style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
        >
          <SpinnerIcon />
        </div>
      ) : !hasContent ? (
        <div
          className="flex flex-col items-center justify-center rounded-xl py-10"
          style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
        >
          <Brain size={24} className="mb-2 text-[var(--c-text-muted)]" />
          <p className="text-xs text-[var(--c-text-muted)]">{titles.empty}</p>
        </div>
      ) : (
        <div
          className="group/card cursor-pointer rounded-xl"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            background: 'var(--c-bg-menu)',
          }}
          onClick={() => setModalOpen(true)}
          onMouseEnter={() => setHovered(true)}
          onMouseLeave={() => { setHovered(false); setMiniHovered(false) }}
        >
          <div className="flex gap-4 p-4">
            {/* mini preview card */}
            <div
              className="shrink-0 overflow-hidden rounded-lg transition-shadow duration-200"
              style={{
                width: 120,
                height: 80,
                border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-page)',
                boxShadow: hovered
                  ? '0 3px 6px -2px rgba(0,0,0,0.08), 1px 0 3px -2px rgba(0,0,0,0.03), -1px 0 3px -2px rgba(0,0,0,0.03)'
                  : '0 1px 3px -1px rgba(0,0,0,0.04)',
              }}
              onMouseEnter={() => setMiniHovered(true)}
              onMouseLeave={() => setMiniHovered(false)}
            >
              <div
                className="overflow-hidden transition-all duration-200"
                style={{
                  padding: '10px 0 0 12px',
                  fontSize: 8,
                  lineHeight: '11px',
                  letterSpacing: '-0.01em',
                  color: hovered ? 'var(--c-text-secondary)' : 'var(--c-text-tertiary)',
                  maxHeight: 80,
                  transformOrigin: 'top left',
                  transform: miniHovered ? 'scale(1.12)' : 'scale(1)',
                  WebkitMaskImage: 'linear-gradient(to bottom, black 40%, transparent 90%), linear-gradient(to left, transparent 0px, black 8px)',
                  maskImage: 'linear-gradient(to bottom, black 40%, transparent 90%), linear-gradient(to left, transparent 0px, black 8px)',
                  WebkitMaskComposite: 'source-in',
                  maskComposite: 'intersect',
                }}
              >
                {impression.slice(0, 400)}
              </div>
            </div>

            {/* text area */}
            <div className="flex min-w-0 flex-1 flex-col justify-center overflow-hidden">
              <p className="text-sm text-[var(--c-text-heading)]" style={{ fontWeight: 450 }}>
                {titles.title}
              </p>
              <div className="relative h-[18px] overflow-hidden">
                {/* default: time label */}
                <p
                  className="absolute inset-0 text-[11px] text-[var(--c-text-muted)] transition-all duration-150 ease-out"
                  style={{
                    transform: hovered ? 'translateX(-16px)' : 'translateX(0)',
                    opacity: hovered ? 0 : 1,
                  }}
                >
                  {timeLabel || '\u00a0'}
                </p>
                {/* hover: view and edit */}
                <p
                  className="absolute inset-0 text-[11px] transition-all duration-150 ease-out"
                  style={{
                    color: 'var(--c-text-muted)',
                    transform: hovered ? 'translateX(0)' : 'translateX(16px)',
                    opacity: hovered ? 1 : 0,
                  }}
                >
                  {titles.viewEdit}
                </p>
              </div>
            </div>
          </div>
        </div>
      )}

      {modalOpen && (
        <PastedContentModal
          text={impression}
          size={byteSize}
          lineCount={lineCount}
          onClose={() => setModalOpen(false)}
          title={titles.modalTitle}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// ModalHitCard — expandable hit card inside Memories modal (L1/L2 layers)
// ---------------------------------------------------------------------------

const CONTENT_MAX_LINES = 6
const CONTENT_LINE_HEIGHT = 20
const CONTENT_COLLAPSED_HEIGHT = CONTENT_MAX_LINES * CONTENT_LINE_HEIGHT
const CONTENT_FADE_HEIGHT = CONTENT_LINE_HEIGHT * 2

function ModalHitCard({ hit, onLoadContent }: {
  hit: SnapshotHit
  onLoadContent: (uri: string, layer: 'overview' | 'read') => Promise<string>
}) {
  const [expanded, setExpanded] = useState(false)
  const [overview, setOverview] = useState<string | null>(null)
  const [fullText, setFullText] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [fullExpanded, setFullExpanded] = useState(false)
  const [needsTruncation, setNeedsTruncation] = useState(false)
  const [fullHeight, setFullHeight] = useState<number | null>(null)
  const fullRef = useRef<HTMLPreElement>(null)

  const loadBoth = useCallback(async () => {
    if (loading || (loaded && (overview || fullText))) return
    setLoading(true)
    try {
      if (hit.is_leaf) {
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
      setLoaded(true)
    } catch {
      setOverview('')
      setFullText('')
    } finally {
      setLoading(false)
    }
  }, [loaded, loading, overview, fullText, hit.uri, hit.is_leaf, onLoadContent])

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

  const resolvedMaxHeight = (collapsed: boolean, h: number | null) =>
    collapsed ? `${CONTENT_COLLAPSED_HEIGHT}px` : h != null ? `${h}px` : undefined

  return (
    <div
      className="rounded-lg"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-sub)' }}
    >
      <div
        role="button"
        tabIndex={0}
        className="flex cursor-pointer items-start gap-3 px-3.5 py-2.5 outline-none transition-colors hover:bg-[var(--c-bg-deep)]/25"
        style={{ borderRadius: expanded ? '0.5rem 0.5rem 0 0' : '0.5rem' }}
        onClick={handleToggle}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleToggle() } }}
      >
        <span className="mt-0.5 shrink-0 text-[var(--c-text-muted)] transition-transform" style={{ transform: expanded ? 'rotate(90deg)' : undefined }}>
          <ChevronRight size={14} />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-1.5">
            {!hit.is_leaf && (
              <span className="inline-flex items-center rounded-md bg-blue-500/15 px-2 py-0.5 text-[10px] font-medium text-blue-400">
                topic
              </span>
            )}
            <span className="text-[10px] text-[var(--c-text-muted)]">{shortUri}</span>
          </div>
          <p className="text-sm text-[var(--c-text-primary)]">{hit.abstract || hit.uri}</p>
        </div>
      </div>

      {expanded && (
        <div className="border-t border-[var(--c-border-subtle)] px-3.5 py-2.5">
          {loading ? (
            <div className="flex justify-center py-4"><SpinnerIcon /></div>
          ) : overview !== null && (
            <div className="flex flex-col gap-3">
              {overview && (
                <div>
                  <p className="mb-1.5 text-[11px] font-medium text-[var(--c-text-muted)]">L1 Overview</p>
                  <pre
                    className="rounded-lg p-3 text-xs leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
                    style={{
                      background: 'var(--c-bg-input)',
                      fontFamily: 'var(--c-font-body)',
                      fontWeight: 'var(--c-text-body-weight)',
                      fontSynthesis: 'none',
                    }}
                  >
                    {overview}
                  </pre>
                </div>
              )}
              {fullText && (
                <div>
                  <p className="mb-1.5 text-[11px] font-medium text-[var(--c-text-muted)]">L2 Full</p>
                  <pre
                    ref={fullRef}
                    className="rounded-lg p-3 text-xs leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
                    style={{
                      background: 'var(--c-bg-input)',
                      overflow: 'hidden',
                      transition: 'max-height 0.3s cubic-bezier(0.25,0.1,0.25,1), mask-image 0.25s ease, -webkit-mask-image 0.25s ease',
                      willChange: 'max-height',
                      maxHeight: resolvedMaxHeight(isCollapsed, fullHeight),
                      fontFamily: 'var(--c-font-body)',
                      fontWeight: 'var(--c-text-body-weight)',
                      fontSynthesis: 'none',
                      ...(isCollapsed
                        ? { WebkitMaskImage: fadeMask, maskImage: fadeMask }
                        : { WebkitMaskImage: 'none', maskImage: 'none' }),
                    }}
                  >
                    {fullText}
                  </pre>
                  {needsTruncation && (
                    <button
                      type="button"
                      onClick={(e) => { e.stopPropagation(); setFullExpanded((v) => !v) }}
                      style={{ display: 'inline-block', marginTop: 6, fontSize: 11, color: 'var(--c-text-muted)', background: 'none', border: 'none', padding: 0, cursor: 'pointer' }}
                      className="hover:text-[var(--c-text-primary)]"
                    >
                      {fullExpanded ? 'Show less' : 'Show more'}
                    </button>
                  )}
                </div>
              )}
              {!overview && !fullText && (
                <p className="text-xs text-[var(--c-text-muted)]">No content available</p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Memories card — snapshot hits in Impression-style mini preview
// ---------------------------------------------------------------------------

function MemoriesCard({
  hits,
  snapshot,
  loading,
  onRebuild,
  rebuilding,
  onLoadContent,
  titles,
}: {
  hits: SnapshotHit[]
  snapshot: string
  loading?: boolean
  onRebuild: () => void
  rebuilding: boolean
  onLoadContent: (uri: string, layer: 'overview' | 'read') => Promise<string>
  titles: {
    title: string
    empty: string
    viewEdit: string
    rebuild: string
    modalTitle: string
  }
}) {
  const [hovered, setHovered] = useState(false)
  const [miniHovered, setMiniHovered] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)

  const hasContent = hits.length > 0 || snapshot.trim().length > 0

  const previewLines = hits.length > 0
    ? hits.slice(0, 7).map((h) => (h.abstract || h.uri).split('\n')[0])
    : snapshot.split('\n').filter((l) => l.trim()).slice(0, 7)

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <FileText size={15} className="text-[var(--c-text-secondary)]" />
          <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{titles.title}</h4>
        </div>
        <button
          type="button"
          onClick={onRebuild}
          disabled={rebuilding}
          className={secondaryButtonXsCls}
          style={secondaryButtonBorderStyle}
        >
          <RefreshCw size={13} className={rebuilding ? 'animate-spin' : ''} />
          {titles.rebuild}
        </button>
      </div>

      {loading && !hasContent ? (
        <div
          className="flex flex-col items-center justify-center rounded-xl py-10"
          style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
        >
          <SpinnerIcon />
        </div>
      ) : !hasContent ? (
        <div
          className="flex flex-col items-center justify-center rounded-xl py-10"
          style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
        >
          <FileText size={24} className="mb-2 text-[var(--c-text-muted)]" />
          <p className="text-xs text-[var(--c-text-muted)]">{titles.empty}</p>
        </div>
      ) : (
        <div
          className="group/card cursor-pointer rounded-xl"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            background: 'var(--c-bg-menu)',
          }}
          onClick={() => setModalOpen(true)}
          onMouseEnter={() => setHovered(true)}
          onMouseLeave={() => { setHovered(false); setMiniHovered(false) }}
        >
          <div className="flex gap-4 p-4">
            <div
              className="shrink-0 overflow-hidden rounded-lg transition-shadow duration-200"
              style={{
                width: 120,
                height: 80,
                border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-page)',
                boxShadow: hovered
                  ? '0 3px 6px -2px rgba(0,0,0,0.08), 1px 0 3px -2px rgba(0,0,0,0.03), -1px 0 3px -2px rgba(0,0,0,0.03)'
                  : '0 1px 3px -1px rgba(0,0,0,0.04)',
              }}
              onMouseEnter={() => setMiniHovered(true)}
              onMouseLeave={() => setMiniHovered(false)}
            >
              <div
                className="overflow-hidden transition-all duration-200"
                style={{
                  padding: '10px 0 0 12px',
                  fontSize: 8,
                  lineHeight: '11px',
                  letterSpacing: '-0.01em',
                  color: hovered ? 'var(--c-text-secondary)' : 'var(--c-text-tertiary)',
                  maxHeight: 80,
                  transformOrigin: 'top left',
                  transform: miniHovered ? 'scale(1.12)' : 'scale(1)',
                  WebkitMaskImage: 'linear-gradient(to bottom, black 40%, transparent 90%), linear-gradient(to left, transparent 0px, black 8px)',
                  maskImage: 'linear-gradient(to bottom, black 40%, transparent 90%), linear-gradient(to left, transparent 0px, black 8px)',
                  WebkitMaskComposite: 'source-in',
                  maskComposite: 'intersect',
                }}
              >
                {previewLines.map((line, i) => (
                  <div key={i} style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{line}</div>
                ))}
              </div>
            </div>

            <div className="flex min-w-0 flex-1 flex-col justify-center overflow-hidden">
              <p className="text-sm text-[var(--c-text-heading)]" style={{ fontWeight: 450 }}>
                {titles.title}
              </p>
              <div className="relative h-[18px] overflow-hidden">
                <p
                  className="absolute inset-0 text-[11px] text-[var(--c-text-muted)] transition-all duration-150 ease-out"
                  style={{
                    transform: hovered ? 'translateX(-16px)' : 'translateX(0)',
                    opacity: hovered ? 0 : 1,
                  }}
                >
                  {hits.length > 0 ? `${hits.length} memories` : '\u00a0'}
                </p>
                <p
                  className="absolute inset-0 text-[11px] transition-all duration-150 ease-out"
                  style={{
                    color: 'var(--c-text-muted)',
                    transform: hovered ? 'translateX(0)' : 'translateX(16px)',
                    opacity: hovered ? 1 : 0,
                  }}
                >
                  {titles.viewEdit}
                </p>
              </div>
            </div>
          </div>
        </div>
      )}

      <Modal open={modalOpen} onClose={() => setModalOpen(false)} title={titles.modalTitle} width="560px">
        {hits.length > 0 ? (
          <div className="flex flex-col gap-2.5">
            {hits.map((hit, i) => (
              <ModalHitCard key={hit.uri + i} hit={hit} onLoadContent={onLoadContent} />
            ))}
          </div>
        ) : (
          <pre
            className="rounded-lg p-3 text-sm leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
            style={{
              background: 'var(--c-bg-input)',
              fontFamily: 'var(--c-font-body)',
              fontWeight: 'var(--c-text-body-weight)',
              fontSynthesis: 'none',
            }}
          >
            {snapshot}
          </pre>
        )}
      </Modal>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Memory error label
// ---------------------------------------------------------------------------

function memoryErrorLabel(type: string): string {
  switch (type) {
    case 'memory.write.failed': return 'Write failed'
    case 'memory.distill.append_failed': return 'Distill append failed'
    case 'memory.distill.commit_failed': return 'Distill commit failed'
    default: return type
  }
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

type Props = { accessToken?: string }

export function MemorySettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings

  const [memConfig, setMemConfigState] = useState<MemoryConfig | null>(() => memoryConfigCache)
  const [snapshot, setSnapshot] = useState<string>('')
  const [hits, setHits] = useState<SnapshotHit[]>([])
  const [loading, setLoading] = useState(() => !memoryConfigCache)
  const [rebuilding, setRebuilding] = useState(memoryUiActionState.rebuildingSnapshot)
  const [configModalOpen, setConfigModalOpen] = useState(false)
  const [configModalProvider, setConfigModalProvider] = useState<'openviking' | 'nowledge'>('openviking')
  const [switching, setSwitching] = useState(false)
  const [errorsModalOpen, setErrorsModalOpen] = useState(false)
  const [memoryErrors, setMemoryErrors] = useState<MemoryErrorEvent[]>([])
  const [impression, setImpression] = useState('')
  const [impressionUpdatedAt, setImpressionUpdatedAt] = useState<string | undefined>()
  const [rebuildingImpression, setRebuildingImpression] = useState(memoryUiActionState.rebuildingImpression)
  const [rebuildImpressionDone, setRebuildImpressionDone] = useState(false)
  const [snapshotLoading, setSnapshotLoading] = useState(false)
  const [impressionLoading, setImpressionLoading] = useState(false)
  // Runtime health probe (lightweight — no full Bridge UI, just status)
  const [health, setHealth] = useState<HealthStatus>('checking')
  const [healthLabel, setHealthLabel] = useState('')

  const memoryApi = getDesktopMemoryApi(accessToken)

  const probeHealth = useCallback(async (cfg: MemoryConfig | null) => {
    if (!cfg?.enabled) {
      setHealth('checking')
      setHealthLabel('')
      return
    }
    if (cfg.provider === 'notebook') {
      setHealth('ok')
      setHealthLabel(ds.memorySystemSimple)
      return
    }
    // Prefer runtime status from the semantic memory API. UI config fields are not a health signal.
    if (memoryApi?.getStatus) {
      try {
        const status = await memoryApi.getStatus()
        if (!status?.configured) {
          setHealth('error')
          setHealthLabel(ds.memoryNotConfiguredHint)
          return
        }
        if (!status?.healthy) {
          setHealth('error')
          setHealthLabel(ds.memoryModuleError)
          return
        }
        if (cfg.provider === 'nowledge') {
          setHealth('ok')
          setHealthLabel(ds.memoryConfigured)
          return
        }
      } catch (err) {
        console.error('memory getStatus failed', err)
        setHealth('error')
        setHealthLabel(ds.memoryModuleError)
        return
      }
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
        // Module can be running while the API is not reachable/healthy.
        if (memoryApi?.getStatus) {
          try {
            const status = await memoryApi.getStatus()
            if (status?.healthy) {
              setHealth('ok')
              setHealthLabel(ds.memoryModuleRunning)
              return
            }
            setHealth('error')
            setHealthLabel(ds.memoryModuleError)
            return
          } catch (err) {
            console.error('memory getStatus failed', err)
            setHealth('error')
            setHealthLabel(ds.memoryModuleError)
            return
          }
        }
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
  }, [ds, memoryApi])

  const loadData = useCallback(async (quiet = false) => {
    if (!memoryApi) { setLoading(false); return }
    if (!quiet) setLoading(true)
    try {
      const cfg = await memoryApi.getConfig()
      memoryConfigCache = cfg
      setMemConfigState(cfg)
      if (!quiet) setLoading(false)
      void probeHealth(cfg)
      if (accessToken) {
        void listMemoryErrors(accessToken, 5)
          .then((errResp) => {
            setMemoryErrors(errResp.errors)
          })
          .catch((err) => { console.error('listMemoryErrors failed', err) })
      }
      const hasSemanticBackend = cfg.provider === 'nowledge'
        ? Boolean(cfg?.nowledge?.baseUrl)
        : Boolean(cfg?.openviking?.vlmModel && cfg?.openviking?.embeddingModel)
      if (cfg.enabled && hasSemanticBackend) {
        setSnapshotLoading(true)
        void memoryApi.getSnapshot()
          .then((snap) => {
            setSnapshot(snap.memory_block ?? '')
            setHits(snap.hits ?? [])
          })
          .catch((err) => { console.error('getSnapshot failed', err) })
          .finally(() => setSnapshotLoading(false))
      } else {
        setSnapshot('')
        setHits([])
        setSnapshotLoading(false)
      }
      if (cfg.enabled) {
        if (memoryApi.getImpression) {
          setImpressionLoading(true)
          void memoryApi.getImpression()
            .then((imp) => {
              setImpression(imp.impression ?? '')
              setImpressionUpdatedAt(imp.updated_at)
            })
            .catch((err) => { console.error('getImpression failed', err) })
            .finally(() => setImpressionLoading(false))
        }
      } else {
        setSnapshot('')
        setHits([])
        setImpression('')
        setImpressionUpdatedAt(undefined)
        setSnapshotLoading(false)
        setImpressionLoading(false)
      }
    } catch (err) {
      console.error('memory loadData failed', err)
      setSnapshotLoading(false)
      setImpressionLoading(false)
    } finally {
      setLoading(false)
    }
  }, [memoryApi, probeHealth, accessToken])

  const rebuildSnapshot = useCallback(async () => {
    if (!memoryApi?.rebuildSnapshot) return
    memoryUiActionState.rebuildingSnapshot = true
    setRebuilding(true)
    try {
      const snap = await memoryApi.rebuildSnapshot()
      setSnapshot(snap.memory_block ?? '')
      setHits(snap.hits ?? [])
    } catch (err) { console.error('rebuildSnapshot failed', err) } finally {
      memoryUiActionState.rebuildingSnapshot = false
      setRebuilding(false)
    }
  }, [memoryApi])

  const rebuildImpression = useCallback(async () => {
    if (!memoryApi?.rebuildImpression) return
    memoryUiActionState.rebuildingImpression = true
    setRebuildingImpression(true)
    try {
      const resp = await memoryApi.rebuildImpression()
      if (memoryApi.getImpression) {
        try {
          const imp = await memoryApi.getImpression()
          setImpression(imp.impression ?? '')
          setImpressionUpdatedAt(imp.updated_at ?? resp.updated_at)
        } catch (err) { console.error('getImpression after rebuild failed', err) }
      }
      setRebuildImpressionDone(true)
      setTimeout(() => setRebuildImpressionDone(false), 2000)
    } catch (err) { console.error('rebuildImpression failed', err) } finally {
      memoryUiActionState.rebuildingImpression = false
      setRebuildingImpression(false)
    }
  }, [memoryApi])

  useEffect(() => { void loadData() }, [loadData])

  const saveConfig = useCallback(async (next: MemoryConfig) => {
    if (!memoryApi) return
    try {
      await memoryApi.setConfig(next)
    } catch (err) {
      console.error('memory setConfig failed', err)
      void loadData(true)
      return
    }
    memoryConfigCache = next
    setMemConfigState(next)
    void probeHealth(next)
    void loadData(true)
  }, [loadData, memoryApi, probeHealth])

  const loadContent = useCallback(async (uri: string, layer: 'overview' | 'read'): Promise<string> => {
    if (!memoryApi?.getContent) return ''
    const resp = await memoryApi.getContent(uri, layer)
    return resp.content ?? ''
  }, [memoryApi])

  const [enableCardHovered, setEnableCardHovered] = useState(false)
  const [summarizeCardHovered, setSummarizeCardHovered] = useState(false)

  // ---------------------------------------------------------------------------

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />
        <div className="flex items-center justify-center py-20"><SpinnerIcon /></div>
      </div>
    )
  }

  if (!memoryApi) {
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
  const activeProvider = memConfig?.provider ?? 'notebook'
  const isConfigured = memConfig?.provider === 'nowledge'
    ? Boolean(memConfig?.nowledge?.baseUrl)
    : Boolean(memConfig?.openviking?.vlmModel && memConfig?.openviking?.embeddingModel)
  const showSemanticCards = (activeProvider === 'openviking' || activeProvider === 'nowledge') && isConfigured
  const showSnapshotCard = showSemanticCards

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.memorySettingsTitle} description={ds.memorySettingsDesc} />

      {/* Enable Memory + Auto-summarize compound card */}
      <div
        className="group/switch-card rounded-xl border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]"
        onMouseEnter={() => {
          setEnableCardHovered(true)
          setSummarizeCardHovered(true)
        }}
        onMouseLeave={() => {
          setEnableCardHovered(false)
          setSummarizeCardHovered(false)
        }}
      >
        <div
          role="button"
          tabIndex={0}
          className="flex cursor-pointer items-center justify-between gap-4 px-4 py-4 outline-none transition-colors hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]"
          onMouseEnter={() => setEnableCardHovered(true)}
          onMouseLeave={() => setEnableCardHovered(false)}
          onClick={() => { if (memConfig) void saveConfig({ ...memConfig, enabled: !enabled }) }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              if (memConfig) void saveConfig({ ...memConfig, enabled: !enabled })
            }
          }}
        >
          <div className="min-w-0 flex-1 pr-2">
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryEnabled}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{ds.memoryEnabledDesc}</p>
          </div>
          <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
            <SettingsSwitch
              checked={enabled}
              onChange={(next) => { if (memConfig) void saveConfig({ ...memConfig, enabled: next }) }}
              forceHover={enableCardHovered}
            />
          </div>
        </div>

        {memConfig && (
          <div
            role="button"
            tabIndex={0}
            className={`flex cursor-pointer items-center justify-between gap-4 border-t border-[var(--c-border-subtle)] px-4 py-4 outline-none transition-all hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)] ${enabled ? '' : 'pointer-events-none opacity-40'}`}
            onMouseEnter={() => setSummarizeCardHovered(true)}
            onMouseLeave={() => setSummarizeCardHovered(false)}
            onClick={() => { if (enabled) void saveConfig({ ...memConfig, memoryCommitEachTurn: memConfig.memoryCommitEachTurn === false }) }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                if (enabled) void saveConfig({ ...memConfig, memoryCommitEachTurn: memConfig.memoryCommitEachTurn === false })
              }
            }}
          >
            <div className="min-w-0 flex-1 pr-2">
              <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryAutoSummarizeLabel}</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{ds.memoryAutoSummarizeDesc}</p>
            </div>
            <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
              <SettingsSwitch
                checked={memConfig.memoryCommitEachTurn !== false}
                onChange={(next) => void saveConfig({ ...memConfig, memoryCommitEachTurn: next })}
                forceHover={summarizeCardHovered}
              />
            </div>
          </div>
        )}
      </div>

      {enabled && memConfig && (
        <>
          {showSemanticCards && (
            <>
              <ImpressionCard
                impression={impression}
                updatedAt={impressionUpdatedAt}
                loading={impressionLoading}
                onRebuild={() => void rebuildImpression()}
                rebuilding={rebuildingImpression}
                rebuildDone={rebuildImpressionDone}
                titles={{
                  title: ds.memoryImpressionTitle,
                  updatedAgo: ds.memoryImpressionUpdatedAgo,
                  empty: ds.memoryImpressionEmpty,
                  viewEdit: ds.memoryImpressionViewEdit,
                  rebuild: ds.memoryImpressionRebuild,
                  modalTitle: ds.memoryImpressionModalTitle,
                }}
              />
              {showSnapshotCard && (
                <MemoriesCard
                  hits={hits}
                  snapshot={snapshot}
                  loading={snapshotLoading}
                  onRebuild={() => void rebuildSnapshot()}
                  rebuilding={rebuilding}
                  onLoadContent={loadContent}
                  titles={{
                    title: ds.memorySnapshotTitle,
                    empty: ds.memorySnapshotEmpty,
                    viewEdit: ds.memorySnapshotViewEdit,
                    rebuild: ds.memoryRebuildSnapshot,
                    modalTitle: ds.memorySnapshotTitle,
                  }}
                />
              )}
            </>
          )}

          {/* 向量记忆后端选择 */}
          <div className="flex flex-col gap-3">
            <div className="flex gap-3">
              <ProviderSelectCard
                title={ds.memoryNowledgeProvider}
                description={ds.memoryNowledgeProviderDesc}
                selected={activeProvider === 'nowledge'}
                onSelect={() => {
                  if (!memConfig || activeProvider === 'nowledge') return
                  setSwitching(true)
                  void (async () => {
                    let baseUrl = memConfig.nowledge?.baseUrl ?? ''
                    if (!baseUrl) {
                      try {
                        const res = await fetch('http://127.0.0.1:14242/health', { signal: AbortSignal.timeout(3000) })
                        if (res.ok) baseUrl = 'http://127.0.0.1:14242'
                      } catch { /* 未检测到 */ }
                    }
                    const next: MemoryConfig = {
                      ...memConfig,
                      provider: 'nowledge',
                      nowledge: { ...memConfig.nowledge, baseUrl: baseUrl || memConfig.nowledge?.baseUrl },
                    }
                    await saveConfig(next)
                  })().finally(() => setSwitching(false))
                }}
                disabled={switching}
              />
              <ProviderSelectCard
                title={ds.memoryProviderOpenviking}
                description={ds.memoryOpenvikingProviderDesc}
                selected={activeProvider === 'openviking'}
                onSelect={() => {
                  if (!memConfig || activeProvider === 'openviking') return
                  setSwitching(true)
                  void saveConfig({ ...memConfig, provider: 'openviking' }).finally(() => setSwitching(false))
                }}
                disabled={switching}
              />
            </div>
            {(activeProvider === 'openviking' || activeProvider === 'nowledge') && (
              <div className="flex items-center gap-2">
                {activeProvider === 'openviking' && memoryErrors.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setErrorsModalOpen(true)}
                    className={secondaryButtonSmCls}
                    style={{ border: '0.5px solid var(--c-status-warning-text)', color: 'var(--c-status-warning-text)' }}
                  >
                    <AlertTriangle size={14} />
                    {ds.memoryRecentErrors}
                  </button>
                )}
                <div className="flex items-center gap-1.5">
                  <div className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: statusDotColor(health) }} />
                  <span className="text-xs" style={{ color: health === 'ok' ? 'var(--c-text-muted)' : statusDotColor(health) }}>
                    {healthLabel}
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    setConfigModalProvider(activeProvider === 'nowledge' ? 'nowledge' : 'openviking')
                    setConfigModalOpen(true)
                  }}
                  className={secondaryButtonSmCls}
                  style={secondaryButtonBorderStyle}
                >
                  <Settings size={14} />
                  {ds.memoryConfigureButton}
                </button>
              </div>
            )}
          </div>
        </>
      )}

      <MemoryConfigModal
        open={configModalOpen}
        onClose={() => setConfigModalOpen(false)}
        accessToken={accessToken}
        memConfig={memConfig}
        provider={configModalProvider}
        onConfigSaved={(cfg) => { memoryConfigCache = cfg; setMemConfigState(cfg); void probeHealth(cfg); void loadData(true) }}
      />

      <Modal open={errorsModalOpen} onClose={() => setErrorsModalOpen(false)} title={ds.memoryErrorsModalTitle} width="520px">
        <div className="flex flex-col gap-3">
          {memoryErrors.map((evt) => (
            <div
              key={evt.event_id}
              className="flex flex-col gap-1 rounded-lg px-3 py-2"
              style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-sub)' }}
            >
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium" style={{ color: 'var(--c-status-warning-text)' }}>
                  {memoryErrorLabel(evt.type)}
                </span>
                <span className="text-xs tabular-nums text-[var(--c-text-muted)]">
                  {formatDateTime(evt.ts, { includeSeconds: true, includeZone: false })}
                </span>
              </div>
              <p className="whitespace-pre-wrap break-all text-xs leading-relaxed text-[var(--c-text-secondary)]">
                {(evt.data as Record<string, string>)?.message ?? ''}
              </p>
            </div>
          ))}
        </div>
      </Modal>
    </div>
  )
}
