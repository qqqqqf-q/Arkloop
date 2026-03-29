import type { ReactNode, RefObject } from 'react'
import { isApiError } from '../api/client'
import type { AppError } from './ErrorCallout'

export function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

export function EyeIcon({ open }: { open: boolean }) {
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

export function normalizeError(error: unknown, fallback: string): AppError {
  if (isApiError(error)) {
    return {
      message: error.message,
      traceId: error.traceId,
      code: error.code,
      details: error.details as Record<string, unknown> | undefined,
    }
  }
  if (error instanceof Error) return { message: error.message }
  return { message: fallback }
}

export function Reveal({ active, children }: { active: boolean; children: ReactNode }) {
  return (
    <div style={{
      display: 'grid',
      gridTemplateRows: active ? '1fr' : '0fr',
      opacity: active ? 1 : 0,
      transition: 'grid-template-rows 0.5s cubic-bezier(0.16,1,0.3,1), opacity 0.4s cubic-bezier(0.16,1,0.3,1)',
    }}>
      <div style={{ overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

export const TRANSITION = '0.42s cubic-bezier(0.4,0,0.2,1)'

export const inputCls = 'w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]'

export const inputStyle = {
  border: '0.5px solid var(--c-border-auth)',
  height: '36px',
  padding: '0 14px',
  fontSize: '13px',
  fontWeight: 500,
  fontFamily: 'inherit',
} as const

export const labelStyle = {
  fontSize: '11px',
  fontWeight: 500 as const,
  color: 'var(--c-placeholder)',
  paddingLeft: '2px',
  marginBottom: '4px',
  display: 'block',
} as const

interface PasswordEyeProps {
  inputRef: RefObject<HTMLInputElement | null>
  placeholder: string
  value: string
  onChange: (v: string) => void
  showPassword: boolean
  onToggleShow: () => void
  autoComplete?: string
}

export function PasswordEye({ inputRef, placeholder, value, onChange, showPassword, onToggleShow, autoComplete = 'current-password' }: PasswordEyeProps) {
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
        autoComplete={autoComplete}
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

export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div style={{ minHeight: '100vh', background: 'var(--c-bg-page)', display: 'flex', flexDirection: 'column', position: 'relative', overflow: 'hidden' }}>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', padding: 'max(48px, calc(50vh - 140px)) 20px 48px', position: 'relative', zIndex: 1 }}>
        <section style={{ width: 'min(400px, 100%)' }}>
          {children}
        </section>
      </div>
      <footer style={{ textAlign: 'center', padding: '16px', fontSize: '12px', color: 'var(--c-text-muted)', position: 'relative', zIndex: 1 }}>
        &copy; 2026 Arkloop
      </footer>
    </div>
  )
}
