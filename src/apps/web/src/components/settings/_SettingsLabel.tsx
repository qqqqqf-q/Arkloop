import type { LabelHTMLAttributes, ReactNode } from 'react'

type Props = {
  size?: 'sm' | 'md'
  children: ReactNode
} & Omit<LabelHTMLAttributes<HTMLLabelElement>, 'children'>

const sizes = {
  sm: 'mb-1 block text-xs font-medium text-[var(--c-text-secondary)]',
  md: 'mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]',
} as const

// eslint-disable-next-line react-refresh/only-export-components
export function settingsLabelCls(size: 'sm' | 'md' = 'sm') {
  return sizes[size]
}

export function SettingsLabel({ size = 'sm', className, children, ...rest }: Props) {
  return (
    <label
      className={`${settingsLabelCls(size)}${className ? ` ${className}` : ''}`}
      {...rest}
    >
      {children}
    </label>
  )
}
