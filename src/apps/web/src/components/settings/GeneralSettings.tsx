import { useEffect, useState } from 'react'
import { Monitor, LogOut, HelpCircle, ArrowUpRight } from 'lucide-react'
import type { MeResponse } from '../../api'
import {
  listLlmProviders,
  listSpawnProfiles,
  resolveOpenVikingConfig,
  setSpawnProfile,
  deleteSpawnProfile,
} from '../../api'
import type { LlmProvider, SpawnProfile } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopMode, isDesktop, isLocalMode, getDesktopApi } from '@arkloop/shared/desktop'
import { openExternal } from '../../openExternal'
import { LanguageContent, ThemeModePicker } from './AppearanceSettings'
import { bridgeClient, checkBridgeAvailable } from '../../api-bridge'
import { SettingsModelDropdown } from './SettingsModelDropdown'

const SETTINGS_ENTER_SETTLE_DELAY_MS = 240

type Props = {
  me: MeResponse | null
  accessToken: string
  onLogout: () => void
  onMeUpdated?: (me: MeResponse) => void
}

export function GeneralSettings({ me, accessToken, onLogout, onMeUpdated: _onMeUpdated }: Props) {
  const { t, locale, setLocale } = useLocale()
  const ds = t.desktopSettings
  const localMode = isLocalMode()
  // 桌面壳或本地连接都不应出现「平台默认」文案（与 isDesktop 是否已挂 arkloop 解耦）
  const nonSaaSUi =
    getDesktopMode() !== null || isDesktop() || localMode

  const [osUsername, setOsUsername] = useState<string | null>(null)
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [toolProfile, setToolProfileState] = useState<SpawnProfile | null>(null)
  const [savingTool, setSavingTool] = useState(false)

  useEffect(() => {
    if (!localMode) return
    getDesktopApi()?.app.getOsUsername?.().then(setOsUsername).catch(() => {})
  }, [localMode])

  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(() => {
      listLlmProviders(accessToken)
        .then((nextProviders) => {
          if (!cancelled) setProviders(nextProviders)
        })
        .catch(() => {})
      listSpawnProfiles(accessToken)
        .then((ps) => {
          if (!cancelled) setToolProfileState(ps.find((p) => p.profile === 'tool') ?? null)
        })
        .catch(() => {})
    }, SETTINGS_ENTER_SETTLE_DELAY_MS)

    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [accessToken])

  const modelOptions = providers
    .flatMap((p) => p.models.filter((m) => m.show_in_picker).map((m) => ({
      value: `${p.name}^${m.model}`,
      label: `${p.name} / ${m.model}`,
    })))

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

  const handleToolModelChange = async (value: string) => {
    setSavingTool(true)
    try {
      if (value === '') {
        await deleteSpawnProfile(accessToken, 'tool')
      } else {
        await setSpawnProfile(accessToken, 'tool', value)
      }
      const ps = await listSpawnProfiles(accessToken)
      setToolProfileState(ps.find((p) => p.profile === 'tool') ?? null)
      void syncToolModelToOpenViking(value)
    } finally {
      setSavingTool(false)
    }
  }

  const syncToolModelToOpenViking = async (value: string) => {
    const desktopApi = getDesktopApi()
    if (!desktopApi?.config) {
      return
    }

    const currentConfig = await desktopApi.config.get()
    if (currentConfig.memory.provider !== 'openviking') {
      return
    }

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
      if (!resolved.vlm || !resolved.embedding) {
        return
      }

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

  const displayName = localMode ? (osUsername ?? me?.username ?? '?') : (me?.username ?? '?')
  const userInitial = displayName.charAt(0).toUpperCase()

  return (
    <div className="flex flex-col gap-6">
      {/* Profile */}
      <div
        className="flex items-center gap-4 rounded-xl bg-[var(--c-bg-menu)] px-5 py-4"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-base font-semibold"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-base font-semibold text-[var(--c-text-heading)]">
            {displayName === '?' ? t.loading : displayName}
          </span>
          {localMode ? (
            <span className="flex items-center gap-1 text-xs text-[var(--c-text-tertiary)]">
              <Monitor size={11} />
              {ds.localModeLabel ?? 'Local'}
            </span>
          ) : me?.email ? (
            <span className="truncate text-xs text-[var(--c-text-tertiary)]">{me.email}</span>
          ) : null}
        </div>
      </div>

      {/* Language & Theme — image-card picker */}
      <div className="flex flex-col gap-4">
        <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
        <ThemeModePicker />
      </div>

      {/* Tool Model */}
      <section>
        <p className="mb-2 text-sm font-medium text-[var(--c-text-heading)]">
          {ds.toolModel}
        </p>
        <div className="flex flex-col gap-2">
          <p className="text-xs text-[var(--c-text-tertiary)]">{ds.toolModelDesc}</p>
          <SettingsModelDropdown
            value={toolModelValue}
            options={modelOptions}
            placeholder={toolModelPlaceholder}
            disabled={savingTool}
            onChange={handleToolModelChange}
          />
          {!toolProfile?.has_override && toolProfile?.auto_model && (
            <p className="text-xs text-[var(--c-text-muted)]">{ds.toolModelAutoHint}</p>
          )}
        </div>
      </section>

      {/* Footer */}
      <div className="flex flex-col gap-1.5">
        <button
            type="button"
            onClick={() => openExternal('https://arkloop.ai/docs')}
            className="flex w-fit items-center gap-1.5 rounded-lg px-1 py-1 text-sm text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]"
          >
            <HelpCircle size={14} /> {t.getHelp} <ArrowUpRight size={11} />
          </button>
        {!isLocalMode() && (
          <button
            onClick={onLogout}
            className="flex w-fit items-center gap-1.5 rounded-lg px-1 py-1 text-sm text-[#ef4444] hover:text-[#f87171]"
          >
            <LogOut size={14} /> {t.logout}
          </button>
        )}
      </div>
    </div>
  )
}
