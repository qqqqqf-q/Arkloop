import { useState, useRef, useEffect, useCallback } from 'react'
import type { LucideIcon } from 'lucide-react'
import {
  X,
  User,
  Settings,
  HelpCircle,
  LogOut,
  ArrowUpRight,
  ChevronDown,
  ChevronLeft,
  Monitor,
  Sun,
  Moon,
  Copy,
  Check,
  RefreshCw,
  Ticket,
  Coins,
  Pencil,
  Flag,
  Puzzle,
  Cpu,
  Bot,
  Radio,
  Wifi,
} from 'lucide-react'
import {
  type MeResponse,
  type InviteCodeResponse,
  type CreditTransaction,
  getMyInviteCode,
  resetMyInviteCode,
  isApiError,
  getMyCredits,
  redeemCode,
  updateMe,
  sendEmailVerification,
  confirmEmailVerification,
  createSuggestionFeedback,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { useTheme } from '../contexts/ThemeContext'
import type { Locale } from '../locales'
import type { Theme } from '@arkloop/shared/contexts/theme'
import { SkillsSettingsContent } from './SkillsSettingsContent'
import { ModelConfigContent } from './ModelConfigContent'
import { AgentSettingsContent } from './AgentSettingsContent'
import { ChannelsSettingsContent } from './ChannelsSettingsContent'
import { ConnectionSettingsContent } from './ConnectionSettingsContent'
import { isDesktop, isLocalMode } from '@arkloop/shared/desktop'

export type SettingsTab = 'account' | 'settings' | 'skills' | 'credits' | 'models' | 'agents' | 'channels' | 'connection'

type NavItem = { key: SettingsTab; icon: LucideIcon }

const BASE_NAV_ITEMS: NavItem[] = [
  { key: 'account', icon: User },
  { key: 'settings', icon: Settings },
  { key: 'skills', icon: Puzzle },
  { key: 'models', icon: Cpu },
  { key: 'agents', icon: Bot },
  { key: 'channels', icon: Radio },
  { key: 'credits', icon: Coins },
]

const DESKTOP_NAV_ITEMS: NavItem[] = [
  ...BASE_NAV_ITEMS,
  { key: 'connection', icon: Wifi },
]

type Props = {
  me: MeResponse | null
  accessToken: string
  initialTab?: SettingsTab
  onClose: () => void
  onLogout: () => void
  onCreditsChanged?: (balance: number) => void
  onMeUpdated?: (me: MeResponse) => void
  onTrySkill?: (prompt: string) => void
}

export function SettingsModal({ me, accessToken, initialTab = 'account', onClose, onLogout, onCreditsChanged, onMeUpdated, onTrySkill }: Props) {
  const { t, locale, setLocale } = useLocale()
  const { theme, setTheme } = useTheme()
  const [activeKey, setActiveKey] = useState<SettingsTab>(initialTab)
  const [profileView, setProfileView] = useState(false)
  const localMode = isLocalMode()
  const navItems = (isDesktop() ? DESKTOP_NAV_ITEMS : BASE_NAV_ITEMS)
    .filter(item => !(localMode && item.key === 'credits'))
  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'
  const activeLabel = t.nav[activeKey as keyof typeof t.nav] ?? t.nav.account

  const handleTabChange = (key: SettingsTab) => {
    setActiveKey(key)
    if (key !== 'account') setProfileView(false)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-[2px]"
      style={{ background: 'var(--c-overlay)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter flex overflow-hidden rounded-2xl shadow-2xl bg-[var(--c-bg-page)]"
        style={{
          width: '832px',
          height: '624px',
          boxShadow: 'inset 0 0 0 0.5px var(--c-modal-ring)',
        }}
      >
        {/* 左侧导航 */}
        <div
          className="flex w-[200px] shrink-0 flex-col py-4 bg-[var(--c-bg-sidebar)]"
          style={{ borderRight: '0.5px solid rgba(0,0,0,0.14)' }}
        >
          <div className="mb-2 px-4 py-1">
            <span className="text-sm font-semibold text-[var(--c-text-heading)]">Arkloop</span>
          </div>

          <nav className="flex flex-col gap-[2px] px-2">
            {navItems.map(({ key, icon: Icon }) => (
              <button
                key={key}
                onClick={() => handleTabChange(key)}
                className={[
                  'flex h-8 items-center gap-2 rounded-md px-2 text-sm transition-colors',
                  activeKey === key
                    ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                    : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
                ].join(' ')}
              >
                <Icon size={15} />
                <span>{t.nav[key as keyof typeof t.nav]}</span>
              </button>
            ))}
          </nav>
        </div>

        {/* 右侧内容 */}
        <div className="flex flex-1 flex-col overflow-hidden">
          <div
            className="flex items-center justify-between px-6 py-4"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            {profileView ? (
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setProfileView(false)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                >
                  <ChevronLeft size={16} />
                </button>
                <h2 className="text-base font-medium text-[var(--c-text-heading)]">{t.profileTitle}</h2>
              </div>
            ) : (
              <h2 className="text-base font-medium text-[var(--c-text-heading)]">{activeLabel}</h2>
            )}
            <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              <X size={16} />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {activeKey === 'account' && !profileView && (
              <AccountContent
                me={me}
                userInitial={userInitial}
                onLogout={() => { onLogout(); onClose() }}
                onEditProfile={() => setProfileView(true)}
              />
            )}
            {activeKey === 'account' && profileView && (
              <ProfileContent
                me={me}
                accessToken={accessToken}
                userInitial={userInitial}
                onMeUpdated={onMeUpdated}
              />
            )}
            {activeKey === 'settings' && (
              <div className="flex flex-col gap-6">
                <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
                <ThemeContent theme={theme} setTheme={setTheme} label={t.appearance} t={t} />
                <InviteCodeContent accessToken={accessToken} />
                <div className="flex flex-col gap-2">
                  <HelpContent label={t.getHelp} />
                  <ReportFeedbackContent accessToken={accessToken} />
                </div>
              </div>
            )}
            {activeKey === 'skills' && (
              <SkillsSettingsContent accessToken={accessToken} onTrySkill={(prompt) => { onClose(); onTrySkill?.(prompt) }} />
            )}
            {activeKey === 'credits' && !localMode && (
              <CreditsContent accessToken={accessToken} onCreditsChanged={onCreditsChanged} />
            )}
            {activeKey === 'models' && (
              <ModelConfigContent accessToken={accessToken} />
            )}
            {activeKey === 'agents' && (
              <AgentSettingsContent accessToken={accessToken} />
            )}
            {activeKey === 'channels' && (
              <ChannelsSettingsContent accessToken={accessToken} />
            )}
            {activeKey === 'connection' && (
              <ConnectionSettingsContent />
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function AccountContent({
  me,
  userInitial,
  onLogout,
  onEditProfile,
}: {
  me: MeResponse | null
  userInitial: string
  onLogout: () => void
  onEditProfile: () => void
}) {
  const { t } = useLocale()

  return (
    <div className="flex flex-col gap-3">
      <div
        className="flex items-center gap-3 rounded-xl p-4 bg-[var(--c-bg-menu)]"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-lg font-medium"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>

        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-base font-semibold text-[var(--c-text-heading)]">
            {me?.username ?? t.loading}
          </span>
          {me?.username && (
            <span className="truncate text-xs text-[var(--c-text-tertiary)]">
              {me.username}
            </span>
          )}
          {me?.email && (
            <div className="flex items-center gap-1.5 mt-0.5">
              <span className="truncate text-xs text-[var(--c-text-tertiary)]">{me.email}</span>
              {!me.email_verified && (
                <span
                  className="shrink-0 rounded px-1 py-px text-[10px] font-medium leading-tight"
                  style={{ background: 'var(--c-status-warn-bg)', color: 'var(--c-status-warn-text)' }}
                >
                  {t.emailUnverified}
                </span>
              )}
            </div>
          )}
        </div>

        <div className="flex items-center gap-1">
          <button
            onClick={onEditProfile}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            title={t.editProfile}
          >
            <Pencil size={14} />
          </button>
          <button
            onClick={onLogout}
            className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            title={t.logout}
          >
            <LogOut size={15} />
          </button>
        </div>
      </div>

      <div
        className="rounded-xl p-4 bg-[var(--c-bg-menu)]"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.enterprisePlan}</span>
        </div>
      </div>
    </div>
  )
}

function ProfileContent({
  me,
  accessToken,
  userInitial,
  onMeUpdated,
}: {
  me: MeResponse | null
  accessToken: string
  userInitial: string
  onMeUpdated?: (me: MeResponse) => void
}) {
  const { t } = useLocale()
  const [displayName, setDisplayName] = useState(me?.username ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [sendingVerify, setSendingVerify] = useState(false)
  const [verifySent, setVerifySent] = useState(false)
  const [verifyCode, setVerifyCode] = useState('')
  const [verifying, setVerifying] = useState(false)
  const [verifyError, setVerifyError] = useState('')
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const isDirty = displayName.trim() !== (me?.username ?? '')

  const handleSave = useCallback(async () => {
    const name = displayName.trim()
    if (!name || !isDirty) return
    setSaving(true)
    setError('')
    try {
      const res = await updateMe(accessToken, name)
      if (me && onMeUpdated) {
        onMeUpdated({ ...me, username: res.username })
      }
    } catch {
      setError(t.requestFailed)
    } finally {
      setSaving(false)
    }
  }, [accessToken, displayName, isDirty, me, onMeUpdated, t])

  const handleCopyId = useCallback(async () => {
    if (!me?.id) return
    await navigator.clipboard.writeText(me.id)
    setCopied(true)
    if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
    copiedTimerRef.current = setTimeout(() => setCopied(false), 2000)
  }, [me?.id])

  const handleSendVerify = useCallback(async () => {
    setSendingVerify(true)
    setVerifyError('')
    try {
      await sendEmailVerification(accessToken)
      setVerifySent(true)
    } catch {
      // 静默失败
    } finally {
      setSendingVerify(false)
    }
  }, [accessToken])

  const handleConfirmVerify = useCallback(async () => {
    const code = verifyCode.trim()
    if (code.length !== 6) return
    setVerifying(true)
    setVerifyError('')
    try {
      await confirmEmailVerification(code)
      if (me && onMeUpdated) {
        onMeUpdated({ ...me, email_verified: true })
      }
      setVerifySent(false)
      setVerifyCode('')
    } catch {
      setVerifyError(t.emailVerifyFailed)
    } finally {
      setVerifying(false)
    }
  }, [verifyCode, me, onMeUpdated, t])

  return (
    <div className="flex flex-col gap-6">
      {/* 头像 + 名称编辑 */}
      <div className="flex items-start gap-4">
        <div
          className="flex h-16 w-16 shrink-0 items-center justify-center rounded-full text-2xl font-medium"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="flex flex-1 flex-col gap-1.5">
          <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.profileName}</span>
          <div className="flex gap-2">
            <input
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') void handleSave() }}
              className="h-9 flex-1 rounded-lg px-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              disabled={saving}
              maxLength={256}
            />
            {isDirty && (
              <button
                onClick={() => void handleSave()}
                disabled={saving || !displayName.trim()}
                className="flex h-9 items-center rounded-lg px-3 text-sm font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              >
                {saving ? '...' : t.profileSave}
              </button>
            )}
          </div>
          {error && (
            <p className="text-xs text-[var(--c-status-error-text)]">{error}</p>
          )}
        </div>
      </div>

      <div style={{ height: '0.5px', background: 'var(--c-border-subtle)' }} />

      {/* 用户名 */}
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.profileUsername}</span>
        <span className="text-sm text-[var(--c-text-tertiary)]">{me?.username ?? '—'}</span>
      </div>

      {/* 用户 ID */}
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.profileUserId}</span>
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-[var(--c-text-tertiary)] select-all">{me?.id ?? '—'}</span>
          {me?.id && (
            <button
              onClick={() => void handleCopyId()}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              title={copied ? '已复制' : '复制'}
            >
              {copied ? <Check size={12} /> : <Copy size={12} />}
            </button>
          )}
        </div>
      </div>

      {/* 邮箱 */}
      {me?.email && (
        <>
          <div style={{ height: '0.5px', background: 'var(--c-border-subtle)' }} />
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.profileEmail}</span>
            <div className="flex items-center gap-2">
              <span className="text-sm text-[var(--c-text-tertiary)]">{me.email}</span>
              {me.email_verified ? (
                <span
                  className="rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                  style={{ background: 'var(--c-status-ok-bg)', color: 'var(--c-status-ok-text)' }}
                >
                  {t.emailVerified}
                </span>
              ) : (
                <span
                  className="rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                  style={{ background: 'var(--c-status-warn-bg)', color: 'var(--c-status-warn-text)' }}
                >
                  {t.emailUnverified}
                </span>
              )}
            </div>
            {!me.email_verified && (
              <>
                <button
                  onClick={() => void handleSendVerify()}
                  disabled={sendingVerify || verifySent}
                  className="mt-1 flex h-8 w-fit items-center rounded-lg px-3 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', cursor: 'pointer' }}
                >
                  {verifySent ? t.emailVerifySent : sendingVerify ? '...' : t.emailVerifySend}
                </button>
                {verifySent && (
                  <div className="mt-2 flex flex-col gap-2">
                    <div className="flex items-center gap-2">
                      <input
                        type="text"
                        inputMode="numeric"
                        maxLength={6}
                        value={verifyCode}
                        onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, ''))}
                        placeholder={t.emailVerifyCodePlaceholder}
                        className="h-8 w-28 rounded-lg px-3 text-sm text-[var(--c-text-heading)]"
                        style={{
                          border: '0.5px solid var(--c-border-subtle)',
                          background: 'var(--c-bg-deep)',
                          outline: 'none',
                          letterSpacing: verifyCode ? '4px' : 'normal',
                        }}
                      />
                      <button
                        onClick={() => void handleConfirmVerify()}
                        disabled={verifyCode.length !== 6 || verifying}
                        className="flex h-8 items-center rounded-lg px-3 text-xs font-medium transition-colors disabled:opacity-50"
                        style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)', cursor: 'pointer' }}
                      >
                        {verifying ? '...' : t.emailVerifyConfirmBtn}
                      </button>
                    </div>
                    {verifyError && (
                      <span className="text-xs" style={{ color: 'var(--c-status-warn-text)' }}>
                        {verifyError}
                      </span>
                    )}
                  </div>
                )}
              </>
            )}
          </div>
        </>
      )}
    </div>
  )
}

