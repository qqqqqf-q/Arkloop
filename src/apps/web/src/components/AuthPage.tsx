import { useState, useMemo, useEffect, type FormEvent } from 'react'
import { login, register, getRegistrationMode, isApiError } from '../api'
import type { RegistrationModeResponse } from '../api'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { useLocale } from '../contexts/LocaleContext'

function GitHubIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
    </svg>
  )
}

function normalizeError(error: unknown, fallback: string): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: fallback }
}

type Props = {
  onLoggedIn: (accessToken: string, refreshToken: string) => void
}

export function AuthPage({ onLoggedIn }: Props) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [inviteCode, setInviteCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const [registrationMode, setRegistrationMode] = useState<RegistrationModeResponse['mode']>('invite_only')
  const { t } = useLocale()

  useEffect(() => {
    getRegistrationMode()
      .then((res) => setRegistrationMode(res.mode))
      .catch(() => {})
  }, [])

  const inviteRequired = registrationMode === 'invite_only'

  const canSubmit = useMemo(() => {
    if (submitting) return false
    if (!loginValue.trim() || !password) return false
    if (mode === 'register' && (!displayName.trim() || password.length < 8)) return false
    if (mode === 'register' && inviteRequired && !inviteCode.trim()) return false
    return true
  }, [loginValue, password, displayName, inviteCode, submitting, mode, inviteRequired])

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      if (mode === 'login') {
        const resp = await login({ login: loginValue, password })
        onLoggedIn(resp.access_token, resp.refresh_token)
      } else {
        const resp = await register({
          login: loginValue,
          password,
          display_name: displayName,
          ...(inviteCode.trim() ? { invite_code: inviteCode.trim() } : {}),
        })
        onLoggedIn(resp.access_token, resp.refresh_token)
      }
    } catch (err) {
      setError(normalizeError(err, t.requestFailed))
    } finally {
      setSubmitting(false)
    }
  }

  const inputStyle = {
    border: '0.5px solid var(--c-border-auth)',
    height: '42px',
    padding: '0 14px',
    fontSize: '13px',
    fontWeight: 500,
    fontFamily: 'inherit',
  }

  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center px-5"
      style={{ background: 'var(--c-bg-deep)', padding: '48px 20px', gap: '32px' }}
    >
      <header className="flex flex-col items-center" style={{ gap: '8px' }}>
        <div style={{ fontSize: '28px', fontWeight: 500, color: 'var(--c-text-primary)' }}>Arkloop</div>
        <div style={{ fontSize: '15px', fontWeight: 500, color: 'var(--c-placeholder)' }}>
          {mode === 'login' ? t.loginMode : t.registerMode}
        </div>
      </header>

      <section
        style={{
          width: 'min(400px, 100%)',
          borderRadius: '20px',
          padding: '28px 32px',
          background: 'var(--c-bg-deep)',
          border: '0.5px solid var(--c-border-auth)',
        }}
      >
        <form className="flex flex-col" style={{ gap: '12px' }} onSubmit={onSubmit}>
          {mode === 'register' && (
            <input
              className="w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]"
              style={inputStyle}
              type="text"
              placeholder={t.enterDisplayName}
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              autoComplete="name"
            />
          )}

          <input
            className="w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]"
            style={inputStyle}
            type="text"
            placeholder={t.enterUsername}
            value={loginValue}
            onChange={(e) => setLoginValue(e.target.value)}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
          />

          <input
            className="w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]"
            style={inputStyle}
            type="password"
            placeholder={t.enterPassword}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />

          {mode === 'register' && (
            <input
              className="w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]"
              style={inputStyle}
              type="text"
              placeholder={t.enterInviteCode}
              value={inviteCode}
              onChange={(e) => setInviteCode(e.target.value)}
              autoComplete="off"
              required={inviteRequired}
            />
          )}

          <button
            type="submit"
            disabled={!canSubmit}
            style={{
              height: '44px',
              marginTop: '4px',
              borderRadius: '10px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '14px',
              fontWeight: 500,
              fontFamily: 'inherit',
              background: 'var(--c-btn-bg)',
              color: 'var(--c-btn-text)',
            }}
            className="disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? '...' : t.continueBtn}
          </button>
        </form>

        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', margin: '16px 0' }}>
          <div style={{ flex: 1, height: '0.5px', background: 'var(--c-border-auth)' }} />
          <span style={{ fontSize: '11px', color: 'var(--c-placeholder)', fontWeight: 500 }}>{t.orDivider}</span>
          <div style={{ flex: 1, height: '0.5px', background: 'var(--c-border-auth)' }} />
        </div>

        <button
          type="button"
          style={{
            width: '100%',
            height: '48px',
            borderRadius: '10px',
            border: '0.5px solid var(--c-border-auth)',
            cursor: 'default',
            fontSize: '13px',
            fontWeight: 500,
            fontFamily: 'inherit',
            background: 'var(--c-bg-github-btn)',
            color: 'var(--c-text-primary)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: '8px',
          }}
        >
          <GitHubIcon />
          {t.githubLogin}
        </button>

        {error && <ErrorCallout error={error} />}
      </section>

      <button
        type="button"
        onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(null); setInviteCode('') }}
        style={{ fontSize: '13px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer' }}
      >
        {mode === 'login' ? t.noAccount : t.hasAccount}
      </button>
    </div>
  )
}
