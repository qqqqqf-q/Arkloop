import { useState, useMemo, useEffect, useCallback, useRef, type FormEvent } from 'react'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { isApiError } from '../api/client'
import { Turnstile } from './Turnstile'
import {
  SpinnerIcon, normalizeError, Reveal, PasswordEye, AuthLayout,
  TRANSITION, inputCls, inputStyle, labelStyle,
} from './auth-ui'
import type { Locale } from '../contexts/LocaleContext'
import type { LoginRequest, LoginResponse } from '../api/types'

type Phase = 'identity' | 'password' | 'otp-email' | 'otp-code'

export type AuthPageTranslations = {
  requestFailed: string
  loginMode: string
  enterYourPasswordTitle: string
  otpLoginTab: string
  continueBtn: string
  otpSendBtn: string
  otpVerifyBtn: string
  backBtn: string
  fieldIdentity: string
  identityPlaceholder: string
  editIdentity: string
  fieldPassword: string
  enterPassword: string
  otpEmailPlaceholder: string
  otpCodePlaceholder: string
  useEmailOtpHint: string
  otpSendingCountdown: (n: number) => string
}

export type AuthApi = {
  login: (req: LoginRequest) => Promise<LoginResponse>
  getCaptchaConfig: () => Promise<{ enabled: boolean; site_key: string }>
  sendEmailOTP: (email: string, cfTurnstileToken?: string) => Promise<void>
  verifyEmailOTP: (email: string, code: string) => Promise<LoginResponse>
}

type Props = {
  onLoggedIn: (accessToken: string) => void
  brandLabel: string
  locale: Locale
  t: AuthPageTranslations
  api: AuthApi
}

const isEmailStr = (v: string) => v.includes('@')

export function AuthPage({ onLoggedIn, brandLabel, locale, t, api }: Props) {
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

  useEffect(() => {
    api.getCaptchaConfig()
      .then((res) => { if (res.enabled) setCaptchaSiteKey(res.site_key) })
      .catch(() => {})
  }, [api])

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
      api.sendEmailOTP(email).catch(() => {})
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
        const resp = await api.login({ login: identity.trim(), password, cf_turnstile_token: captchaSiteKey ? turnstileToken : undefined })
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
      try { await api.sendEmailOTP(email, captchaSiteKey ? turnstileToken : undefined) } catch { /* noop */ } finally {
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
        const resp = await api.verifyEmailOTP(email, code)
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
    <AuthLayout>
          {/* header */}
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
                {brandLabel}
              </div>
            </Reveal>
          </div>

          {/* form */}
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

            {/* password */}
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

            {/* otp email */}
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

            {/* otp code */}
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

          {/* otp hint under password phase */}
          <Reveal active={phase === 'password'}>
            <button
              type="button"
              onClick={switchToOtp}
              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
            >
              {t.useEmailOtpHint}
            </button>
          </Reveal>

          {/* otp resend */}
          <Reveal active={phase === 'otp-code'}>
            <button
              type="button"
              disabled={otpCountdown > 0 || otpSending}
              onClick={async () => {
                const email = otpEmail.trim()
                if (!email) return
                setOtpSending(true)
                try { await api.sendEmailOTP(email) } catch { /* noop */ } finally {
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

          {error && <ErrorCallout error={error} locale={locale} requestFailedText={t.requestFailed} />}
    </AuthLayout>
  )
}
