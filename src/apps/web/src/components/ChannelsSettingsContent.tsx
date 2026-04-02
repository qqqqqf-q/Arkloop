import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Link2 } from 'lucide-react'
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
  isApiError,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { AutoResizeTextarea } from '@arkloop/shared'
import { CopyIconButton } from './CopyIconButton'

type Props = {
  accessToken: string
}

const CHANNEL_TYPES = ['telegram', 'discord', 'feishu'] as const
type ChannelType = (typeof CHANNEL_TYPES)[number]

function parseAllowedUserIds(input: string): string[] {
  return input
    .split(/[\n,\s]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function ChannelsSettingsContent({ accessToken }: Props) {
  const { t } = useLocale()
  const ct = t.channels

  const [channels, setChannels] = useState<ChannelResponse[]>([])
  const [identities, setIdentities] = useState<ChannelIdentityResponse[]>([])
  const [personas, setPersonas] = useState<Persona[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [showForm, setShowForm] = useState(false)
  const [formType, setFormType] = useState<ChannelType>('telegram')
  const [formToken, setFormToken] = useState('')
  const [formPersonaId, setFormPersonaId] = useState('')
  const [formAllowedUsers, setFormAllowedUsers] = useState('')
  const [saving, setSaving] = useState(false)
  const [tokenDrafts, setTokenDrafts] = useState<Record<string, string>>({})

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

  const handleCreate = async () => {
    setSaving(true)
    setError('')
    try {
      await createChannel(accessToken, {
        channel_type: formType,
        bot_token: formToken,
        persona_id: formPersonaId || undefined,
        config_json: formType === 'telegram'
          ? { allowed_user_ids: parseAllowedUserIds(formAllowedUsers) }
          : undefined,
      })
      setShowForm(false)
      setFormToken('')
      setFormPersonaId('')
      setFormAllowedUsers('')
      await load()
    } catch (err) {
      setError(isApiError(err) ? err.message : ct.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const handleToggle = async (ch: ChannelResponse) => {
    if (!ch.is_active && ch.channel_type === 'telegram') {
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
    if (!confirm(ct.deleteConfirm)) return
    try {
      await deleteChannel(accessToken, ch.id)
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
      setError('Failed to generate bind code')
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
    if (!confirm(ct.unbindConfirm)) return
    try {
      await unbindChannelIdentity(accessToken, id)
      await load()
    } catch {
      setError(ct.unbindFailed)
    }
  }

  const channelLabel = (type: string) => {
    const map: Record<string, string> = { telegram: ct.telegram, discord: ct.discord, feishu: ct.feishu }
    return map[type] || type
  }

  const usedTypes = new Set(channels.map((c) => c.channel_type))

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
            <select
              value={formType}
              onChange={(e) => setFormType(e.target.value as ChannelType)}
              className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {CHANNEL_TYPES.filter((ct) => !usedTypes.has(ct)).map((ct) => (
                <option key={ct} value={ct}>{channelLabel(ct)}</option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.botToken}</label>
            <input
              type="password"
              value={formToken}
              onChange={(e) => setFormToken(e.target.value)}
              placeholder={ct.botTokenPlaceholder}
              className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)]"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            />
          </div>

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

          {personas.length > 0 && (
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.persona}</label>
              <select
                value={formPersonaId}
                onChange={(e) => setFormPersonaId(e.target.value)}
                className="h-9 rounded-lg bg-[var(--c-bg-input)] px-3 text-sm text-[var(--c-text-primary)] outline-none"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              >
                <option value="">{ct.personaDefault}</option>
                {personas.map((p) => (
                  <option key={p.id} value={p.id}>{p.display_name || p.id}</option>
                ))}
              </select>
            </div>
          )}

          <div className="flex items-center gap-2 pt-1">
            <button
              onClick={handleCreate}
              disabled={saving || !formToken.trim()}
              className="rounded-lg px-4 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-50"
              style={{ background: 'var(--c-accent, #3b82f6)' }}
            >
              {saving ? ct.saving : ct.save}
            </button>
            <button
              onClick={() => { setShowForm(false); setFormToken(''); setFormPersonaId(''); setFormAllowedUsers('') }}
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
                    onClick={() => handleDelete(ch)}
                    className="text-[var(--c-text-muted)] hover:text-[var(--c-status-error,#ef4444)]"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              </div>

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
                  onClick={() => handleUnbind(id.id)}
                  className="text-xs text-[var(--c-text-muted)] hover:text-[var(--c-status-error,#ef4444)]"
                >
                  {ct.unbind}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
