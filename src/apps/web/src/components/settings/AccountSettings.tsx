import { useState, useCallback, useEffect } from 'react'
import { LogOut, Pencil } from 'lucide-react'
import {
  type MeResponse,
  updateMe,
  sendEmailVerification,
  confirmEmailVerification,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { useToast } from '@arkloop/shared'
import { CopyIconButton } from '../CopyIconButton'
import { SettingsButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'

export function AccountContent({
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

export function ProfileContent({
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
  const { addToast } = useToast()
  const [displayName, setDisplayName] = useState(me?.username ?? '')
  const [saving, setSaving] = useState(false)
  const [sendingVerify, setSendingVerify] = useState(false)
  const [verifySent, setVerifySent] = useState(false)
  const [verifyCode, setVerifyCode] = useState('')
  const [verifying, setVerifying] = useState(false)
  const [verifyError, setVerifyError] = useState('')

  const isDirty = displayName.trim() !== (me?.username ?? '')

  const handleSave = useCallback(async () => {
    const name = displayName.trim()
    if (!name || !isDirty) return
    setSaving(true)
    try {
      const res = await updateMe(accessToken, { username: name })
      if (me && onMeUpdated) {
        onMeUpdated({ ...me, username: res.username })
      }
      addToast(t.profileSaved, 'success')
    } catch {
      addToast(t.requestFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, displayName, isDirty, me, onMeUpdated, t, addToast])

  useEffect(() => {
    if (!isDirty) return
    const timer = setTimeout(() => void handleSave(), 500)
    return () => clearTimeout(timer)
  }, [displayName, isDirty, handleSave])

  const handleCopyId = useCallback(async () => {
    if (!me?.id) return
    await navigator.clipboard.writeText(me.id)
  }, [me?.id])

  const handleSendVerify = useCallback(async () => {
    setSendingVerify(true)
    setVerifyError('')
    try {
      await sendEmailVerification(accessToken)
      setVerifySent(true)
    } catch {
      // silent
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
      {/* avatar + name edit */}
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
          </div>
        </div>
      </div>

      <div style={{ height: '0.5px', background: 'var(--c-border-subtle)' }} />

      {/* username */}
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.profileUsername}</span>
        <span className="text-sm text-[var(--c-text-tertiary)]">{me?.username ?? '\u2014'}</span>
      </div>

      {/* user id */}
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.profileUserId}</span>
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-[var(--c-text-tertiary)] select-all">{me?.id ?? '\u2014'}</span>
          {me?.id && (
            <CopyIconButton
              onCopy={() => void handleCopyId()}
              size={12}
              tooltip={t.copyAction}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            />
          )}
        </div>
      </div>

      {/* email */}
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
                      <SettingsInput
                        type="text"
                        variant="sm"
                        inputMode="numeric"
                        maxLength={6}
                        value={verifyCode}
                        onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, ''))}
                        placeholder={t.emailVerifyCodePlaceholder}
                        className="w-28"
                        style={{ letterSpacing: verifyCode ? '4px' : 'normal' }}
                      />
                      <SettingsButton
                        variant="primary"
                        onClick={() => void handleConfirmVerify()}
                        disabled={verifyCode.length !== 6 || verifying}
                        className="text-xs"
                      >
                        {verifying ? '...' : t.emailVerifyConfirmBtn}
                      </SettingsButton>
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
