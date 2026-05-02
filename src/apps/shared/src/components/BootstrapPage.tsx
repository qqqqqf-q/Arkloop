import { useEffect, useMemo, useState, useRef, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { verifyBootstrapToken, setupBootstrapAdmin } from '../api/bootstrap'
import type { BootstrapSetupRequest, BootstrapSetupResponse, BootstrapVerifyResponse } from '../api/bootstrap'
import { ErrorCallout } from './ErrorCallout'
import {
  SpinnerIcon, normalizeError, Reveal, PasswordEye, AuthLayout,
  TRANSITION, inputCls, inputStyle, labelStyle,
} from './auth-ui'
import type { Locale } from '../contexts/LocaleContext'

export type BootstrapTranslations = {
  bootstrap: {
    title: string
    subtitle: string
    username: string
    usernamePlaceholder: string
    password: string
    passwordPlaceholder: string
    confirmPassword: string
    confirmPasswordPlaceholder: string
    passwordMismatch: string
    submit: string
    verifying: string
    successTitle: string
    successBody: string
    invalidTitle: string
    invalidBody: string
    expiresAt: (value: string) => string
    selectHint: string
  }
  loading: string
  requestFailed: string
}

export type ConsoleTarget = {
  name: string
  description: string
  url: string
  current?: boolean
}

export type BootstrapApi = {
  verifyToken: (token: string) => Promise<BootstrapVerifyResponse>
  setup: (req: BootstrapSetupRequest) => Promise<BootstrapSetupResponse>
}

type Phase = 'verifying' | 'invalid' | 'form' | 'success'

type Props = {
  onLoggedIn: (accessToken: string) => void
  t: BootstrapTranslations
  locale: Locale
  consoles?: ConsoleTarget[]
  tokenOverride?: string
  api?: BootstrapApi
  successPath?: string
}

const defaultBootstrapApi: BootstrapApi = {
  verifyToken: verifyBootstrapToken,
  setup: setupBootstrapAdmin,
}

export function BootstrapPage({ onLoggedIn, t, locale, consoles, tokenOverride, api = defaultBootstrapApi, successPath = '/dashboard' }: Props) {
  const { token: routeToken = '' } = useParams()
  const token = tokenOverride ?? routeToken
  const navigate = useNavigate()

  const [phase, setPhase] = useState<Phase>('verifying')
  const [expiresAt, setExpiresAt] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [pendingAccessToken, setPendingAccessToken] = useState<string | null>(null)
  const [error, setError] = useState<ReturnType<typeof normalizeError> | null>(null)

  const usernameRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)
  const confirmPasswordRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let cancelled = false
    setPhase('verifying')
    setError(null)

    api.verifyToken(token)
      .then((resp) => {
        if (cancelled) return
        if (resp.valid) {
          setExpiresAt(resp.expires_at)
          setPhase('form')
        } else {
          setPhase('invalid')
        }
      })
      .catch((err) => {
        if (cancelled) return
        setPhase('invalid')
        setError(normalizeError(err, t.requestFailed))
      })

    return () => { cancelled = true }
  }, [api, token, t.requestFailed])

  useEffect(() => {
    if (phase !== 'form') return
    const timer = setTimeout(() => usernameRef.current?.focus(), 420)
    return () => clearTimeout(timer)
  }, [phase])

  const hasMultipleConsoles = consoles && consoles.length > 1

  useEffect(() => {
    if (phase !== 'success' || !pendingAccessToken) return
    if (hasMultipleConsoles) return
      const timer = window.setTimeout(() => {
      onLoggedIn(pendingAccessToken)
      navigate(successPath, { replace: true })
    }, 900)
    return () => window.clearTimeout(timer)
  }, [navigate, onLoggedIn, pendingAccessToken, phase, hasMultipleConsoles, successPath])

  const expiresLabel = useMemo(() => {
    if (!expiresAt) return ''
    const date = new Date(expiresAt)
    const formatted = Number.isNaN(date.valueOf())
      ? expiresAt
      : new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
          dateStyle: 'medium',
          timeStyle: 'short',
        }).format(date)
    return t.bootstrap.expiresAt(formatted)
  }, [expiresAt, locale, t.bootstrap])

  const canSubmit = useMemo(() => {
    if (submitting) return false
    return username.trim().length > 0 && password.length > 0 && confirmPassword.length > 0
  }, [submitting, username, password, confirmPassword])

  const passwordMismatch = password.length > 0 && confirmPassword.length > 0 && password !== confirmPassword

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit || !token || passwordMismatch) return
    setSubmitting(true)
    setError(null)
    try {
      const resp = await api.setup({
        token,
        username: username.trim(),
        password,
        locale,
      })
      setPendingAccessToken(resp.access_token)
      setPhase('success')
    } catch (err) {
      const normalized = normalizeError(err, t.requestFailed)
      setError(normalized)
      if (normalized.code === 'bootstrap.invalid_token' || normalized.code === 'bootstrap.already_initialized') {
        setPhase('invalid')
      }
    } finally {
      setSubmitting(false)
    }
  }

  function handleConsoleSelect(target: ConsoleTarget) {
    if (!pendingAccessToken) return
    onLoggedIn(pendingAccessToken)
    if (target.current) {
      navigate(successPath, { replace: true })
    } else {
      window.location.href = `${target.url}#_t=${encodeURIComponent(pendingAccessToken)}`
    }
  }

  const phaseSubtitles: Partial<Record<Phase, string>> = {
    form: t.bootstrap.subtitle,
    success: t.bootstrap.successBody,
  }

  return (
    <AuthLayout>
          {/* header */}
          <div style={{ height: '64px', marginBottom: '20px' }}>
            <div style={{
              display: 'block',
              width: 'fit-content',
              position: 'relative',
              left: phase === 'verifying' || phase === 'success' ? '50%' : '0',
              transform: phase === 'verifying' || phase === 'success' ? 'translateX(-50%)' : 'translateX(0)',
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
                opacity: phase === 'verifying' ? 1 : 0,
                transition: 'opacity 0.2s ease',
                pointerEvents: 'none',
                userSelect: 'none',
              }}>
                {t.bootstrap.verifying}
              </div>
              <div style={{
                position: 'absolute', left: 0, top: 0,
                fontSize: '13px', fontWeight: 500, color: 'var(--c-placeholder)',
                opacity: phase === 'form' || phase === 'invalid' ? 1 : 0,
                transform: phase === 'form' || phase === 'invalid' ? 'translateY(0)' : 'translateY(3px)',
                transition: 'opacity 0.25s ease 0.12s, transform 0.25s ease 0.12s',
                pointerEvents: 'none',
                userSelect: 'none',
                whiteSpace: 'nowrap',
              }}>
                {phase === 'invalid' ? t.bootstrap.invalidTitle : (phaseSubtitles[phase] ?? '')}
              </div>
              <div style={{
                position: 'absolute', width: '100%', textAlign: 'center',
                fontSize: '15px', fontWeight: 500, color: 'var(--c-placeholder)',
                opacity: phase === 'success' ? 1 : 0,
                transition: 'opacity 0.25s ease 0.12s',
                pointerEvents: 'none',
                userSelect: 'none',
              }}>
                {t.bootstrap.successTitle}
              </div>
            </div>

            <Reveal active={phase === 'verifying'}>
              <div style={{ textAlign: 'center', fontSize: '12px', fontWeight: 500, color: 'var(--c-placeholder)', letterSpacing: '0.1em', textTransform: 'uppercase' as const, marginTop: '2px' }}>
                {t.bootstrap.title}
              </div>
            </Reveal>
          </div>

          {/* invalid state */}
          <Reveal active={phase === 'invalid'}>
            <div style={{ paddingTop: '4px' }}>
              <div style={{ fontSize: '13px', color: 'var(--c-text-secondary)' }}>
                {t.bootstrap.invalidBody}
              </div>
              {error && <ErrorCallout error={error} locale={locale} requestFailedText={t.requestFailed} />}
            </div>
          </Reveal>

          {/* success state */}
          <Reveal active={phase === 'success'}>
            <div style={{ paddingTop: '4px' }}>
              {hasMultipleConsoles ? (
                <>
                  <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', textAlign: 'center', marginBottom: '14px' }}>
                    {t.bootstrap.selectHint}
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column' as const, gap: '8px' }}>
                    {consoles!.map((c) => (
                      <button
                        key={c.url}
                        onClick={() => handleConsoleSelect(c)}
                        style={{
                          width: '100%',
                          padding: '14px 16px',
                          borderRadius: '10px',
                          border: '0.5px solid var(--c-border)',
                          background: 'var(--c-bg-input)',
                          display: 'flex',
                          alignItems: 'center',
                          gap: '12px',
                          cursor: 'pointer',
                          textAlign: 'left' as const,
                          fontFamily: 'inherit',
                          transition: TRANSITION,
                        }}
                      >
                        <div>
                          <div style={{ fontSize: '14px', fontWeight: 500, color: 'var(--c-text-primary)' }}>
                            {c.name}
                          </div>
                          <div style={{ fontSize: '12px', color: 'var(--c-placeholder)', marginTop: '2px' }}>
                            {c.description}
                          </div>
                        </div>
                        <span style={{ marginLeft: 'auto', fontSize: '15px', color: 'var(--c-placeholder)' }}>{'\u2192'}</span>
                      </button>
                    ))}
                  </div>
                </>
              ) : (
                <div style={{ textAlign: 'center' }}>
                  <div style={{ fontSize: '13px', color: 'var(--c-text-secondary)' }}>
                    {t.bootstrap.successBody}
                  </div>
                </div>
              )}
            </div>
          </Reveal>

          {/* form */}
          <Reveal active={phase === 'form'}>
            <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column' as const }}>

              <label style={labelStyle}>{t.bootstrap.username}</label>
              <input
                ref={usernameRef}
                className={inputCls}
                style={inputStyle}
                type="text"
                placeholder={t.bootstrap.usernamePlaceholder}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
              />

              <div style={{ paddingTop: '10px' }}>
                <label style={labelStyle}>{t.bootstrap.password}</label>
                <PasswordEye
                  inputRef={passwordRef}
                  placeholder={t.bootstrap.passwordPlaceholder}
                  value={password}
                  onChange={setPassword}
                  showPassword={showPassword}
                  onToggleShow={() => setShowPassword((v) => !v)}
                  autoComplete="new-password"
                />
              </div>

              <div style={{ paddingTop: '10px' }}>
                <label style={labelStyle}>{t.bootstrap.confirmPassword}</label>
                <PasswordEye
                  inputRef={confirmPasswordRef}
                  placeholder={t.bootstrap.confirmPasswordPlaceholder}
                  value={confirmPassword}
                  onChange={setConfirmPassword}
                  showPassword={showConfirmPassword}
                  onToggleShow={() => setShowConfirmPassword((v) => !v)}
                  autoComplete="new-password"
                />
                {passwordMismatch && (
                  <div style={{ marginTop: '4px', fontSize: '11px', color: 'var(--c-error, #ef4444)', paddingLeft: '2px' }}>
                    {t.bootstrap.passwordMismatch}
                  </div>
                )}
              </div>

              {expiresLabel && (
                <div style={{ marginTop: '10px', fontSize: '11px', color: 'var(--c-placeholder)', paddingLeft: '2px' }}>
                  {expiresLabel}
                </div>
              )}

              {error && <ErrorCallout error={error} locale={locale} requestFailedText={t.requestFailed} />}

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
                {submitting ? <><SpinnerIcon />{t.loading}</> : t.bootstrap.submit}
              </button>
            </form>
          </Reveal>
    </AuthLayout>
  )
}
