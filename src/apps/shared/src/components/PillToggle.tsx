import { useState } from 'react'

type Props = {
  checked: boolean
  onChange: (next: boolean) => void
  disabled?: boolean
  forceHover?: boolean
  size?: 'default' | 'sm'
}

const sizes = {
  default: { track: 36, thumb: 16, gap: 2, travel: 16 },
  sm: { track: 28, thumb: 12, gap: 2, travel: 12 },
} as const

export function PillToggle({ checked, onChange, disabled, forceHover, size = 'default' }: Props) {
  const [hovered, setHovered] = useState(false)
  const showRing = (hovered || forceHover) && !disabled
  const s = sizes[size]

  return (
    <label
      className={`relative inline-flex shrink-0 items-center ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'}`}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="peer sr-only"
      />
      <span
        className="block rounded-full"
        style={{
          width: s.track,
          height: s.thumb + s.gap * 2,
          background: checked ? 'var(--c-btn-bg)' : 'var(--c-border-mid)',
          boxShadow: showRing ? '0 0 0 1.5px var(--c-accent)' : '0 0 0 0px var(--c-accent)',
          transition: 'background-color 200ms, box-shadow 200ms',
        }}
      />
      <span
        className="absolute rounded-full"
        style={{
          width: s.thumb,
          height: s.thumb,
          top: s.gap,
          left: s.gap,
          background: checked ? 'var(--c-btn-text)' : 'var(--c-bg-page)',
          transform: checked ? `translateX(${s.travel}px)` : 'translateX(0)',
          transition: 'transform 200ms, background-color 200ms',
        }}
      />
    </label>
  )
}
