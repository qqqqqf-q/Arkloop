import { useState, useMemo, useEffect, useRef, type FormEvent } from 'react'
import { login, register, getRegistrationMode, isApiError, sendEmailOTP, verifyEmailOTP, checkUser } from '../api'
import type { RegistrationModeResponse } from '../api'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { useLocale } from '../contexts/LocaleContext'

function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

function GitHubIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
    </svg>
  )
}

function EyeIcon({ open }: { open: boolean }) {
  return open ? (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  ) : (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-6 0-10-8-10-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c6 0 10 8 10 8a18.5 18.5 0 0 1-2.16 3.19M1 1l22 22" />
    </svg>
  )
}

function normalizeError(error: unknown, fallback: string): AppError {
  if (isApiError(error)) return { message: error.message, traceId: error.traceId, code: error.code }
  if (error instanceof Error) return { message: error.message }
  return { message: fallback }
}

type Phase = 'identity' | 'password' | 'otp-email' | 'otp-code' | 'register'

type Props = {
  onLoggedIn: (accessToken: string, refreshToken: string) => void
}

const isEmailStr = (v: string) => v.includes('@')

export function AuthPage({ onLoggedIn }: Props) {
  const [identity, setIdentity] = useState('')
  const [phase, setPhase] = useState<Phase>('identity')
  const [maskedEmail, setMaskedEmail] = useState('')
  const [checking, setChecking] = useState(false)

  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const [otpEmail, setOtpEmail] = useState('')
  const [otpCode, setOtpCode] = useState('')
  const [otpCountdown, setOtpCountdown] = useState(0)
  const [otpSending, setOtpSending] = useState(false)
  const [otpSubmitting, setOtpSubmitting] = useState(false)
  const countdownRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const [regLogin, setRegLogin] = useState('')
  const [regEmail, setRegEmail] = useState('')
  const [regPassword, setRegPassword] = useState('')
  const [regInviteCode, setRegInviteCode] = useState('')
  const [regSubmitting, setRegSubmitting] = useState(false)

  const [error, setError] = useState<AppError | null>(null)
  const [registrationMode, setRegistrationMode] = useState<RegistrationModeResponse['mode']>('invite_only')

  const { t } = useLocale()

  useEffect(() => {
    const root = document.documentElement
    const prev = root.getAttribute('data-theme')
    root.removeAttribute('data-theme')
    return () => { if (prev) root.setAttribute('data-theme', prev) }
  }, [])

  useEffect(() => {
    getRegistrationMode().then((res) => setRegistrationMode(res.mode)).catch(() => {})
  }, [])

  useEffect(() => () => { if (countdownRef.current) clearInterval(countdownRef.current) }, [])

  const inviteRequired = registrationMode === 'invite_only'

  const resetToIdentity = () => {
    setPhase('identity')
    setPassword('')
    setShowPassword(false)
    setOtpEmail('')
    setOtpCode('')
    setOtpCountdown(0)
    if (countdownRef.current) clearInterval(countdownRef.current)
    setMaskedEmail('')
    setError(null)
  }

  const startCountdown = () => {
    setOtpCountdown(60)
    if (countdownRef.current) clearInterval(countdownRef.current)
    countdownRef.current = setInterval(() => {
      setOtpCountdown((c) => {
        if (c <= 1) { clearInterval(countdownRef.current!); return 0 }
        return c - 1
      })
    }, 1000)
  }

  const switchToOtp = () => {
    setOtpEmail(isEmailStr(identity.trim()) ? identity.trim() : '')
    setOtpCode('')
    setPhase('otp-email')
    setError(null)
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)

    if (phase === 'identity') {
      const id = identity.trim()
      if (!id) return
      setChecking(true)
      try {
        const res = await checkUser(id)
        if (res.exists) {
          setMaskedEmail(res.masked_email ?? '')
          setPhase('password')
        } else {
          if (isEmailStr(id)) { setRegEmail(id); setRegLogin(id.split('@')[0]) }
          else { setRegLogin(id); setRegEmail('') }
          setRegPassword(''); setRegInviteCode('')
          setPhase('register')
        }
      } catch (err) {
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setChecking(false)
      }
      return
    }

    if (phase === 'password') {
      if (!password) return
      setSubmitting(true)
      try {
        const resp = await login({ login: identity.trim(), password })
        onLoggedIn(resp.access_token, resp.refresh_token)
      } catch (err) {
        if (isApiError(err) && err.code === 'auth.email_not_verified') { switchToOtp(); return }
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setSubmitting(false)
      }
      return
    }

    if (phase === 'otp-email') {
      const email = otpEmail.trim()
      if (!email) return
      setOtpSending(true)
      try { await sendEmailOTP(email) } catch { /* 静默 */ } finally {
        setOtpSending(false)
        setPhase('otp-code')
        startCountdown()
      }
      return
    }

    if (phase === 'otp-code') {
      const email = otpEmail.trim()
      const code = otpCode.trim()
      if (!email || code.length !== 6) return
      setOtpSubmitting(true)
      try {
        const resp = await verifyEmailOTP(email, code)
        onLoggedIn(resp.access_token, resp.refresh_token)
      } catch (err) {
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setOtpSubmitting(false)
      }
      return
    }

    if (phase === 'register') {
      setRegSubmitting(true)
      try {
        const resp = await register({
          login: regLogin.trim(),
          password: regPassword,
          email: regEmail.trim(),
          ...(regInviteCode.trim() ? { invite_code: regInviteCode.trim() } : {}),
        })
        onLoggedIn(resp.access_token, resp.refresh_token)
      } catch (err) {
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setRegSubmitting(false)
      }
    }
  }

  const isLoading = checking || submitting || otpSending || otpSubmitting || regSubmitting

  const canSubmit = useMemo(() => {
    if (isLoading) return false
    if (phase === 'identity') return identity.trim().length > 0
    if (phase === 'password') return password.length > 0
    if (phase === 'otp-email') return otpEmail.trim().length > 0
    if (phase === 'otp-code') return otpEmail.trim().length > 0 && otpCode.length === 6
    if (phase === 'register') {
      if (!regLogin.trim() || !regEmail.trim() || regPassword.length < 8) return false
      if (inviteRequired && !regInviteCode.trim()) return false
      return true
    }
    return false
  }, [phase, identity, password, otpEmail, otpCode, regLogin, regEmail, regPassword, regInviteCode, inviteRequired, isLoading])

  const btnLabel = useMemo(() => {
    if (phase === 'otp-email') return t.otpSendBtn
    if (phase === 'otp-code') return t.otpVerifyBtn
    return t.continueBtn
  }, [phase, t])

  const phaseSubtitle: Partial<Record<Phase, string>> = {
    password: t.enterYourPasswordTitle,
    'otp-email': t.otpLoginTab,
    'otp-code': t.otpLoginTab,
    register: t.registerMode,
  }

  const inputStyle = {
    border: '0.5px solid var(--c-border-auth)',
    height: '36px',
    padding: '0 14px',
    fontSize: '13px',
    fontWeight: 500,
    fontFamily: 'inherit',
  } as const
  const inputCls = 'w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]'

  const FieldLabel = ({ text }: { text: string }) => (
    <div style={{ fontSize: '11px', fontWeight: 500, color: 'var(--c-placeholder)', marginBottom: '4px', paddingLeft: '2px' }}>
      {text}
    </div>
  )

  // Continue / Submit 按钮（全宽）
  const SubmitBtn = () => (
    <button
      type="submit"
      disabled={!canSubmit}
      style={{
        height: '38px',
        borderRadius: '10px',
        border: 'none',
        cursor: 'pointer',
        fontSize: '14px',
        fontWeight: 500,
        fontFamily: 'inherit',
        background: 'var(--c-btn-bg)',
        color: 'var(--c-btn-text)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: '6px',
        width: '100%',
      }}
      className="disabled:cursor-not-allowed disabled:opacity-50"
    >
      {isLoading ? <><SpinnerIcon />{btnLabel}</> : btnLabel}
    </button>
  )

  // 返回按钮（全宽 ghost，与 Continue 同高）
  const BackBtn = () => (
    <button
      type="button"
      onClick={resetToIdentity}
      style={{
        height: '38px',
        width: '100%',
        borderRadius: '10px',
        border: 'none',
        background: 'transparent',
        cursor: 'pointer',
        fontSize: '14px',
        fontWeight: 500,
        fontFamily: 'inherit',
        color: 'var(--c-placeholder)',
      }}
    >
      {t.backBtn}
    </button>
  )

  return (
    <div
      style={{
        minHeight: '100vh',
        background: 'var(--c-bg-page)',
        display: 'flex',
        flexDirection: 'column' as const,
        position: 'relative' as const,
        overflow: 'hidden',
      }}
    >
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      <div
        className="flex flex-col items-center justify-center"
        style={{ flex: 1, padding: '48px 20px', position: 'relative', zIndex: 1 }}
      >
        {/* identity 阶段：居中 header */}
        {phase === 'identity' && (
          <header className="flex flex-col items-center" style={{ gap: '8px', marginBottom: '32px' }}>
            <div style={{ fontSize: '28px', fontWeight: 500, color: 'var(--c-text-primary)' }}>Arkloop</div>
            <div style={{ fontSize: '15px', fontWeight: 500, color: 'var(--c-placeholder)' }}>{t.loginMode}</div>
          </header>
        )}

        <section style={{ width: 'min(400px, 100%)' }}>
          {/* 非 identity 阶段：左对齐 header 放在 section 内 */}
          {phase !== 'identity' && (
            <div style={{ marginBottom: '24px' }}>
              <div style={{ fontSize: '26px', fontWeight: 500, color: 'var(--c-text-primary)' }}>Arkloop</div>
              <div style={{ fontSize: '13px', fontWeight: 500, color: 'var(--c-placeholder)', marginTop: '4px' }}>
                {phaseSubtitle[phase]}
              </div>
            </div>
          )}

          {phase === 'register' && (
            <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginBottom: '14px' }}>
              {t.creatingAccountHint}
            </div>
          )}

          <form className="flex flex-col" style={{ gap: '10px' }} onSubmit={handleSubmit}>

            {/* identity 阶段：可编辑输入框 */}
            {phase === 'identity' && (
              <input
                className={inputCls}
                style={inputStyle}
                type="text"
                placeholder={t.identityPlaceholder}
                value={identity}
                onChange={(e) => setIdentity(e.target.value)}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                autoFocus
              />
            )}

            {/* 密码阶段：静态 identity 显示 + 密码框 */}
            {phase === 'password' && (
              <>
                <div>
                  <FieldLabel text={t.fieldIdentity} />
                  <div
                    className={inputCls}
                    style={{
                      ...inputStyle,
                      borderRadius: '10px',
                      display: 'flex',
                      alignItems: 'center',
                      color: 'var(--c-text-secondary)',
                    }}
                  >
                    {identity.trim()}
                  </div>
                </div>
                <div>
                  <FieldLabel text={t.fieldPassword} />
                  <div style={{ position: 'relative' }}>
                    <input
                      className={inputCls}
                      style={{ ...inputStyle, paddingRight: '40px' }}
                      type={showPassword ? 'text' : 'password'}
                      placeholder={t.enterPassword}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      autoComplete="current-password"
                      autoFocus
                    />
                    <button
                      type="button"
                      tabIndex={-1}
                      onClick={() => setShowPassword((v) => !v)}
                      style={{
                        position: 'absolute',
                        right: '10px',
                        top: '50%',
                        transform: 'translateY(-50%)',
                        background: 'none',
                        border: 'none',
                        cursor: 'pointer',
                        color: 'var(--c-placeholder)',
                        padding: '2px',
                        display: 'flex',
                        alignItems: 'center',
                      }}
                    >
                      <EyeIcon open={showPassword} />
                    </button>
                  </div>
                </div>
              </>
            )}

            {/* OTP 邮箱输入 */}
            {(phase === 'otp-email' || phase === 'otp-code') && (
              <div>
                <FieldLabel text={t.otpEmailPlaceholder} />
                <input
                  className={inputCls}
                  style={inputStyle}
                  type="email"
                  placeholder={t.otpEmailPlaceholder}
                  value={otpEmail}
                  onChange={(e) => setOtpEmail(e.target.value)}
                  disabled={phase === 'otp-code'}
                  autoComplete="email"
                  autoCapitalize="none"
                  spellCheck={false}
                  autoFocus={phase === 'otp-email' && !otpEmail}
                />
                {/* 脱敏邮箱提示：仅在 otp-email 阶段且有 maskedEmail 时展示 */}
                {maskedEmail && phase === 'otp-email' && (
                  <div style={{ fontSize: '11px', color: 'var(--c-placeholder)', marginTop: '4px', paddingLeft: '2px' }}>
                    {maskedEmail}
                  </div>
                )}
              </div>
            )}

            {/* OTP 验证码输入 */}
            {phase === 'otp-code' && (
              <div>
                <FieldLabel text={t.otpCodePlaceholder} />
                <input
                  className={inputCls}
                  style={inputStyle}
                  type="text"
                  inputMode="numeric"
                  placeholder={t.otpCodePlaceholder}
                  value={otpCode}
                  onChange={(e) => setOtpCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  autoComplete="one-time-code"
                  autoFocus
                />
              </div>
            )}

            {/* 注册字段 */}
            {phase === 'register' && (
              <>
                {isEmailStr(identity.trim()) ? (
                  <div>
                    <FieldLabel text={t.enterUsername} />
                    <input
                      className={inputCls}
                      style={inputStyle}
                      type="text"
                      placeholder={t.enterUsername}
                      value={regLogin}
                      onChange={(e) => setRegLogin(e.target.value)}
                      autoComplete="username"
                      autoCapitalize="none"
                      spellCheck={false}
                      autoFocus
                    />
                  </div>
                ) : (
                  <div>
                    <FieldLabel text={t.enterEmail} />
                    <input
                      className={inputCls}
                      style={inputStyle}
                      type="email"
                      placeholder={t.enterEmail}
                      value={regEmail}
                      onChange={(e) => setRegEmail(e.target.value)}
                      autoComplete="email"
                      autoCapitalize="none"
                      spellCheck={false}
                      autoFocus
                    />
                  </div>
                )}
                <div>
                  <FieldLabel text={t.fieldPassword} />
                  <div style={{ position: 'relative' }}>
                    <input
                      className={inputCls}
                      style={{ ...inputStyle, paddingRight: '40px' }}
                      type={showPassword ? 'text' : 'password'}
                      placeholder={t.enterPassword}
                      value={regPassword}
                      onChange={(e) => setRegPassword(e.target.value)}
                      autoComplete="new-password"
                    />
                    <button
                      type="button"
                      tabIndex={-1}
                      onClick={() => setShowPassword((v) => !v)}
                      style={{
                        position: 'absolute',
                        right: '10px',
                        top: '50%',
                        transform: 'translateY(-50%)',
                        background: 'none',
                        border: 'none',
                        cursor: 'pointer',
                        color: 'var(--c-placeholder)',
                        padding: '2px',
                        display: 'flex',
                        alignItems: 'center',
                      }}
                    >
                      <EyeIcon open={showPassword} />
                    </button>
                  </div>
                </div>
                <div>
                  <FieldLabel text={inviteRequired ? t.enterInviteCode : t.enterInviteCodeOptional} />
                  <input
                    className={inputCls}
                    style={inputStyle}
                    type="text"
                    placeholder={inviteRequired ? t.enterInviteCode : t.enterInviteCodeOptional}
                    value={regInviteCode}
                    onChange={(e) => setRegInviteCode(e.target.value)}
                    autoComplete="off"
                  />
                </div>
              </>
            )}

            <SubmitBtn />
            {phase !== 'identity' && <BackBtn />}
          </form>

          {/* 密码阶段：OTP 跳转提示（在表单外，Back 按钮下方） */}
          {phase === 'password' && (
            <button
              type="button"
              onClick={switchToOtp}
              style={{
                marginTop: '2px',
                fontSize: '12px',
                color: 'var(--c-placeholder)',
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                padding: '4px 0',
                display: 'block',
              }}
            >
              {t.useEmailOtpHint}
            </button>
          )}

          {/* OTP code 阶段：重发（只在这里显示，不重复） */}
          {phase === 'otp-code' && (
            <button
              type="button"
              disabled={otpCountdown > 0 || otpSending}
              onClick={async () => {
                const email = otpEmail.trim()
                if (!email) return
                setOtpSending(true)
                try { await sendEmailOTP(email) } catch { /* 静默 */ } finally {
                  setOtpSending(false)
                  startCountdown()
                }
              }}
              style={{
                marginTop: '2px',
                fontSize: '12px',
                color: 'var(--c-placeholder)',
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                padding: '4px 0',
                display: 'block',
              }}
              className="disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {otpCountdown > 0 ? t.otpSendingCountdown(otpCountdown) : t.otpSendBtn}
            </button>
          )}

          {/* identity 阶段：GitHub 登录 */}
          {phase === 'identity' && (
            <>
              <div style={{ display: 'flex', alignItems: 'center', gap: '10px', margin: '16px 0' }}>
                <div style={{ flex: 1, height: '0.5px', background: 'var(--c-border-auth)' }} />
                <span style={{ fontSize: '11px', color: 'var(--c-placeholder)', fontWeight: 500 }}>{t.orDivider}</span>
                <div style={{ flex: 1, height: '0.5px', background: 'var(--c-border-auth)' }} />
              </div>
              <button type="button" className="github-btn">
                <GitHubIcon />
                {t.githubLogin}
              </button>
            </>
          )}

          {error && <ErrorCallout error={error} />}
        </section>
      </div>

      <footer
        style={{
          textAlign: 'center',
          padding: '16px',
          fontSize: '12px',
          color: 'var(--c-text-muted)',
          position: 'relative',
          zIndex: 1,
        }}
      >
        © 2026 Arkloop
      </footer>
    </div>
  )
}
