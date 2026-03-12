import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  Copy, Check, RefreshCw, Loader2,
  CircleDot, CircleOff, CircleAlert, CirclePause, CirclePlay, CircleDashed,
} from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge, type BadgeVariant } from '../../components/Badge'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import type { LocaleStrings } from '../../locales'
import {
  checkBridgeAvailable,
  bridgeClient,
  type ModuleInfo,
  type ModuleStatus,
  type ModuleCategory,
  type ModuleAction,
} from '../../api/bridge'
import {
  STATIC_MODULES,
  INSTALL_COMMANDS,
  AGENT_PROMPTS,
} from './module-registry'

type ModulesLocale = LocaleStrings['pages']['modules']

function statusBadgeVariant(status: ModuleStatus): BadgeVariant {
  switch (status) {
    case 'running': return 'success'
    case 'stopped': return 'neutral'
    case 'pending_bootstrap': return 'warning'
    case 'error': return 'error'
    case 'installed_disconnected': return 'warning'
    case 'not_installed': return 'neutral'
  }
}

function statusIcon(status: ModuleStatus) {
  const size = 13
  switch (status) {
    case 'running': return <CircleDot size={size} />
    case 'stopped': return <CirclePause size={size} />
    case 'pending_bootstrap': return <CircleAlert size={size} />
    case 'error': return <CircleOff size={size} />
    case 'installed_disconnected': return <CircleDashed size={size} />
    case 'not_installed': return <CirclePlay size={size} />
  }
}

function statusLabel(status: ModuleStatus, t: ModulesLocale): string {
  const map: Record<ModuleStatus, string> = {
    not_installed: t.statusNotInstalled,
    installed_disconnected: t.statusDisconnected,
    pending_bootstrap: t.statusPendingBootstrap,
    running: t.statusRunning,
    stopped: t.statusStopped,
    error: t.statusError,
  }
  return map[status]
}

function categoryLabel(cat: ModuleCategory, t: ModulesLocale): string {
  const map: Record<ModuleCategory, string> = {
    memory: t.tabMemory,
    sandbox: t.tabSandbox,
    search: t.tabSearch,
    browser: t.tabBrowser,
    console: t.tabConsole,
    infrastructure: 'Infra',
  }
  return map[cat]
}

function availableActions(mod: ModuleInfo, bridgeOnline: boolean): ModuleAction[] {
  if (!bridgeOnline) return []
  switch (mod.status) {
    case 'not_installed':
      return mod.capabilities.installable ? ['install'] : []
    case 'stopped':
      return ['start', 'restart']
    case 'running':
      return ['stop', 'restart', ...(mod.capabilities.configurable ? ['configure_connection' as ModuleAction] : [])]
    case 'pending_bootstrap':
      return mod.capabilities.bootstrap_supported ? ['bootstrap_defaults'] : []
    case 'installed_disconnected':
      return mod.capabilities.configurable ? ['configure_connection'] : []
    case 'error':
      return ['restart', 'stop']
  }
}

function actionLabel(action: ModuleAction, t: ModulesLocale): string {
  const map: Record<ModuleAction, string> = {
    install: t.actionInstall,
    start: t.actionStart,
    stop: t.actionStop,
    restart: t.actionRestart,
    configure_connection: t.actionConfigure,
    bootstrap_defaults: t.actionBootstrap,
  }
  return map[action]
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(text)
    setCopied(true)
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => setCopied(false), 2000)
  }, [text])

  useEffect(() => () => { if (timerRef.current) clearTimeout(timerRef.current) }, [])

  return (
    <button
      onClick={handleCopy}
      className="flex items-center gap-1.5 rounded-md border border-[var(--c-border)] bg-[var(--c-bg-page)] px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      {copied ? <Check size={12} /> : <Copy size={12} />}
      {label}
    </button>
  )
}

