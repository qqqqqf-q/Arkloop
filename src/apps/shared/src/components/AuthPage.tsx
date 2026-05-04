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

type Phase = 'identity' | 'password' | 'otp-email' | 'otp-code' | 'register'

export type ResolveIdentityResponse = {
  next_step: 'password' | 'register' | 'setup_required'
  flow_token?: string
  masked_email?: string
  otp_available?: boolean
  invite_required?: boolean
  prefill?: { login?: string; email?: string }
}

export type RegisterRequest = {
  login: string
  password: string
  email: string
  locale: string
  cf_turnstile_token?: string
  invite_code?: string
}

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
  // register phase (optional, only needed by web)
  registerMode?: string
  creatingAccountHint?: string
  enterUsername?: string
  enterEmail?: string
  enterInviteCode?: string
  enterInviteCodeOptional?: string
  registerPasswordHint?: string
}

export type AuthApi = {
  login: (req: LoginRequest) => Promise<LoginResponse>
  getCaptchaConfig: () => Promise<{ enabled: boolean; site_key: string }>
  sendEmailOTP?: (email: string, cfTurnstileToken?: string) => Promise<void>
  verifyEmailOTP?: (email: string, code: string) => Promise<LoginResponse>
  // resolve-identity flow (web)
  resolveIdentity?: (req: { identity: string; cf_turnstile_token?: string }) => Promise<ResolveIdentityResponse>
  getRegistrationMode?: () => Promise<{ mode: 'invite_only' | 'open' }>
  register?: (req: RegisterRequest) => Promise<LoginResponse>
  sendResolvedEmailOTP?: (flowToken: string, cfTurnstileToken?: string) => Promise<void>
  verifyResolvedEmailOTP?: (flowToken: string, code: string) => Promise<LoginResponse>
}

type Props = {
  onLoggedIn: (accessToken: string) => void
  brandLabel: string
  locale: Locale
  t: AuthPageTranslations
  api: AuthApi
}

const isEmailStr = (v: string) => v.includes('@')

const passwordEncoder = new TextEncoder()

function registerPasswordMeetsPolicy(password: string): boolean {
  const len = passwordEncoder.encode(password).length
  return len >= 8 && len <= 72 && /\p{L}/u.test(password) && /\p{N}/u.test(password)
}

