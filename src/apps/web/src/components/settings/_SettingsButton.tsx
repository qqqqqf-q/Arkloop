import { useLayoutEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { Check, Copy } from 'lucide-react'

export type SettingsButtonVariant = 'primary' | 'secondary' | 'danger'
export type SettingsButtonSize = 'default' | 'modal'

export const settingsDangerText = 'var(--c-danger-action-text)'
export const settingsSecondaryFrameBorder =
  'color-mix(in srgb, var(--c-border) 91%, var(--c-text-primary) 9%)'

type SettingsButtonProps = {
  variant?: SettingsButtonVariant
  size?: SettingsButtonSize
  icon?: ReactNode
} & ButtonHTMLAttributes<HTMLButtonElement>

const sizeClasses: Record<SettingsButtonSize, string> = {
  default: 'h-[32px] rounded-[6.5px] px-3.5',
  modal: 'h-[35px] min-w-[54px] rounded-[7px] px-3',
}

const variantClasses: Record<SettingsButtonVariant, string> = {
  primary:
    'text-[var(--c-btn-text)] transition-[box-shadow] duration-150 hover:[box-shadow:inset_0_0_0_999px_rgba(255,255,255,0.07),0_0_0_0.2px_var(--c-btn-bg)] active:[box-shadow:inset_0_0_0_999px_rgba(0,0,0,0.04)]',
  secondary:
    'bg-[var(--c-bg-input)] text-[color-mix(in_srgb,var(--c-text-secondary)_72%,var(--c-text-primary)_28%)] [background-clip:padding-box] transition-colors duration-[180ms] hover:border-transparent hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
  danger:
    'bg-[var(--c-bg-input)] text-[var(--c-text-muted)] [background-clip:padding-box] transition-colors duration-[180ms] hover:border-transparent hover:bg-[var(--c-bg-deep)]',
}

export function SettingsButton({
  variant = 'secondary',
  size = 'default',
  icon,
  children,
  className,
  style,
  ...props
}: SettingsButtonProps) {
  const framed = variant !== 'primary'
  const hasChildren = children !== undefined && children !== null && children !== false && children !== ''

  return (
    <button
      type="button"
      className={[
        'inline-flex items-center justify-center gap-1.5 text-sm font-[450] disabled:cursor-not-allowed disabled:opacity-40 [&>svg]:h-4 [&>svg]:w-4 [&>svg]:shrink-0',
        sizeClasses[size],
        variantClasses[variant],
        variant === 'danger' ? 'hover:text-[var(--c-danger-action-text)]' : '',
        className,
      ].filter(Boolean).join(' ')}
      style={{
        ...(variant === 'primary' ? { background: 'var(--c-btn-bg)' } : undefined),
        ...(framed ? { border: `0.65px solid ${settingsSecondaryFrameBorder}` } : undefined),
        ...style,
      }}
      {...props}
    >
      {icon}
      {hasChildren && <span className="truncate">{children}</span>}
    </button>
  )
}

type SettingsIconButtonProps = {
  label: string
  danger?: boolean
  variant?: 'framed' | 'plain'
  children: ReactNode
} & ButtonHTMLAttributes<HTMLButtonElement>

export function SettingsIconButton({
  label,
  danger = false,
  variant = 'framed',
  children,
  className,
  style,
  ...props
}: SettingsIconButtonProps) {
  const framed = variant === 'framed'

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={[
        'inline-flex h-[32px] w-[32px] items-center justify-center rounded-[6.5px] text-[color-mix(in_srgb,var(--c-text-secondary)_72%,var(--c-text-primary)_28%)] transition-colors duration-[180ms] [&>svg]:h-4 [&>svg]:w-4 [&>svg]:shrink-0',
        framed
          ? 'bg-[var(--c-bg-input)] [background-clip:padding-box] hover:border-transparent hover:bg-[var(--c-bg-deep)]'
          : 'bg-transparent hover:bg-[color-mix(in_srgb,var(--c-bg-deep)_58%,transparent)]',
        danger ? 'hover:text-[var(--c-danger-action-text)]' : 'hover:text-[var(--c-text-primary)]',
        className,
      ].filter(Boolean).join(' ')}
      style={{
        ...(framed ? { border: `0.65px solid ${settingsSecondaryFrameBorder}` } : undefined),
        ...style,
      }}
      {...props}
    >
      {children}
    </button>
  )
}

export function SettingsCopyButton({
  label = 'Copy',
  copiedLabel = 'Copied',
}: {
  label?: string
  copiedLabel?: string
}) {
  const [copied, setCopied] = useState(false)
  const contentRef = useRef<HTMLSpanElement>(null)
  const [width, setWidth] = useState<number | null>(null)

  const handleCopyPreview = () => {
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  useLayoutEffect(() => {
    if (!contentRef.current) return
    setWidth(Math.ceil(contentRef.current.getBoundingClientRect().width))
  }, [copied])

  return (
    <button
      type="button"
      onClick={handleCopyPreview}
      className="inline-flex h-[32px] items-center justify-center overflow-hidden rounded-[6.5px] bg-[var(--c-bg-input)] text-sm font-[450] text-[color-mix(in_srgb,var(--c-text-secondary)_72%,var(--c-text-primary)_28%)] [background-clip:padding-box] transition-[width,background-color,border-color,color] duration-[220ms] ease-out hover:border-transparent hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
      style={{
        width: width === null ? undefined : width,
        border: `0.65px solid ${settingsSecondaryFrameBorder}`,
      }}
    >
      <span
        ref={contentRef}
        className="inline-flex items-center justify-center gap-1.5 whitespace-nowrap px-3.5"
      >
        {copied ? <Check size={14} /> : <Copy size={14} />}
        {copied ? copiedLabel : label}
      </span>
    </button>
  )
}
