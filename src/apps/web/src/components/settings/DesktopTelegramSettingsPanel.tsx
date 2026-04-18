import { useCallback, useEffect, useMemo, useState } from 'react'
import { Radio } from 'lucide-react'
import { openExternal } from '../../openExternal'
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
import { PillToggle } from '@arkloop/shared'
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

function RadioOption({
  checked,
  label,
  onChange,
}: {
  checked: boolean
  label: string
  onChange: () => void
}) {
  return (
    <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--c-text-secondary)]">
      <span
        className="flex h-4 w-4 items-center justify-center rounded-full border-[1.5px] border-[var(--c-border-mid)]"
      >
        {checked && (
          <span className="h-2 w-2 rounded-full bg-[var(--c-accent)]" />
        )}
      </span>
      <input type="radio" className="sr-only" checked={checked} onChange={onChange} />
      {label}
    </label>
  )
}

export function DesktopTelegramSettingsPanel({
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

  // Private chat access
  const persistedPrivateIDs = useMemo(() => {
    const next = readStringArrayConfig(channel, 'private_allowed_user_ids')
    if (next.length > 0) return next
    return readStringArrayConfig(channel, 'allowed_user_ids')
  }, [channel])
  const [privateRestrict, setPrivateRestrict] = useState(persistedPrivateIDs.length > 0)
  const [privateIDs, setPrivateIDs] = useState(persistedPrivateIDs)
  const [privateInput, setPrivateInput] = useState('')

  // Group chat access
  const persistedGroupIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_group_ids'), [channel])
  const [groupRestrict, setGroupRestrict] = useState(persistedGroupIDs.length > 0)
  const [groupIDs, setGroupIDs] = useState(persistedGroupIDs)
  const [groupInput, setGroupInput] = useState('')

  const [tokenDraft, setTokenDraft] = useState('')
  const [defaultModel, setDefaultModel] = useState((channel?.config_json?.default_model as string | undefined) ?? '')
  const [verifying, setVerifying] = useState(false)
  const [verifyResult, setVerifyResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [bindCode, setBindCode] = useState<string | null>(null)
  const [generatingCode, setGeneratingCode] = useState(false)
  const [bindings, setBindings] = useState<ChannelBindingResponse[]>([])

  const refreshBindings = useCallback(async () => {
    if (!channel?.id) {
      setBindings([])
      return
    }
    try {
      setBindings(await listChannelBindings(accessToken, channel.id))
    } catch {
      setBindings([])
    }
  }, [accessToken, channel?.id])

  useEffect(() => {
    setEnabled(channel?.is_active ?? false)
    setPersonaID(resolvePersonaID(personas, channel?.persona_id))

    const nextPrivate = readStringArrayConfig(channel, 'private_allowed_user_ids')
    setPrivateRestrict(nextPrivate.length > 0)
    setPrivateIDs(nextPrivate)
    setPrivateInput('')

    const nextGroup = readStringArrayConfig(channel, 'allowed_group_ids')
    setGroupRestrict(nextGroup.length > 0)
    setGroupIDs(nextGroup)
    setGroupInput('')

    setTokenDraft('')
    setDefaultModel((channel?.config_json?.default_model as string | undefined) ?? '')
    setVerifyResult(null)
  }, [channel, personas])

  useEffect(() => {
    void refreshBindings()
    if (!channel?.id) {
      return
    }
    const timer = window.setInterval(() => {
      void refreshBindings()
    }, 5000)
    return () => window.clearInterval(timer)
  }, [channel?.id, refreshBindings])

  const modelOptions = useMemo(() => buildModelOptions(providers), [providers])
  const personaOptions = useMemo(
    () => personas.map((p) => ({ value: p.id, label: p.display_name || p.id })),
    [personas],
  )

  const effectivePrivateIDs = useMemo(() => mergeListValues(privateIDs, privateInput), [privateIDs, privateInput])
  const effectiveGroupIDs = useMemo(() => mergeListValues(groupIDs, groupInput), [groupIDs, groupInput])
  const effectivePersonaID = useMemo(
    () => resolvePersonaID(personas, channel?.persona_id),
    [personas, channel?.persona_id],
  )
  const persistedDefaultModel = (channel?.config_json?.default_model as string | undefined) ?? ''
  const tokenConfigured = channel?.has_credentials === true

  const dirty = useMemo(() => {
    if ((channel?.is_active ?? false) !== enabled) return true
    if (effectivePersonaID !== personaID) return true
    if (!sameItems(persistedPrivateIDs, privateRestrict ? effectivePrivateIDs : [])) return true
    if (!sameItems(persistedGroupIDs, groupRestrict ? effectiveGroupIDs : [])) return true
    if (defaultModel !== persistedDefaultModel) return true
    return tokenDraft.trim().length > 0
  }, [
    channel,
    defaultModel,
    effectiveGroupIDs,
    effectivePersonaID,
    effectivePrivateIDs,
    enabled,
    groupRestrict,
    persistedGroupIDs,
    persistedPrivateIDs,
    personaID,
    persistedDefaultModel,
    privateRestrict,
    tokenDraft,
  ])
  const canSave = dirty && (channel !== null || tokenDraft.trim().length > 0)

  const handleAddPrivate = () => {
    const next = mergeListValues(privateIDs, privateInput)
    if (next.length === privateIDs.length) return
    setPrivateIDs(next)
    setPrivateInput('')
    setSaved(false)
  }

  const handleAddGroup = () => {
    const next = mergeListValues(groupIDs, groupInput)
    if (next.length === groupIDs.length) return
    // 只允许负数 chat_id
    const valid = next.filter((id) => /^-[0-9]+$/.test(id))
    if (valid.length !== next.length) return
    setGroupIDs(next)
    setGroupInput('')
    setSaved(false)
  }

  const handleSave = async () => {
    const nextPrivateIDs = privateRestrict ? mergeListValues(privateIDs, privateInput) : []
    const nextGroupIDs = groupRestrict ? mergeListValues(groupIDs, groupInput) : []

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
        private_allowed_user_ids: nextPrivateIDs,
        allowed_group_ids: nextGroupIDs,
      }
      // Never send the legacy key
      delete configJSON.allowed_user_ids

      if (defaultModel.trim()) configJSON.default_model = defaultModel.trim()
      else delete configJSON.default_model

      if (channel == null) {
        const created = await createChannel(accessToken, {
          channel_type: 'telegram',
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

      setPrivateIDs(nextPrivateIDs)
      setPrivateInput('')
      setGroupIDs(nextGroupIDs)
      setGroupInput('')
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
        setVerifyResult({ ok: true, message: result.bot_username ? `@${result.bot_username}` : ds.connectorVerifyOk })
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
      const result = await createChannelBindCode(accessToken, 'telegram')
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
        heartbeat_model: next.model,
      })
      await refreshBindings()
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
                  <Radio size={18} />
                </span>
                <div className="min-w-0">
                  <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.telegram}</div>
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

            <PillToggle checked={enabled} onChange={(next) => { setEnabled(next); setSaved(false) }} />
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="md:col-span-2">
              <TokenField
                label={ct.botToken}
                value={tokenDraft}
                placeholder={tokenConfigured && !tokenDraft ? ct.tokenAlreadyConfigured : ct.botTokenPlaceholder}
                onChange={(value) => {
                  setTokenDraft(value)
                  setSaved(false)
                }}
              />
              <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">
                {ct.botTokenHint}{' '}
                <button
                  type="button"
                  onClick={() => openExternal('https://t.me/BotFather')}
                  className="text-[var(--c-text-secondary)] underline underline-offset-2 hover:text-[var(--c-text-primary)]"
                >
                  @BotFather
                </button>
              </p>
            </div>

            <div className="md:col-span-2">
              <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.telegramPrivateChatAccess}</div>
              <div className="mt-3 flex flex-col gap-2">
                <RadioOption
                  checked={!privateRestrict}
                  label={ct.telegramAllowEveryone}
                  onChange={() => {
                    setPrivateRestrict(false)
                    setSaved(false)
                  }}
                />
                <RadioOption
                  checked={privateRestrict}
                  label={ct.telegramSpecificUsersOnly}
                  onChange={() => {
                    setPrivateRestrict(true)
                    setSaved(false)
                  }}
                />
              </div>
              {privateRestrict && (
                <div className="mt-3">
                  <ListField
                    label={ct.allowedUsers}
                    values={privateIDs}
                    inputValue={privateInput}
                    placeholder={ct.allowedUsersPlaceholder}
                    addLabel={t.skills.add}
                    onInputChange={setPrivateInput}
                    onAdd={handleAddPrivate}
                    onRemove={(value) => {
                      setPrivateIDs((current) => current.filter((item) => item !== value))
                      setSaved(false)
                    }}
                  />
                  <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">
                    {ct.allowedUsersHint}{' '}
                    <button
                      type="button"
                      onClick={() => openExternal('https://t.me/userinfobot')}
                      className="text-[var(--c-text-secondary)] underline underline-offset-2 hover:text-[var(--c-text-primary)]"
                    >
                      @userinfobot
                    </button>
                  </p>
                </div>
              )}
            </div>

            <div className="md:col-span-2">
              <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.telegramGroupChatAccess}</div>
              <div className="mt-3 flex flex-col gap-2">
                <RadioOption
                  checked={!groupRestrict}
                  label={ct.telegramAllowAllGroups}
                  onChange={() => {
                    setGroupRestrict(false)
                    setSaved(false)
                  }}
                />
                <RadioOption
                  checked={groupRestrict}
                  label={ct.telegramSpecificGroupsOnly}
                  onChange={() => {
                    setGroupRestrict(true)
                    setSaved(false)
                  }}
                />
              </div>
              {groupRestrict && (
                <div className="mt-3">
                  <ListField
                    label={ct.telegramAllowedGroupsLabel}
                    values={groupIDs}
                    inputValue={groupInput}
                    placeholder={ct.telegramAllowedGroupsPlaceholder}
                    addLabel={t.skills.add}
                    onInputChange={setGroupInput}
                    onAdd={handleAddGroup}
                    onRemove={(value) => {
                      setGroupIDs((current) => current.filter((item) => item !== value))
                      setSaved(false)
                    }}
                  />
                  <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">
                    {ct.telegramAllowedGroupsHint}{' '}
                    <button
                      type="button"
                      onClick={() => openExternal('https://t.me/getidsbot')}
                      className="text-[var(--c-text-secondary)] underline underline-offset-2 hover:text-[var(--c-text-primary)]"
                    >
                      @getidsbot
                    </button>
                  </p>
                </div>
              )}
            </div>

            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.persona}
              </label>
              <ModelDropdown
                value={personaID}
                options={personaOptions}
                placeholder={ct.personaDefault}
                disabled={saving}
                onChange={(value) => {
                  setPersonaID(value)
                  setSaved(false)
                }}
              />
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

      <div
        className="rounded-2xl px-5 py-4"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.heartbeatCardTitle}</div>
        <p className="mt-1.5 text-xs leading-relaxed text-[var(--c-text-muted)]">{ct.heartbeatCardDesc}</p>
        <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">{ct.heartbeatCardHint}</p>
      </div>

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
