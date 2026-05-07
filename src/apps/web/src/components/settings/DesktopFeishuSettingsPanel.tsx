import { useEffect, useMemo, useRef, useState } from 'react'
import {
  type ChannelResponse,
  type LlmProvider,
  type Persona,
  createChannel,
  isApiError,
  updateChannel,
  verifyChannel,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  channelRowsCls,
  ChannelDetailRow,
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
import { SettingsSwitch } from './_SettingsSwitch'
import { SettingsSelect } from './_SettingsSelect'

type Props = {
  accessToken: string
  channel: ChannelResponse | null
  personas: Persona[]
  providers: LlmProvider[]
  reload: () => Promise<void>
}

function readStringConfig(channel: ChannelResponse | null, key: string): string {
  const raw = channel?.config_json?.[key]
  return typeof raw === 'string' ? raw : ''
}

export function DesktopFeishuSettingsPanel({
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
  const [appID, setAppID] = useState(readStringConfig(channel, 'app_id'))
  const [domain, setDomain] = useState(readStringConfig(channel, 'domain') || 'feishu')
  const [appSecretDraft, setAppSecretDraft] = useState('')
  const [verificationTokenDraft, setVerificationTokenDraft] = useState('')
  const [encryptKeyDraft, setEncryptKeyDraft] = useState('')
  const [defaultModel, setDefaultModel] = useState(readStringConfig(channel, 'default_model'))
  const [allowedUserIDs, setAllowedUserIDs] = useState(readStringArrayConfig(channel, 'allowed_user_ids'))
  const [allowedUserInput, setAllowedUserInput] = useState('')
  const [allowedChatIDs, setAllowedChatIDs] = useState(readStringArrayConfig(channel, 'allowed_chat_ids'))
  const [allowedChatInput, setAllowedChatInput] = useState('')
  const [triggerKeywords, setTriggerKeywords] = useState(readStringArrayConfig(channel, 'trigger_keywords'))
  const [triggerKeywordInput, setTriggerKeywordInput] = useState('')
  const [verifying, setVerifying] = useState(false)
  const [verifyResult, setVerifyResult] = useState<{ ok: boolean; message: string } | null>(null)
  const previousChannelIDRef = useRef(channel?.id ?? null)

  useEffect(() => {
    const nextChannelID = channel?.id ?? null
    const channelChanged = previousChannelIDRef.current !== nextChannelID
    previousChannelIDRef.current = nextChannelID
    setEnabled(channel?.is_active ?? false)
    setPersonaID(resolvePersonaID(personas, channel?.persona_id))
    setAppID(readStringConfig(channel, 'app_id'))
    setDomain(readStringConfig(channel, 'domain') || 'feishu')
    setAppSecretDraft('')
    setVerificationTokenDraft('')
    setEncryptKeyDraft('')
    setDefaultModel(readStringConfig(channel, 'default_model'))
    setAllowedUserIDs(readStringArrayConfig(channel, 'allowed_user_ids'))
    setAllowedUserInput('')
    setAllowedChatIDs(readStringArrayConfig(channel, 'allowed_chat_ids'))
    setAllowedChatInput('')
    setTriggerKeywords(readStringArrayConfig(channel, 'trigger_keywords'))
    setTriggerKeywordInput('')
    if (channelChanged) setVerifyResult(null)
  }, [channel, personas])

  const modelOptions = useMemo(() => buildModelOptions(providers), [providers])
  const personaOptions = useMemo(
    () => personas.map((p) => ({ value: p.id, label: p.display_name || p.id })),
    [personas],
  )
  const persistedAppID = readStringConfig(channel, 'app_id')
  const persistedDomain = readStringConfig(channel, 'domain') || 'feishu'
  const persistedDefaultModel = readStringConfig(channel, 'default_model')
  const persistedAllowedUserIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_user_ids'), [channel])
  const persistedAllowedChatIDs = useMemo(() => readStringArrayConfig(channel, 'allowed_chat_ids'), [channel])
  const persistedTriggerKeywords = useMemo(() => readStringArrayConfig(channel, 'trigger_keywords'), [channel])
  const effectiveAllowedUserIDs = useMemo(
    () => mergeListValues(allowedUserIDs, allowedUserInput),
    [allowedUserIDs, allowedUserInput],
  )
  const effectiveAllowedChatIDs = useMemo(
    () => mergeListValues(allowedChatIDs, allowedChatInput),
    [allowedChatIDs, allowedChatInput],
  )
  const effectiveTriggerKeywords = useMemo(
    () => mergeListValues(triggerKeywords, triggerKeywordInput).map((item) => item.toLowerCase()),
    [triggerKeywords, triggerKeywordInput],
  )
  const effectivePersonaID = useMemo(
    () => resolvePersonaID(personas, channel?.persona_id),
    [personas, channel?.persona_id],
  )
  const tokenConfigured = channel?.has_credentials === true

  const dirty = useMemo(() => {
    if ((channel?.is_active ?? false) !== enabled) return true
    if (effectivePersonaID !== personaID) return true
    if (appID.trim() !== persistedAppID) return true
    if (domain !== persistedDomain) return true
    if (defaultModel !== persistedDefaultModel) return true
    if (!sameItems(persistedAllowedUserIDs, effectiveAllowedUserIDs)) return true
    if (!sameItems(persistedAllowedChatIDs, effectiveAllowedChatIDs)) return true
    if (!sameItems(persistedTriggerKeywords, effectiveTriggerKeywords)) return true
    return appSecretDraft.trim().length > 0 ||
      verificationTokenDraft.trim().length > 0 ||
      encryptKeyDraft.trim().length > 0
  }, [
    appID,
    appSecretDraft,
    channel,
    defaultModel,
    domain,
    effectiveAllowedChatIDs,
    effectiveAllowedUserIDs,
    effectivePersonaID,
    effectiveTriggerKeywords,
    enabled,
    encryptKeyDraft,
    personaID,
    persistedAllowedChatIDs,
    persistedAllowedUserIDs,
    persistedAppID,
    persistedDefaultModel,
    persistedDomain,
    persistedTriggerKeywords,
    verificationTokenDraft,
  ])

  const createReady = channel !== null ||
    (appID.trim() !== '' &&
      appSecretDraft.trim() !== '' &&
      verificationTokenDraft.trim() !== '' &&
      encryptKeyDraft.trim() !== '' &&
      personaID !== '')
  const canSave = dirty && createReady

  const handleSave = async () => {
    if (!personaID) {
      setError(ct.personaRequired)
      return
    }
    if (appID.trim() === '') {
      setError(ct.feishuAppIDRequired)
      return
    }
    if (channel === null && (appSecretDraft.trim() === '' || verificationTokenDraft.trim() === '' || encryptKeyDraft.trim() === '')) {
      setError(ct.feishuCredentialsRequired)
      return
    }

    setSaving(true)
    setError('')
    try {
      const configJSON: Record<string, unknown> = {
        ...(channel?.config_json ?? {}),
        app_id: appID.trim(),
        domain,
        allowed_user_ids: effectiveAllowedUserIDs,
        allowed_chat_ids: effectiveAllowedChatIDs,
        trigger_keywords: effectiveTriggerKeywords,
      }
      if (defaultModel.trim()) configJSON.default_model = defaultModel.trim()
      else delete configJSON.default_model
      if (verificationTokenDraft.trim()) configJSON.verification_token = verificationTokenDraft.trim()
      if (encryptKeyDraft.trim()) configJSON.encrypt_key = encryptKeyDraft.trim()

      if (channel === null) {
        const created = await createChannel(accessToken, {
          channel_type: 'feishu',
          bot_token: appSecretDraft.trim(),
          persona_id: personaID,
          config_json: configJSON,
        })
        if (enabled) {
          await updateChannel(accessToken, created.id, { is_active: true })
        }
      } else {
        await updateChannel(accessToken, channel.id, {
          bot_token: appSecretDraft.trim() || undefined,
          persona_id: personaID || null,
          is_active: enabled,
          config_json: configJSON,
        })
      }

      setAppSecretDraft('')
      setVerificationTokenDraft('')
      setEncryptKeyDraft('')
      setAllowedUserIDs(effectiveAllowedUserIDs)
      setAllowedUserInput('')
      setAllowedChatIDs(effectiveAllowedChatIDs)
      setAllowedChatInput('')
      setTriggerKeywords(effectiveTriggerKeywords)
      setTriggerKeywordInput('')
      setSaved(true)
      window.setTimeout(() => setSaved(false), 2500)
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
        const parts = [
          result.application_name?.trim() || '',
          result.bot_user_id?.trim() || '',
        ].filter(Boolean)
        await reload()
        setVerifyResult({ ok: true, message: parts.join(' · ') || ds.connectorVerifyOk })
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
            <div>
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.feishuAppID}
              </label>
              <input
                value={appID}
                onChange={(event) => {
                  setAppID(event.target.value)
                  setSaved(false)
                }}
                placeholder={ct.feishuAppIDPlaceholder}
                className={inputCls}
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.feishuDomain}
              </label>
              <SettingsSelect
                value={domain}
                onChange={(value) => {
                  setDomain(value)
                  setSaved(false)
                }}
                options={[
                  { value: 'feishu', label: ct.feishuDomainFeishu },
                  { value: 'lark', label: ct.feishuDomainLark },
                ]}
                triggerClassName="h-9"
              />
            </div>

            <TokenField
              label={ct.feishuAppSecret}
              value={appSecretDraft}
              placeholder={tokenConfigured && !appSecretDraft ? ct.tokenAlreadyConfigured : ct.feishuAppSecretPlaceholder}
              onChange={(value) => {
                setAppSecretDraft(value)
                setSaved(false)
              }}
            />

            <TokenField
              label={ct.feishuVerificationToken}
              value={verificationTokenDraft}
              placeholder={tokenConfigured && !verificationTokenDraft ? ct.tokenAlreadyConfigured : ct.feishuVerificationTokenPlaceholder}
              onChange={(value) => {
                setVerificationTokenDraft(value)
                setSaved(false)
              }}
            />

            <TokenField
              label={ct.feishuEncryptKey}
              value={encryptKeyDraft}
              placeholder={tokenConfigured && !encryptKeyDraft ? ct.tokenAlreadyConfigured : ct.feishuEncryptKeyPlaceholder}
              onChange={(value) => {
                setEncryptKeyDraft(value)
                setSaved(false)
              }}
            />

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

            <div
              className="md:col-span-2 relative px-5 py-4"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              <div className="mb-4 text-sm font-medium text-[var(--c-text-heading)]">{ct.accessControl}</div>
              <div className={channelRowsCls}>
                <ListField
                  label={ct.feishuAllowedUsers}
                  values={allowedUserIDs}
                  inputValue={allowedUserInput}
                  placeholder={ct.feishuAllowedUsersPlaceholder}
                  addLabel={t.skills.add}
                  onInputChange={setAllowedUserInput}
                  onAdd={() => {
                    setAllowedUserIDs(mergeListValues(allowedUserIDs, allowedUserInput))
                    setAllowedUserInput('')
                    setSaved(false)
                  }}
                  onRemove={(value) => {
                    setAllowedUserIDs((current) => current.filter((item) => item !== value))
                    setSaved(false)
                  }}
                />

                <ListField
                  label={ct.feishuAllowedChats}
                  values={allowedChatIDs}
                  inputValue={allowedChatInput}
                  placeholder={ct.feishuAllowedChatsPlaceholder}
                  addLabel={t.skills.add}
                  onInputChange={setAllowedChatInput}
                  onAdd={() => {
                    setAllowedChatIDs(mergeListValues(allowedChatIDs, allowedChatInput))
                    setAllowedChatInput('')
                    setSaved(false)
                  }}
                  onRemove={(value) => {
                    setAllowedChatIDs((current) => current.filter((item) => item !== value))
                    setSaved(false)
                  }}
                />

                <ListField
                  label={ct.feishuTriggerKeywords}
                  values={triggerKeywords}
                  inputValue={triggerKeywordInput}
                  placeholder={ct.feishuTriggerKeywordsPlaceholder}
                  addLabel={t.skills.add}
                  onInputChange={setTriggerKeywordInput}
                  onAdd={() => {
                    setTriggerKeywords(mergeListValues(triggerKeywords, triggerKeywordInput).map((item) => item.toLowerCase()))
                    setTriggerKeywordInput('')
                    setSaved(false)
                  }}
                  onRemove={(value) => {
                    setTriggerKeywords((current) => current.filter((item) => item !== value))
                    setSaved(false)
                  }}
                />
              </div>
            </div>
          </div>
        </div>
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
      {verifyResult && (
        <p
          className="px-1 text-xs"
          style={{ color: verifyResult.ok ? 'var(--c-status-success)' : 'var(--c-status-error)' }}
        >
          {verifyResult.message}
        </p>
      )}
    </div>
  )
}
