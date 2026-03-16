import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { ShieldAlert, Loader2 } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { useOperations } from '@arkloop/shared'
import {
  listPlatformSettings,
  updatePlatformSetting,
  deletePlatformSetting,
  isApiError,
} from '../api'
import { listAuditLogs } from '../api/audit'
import { bridgeClient, checkBridgeAvailable } from '../api/bridge'
import {
  TabBar, AuditTab, SemanticSetupPanel, LayerCard,
  SETTING_KEYS, LAYERS, TABS,
  type Tab,
} from '@arkloop/shared/components/prompt-injection'

export function SecurityPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const { startOperation, setHistoryOpen } = useOperations()
  const ts = t.security

  const [activeTab, setActiveTab] = useState<Tab>('layers')
  const [values, setValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState<string | null>(null)
  const [semanticProvider, setSemanticProvider] = useState<string>('')
  const [bridgeAvailable, setBridgeAvailable] = useState(false)
  const [localModelInstalled, setLocalModelInstalled] = useState(false)
  const [semanticSetupOpen, setSemanticSetupOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await listPlatformSettings(accessToken)
      const map: Record<string, string> = {}
      for (const s of list) map[s.key] = s.value
      setValues(map)

      const provider = map[SETTING_KEYS.SEMANTIC_PROVIDER] ?? ''
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
    setValues(prev => ({ ...prev, [key]: String(!current) }))
    try {
      await updatePlatformSetting(key, String(!current), accessToken)
      addToast(ts.toastUpdated, 'success')
    } catch (err) {
      // 回滚
      setValues(prev => ({ ...prev, [key]: String(current) }))
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    } finally {
      setToggling(null)
    }
  }, [accessToken, addToast, ts.toastUpdated, ts.toastFailed])

  const handleReconfigure = useCallback(async () => {
    try {
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_PROVIDER, accessToken).catch(() => {})
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_API_ENDPOINT, accessToken).catch(() => {})
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_API_KEY, accessToken).catch(() => {})
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_API_MODEL, accessToken).catch(() => {})
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_API_TIMEOUT_MS, accessToken).catch(() => {})
      await updatePlatformSetting(SETTING_KEYS.SEMANTIC_ENABLED, 'false', accessToken)
      setSemanticProvider('')
      setValues(prev => ({
        ...prev,
        [SETTING_KEYS.SEMANTIC_ENABLED]: 'false',
        [SETTING_KEYS.SEMANTIC_PROVIDER]: '',
        [SETTING_KEYS.SEMANTIC_API_ENDPOINT]: '',
        [SETTING_KEYS.SEMANTIC_API_KEY]: '',
        [SETTING_KEYS.SEMANTIC_API_MODEL]: '',
        [SETTING_KEYS.SEMANTIC_API_TIMEOUT_MS]: '',
      }))
      setSemanticSetupOpen(true)
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    }
  }, [accessToken, addToast, ts.toastFailed])

  const isEnabled = (key: string, defaultVal = true) =>
    key in values ? values[key] === 'true' : defaultVal

  const semanticConfigured = semanticProvider !== ''
  const semanticEndpoint = values[SETTING_KEYS.SEMANTIC_API_ENDPOINT] ?? ''
  const semanticModel = values[SETTING_KEYS.SEMANTIC_API_MODEL] ?? 'openai/gpt-oss-safeguard-20b'
  const semanticTimeoutMs = values[SETTING_KEYS.SEMANTIC_API_TIMEOUT_MS] ?? '4000'
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
              {LAYERS.map(layer => {
                const enabled = isEnabled(layer.settingsKey, layer.defaultEnabled !== undefined ? layer.defaultEnabled : true)
                const isSemantic = layer.id === 'semantic'
                return (
                  <LayerCard
                    key={layer.id}
                    layer={layer}
                    enabled={enabled}
                    toggling={toggling === layer.settingsKey}
                    texts={ts}
                    semanticConfigured={semanticConfigured}
                    semanticProvider={semanticProvider}
                    localModelInstalled={localModelInstalled}
                    semanticCanEnable={semanticCanEnable}
                    onToggle={() => void toggle(layer.settingsKey, enabled)}
                    onReconfigure={() => void handleReconfigure()}
                    onSetupToggle={() => setSemanticSetupOpen(v => !v)}
                    setupPanel={
                      isSemantic && !semanticConfigured && semanticSetupOpen ? (
                        <SemanticSetupPanel
                          accessToken={accessToken}
                          bridgeAvailable={bridgeAvailable}
                          onSaved={load}
                          texts={ts}
                          setSetting={updatePlatformSetting}
                          bridgeInstall={v => bridgeClient.performAction('prompt-guard', 'install', { variant: v })}
                          waitForInstallCompletion={opId => bridgeClient.waitForOperation(opId)}
                          formatError={err => err instanceof Error ? err.message : ts.toastFailed}
                          defaultMode="api"
                          initialApiEndpoint={semanticEndpoint}
                          initialApiModel={semanticModel}
                          initialApiTimeoutMs={semanticTimeoutMs}
                          onInstallStarted={opId => {
                            startOperation('prompt-guard', 'Prompt Guard', 'install', opId)
                            setHistoryOpen(true)
                          }}
                        />
                      ) : undefined
                    }
                  />
                )
              })}
            </div>
          )
        )}

        {activeTab === 'audit' && (
          <AuditTab
            accessToken={accessToken}
            texts={ts}
            listAuditLogs={listAuditLogs}
          />
        )}
      </div>
    </div>
  )
}
