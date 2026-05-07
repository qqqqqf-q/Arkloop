import { useState, useEffect, useCallback, useRef } from 'react'
import { Loader2, Plus, Trash2, Star, Eye, EyeOff, Mic, Pencil } from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { DesktopConfig } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listAsrCredentials,
  createAsrCredential,
  deleteAsrCredential,
  setDefaultAsrCredential,
  updateAsrCredential,
  type AsrCredential,
  type CreateAsrCredentialRequest,
  type UpdateAsrCredentialRequest,
} from '../../api'

type Props = {
  accessToken: string
  initialConfig?: DesktopConfig | null
}

const PROVIDERS = [
  { value: 'groq', label: 'Groq' },
  { value: 'openai', label: 'OpenAI' },
]

const MODELS: Record<string, { value: string; label: string }[]> = {
  groq: [
    { value: 'whisper-large-v3-turbo', label: 'whisper-large-v3-turbo' },
    { value: 'whisper-large-v3', label: 'whisper-large-v3' },
    { value: 'distil-whisper-large-v3-en', label: 'distil-whisper-large-v3-en' },
  ],
  openai: [
    { value: 'whisper-1', label: 'whisper-1' },
  ],
}

const LANGUAGES = [
  { value: '', label: 'auto' }, // auto-detect
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
  { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' },
  { value: 'es', label: 'Español' },
  { value: 'fr', label: 'Français' },
  { value: 'de', label: 'Deutsch' },
]

import { settingsSectionCls } from './_SettingsSection'
import { SettingsButton, SettingsIconButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'
import { SettingsModalFrame } from './_SettingsModalFrame'
import { SettingsSelect } from './_SettingsSelect'
import { SettingsSwitch } from './_SettingsSwitch'

const sectionCls = settingsSectionCls

const fieldLabelCls = 'block text-[11px] font-medium text-[var(--c-placeholder)] mb-1 pl-[2px]'

// -- Add Credential Modal --

function AddCredentialModal({
  ds,
  accessToken,
  onClose,
  onCreated,
}: {
  ds: ReturnType<typeof useLocale>['t']['desktopSettings']
  accessToken: string
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [provider, setProvider] = useState<string>('groq')
  const [model, setModel] = useState<string>('whisper-large-v3-turbo')
  const [apiKey, setApiKey] = useState('')
  const [isDefault, setIsDefault] = useState(false)
  const [showKey, setShowKey] = useState(false)
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    if (!name.trim() || !apiKey.trim()) return
    setSaving(true)
    try {
      const req: CreateAsrCredentialRequest = {
        name: name.trim(),
        provider,
        api_key: apiKey.trim(),
        model,
        is_default: isDefault,
      }
      await createAsrCredential(req, accessToken)
      onCreated()
    } catch (err) {
      console.error('create ASR credential failed', err)
    } finally {
      setSaving(false)
    }
  }

  const modelOptions = MODELS[provider] ?? []

  return (
    <SettingsModalFrame
      open
      title={ds.voiceCredsAddTitle}
      onClose={onClose}
      width={510}
      footer={(
        <>
          <SettingsButton size="modal" variant="secondary" onClick={onClose} disabled={saving}>
            {ds.voiceCredsCancel}
          </SettingsButton>
          <SettingsButton
            size="modal"
            variant="primary"
            onClick={() => void handleSave()}
            disabled={saving || !name.trim() || !apiKey.trim()}
            icon={saving ? <Loader2 size={14} className="animate-spin" /> : undefined}
          >
            {ds.voiceCredsSave}
          </SettingsButton>
        </>
      )}
    >
        <div className="mt-7 grid grid-cols-2 gap-x-4 gap-y-4">
          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsName}</label>
            <SettingsInput
              variant="md"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={ds.voiceCredsNamePlaceholder}
            />
          </div>

          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsProvider}</label>
            <SettingsSelect
              value={provider}
              onChange={(v) => { setProvider(v); setModel(MODELS[v]?.[0]?.value ?? '') }}
              options={PROVIDERS}
              triggerClassName="h-[35px]"
            />
          </div>

          <div className="col-span-2">
            <label className={fieldLabelCls}>{ds.voiceCredsModel}</label>
            <SettingsSelect
              value={model}
              onChange={setModel}
              options={modelOptions}
              triggerClassName="h-[35px]"
            />
          </div>

          <div className="col-span-2">
            <label className={fieldLabelCls}>{ds.voiceCredsApiKey}</label>
            <div className="relative">
              <SettingsInput
                variant="md"
                type={showKey ? 'text' : 'password'}
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={ds.voiceCredsApiKeyPlaceholder}
                className="pr-9"
              />
              <button
                type="button"
                onClick={() => setShowKey((v) => !v)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
              >
                {showKey ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
            </div>
          </div>
        </div>

        <div className="mt-5 flex items-center justify-between gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={isDefault}
              onChange={(e) => setIsDefault(e.target.checked)}
              className="accent-[var(--c-btn-bg)]"
            />
            {ds.voiceCredsIsDefault}
          </label>
        </div>
    </SettingsModalFrame>
  )
}

// -- Delete Confirm Modal --

function DeleteConfirmModal({
  ds,
  cred,
  onClose,
  onConfirm,
}: {
  ds: ReturnType<typeof useLocale>['t']['desktopSettings']
  cred: AsrCredential
  onClose: () => void
  onConfirm: () => void
}) {
  const [deleting, setDeleting] = useState(false)

  const handleConfirm = async () => {
    setDeleting(true)
    try { await onConfirm() } finally { setDeleting(false) }
  }

  return (
    <SettingsModalFrame
      open
      title={ds.voiceCredsDelete}
      onClose={onClose}
      width={420}
      footer={(
        <>
          <SettingsButton size="modal" variant="secondary" onClick={onClose} disabled={deleting}>
            {ds.voiceCredsCancel}
          </SettingsButton>
          <SettingsButton
            size="modal"
            variant="danger"
            onClick={() => void handleConfirm()}
            disabled={deleting}
            icon={deleting ? <Loader2 size={14} className="animate-spin" /> : undefined}
          >
            {ds.voiceCredsDelete}
          </SettingsButton>
        </>
      )}
    >
        <p className="mt-7 text-sm text-[var(--c-text-primary)]">
          {ds.voiceCredsDeleteConfirm}{' '}
          <span className="font-medium">{cred.name}</span>
        </p>
    </SettingsModalFrame>
  )
}

// -- Edit Credential Modal --

function EditCredentialModal({
  ds,
  cred,
  accessToken,
  onClose,
  onUpdated,
}: {
  ds: ReturnType<typeof useLocale>['t']['desktopSettings']
  cred: AsrCredential
  accessToken: string
  onClose: () => void
  onUpdated: () => void
}) {
  const [name, setName] = useState(cred.name)
  const [model, setModel] = useState(cred.model)
  const [isDefault, setIsDefault] = useState(cred.is_default)
  const [saving, setSaving] = useState(false)

  const provider = cred.provider // provider is not editable
  const modelOptions = MODELS[provider] ?? []

  const handleSave = async () => {
    if (!name.trim()) return
    setSaving(true)
    try {
      const req: UpdateAsrCredentialRequest = {
        name: name.trim(),
        model: model,
        is_default: isDefault,
      }
      await updateAsrCredential(cred.id, req, accessToken)
      onUpdated()
    } catch (err) {
      console.error('update ASR credential failed', err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsModalFrame
      open
      title={ds.voiceCredsEditTitle}
      onClose={onClose}
      width={510}
      footer={(
        <>
          <SettingsButton size="modal" variant="secondary" onClick={onClose} disabled={saving}>
            {ds.voiceCredsCancel}
          </SettingsButton>
          <SettingsButton
            size="modal"
            variant="primary"
            onClick={() => void handleSave()}
            disabled={saving || !name.trim()}
            icon={saving ? <Loader2 size={14} className="animate-spin" /> : undefined}
          >
            {ds.voiceCredsSave}
          </SettingsButton>
        </>
      )}
    >
        <div className="mt-7 grid grid-cols-2 gap-x-4 gap-y-4">
          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsName}</label>
            <SettingsInput
              variant="md"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsProvider}</label>
            <SettingsInput
              variant="md"
              value={provider}
              disabled
              className="cursor-not-allowed bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]"
            />
          </div>

          <div className="col-span-2">
            <label className={fieldLabelCls}>{ds.voiceCredsModel}</label>
            <SettingsSelect
              value={model}
              onChange={setModel}
              options={modelOptions}
              triggerClassName="h-[35px]"
            />
          </div>
        </div>

        <div className="mt-5 flex items-center justify-between gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={isDefault}
              onChange={(e) => setIsDefault(e.target.checked)}
              className="accent-[var(--c-btn-bg)]"
            />
            {ds.voiceCredsIsDefault}
          </label>
        </div>
    </SettingsModalFrame>
  )
}

// -- Main VoiceSettings --

export function VoiceSettings({ accessToken, initialConfig = null }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()

  const [voiceEnabled, setVoiceEnabled] = useState(false)
  const [voiceLanguage, setVoiceLanguage] = useState('')
  const [toggleSaving, setToggleSaving] = useState(false)
  const toggleSavingRef = useRef(false)
  const voiceEnabledRef = useRef(false)
  const [configLoading, setConfigLoading] = useState(true)
  const [voiceCardHovered, setVoiceCardHovered] = useState(false)

  const [credentials, setCredentials] = useState<AsrCredential[]>([])
  const [credsLoading, setCredsLoading] = useState(false)
  const [credsError, setCredsError] = useState<string | null>(null)

  const [showAddModal, setShowAddModal] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<AsrCredential | null>(null)
  const [editTarget, setEditTarget] = useState<AsrCredential | null>(null)

  // Read initial voice config
  useEffect(() => {
    if (!api) { setConfigLoading(false); return }
    const applyConfig = (cfg: DesktopConfig) => {
      setVoiceEnabled(cfg.voice?.enabled ?? false)
      voiceEnabledRef.current = cfg.voice?.enabled ?? false
      setVoiceLanguage(cfg.voice?.language ?? '')
      setConfigLoading(false)
    }
    if (initialConfig) {
      applyConfig(initialConfig)
      return
    }
    void api.config.get().then(applyConfig)
  }, [api, initialConfig])

  // Listen for config changes from other sources (e.g. VoiceInput writing via ChatInput)
  useEffect(() => {
    if (!api) return
    return api.config.onChanged((cfg) => {
      setVoiceEnabled(cfg.voice?.enabled ?? false)
      voiceEnabledRef.current = cfg.voice?.enabled ?? false
      setVoiceLanguage(cfg.voice?.language ?? '')
    })
  }, [api])

  const fetchCredentials = useCallback(async () => {
    setCredsLoading(true)
    setCredsError(null)
    try {
      setCredentials(await listAsrCredentials(accessToken))
    } catch {
      setCredsError(ds.voiceCredsLoadError)
    } finally {
      setCredsLoading(false)
    }
  }, [accessToken, ds.voiceCredsLoadError])

  useEffect(() => { void fetchCredentials() }, [fetchCredentials])

  const handleToggleVoice = useCallback(async (enabled: boolean) => {
    if (!api || toggleSavingRef.current) return
    toggleSavingRef.current = true
    setToggleSaving(true)
    try {
      const cfg = await api.config.get()
      await api.config.set({ ...cfg, voice: { enabled, language: voiceLanguage } })
      setVoiceEnabled(enabled)
      voiceEnabledRef.current = enabled
    } catch (err) {
      console.error('voice toggle failed', err)
    } finally {
      toggleSavingRef.current = false
      setToggleSaving(false)
    }
  }, [api, voiceLanguage])

  const handleLanguageChange = useCallback(async (language: string) => {
    if (!api) return
    try {
      const cfg = await api.config.get()
      await api.config.set({ ...cfg, voice: { enabled: voiceEnabledRef.current, language } })
      setVoiceLanguage(language)
    } catch (err) {
      console.error('voice language change failed', err)
    }
  }, [api])

  const handleSetDefault = useCallback(async (id: string) => {
    try {
      await setDefaultAsrCredential(id, accessToken)
      await fetchCredentials()
    } catch (err) {
      console.error('set default ASR credential failed', err)
    }
  }, [accessToken, fetchCredentials])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    await deleteAsrCredential(deleteTarget.id, accessToken)
    setDeleteTarget(null)
    await fetchCredentials()
  }, [deleteTarget, accessToken, fetchCredentials])

  if (configLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{ds.voiceTitle}</h3>
        <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{ds.voiceDesc}</p>
      </div>

      {/* Enable toggle */}
      <div
        role="button"
        tabIndex={0}
        className={`${sectionCls} flex cursor-pointer items-center justify-between outline-none transition-colors hover:bg-[var(--c-bg-deep)]/40 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]`}
        onMouseEnter={() => setVoiceCardHovered(true)}
        onMouseLeave={() => setVoiceCardHovered(false)}
        onClick={() => { if (!toggleSaving) void handleToggleVoice(!voiceEnabled) }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            if (!toggleSaving) void handleToggleVoice(!voiceEnabled)
          }
        }}
      >
        <div>
          <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.voiceEnableLabel}</p>
          <p className="mt-0.5 text-xs text-[var(--c-text-secondary)]">{ds.voiceEnableDesc}</p>
        </div>
        <div onClick={(e) => e.stopPropagation()}>
          <SettingsSwitch
            checked={voiceEnabled}
            onChange={handleToggleVoice}
            disabled={toggleSaving}
            forceHover={voiceCardHovered}
          />
        </div>
      </div>

      {/* Language selection */}
      <div className={sectionCls}>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.voiceLangLabel}</p>
          </div>
          <SettingsSelect
            value={voiceLanguage}
            onChange={handleLanguageChange}
            options={LANGUAGES}
            triggerClassName="w-[140px]"
          />
        </div>
      </div>

      {/* ASR credentials */}
      <div className={sectionCls}>
        <div className="flex items-center justify-between">
          <h4 className="text-sm font-medium text-[var(--c-text-heading)]">{ds.voiceCredsTitle}</h4>
          <SettingsButton
            variant="primary"
            icon={<Plus size={14} />}
            onClick={() => setShowAddModal(true)}
          >
            {ds.voiceCredsAdd}
          </SettingsButton>
        </div>

        {credsLoading ? (
          <div className="mt-3 flex items-center justify-center py-6">
            <Loader2 size={16} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : credsError ? (
          <p className="mt-3 text-sm text-[var(--c-status-error)]">{credsError}</p>
        ) : credentials.length === 0 ? (
          <div className="mt-3 flex flex-col items-center py-6">
            <Mic size={24} className="mb-2 text-[var(--c-text-muted)]" />
            <p className="text-sm text-[var(--c-text-muted)]">{ds.voiceCredsEmpty}</p>
          </div>
        ) : (
          <div className="mt-3 flex flex-col gap-2">
            {credentials.map((cred) => (
              <div
                key={cred.id}
                className="group flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--c-border-subtle)] px-4 py-2.5"
                style={{ contentVisibility: 'auto', containIntrinsicBlockSize: '60px' }}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <p className="truncate text-sm font-medium text-[var(--c-text-primary)]">{cred.name}</p>
                    {cred.is_default && (
                      <span
                        className="shrink-0 rounded-md px-2 py-0.5 text-xs font-medium"
                        style={{ background: 'var(--c-bg-sub)', color: 'var(--c-text-muted)' }}
                      >
                        {ds.voiceCredsDefault}
                      </span>
                    )}
                  </div>
                  <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                    {cred.provider} · {cred.model}
                    {cred.key_prefix ? ` · ${cred.key_prefix}…` : ''}
                  </p>
                </div>
                <div className="flex w-full shrink-0 items-center justify-end gap-1.5 sm:w-auto">
                  {!cred.is_default && (
                    <SettingsIconButton
                      label={ds.voiceCredsSetDefault}
                      onClick={() => void handleSetDefault(cred.id)}
                    >
                      <Star size={14} />
                    </SettingsIconButton>
                  )}
                  <SettingsIconButton
                    label={ds.voiceCredsEdit}
                    onClick={() => setEditTarget(cred)}
                  >
                    <Pencil size={14} />
                  </SettingsIconButton>
                  <SettingsIconButton
                    label={ds.voiceCredsDelete}
                    danger
                    onClick={() => setDeleteTarget(cred)}
                  >
                    <Trash2 size={14} />
                  </SettingsIconButton>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {showAddModal && (
        <AddCredentialModal
          ds={ds}
          accessToken={accessToken}
          onClose={() => setShowAddModal(false)}
          onCreated={async () => { setShowAddModal(false); await fetchCredentials() }}
        />
      )}

      {deleteTarget && (
        <DeleteConfirmModal
          ds={ds}
          cred={deleteTarget}
          onClose={() => setDeleteTarget(null)}
          onConfirm={handleDelete}
        />
      )}

      {editTarget && (
        <EditCredentialModal
          ds={ds}
          cred={editTarget}
          accessToken={accessToken}
          onClose={() => setEditTarget(null)}
          onUpdated={async () => { setEditTarget(null); await fetchCredentials() }}
        />
      )}
    </div>
  )
}
