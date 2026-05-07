import { useState, useEffect, useCallback } from 'react'
import { Loader2 } from 'lucide-react'
import { isApiError } from '@arkloop/shared/api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listPlatformSettings,
  updatePlatformSetting,
  deletePlatformSetting,
  listAuditLogs,
} from '../../api-admin'
import { bridgeClient, checkBridgeAvailable } from '../../api-bridge'
import {
  AuditTab,
  SemanticSetupPanel,
  LayerCard,
  SETTING_KEYS,
  LAYERS,
  TABS,
  type Tab,
} from '@arkloop/shared/components/prompt-injection'
import { useToast } from '@arkloop/shared'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { SettingsSegmentedControl } from './_SettingsSegmentedControl'

type Props = { accessToken: string }

export function DesktopPromptInjectionSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const { addToast } = useToast()
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
      await updatePlatformSetting(accessToken, key, String(!current))
      addToast(ts.toastUpdated, 'success')
    } catch (err) {
      setValues(prev => ({ ...prev, [key]: String(current) }))
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    } finally {
      setToggling(null)
    }
  }, [accessToken, addToast, ts.toastUpdated, ts.toastFailed])

  const handleReconfigure = useCallback(async () => {
    try {
      await deletePlatformSetting(accessToken, SETTING_KEYS.SEMANTIC_PROVIDER).catch((err) => { console.error('delete semantic_provider failed', err) })
      await deletePlatformSetting(accessToken, SETTING_KEYS.SEMANTIC_API_ENDPOINT).catch((err) => { console.error('delete semantic_api_endpoint failed', err) })
      await deletePlatformSetting(accessToken, SETTING_KEYS.SEMANTIC_API_KEY).catch((err) => { console.error('delete semantic_api_key failed', err) })
      await deletePlatformSetting(accessToken, SETTING_KEYS.SEMANTIC_API_MODEL).catch((err) => { console.error('delete semantic_api_model failed', err) })
      await deletePlatformSetting(accessToken, SETTING_KEYS.SEMANTIC_API_TIMEOUT_MS).catch((err) => { console.error('delete semantic_api_timeout_ms failed', err) })
      await updatePlatformSetting(accessToken, SETTING_KEYS.SEMANTIC_ENABLED, 'false')
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

  const setPlatformSetting = (key: string, value: string, token: string) =>
    updatePlatformSetting(token, key, value)

  return (
    <div className="flex max-w-[900px] flex-col gap-6 pb-10">
      <SettingsSectionHeader title={ts.title} description={ts.description} />

      <SettingsSegmentedControl<Tab>
        options={tabItems.map((item) => ({ value: item.key, label: item.label }))}
        value={activeTab}
        onChange={setActiveTab}
      />

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
                        setSetting={setPlatformSetting}
                        bridgeInstall={v => bridgeClient.performAction('prompt-guard', 'install', { variant: v })}
                        waitForInstallCompletion={opId => bridgeClient.waitForOperation(opId)}
                        formatError={err => err instanceof Error ? err.message : ts.toastFailed}
                        defaultMode="api"
                        initialApiEndpoint={semanticEndpoint}
                        initialApiModel={semanticModel}
                        initialApiTimeoutMs={semanticTimeoutMs}
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
          listAuditLogs={(params, token) => listAuditLogs(token, params)}
        />
      )}
    </div>
  )
}
