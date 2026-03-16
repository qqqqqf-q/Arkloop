import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  Copy, Check, RefreshCw, Loader2,
  CircleDot, CircleOff, CircleAlert, CirclePause, CirclePlay, CircleDashed,
} from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge, type BadgeVariant } from '../../components/Badge'
import { OperationModal } from '../../components/OperationModal'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { useOperations } from '@arkloop/shared'
import type { LocaleStrings } from '../../locales'
import { listPlatformSettings, type PlatformSetting } from '../../api/platform-settings'
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
type PromptGuardMode = 'unknown' | 'disabled' | 'local' | 'api'

const MODULES_CATEGORY_STORAGE_KEY = 'arkloop:console:modules:selected-category'
const PROMPT_GUARD_MODULE_ID = 'prompt-guard'
const SEMANTIC_ENABLED_KEY = 'security.injection_scan.semantic_enabled'
const SEMANTIC_PROVIDER_KEY = 'security.semantic_scanner.provider'

function readStoredModuleCategory(): ModuleCategory | null {
  try {
    const raw = window.localStorage.getItem(MODULES_CATEGORY_STORAGE_KEY)
    if (
      raw === 'memory' ||
      raw === 'sandbox' ||
      raw === 'search' ||
      raw === 'browser' ||
      raw === 'console' ||
      raw === 'security' ||
      raw === 'infrastructure'
    ) {
      return raw
    }
  } catch {}
  return null
}

function rememberModuleCategory(category: ModuleCategory) {
  try {
    window.localStorage.setItem(MODULES_CATEGORY_STORAGE_KEY, category)
  } catch {}
}

function resolvePromptGuardMode(settings: PlatformSetting[]): PromptGuardMode {
  const map = new Map(settings.map((item) => [item.key, item.value]))
  const semanticEnabled = map.get(SEMANTIC_ENABLED_KEY) === 'true'
  const provider = (map.get(SEMANTIC_PROVIDER_KEY) ?? '').trim().toLowerCase()

  if (!semanticEnabled || provider === '') return 'disabled'
  if (provider === 'api') return 'api'
  if (provider === 'local') return 'local'
  return 'unknown'
}

function applyPromptGuardModuleView(modules: ModuleInfo[], mode: PromptGuardMode): ModuleInfo[] {
  return modules.map((mod) => {
    if (mod.id !== PROMPT_GUARD_MODULE_ID) return mod
    if (mode === 'local' || mode === 'unknown') return mod
    if (mod.status !== 'running') return mod
    return { ...mod, status: 'stopped' }
  })
}

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
    security: t.tabSecurity,
    infrastructure: 'Infra',
  }
  return map[cat]
}