export function AuthPage({ onLoggedIn, brandLabel, locale, t, api }: Props) {
  const [identity, setIdentity] = useState('')
  const [phase, setPhase] = useState<Phase>('identity')
  const [maskedEmail, setMaskedEmail] = useState('')
  const [checking, setChecking] = useState(false)
  const [flowToken, setFlowToken] = useState('')
  const [otpAvailable, setOtpAvailable] = useState(false)

  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const [otpEmail, setOtpEmail] = useState('')
  const [otpCode, setOtpCode] = useState('')
  const [otpCountdown, setOtpCountdown] = useState(0)
  const [otpSending, setOtpSending] = useState(false)
  const [otpSubmitting, setOtpSubmitting] = useState(false)
  const countdownRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // register state
  const [regLogin, setRegLogin] = useState('')
  const [regEmail, setRegEmail] = useState('')
  const [regPassword, setRegPassword] = useState('')
  const [regInviteCode, setRegInviteCode] = useState('')
  const [regSubmitting, setRegSubmitting] = useState(false)
  const [registerEmailLocked, setRegisterEmailLocked] = useState(false)
  const [registrationMode, setRegistrationMode] = useState<'invite_only' | 'open'>('invite_only')

  const [error, setError] = useState<AppError | null>(null)
  const [captchaSiteKey, setCaptchaSiteKey] = useState('')
  const [turnstileToken, setTurnstileToken] = useState('')

  const passwordRef = useRef<HTMLInputElement>(null)
  const otpEmailRef = useRef<HTMLInputElement>(null)
  const otpCodeRef = useRef<HTMLInputElement>(null)
  const regFirstRef = useRef<HTMLInputElement>(null)

  const hasResolveFlow = !!api.resolveIdentity
  const inviteRequired = registrationMode === 'invite_only'

  useEffect(() => {
    void Promise.all([
      api.getCaptchaConfig()
        .then((res) => { if (res.enabled) setCaptchaSiteKey(res.site_key) })
        .catch(() => {}),
      api.getRegistrationMode?.()
        .then((res) => setRegistrationMode(res.mode))
        .catch(() => {}),
    ].filter(Boolean))
  }, [api])

  useEffect(() => () => { if (countdownRef.current) clearInterval(countdownRef.current) }, [])

  useEffect(() => {
    const delay = 420
    const refs: Record<string, React.RefObject<HTMLInputElement | null>> = {
      password: passwordRef,
      'otp-email': otpEmailRef,
      'otp-code': otpCodeRef,
      register: regFirstRef,
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
    setFlowToken('')
    setOtpAvailable(false)
    setRegisterEmailLocked(false)
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

  const switchToOtp = async () => {
    setError(null)
    if (flowToken && api.sendResolvedEmailOTP) {
      setOtpSending(true)
      try {
        await api.sendResolvedEmailOTP(flowToken, captchaSiteKey ? turnstileToken : undefined)
        setOtpCode('')
        setPhase('otp-code')
        startCountdown()
        setTurnstileToken('')
      } catch (err) {
        setTurnstileToken('')
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setOtpSending(false)
      }
    } else if (api.sendEmailOTP) {
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
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)

    if (phase === 'identity') {
      const id = identity.trim()
      if (!id) return
      setChecking(true)
      try {
        if (api.resolveIdentity) {
          const res = await api.resolveIdentity({
            identity: id,
            cf_turnstile_token: captchaSiteKey ? turnstileToken : undefined,
          })
          setTurnstileToken('')
          if (res.next_step === 'password') {
            setMaskedEmail(res.masked_email ?? '')
            setFlowToken(res.flow_token ?? '')
            setOtpAvailable(res.otp_available ?? false)
            setPhase('password')
          } else if (res.next_step === 'setup_required') {
            setError({ code: 'auth.setup_required', message: 'setup_required' })
          } else {
            setRegLogin(res.prefill?.login ?? '')
            setRegEmail(res.prefill?.email ?? '')
            setRegPassword('')
            setRegInviteCode('')
            setRegisterEmailLocked(Boolean(res.prefill?.email))
            setFlowToken('')
            setOtpAvailable(false)
            setPhase('register')
          }
        } else {
          setMaskedEmail('')
          setPhase('password')
        }
      } catch (err) {
        setTurnstileToken('')
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

    if (phase === 'otp-email' && api.sendEmailOTP) {
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
      const code = otpCode.trim()
      if (code.length !== 6) return
      setOtpSubmitting(true)
      try {
        let resp: LoginResponse
        if (flowToken && api.verifyResolvedEmailOTP) {
          resp = await api.verifyResolvedEmailOTP(flowToken, code)
        } else if (api.verifyEmailOTP) {
          const email = otpEmail.trim()
          if (!email) return
          resp = await api.verifyEmailOTP(email, code)
        } else {
          return
        }
        onLoggedIn(resp.access_token)
      } catch (err) {
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setOtpSubmitting(false)
      }
      return
    }

    if (phase === 'register' && api.register) {
      setRegSubmitting(true)
      try {
        const resp = await api.register({
          login: regLogin.trim(),
          password: regPassword,
          email: regEmail.trim(),
          locale,
          cf_turnstile_token: captchaSiteKey ? turnstileToken : undefined,
          ...(regInviteCode.trim() ? { invite_code: regInviteCode.trim() } : {}),
        })
        onLoggedIn(resp.access_token)
      } catch (err) {
        setTurnstileToken('')
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setRegSubmitting(false)
      }
    }
  }

  const isLoading = checking || submitting || otpSending || otpSubmitting || regSubmitting

  const canSubmit = useMemo(() => {
    if (isLoading) return false
    const captchaOk = !captchaSiteKey || !!turnstileToken
    if (phase === 'identity') return identity.trim().length > 0 && (hasResolveFlow ? captchaOk : true)
    if (phase === 'password') return password.length > 0 && captchaOk
    if (phase === 'otp-email') return otpEmail.trim().length > 0 && captchaOk
    if (phase === 'otp-code') {
      if (flowToken) return otpCode.length === 6
      return otpEmail.trim().length > 0 && otpCode.length === 6
    }
    if (phase === 'register') {
      if (!regLogin.trim() || !regEmail.trim() || !registerPasswordMeetsPolicy(regPassword)) return false
      if (inviteRequired && !regInviteCode.trim()) return false
      return captchaOk
    }
    return false
  }, [phase, identity, password, otpEmail, otpCode, regLogin, regEmail, regPassword, regInviteCode, inviteRequired, isLoading, captchaSiteKey, turnstileToken, flowToken, hasResolveFlow])

  const btnLabel = useMemo(() => {
    if (phase === 'otp-email') return t.otpSendBtn
    if (phase === 'otp-code') return t.otpVerifyBtn
    return t.continueBtn
  }, [phase, t])

  const phaseSubtitles: Partial<Record<Phase, string>> = {
    password: t.enterYourPasswordTitle,
    'otp-email': t.otpLoginTab,
    'otp-code': t.otpLoginTab,
    register: t.registerMode ?? '',
  }

  const showOtpHint = phase === 'password' && (hasResolveFlow ? otpAvailable : true)
  const showCaptcha = captchaSiteKey && (
    phase === 'identity' && hasResolveFlow
    || phase === 'password'
    || phase === 'otp-email'
    || phase === 'register'
    || phase === 'otp-code'
  )

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
            <Reveal active={phase === 'otp-email' || (phase === 'otp-code' && !flowToken && !isEmailStr(identity.trim()))}>
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
                {maskedEmail && (
                  <div style={{ fontSize: '11px', color: 'var(--c-placeholder)', marginTop: '6px', paddingLeft: '2px' }}>
                    {maskedEmail}
                  </div>
                )}
              </div>
            </Reveal>

            {/* register */}
            <Reveal active={phase === 'register'}>
              <div style={{ paddingTop: '6px' }}>
                <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginBottom: '10px' }}>{t.creatingAccountHint ?? ''}</div>
                <div style={{ marginBottom: '10px' }}>
                  <label style={labelStyle}>{t.enterUsername ?? ''}</label>
                  <input
                    ref={registerEmailLocked ? regFirstRef : undefined}
                    className={inputCls}
                    style={inputStyle}
                    type="text"
                    placeholder={t.enterUsername ?? ''}
                    value={regLogin}
                    onChange={(e) => setRegLogin(e.target.value)}
                    autoComplete="username"
                    autoCapitalize="none"
                    spellCheck={false}
                  />
                </div>
                <div style={{ marginBottom: '10px' }}>
                  <label style={labelStyle}>{t.enterEmail ?? ''}</label>
                  <input
                    ref={registerEmailLocked ? undefined : regFirstRef}
                    className={inputCls}
                    style={{
                      ...inputStyle,
                      color: registerEmailLocked ? 'var(--c-text-secondary)' : 'var(--c-text-primary)',
                    }}
                    type="email"
                    placeholder={t.enterEmail ?? ''}
                    value={regEmail}
                    onChange={(e) => setRegEmail(e.target.value)}
                    autoComplete="email"
                    autoCapitalize="none"
                    spellCheck={false}
                    readOnly={registerEmailLocked}
                  />
                </div>
                <div style={{ marginBottom: '10px' }}>
                  <label style={labelStyle}>{t.fieldPassword}</label>
                  <PasswordEye
                    inputRef={null as unknown as React.RefObject<HTMLInputElement>}
                    placeholder={t.enterPassword}
                    value={regPassword}
                    onChange={setRegPassword}
                    showPassword={showPassword}
                    onToggleShow={() => setShowPassword((v) => !v)}
                    autoComplete="new-password"
                  />
                  <div style={{ fontSize: '11px', color: 'var(--c-placeholder)', marginTop: '6px', paddingLeft: '2px' }}>{t.registerPasswordHint ?? ''}</div>
                </div>
                <div>
                  <label style={labelStyle}>{inviteRequired ? (t.enterInviteCode ?? '') : (t.enterInviteCodeOptional ?? '')}</label>
                  <input
                    className={inputCls}
                    style={inputStyle}
                    type="text"
                    placeholder={inviteRequired ? (t.enterInviteCode ?? '') : (t.enterInviteCodeOptional ?? '')}
                    value={regInviteCode}
                    onChange={(e) => setRegInviteCode(e.target.value)}
                    autoComplete="off"
                  />
                </div>
              </div>
            </Reveal>

            {showCaptcha && (
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
          <Reveal active={showOtpHint}>
            <button
              type="button"
              onClick={switchToOtp}
              disabled={otpSending || (!!captchaSiteKey && !turnstileToken && hasResolveFlow)}
              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
              className="disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {t.useEmailOtpHint}
            </button>
          </Reveal>

          {/* otp resend */}
          <Reveal active={phase === 'otp-code'}>
            <button
              type="button"
              disabled={otpCountdown > 0 || otpSending || (flowToken ? (!flowToken || (!!captchaSiteKey && !turnstileToken)) : false)}
              onClick={async () => {
                if (flowToken && api.sendResolvedEmailOTP) {
                  setOtpSending(true)
                  try {
                    await api.sendResolvedEmailOTP(flowToken, captchaSiteKey ? turnstileToken : undefined)
                    setTurnstileToken('')
                    startCountdown()
                  } catch (err) {
                    setTurnstileToken('')
                    setError(normalizeError(err, t.requestFailed))
                  } finally {
                    setOtpSending(false)
                  }
                } else if (api.sendEmailOTP) {
                  const email = otpEmail.trim()
                  if (!email) return
                  setOtpSending(true)
                  try { await api.sendEmailOTP(email) } catch { /* noop */ } finally {
                    setOtpSending(false)
                    startCountdown()
                  }
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
