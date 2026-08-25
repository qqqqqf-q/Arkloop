import { useState, useEffect, useRef, type FormEvent } from 'react'
import { ErrorCallout, type AppError } from './ErrorCallout'
import {
  SpinnerIcon, normalizeError, Reveal, PasswordEye, AuthLayout,
  TRANSITION, inputCls, inputStyle, labelStyle,
} from './auth-ui'
import type { Locale } from '../contexts/LocaleContext'
import type { LoginRequest, LoginResponse } from '../api/types'

type Phase = 'identity' | 'password'

// 对齐后端 resolve 塌缩后的本地语义:register/OTP 已随远程注册登录删除,
// 只剩 password(本机已设密码)与 setup_required(desktop 密码未设置,
// 由 ark CLI headless 引导完成设置,浏览器侧只能提示)。
export type ResolveIdentityResponse = {
  next_step: 'password' | 'setup_required'
}

export type AuthPageTranslations = {
  requestFailed: string
  loginMode: string
  enterYourPasswordTitle: string
  continueBtn: string
  backBtn: string
  fieldIdentity: string
  identityPlaceholder: string
  editIdentity: string
  fieldPassword: string
  enterPassword: string
}

export type AuthApi = {
  login: (req: LoginRequest) => Promise<LoginResponse>
  resolveIdentity: (req: { identity: string }) => Promise<ResolveIdentityResponse>
}

type Props = {
  onLoggedIn: (accessToken: string) => void
  brandLabel: string
  locale: Locale
  t: AuthPageTranslations
  api: AuthApi
}

export function AuthPage({ onLoggedIn, brandLabel, locale, t, api }: Props) {
  const [identity, setIdentity] = useState('')
  const [phase, setPhase] = useState<Phase>('identity')
  const [checking, setChecking] = useState(false)

  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const [error, setError] = useState<AppError | null>(null)

  const passwordRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (phase !== 'password') return
    const timer = setTimeout(() => passwordRef.current?.focus(), 420)
    return () => clearTimeout(timer)
  }, [phase])

  const resetToIdentity = () => {
    setPhase('identity')
    setPassword('')
    setShowPassword(false)
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
        const res = await api.resolveIdentity({ identity: id })
        if (res.next_step === 'setup_required') {
          setError({ code: 'auth.setup_required', message: 'setup_required' })
        } else {
          setPhase('password')
        }
      } catch (err) {
        setError(normalizeError(err, t.requestFailed))
      } finally {
        setChecking(false)
      }
      return
    }

    if (!password) return
    setSubmitting(true)
    try {
      const resp = await api.login({ login: identity.trim(), password })
      onLoggedIn(resp.access_token)
    } catch (err) {
      setError(normalizeError(err, t.requestFailed))
    } finally {
      setSubmitting(false)
    }
  }

  const isLoading = checking || submitting
  const canSubmit = !isLoading && (phase === 'identity' ? identity.trim().length > 0 : password.length > 0)

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
                {phase === 'password' ? t.enterYourPasswordTitle : ''}
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
              {isLoading ? <><SpinnerIcon />{t.continueBtn}</> : t.continueBtn}
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

          {error && <ErrorCallout error={error} locale={locale} requestFailedText={t.requestFailed} />}
    </AuthLayout>
  )
}
