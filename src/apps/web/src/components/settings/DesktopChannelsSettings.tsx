import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Check,
  Eye,
  EyeOff,
  Link2,
  Loader2,
  Plus,
  Radio,
  X,
} from 'lucide-react'
import {
  type ChannelIdentityResponse,
  type ChannelResponse,
  type Persona,
  createChannel,
  createChannelBindCode,
  isApiError,
  listChannelPersonas,
  listChannels,
  listMyChannelIdentities,
  unbindChannelIdentity,
  updateChannel,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { DEFAULT_PERSONA_KEY } from '../../storage'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

type Props = {
  accessToken: string
}

const inputCls =
  'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)] transition-colors'
const secondaryButtonCls =
  'inline-flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50'
const primaryButtonCls =
  'inline-flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors hover:opacity-90 disabled:opacity-50'

function readAllowedUserIDs(channel: ChannelResponse | null): string[] {
  const raw = channel?.config_json?.allowed_user_ids
  if (!Array.isArray(raw)) return []
  const seen = new Set<string>()
  const values: string[] = []
  for (const item of raw) {
    if (typeof item !== 'string') continue
    const cleaned = item.trim()
    if (!cleaned || seen.has(cleaned)) continue
    seen.add(cleaned)
    values.push(cleaned)
  }
  return values
}

function parseAllowedUserIDs(input: string): string[] {
  return input
    .split(/[\n,\s]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function mergeAllowedUserIDs(existing: string[], pendingInput: string): string[] {
  const seen = new Set<string>()
  const merged: string[] = []

  for (const item of [...existing, ...parseAllowedUserIDs(pendingInput)]) {
    if (!item || seen.has(item)) continue
    seen.add(item)
    merged.push(item)
  }

  return merged
}

function sameItems(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((item, index) => item === b[index])
}

function defaultPersonaID(personas: Persona[]): string {
  const preferred = personas.find((persona) => persona.persona_key === DEFAULT_PERSONA_KEY)
  return preferred?.id ?? personas[0]?.id ?? ''
}

function resolvePersonaID(personas: Persona[], storedPersonaID?: string | null): string {
  const cleaned = storedPersonaID?.trim()
  if (cleaned) return cleaned
  return defaultPersonaID(personas)
}

function StatusBadge({
  active,
  label,
}: {
  active: boolean
  label: string
}) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium"
      style={{
        background: active ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))' : 'var(--c-bg-deep)',
        color: active ? 'var(--c-status-success, #22c55e)' : 'var(--c-text-muted)',
      }}
    >
      <span
        className="inline-block h-1.5 w-1.5 rounded-full"
        style={{ background: active ? 'currentColor' : 'var(--c-text-muted)' }}
      />
      {label}
    </span>
  )
}