const LOCALE_OPTIONS: { value: Locale; label: string }[] = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
]

function LanguageContent({
  locale,
  setLocale,
  label,
}: {
  locale: Locale
  setLocale: (l: Locale) => void
  label: string
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const currentLabel = LOCALE_OPTIONS.find(o => o.value === locale)?.label ?? locale

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node)
      ) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      <div className="relative">
        <button
          ref={btnRef}
          type="button"
          onClick={() => setOpen(v => !v)}
          className="flex h-9 w-[240px] items-center justify-between rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          <span>{currentLabel}</span>
          <ChevronDown size={13} />
        </button>
        {open && (
          <div
            ref={menuRef}
            className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              width: '240px',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            {LOCALE_OPTIONS.map(({ value, label: optLabel }) => (
              <button
                key={value}
                type="button"
                onClick={() => { setLocale(value); setOpen(false) }}
                className="flex w-full items-center px-3 py-2 text-sm transition-colors duration-100 bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
                style={{
                  borderRadius: '8px',
                  fontWeight: locale === value ? 600 : 400,
                  color: locale === value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                }}
              >
                {optLabel}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

type ThemeOption = { value: Theme; label: string; icon: LucideIcon }

function ThemeContent({
  theme,
  setTheme,
  label,
  t,
}: {
  theme: Theme
  setTheme: (t: Theme) => void
  label: string
  t: { themeSystem: string; themeLight: string; themeDark: string }
}) {
  const options: ThemeOption[] = [
    { value: 'system', label: t.themeSystem, icon: Monitor },
    { value: 'light',  label: t.themeLight,  icon: Sun     },
    { value: 'dark',   label: t.themeDark,   icon: Moon    },
  ]

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      <div
        className="flex w-[240px] rounded-lg p-[3px]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        {options.map(({ value, label: optLabel, icon: Icon }) => {
          const active = theme === value
          return (
            <button
              key={value}
              type="button"
              onClick={() => setTheme(value)}
              className="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1.5 text-xs transition-colors duration-100"
              style={{
                background: active ? 'var(--c-bg-deep)' : 'transparent',
                color: active ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                fontWeight: active ? 500 : 400,
              }}
            >
              <Icon size={13} />
              <span>{optLabel}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function InviteCodeContent({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const [inviteCode, setInviteCode] = useState<InviteCodeResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [copied, setCopied] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [error, setError] = useState('')
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    void (async () => {
      try {
        const code = await getMyInviteCode(accessToken)
        setInviteCode(code)
      } catch {
        setError(t.requestFailed)
      } finally {
        setLoading(false)
      }
    })()
  }, [accessToken, t.requestFailed])

  const handleCopy = useCallback(async () => {
    if (!inviteCode) return
    await navigator.clipboard.writeText(inviteCode.code)
    setCopied(true)
    if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
    copiedTimerRef.current = setTimeout(() => setCopied(false), 2000)
  }, [inviteCode])

  const handleReset = useCallback(async () => {
    setResetting(true)
    setError('')
    try {
      const code = await resetMyInviteCode(accessToken)
      setInviteCode(code)
    } catch (err) {
      if (isApiError(err) && err.code === 'invite_codes.reset_cooldown') {
        setError(t.inviteCodeResetCooldown)
      } else {
        setError(t.requestFailed)
      }
    } finally {
      setResetting(false)
    }
  }, [accessToken, t])

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Ticket size={15} className="text-[var(--c-text-heading)]" />
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.inviteCode}</span>
      </div>
      <p className="text-xs text-[var(--c-text-tertiary)]">{t.inviteCodeDesc}</p>

      {loading ? (
        <div className="flex h-9 w-[240px] items-center px-3 text-sm text-[var(--c-text-tertiary)]">
          ...
        </div>
      ) : inviteCode ? (
        <div className="flex flex-col gap-2">
          <div
            className="flex w-[360px] items-center gap-2 rounded-lg px-3 py-2"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            <span className="flex-1 font-mono text-sm font-medium tracking-wider text-[var(--c-text-heading)]">
              {inviteCode.code}
            </span>
            <span className="text-xs text-[var(--c-text-tertiary)]">
              {t.inviteCodeUses(inviteCode.use_count, inviteCode.max_uses)}
            </span>
            <button
              onClick={handleCopy}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
              title={copied ? t.inviteCodeCopied : t.inviteCodeCopy}
            >
              {copied ? <Check size={13} /> : <Copy size={13} />}
            </button>
            <button
              onClick={handleReset}
              disabled={resetting}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)] disabled:opacity-50"
              title={t.inviteCodeReset}
            >
              <RefreshCw size={13} className={resetting ? 'animate-spin' : ''} />
            </button>
          </div>
          {error && (
            <p className="text-xs text-[var(--c-status-error-text)]">{error}</p>
          )}
        </div>
      ) : error ? (
        <p className="text-xs text-[var(--c-status-error-text)]">{error}</p>
      ) : null}
    </div>
  )
}

function HelpContent({ label }: { label: string }) {
  return (
    <div className="flex flex-col gap-2">
      <a
        href="https://docs.arkloop.com"
        target="_blank"
        rel="noopener noreferrer"
        className="flex h-9 w-[240px] items-center gap-2 rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        <HelpCircle size={15} />
        <span>{label}</span>
        <ArrowUpRight size={12} style={{ marginLeft: 'auto' }} />
      </a>
    </div>
  )
}

function ReportFeedbackContent({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const [open, setOpen] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [success, setSuccess] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) {
      setFeedback('')
      setSubmitting(false)
      setSuccess(false)
      setError('')
    }
  }, [open])

  useEffect(() => {
    if (!success) return
    const timer = window.setTimeout(() => setOpen(false), 1400)
    return () => window.clearTimeout(timer)
  }, [success])

  const handleSubmit = useCallback(async () => {
    const content = feedback.trim()
    if (!content || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await createSuggestionFeedback(accessToken, content)
      setSuccess(true)
    } catch (err) {
      setError(isApiError(err) ? err.message : t.requestFailed)
    } finally {
      setSubmitting(false)
    }
  }, [accessToken, feedback, submitting, t.requestFailed])

  return (
    <>
      <div className="flex flex-col gap-2">
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="flex h-9 w-[240px] items-center gap-2 rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          <Flag size={15} />
          <span>{t.submitSuggestion}</span>
        </button>
      </div>

      {open && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center"
          style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
          onMouseDown={(e) => { if (e.target === e.currentTarget) setOpen(false) }}
        >
          <div
            className="modal-enter w-full max-w-lg rounded-2xl p-6"
            style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{t.suggestionTitle}</h3>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              >
                <X size={16} />
              </button>
            </div>

            <textarea
              value={feedback}
              onChange={(e) => setFeedback(e.target.value)}
              placeholder={t.suggestionPlaceholder}
              maxLength={2000}
              rows={5}
              disabled={submitting || success}
              className="w-full resize-none rounded-lg border px-3 py-2 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
              style={{ borderColor: 'var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            />

            <div className="mt-2 flex items-center justify-between">
              <span className="text-xs text-[var(--c-text-tertiary)]">{feedback.length}/2000</span>
              {error && <span className="text-xs text-[var(--c-status-error-text)]">{error}</span>}
              {!error && success && <span className="text-xs text-[var(--c-status-success-text,#22c55e)]">{t.suggestionSuccess}</span>}
            </div>

            <div className="mt-4 flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              >
                {t.reportCancel}
              </button>
              <button
                type="button"
                onClick={() => void handleSubmit()}
                disabled={submitting || success || !feedback.trim()}
                className="rounded-lg px-4 py-2 text-sm font-medium transition-colors disabled:opacity-50"
                style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
              >
                {submitting ? '...' : t.suggestionSubmit}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}

function CreditsContent({ accessToken, onCreditsChanged }: { accessToken: string; onCreditsChanged?: (balance: number) => void }) {
  const { t } = useLocale()
  const [balance, setBalance] = useState<number | null>(null)
  const [balanceLoading, setBalanceLoading] = useState(true)
  const [redeemInput, setRedeemInput] = useState('')
  const [redeemLoading, setRedeemLoading] = useState(false)
  const [redeemMsg, setRedeemMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [transactions, setTransactions] = useState<CreditTransaction[] | null>(null)
  const [monthlyTransactions, setMonthlyTransactions] = useState<CreditTransaction[] | null>(null)
  const [txLoading, setTxLoading] = useState(false)
  const [txError, setTxError] = useState('')
  const now = new Date()
  const [filterYear, setFilterYear] = useState(now.getFullYear())
  const [filterMonth, setFilterMonth] = useState(now.getMonth() + 1)

  useEffect(() => {
    void (async () => {
      try {
        const data = await getMyCredits(accessToken)
        setBalance(data.balance)
        setTransactions(data.transactions)
        onCreditsChanged?.(data.balance)
      } catch {
        setBalance(null)
      } finally {
        setBalanceLoading(false)
      }
    })()
  }, [accessToken, onCreditsChanged])

  const handleRedeem = useCallback(async () => {
    const code = redeemInput.trim()
    if (!code) return
    setRedeemLoading(true)
    setRedeemMsg(null)
    try {
      const res = await redeemCode(accessToken, code)
      setRedeemMsg({ ok: true, text: t.creditsRedeemSuccess(res.value) })
      setRedeemInput('')
      const updated = await getMyCredits(accessToken)
      setBalance(updated.balance)
      setTransactions(updated.transactions)
      onCreditsChanged?.(updated.balance)
    } catch {
      setRedeemMsg({ ok: false, text: t.creditsRedeemError(code) })
    } finally {
      setRedeemLoading(false)
    }
  }, [accessToken, onCreditsChanged, redeemInput, t])

  const handleQueryUsage = useCallback(async () => {
    setTxLoading(true)
    setTxError('')
    try {
      const from = `${filterYear}-${String(filterMonth).padStart(2, '0')}-01`
      // to = 下个月第一天（后端用 < 做过滤）
      const nextMonth = filterMonth === 12 ? 1 : filterMonth + 1
      const nextYear = filterMonth === 12 ? filterYear + 1 : filterYear
      const to = `${nextYear}-${String(nextMonth).padStart(2, '0')}-01`
      const data = await getMyCredits(accessToken, from, to)
      setMonthlyTransactions(data.transactions)
    } catch {
      setTxError(t.requestFailed)
    } finally {
      setTxLoading(false)
    }
  }, [accessToken, filterYear, filterMonth, t])

  return (
    <div className="flex flex-col gap-6">
      {/* 积分余额 */}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.creditsBalance}</span>
        <div
          className="flex h-12 w-[240px] items-center rounded-lg px-4"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          {balanceLoading ? (
            <span className="text-sm text-[var(--c-text-tertiary)]">...</span>
          ) : (
            <span className="text-xl font-semibold tabular-nums text-[var(--c-text-heading)]">
              {balance ?? '-'}
              <span className="ml-1.5 text-xs font-normal text-[var(--c-text-tertiary)]">
                {t.creditsBalanceUnit}
              </span>
            </span>
          )}
        </div>
      </div>

      {/* 兑换码 */}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.creditsRedeem}</span>
        <div className="flex gap-2">
          <input
            type="text"
            value={redeemInput}
            onChange={(e) => setRedeemInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleRedeem() }}
            placeholder={t.creditsRedeemPlaceholder}
            className="h-9 w-[240px] rounded-lg px-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            disabled={redeemLoading}
          />
          <button
            onClick={() => void handleRedeem()}
            disabled={redeemLoading || !redeemInput.trim()}
            className="flex h-9 items-center rounded-lg px-3 text-sm font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            {t.creditsRedeemBtn}
          </button>
        </div>
        {redeemMsg && (
          <p
            className="text-xs"
            style={{ color: redeemMsg.ok ? 'var(--c-status-success-text)' : 'var(--c-status-error-text)' }}
          >
            {redeemMsg.text}
          </p>
        )}
      </div>

      {/* 我的用量 */}
      <div className="flex flex-col gap-4">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.creditsUsage}</span>

        {/* 最近 */}
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{t.creditsHistoryRecent}</span>
          <CreditTransactionTable transactions={transactions} loading={balanceLoading} t={t} />
        </div>

        {/* 按月度查询 */}
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{t.creditsHistoryMonthly}</span>
          <div className="flex items-center gap-2">
            <select
              value={filterYear}
              onChange={(e) => setFilterYear(Number(e.target.value))}
              className="h-8 rounded-lg px-2 text-sm text-[var(--c-text-heading)] outline-none"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              {Array.from({ length: 3 }, (_, i) => now.getFullYear() - i).map(y => (
                <option key={y} value={y}>{y}</option>
              ))}
            </select>
            <select
              value={filterMonth}
              onChange={(e) => setFilterMonth(Number(e.target.value))}
              className="h-8 rounded-lg px-2 text-sm text-[var(--c-text-heading)] outline-none"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              {Array.from({ length: 12 }, (_, i) => i + 1).map(m => (
                <option key={m} value={m}>{new Date(2000, m - 1).toLocaleString(undefined, { month: 'long' })}</option>
              ))}
            </select>
            <button
              onClick={() => void handleQueryUsage()}
              disabled={txLoading}
              className="flex h-8 items-center rounded-lg px-3 text-sm font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              {t.creditsUsageQuery}
            </button>
          </div>
          {txError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{txError}</p>
          )}
          {monthlyTransactions !== null && (
            <CreditTransactionTable transactions={monthlyTransactions} loading={txLoading} t={t} />
          )}
        </div>
      </div>
    </div>
  )
}

