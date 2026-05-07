import { useEffect, useMemo, useState } from 'react'
import { Loader2, X, Zap } from 'lucide-react'
import {
  deleteSpawnProfile,
  listLlmProviders,
  listSpawnProfiles,
  resolveOpenVikingConfig,
  setSpawnProfile,
  testLlmProviderModel,
} from '../../api'
import type { LlmProvider, SpawnProfile } from '../../api'
import { bridgeClient, checkBridgeAvailable } from '../../api-bridge'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi, getDesktopMode, isDesktop, isLocalMode } from '@arkloop/shared/desktop'
import { AnimatedCheck } from '../AnimatedCheck'
import { SettingsModelDropdown } from './SettingsModelDropdown'
import { SettingsButton, SettingsIconButton } from './_SettingsButton'

type Props = {
  accessToken: string
  disabled?: boolean
}

export function ToolModelSettingControl({ accessToken, disabled = false }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const [profiles, setProfiles] = useState<SpawnProfile[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; latency?: number; error?: string } | null>(null)
  const [showTestError, setShowTestError] = useState(false)
  const nonSaaSUi = getDesktopMode() !== null || isDesktop() || isLocalMode()

  useEffect(() => {
    listSpawnProfiles(accessToken).then(setProfiles).catch(() => {})
    listLlmProviders(accessToken).then(setProviders).catch(() => {})
  }, [accessToken])

  const modelOptions = useMemo(() => providers
    .flatMap((provider) => provider.models
      .filter((model) => model.show_in_picker)
      .map((model) => ({
        value: `${provider.name}^${model.model}`,
        label: `${provider.name} / ${model.model}`,
      }))), [providers])

  const toolProfile = profiles.find((profile) => profile.profile === 'tool')
  const toolModelValue = toolProfile?.has_override ? toolProfile.resolved_model : ''
  const toolModelPlaceholder = (() => {
    const autoModel = toolProfile?.auto_model
    if (autoModel) {
      const parts = autoModel.split('^')
      const displayName = parts.length === 2 ? `${parts[0]} / ${parts[1]}` : autoModel
      return `${displayName} (${ds.toolModelAutoSuffix})`
    }
    return nonSaaSUi ? ds.toolModelSameAsChat : ds.toolModelPlatformDefault
  })()
  const effectiveToolModelValue = toolModelValue || toolProfile?.auto_model || ''
  const toolModelSelection = useMemo(() => {
    if (!effectiveToolModelValue.includes('^')) return null
    const [providerName, ...rest] = effectiveToolModelValue.split('^')
    const modelName = rest.join('^')
    if (!providerName || !modelName) return null
    const provider = providers.find((item) => item.name === providerName)
    const model = provider?.models.find((item) => item.model === modelName)
    if (!provider || !model) return null
    return { provider, model }
  }, [effectiveToolModelValue, providers])

  const buildOpenVikingConfigureParams = (
    rootApiKey: string | undefined,
    vlm: NonNullable<Awaited<ReturnType<typeof resolveOpenVikingConfig>>['vlm']>,
    embedding: NonNullable<Awaited<ReturnType<typeof resolveOpenVikingConfig>>['embedding']>,
  ): Record<string, unknown> => ({
    embedding_provider: embedding.provider,
    embedding_model: embedding.model,
    embedding_api_key: embedding.api_key,
    embedding_api_base: embedding.api_base,
    embedding_extra_headers: embedding.extra_headers ?? {},
    embedding_dimension: String(embedding.dimension),
    vlm_provider: vlm.provider,
    vlm_model: vlm.model,
    vlm_api_key: vlm.api_key,
    vlm_api_base: vlm.api_base,
    vlm_extra_headers: vlm.extra_headers ?? {},
    root_api_key: rootApiKey ?? null,
  })

  const syncToolModelToOpenViking = async (value: string) => {
    const desktopApi = getDesktopApi()
    if (!desktopApi?.config) return

    const currentConfig = await desktopApi.config.get()
    if (currentConfig.memory.provider !== 'openviking') return

    const currentOV = currentConfig.memory.openviking ?? {}
    const providerName = value.split('^', 1)[0] ?? ''
    const modelName = value.includes('^') ? value.split('^').slice(1).join('^') : ''
    const matchedProvider = providers.find((provider) => provider.name === providerName)

    const nextOV = {
      ...currentOV,
      vlmSelector: value || undefined,
      vlmModel: modelName || undefined,
      vlmProvider: matchedProvider?.provider ?? currentOV.vlmProvider,
      vlmApiKey: undefined,
      vlmApiBase: matchedProvider?.base_url ?? currentOV.vlmApiBase,
    }

    if (
      value === ''
      || !currentOV.embeddingSelector
      || !(await checkBridgeAvailable().catch(() => false))
    ) {
      await desktopApi.config.set({
        ...currentConfig,
        memory: {
          ...currentConfig.memory,
          openviking: nextOV,
        },
      })
      return
    }

    try {
      const resolved = await resolveOpenVikingConfig(accessToken, {
        vlm_selector: value,
        embedding_selector: currentOV.embeddingSelector,
        embedding_dimension_hint: currentOV.embeddingDimension,
      })
      if (!resolved.vlm || !resolved.embedding) return

      const params = buildOpenVikingConfigureParams(currentOV.rootApiKey, resolved.vlm, resolved.embedding)
      const { operation_id } = await bridgeClient.performAction('openviking', 'configure', params)
      await new Promise<void>((resolve, reject) => {
        let done = false
        const stop = bridgeClient.streamOperation(operation_id, () => {}, (result) => {
          if (done) return
          done = true
          stop()
          if (result.status === 'completed') resolve()
          else reject(new Error(result.error ?? 'configure failed'))
        })
      })

      const syncedOV = {
        ...nextOV,
        vlmSelector: resolved.vlm.selector,
        vlmProvider: resolved.vlm.provider,
        vlmModel: resolved.vlm.model,
        vlmApiKey: undefined,
        vlmApiBase: resolved.vlm.api_base,
        embeddingSelector: resolved.embedding.selector,
        embeddingProvider: resolved.embedding.provider,
        embeddingModel: resolved.embedding.model,
        embeddingApiKey: undefined,
        embeddingApiBase: resolved.embedding.api_base,
        embeddingDimension: resolved.embedding.dimension,
      }
      await desktopApi.config.set({
        ...currentConfig,
        memory: {
          ...currentConfig.memory,
          openviking: syncedOV,
        },
      })
    } catch {
      // 工具模型保存不应被 OpenViking 同步失败阻断。
    }
  }

  const handleChange = async (value: string) => {
    setSaving(true)
    setTestResult(null)
    try {
      if (value === '') {
        await deleteSpawnProfile(accessToken, 'tool')
      } else {
        await setSpawnProfile(accessToken, 'tool', value)
      }
      const updated = await listSpawnProfiles(accessToken)
      setProfiles(updated)
      void syncToolModelToOpenViking(value)
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    if (!toolModelSelection) return
    setTesting(true)
    try {
      const result = await testLlmProviderModel(accessToken, toolModelSelection.provider.id, toolModelSelection.model.id)
      setTestResult({ success: result.success, latency: result.latency_ms ?? undefined, error: result.error ?? undefined })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error'
      setTestResult({ success: false, error: message })
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex w-full items-center gap-2">
        <div className="min-w-0 flex-1">
          <SettingsModelDropdown
            value={toolModelValue}
            options={modelOptions}
            placeholder={toolModelPlaceholder}
            disabled={disabled || saving}
            onChange={(value) => void handleChange(value)}
          />
        </div>
        <SettingsIconButton
          label={ds.toolModel}
          onClick={() => {
            if (testResult?.success) {
              setTestResult(null)
              return
            }
            void handleTest()
          }}
          disabled={testing || (!toolModelSelection && !testResult)}
          className="h-9 w-9"
        >
          {testing
            ? <Loader2 size={14} className="animate-spin" />
            : testResult
              ? testResult.success
                ? <AnimatedCheck size={14} color="var(--c-status-success-text)" />
                : <X size={14} className="text-[var(--c-status-error-text)]" />
              : <Zap size={14} strokeWidth={1.5} />}
        </SettingsIconButton>
        {testResult && !testResult.success && !testing && (
          <div className="relative">
            <SettingsButton
              variant="danger"
              onClick={() => setShowTestError((value) => !value)}
              className="h-9 shrink-0 text-xs"
            >
              Error
            </SettingsButton>
            {showTestError && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowTestError(false)} />
                <div
                  className="dropdown-menu absolute right-0 top-[calc(100%+6px)] z-50 max-w-[320px] min-w-[200px]"
                  style={{
                    border: '0.5px solid var(--c-border-subtle)',
                    borderRadius: '10px',
                    padding: '12px',
                    background: 'var(--c-bg-menu)',
                    boxShadow: 'var(--c-dropdown-shadow)',
                    maxHeight: '160px',
                    overflowY: 'auto',
                  }}
                >
                  <pre className="whitespace-pre-wrap break-all text-xs text-[var(--c-text-secondary)]">{testResult.error ?? ''}</pre>
                </div>
              </>
            )}
          </div>
        )}
      </div>
      {!toolProfile?.has_override && toolProfile?.auto_model && (
        <p className="text-xs text-[var(--c-text-muted)]">{ds.toolModelAutoHint}</p>
      )}
    </div>
  )
}