function ModuleRow({
  mod,
  bridgeOnline,
  t,
  onAction,
}: {
  mod: ModuleInfo
  bridgeOnline: boolean
  t: ModulesLocale
  onAction: (moduleId: string, action: ModuleAction) => void
}) {
  const actions = availableActions(mod, bridgeOnline)
  const command = INSTALL_COMMANDS[mod.id]
  const agentPrompt = AGENT_PROMPTS[mod.id]

  const displayStatus = bridgeOnline ? mod.status : null
  const badgeLabel = displayStatus ? statusLabel(displayStatus, t) : t.statusUnknown
  const badgeVariant = displayStatus ? statusBadgeVariant(displayStatus) : 'neutral' as BadgeVariant

  return (
    <div className="rounded-md border border-[var(--c-border-console)] px-3 py-2.5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="font-mono text-xs text-[var(--c-text-primary)]">{mod.name}</span>
          <Badge variant={badgeVariant}>
            <span className="flex items-center gap-1">
              {displayStatus ? statusIcon(displayStatus) : <CircleDashed size={13} />}
              {badgeLabel}
            </span>
          </Badge>
        </div>
        <div className="flex items-center gap-1.5">
          {actions.map((action) => (
            <button
              key={action}
              onClick={() => onAction(mod.id, action)}
              className="rounded-md bg-[var(--c-bg-tag)] px-2 py-1 text-[11px] font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-border)]"
            >
              {actionLabel(action, t)}
            </button>
          ))}
          {!bridgeOnline && command && (
            <CopyButton text={command} label={t.copyCommand} />
          )}
          {!bridgeOnline && agentPrompt && (
            <CopyButton text={agentPrompt} label={t.copyAgentPrompt} />
          )}
        </div>
      </div>
      <div className="mt-1.5 flex items-center gap-2 text-xs text-[var(--c-text-muted)]">
        <span>{mod.description}</span>
        {mod.port && (
          <span className="shrink-0 cursor-default rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 font-mono transition-colors hover:bg-[var(--c-border)]">
            :{mod.port}
          </span>
        )}
        {mod.depends_on.length > 0 && (
          <span className="shrink-0">{t.dependsOn}: {mod.depends_on.join(', ')}</span>
        )}
      </div>
    </div>
  )
}

export function ModulesPage() {
  const { accessToken: _ } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tm = t.pages.modules

  const [bridgeOnline, setBridgeOnline] = useState(false)
  const [modules, setModules] = useState<ModuleInfo[]>(STATIC_MODULES)
  const [selectedCategory, setSelectedCategory] = useState<ModuleCategory>('memory')
  const [loading, setLoading] = useState(false)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const categoryList = useMemo(() => {
    const seen = new Set<ModuleCategory>()
    const result: ModuleCategory[] = []
    for (const mod of modules) {
      if (!seen.has(mod.category)) {
        seen.add(mod.category)
        result.push(mod.category)
      }
    }
    return result
  }, [modules])

  const loadModules = useCallback(async () => {
    setLoading(true)
    try {
      const online = await checkBridgeAvailable()
      if (!mountedRef.current) return
      setBridgeOnline(online)

      if (online) {
        const data = await bridgeClient.listModules()
        if (!mountedRef.current) return
        setModules(data)
      } else {
        setModules(STATIC_MODULES)
      }
    } catch {
      if (!mountedRef.current) return
      setBridgeOnline(false)
      setModules(STATIC_MODULES)
    } finally {
      if (mountedRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => { void loadModules() }, [loadModules])

  const handleAction = useCallback(async (moduleId: string, action: ModuleAction) => {
    try {
      await bridgeClient.performAction(moduleId, action)
      addToast(`${action} -> ${moduleId}`, 'success')
      void loadModules()
    } catch (err) {
      addToast(err instanceof Error ? err.message : t.requestFailed, 'error')
    }
  }, [addToast, loadModules, t.requestFailed])

  const filteredModules = modules.filter((m) => m.category === selectedCategory)

  const sectionCls = 'rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5'

  const headerActions = (
    <div className="flex items-center gap-3">
      <div className="flex items-center gap-1.5">
        <span
          className={`h-2 w-2 rounded-full ${bridgeOnline ? 'bg-emerald-500' : 'bg-[var(--c-text-muted)] opacity-40'}`}
        />
        <span className="text-xs text-[var(--c-text-muted)]">
          {bridgeOnline ? tm.bridgeOnline : tm.bridgeOffline}
        </span>
      </div>
      <button
        onClick={() => void loadModules()}
        disabled={loading}
        className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
      >
        <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
      </button>
    </div>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tm.title} actions={headerActions} />

      {loading && modules === STATIC_MODULES ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      ) : (
        <div className="flex flex-1 overflow-hidden">
          {/* Left: category list */}
          <div className="w-[160px] shrink-0 overflow-y-auto border-r border-[var(--c-border-console)] p-2">
            <div className="flex flex-col gap-[3px]">
              {categoryList.map((cat) => {
                const active = cat === selectedCategory
                return (
                  <button
                    key={cat}
                    onClick={() => setSelectedCategory(cat)}
                    className={[
                      'flex h-[30px] items-center rounded-[5px] px-3 text-sm font-medium transition-colors',
                      active
                        ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                        : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                    ].join(' ')}
                  >
                    {categoryLabel(cat, tm)}
                  </button>
                )
              })}
            </div>
          </div>

          {/* Right: modules in selected category */}
          <div className="flex-1 overflow-y-auto p-6">
            <div className="mx-auto max-w-xl space-y-6">
              <div className={sectionCls}>
                <div className="space-y-3">
                  {filteredModules.map((mod) => (
                    <ModuleRow
                      key={mod.id}
                      mod={mod}
                      bridgeOnline={bridgeOnline}
                      t={tm}
                      onAction={handleAction}
                    />
                  ))}
                  {filteredModules.length === 0 && (
                    <p className="text-sm text-[var(--c-text-muted)]">{tm.noModules}</p>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
