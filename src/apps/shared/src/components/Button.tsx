import type { ButtonHTMLAttributes } from 'react'
import { SpinnerIcon } from './auth-ui'

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'ghost' | 'danger' | 'outline'
  size?: 'sm' | 'md'
  loading?: boolean
}

export function Button({
  variant = 'primary',
  size = 'md',
  loading = false,
  className = '',
  children,
  disabled,
  ...props
}: ButtonProps) {
  const variantCls = {
    primary: 'bg-[var(--c-btn-bg)] text-[var(--c-btn-text)] hover:[box-shadow:inset_0_0_0_999px_rgba(255,255,255,0.07),0_0_0_0.2px_var(--c-btn-bg)]',
    ghost: 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]',
    danger: 'bg-[var(--c-bg-input)] text-[var(--c-danger-action-text)] [background-clip:padding-box] hover:border-transparent hover:bg-[var(--c-bg-deep)]',
    outline: 'border-[0.65px] border-[color-mix(in_srgb,var(--c-border)_91%,var(--c-text-primary)_9%)] bg-[var(--c-bg-input)] text-[color-mix(in_srgb,var(--c-text-secondary)_72%,var(--c-text-primary)_28%)] [background-clip:padding-box] hover:border-transparent hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
  }[variant]

  const sizeCls = {
    sm: 'h-[32px] rounded-[6.5px] px-3.5 text-sm',
    md: 'h-[35px] rounded-[7px] px-3 text-sm',
  }[size]

  return (
    <button
      type="button"
      disabled={disabled || loading}
      className={`inline-flex items-center justify-center gap-1.5 font-[450] transition-colors duration-[180ms] disabled:cursor-not-allowed disabled:opacity-40 ${variantCls} ${sizeCls} ${className}`}
      {...props}
    >
      {loading && <SpinnerIcon />}
      {children}
    </button>
  )
}
