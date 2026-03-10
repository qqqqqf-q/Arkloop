import { useState, useMemo, useEffect, useCallback, useRef, type FormEvent } from 'react'
import { login, isApiError, getCaptchaConfig, sendEmailOTP, verifyEmailOTP } from '../api'
import { ErrorCallout, type AppError } from '../components/ErrorCallout'
import { Turnstile } from '../components/Turnstile'
import { useLocale } from '../contexts/LocaleContext'

function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

function EyeIcon({ open }: { open: boolean }) {
  return open ? (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" />
    </svg>
  ) : (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-6 0-10-8-10-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c6 0 10 8 10 8a18.5 18.5 0 0 1-2.16 3.19M1 1l22 22" />
    </svg>
  )
}

function normalizeError(error: unknown, fallback: string): AppError {
  if (isApiError(error)) return { message: error.message, traceId: error.traceId, code: error.code }
  if (error instanceof Error) return { message: error.message }
  return { message: fallback }
}

function Reveal({ active, children }: { active: boolean; children: React.ReactNode }) {
  return (
    <div style={{
      display: 'grid',
      gridTemplateRows: active ? '1fr' : '0fr',
      opacity: active ? 1 : 0,
      transition: 'grid-template-rows 0.38s cubic-bezier(0.4,0,0.2,1), opacity 0.25s ease',
    }}>
      <div style={{ overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

type Phase = 'identity' | 'password' | 'otp-email' | 'otp-code'

type Props = {
  onLoggedIn: (accessToken: string) => void
}

const isEmailStr = (v: string) => v.includes('@')
const TRANSITION = '0.42s cubic-bezier(0.4,0,0.2,1)'

const inputCls = 'w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]'
const inputStyle = {
  border: '0.5px solid var(--c-border-auth)',
  height: '36px',
  padding: '0 14px',
  fontSize: '13px',
  fontWeight: 500,
  fontFamily: 'inherit',
} as const
const labelStyle = {
  fontSize: '11px',
  fontWeight: 500 as const,
  color: 'var(--c-placeholder)',
  paddingLeft: '2px',
  marginBottom: '4px',
  display: 'block',
} as const

interface PasswordEyeProps {
  inputRef: React.RefObject<HTMLInputElement | null>
  placeholder: string
  value: string
  onChange: (v: string) => void
  showPassword: boolean
  onToggleShow: () => void
}

function PasswordEye({ inputRef, placeholder, value, onChange, showPassword, onToggleShow }: PasswordEyeProps) {
  return (
    <div style={{ position: 'relative' }}>
      <input
        ref={inputRef}
        className={inputCls}
        style={{ ...inputStyle, paddingRight: '38px' }}
        type={showPassword ? 'text' : 'password'}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="current-password"
      />
      <button
        type="button"
        tabIndex={-1}
        onClick={onToggleShow}
        style={{ position: 'absolute', right: '10px', top: '50%', transform: 'translateY(-50%)', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--c-placeholder)', padding: '2px', display: 'flex', alignItems: 'center' }}
      >
        <EyeIcon open={showPassword} />
      </button>
    </div>
  )
}

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

  const [error, setError] = useState<AppError | null>(null)
  const [captchaSiteKey, setCaptchaSiteKey] = useState('')
  const [turnstileToken, setTurnstileToken] = useState('')

  const passwordRef = useRef<HTMLInputElement>(null)
  const otpEmailRef = useRef<HTMLInputElement>(null)
  const otpCodeRef = useRef<HTMLInputElement>(null)

  const { t } = useLocale()

  useEffect(() => {
    getCaptchaConfig()
      .then((res) => { if (res.enabled) setCaptchaSiteKey(res.site_key) })
      .catch(() => {})
  }, [])

  useEffect(() => () => { if (countdownRef.current) clearInterval(countdownRef.current) }, [])

  useEffect(() => {
    const delay = 420
    const refs: Record<string, React.RefObject<HTMLInputElement | null>> = {
      password: passwordRef,
      'otp-email': otpEmailRef,
      'otp-code': otpCodeRef,
    }
    const ref = refs[phase]
    if (!ref) return
    const timer = setTimeout(() => ref.current?.focus(), delay)
    return () => clearTimeout(timer)
  }, [phase])

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
    setTurnstileToken('')
  }

  const handleTurnstileSuccess = useCallback((token: string) => {
    setTurnstileToken(token)
  }, [])

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
    setError(null)
    if (isEmailStr(identity.trim())) {
      const email = identity.trim()
      setOtpEmail(email)
      setPhase('otp-code')
      startCountdown()
      sendEmailOTP(email).catch(() => {})
    } else {
      setOtpEmail('')
      setOtpCode('')
      setPhase('otp-email')
    }
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)

    if (phase === 'identity') {
      const id = identity.trim()
      if (!id) return
      setChecking(true)
      try {
        setMaskedEmail('')
        setPhase('password')
      } finally {
        setChecking(false)
      }
      return
    }

    if (phase === 'password') {
      if (!password) return
      setSubmitting(true)
      try {
        const resp = await login({ login: identity.trim(), password, cf_turnstile_token: captchaSiteKey ? turnstileToken : undefined })
        onLoggedIn(resp.access_token)
      } catch (err) {
        setTurnstileToken('')
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
      try { await sendEmailOTP(email, captchaSiteKey ? turnstileToken : undefined) } catch { /* noop */ } finally {
        setOtpSending(false)
        setTurnstileToken('')
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
        onLoggedIn(resp.access_token)
      } catch (err) {
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setOtpSubmitting(false)
      }
    }
  }

  const isLoading = checking || submitting || otpSending || otpSubmitting

  const canSubmit = useMemo(() => {
    if (isLoading) return false
    const captchaOk = !captchaSiteKey || !!turnstileToken
    if (phase === 'identity') return identity.trim().length > 0
    if (phase === 'password') return password.length > 0 && captchaOk
    if (phase === 'otp-email') return otpEmail.trim().length > 0 && captchaOk
    if (phase === 'otp-code') return otpEmail.trim().length > 0 && otpCode.length === 6
    return false
  }, [phase, identity, password, otpEmail, otpCode, isLoading, captchaSiteKey, turnstileToken])

  const btnLabel = useMemo(() => {
    if (phase === 'otp-email') return t.otpSendBtn
    if (phase === 'otp-code') return t.otpVerifyBtn
    return t.continueBtn
  }, [phase, t])

  const phaseSubtitles: Partial<Record<Phase, string>> = {
    password: t.enterYourPasswordTitle,
    'otp-email': t.otpLoginTab,
    'otp-code': t.otpLoginTab,
  }

  return (
    <div style={{ minHeight: '100vh', background: 'var(--c-bg-page)', display: 'flex', flexDirection: 'column' as const, position: 'relative' as const, overflow: 'hidden' }}>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      <div
        style={{ flex: 1, display: 'flex', flexDirection: 'column' as const, alignItems: 'center', padding: 'max(48px, calc(50vh - 140px)) 20px 48px', position: 'relative', zIndex: 1 }}
      >
        <section style={{ width: 'min(400px, 100%)' }}>

          <div style={{ height: '64px', marginBottom: '20px' }}>
            <div style={{
              display: 'block',
              width: 'fit-content',
              position: 'relative',
              left: phase === 'identity' ? '50%' : '0',
              transform: phase === 'identity' ? 'translateX(-50%)' : 'translateX(0)',
              transition: `left ${TRANSITION}, transform ${TRANSITION}`,
              fontSize: '28px',
              fontWeight: 500,
              color: 'var(--c-text-primary)',
              lineHeight: 1,
            }}>
              Arkloop
            </div>

            <div style={{ position: 'relative', height: '22px', marginTop: '8px' }}>
              <div style={{
                position: 'absolute', width: '100%', textAlign: 'center',
                fontSize: '15px', fontWeight: 500, color: 'var(--c-placeholder)',
                opacity: phase === 'identity' ? 1 : 0,
                transition: 'opacity 0.2s ease',
                pointerEvents: 'none',
                userSelect: 'none',
              }}>
                {t.loginMode}
              </div>
              <div style={{
                position: 'absolute', left: 0, top: 0,
                fontSize: '13px', fontWeight: 500, color: 'var(--c-placeholder)',
                opacity: phase !== 'identity' ? 1 : 0,
                transform: phase !== 'identity' ? 'translateY(0)' : 'translateY(3px)',
                transition: 'opacity 0.25s ease 0.12s, transform 0.25s ease 0.12s',
                pointerEvents: 'none',
                userSelect: 'none',
                whiteSpace: 'nowrap',
              }}>
                {phaseSubtitles[phase] ?? ''}
              </div>
            </div>

            <Reveal active={phase === 'identity'}>
              <div style={{ textAlign: 'center', fontSize: '12px', fontWeight: 500, color: 'var(--c-placeholder)', letterSpacing: '0.1em', textTransform: 'uppercase' as const, marginTop: '2px' }}>
                Lite
              </div>
            </Reveal>
          </div>

          <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column' as const }}>

            <div style={{
              height: '18px',
              opacity: phase !== 'identity' ? 1 : 0,
              transition: `opacity ${TRANSITION}`,
              ...labelStyle,
            }}>
              {t.fieldIdentity}
            </div>

            {phase === 'identity' ? (
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
            ) : (
              <div
                className={inputCls}
                style={{ ...inputStyle, borderRadius: '10px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: 'var(--c-text-secondary)' }}
              >
                <span>{identity.trim()}</span>
                <button
                  type="button"
                  onClick={resetToIdentity}
                  style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#3b82f6', fontSize: '12px', fontWeight: 500, padding: '0 2px', flexShrink: 0 }}
                >
                  {t.editIdentity}
                </button>
              </div>
            )}

            <Reveal active={phase === 'password'}>
              <div style={{ paddingTop: '10px' }}>
                <label style={labelStyle}>{t.fieldPassword}</label>
                <PasswordEye
                  inputRef={passwordRef}
                  placeholder={t.enterPassword}
                  value={password}
                  onChange={setPassword}
                  showPassword={showPassword}
                  onToggleShow={() => setShowPassword((v) => !v)}
                />
              </div>
            </Reveal>

            <Reveal active={phase === 'otp-email' || (phase === 'otp-code' && !isEmailStr(identity.trim()))}>
              <div style={{ paddingTop: '10px' }}>
                <label style={labelStyle}>{t.otpEmailPlaceholder}</label>
                <input
                  ref={otpEmailRef}
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
                />
                {maskedEmail && phase === 'otp-email' && (
                  <div style={{ fontSize: '11px', color: 'var(--c-placeholder)', marginTop: '4px', paddingLeft: '2px' }}>
                    {maskedEmail}
                  </div>
                )}
              </div>
            </Reveal>

            <Reveal active={phase === 'otp-code'}>
              <div style={{ paddingTop: '10px' }}>
                <label style={labelStyle}>{t.otpCodePlaceholder}</label>
                <input
                  ref={otpCodeRef}
                  className={inputCls}
                  style={inputStyle}
                  type="text"
                  inputMode="numeric"
                  placeholder={t.otpCodePlaceholder}
                  value={otpCode}
                  onChange={(e) => setOtpCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  autoComplete="one-time-code"
                />
              </div>
            </Reveal>

            {captchaSiteKey && (phase === 'password' || phase === 'otp-email') && (
              <div style={{ marginTop: '12px' }}>
                <Turnstile
                  siteKey={captchaSiteKey}
                  onSuccess={handleTurnstileSuccess}
                  onExpire={() => setTurnstileToken('')}
                />
              </div>
            )}

            <button
              type="submit"
              disabled={!canSubmit}
              style={{
                height: '38px',
                marginTop: '12px',
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

            <Reveal active={phase !== 'identity'}>
              <button
                type="button"
                onClick={resetToIdentity}
                style={{
                  height: '38px',
                  marginTop: '4px',
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
            </Reveal>

          </form>

          <Reveal active={phase === 'password'}>
            <button
              type="button"
              onClick={switchToOtp}
              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
            >
              {t.useEmailOtpHint}
            </button>
          </Reveal>

          <Reveal active={phase === 'otp-code'}>
            <button
              type="button"
              disabled={otpCountdown > 0 || otpSending}
              onClick={async () => {
                const email = otpEmail.trim()
                if (!email) return
                setOtpSending(true)
                try { await sendEmailOTP(email) } catch { /* noop */ } finally {
                  setOtpSending(false)
                  startCountdown()
                }
              }}
              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
              className="disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {otpCountdown > 0 ? t.otpSendingCountdown(otpCountdown) : t.otpSendBtn}
            </button>
          </Reveal>

          {error && <ErrorCallout error={error} />}
        </section>
      </div>

      <footer style={{ textAlign: 'center', padding: '16px', fontSize: '12px', color: 'var(--c-text-muted)', position: 'relative', zIndex: 1 }}>
        &copy; 2026 Arkloop
      </footer>
    </div>
  )
}
