import { useState, useMemo, type FormEvent } from 'react'
import { login, register, isApiError } from '../api'
import { ErrorCallout, type AppError } from './ErrorCallout'

function normalizeError(error: unknown): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: '请求失败' }
}

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function AuthPage({ onLoggedIn }: Props) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)

  const canSubmit = useMemo(() => {
    if (submitting) return false
    if (!loginValue.trim() || !password) return false
    if (mode === 'register' && (!displayName.trim() || password.length < 8)) return false
    return true
  }, [loginValue, password, displayName, submitting, mode])

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      if (mode === 'login') {
        const resp = await login({ login: loginValue, password })
        onLoggedIn(resp.access_token)
      } else {
        const resp = await register({ login: loginValue, password, display_name: displayName })
        onLoggedIn(resp.access_token)
      }
    } catch (err) {
      setError(normalizeError(err))
    } finally {
      setSubmitting(false)
    }
  }

  const inputStyle = {
    border: '0.5px solid #5E5A5A',
  }

  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center px-5"
      style={{ background: '#141413', padding: '72px 20px', gap: '48px' }}
    >
      <header className="flex flex-col items-center" style={{ gap: '10px' }}>
        <div style={{ fontSize: '36px', fontWeight: 500, color: '#FAF9F5' }}>Arkloop</div>
        <div style={{ fontSize: '20px', fontWeight: 500, color: 'rgba(255, 255, 255, 0.42)' }}>
          {mode === 'login' ? 'Login' : 'Register'}
        </div>
      </header>

      <section
        style={{
          width: 'min(480px, 100%)',
          borderRadius: '24px',
          padding: '40px 44px',
          background: '#141413',
          border: '0.5px solid #3A3A3A',
          fontSize: '20px',
          fontWeight: 500,
        }}
      >
        <form className="flex flex-col" style={{ gap: '18px' }} onSubmit={onSubmit}>
          {mode === 'register' && (
            <input
              className="w-full rounded-[12px] bg-[#30302e] text-[#FAF9F5] outline-none placeholder:text-[rgba(255,255,255,0.42)]"
              style={{ ...inputStyle, height: '54px', padding: '0 18px', fontSize: '15px', fontWeight: 500, fontFamily: 'inherit' }}
              type="text"
              placeholder="Enter your display name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              autoComplete="name"
            />
          )}

          <input
            className="w-full rounded-[12px] bg-[#30302e] text-[#FAF9F5] outline-none placeholder:text-[rgba(255,255,255,0.42)]"
            style={{ ...inputStyle, height: '54px', padding: '0 18px', fontSize: '15px', fontWeight: 500, fontFamily: 'inherit' }}
            type="text"
            placeholder="Enter your username"
            value={loginValue}
            onChange={(e) => setLoginValue(e.target.value)}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
          />

          <input
            className="w-full rounded-[12px] bg-[#30302e] text-[#FAF9F5] outline-none placeholder:text-[rgba(255,255,255,0.42)]"
            style={{ ...inputStyle, height: '54px', padding: '0 18px', fontSize: '15px', fontWeight: 500, fontFamily: 'inherit' }}
            type="password"
            placeholder="Enter your password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />

          <button
            type="submit"
            disabled={!canSubmit}
            style={{
              height: '56px',
              marginTop: '10px',
              borderRadius: '12px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '20px',
              fontWeight: 500,
              background: '#FAF9F6',
              color: '#141413',
            }}
            className="disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? '...' : 'Continue'}
          </button>
        </form>

        {error && <ErrorCallout error={error} />}
      </section>

      <button
        type="button"
        onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(null) }}
        style={{ fontSize: '14px', color: 'rgba(255, 255, 255, 0.42)', background: 'none', border: 'none', cursor: 'pointer' }}
      >
        {mode === 'login' ? '没有账号？注册' : '已有账号？登录'}
      </button>
    </div>
  )
}
