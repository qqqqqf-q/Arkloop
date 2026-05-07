import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  type ChannelBindingResponse,
  type ChannelResponse,
  type LlmProvider,
  type NapCatStatus,
  type Persona,
  createChannel,
  createChannelBindCode,
  deleteChannelBinding,
  isApiError,
  listChannelBindings,
  updateChannel,
  updateChannelBinding,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  channelRowsCls,
  ChannelDetailRow,
  BindingsCard,
  buildModelOptions,
  inputCls,
  ListField,
  mergeListValues,
  ModelDropdown,
  readStringArrayConfig,
  resolvePersonaID,
  sameItems,
  SaveActions,
  TokenField,
} from './DesktopChannelSettingsShared'
import { QQLoginFlow } from '../QQLoginFlow'
import { SettingsSwitch } from './_SettingsSwitch'

type Props = {
  accessToken: string
  channel: ChannelResponse | null
  personas: Persona[]
  providers: LlmProvider[]
  reload: () => Promise<void>
}

export function DesktopQQSettingsPanel({
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
  const [allowedUserIDs, setAllowedUserIDs] = useState(readStringArrayConfig(channel, 'allowed_user_ids'))
  const [allowedUserInput, setAllowedUserInput] = useState('')
  const [allowedGroupIDs, setAllowedGroupIDs] = useState(readStringArrayConfig(channel, 'allowed_group_ids'))
  const [allowedGroupInput, setAllowedGroupInput] = useState('')
  const [defaultModel, setDefaultModel] = useState((channel?.config_json?.default_model as string | undefined) ?? '')
  const [bindCode, setBindCode] = useState<string | null>(null)
  const [generatingCode, setGeneratingCode] = useState(false)
  const [bindings, setBindings] = useState<ChannelBindingResponse[]>([])
  const [napCatStatus, setNapCatStatus] = useState<NapCatStatus | null>(null)
  const [onebotWSUrl, setOnebotWSUrl] = useState((channel?.config_json?.onebot_ws_url as string | undefined) ?? '')
  const [onebotHTTPUrl, setOnebotHTTPUrl] = useState((channel?.config_json?.onebot_http_url as string | undefined) ?? '')
  const [onebotToken, setOnebotToken] = useState((channel?.config_json?.onebot_token as string | undefined) ?? '')
  const [autoLoginUin, setAutoLoginUin] = useState((channel?.config_json?.auto_login_uin as string | undefined) ?? '')
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
    setAllowedUserIDs(readStringArrayConfig(channel, 'allowed_user_ids'))
    setAllowedUserInput('')
    setAllowedGroupIDs(readStringArrayConfig(channel, 'allowed_group_ids'))
    setAllowedGroupInput('')
    setDefaultModel((channel?.config_json?.default_model as string | undefined) ?? '')
    setOnebotWSUrl((channel?.config_json?.onebot_ws_url as string | undefined) ?? '')
    setOnebotHTTPUrl((channel?.config_json?.onebot_http_url as string | undefined) ?? '')
    setOnebotToken((channel?.config_json?.onebot_token as string | undefined) ?? '')
    setAutoLoginUin((channel?.config_json?.auto_login_uin as string | undefined) ?? '')
  }, [channel, personas])

  useEffect(() => {
    void refreshBindings()
    if (!channel?.id) return
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
  const persistedAllowedUserIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_user_ids'), [channel])
  const persistedAllowedGroupIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_group_ids'), [channel])
  const effectiveAllowedUserIDs = useMemo(
    () => mergeListValues(allowedUserIDs, allowedUserInput),
    [allowedUserIDs, allowedUserInput],
  )
  const effectiveAllowedGroupIDs = useMemo(
    () => mergeListValues(allowedGroupIDs, allowedGroupInput),
    [allowedGroupIDs, allowedGroupInput],
  )
  const effectivePersonaID = useMemo(
    () => resolvePersonaID(personas, channel?.persona_id),
    [personas, channel?.persona_id],
  )
  const persistedDefaultModel = (channel?.config_json?.default_model as string | undefined) ?? ''
  const persistedOnebotWSUrl = (channel?.config_json?.onebot_ws_url as string | undefined) ?? ''
  const persistedOnebotHTTPUrl = (channel?.config_json?.onebot_http_url as string | undefined) ?? ''
  const persistedOnebotToken = (channel?.config_json?.onebot_token as string | undefined) ?? ''
  const persistedAutoLoginUin = (channel?.config_json?.auto_login_uin as string | undefined) ?? ''
  const dirty = useMemo(() => {
    if ((channel?.is_active ?? false) !== enabled) return true
    if (effectivePersonaID !== personaID) return true
    if (!sameItems(persistedAllowedUserIDs, effectiveAllowedUserIDs)) return true
    if (!sameItems(persistedAllowedGroupIDs, effectiveAllowedGroupIDs)) return true
    if (defaultModel !== persistedDefaultModel) return true
    if (onebotWSUrl !== persistedOnebotWSUrl) return true
    if (onebotHTTPUrl !== persistedOnebotHTTPUrl) return true
    if (onebotToken !== persistedOnebotToken) return true
    if (autoLoginUin !== persistedAutoLoginUin) return true
    return false
  }, [
    channel,
    defaultModel,
    effectiveAllowedUserIDs,
    effectiveAllowedGroupIDs,
    effectivePersonaID,
    enabled,
    personaID,
    persistedAllowedUserIDs,
    persistedAllowedGroupIDs,
    persistedDefaultModel,
    onebotWSUrl,
    onebotHTTPUrl,
    onebotToken,
    persistedOnebotWSUrl,
    persistedOnebotHTTPUrl,
    persistedOnebotToken,
    autoLoginUin,
    persistedAutoLoginUin,
  ])
  const canSave = dirty || channel === null

  const handleAddAllowedUsers = () => {
    const nextIDs = mergeListValues(allowedUserIDs, allowedUserInput)
    if (nextIDs.length === allowedUserIDs.length) return
    setAllowedUserIDs(nextIDs)
    setAllowedUserInput('')
    setSaved(false)
  }

  const handleAddAllowedGroups = () => {
    const nextIDs = mergeListValues(allowedGroupIDs, allowedGroupInput)
    if (nextIDs.length === allowedGroupIDs.length) return
    setAllowedGroupIDs(nextIDs)
    setAllowedGroupInput('')
    setSaved(false)
  }

  const handleSave = async () => {
    const nextAllowedUserIDs = mergeListValues(allowedUserIDs, allowedUserInput)
    const nextAllowedGroupIDs = mergeListValues(allowedGroupIDs, allowedGroupInput)

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
        allowed_user_ids: nextAllowedUserIDs,
        allowed_group_ids: nextAllowedGroupIDs,
      }
      if (defaultModel.trim()) configJSON.default_model = defaultModel.trim()
      else delete configJSON.default_model
      if (onebotWSUrl.trim()) configJSON.onebot_ws_url = onebotWSUrl.trim()
      else delete configJSON.onebot_ws_url
      if (onebotHTTPUrl.trim()) configJSON.onebot_http_url = onebotHTTPUrl.trim()
      else delete configJSON.onebot_http_url
      if (onebotToken.trim()) configJSON.onebot_token = onebotToken.trim()
      else delete configJSON.onebot_token
      if (autoLoginUin.trim()) configJSON.auto_login_uin = autoLoginUin.trim()
      else delete configJSON.auto_login_uin

      if (channel == null) {
        const created = await createChannel(accessToken, {
          channel_type: 'qq',
          bot_token: '',
          persona_id: personaID || undefined,
          config_json: configJSON,
        })
        if (enabled) {
          await updateChannel(accessToken, created.id, { is_active: true })
        }
      } else {
        await updateChannel(accessToken, channel.id, {
          persona_id: personaID || null,
          is_active: enabled,
          config_json: configJSON,
        })
      }

      setAllowedUserIDs(nextAllowedUserIDs)
      setAllowedUserInput('')
      setAllowedGroupIDs(nextAllowedGroupIDs)
      setAllowedGroupInput('')
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

  const handleGenerateBindCode = async () => {
    setGeneratingCode(true)
    setError('')
    try {
      const result = await createChannelBindCode(accessToken, 'qq')
      setBindCode(result.token)
    } catch {
      setError(ct.loadFailed)
    } finally {
      setGeneratingCode(false)
    }
  }

  const handleUnbind = async (binding: ChannelBindingResponse) => {
    if (!channel) return
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

  const handleNapCatStatus = useCallback((status: NapCatStatus | null) => {
    setNapCatStatus(status)
    if (status?.logged_in && status.onebot_ws_url && !onebotWSUrl) {
      setOnebotWSUrl(status.onebot_ws_url)
    }
    if (status?.logged_in && status.onebot_http_url && !onebotHTTPUrl) {
      setOnebotHTTPUrl(status.onebot_http_url)
    }
  }, [onebotWSUrl, onebotHTTPUrl])

  const autoLoginOptions = useMemo(() => {
    const opts: { value: string; label: string }[] = []
    const seen = new Set<string>()
    if (napCatStatus?.logged_in && napCatStatus.qq) {
      seen.add(napCatStatus.qq)
      opts.push({ value: napCatStatus.qq, label: napCatStatus.nickname ? `${napCatStatus.nickname} (${napCatStatus.qq})` : napCatStatus.qq })
    }
    for (const a of napCatStatus?.quick_login_list ?? []) {
      if (a.uin && !seen.has(a.uin)) {
        seen.add(a.uin)
        opts.push({ value: a.uin, label: a.nickname ? `${a.nickname} (${a.uin})` : a.uin })
      }
    }
    if (autoLoginUin && !seen.has(autoLoginUin)) {
      opts.push({ value: autoLoginUin, label: autoLoginUin })
    }
    return opts
  }, [napCatStatus, autoLoginUin])

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

  const isWindows = napCatStatus?.platform === 'windows'

  return (
    <div className="flex flex-col gap-6">
      {error && (
        <div
          className="rounded-xl px-5 py-3 text-sm"
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
        className="overflow-hidden rounded-xl"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <div className="flex flex-col">
          <div className={channelRowsCls}>
            <ChannelDetailRow label={t.agentSettings.reasoningModes.enabled}>
              <div className="flex justify-end">
                <SettingsSwitch checked={enabled} onChange={(next) => { setEnabled(next); setSaved(false) }} />
              </div>
            </ChannelDetailRow>
            <ChannelDetailRow label={ct.qqSetup}>
              <QQLoginFlow accessToken={accessToken} channelId={channel?.id ?? ''} onStatusChange={handleNapCatStatus} />
            </ChannelDetailRow>
            {isWindows && (
              <ChannelDetailRow label={ct.qqAutoLogin}>
                <div>
                  <ModelDropdown
                    value={autoLoginUin}
                    options={autoLoginOptions}
                    placeholder={ct.qqAutoLoginNone}
                    disabled={saving}
                    onChange={(v) => { setAutoLoginUin(v); setSaved(false) }}
                  />
                  <p className="mt-1.5 text-[11px] leading-relaxed text-[var(--c-text-muted)]">{ct.qqAutoLoginDesc}</p>
                </div>
              </ChannelDetailRow>
            )}
            <ChannelDetailRow label={ct.qqOneBotWSUrl}>
              <input
                type="text"
                value={onebotWSUrl}
                onChange={(e) => { setOnebotWSUrl(e.target.value); setSaved(false) }}
                placeholder={ct.qqOneBotWSUrlPlaceholder}
                disabled={saving}
                className={inputCls}
              />
            </ChannelDetailRow>
            <ChannelDetailRow label={ct.qqOneBotHTTPUrl}>
              <input
                type="text"
                value={onebotHTTPUrl}
                onChange={(e) => { setOnebotHTTPUrl(e.target.value); setSaved(false) }}
                placeholder={ct.qqOneBotHTTPUrlPlaceholder}
                disabled={saving}
                className={inputCls}
              />
            </ChannelDetailRow>
            <ChannelDetailRow label={ct.qqOneBotToken}>
              <TokenField
                value={onebotToken}
                placeholder={ct.qqOneBotTokenPlaceholder}
                onChange={(v) => { setOnebotToken(v); setSaved(false) }}
              />
            </ChannelDetailRow>
            {/* access control card */}
            <div className="md:col-span-2">
              <div
                className="relative px-5 py-4"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              >
                <div className="mb-4">
                  <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.accessControl}</div>
                </div>

                <div className="mb-4">
                  <ListField
                    label={ct.qqAllowedUsers}
                    values={allowedUserIDs}
                    inputValue={allowedUserInput}
                    placeholder={ct.qqAllowedUsersPlaceholder}
                    addLabel={t.skills.add}
                    onInputChange={setAllowedUserInput}
                    onAdd={handleAddAllowedUsers}
                    onRemove={(value) => {
                      setAllowedUserIDs((current) => current.filter((item) => item !== value))
                      setSaved(false)
                    }}
                  />
                </div>

                <ListField
                  label={ct.qqAllowedGroups}
                  values={allowedGroupIDs}
                  inputValue={allowedGroupInput}
                  placeholder={ct.qqAllowedGroupsPlaceholder}
                  addLabel={t.skills.add}
                  onInputChange={setAllowedGroupInput}
                  onAdd={handleAddAllowedGroups}
                  onRemove={(value) => {
                    setAllowedGroupIDs((current) => current.filter((item) => item !== value))
                    setSaved(false)
                  }}
                />
              </div>
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
        className="rounded-xl px-5 py-4"
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
        canVerify={false}
        verifying={false}
        saveLabel={ct.save}
        savingLabel={ct.saving}
        verifyLabel=""
        verifyingLabel=""
        savedLabel={ds.connectorSaved}
        onSave={() => void handleSave()}
        onVerify={() => {}}
      />
    </div>
  )
}
