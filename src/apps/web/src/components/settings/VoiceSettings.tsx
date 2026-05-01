import { useState, useEffect, useCallback, useRef } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, Plus, Trash2, Star, X, Eye, EyeOff, Mic, ChevronDown, Pencil } from 'lucide-react'
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
import { PillToggle } from '@arkloop/shared'

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

const sectionCls = settingsSectionCls

const fieldLabelCls = 'block text-[11px] font-medium text-[var(--c-placeholder)] mb-1 pl-[2px]'
const fieldInputCls = 'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)] focus:border-[var(--c-border)]'

// -- Reusable custom dropdown --

function CustomDropdown<T extends string>({
  value,
  onChange,
  options,
  style,
}: {
  value: T
  onChange: (v: T) => void
  options: { value: T; label: string }[]
  style?: React.CSSProperties
}) {
  const [open, setOpen] = useState(false)
  const [menuStyle, setMenuStyle] = useState<React.CSSProperties>({})
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node) || btnRef.current?.contains(e.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const handleOpen = () => {
    if (!open && btnRef.current) {
      const rect = btnRef.current.getBoundingClientRect()
      const scrollY = window.scrollY || document.documentElement.scrollTop || 0
      const scrollX = window.scrollX || document.documentElement.scrollLeft || 0
      setMenuStyle({
        position: 'absolute',
        top: rect.bottom + scrollY + 4,
        left: rect.left + scrollX,
        width: rect.width,
        zIndex: 9999,
      })
    }
    setOpen((v) => !v)
  }

  const menu = open ? (
    <div
      ref={menuRef}
      className="dropdown-menu"
      style={{
        ...menuStyle,
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        padding: '4px',
        background: 'var(--c-bg-menu)',
        boxShadow: 'var(--c-dropdown-shadow)',
        maxHeight: '220px',
        overflowY: 'auto',
      }}
    >
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => { onChange(opt.value); setOpen(false) }}
          className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
          style={{
            color: value === opt.value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
            fontWeight: value === opt.value ? 500 : 400,
          }}
        >
          <span>{opt.label}</span>
          {value === opt.value && <Star size={11} className="shrink-0" />}
        </button>
      ))}
    </div>
  ) : null

  return (
    <div className="relative">
      <button
        ref={btnRef}
        type="button"
        onClick={handleOpen}
        className="flex w-full items-center justify-between rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors hover:bg-[var(--c-bg-deep)]"
        style={style}
      >
        <span className="truncate">{options.find((o) => o.value === value)?.label ?? value}</span>
        <ChevronDown size={13} className="ml-2 shrink-0 text-[var(--c-text-muted)]" />
      </button>
      {menu && createPortal(menu, document.body)}
    </div>
  )
}

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

  return createPortal(
    <div
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'var(--c-overlay)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex w-[460px] flex-col gap-5 rounded-[14px] p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-[15px] font-semibold text-[var(--c-text-heading)]">{ds.voiceCredsAddTitle}</h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <X size={14} />
          </button>
        </div>

        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          {/* Name */}
          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsName}</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={ds.voiceCredsNamePlaceholder}
              className={fieldInputCls}
            />
          </div>

          {/* Provider */}
          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsProvider}</label>
            <CustomDropdown
              value={provider}
              onChange={(v) => { setProvider(v); setModel(MODELS[v]?.[0]?.value ?? '') }}
              options={PROVIDERS}
            />
          </div>

          {/* Model */}
          <div className="col-span-2">
            <label className={fieldLabelCls}>{ds.voiceCredsModel}</label>
            <CustomDropdown
              value={model}
              onChange={setModel}
              options={modelOptions}
              style={{ width: '100%' }}
            />
          </div>

          {/* API Key */}
          <div className="col-span-2">
            <label className={fieldLabelCls}>{ds.voiceCredsApiKey}</label>
            <div className="relative">
              <input
                type={showKey ? 'text' : 'password'}
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={ds.voiceCredsApiKeyPlaceholder}
                className={`${fieldInputCls} pr-9`}
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

        <div className="flex items-center justify-between gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={isDefault}
              onChange={(e) => setIsDefault(e.target.checked)}
              className="accent-[var(--c-btn-bg)]"
            />
            {ds.voiceCredsIsDefault}
          </label>
          <div className="flex items-center justify-end gap-2">
            <button
              onClick={onClose}
              disabled={saving}
              className="rounded-lg px-4 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {ds.voiceCredsCancel}
            </button>
            <button
              onClick={() => void handleSave()}
              disabled={saving || !name.trim() || !apiKey.trim()}
              className="rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)] disabled:opacity-50"
              style={{ background: 'var(--c-btn-bg)' }}
            >
              {saving ? <Loader2 size={14} className="animate-spin" /> : ds.voiceCredsSave}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
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

  return createPortal(
    <div
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'var(--c-overlay)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex w-[380px] flex-col gap-4 rounded-[14px] p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
      >
        <p className="text-sm text-[var(--c-text-primary)]">
          {ds.voiceCredsDeleteConfirm}{' '}
          <span className="font-medium">{cred.name}</span>
        </p>
        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            disabled={deleting}
            className="rounded-lg px-4 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {ds.voiceCredsCancel}
          </button>
          <button
            onClick={() => void handleConfirm()}
            disabled={deleting}
            className="rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-status-error)] transition-opacity hover:opacity-90 disabled:opacity-50"
            style={{ border: '0.5px solid var(--c-status-error)' }}
          >
            {deleting ? <Loader2 size={14} className="animate-spin" /> : ds.voiceCredsDelete}
          </button>
        </div>
      </div>
    </div>,
    document.body,
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

  return createPortal(
    <div
      className="overlay-fade-in fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'var(--c-overlay)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex w-[460px] flex-col gap-5 rounded-[14px] p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-[15px] font-semibold text-[var(--c-text-heading)]">{ds.voiceCredsEditTitle}</h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          >
            <X size={14} />
          </button>
        </div>

        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          {/* Name */}
          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsName}</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className={fieldInputCls}
            />
          </div>

          {/* Provider (read-only) */}
          <div>
            <label className={fieldLabelCls}>{ds.voiceCredsProvider}</label>
            <input
              value={provider}
              disabled
              className={`${fieldInputCls} cursor-not-allowed bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]`}
            />
          </div>

          {/* Model */}
          <div className="col-span-2">
            <label className={fieldLabelCls}>{ds.voiceCredsModel}</label>
            <CustomDropdown
              value={model}
              onChange={setModel}
              options={modelOptions}
              style={{ width: '100%' }}
            />
          </div>

        </div>

        <div className="flex items-center justify-between gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={isDefault}
              onChange={(e) => setIsDefault(e.target.checked)}
              className="accent-[var(--c-btn-bg)]"
            />
            {ds.voiceCredsIsDefault}
          </label>
          <div className="flex items-center justify-end gap-2">
            <button
              onClick={onClose}
              disabled={saving}
              className="rounded-lg px-4 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {ds.voiceCredsCancel}
            </button>
            <button
              onClick={() => void handleSave()}
              disabled={saving || !name.trim()}
              className="rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)] disabled:opacity-50"
              style={{ background: 'var(--c-btn-bg)' }}
            >
              {saving ? <Loader2 size={14} className="animate-spin" /> : ds.voiceCredsSave}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
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
          <PillToggle
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
          <CustomDropdown
            value={voiceLanguage}
            onChange={handleLanguageChange}
            options={LANGUAGES}
            style={{ minWidth: '140px' }}
          />
        </div>
      </div>

      {/* ASR credentials */}
      <div className={sectionCls}>
        <div className="flex items-center justify-between">
          <h4 className="text-sm font-medium text-[var(--c-text-heading)]">{ds.voiceCredsTitle}</h4>
          <button
            onClick={() => setShowAddModal(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)]"
            style={{ background: 'var(--c-btn-bg)' }}
          >
            <Plus size={14} />
            {ds.voiceCredsAdd}
          </button>
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
                    <button
                      onClick={() => void handleSetDefault(cred.id)}
                      className="rounded-md p-1.5 text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
                      title={ds.voiceCredsSetDefault}
                    >
                      <Star size={14} />
                    </button>
                  )}
                  <button
                    onClick={() => setEditTarget(cred)}
                    className="rounded-md p-1.5 text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
                    title={ds.voiceCredsEdit}
                  >
                    <Pencil size={14} />
                  </button>
                  <button
                    onClick={() => setDeleteTarget(cred)}
                    className="rounded-md p-1.5 text-[var(--c-text-muted)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error)]"
                    title={ds.voiceCredsDelete}
                  >
                    <Trash2 size={14} />
                  </button>
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
