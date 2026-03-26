import { useEffect, useMemo, useState } from 'react'
import { MessageCircleMore } from 'lucide-react'
import {
  type ChannelBindingResponse,
  type ChannelResponse,
  type LlmProvider,
  type Persona,
  createChannel,
  createChannelBindCode,
  deleteChannelBinding,
  isApiError,
  listChannelBindings,
  updateChannel,
  updateChannelBinding,
  verifyChannel,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { SettingsPillToggle } from './_SettingsPillToggle'
import {
  BindingsCard,
  buildModelOptions,
  ListField,
  mergeListValues,
  ModelDropdown,
  readStringArrayConfig,
  resolvePersonaID,
  sameItems,
  SaveActions,
  StatusBadge,
  TokenField,
} from './DesktopChannelSettingsShared'

type Props = {
  accessToken: string
  channel: ChannelResponse | null
  personas: Persona[]
  providers: LlmProvider[]
  reload: () => Promise<void>
}

export function DesktopDiscordSettingsPanel({
  accessToken,
  channel,
  personas,
  providers,
  reload,
}: Props) {
  const { t } = useLocale()
  const ct = t.channels
  const ds = t.desktopSettings

  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const [enabled, setEnabled] = useState(channel?.is_active ?? false)
  const [personaID, setPersonaID] = useState(resolvePersonaID(personas, channel?.persona_id))
  const [tokenDraft, setTokenDraft] = useState('')
  const [defaultModel, setDefaultModel] = useState((channel?.config_json?.default_model as string | undefined) ?? '')
  const [allowedServerIDs, setAllowedServerIDs] = useState(readStringArrayConfig(channel, 'allowed_server_ids'))
  const [allowedServerInput, setAllowedServerInput] = useState('')
  const [allowedChannelIDs, setAllowedChannelIDs] = useState(readStringArrayConfig(channel, 'allowed_channel_ids'))
  const [allowedChannelInput, setAllowedChannelInput] = useState('')
  const [verifying, setVerifying] = useState(false)
  const [verifyResult, setVerifyResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [bindCode, setBindCode] = useState<string | null>(null)
  const [generatingCode, setGeneratingCode] = useState(false)
  const [bindings, setBindings] = useState<ChannelBindingResponse[]>([])

  useEffect(() => {
    setEnabled(channel?.is_active ?? false)
    setPersonaID(resolvePersonaID(personas, channel?.persona_id))
    setTokenDraft('')
    setDefaultModel((channel?.config_json?.default_model as string | undefined) ?? '')
    setAllowedServerIDs(readStringArrayConfig(channel, 'allowed_server_ids'))
    setAllowedServerInput('')
    setAllowedChannelIDs(readStringArrayConfig(channel, 'allowed_channel_ids'))
    setAllowedChannelInput('')
    setVerifyResult(null)
  }, [channel, personas])

  useEffect(() => {
    if (!channel?.id) {
      setBindings([])
      return
    }
    let cancelled = false
    listChannelBindings(accessToken, channel.id)
      .then((items) => {
        if (!cancelled) setBindings(items)
      })
      .catch(() => {
        if (!cancelled) setBindings([])
      })
    return () => {
      cancelled = true
    }
  }, [accessToken, channel?.id])

  const modelOptions = useMemo(() => buildModelOptions(providers), [providers])
  const persistedAllowedServerIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_server_ids'), [channel])
  const persistedAllowedChannelIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_channel_ids'), [channel])
  const effectiveAllowedServerIDs = useMemo(
    () => mergeListValues(allowedServerIDs, allowedServerInput),
    [allowedServerIDs, allowedServerInput],
  )
  const effectiveAllowedChannelIDs = useMemo(
    () => mergeListValues(allowedChannelIDs, allowedChannelInput),
    [allowedChannelIDs, allowedChannelInput],
  )
  const effectivePersonaID = useMemo(
    () => resolvePersonaID(personas, channel?.persona_id),
    [personas, channel?.persona_id],
  )
  const persistedDefaultModel = (channel?.config_json?.default_model as string | undefined) ?? ''
  const tokenConfigured = channel?.has_credentials === true
  const dirty = useMemo(() => {
    if ((channel?.is_active ?? false) !== enabled) return true
    if (effectivePersonaID !== personaID) return true
    if (!sameItems(persistedAllowedServerIDs, effectiveAllowedServerIDs)) return true
    if (!sameItems(persistedAllowedChannelIDs, effectiveAllowedChannelIDs)) return true
    if (defaultModel !== persistedDefaultModel) return true
    return tokenDraft.trim().length > 0
  }, [
    channel,
    defaultModel,
    effectiveAllowedChannelIDs,
    effectiveAllowedServerIDs,
    effectivePersonaID,
    enabled,
    personaID,
    persistedAllowedChannelIDs,
    persistedAllowedServerIDs,
    persistedDefaultModel,
    tokenDraft,
  ])
  const canSave = dirty && (channel !== null || tokenDraft.trim().length > 0)

  const handleAddServerIDs = () => {
    const nextIDs = mergeListValues(allowedServerIDs, allowedServerInput)
    if (nextIDs.length === allowedServerIDs.length) return
    setAllowedServerIDs(nextIDs)
    setAllowedServerInput('')
    setSaved(false)
  }

  const handleAddChannelIDs = () => {
    const nextIDs = mergeListValues(allowedChannelIDs, allowedChannelInput)
    if (nextIDs.length === allowedChannelIDs.length) return
    setAllowedChannelIDs(nextIDs)
    setAllowedChannelInput('')
    setSaved(false)
  }

  const handleSave = async () => {
    const nextAllowedServerIDs = mergeListValues(allowedServerIDs, allowedServerInput)
    const nextAllowedChannelIDs = mergeListValues(allowedChannelIDs, allowedChannelInput)

    if (enabled && !personaID) {
      setError(ct.personaRequired)
      return
    }

    setSaving(true)
    setError('')
    try {
      const base =
        channel?.config_json !== null &&
        channel?.config_json !== undefined &&
        typeof channel.config_json === 'object' &&
        !Array.isArray(channel.config_json)
          ? { ...(channel.config_json as Record<string, unknown>) }
          : {}

      const configJSON: Record<string, unknown> = {
        ...base,
        allowed_server_ids: nextAllowedServerIDs,
        allowed_channel_ids: nextAllowedChannelIDs,
      }
      if (defaultModel.trim()) configJSON.default_model = defaultModel.trim()
      else delete configJSON.default_model

      if (channel == null) {
        const created = await createChannel(accessToken, {
          channel_type: 'discord',
          bot_token: tokenDraft.trim(),
          persona_id: personaID || undefined,
          config_json: configJSON,
        })
        if (enabled) {
          await updateChannel(accessToken, created.id, { is_active: true })
        }
      } else {
        await updateChannel(accessToken, channel.id, {
          bot_token: tokenDraft.trim() || undefined,
          persona_id: personaID || null,
          is_active: enabled,
          config_json: configJSON,
        })
      }

      setAllowedServerIDs(nextAllowedServerIDs)
      setAllowedServerInput('')
      setAllowedChannelIDs(nextAllowedChannelIDs)
      setAllowedChannelInput('')
      setSaved(true)
      setTimeout(() => setSaved(false), 2500)
      await reload()
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        setError(ds.connectorSaveTimeout)
      } else {
        setError(isApiError(err) ? err.message : ct.saveFailed)
      }
    } finally {
      setSaving(false)
    }
  }

  const handleVerify = async () => {
    if (!channel) return
    setVerifying(true)
    setVerifyResult(null)
    try {
      const result = await verifyChannel(accessToken, channel.id)
      if (result.ok) {
        const details = [
          result.application_name?.trim() ? result.application_name.trim() : '',
          result.bot_username?.trim() ? `@${result.bot_username.trim()}` : '',
        ].filter(Boolean)
        const detailText = details.join(' · ')
        const applicationID = result.application_id?.trim()
        const message = detailText || ds.connectorVerifyOk
        setVerifyResult({
          ok: true,
          message: applicationID ? `${message} · ${applicationID}` : message,
        })
      } else {
        setVerifyResult({ ok: false, message: result.error ?? ds.connectorVerifyFail })
      }
    } catch (err) {
      const message = err instanceof Error && err.name === 'AbortError'
        ? ds.connectorSaveTimeout
        : isApiError(err) ? err.message : ds.connectorVerifyFail
      setVerifyResult({ ok: false, message })
    } finally {
      setVerifying(false)
    }
  }

  const handleGenerateBindCode = async () => {
    setGeneratingCode(true)
    setError('')
    try {
      const result = await createChannelBindCode(accessToken, 'discord')
      setBindCode(result.token)
    } catch {
      setError(ct.loadFailed)
    } finally {
      setGeneratingCode(false)
    }
  }

  const handleUnbind = async (binding: ChannelBindingResponse) => {
    if (!channel) return
    if (!confirm(ct.unbindConfirm)) return
    try {
      await deleteChannelBinding(accessToken, channel.id, binding.binding_id)
      const nextBindings = await listChannelBindings(accessToken, channel.id)
      setBindings(nextBindings)
    } catch {
      setError(ct.unbindFailed)
    }
  }

  const handleMakeOwner = async (binding: ChannelBindingResponse) => {
    if (!channel) return
    setError('')
    try {
      await updateChannelBinding(accessToken, channel.id, binding.binding_id, { make_owner: true })
      const nextBindings = await listChannelBindings(accessToken, channel.id)
      setBindings(nextBindings)
    } catch {
      setError(ct.saveFailed)
    }
  }

  const handleSaveHeartbeat = async (
    binding: ChannelBindingResponse,
    next: { enabled: boolean; interval: number; model: string },
  ) => {
    if (!channel) return
    setError('')
    try {
      await updateChannelBinding(accessToken, channel.id, binding.binding_id, {
        heartbeat_enabled: next.enabled,
        heartbeat_interval_minutes: next.interval,
        heartbeat_model: next.model || null,
      })
      const nextBindings = await listChannelBindings(accessToken, channel.id)
      setBindings(nextBindings)
    } catch {
      setError(ct.saveFailed)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      {error && (
        <div
          className="rounded-xl px-4 py-3 text-sm"
          style={{
            border: '0.5px solid color-mix(in srgb, var(--c-status-error, #ef4444) 24%, transparent)',
            background: 'var(--c-status-error-bg, rgba(239,68,68,0.08))',
            color: 'var(--c-status-error-text, #ef4444)',
          }}
        >
          {error}
        </div>
      )}

      <div
        className="rounded-2xl p-5"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-[var(--c-bg-deep)] text-[var(--c-text-secondary)]">
                  <MessageCircleMore size={18} />
                </span>
                <div className="min-w-0">
                  <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.discord}</div>
                  <div className="mt-1 flex flex-wrap items-center gap-2">
                    <StatusBadge active={enabled} label={enabled ? ct.active : ct.inactive} />
                    <StatusBadge
                      active={tokenConfigured}
                      label={tokenConfigured ? ds.connectorConfigured : ds.connectorNotConfigured}
                    />
                    {verifyResult && (
                      <span
                        className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium"
                        style={{
                          background: verifyResult.ok
                            ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))'
                            : 'var(--c-status-error-bg, rgba(239,68,68,0.08))',
                          color: verifyResult.ok
                            ? 'var(--c-status-success, #22c55e)'
                            : 'var(--c-status-error, #ef4444)',
                        }}
                      >
                        {verifyResult.message}
                      </span>
                    )}
                  </div>
                </div>
              </div>
            </div>

            <SettingsPillToggle checked={enabled} onChange={(next) => { setEnabled(next); setSaved(false) }} />
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <TokenField
              label={ct.botToken}
              value={tokenDraft}
              placeholder={tokenConfigured && !tokenDraft ? ct.tokenAlreadyConfigured : ct.botTokenPlaceholder}
              onChange={(value) => {
                setTokenDraft(value)
                setSaved(false)
              }}
            />

            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.persona}
              </label>
              <select
                value={personaID}
                onChange={(event) => {
                  setPersonaID(event.target.value)
                  setSaved(false)
                }}
                className="w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)] transition-colors"
              >
                {personas.length === 0 && <option value="">{ct.personaDefault}</option>}
                {personas.map((persona) => (
                  <option key={persona.id} value={persona.id}>
                    {persona.display_name || persona.id}
                  </option>
                ))}
              </select>
            </div>

            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ds.connectorDefaultModel}
              </label>
              <ModelDropdown
                value={defaultModel}
                options={modelOptions}
                placeholder={ds.connectorDefaultModelPlaceholder}
                disabled={saving}
                onChange={(value) => {
                  setDefaultModel(value)
                  setSaved(false)
                }}
              />
            </div>

            <div className="md:col-span-2">
              <div
                className="rounded-xl px-4 py-4"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              >
                <div className="mb-4">
                  <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.accessControl}</div>
                </div>
                <div className="grid gap-4 md:grid-cols-2">
                  <ListField
                    label={ct.allowedServerIds}
                    values={allowedServerIDs}
                    inputValue={allowedServerInput}
                    placeholder={ct.allowedServerIdsPlaceholder}
                    addLabel={t.skills.add}
                    onInputChange={setAllowedServerInput}
                    onAdd={handleAddServerIDs}
                    onRemove={(value) => {
                      setAllowedServerIDs((current) => current.filter((item) => item !== value))
                      setSaved(false)
                    }}
                  />

                  <ListField
                    label={ct.allowedChannelIds}
                    values={allowedChannelIDs}
                    inputValue={allowedChannelInput}
                    placeholder={ct.allowedChannelIdsPlaceholder}
                    addLabel={t.skills.add}
                    onInputChange={setAllowedChannelInput}
                    onAdd={handleAddChannelIDs}
                    onRemove={(value) => {
                      setAllowedChannelIDs((current) => current.filter((item) => item !== value))
                      setSaved(false)
                    }}
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <BindingsCard
        title={ct.bindingsTitle}
        bindings={bindings}
        bindCode={bindCode}
        generating={generatingCode}
        generateLabel={generatingCode ? ct.generating : ct.generateCode}
        regenerateLabel={ds.connectorRegenerateCode}
        emptyLabel={ct.bindingsEmpty}
        ownerLabel={ct.bindingOwner}
        adminLabel={ct.bindingAdmin}
        setOwnerLabel={ct.setOwner}
        unbindLabel={ct.unbind}
        heartbeatEnabledLabel={ct.heartbeatEnabled}
        heartbeatIntervalLabel={ct.heartbeatInterval}
        heartbeatModelLabel={ct.heartbeatModel}
        heartbeatSaveLabel={ct.save}
        heartbeatSavingLabel={ct.saving}
        modelOptions={modelOptions}
        onGenerate={() => void handleGenerateBindCode()}
        onUnbind={(binding) => handleUnbind(binding)}
        onMakeOwner={(binding) => handleMakeOwner(binding)}
        onSaveHeartbeat={(binding, next) => handleSaveHeartbeat(binding, next)}
        onOwnerUnbindAttempt={() => setError(ct.ownerUnbindBlocked)}
      />

      <SaveActions
        saving={saving}
        saved={saved}
        dirty={dirty}
        canSave={canSave}
        canVerify={tokenConfigured}
        verifying={verifying}
        saveLabel={ct.save}
        savingLabel={ct.saving}
        verifyLabel={ds.connectorVerify}
        verifyingLabel={ds.connectorVerifying}
        savedLabel={ds.connectorSaved}
        onSave={() => void handleSave()}
        onVerify={() => void handleVerify()}
      />
    </div>
  )
}
