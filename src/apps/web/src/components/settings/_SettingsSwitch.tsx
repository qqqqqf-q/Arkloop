import type { ChangeEvent } from 'react'

type Props = {
  checked: boolean
  onChange: (next: boolean) => void
  disabled?: boolean
  forceHover?: boolean
  size?: 'default' | 'sm'
}

const sizes = {
  default: { target: 32, track: 36, thumb: 16, gap: 2, travel: 16 },
  sm: { target: 28, track: 28, thumb: 12, gap: 2, travel: 12 },
} as const

export function SettingsSwitch({ checked, onChange, disabled, forceHover, size = 'default' }: Props) {
  const s = sizes[size]
  const handleChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange(event.target.checked)
  }

  return (
    <label
      className={[
        'group/switch relative inline-flex shrink-0 items-center',
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
      ].join(' ')}
      style={{ height: s.target }}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={handleChange}
        className="peer sr-only"
      />
      <span
        className={[
          'block rounded-full transition-[background-color,box-shadow] duration-[180ms]',
          !disabled
            ? 'group-hover/switch:[box-shadow:0_0_0_1px_color-mix(in_srgb,var(--c-accent)_68%,transparent)]'
            : '',
          forceHover && !disabled ? '[box-shadow:0_0_0_1px_color-mix(in_srgb,var(--c-accent)_68%,transparent)]' : '',
        ].join(' ')}
        style={{
          width: s.track,
          height: s.thumb + s.gap * 2,
          background: checked
            ? 'var(--c-btn-bg)'
            : 'color-mix(in srgb, var(--c-border-mid) 74%, var(--c-bg-input) 26%)',
        }}
      />
      <span
        className="absolute rounded-full transition-[transform,background-color] duration-[180ms] ease-[cubic-bezier(.2,.8,.2,1)]"
        style={{
          width: s.thumb,
          height: s.thumb,
          top: (s.target - s.thumb) / 2,
          left: s.gap,
          background: checked ? 'var(--c-btn-text)' : 'var(--c-bg-input)',
          transform: checked ? `translateX(${s.travel}px)` : 'translateX(0)',
          boxShadow: '0 0.5px 1px rgba(0,0,0,0.18)',
        }}
      />
    </label>
  )
}
