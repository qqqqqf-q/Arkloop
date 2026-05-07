import { useCallback, useEffect, useMemo, useRef, useState, useLayoutEffect } from 'react'
import {
  type ChannelBindingResponse,
  type ChannelResponse,
  type LlmProvider,
  type Persona,
  createChannel,
  createChannelBindCode,
  deleteChannelBinding,
  getWeixinQRCode,
  getWeixinQRCodeStatus,
  isApiError,
  listChannelBindings,
  updateChannel,
  updateChannelBinding,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { secondaryButtonSmCls, secondaryButtonBorderStyle } from '../buttonStyles'
import {
  channelRowsCls,
  ChannelDetailRow,
  BindingsCard,
  buildModelOptions,
  ListField,
  mergeListValues,
  ModelDropdown,
  readStringArrayConfig,
  resolvePersonaID,
  sameItems,
  SaveActions,
} from './DesktopChannelSettingsShared'
import { Loader2, RefreshCw } from 'lucide-react'
import QRCode from 'qrcode'
import { SettingsSwitch } from './_SettingsSwitch'

type Props = {
  accessToken: string
  channel: ChannelResponse | null
  personas: Persona[]
  providers: LlmProvider[]
  reload: () => Promise<void>
}

export function DesktopWeixinSettingsPanel({
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

  const [defaultModel, setDefaultModel] = useState((channel?.config_json?.default_model as string | undefined) ?? '')
  const [bindCode, setBindCode] = useState<string | null>(null)
  const [generatingCode, setGeneratingCode] = useState(false)
  const [bindings, setBindings] = useState<ChannelBindingResponse[]>([])

  // QR code login state
  const [qrCodeImg, setQrCodeImg] = useState<string | null>(null)
  const [qrCodeDataURL, setQrCodeDataURL] = useState<string | null>(null)
  const [qrStatus, setQrStatus] = useState<'idle' | 'loading' | 'scanned' | 'confirmed' | 'expired'>('idle')
  const [botToken, setBotToken] = useState('')
  const [baseURL, setBaseURL] = useState((channel?.config_json?.base_url as string | undefined) ?? '')
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)

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

    const nextPrivate = (() => {
      const scoped = readStringArrayConfig(channel, 'private_allowed_user_ids')
      if (scoped.length > 0) return scoped
      return readStringArrayConfig(channel, 'allowed_user_ids')
    })()
    setPrivateRestrict(nextPrivate.length > 0)
    setPrivateIDs(nextPrivate)
    setPrivateInput('')

    const nextGroup = readStringArrayConfig(channel, 'allowed_group_ids')
    setGroupRestrict(nextGroup.length > 0)
    setGroupIDs(nextGroup)
    setGroupInput('')

    setDefaultModel((channel?.config_json?.default_model as string | undefined) ?? '')
    setBaseURL((channel?.config_json?.base_url as string | undefined) ?? '')
    setBotToken('')
    setQrCodeImg(null)
    setQrCodeDataURL(null)
    setQrStatus('idle')
    clearPolling()
  }, [channel, personas])

  useEffect(() => {
    void refreshBindings()
    if (!channel?.id) return
    const timer = window.setInterval(() => {
      void refreshBindings()
    }, 5000)
    return () => window.clearInterval(timer)
  }, [channel?.id, refreshBindings])

  useEffect(() => {
    return () => clearPolling()
  }, [])

  // 把 LiteApp URL / base64 转成 QR 码 data URL
  useLayoutEffect(() => {
    let cancelled = false
    if (!qrCodeImg) {
      setQrCodeDataURL(null)
      return
    }
    // 如果已经是 base64 图片，直接使用
    if (!qrCodeImg.startsWith('http')) {
      setQrCodeDataURL(`data:image/png;base64,${qrCodeImg}`)
      return
    }
    QRCode.toDataURL(qrCodeImg, { width: 200, margin: 2 })
      .then((url) => { if (!cancelled) setQrCodeDataURL(url) })
      .catch(() => { if (!cancelled) setQrCodeDataURL(null) })
    return () => { cancelled = true }
  }, [qrCodeImg])

  function clearPolling() {
    if (pollingRef.current) {
      clearInterval(pollingRef.current)
      pollingRef.current = null
    }
  }

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
  const persistedBaseURL = (channel?.config_json?.base_url as string | undefined) ?? ''
  const dirty = useMemo(() => {
    if ((channel?.is_active ?? false) !== enabled) return true
    if (effectivePersonaID !== personaID) return true
    if (!sameItems(persistedPrivateIDs, privateRestrict ? effectivePrivateIDs : [])) return true
    if (!sameItems(persistedGroupIDs, groupRestrict ? effectiveGroupIDs : [])) return true
    if (defaultModel !== persistedDefaultModel) return true
    if (baseURL !== persistedBaseURL) return true
    return botToken.trim().length > 0
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
    persistedBaseURL,
    privateRestrict,
    botToken,
    baseURL,
  ])
  const canSave = dirty && (channel !== null || botToken.trim().length > 0)

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
    setGroupIDs(next)
    setGroupInput('')
    setSaved(false)
  }

  const handleStartQRLogin = async () => {
    setError('')
    setQrStatus('loading')
    clearPolling()
    try {
      const qr = await getWeixinQRCode(accessToken)
      setQrCodeImg(qr.qrcode_img_content)
      setQrStatus('scanned')

      // Start polling every 2 seconds
      pollingRef.current = setInterval(async () => {
        try {
          const status = await getWeixinQRCodeStatus(accessToken, qr.qrcode)
          if (status.status === 'confirmed' && status.bot_token) {
            clearPolling()
            setBotToken(status.bot_token)
            if (status.baseurl) setBaseURL(status.baseurl)
            setQrStatus('confirmed')
            setQrCodeImg(null)
            setQrCodeDataURL(null)
            setSaved(false)
          } else if (status.status === 'expired') {
            clearPolling()
            setQrStatus('expired')
          }
        } catch (err) {
          clearPolling()
          setQrStatus('idle')
          setQrCodeImg(null)
          setQrCodeDataURL(null)
          setError(isApiError(err) ? err.message : ct.loadFailed)
        }
      }, 2000)
    } catch (err) {
      setQrStatus('idle')
      setError(isApiError(err) ? err.message : ct.loadFailed)
    }
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

      if (baseURL.trim()) configJSON.base_url = baseURL.trim()
      else delete configJSON.base_url

      if (defaultModel.trim()) configJSON.default_model = defaultModel.trim()
      else delete configJSON.default_model

      if (channel == null) {
        const created = await createChannel(accessToken, {
          channel_type: 'weixin',
          bot_token: botToken.trim(),
          persona_id: personaID || undefined,
          config_json: configJSON,
        })
        if (enabled) {
          await updateChannel(accessToken, created.id, { is_active: true })
        }
      } else {
        await updateChannel(accessToken, channel.id, {
          bot_token: botToken.trim() || undefined,
          persona_id: personaID || null,
          is_active: enabled,
          config_json: configJSON,
        })
      }

      setPrivateIDs(nextPrivateIDs)
      setPrivateInput('')
      setGroupIDs(nextGroupIDs)
      setGroupInput('')
      setBotToken('')
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
      const result = await createChannelBindCode(accessToken, 'weixin')
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

  const channelHasToken = channel?.has_credentials === true

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
            {/* QR Code Login */}
            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.botToken}
              </label>

              {channelHasToken && !botToken ? (
                <div className="flex items-center gap-3">
                  <span
                    className="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium"
                    style={{
                      background: 'var(--c-status-success-bg, rgba(34,197,94,0.1))',
                      color: 'var(--c-status-success, #22c55e)',
                    }}
                  >
                    {ct.weixinLoggedIn}
                  </span>
                  <button
                    type="button"
                    onClick={handleStartQRLogin}
                    disabled={qrStatus === 'loading'}
                    className={`${secondaryButtonSmCls} shrink-0`}
                    style={secondaryButtonBorderStyle}
                  >
                    {qrStatus === 'loading' ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                    {ct.weixinRelogin}
                  </button>
                </div>
              ) : qrCodeImg ? (
                <div className="flex flex-col items-start gap-3">
                  <div
                    className="rounded-xl p-3"
                    style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                  >
                    <img
                      src={qrCodeDataURL ?? ''}
                      alt="WeChat QR Code"
                      className="h-[200px] w-[200px]"
                    />
                  </div>
                  <div className="flex items-center gap-3">
                    {qrStatus === 'scanned' && (
                      <span className="text-xs text-[var(--c-text-secondary)]">{ct.weixinScanning}</span>
                    )}
                    {qrStatus === 'expired' && (
                      <span className="text-xs" style={{ color: 'var(--c-status-error-text, #ef4444)' }}>
                        {ct.weixinQRExpired}
                      </span>
                    )}
                    {(qrStatus === 'expired' || qrStatus === 'scanned') && (
                      <button
                        type="button"
                        onClick={handleStartQRLogin}
                        className={`${secondaryButtonSmCls} shrink-0`}
                        style={secondaryButtonBorderStyle}
                      >
                        <RefreshCw size={14} />
                        {ct.weixinRelogin}
                      </button>
                    )}
                  </div>
                </div>
              ) : qrStatus === 'confirmed' ? (
                <div className="flex items-center gap-3">
                  <span
                    className="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium"
                    style={{
                      background: 'var(--c-status-success-bg, rgba(34,197,94,0.1))',
                      color: 'var(--c-status-success, #22c55e)',
                    }}
                  >
                    {ct.weixinLoggedIn}
                  </span>
                  <button
                    type="button"
                    onClick={handleStartQRLogin}
                    className={`${secondaryButtonSmCls} shrink-0`}
                    style={secondaryButtonBorderStyle}
                  >
                    <RefreshCw size={14} />
                    {ct.weixinRelogin}
                  </button>
                </div>
              ) : (
                <div>
                  <button
                    type="button"
                    onClick={handleStartQRLogin}
                    disabled={qrStatus === 'loading'}
                    className={`${secondaryButtonSmCls}`}
                    style={secondaryButtonBorderStyle}
                  >
                    {qrStatus === 'loading' ? <Loader2 size={14} className="animate-spin" /> : null}
                    {ct.weixinScanQR}
                  </button>
                  <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">{ct.weixinBotTokenHint}</p>
                </div>
              )}
            </div>

            {/* Private chat access */}
            <div className="md:col-span-2">
              <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.telegramPrivateChatAccess}</div>
              <div className="mt-3">
                <ModelDropdown
                  value={privateRestrict ? 'restrict' : 'all'}
                  options={[
                    { value: 'all', label: ct.telegramAllowEveryone },
                    { value: 'restrict', label: ct.telegramSpecificUsersOnly },
                  ]}
                  showEmpty={false}
                  onChange={(value) => {
                    setPrivateRestrict(value === 'restrict')
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
                  <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">{ct.weixinAllowedUsersHint}</p>
                </div>
              )}
            </div>

            {/* Group chat access */}
            <div className="md:col-span-2">
              <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.telegramGroupChatAccess}</div>
              <div className="mt-3">
                <ModelDropdown
                  value={groupRestrict ? 'restrict' : 'all'}
                  options={[
                    { value: 'all', label: ct.telegramAllowAllGroups },
                    { value: 'restrict', label: ct.telegramSpecificGroupsOnly },
                  ]}
                  showEmpty={false}
                  onChange={(value) => {
                    setGroupRestrict(value === 'restrict')
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
                  <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">{ct.weixinAllowedGroupsHint}</p>
                </div>
              )}
            </div>

            {/* Persona */}
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

            {/* Default model */}
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
        verifyLabel={ds.connectorVerify}
        verifyingLabel={ds.connectorVerifying}
        savedLabel={ds.connectorSaved}
        onSave={() => void handleSave()}
        onVerify={() => {}}
      />
    </div>
  )
}
