import { useEffect, useRef, useState } from 'react'
import { Monitor, LogOut, HelpCircle, ChevronDown } from 'lucide-react'
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
import { isLocalMode, getDesktopApi } from '@arkloop/shared/desktop'
import { LanguageContent, ThemeModePicker } from './AppearanceSettings'
import { bridgeClient, checkBridgeAvailable } from '../../api-bridge'

type Props = {
  me: MeResponse | null
  accessToken: string
  onLogout: () => void
  onMeUpdated?: (me: MeResponse) => void
}

type ModelOption = { value: string; label: string }

const OPENVIKING_COMPATIBLE_PROVIDER = 'openai'

function ModelDropdown({
  value,
  options,
  placeholder,
  disabled,
  onChange,
}: {
  value: string
  options: ModelOption[]
  placeholder: string
  disabled: boolean
  onChange: (v: string) => void
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  const currentLabel = options.find(o => o.value === value)?.label ?? (value || placeholder)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node)
      ) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="relative">
      <button
        ref={btnRef}
        type="button"
        disabled={disabled}
        onClick={() => setOpen(v => !v)}
        className="flex h-9 w-full items-center justify-between rounded-lg px-3 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-secondary)' }}
      >
        <span className="truncate">{currentLabel}</span>
        <ChevronDown size={13} className="ml-2 shrink-0" />
      </button>

      {open && (
        <div
          ref={menuRef}
          className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            width: '100%',
            boxShadow: 'var(--c-dropdown-shadow)',
            maxHeight: '220px',
            overflowY: 'auto',
          }}
        >
          <button
            type="button"
            onClick={() => { onChange(''); setOpen(false) }}
            className="flex w-full items-center px-3 py-2 text-sm transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
            style={{
              borderRadius: '8px',
              fontWeight: !value ? 600 : 400,
              color: !value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
            }}
          >
            {placeholder}
          </button>
          {options.map(({ value: v, label }) => (
            <button
              key={v}
              type="button"
              onClick={() => { onChange(v); setOpen(false) }}
              className="flex w-full items-center px-3 py-2 text-sm transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
              style={{
                borderRadius: '8px',
                fontWeight: value === v ? 600 : 400,
                color: value === v ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
              }}
            >
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

export function GeneralSettings({ me, accessToken, onLogout, onMeUpdated: _onMeUpdated }: Props) {
  const { t, locale, setLocale } = useLocale()
  const ds = t.desktopSettings
  const localMode = isLocalMode()

  const [osUsername, setOsUsername] = useState<string | null>(null)
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [toolProfile, setToolProfileState] = useState<SpawnProfile | null>(null)
  const [savingTool, setSavingTool] = useState(false)

  useEffect(() => {
    if (!localMode) return
    getDesktopApi()?.app.getOsUsername?.().then(setOsUsername).catch(() => {})
  }, [localMode])

  useEffect(() => {
    listLlmProviders(accessToken).then(setProviders).catch(() => {})
    listSpawnProfiles(accessToken)
      .then((ps) => setToolProfileState(ps.find((p) => p.profile === 'tool') ?? null))
      .catch(() => {})
  }, [accessToken])

  const modelOptions: ModelOption[] = providers
    .flatMap((p) => p.models.filter((m) => m.show_in_picker).map((m) => ({
      value: `${p.name}^${m.model}`,
      label: `${p.name} / ${m.model}`,
    })))

  const toolModelValue = toolProfile?.has_override ? toolProfile.resolved_model : ''

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

    let nextOV = {
      ...currentOV,
      vlmSelector: value || undefined,
      vlmModel: modelName || undefined,
      vlmProvider: matchedProvider?.provider ?? currentOV.vlmProvider,
      vlmApiKey: undefined,
      vlmApiBase: matchedProvider?.base_url ?? currentOV.vlmApiBase,
    }
    await desktopApi.config.set({
      ...currentConfig,
      memory: {
        ...currentConfig.memory,
        openviking: nextOV,
      },
    })

    if (
      value === ''
      || !currentOV.embeddingSelector
      || matchedProvider?.provider !== OPENVIKING_COMPATIBLE_PROVIDER
      || !(await checkBridgeAvailable().catch(() => false))
    ) {
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

      const params: Record<string, string> = {
        'embedding.provider': resolved.embedding.provider,
        'embedding.model': resolved.embedding.model,
        'embedding.api_key': resolved.embedding.api_key,
        'embedding.api_base': resolved.embedding.api_base,
        'embedding.dimension': String(resolved.embedding.dimension),
        'vlm.provider': resolved.vlm.provider,
        'vlm.model': resolved.vlm.model,
        'vlm.api_key': resolved.vlm.api_key,
        'vlm.api_base': resolved.vlm.api_base,
      }
      if (currentOV.rootApiKey) {
        params.root_api_key = currentOV.rootApiKey
      }

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

      nextOV = {
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
          openviking: nextOV,
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
        className="flex items-center gap-3 rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-sm font-semibold text-[var(--c-text-heading)]">
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
          <ModelDropdown
            value={toolModelValue}
            options={modelOptions}
            placeholder={ds.toolModelPlatformDefault}
            disabled={savingTool}
            onChange={handleToolModelChange}
          />
        </div>
      </section>

      {/* Footer */}
      <div className="flex flex-col gap-1.5">
        <a
          href="https://arkloop.ai/docs"
          target="_blank"
          rel="noopener noreferrer"
          className="flex w-fit items-center gap-1.5 rounded-lg px-1 py-1 text-sm text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]"
        >
          <HelpCircle size={14} /> {t.getHelp}
        </a>
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
