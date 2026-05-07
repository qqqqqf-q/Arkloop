import { Search } from 'lucide-react'
import { forwardRef } from 'react'
import type { InputHTMLAttributes } from 'react'

type Props = {
  variant?: 'sm' | 'md'
} & InputHTMLAttributes<HTMLInputElement>

const base =
  'w-full border-[0.65px] [border-color:color-mix(in_srgb,var(--c-border)_64%,var(--c-bg-input)_36%)] ' +
  'bg-[var(--c-bg-input)] text-sm font-[450] text-[var(--c-text-primary)] outline-none ' +
  'placeholder:font-[350] placeholder:text-[var(--c-text-muted)] transition-colors duration-[180ms] ' +
  'hover:[border-color:color-mix(in_srgb,var(--c-border)_72%,var(--c-text-primary)_28%)] ' +
  'focus:[border-color:color-mix(in_srgb,var(--c-border)_72%,var(--c-text-primary)_28%)]'

const sizes = {
  sm: 'h-[32px] rounded-[6.5px] px-3',
  md: 'h-[35px] rounded-[6.5px] px-3.5',
} as const

// eslint-disable-next-line react-refresh/only-export-components
export function settingsInputCls(variant: 'sm' | 'md' = 'sm') {
  return `${base} ${sizes[variant]}`
}

export const SettingsInput = forwardRef<HTMLInputElement, Props>(function SettingsInput(
  { variant = 'sm', className, ...rest },
  ref,
) {
  return (
    <input
      ref={ref}
      className={`${settingsInputCls(variant)}${className ? ` ${className}` : ''}`}
      {...rest}
    />
  )
})

export function SettingsSearchInput({
  placeholder = 'Search',
  className,
  ...rest
}: Omit<Props, 'variant'>) {
  return (
    <div className="relative">
      <Search
        size={14}
        className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
      />
      <SettingsInput
        className={`pl-8${className ? ` ${className}` : ''}`}
        placeholder={placeholder}
        {...rest}
      />
    </div>
  )
}
