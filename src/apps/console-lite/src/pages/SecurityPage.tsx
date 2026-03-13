import { useState, useEffect, useCallback, useRef, Fragment } from 'react'
import { useOutletContext } from 'react-router-dom'
import { ShieldAlert, Loader2, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import {
  listPlatformSettings,
  updatePlatformSetting,
  isApiError,
} from '../api'
import { listAuditLogs, type AuditLog } from '../api/audit'
import { bridgeClient, checkBridgeAvailable } from '../api/bridge'

const KEY_REGEX_ENABLED = 'security.injection_scan.regex_enabled'
const KEY_TRUST_SOURCE_ENABLED = 'security.injection_scan.trust_source_enabled'
const KEY_SEMANTIC_ENABLED = 'security.injection_scan.semantic_enabled'
const KEY_SEMANTIC_PROVIDER = 'security.semantic_scanner.provider'
const KEY_SEMANTIC_API_ENDPOINT = 'security.semantic_scanner.api_endpoint'
const KEY_SEMANTIC_API_KEY = 'security.semantic_scanner.api_key'
const AUDIT_ACTION = 'security.injection_detected'
const AUDIT_PAGE_SIZE = 30

type Layer = {
  id: string
  nameKey: 'layerRegex' | 'layerSemantic' | 'layerTrustSource'
  descKey: 'layerRegexDesc' | 'layerSemanticDesc' | 'layerTrustSourceDesc'
  settingKey: string
}

const LAYERS: Layer[] = [
  { id: 'regex', nameKey: 'layerRegex', descKey: 'layerRegexDesc', settingKey: KEY_REGEX_ENABLED },
  { id: 'trust-source', nameKey: 'layerTrustSource', descKey: 'layerTrustSourceDesc', settingKey: KEY_TRUST_SOURCE_ENABLED },
  { id: 'semantic', nameKey: 'layerSemantic', descKey: 'layerSemanticDesc', settingKey: KEY_SEMANTIC_ENABLED },
]

type Tab = 'layers' | 'audit'
const TABS: Tab[] = ['layers', 'audit']

function truncateId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

function TabBar({ tabs, active, onChange }: {
  tabs: { key: Tab; label: string }[]
  active: Tab
  onChange: (t: Tab) => void
}) {
  const barRef = useRef<HTMLDivElement>(null)
  const [indicator, setIndicator] = useState({ left: 0, width: 0 })

  useEffect(() => {
    const container = barRef.current
    if (!container) return
    const btn = container.querySelector<HTMLButtonElement>(`[data-tab="${active}"]`)
    if (!btn) return
    setIndicator({ left: btn.offsetLeft, width: btn.offsetWidth })
  }, [active])

  return (
    <div ref={barRef} className="relative mb-5 flex gap-1 border-b border-[var(--c-border-console)]">
      {tabs.map(tab => (
        <button
          key={tab.key}
          data-tab={tab.key}
          onClick={() => onChange(tab.key)}
          className={`relative px-3 py-1.5 text-xs transition-colors ${
            active === tab.key
              ? 'font-medium text-[var(--c-text-primary)]'
              : 'text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]'
          }`}
        >
          {tab.label}
        </button>
      ))}
      <span
        className="absolute bottom-0 h-0.5 bg-[var(--c-text-primary)] transition-all duration-200"
        style={{ left: indicator.left, width: indicator.width }}
      />
    </div>
  )
}

function AuditTab({ accessToken }: { accessToken: string }) {
  const { addToast } = useToast()
  const { t } = useLocale()
  const ts = t.security

  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [offset, setOffset] = useState(0)
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())

  const fetchLogs = useCallback(async (currentOffset: number) => {
    setLoading(true)
    try {
      const resp = await listAuditLogs(
        { action: AUDIT_ACTION, limit: AUDIT_PAGE_SIZE, offset: currentOffset },
        accessToken,
      )
      setLogs(resp.data)
      setTotal(resp.total)
    } catch {
      addToast(ts.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, ts.toastLoadFailed])

  useEffect(() => { fetchLogs(offset) }, [fetchLogs, offset])

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const totalPages = Math.ceil(total / AUDIT_PAGE_SIZE)
  const currentPage = Math.floor(offset / AUDIT_PAGE_SIZE) + 1

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  if (logs.length === 0) {
    return (
      <div className="flex items-center justify-center py-16">
        <p className="text-sm text-[var(--c-text-muted)]">{ts.auditEmpty}</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col">
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs text-[var(--c-text-muted)]">{total} events</span>
        <button
          onClick={() => fetchLogs(offset)}
          className="flex items-center gap-1 rounded-lg border border-[var(--c-border-console)] px-2 py-1 text-xs text-[var(--c-text-muted)] hover:bg-[var(--c-bg-sub)]"
        >
          <RefreshCw size={12} />
        </button>
      </div>
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-[var(--c-border-console)]">
            <th className="w-6 px-2 py-2" />
            <th className="whitespace-nowrap px-3 py-2 text-xs font-medium text-[var(--c-text-muted)]">{ts.auditColTime}</th>
            <th className="whitespace-nowrap px-3 py-2 text-xs font-medium text-[var(--c-text-muted)]">{ts.auditColRunId}</th>
            <th className="whitespace-nowrap px-3 py-2 text-xs font-medium text-[var(--c-text-muted)]">{ts.auditColCount}</th>
            <th className="whitespace-nowrap px-3 py-2 text-xs font-medium text-[var(--c-text-muted)]">{ts.auditColPatterns}</th>
          </tr>
        </thead>
        <tbody>
          {logs.map(log => {
            const expanded = expandedIds.has(log.id)
            const meta = log.metadata
            const count = (meta?.detection_count as number) ?? 0
            const patterns = (meta?.patterns as Array<Record<string, string>>) ?? []
            const hasDetail = patterns.length > 0

            return (
              <Fragment key={log.id}>
                <tr
                  onClick={() => hasDetail && toggleExpand(log.id)}
                  className={[
                    'border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]',
                    hasDetail ? 'cursor-pointer' : '',
                  ].join(' ')}
                >
                  <td className="w-6 px-2 py-2 text-[var(--c-text-muted)]">
                    {hasDetail && (expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs tabular-nums text-[var(--c-text-secondary)]">
                    {new Date(log.created_at).toLocaleString()}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-[var(--c-text-secondary)]">
                    <span className="font-mono text-xs" title={log.target_id ?? ''}>
                      {log.target_id ? truncateId(log.target_id) : '--'}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs text-[var(--c-text-secondary)]">{count}</td>
                  <td className="px-3 py-2 text-xs text-[var(--c-text-secondary)]">
                    {patterns.slice(0, 3).map(p => p.pattern_id ?? p.category).join(', ')}
                    {patterns.length > 3 && ` +${patterns.length - 3}`}
                  </td>
                </tr>
                {expanded && (
                  <tr className="bg-[var(--c-bg-deep2)]">
                    <td colSpan={5} className="px-4 py-3">
                      <pre className="overflow-auto rounded-md bg-[var(--c-bg-tag)] p-3 text-xs leading-relaxed text-[var(--c-text-secondary)]">
                        {JSON.stringify(meta, null, 2)}
                      </pre>
                    </td>
                  </tr>
                )}
              </Fragment>
            )
          })}
        </tbody>
      </table>
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t border-[var(--c-border-console)] px-3 py-2">
          <span className="text-xs text-[var(--c-text-muted)]">
            {offset + 1}--{Math.min(offset + AUDIT_PAGE_SIZE, total)} / {total}
          </span>
          <div className="flex gap-2">
            <button
              onClick={() => setOffset(p => Math.max(0, p - AUDIT_PAGE_SIZE))}
              disabled={currentPage <= 1}
              className="rounded border border-[var(--c-border-console)] px-2 py-0.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-40"
            >
              Prev
            </button>
            <span className="flex items-center text-xs text-[var(--c-text-muted)]">{currentPage} / {totalPages}</span>
            <button
              onClick={() => setOffset(p => p + AUDIT_PAGE_SIZE)}
              disabled={currentPage >= totalPages}
              className="rounded border border-[var(--c-border-console)] px-2 py-0.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-40"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function SemanticSetupPanel({
  accessToken,
  bridgeAvailable,
  onSaved,
}: {
  accessToken: string
  bridgeAvailable: boolean
  onSaved: () => void
}) {
  const { addToast } = useToast()
  const { t } = useLocale()
  const ts = t.security

  const [mode, setMode] = useState<'local' | 'api'>('api')
  const [endpoint, setEndpoint] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    if (mode === 'api' && !endpoint.trim()) return
    setSaving(true)
    try {
      await updatePlatformSetting(KEY_SEMANTIC_PROVIDER, mode, accessToken)
      if (mode === 'api') {
        await updatePlatformSetting(KEY_SEMANTIC_API_ENDPOINT, endpoint.trim(), accessToken)
        if (apiKey.trim()) {
          await updatePlatformSetting(KEY_SEMANTIC_API_KEY, apiKey.trim(), accessToken)
        }
      }
      addToast(ts.toastUpdated, 'success')
      onSaved()
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  const modeBtn = (value: 'local' | 'api', label: string) => (
    <button
      onClick={() => setMode(value)}
      className={[
        'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
        mode === value
          ? 'bg-[var(--c-text-primary)] text-[var(--c-bg-card)]'
          : 'bg-[var(--c-bg-tag)] text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]',
      ].join(' ')}
    >
      {label}
    </button>
  )

  return (
    <div className="mt-2 rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-deep2)] p-4">
      <div className="mb-4 flex gap-2">
        {modeBtn('local', ts.semanticProviderLocal)}
        {modeBtn('api', ts.semanticProviderApi)}
      </div>

      {mode === 'local' && (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-[var(--c-text-muted)]">{ts.semanticLocalDesc}</p>
          {!bridgeAvailable && (
            <p className="text-xs text-[var(--c-status-warning-text)]">{ts.semanticBridgeRequired}</p>
          )}
          <button
            disabled={!bridgeAvailable || saving}
            onClick={() => void handleSave()}
            className={[
              'w-fit rounded-md border px-2.5 py-1 text-xs font-medium transition-colors',
              bridgeAvailable
                ? 'border-[var(--c-status-success-text)] text-[var(--c-status-success-text)] hover:bg-[var(--c-status-success-bg)]'
                : 'border-[var(--c-border-console)] text-[var(--c-text-muted)] opacity-50 cursor-not-allowed',
            ].join(' ')}
          >
            {saving ? <Loader2 size={12} className="inline animate-spin" /> : ts.actionSave}
          </button>
        </div>
      )}

      {mode === 'api' && (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-[var(--c-text-muted)]">{ts.semanticApiDesc}</p>
          <input
            type="url"
            value={endpoint}
            onChange={e => setEndpoint(e.target.value)}
            placeholder={ts.semanticApiEndpointHint}
            className="rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-card)] px-3 py-2 text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none focus:ring-1 focus:ring-[var(--c-text-muted)]"
          />
          <input
            type="password"
            value={apiKey}
            onChange={e => setApiKey(e.target.value)}
            placeholder={ts.semanticApiKeyHint}
            className="rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-card)] px-3 py-2 text-xs text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none focus:ring-1 focus:ring-[var(--c-text-muted)]"
          />
          <button
            disabled={saving || !endpoint.trim()}
            onClick={() => void handleSave()}
            className={[
              'w-fit rounded-md border px-2.5 py-1 text-xs font-medium transition-colors',
              endpoint.trim()
                ? 'border-[var(--c-status-success-text)] text-[var(--c-status-success-text)] hover:bg-[var(--c-status-success-bg)]'
                : 'border-[var(--c-border-console)] text-[var(--c-text-muted)] opacity-50 cursor-not-allowed',
            ].join(' ')}
          >
            {saving ? <Loader2 size={12} className="inline animate-spin" /> : ts.actionSave}
          </button>
        </div>
      )}
    </div>
  )
}

export function SecurityPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const ts = t.security

  const [activeTab, setActiveTab] = useState<Tab>('layers')
  const [values, setValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState<string | null>(null)
  const [semanticProvider, setSemanticProvider] = useState('')
  const [bridgeAvailable, setBridgeAvailable] = useState(false)
  const [localModelInstalled, setLocalModelInstalled] = useState(false)
  const [semanticSetupOpen, setSemanticSetupOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await listPlatformSettings(accessToken)
      const map: Record<string, string> = {}
      for (const s of list) map[s.key] = s.value
      setValues(map)

      const provider = map[KEY_SEMANTIC_PROVIDER] ?? ''
      setSemanticProvider(provider)

      const online = await checkBridgeAvailable()
      setBridgeAvailable(online)

      if (online && provider === 'local') {
        try {
          const modules = await bridgeClient.listModules()
          const pg = modules.find(m => m.id === 'prompt-guard')
          setLocalModelInstalled(pg?.status === 'running' || pg?.status === 'installed_disconnected')
        } catch {
          setLocalModelInstalled(false)
        }
      }
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, ts.toastLoadFailed])

  useEffect(() => { void load() }, [load])

  const toggle = useCallback(async (key: string, current: boolean) => {
    setToggling(key)
    try {
      await updatePlatformSetting(key, String(!current), accessToken)
      setValues((prev) => ({ ...prev, [key]: String(!current) }))
      addToast(ts.toastUpdated, 'success')
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    } finally {
      setToggling(null)
    }
  }, [accessToken, addToast, ts.toastUpdated, ts.toastFailed])

  const handleReconfigure = useCallback(async () => {
    try {
      await updatePlatformSetting(KEY_SEMANTIC_PROVIDER, '', accessToken)
      await updatePlatformSetting(KEY_SEMANTIC_ENABLED, 'false', accessToken)
      setSemanticProvider('')
      setValues(prev => ({ ...prev, [KEY_SEMANTIC_ENABLED]: 'false', [KEY_SEMANTIC_PROVIDER]: '' }))
      setSemanticSetupOpen(true)
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    }
  }, [accessToken, addToast, ts.toastFailed])

  const isEnabled = (key: string) => values[key] === 'true'

  const semanticConfigured = semanticProvider !== ''
  const semanticEndpoint = values[KEY_SEMANTIC_API_ENDPOINT] ?? ''
  const semanticCanEnable = semanticProvider === 'api'
    ? semanticEndpoint !== ''
    : semanticProvider === 'local'
      ? localModelInstalled
      : false

  const tabItems = TABS.map(key => ({
    key,
    label: key === 'layers' ? ts.tabLayers : ts.tabAudit,
  }))

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            <ShieldAlert size={15} />
            {ts.title}
          </span>
        }
      />

      <div className="flex-1 overflow-y-auto p-6">
        <p className="mb-4 text-xs text-[var(--c-text-muted)]">{ts.description}</p>

        <TabBar tabs={tabItems} active={activeTab} onChange={setActiveTab} />

        {activeTab === 'layers' && (
          loading ? (
            <div className="flex items-center justify-center py-16">
              <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {LAYERS.map((layer) => {
                const enabled = isEnabled(layer.settingKey)
                const busy = toggling === layer.settingKey
                const isSemantic = layer.id === 'semantic'

                const semanticBadge = () => {
                  if (!isSemantic) return null
                  if (!semanticConfigured) return (
                    <span className="rounded-md bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
                      {ts.statusNotConfigured}
                    </span>
                  )
                  if (semanticProvider === 'local' && !localModelInstalled) return (
                    <span className="rounded-md bg-[var(--c-status-warning-bg)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-status-warning-text)]">
                      {ts.statusPendingInstall}
                    </span>
                  )
                  return null
                }

                const canToggle = !isSemantic || (semanticConfigured && semanticCanEnable)

                return (
                  <div key={layer.id}>
                    <div className="flex items-center justify-between rounded-lg border border-[var(--c-border-console)] px-4 py-3">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium text-[var(--c-text-primary)]">
                            {ts[layer.nameKey]}
                          </span>
                          {semanticBadge() ?? (
                            <span
                              className={[
                                'rounded-md px-1.5 py-0.5 text-[10px] font-medium',
                                enabled
                                  ? 'bg-[var(--c-status-success-bg)] text-[var(--c-status-success-text)]'
                                  : 'bg-[var(--c-status-warning-bg)] text-[var(--c-status-warning-text)]',
                              ].join(' ')}
                            >
                              {enabled ? ts.statusEnabled : ts.statusDisabled}
                            </span>
                          )}
                          {isSemantic && semanticConfigured && (
                            <span className="text-[10px] text-[var(--c-text-muted)]">
                              ({semanticProvider === 'api' ? 'API' : 'Local'})
                            </span>
                          )}
                        </div>
                        <p className="mt-1 text-xs text-[var(--c-text-muted)]">
                          {ts[layer.descKey]}
                        </p>
                      </div>

                      <div className="flex shrink-0 items-center gap-2">
                        {isSemantic && semanticConfigured && (
                          <button
                            onClick={() => void handleReconfigure()}
                            className="rounded-md px-2 py-1 text-[10px] text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
                          >
                            {ts.actionReconfigure}
                          </button>
                        )}
                        {isSemantic && !semanticConfigured ? (
                          <button
                            onClick={() => setSemanticSetupOpen(v => !v)}
                            className="rounded-md border border-[var(--c-border-console)] px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
                          >
                            {ts.actionConfigure}
                          </button>
                        ) : (
                          <button
                            disabled={busy || !canToggle}
                            onClick={() => void toggle(layer.settingKey, enabled)}
                            className={[
                              'rounded-md border px-2.5 py-1 text-xs font-medium transition-colors',
                              enabled
                                ? 'border-[var(--c-border-console)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)]'
                                : 'border-[var(--c-status-success-text)] text-[var(--c-status-success-text)] hover:bg-[var(--c-status-success-bg)]',
                              (busy || !canToggle) ? 'opacity-50 cursor-not-allowed' : '',
                            ].join(' ')}
                          >
                            {busy
                              ? <Loader2 size={12} className="inline animate-spin" />
                              : enabled ? ts.actionDisable : ts.actionEnable
                            }
                          </button>
                        )}
                      </div>
                    </div>

                    {isSemantic && !semanticConfigured && semanticSetupOpen && (
                      <SemanticSetupPanel
                        accessToken={accessToken}
                        bridgeAvailable={bridgeAvailable}
                        onSaved={load}
                      />
                    )}
                  </div>
                )
              })}
            </div>
          )
        )}

        {activeTab === 'audit' && <AuditTab accessToken={accessToken} />}
      </div>
    </div>
  )
}
