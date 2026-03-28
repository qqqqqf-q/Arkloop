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
    primary: 'bg-[var(--c-btn-bg)] text-[var(--c-btn-text)] hover:opacity-90',
    ghost: 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)]',
    danger: 'bg-red-600 text-white hover:bg-red-700',
    outline: 'border border-[var(--c-border)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)]',
  }[variant]

  const sizeCls = {
    sm: 'px-3 py-1.5 text-xs rounded-lg',
    md: 'px-4 py-2 text-sm rounded-lg',
  }[size]

  return (
    <button
      type="button"
      disabled={disabled || loading}
      className={`font-medium transition-opacity disabled:opacity-50 inline-flex items-center gap-2 ${variantCls} ${sizeCls} ${className}`}
      {...props}
    >
      {loading && <SpinnerIcon />}
      {children}
    </button>
  )
}
