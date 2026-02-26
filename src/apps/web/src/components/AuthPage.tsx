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

// CSS grid 0fr→1fr 展开动效，无需预知高度
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

type Phase = 'identity' | 'password' | 'otp-email' | 'otp-code' | 'register'

type Props = { onLoggedIn: (accessToken: string, refreshToken: string) => void }

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
  inputRef: React.RefObject<HTMLInputElement>
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

  const [regLogin, setRegLogin] = useState('')
  const [regEmail, setRegEmail] = useState('')
  const [regPassword, setRegPassword] = useState('')
  const [regInviteCode, setRegInviteCode] = useState('')
  const [regSubmitting, setRegSubmitting] = useState(false)

  const [error, setError] = useState<AppError | null>(null)
  const [registrationMode, setRegistrationMode] = useState<RegistrationModeResponse['mode']>('invite_only')

  const passwordRef = useRef<HTMLInputElement>(null)
  const otpEmailRef = useRef<HTMLInputElement>(null)
  const otpCodeRef = useRef<HTMLInputElement>(null)
  const regFirstRef = useRef<HTMLInputElement>(null)

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

  // 阶段切换后自动聚焦（等动效完成）
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
    const t = setTimeout(() => ref.current?.focus(), delay)
    return () => clearTimeout(t)
  }, [phase])

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
    setError(null)
    if (isEmailStr(identity.trim())) {
      // identity 已是邮箱：直接发送 OTP，跳过邮箱输入阶段
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

  const phaseSubtitles: Partial<Record<Phase, string>> = {
    password: t.enterYourPasswordTitle,
    'otp-email': t.otpLoginTab,
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
              marginBottom: '4px',
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
                  onClick={goBack}
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

            {/* ── OTP 邮箱组：identity 是邮箱时不展开（上方已显示） ── */}
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
              </div>
            </Reveal>

            {/* ── 注册组 ── */}
            <Reveal active={phase === 'register'}>
              <div style={{ paddingTop: '6px' }}>
                <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginBottom: '10px' }}>{t.creatingAccountHint}</div>
                {isEmailStr(identity.trim()) ? (
                  <div style={{ marginBottom: '10px' }}>
                    <label style={labelStyle}>{t.enterUsername}</label>
                    <input
                      ref={regFirstRef}
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
                ) : (
                  <div style={{ marginBottom: '10px' }}>
                    <label style={labelStyle}>{t.enterEmail}</label>
                    <input
                      ref={regFirstRef}
                      className={inputCls}
                      style={inputStyle}
                      type="email"
                      placeholder={t.enterEmail}
                      value={regEmail}
                      onChange={(e) => setRegEmail(e.target.value)}
                      autoComplete="email"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </div>
                )}
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
          <Reveal active={phase === 'password'}>
            <button
              type="button"
              onClick={switchToOtp}
              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
            >
              {t.useEmailOtpHint}
            </button>
          </Reveal>

          {/* OTP code 阶段：重发（只此一处） */}
          <Reveal active={phase === 'otp-code'}>
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
              style={{ marginTop: '6px', fontSize: '12px', color: 'var(--c-placeholder)', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'block' }}
              className="disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {otpCountdown > 0 ? t.otpSendingCountdown(otpCountdown) : t.otpSendBtn}
            </button>
          </Reveal>

          {/* identity 阶段：GitHub 登录 */}
          <Reveal active={phase === 'identity'}>
            <div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '10px', margin: '16px 0' }}>
                <div style={{ flex: 1, height: '0.5px', background: 'var(--c-border-auth)' }} />
                <span style={{ fontSize: '11px', color: 'var(--c-placeholder)', fontWeight: 500 }}>{t.orDivider}</span>
                <div style={{ flex: 1, height: '0.5px', background: 'var(--c-border-auth)' }} />
              </div>
              <button type="button" className="github-btn">
                <GitHubIcon />{t.githubLogin}
              </button>
            </div>
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