export function DesktopChannelsSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ct = t.channels
  const ds = t.desktopSettings

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  const [telegramChannel, setTelegramChannel] = useState<ChannelResponse | null>(null)
  const [personas, setPersonas] = useState<Persona[]>([])
  const [identities, setIdentities] = useState<ChannelIdentityResponse[]>([])

  const [enabled, setEnabled] = useState(false)
  const [personaID, setPersonaID] = useState('')
  const [allowedUserIDs, setAllowedUserIDs] = useState<string[]>([])
  const [allowedUserInput, setAllowedUserInput] = useState('')

  const [tokenDraft, setTokenDraft] = useState('')
  const [showToken, setShowToken] = useState(false)

  const [bindCode, setBindCode] = useState<string | null>(null)
  const [generatingCode, setGeneratingCode] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [channels, linkedIdentities, allPersonas] = await Promise.all([
        listChannels(accessToken),
        listMyChannelIdentities(accessToken).catch(() => [] as ChannelIdentityResponse[]),
        listChannelPersonas(accessToken).catch(() => [] as Persona[]),
      ])
      const channel = channels.find((item) => item.channel_type === 'telegram') ?? null
      setTelegramChannel(channel)
      setPersonas(allPersonas)
      setIdentities(linkedIdentities.filter((item) => item.channel_type === 'telegram'))
      setEnabled(channel?.is_active ?? false)
      setPersonaID(resolvePersonaID(allPersonas, channel?.persona_id))
      setAllowedUserIDs(readAllowedUserIDs(channel))
      setTokenDraft('')
      setAllowedUserInput('')
      setError('')
    } catch {
      setError(ct.loadFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, ct.loadFailed])

  useEffect(() => {
    void load()
  }, [load])

  const persistedAllowedUserIDs = useMemo(
    () => readAllowedUserIDs(telegramChannel),
    [telegramChannel],
  )
  const effectiveAllowedUserIDs = useMemo(
    () => mergeAllowedUserIDs(allowedUserIDs, allowedUserInput),
    [allowedUserIDs, allowedUserInput],
  )
  const effectivePersonaID = useMemo(
    () => resolvePersonaID(personas, telegramChannel?.persona_id),
    [personas, telegramChannel?.persona_id],
  )

  const dirty = useMemo(() => {
    if (loading) return false
    if ((telegramChannel?.is_active ?? false) !== enabled) return true
    if (effectivePersonaID !== personaID) return true
    if (!sameItems(persistedAllowedUserIDs, effectiveAllowedUserIDs)) return true
    return tokenDraft.trim().length > 0
  }, [
    effectivePersonaID,
    effectiveAllowedUserIDs,
    enabled,
    loading,
    persistedAllowedUserIDs,
    personaID,
    telegramChannel,
    tokenDraft,
  ])

  const canSave = dirty && (telegramChannel !== null || tokenDraft.trim().length > 0)
  const tokenConfigured = telegramChannel !== null

  const handleAddAllowedUsers = () => {
    const nextIDs = mergeAllowedUserIDs(allowedUserIDs, allowedUserInput)
    if (nextIDs.length === allowedUserIDs.length) return
    setAllowedUserIDs(nextIDs)
    setAllowedUserInput('')
    setSaved(false)
  }

  const handleRemoveAllowedUser = (value: string) => {
    setAllowedUserIDs((current) => current.filter((item) => item !== value))
    setSaved(false)
  }

  const handleSave = async () => {
    const nextAllowedUserIDs = mergeAllowedUserIDs(allowedUserIDs, allowedUserInput)

    if (enabled && !personaID) {
      setError(ct.personaRequired)
      return
    }
    if (enabled && nextAllowedUserIDs.length === 0) {
      setError(ct.allowlistRequired)
      return
    }

    setSaving(true)
    setError('')
    try {
      const configJSON = { allowed_user_ids: nextAllowedUserIDs }

      if (telegramChannel == null) {
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
        await updateChannel(accessToken, telegramChannel.id, {
          bot_token: tokenDraft.trim() || undefined,
          persona_id: personaID || null,
          is_active: enabled,
          config_json: configJSON,
        })
      }

      setAllowedUserIDs(nextAllowedUserIDs)
      setAllowedUserInput('')
      setSaved(true)
      setTimeout(() => setSaved(false), 2500)
      await load()
    } catch (err) {
      setError(isApiError(err) ? err.message : ct.saveFailed)
    } finally {
      setSaving(false)
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

  const handleUnbind = async (identityID: string) => {
    if (!confirm(ct.unbindConfirm)) return
    try {
      await unbindChannelIdentity(accessToken, identityID)
      await load()
    } catch {
      setError(ct.unbindFailed)
    }
  }

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <SettingsSectionHeader title={ct.telegram} />
        <div className="flex items-center justify-center py-20 text-[var(--c-text-muted)]">
          <Loader2 size={20} className="animate-spin" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ct.telegram} />

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
                    <StatusBadge
                      active={enabled}
                      label={enabled ? ct.active : ct.inactive}
                    />
                    <StatusBadge
                      active={tokenConfigured}
                      label={tokenConfigured ? ds.connectorConfigured : ds.connectorNotConfigured}
                    />
                  </div>
                </div>
              </div>
            </div>

            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              onClick={() => {
                setEnabled((current) => !current)
                setSaved(false)
              }}
              className={[
                'relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200',
                enabled ? 'bg-[var(--c-accent)]' : 'bg-[var(--c-border)]',
              ].join(' ')}
            >
              <span
                className={[
                  'pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow transition-transform duration-200',
                  enabled ? 'translate-x-5' : 'translate-x-0',
                ].join(' ')}
              />
            </button>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.botToken}
              </label>
              <div className="relative">
                <input
                  type={showToken ? 'text' : 'password'}
                  value={tokenDraft}
                  onChange={(e) => {
                    setTokenDraft(e.target.value)
                    setSaved(false)
                  }}
                  placeholder={ct.botTokenPlaceholder}
                  className={inputCls}
                />
                <button
                  type="button"
                  onClick={() => setShowToken((current) => !current)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
                >
                  {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
                </button>
              </div>
            </div>

            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.allowedUsers}
              </label>
              {allowedUserIDs.length > 0 && (
                <div className="mb-2 flex flex-wrap gap-2">
                  {allowedUserIDs.map((item) => (
                    <span
                      key={item}
                      className="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs text-[var(--c-text-primary)]"
                      style={{ background: 'var(--c-bg-deep)' }}
                    >
                      {item}
                      <button
                        type="button"
                        onClick={() => handleRemoveAllowedUser(item)}
                        className="text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-primary)]"
                        aria-label={ct.delete}
                      >
                        <X size={12} />
                      </button>
                    </span>
                  ))}
                </div>
              )}
              <div className="flex gap-2">
                <input
                  type="text"
                  value={allowedUserInput}
                  onChange={(e) => setAllowedUserInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      handleAddAllowedUsers()
                    }
                  }}
                  placeholder={ct.allowedUsersPlaceholder}
                  className={inputCls}
                />
                <button
                  type="button"
                  onClick={handleAddAllowedUsers}
                  className={`${secondaryButtonCls} shrink-0`}
                  style={{
                    border: '0.5px solid var(--c-border-subtle)',
                    background: 'var(--c-bg-page)',
                  }}
                >
                  <Plus size={14} />
                  {t.skills.add}
                </button>
              </div>
            </div>

            <div className="md:col-span-2">
              <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
                {ct.persona}
              </label>
              <select
                value={personaID}
                onChange={(e) => {
                  setPersonaID(e.target.value)
                  setSaved(false)
                }}
                className={inputCls}
              >
                {personas.length === 0 && (
                  <option value="">{ct.personaDefault}</option>
                )}
                {personas.map((persona) => (
                  <option key={persona.id} value={persona.id}>
                    {persona.display_name || persona.id}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </div>
      </div>

      <div
        className="rounded-2xl p-5"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="text-sm font-medium text-[var(--c-text-heading)]">{ct.bindingsTitle}</div>
              {bindCode && (
                <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                  <span className="text-xs text-[var(--c-text-muted)]">{ct.bindCode}</span>
                  <code className="rounded-md bg-[var(--c-bg-deep)] px-2 py-1 font-mono text-[var(--c-text-heading)]">
                    {bindCode}
                  </code>
                </div>
              )}
            </div>

            <button
              type="button"
              onClick={() => void handleGenerateBindCode()}
              disabled={generatingCode}
              className={`${secondaryButtonCls} shrink-0`}
              style={{
                border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-page)',
              }}
            >
              {generatingCode ? <Loader2 size={14} className="animate-spin" /> : <Link2 size={14} />}
              {generatingCode ? ct.generating : ct.generateCode}
            </button>
          </div>

          {bindCode && (
            <p className="text-xs text-[var(--c-text-muted)]">
              {ct.bindCodeHint.replace('{code}', bindCode)}
            </p>
          )}

          {identities.length === 0 ? (
            <p className="text-sm text-[var(--c-text-muted)]">{ct.bindingsEmpty}</p>
          ) : (
            <div className="flex flex-col gap-2">
              {identities.map((identity) => (
                <div
                  key={identity.id}
                  className="flex items-center justify-between gap-3 rounded-xl px-4 py-3"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium text-[var(--c-text-heading)]">
                      {identity.display_name || identity.platform_subject_id}
                    </div>
                    <div className="truncate text-xs text-[var(--c-text-muted)]">
                      {identity.platform_subject_id}
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => void handleUnbind(identity.id)}
                    className="shrink-0 rounded-md px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                  >
                    {ct.unbind}
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="flex items-center gap-3 border-t border-[var(--c-border-subtle)] pt-4">
        <button
          type="button"
          onClick={() => void handleSave()}
          disabled={saving || !canSave}
          className={primaryButtonCls}
          style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
        >
          {saving && <Loader2 size={13} className="animate-spin" />}
          {!saving && saved && <Check size={13} />}
          {saving ? ct.saving : ct.save}
        </button>
        {saved && !dirty && (
          <span
            className="inline-flex items-center gap-1 text-xs"
            style={{ color: 'var(--c-status-success, #22c55e)' }}
          >
            <Check size={11} />
            {ds.connectorSaved}
          </span>
        )}
      </div>
    </div>
  )
}
