import type { InputHTMLAttributes } from 'react'

type Props = {
  variant?: 'sm' | 'md'
} & InputHTMLAttributes<HTMLInputElement>

const base =
  'w-full border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 text-sm ' +
  'text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] ' +
  'focus:border-[var(--c-border)]'

const sizes = {
  sm: 'rounded-md py-1.5',
  md: 'rounded-lg py-2',
} as const

// eslint-disable-next-line react-refresh/only-export-components
export function settingsInputCls(variant: 'sm' | 'md' = 'sm') {
  return `${base} ${sizes[variant]}`
}

export function SettingsInput({ variant = 'sm', className, ...rest }: Props) {
  return (
    <input
      className={`${settingsInputCls(variant)}${className ? ` ${className}` : ''}`}
      {...rest}
    />
  )
}
