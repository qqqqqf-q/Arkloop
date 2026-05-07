import { useState, useEffect, useCallback, useMemo } from 'react'
import { ExternalLink, Plus, Trash2, Link2 } from 'lucide-react'
import {
  type ChannelResponse,
  type ChannelIdentityResponse,
  type Persona,
  listChannels,
  listChannelPersonas,
  createChannel,
  updateChannel,
  deleteChannel,
  listMyChannelIdentities,
  createChannelBindCode,
  unbindChannelIdentity,
  verifyChannel,
  isApiError,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { AutoResizeTextarea, ConfirmDialog } from '@arkloop/shared'
import { QQLoginFlow } from './QQLoginFlow'
import { CopyIconButton } from './CopyIconButton'
import { ModelDropdown } from './settings/DesktopChannelSettingsShared'
import { openExternal } from '../openExternal'
import { SettingsSelect } from './settings/_SettingsSelect'

type Props = {
  accessToken: string
}

const CHANNEL_TYPES = ['telegram', 'discord', 'feishu', 'qqbot', 'qq', 'weixin'] as const
type ChannelType = (typeof CHANNEL_TYPES)[number]

function parseAllowedUserIds(input: string): string[] {
  return input
    .split(/[\n,\s]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function readChannelConfigString(channel: ChannelResponse, key: string): string {
  const raw = channel.config_json?.[key]
  return typeof raw === 'string' ? raw : ''
}

export function ChannelsSettingsContent({ accessToken }: Props) {
  const { t } = useLocale()
  const ct = t.channels
  const ds = t.desktopSettings

  const [channels, setChannels] = useState<ChannelResponse[]>([])
  const [identities, setIdentities] = useState<ChannelIdentityResponse[]>([])
  const [personas, setPersonas] = useState<Persona[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [showForm, setShowForm] = useState(false)
  const [formType, setFormType] = useState<ChannelType>('telegram')
  const [formAppID, setFormAppID] = useState('')
  const [formToken, setFormToken] = useState('')
  const [formPersonaId, setFormPersonaId] = useState('')
  const [formAllowedUsers, setFormAllowedUsers] = useState('')
  const [formAllowedGroups, setFormAllowedGroups] = useState('')
  const [formDefaultModel, setFormDefaultModel] = useState('')
  const [formFeishuAppID, setFormFeishuAppID] = useState('')
  const [formFeishuDomain, setFormFeishuDomain] = useState<'feishu' | 'lark'>('feishu')
  const [formFeishuVerificationToken, setFormFeishuVerificationToken] = useState('')
  const [formFeishuEncryptKey, setFormFeishuEncryptKey] = useState('')
  const [formFeishuAllowedUsers, setFormFeishuAllowedUsers] = useState('')
  const [formFeishuAllowedChats, setFormFeishuAllowedChats] = useState('')
  const [formFeishuTriggerKeywords, setFormFeishuTriggerKeywords] = useState('')
  const [saving, setSaving] = useState(false)
  const [tokenDrafts, setTokenDrafts] = useState<Record<string, string>>({})
  const [verifyingChannelId, setVerifyingChannelId] = useState<string | null>(null)
  const [verifyResults, setVerifyResults] = useState<Record<string, { ok: boolean; message: string }>>({})
  const [deleteTarget, setDeleteTarget] = useState<ChannelResponse | null>(null)
  const [unbindTarget, setUnbindTarget] = useState<string | null>(null)

  const personaOptions = useMemo(
    () => personas.map((p) => ({ value: p.id, label: p.display_name || p.id })),
    [personas]
  )

  const [bindCode, setBindCode] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)

  const load = useCallback(async () => {
    try {
      const [ch, ids, p] = await Promise.all([
        listChannels(accessToken),
        listMyChannelIdentities(accessToken).catch(() => [] as ChannelIdentityResponse[]),
        listChannelPersonas(accessToken).catch(() => [] as Persona[]),
      ])
      setChannels(ch)
      setIdentities(ids)
      setPersonas(p)
      setError('')
    } catch {
      setError(ct.loadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, ct.loadFailed])

  useEffect(() => { load() }, [load])

  const resetCreateForm = () => {
    setShowForm(false)
    setFormAppID('')
    setFormToken('')
    setFormPersonaId('')
    setFormAllowedUsers('')
    setFormAllowedGroups('')
    setFormDefaultModel('')
    setFormFeishuAppID('')
    setFormFeishuDomain('feishu')
    setFormFeishuVerificationToken('')
    setFormFeishuEncryptKey('')
    setFormFeishuAllowedUsers('')
    setFormFeishuAllowedChats('')
    setFormFeishuTriggerKeywords('')
  }

  const handleCreate = async () => {
    if (formType === 'qqbot') {
      if (!formAppID.trim()) {
        setError(ct.qqBotAppIDRequired)
        return
      }
      if (!formToken.trim()) {
        setError(ct.qqBotClientSecretRequired)
        return
      }
    }
    if (formType === 'feishu') {
      if (!formPersonaId) {
        setError(ct.personaRequired)
        return
      }
      if (!formFeishuAppID.trim()) {
        setError(ct.feishuAppIDRequired)
        return
      }
      if (!formToken.trim() || !formFeishuVerificationToken.trim() || !formFeishuEncryptKey.trim()) {
        setError(ct.feishuCredentialsRequired)
        return
      }
    }

    setSaving(true)
    setError('')
    try {
      const configJSON = (() => {
        if (formType === 'telegram') {
          return { allowed_user_ids: parseAllowedUserIds(formAllowedUsers) }
        }
        if (formType === 'qqbot') {
          return {
            app_id: formAppID.trim(),
            allowed_user_ids: parseAllowedUserIds(formAllowedUsers),
            allowed_group_ids: parseAllowedUserIds(formAllowedGroups),
            default_model: formDefaultModel.trim(),
          }
        }
        if (formType === 'feishu') {
          return {
            app_id: formFeishuAppID.trim(),
            domain: formFeishuDomain,
            verification_token: formFeishuVerificationToken.trim(),
            encrypt_key: formFeishuEncryptKey.trim(),
            allowed_user_ids: parseAllowedUserIds(formFeishuAllowedUsers),
            allowed_chat_ids: parseAllowedUserIds(formFeishuAllowedChats),
            trigger_keywords: parseAllowedUserIds(formFeishuTriggerKeywords).map((item) => item.toLowerCase()),
          }
        }
        return undefined
      })()
      await createChannel(accessToken, {
        channel_type: formType,
        bot_token: formType === 'qq' ? '' : formToken.trim(),
        persona_id: formPersonaId || undefined,
        config_json: configJSON,
      })
      await load()
    } catch (err) {
      setError(isApiError(err) ? err.message : ct.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const handleVerify = async (ch: ChannelResponse) => {
    setVerifyingChannelId(ch.id)
    setError('')
    try {
      const result = await verifyChannel(accessToken, ch.id)
      const message = result.ok
        ? [result.application_name, result.bot_user_id].map((part) => part?.trim()).filter(Boolean).join(' · ') || ds.connectorVerifyOk
        : result.error || ds.connectorVerifyFail
      setVerifyResults((prev) => ({ ...prev, [ch.id]: { ok: result.ok, message } }))
      if (result.ok) {
        await load()
      }
    } catch (err) {
      setVerifyResults((prev) => ({
        ...prev,
        [ch.id]: { ok: false, message: isApiError(err) ? err.message : ds.connectorVerifyFail },
      }))
    } finally {
      setVerifyingChannelId(null)
    }
  }

  const handleToggle = async (ch: ChannelResponse) => {
    if (!ch.is_active && (ch.channel_type === 'telegram' || ch.channel_type === 'qqbot' || ch.channel_type === 'feishu')) {
      if (!ch.persona_id) {
        setError(ct.personaRequired)
        return
      }
    }
    try {
      await updateChannel(accessToken, ch.id, { is_active: !ch.is_active })
      await load()
    } catch (err) {
      setError(isApiError(err) ? err.message : ct.saveFailed)
    }
  }

  const handleDelete = async (ch: ChannelResponse) => {
    try {
      await deleteChannel(accessToken, ch.id)
      setDeleteTarget(null)
      await load()
    } catch (err) {
      setError(isApiError(err) ? err.message : ct.deleteFailed)
    }
  }

  const handleCopyWebhook = (url: string) => {
    navigator.clipboard.writeText(url)
  }

  const handleGenerateBindCode = async () => {
    setGenerating(true)
    try {
      const res = await createChannelBindCode(accessToken)
      setBindCode(res.token)
    } catch {
      setError(ct.loadFailed)
    } finally {
      setGenerating(false)
    }
  }

  const handleUpdateToken = async (channelId: string, token: string) => {
    try {
      await updateChannel(accessToken, channelId, { bot_token: token })
      setTokenDrafts(prev => ({ ...prev, [channelId]: '' }))
      await load()
    } catch (err) {
      setError(isApiError(err) ? err.message : ct.saveFailed)
    }
  }

  const handleUnbind = async (id: string) => {
    try {
      await unbindChannelIdentity(accessToken, id)
      setUnbindTarget(null)
      await load()
    } catch {
      setError(ct.unbindFailed)
    }
  }

  const channelLabel = (type: string) => {
    const map: Record<string, string> = {
      telegram: ct.telegram,
      discord: ct.discord,
      feishu: ct.feishu,
      qqbot: ct.qq,
      qq: ct.qqOneBot,
      weixin: ct.weixin,
    }
    return map[type] || type
  }

  const usedTypes = new Set(channels.map((c) => c.channel_type))
  const createDisabled =
    saving ||
    (formType === 'qq'
      ? false
      : formType === 'qqbot'
        ? !formAppID.trim() || !formToken.trim()
        : formType === 'feishu'
          ? !formToken.trim() || !formPersonaId || !formFeishuAppID.trim() || !formFeishuVerificationToken.trim() || !formFeishuEncryptKey.trim()
          : !formToken.trim())

  if (loading) return <div className="text-sm text-[var(--c-text-tertiary)]">{t.loading}</div>

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-[var(--c-text-heading)]">{ct.title}</h3>
          <p className="mt-0.5 text-xs text-[var(--c-text-tertiary)]">{ct.subtitle}</p>
        </div>
        {!showForm && (
          <button
            onClick={() => setShowForm(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <Plus size={14} />
            {ct.addChannel}
          </button>
        )}
      </div>

      {error && (
        <div className="rounded-lg px-3 py-2 text-xs" style={{ color: 'var(--c-status-error-text, #ef4444)', background: 'var(--c-status-error-bg, rgba(239,68,68,0.08))' }}>
          {error}
        </div>
      )}

      {/* Create form */}
      {showForm && (
        <div className="flex flex-col gap-3 rounded-lg p-4" style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-sub)' }}>
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.platform}</label>
            <SettingsSelect
              value={formType}
              onChange={(value) => {
                setFormType(value as ChannelType)
                setError('')
              }}
              options={CHANNEL_TYPES
                .filter((ct) => !usedTypes.has(ct))
                .map((item) => ({ value: item, label: channelLabel(item) }))}
              triggerClassName="h-9"
            />
          </div>

          {formType === 'qqbot' && (
            <div
              className="rounded-lg px-3 py-2"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                <div className="text-xs font-medium text-[var(--c-text-heading)]">{ct.qqBotOfficialIntro}</div>
                <button
                  type="button"
                  onClick={() => openExternal('https://q.qq.com/qqbot/')}
                  className="inline-flex shrink-0 items-center gap-1 text-xs text-[var(--c-text-secondary)] underline underline-offset-2 hover:text-[var(--c-text-primary)]"
                >
                  <ExternalLink size={13} />
                  {ct.qqBotOfficialPortal}
                </button>
              </div>
              <ol className="mt-1.5 list-decimal space-y-1 pl-4 text-xs leading-5 text-[var(--c-text-tertiary)]">
                {ct.qqBotSetupSteps.map((step) => (
                  <li key={step}>{step}</li>
                ))}
              </ol>
            </div>
          )}

          {formType === 'qqbot' && (
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.qqBotAppID}</label>
              <input
                type="text"
                value={formAppID}
                onChange={(e) => setFormAppID(e.target.value)}
                placeholder={ct.qqBotAppIDPlaceholder}
                className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              />
            </div>
          )}

          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--c-text-secondary)]">
              {formType === 'feishu' ? ct.feishuAppSecret : formType === 'qqbot' ? ct.qqBotClientSecret : ct.botToken}
            </label>
            {formType === 'qq' ? (
              <p className="text-xs text-[var(--c-text-tertiary)]">{ct.qqChannelCreateHint}</p>
            ) : (
              <input
                type="password"
                value={formToken}
                onChange={(e) => setFormToken(e.target.value)}
                placeholder={
                  formType === 'feishu' ? ct.feishuAppSecretPlaceholder : formType === 'qqbot' ? ct.qqBotClientSecretPlaceholder : ct.botTokenPlaceholder
                }
                className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              />
            )}
          </div>

          {formType === 'qqbot' && (
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ds.connectorDefaultModel}</label>
              <input
                type="text"
                value={formDefaultModel}
                onChange={(e) => setFormDefaultModel(e.target.value)}
                placeholder={ds.connectorDefaultModelPlaceholder}
                className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              />
            </div>
          )}

          {formType === 'telegram' && (
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.allowedUsers}</label>
              <AutoResizeTextarea
                value={formAllowedUsers}
                onChange={(e) => setFormAllowedUsers(e.target.value)}
                placeholder={ct.allowedUsersPlaceholder}
                rows={3}
                minRows={3}
                maxHeight={220}
                className="rounded-lg bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] resize-none"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              />
            </div>
          )}

          {formType === 'qqbot' && (
            <>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.qqBotAllowedUsers}</label>
                <AutoResizeTextarea
                  value={formAllowedUsers}
                  onChange={(e) => setFormAllowedUsers(e.target.value)}
                  placeholder={ct.qqBotAllowedUsersPlaceholder}
                  rows={3}
                  minRows={3}
                  maxHeight={220}
                  className="rounded-lg bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] resize-none"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.qqBotAllowedGroups}</label>
                <AutoResizeTextarea
                  value={formAllowedGroups}
                  onChange={(e) => setFormAllowedGroups(e.target.value)}
                  placeholder={ct.qqBotAllowedGroupsPlaceholder}
                  rows={3}
                  minRows={3}
                  maxHeight={220}
                  className="rounded-lg bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] resize-none"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                />
              </div>
            </>
          )}

          {formType === 'feishu' && (
            <>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuAppID}</label>
                  <input
                    value={formFeishuAppID}
                    onChange={(e) => setFormFeishuAppID(e.target.value)}
                    placeholder={ct.feishuAppIDPlaceholder}
                    className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuDomain}</label>
                  <SettingsSelect
                    value={formFeishuDomain}
                    onChange={(value) => setFormFeishuDomain(value as 'feishu' | 'lark')}
                    options={[
                      { value: 'feishu', label: ct.feishuDomainFeishu },
                      { value: 'lark', label: ct.feishuDomainLark },
                    ]}
                    triggerClassName="h-9"
                  />
                </div>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuVerificationToken}</label>
                  <input
                    type="password"
                    value={formFeishuVerificationToken}
                    onChange={(e) => setFormFeishuVerificationToken(e.target.value)}
                    placeholder={ct.feishuVerificationTokenPlaceholder}
                    className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuEncryptKey}</label>
                  <input
                    type="password"
                    value={formFeishuEncryptKey}
                    onChange={(e) => setFormFeishuEncryptKey(e.target.value)}
                    placeholder={ct.feishuEncryptKeyPlaceholder}
                    className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  />
                </div>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuAllowedUsers}</label>
                  <AutoResizeTextarea
                    value={formFeishuAllowedUsers}
                    onChange={(e) => setFormFeishuAllowedUsers(e.target.value)}
                    placeholder={ct.feishuAllowedUsersPlaceholder}
                    rows={2}
                    minRows={2}
                    maxHeight={160}
                    className="rounded-lg bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] resize-none"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuAllowedChats}</label>
                  <AutoResizeTextarea
                    value={formFeishuAllowedChats}
                    onChange={(e) => setFormFeishuAllowedChats(e.target.value)}
                    placeholder={ct.feishuAllowedChatsPlaceholder}
                    rows={2}
                    minRows={2}
                    maxHeight={160}
                    className="rounded-lg bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] resize-none"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  />
                </div>
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.feishuTriggerKeywords}</label>
                <AutoResizeTextarea
                  value={formFeishuTriggerKeywords}
                  onChange={(e) => setFormFeishuTriggerKeywords(e.target.value)}
                  placeholder={ct.feishuTriggerKeywordsPlaceholder}
                  rows={2}
                  minRows={2}
                  maxHeight={160}
                  className="rounded-lg bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] resize-none"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                />
              </div>
            </>
          )}

          {personas.length > 0 && (
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.persona}</label>
              <ModelDropdown
                value={formPersonaId}
                options={personaOptions}
                placeholder={ct.personaDefault}
                disabled={saving}
                onChange={(value) => setFormPersonaId(value)}
              />
            </div>
          )}

          <div className="flex items-center gap-2 pt-1">
            <button
              onClick={handleCreate}
              disabled={createDisabled}
              className="rounded-lg px-4 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-50"
              style={{ background: 'var(--c-accent, #3b82f6)' }}
            >
              {saving ? ct.saving : ct.save}
            </button>
            <button
              onClick={resetCreateForm}
              className="rounded-lg px-4 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {ct.cancel}
            </button>
          </div>
        </div>
      )}

      {/* Channel list */}
      {channels.length === 0 && !showForm ? (
        <div className="flex flex-col items-center gap-2 py-12 text-center">
          <p className="text-sm text-[var(--c-text-secondary)]">{ct.noChannels}</p>
          <p className="text-xs text-[var(--c-text-tertiary)]">{ct.noChannelsDesc}</p>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {channels.map((ch) => (
            <div
              key={ch.id}
              className="flex flex-col gap-2 rounded-lg px-4 py-3"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              <div className="flex items-center gap-3">
                <div className="flex flex-1 flex-col gap-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-[var(--c-text-heading)]">{channelLabel(ch.channel_type)}</span>
                    <span
                      className="rounded px-1.5 py-0.5 text-[10px] font-medium"
                      style={{
                        background: ch.is_active ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))' : 'var(--c-bg-deep)',
                        color: ch.is_active ? 'var(--c-status-success, #22c55e)' : 'var(--c-text-muted)',
                      }}
                    >
                      {ch.is_active ? ct.active : ct.inactive}
                    </span>
                  </div>
                  {ch.webhook_url && (
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-xs text-[var(--c-text-tertiary)]">{ch.webhook_url}</span>
                      <CopyIconButton
                        onCopy={() => handleCopyWebhook(ch.webhook_url!)}
                        size={12}
                        tooltip={t.copyAction}
                        className="shrink-0 text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]"
                      />
                    </div>
                  )}
                </div>

                <div className="flex items-center gap-2 shrink-0">
                  <button
                    onClick={() => handleToggle(ch)}
                    className="rounded-lg px-3 py-1 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  >
                    {ch.is_active ? ct.inactive : ct.active}
                  </button>
                  <button
                    onClick={() => setDeleteTarget(ch)}
                    className="text-[var(--c-text-muted)] hover:text-[var(--c-status-error,#ef4444)]"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              </div>

              {ch.channel_type === 'qq' && (
                <QQLoginFlow accessToken={accessToken} channelId={ch.id} />
              )}

              {ch.channel_type === 'telegram' && (
                <div className="flex gap-2">
                  <input
                    type="password"
                    value={tokenDrafts[ch.id] ?? ''}
                    onChange={(e) => setTokenDrafts(prev => ({ ...prev, [ch.id]: e.target.value }))}
                    placeholder={ch.has_credentials && !(tokenDrafts[ch.id] ?? '') ? ct.tokenAlreadyConfigured : ct.botTokenPlaceholder}
                    className="h-8 flex-1 rounded-lg bg-[var(--c-bg-input)] px-3 text-xs text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  />
                  {(tokenDrafts[ch.id] ?? '').trim() && (
                    <button
                      onClick={() => handleUpdateToken(ch.id, tokenDrafts[ch.id])}
                      className="rounded-lg px-3 text-xs font-medium text-white"
                      style={{ background: 'var(--c-accent, #3b82f6)' }}
                    >
                      {ct.save}
                    </button>
                  )}
                </div>
              )}

              {ch.channel_type === 'feishu' && (
                <div className="flex flex-col gap-2">
                  {(readChannelConfigString(ch, 'bot_name') || readChannelConfigString(ch, 'bot_open_id')) && (
                    <div className="text-xs text-[var(--c-text-tertiary)]">
                      {readChannelConfigString(ch, 'bot_name') || readChannelConfigString(ch, 'bot_open_id')}
                    </div>
                  )}
                  {verifyResults[ch.id] && (
                    <div
                      className="rounded-lg px-3 py-2 text-xs"
                      style={{
                        background: verifyResults[ch.id].ok
                          ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))'
                          : 'var(--c-status-error-bg, rgba(239,68,68,0.08))',
                        color: verifyResults[ch.id].ok
                          ? 'var(--c-status-success, #22c55e)'
                          : 'var(--c-status-error, #ef4444)',
                      }}
                    >
                      {verifyResults[ch.id].message}
                    </div>
                  )}
                  <button
                    onClick={() => void handleVerify(ch)}
                    disabled={!ch.has_credentials || verifyingChannelId === ch.id}
                    className="self-start rounded-lg px-3 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
                    style={{ border: '0.5px solid var(--c-border-subtle)' }}
                  >
                    {verifyingChannelId === ch.id ? ds.connectorVerifying : ds.connectorVerify}
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Bindings section */}
      <div className="flex flex-col gap-3 pt-2" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-[var(--c-text-heading)]">{ct.bindingsTitle}</h3>
          <button
            onClick={handleGenerateBindCode}
            disabled={generating}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <Link2 size={14} />
            {generating ? ct.generating : ct.generateCode}
          </button>
        </div>

        {bindCode && (
          <div className="flex flex-col gap-1 rounded-lg p-3" style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}>
            <div className="flex items-center gap-2">
              <span className="text-xs text-[var(--c-text-secondary)]">{ct.bindCode}:</span>
              <code className="rounded bg-[var(--c-bg-deep)] px-2 py-0.5 text-sm font-mono font-semibold text-[var(--c-text-heading)]">{bindCode}</code>
            </div>
            <p className="text-xs text-[var(--c-text-tertiary)]">{ct.bindCodeHint.replace('{code}', bindCode)}</p>
          </div>
        )}

        {identities.length === 0 ? (
          <p className="text-xs text-[var(--c-text-tertiary)]">{ct.bindingsEmpty}</p>
        ) : (
          <div className="flex flex-col gap-1.5">
            {identities.map((id) => (
              <div
                key={id.id}
                className="flex items-center gap-3 rounded-lg px-3 py-2"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              >
                <div className="flex flex-1 flex-col min-w-0">
                  <span className="text-sm text-[var(--c-text-heading)]">{id.display_name || id.platform_subject_id}</span>
                  <span className="text-xs text-[var(--c-text-tertiary)]">{channelLabel(id.channel_type)}</span>
                </div>
                <button
                  onClick={() => setUnbindTarget(id.id)}
                  className="text-xs text-[var(--c-text-muted)] hover:text-[var(--c-status-error,#ef4444)]"
                >
                  {ct.unbind}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
      <ConfirmDialog
        open={deleteTarget !== null}
        title={ct.delete}
        message={ct.deleteConfirm}
        confirmLabel={ct.delete}
        cancelLabel={ct.cancel}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => { if (deleteTarget) void handleDelete(deleteTarget) }}
      />
      <ConfirmDialog
        open={unbindTarget !== null}
        title={ct.unbind}
        message={ct.unbindConfirm}
        confirmLabel={ct.unbind}
        cancelLabel={ct.cancel}
        onClose={() => setUnbindTarget(null)}
        onConfirm={() => { if (unbindTarget) void handleUnbind(unbindTarget) }}
      />
    </div>
  )
}