type TableLocale = {
  creditsHistoryDetails: string
  creditsHistoryDate: string
  creditsHistoryCreditChange: string
  creditsHistoryEmpty: string
  creditsTxTypeLabel: (type: string) => string
}

function CreditTransactionTable({
  transactions,
  loading,
  t,
}: {
  transactions: CreditTransaction[] | null
  loading: boolean
  t: TableLocale
}) {
  if (loading) {
    return <p className="text-xs text-[var(--c-text-tertiary)]">...</p>
  }
  if (!transactions || transactions.length === 0) {
    return <p className="text-xs text-[var(--c-text-tertiary)]">{t.creditsHistoryEmpty}</p>
  }

  return (
    <div
      className="overflow-y-auto rounded-xl"
      style={{ border: '0.5px solid var(--c-border-subtle)', maxHeight: '320px' }}
    >
      <table className="w-full text-sm">
        <thead className="sticky top-0" style={{ background: 'var(--c-bg-page)', zIndex: 1 }}>
          <tr style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}>
            <th className="px-4 py-2 text-left text-xs font-medium text-[var(--c-text-tertiary)]">
              {t.creditsHistoryDetails}
            </th>
            <th className="px-4 py-2 text-left text-xs font-medium text-[var(--c-text-tertiary)] whitespace-nowrap">
              {t.creditsHistoryDate}
            </th>
            <th className="px-4 py-2 text-right text-xs font-medium text-[var(--c-text-tertiary)] whitespace-nowrap">
              {t.creditsHistoryCreditChange}
            </th>
          </tr>
        </thead>
        <tbody>
          {transactions.map((tx) => {
            const detail = tx.thread_title ?? tx.note ?? t.creditsTxTypeLabel(tx.type)
            const dateStr = new Date(tx.created_at).toLocaleDateString(undefined, {
              year: 'numeric',
              month: 'short',
              day: 'numeric',
            })
            const isPositive = tx.amount >= 0
            return (
              <tr
                key={tx.id}
                style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
              >
                <td
                  className="max-w-[240px] truncate px-4 py-2 text-[var(--c-text-heading)]"
                  title={detail}
                >
                  {detail}
                </td>
                <td className="whitespace-nowrap px-4 py-2 text-xs text-[var(--c-text-tertiary)]">
                  {dateStr}
                </td>
                <td
                  className="whitespace-nowrap px-4 py-2 text-right font-medium tabular-nums"
                  style={{ color: isPositive ? 'var(--c-status-success-text)' : 'var(--c-status-error-text)' }}
                >
                  {isPositive ? '+' : ''}{tx.amount}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