function availableActions(mod: ModuleInfo, bridgeOnline: boolean, promptGuardMode: PromptGuardMode): ModuleAction[] {
  if (!bridgeOnline) return []
  if (mod.id === PROMPT_GUARD_MODULE_ID) {
    if (promptGuardMode === 'local' && mod.status === 'not_installed' && mod.capabilities.installable) {
      return ['install']
    }
    return []
  }
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
    configure: t.actionConfigure,
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
  busy,
  t,
  onAction,
  promptGuardMode,
}: {
  mod: ModuleInfo
  bridgeOnline: boolean
  busy: boolean
  t: ModulesLocale
  onAction: (moduleId: string, action: ModuleAction) => void
  promptGuardMode: PromptGuardMode
}) {
  const actions = availableActions(mod, bridgeOnline, promptGuardMode)
  const command = INSTALL_COMMANDS[mod.id]
  const agentPrompt = AGENT_PROMPTS[mod.id]

  const displayStatus = bridgeOnline ? mod.status : null
  const badgeLabel = displayStatus ? statusLabel(displayStatus, t) : t.statusUnknown
  const badgeVariant = busy ? 'warning' as BadgeVariant : (displayStatus ? statusBadgeVariant(displayStatus) : 'neutral' as BadgeVariant)

  return (
    <div className="rounded-md border border-[var(--c-border-console)] px-3 py-2.5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="font-mono text-xs text-[var(--c-text-primary)]">{mod.name}</span>
          <Badge variant={badgeVariant}>
            <span className="flex items-center gap-1">
              {busy
                ? <Loader2 size={13} className="animate-spin" />
                : (displayStatus ? statusIcon(displayStatus) : <CircleDashed size={13} />)}
              {busy ? '...' : badgeLabel}
            </span>
          </Badge>
        </div>
        <div className="flex items-center gap-1.5">
          {!busy && actions.map((action) => (
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
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tm = t.pages.modules
  const { startOperation, isModuleBusy, operations } = useOperations()

  const [bridgeOnline, setBridgeOnline] = useState(false)
  const [modules, setModules] = useState<ModuleInfo[]>(STATIC_MODULES)
  const [selectedCategory, setSelectedCategory] = useState<ModuleCategory>(() => readStoredModuleCategory() ?? 'memory')
  const [promptGuardMode, setPromptGuardMode] = useState<PromptGuardMode>('unknown')
  const [loading, setLoading] = useState(false)
  const mountedRef = useRef(true)
  const prevActiveRef = useRef(0)
  const [activeOp, setActiveOp] = useState<{
    moduleId: string; moduleName: string; action: ModuleAction; operationId: string
  } | null>(null)

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

  useEffect(() => {
    if (categoryList.length === 0) return
    if (categoryList.includes(selectedCategory)) return
    const stored = readStoredModuleCategory()
    setSelectedCategory(stored && categoryList.includes(stored) ? stored : categoryList[0])
  }, [categoryList, selectedCategory])

  useEffect(() => {
    rememberModuleCategory(selectedCategory)
  }, [selectedCategory])

  const loadModules = useCallback(async () => {
    setLoading(true)
    try {
      const [online, settings] = await Promise.all([
        checkBridgeAvailable(),
        listPlatformSettings(accessToken).catch(() => [] as PlatformSetting[]),
      ])
      if (!mountedRef.current) return
      setBridgeOnline(online)
      const nextPromptGuardMode = resolvePromptGuardMode(settings)
      setPromptGuardMode(nextPromptGuardMode)

      if (online) {
        const data = await bridgeClient.listModules()
        if (!mountedRef.current) return
        setModules(applyPromptGuardModuleView(data, nextPromptGuardMode))
      } else {
        setModules(applyPromptGuardModuleView(STATIC_MODULES, nextPromptGuardMode))
      }
    } catch {
      if (!mountedRef.current) return
      setBridgeOnline(false)
      setPromptGuardMode('unknown')
      setModules(STATIC_MODULES)
    } finally {
      if (mountedRef.current) setLoading(false)
    }
  }, [accessToken])

  useEffect(() => { void loadModules() }, [loadModules])

  // reload modules when an operation finishes
  const activeCount = operations.filter((op) => op.status === 'running').length
  useEffect(() => {
    if (prevActiveRef.current > 0 && activeCount < prevActiveRef.current) {
      void loadModules()
    }
    prevActiveRef.current = activeCount
  }, [activeCount, loadModules])

  const handleAction = useCallback(async (moduleId: string, action: ModuleAction) => {
    try {
      const { operation_id } = await bridgeClient.performAction(moduleId, action)
      const mod = modules.find((m) => m.id === moduleId)
      const moduleName = mod?.name ?? moduleId
      startOperation(moduleId, moduleName, action, operation_id)
      setActiveOp({ moduleId, moduleName, action, operationId: operation_id })
    } catch (err) {
      addToast(err instanceof Error ? err.message : t.requestFailed, 'error')
    }
  }, [addToast, modules, startOperation, t.requestFailed])

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
                    onClick={() => {
                      rememberModuleCategory(cat)
                      setSelectedCategory(cat)
                    }}
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
                      busy={isModuleBusy(mod.id)}
                      t={tm}
                      onAction={handleAction}
                      promptGuardMode={promptGuardMode}
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

      {activeOp && (
        <OperationModal
          moduleId={activeOp.moduleId}
          moduleName={activeOp.moduleName}
          action={activeOp.action}
          operationId={activeOp.operationId}
          onClose={() => { setActiveOp(null); void loadModules() }}
        />
      )}
    </div>
  )
}
