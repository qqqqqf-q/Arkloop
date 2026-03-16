import { useState, useMemo, useEffect, useCallback, useRef, type FormEvent } from 'react'
import {
  login,
  register,
  getRegistrationMode,
  resolveIdentity,
  sendResolvedEmailOTP,
  verifyResolvedEmailOTP,
  getCaptchaConfig,
  isApiError,
} from '../api'
import type { RegistrationModeResponse } from '../api'
import { ErrorCallout } from './ErrorCallout'
import type { AppError } from './ErrorCallout'
import { Turnstile } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import {
  SpinnerIcon,
  EyeIcon,
  normalizeError,
  Reveal,
  PasswordEye,
  TRANSITION,
  inputCls,
  inputStyle,
  labelStyle,
} from '@arkloop/shared/components/auth-ui'

type Phase = 'identity' | 'password' | 'otp-code' | 'register'

type Props = { onLoggedIn: (accessToken: string) => void }

const passwordEncoder = new TextEncoder()

function registerPasswordMeetsPolicy(password: string): boolean {
	const passwordBytes = passwordEncoder.encode(password).length
	return passwordBytes >= 8 && passwordBytes <= 72 && /\p{L}/u.test(password) && /\p{N}/u.test(password)
}

export function AuthPage({ onLoggedIn }: Props) {
  const [identity, setIdentity] = useState('')
  const [phase, setPhase] = useState<Phase>('identity')
  const [maskedEmail, setMaskedEmail] = useState('')
  const [checking, setChecking] = useState(false)
  const [flowToken, setFlowToken] = useState('')
  const [otpAvailable, setOtpAvailable] = useState(false)

  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)

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
  const [registerEmailLocked, setRegisterEmailLocked] = useState(false)

  const [error, setError] = useState<AppError | null>(null)
  const [registrationMode, setRegistrationMode] = useState<RegistrationModeResponse['mode']>('invite_only')
  const [captchaSiteKey, setCaptchaSiteKey] = useState('')
  const [turnstileToken, setTurnstileToken] = useState('')

  const passwordRef = useRef<HTMLInputElement>(null)
  const otpCodeRef = useRef<HTMLInputElement>(null)
  const regFirstRef = useRef<HTMLInputElement>(null)

  const { t, locale } = useLocale()

  useEffect(() => {
    const root = document.documentElement
    const prev = root.getAttribute('data-theme')
    root.removeAttribute('data-theme')
    return () => { if (prev) root.setAttribute('data-theme', prev) }
  }, [])

  useEffect(() => {
    void Promise.all([
      getRegistrationMode().then((res) => setRegistrationMode(res.mode)).catch(() => {}),
      getCaptchaConfig().then((res) => { if (res.enabled) setCaptchaSiteKey(res.site_key) }).catch(() => {}),
    ])
  }, [])

  useEffect(() => () => { if (countdownRef.current) clearInterval(countdownRef.current) }, [])

  // 阶段切换后自动聚焦（等动效完成）
  useEffect(() => {
    const delay = 420
    const refs: Record<string, React.RefObject<HTMLInputElement | null>> = {
      password: passwordRef,
      'otp-code': otpCodeRef,
      register: regFirstRef,
    }
    const ref = refs[phase]
    if (!ref) return
    const t = setTimeout(() => ref.current?.focus(), delay)
    return () => clearTimeout(t)
  }, [phase])

  const inviteRequired = registrationMode === 'invite_only'

  const resetToIdentity = () => {
    setPhase('identity')
    setPassword('')
    setShowPassword(false)
    setOtpCode('')
    setOtpCountdown(0)
    if (countdownRef.current) clearInterval(countdownRef.current)
    setMaskedEmail('')
    setFlowToken('')
    setOtpAvailable(false)
    setError(null)
    setTurnstileToken('')
    setRegisterEmailLocked(false)
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
    if (!flowToken || !otpAvailable) return
    setOtpSending(true)
    try {
      await sendResolvedEmailOTP(flowToken, captchaSiteKey ? turnstileToken : undefined)
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
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)

    if (phase === 'identity') {
      const id = identity.trim()
      if (!id) return
      setChecking(true)
      try {
        const res = await resolveIdentity({
          identity: id,
          cf_turnstile_token: captchaSiteKey ? turnstileToken : undefined,
        })
        setTurnstileToken('')
        if (res.next_step === 'password') {
          setMaskedEmail(res.masked_email ?? '')
          setFlowToken(res.flow_token)
          setOtpAvailable(res.otp_available)
          setPhase('password')
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

    if (phase === 'otp-code') {
      const code = otpCode.trim()
      if (!flowToken || code.length !== 6) return
      setOtpSubmitting(true)
      try {
        const resp = await verifyResolvedEmailOTP(flowToken, code)
        onLoggedIn(resp.access_token)
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
    if (phase === 'identity') return identity.trim().length > 0 && captchaOk
    if (phase === 'password') return password.length > 0 && captchaOk
    if (phase === 'otp-code') return otpCode.length === 6
    if (phase === 'register') {
      if (!regLogin.trim() || !regEmail.trim() || !registerPasswordMeetsPolicy(regPassword)) return false
      if (inviteRequired && !regInviteCode.trim()) return false
      return captchaOk
    }
    return false
  }, [phase, identity, password, otpCode, regLogin, regEmail, regPassword, regInviteCode, inviteRequired, isLoading, captchaSiteKey, turnstileToken])

  const btnLabel = useMemo(() => {
    if (phase === 'otp-code') return t.otpVerifyBtn
    return t.continueBtn
  }, [phase, t])

  const phaseSubtitles: Partial<Record<Phase, string>> = {
    password: t.enterYourPasswordTitle,
    'otp-code': t.otpLoginTab,
    register: t.registerMode,
  }

  return (
    <div style={{ minHeight: '100vh', background: 'var(--c-bg-page)', display: 'flex', flexDirection: 'column' as const, position: 'relative' as const, overflow: 'hidden' }}>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      {/* 使用 padding-top 固定 section 顶部位置，section 向下展开，identity 输入框不移动 */}
      <div
        style={{ flex: 1, display: 'flex', flexDirection: 'column' as const, alignItems: 'center', padding: 'max(48px, calc(50vh - 140px)) 20px 48px', position: 'relative', zIndex: 1 }}
      >
        <section style={{ width: 'min(400px, 100%)' }}>

          {/* ── HEADER ── 固定高度，内部动效 */}
          <div style={{ height: '64px', marginBottom: '20px' }}>
            {/* Arkloop：居中 → 左对齐 */}
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

            {/* 副标题区（固定高度，两层叠加淡入淡出） */}
            <div style={{ position: 'relative', height: '22px', marginTop: '8px' }}>
              {/* "登录" - 居中，淡出 */}
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
              {/* 阶段副标题 - 左对齐，淡入 */}
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
          </div>

          {/* ── FORM ── */}
          <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column' as const }}>

            {/* identity label：占位始终存在，非 identity 阶段才可见 → identity 输入框 Y 位置固定 */}
            <div style={{
              height: '18px',
              opacity: phase !== 'identity' ? 1 : 0,
              transition: `opacity ${TRANSITION}`,
              ...labelStyle,
            }}>
              {t.fieldIdentity}
            </div>

            {/* identity 输入框 / 静态展示 */}
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

            {/* ── 密码组 ── */}
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

	            {/* ── OTP 验证码组 ── */}
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

	            {/* ── 注册组 ── */}
	            <Reveal active={phase === 'register'}>
	              <div style={{ paddingTop: '6px' }}>
	                <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginBottom: '10px' }}>{t.creatingAccountHint}</div>
	                <div style={{ marginBottom: '10px' }}>
	                  <label style={labelStyle}>{t.enterUsername}</label>
	                  <input
	                    ref={registerEmailLocked ? regFirstRef : undefined}
	                    className={inputCls}
	                    style={inputStyle}
	                    type="text"
	                    placeholder={t.enterUsername}
	                    value={regLogin}
	                    onChange={(e) => setRegLogin(e.target.value)}
	                    autoComplete="username"
	                    autoCapitalize="none"
	                    spellCheck={false}
	                  />
	                </div>
	                <div style={{ marginBottom: '10px' }}>
	                  <label style={labelStyle}>{t.enterEmail}</label>
	                  <input
	                    ref={registerEmailLocked ? undefined : regFirstRef}
	                    className={inputCls}
	                    style={{
	                      ...inputStyle,
	                      color: registerEmailLocked ? 'var(--c-text-secondary)' : 'var(--c-text-primary)',
	                    }}
	                    type="email"
	                    placeholder={t.enterEmail}
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
                  <div style={{ position: 'relative' }}>
                    <input
                      className={inputCls}
                      style={{ ...inputStyle, paddingRight: '38px' }}
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
                      style={{ position: 'absolute', right: '10px', top: '50%', transform: 'translateY(-50%)', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--c-placeholder)', padding: '2px', display: 'flex', alignItems: 'center' }}
                    >
                      <EyeIcon open={showPassword} />
                    </button>
                  </div>
                  <div style={{ fontSize: '11px', color: 'var(--c-placeholder)', marginTop: '6px', paddingLeft: '2px' }}>{t.registerPasswordHint}</div>
                </div>
                <div>
                  <label style={labelStyle}>{inviteRequired ? t.enterInviteCode : t.enterInviteCodeOptional}</label>
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
              </div>
            </Reveal>

            {/* Continue 按钮 */}
	            {captchaSiteKey && (phase === 'identity' || phase === 'password' || phase === 'register' || phase === 'otp-code') && (
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

            {/* 返回按钮（全宽 ghost，与 Continue 同高） */}
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

	          {/* 密码阶段：OTP 提示 */}
	          <Reveal active={phase === 'password' && otpAvailable}>
	            <button
	              type="button"
	              onClick={switchToOtp}
	              disabled={otpSending || (!!captchaSiteKey && !turnstileToken)}
	              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
	              className="disabled:opacity-40 disabled:cursor-not-allowed"
	            >
	              {t.useEmailOtpHint}
	            </button>
	          </Reveal>

	          {/* OTP code 阶段：重发（只此一处） */}
	          <Reveal active={phase === 'otp-code'}>
	            <button
	              type="button"
	              disabled={otpCountdown > 0 || otpSending || !flowToken || (!!captchaSiteKey && !turnstileToken)}
	              onClick={async () => {
	                setOtpSending(true)
	                try {
	                  await sendResolvedEmailOTP(flowToken, captchaSiteKey ? turnstileToken : undefined)
	                  setTurnstileToken('')
	                  startCountdown()
	                } catch (err) {
	                  setTurnstileToken('')
	                  setError(normalizeError(err, t.requestFailed))
	                } finally {
	                  setOtpSending(false)
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
        © 2026 Arkloop
      </footer>
    </div>
  )
}
