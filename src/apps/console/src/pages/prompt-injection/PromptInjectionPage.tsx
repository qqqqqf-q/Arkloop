import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getPlatformSetting, setPlatformSetting, deletePlatformSetting } from '../../api/platform-settings'
import { listAuditLogs } from '../../api/audit'
import { bridgeClient, checkBridgeAvailable } from '../../api/bridge'
import {
  TabBar, AuditTab, SemanticSetupPanel, LayerCard,
  SETTING_KEYS, LAYERS, TABS,
  type Tab,
} from '@arkloop/shared/components/prompt-injection'

export function PromptInjectionPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tp = t.pages.promptInjection

  const [activeTab, setActiveTab] = useState<Tab>('layers')
  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState('')
  const [settings, setSettings] = useState<Record<string, boolean>>({})
  const [semanticProvider, setSemanticProvider] = useState('')
  const [semanticEndpoint, setSemanticEndpoint] = useState('')
  const [bridgeAvailable, setBridgeAvailable] = useState(false)
  const [localModelInstalled, setLocalModelInstalled] = useState(false)
  const [semanticSetupOpen, setSemanticSetupOpen] = useState(false)

  const loadSettings = useCallback(async () => {
    setLoading(true)
    try {
      const [regexResult, trustResult, semanticResult, providerResult, endpointResult, blockingResult, toolScanResult] = await Promise.all([
        getPlatformSetting(SETTING_KEYS.REGEX_ENABLED, accessToken).catch(() => ({ value: 'true' })),
        getPlatformSetting(SETTING_KEYS.TRUST_SOURCE_ENABLED, accessToken).catch(() => ({ value: 'true' })),
        getPlatformSetting(SETTING_KEYS.SEMANTIC_ENABLED, accessToken).catch(() => ({ value: 'true' })),
        getPlatformSetting(SETTING_KEYS.SEMANTIC_PROVIDER, accessToken).catch(() => ({ value: '' })),
        getPlatformSetting(SETTING_KEYS.SEMANTIC_API_ENDPOINT, accessToken).catch(() => ({ value: '' })),
        getPlatformSetting(SETTING_KEYS.BLOCKING_ENABLED, accessToken).catch(() => ({ value: 'false' })),
        getPlatformSetting(SETTING_KEYS.TOOL_SCAN_ENABLED, accessToken).catch(() => ({ value: 'true' })),
      ])
      setSettings({
        [SETTING_KEYS.REGEX_ENABLED]: regexResult.value === 'true',
        [SETTING_KEYS.TRUST_SOURCE_ENABLED]: trustResult.value === 'true',
        [SETTING_KEYS.SEMANTIC_ENABLED]: semanticResult.value === 'true',
        [SETTING_KEYS.BLOCKING_ENABLED]: blockingResult.value === 'true',
        [SETTING_KEYS.TOOL_SCAN_ENABLED]: toolScanResult.value === 'true',
      })
      setSemanticProvider(providerResult.value)
      setSemanticEndpoint(endpointResult.value)

      const online = await checkBridgeAvailable()
      setBridgeAvailable(online)

      if (online && providerResult.value === 'local') {
        try {
          const modules = await bridgeClient.listModules()
          const pg = modules.find(m => m.id === 'prompt-guard')
          setLocalModelInstalled(pg?.status === 'running' || pg?.status === 'installed_disconnected')
        } catch {
          setLocalModelInstalled(false)
        }
      }
    } catch (err) {
      addToast(isApiError(err) ? err.message : tp.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tp.toastLoadFailed])

  useEffect(() => { loadSettings() }, [loadSettings])

  const handleToggle = useCallback(async (key: string, current: boolean) => {
    if (toggling) return
    setToggling(key)
    setSettings(prev => ({ ...prev, [key]: !current }))
    try {
      await setPlatformSetting(key, String(!current), accessToken)
      addToast(tp.toastUpdated, 'success')
    } catch (err) {
      // 回滚
      setSettings(prev => ({ ...prev, [key]: current }))
      addToast(isApiError(err) ? err.message : tp.toastFailed, 'error')
    } finally {
      setToggling('')
    }
  }, [toggling, accessToken, addToast, tp.toastUpdated, tp.toastFailed])

  const handleReconfigure = useCallback(async () => {
    try {
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_PROVIDER, accessToken).catch(() => {})
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_API_ENDPOINT, accessToken).catch(() => {})
      await deletePlatformSetting(SETTING_KEYS.SEMANTIC_API_KEY, accessToken).catch(() => {})
      await setPlatformSetting(SETTING_KEYS.SEMANTIC_ENABLED, 'false', accessToken)
      setSemanticProvider('')
      setSettings(prev => ({ ...prev, [SETTING_KEYS.SEMANTIC_ENABLED]: false }))
      setSemanticSetupOpen(true)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tp.toastFailed, 'error')
    }
  }, [accessToken, addToast, tp.toastFailed])

  const tabItems = TABS.map(key => ({
    key,
    label: key === 'layers' ? tp.tabLayers : tp.tabAudit,
  }))

  const semanticConfigured = semanticProvider !== ''
  const semanticCanEnable = semanticProvider === 'api'
    ? semanticEndpoint !== ''
    : semanticProvider === 'local'
      ? localModelInstalled
      : false

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tp.title} />
      <div className="flex-1 overflow-y-auto p-6">
        <p className="mb-4 text-sm text-[var(--c-text-secondary)]">{tp.description}</p>

        <TabBar tabs={tabItems} active={activeTab} onChange={setActiveTab} />

        {activeTab === 'layers' && (
          loading ? (
            <div className="flex items-center justify-center py-16">
              <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {LAYERS.map(layer => {
                const enabled = settings[layer.settingsKey] ?? (layer.defaultEnabled !== undefined ? layer.defaultEnabled : true)
                const isSemantic = layer.id === 'semantic'
                return (
                  <LayerCard
                    key={layer.id}
                    layer={layer}
                    enabled={enabled}
                    toggling={toggling === layer.settingsKey}
                    texts={tp}
                    semanticConfigured={semanticConfigured}
                    semanticProvider={semanticProvider}
                    localModelInstalled={localModelInstalled}
                    semanticCanEnable={semanticCanEnable}
                    onToggle={() => handleToggle(layer.settingsKey, enabled)}
                    onReconfigure={handleReconfigure}
                    onSetupToggle={() => setSemanticSetupOpen(v => !v)}
                    setupPanel={
                      isSemantic && !semanticConfigured && semanticSetupOpen ? (
                        <SemanticSetupPanel
                          accessToken={accessToken}
                          bridgeAvailable={bridgeAvailable}
                          onSaved={loadSettings}
                          texts={tp}
                          setSetting={setPlatformSetting}
                          bridgeInstall={v => bridgeClient.performAction('prompt-guard', 'install', { variant: v })}
                          formatError={err => isApiError(err) ? err.message : tp.toastFailed}
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
            texts={tp}
            listAuditLogs={listAuditLogs}
          />
        )}
      </div>
    </div>
  )
}
