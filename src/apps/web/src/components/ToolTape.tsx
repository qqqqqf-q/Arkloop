import type { ReactNode } from 'react'
import type { ShortcutBinding } from '../shortcuts'
import { formatShortcut } from '../shortcuts'

type ShortcutHintProps = {
  binding: ShortcutBinding
  className?: string
}

type ToolTapeProps = {
  label: ReactNode
  shortcut?: ShortcutBinding
}

export function ShortcutHint({ binding, className }: ShortcutHintProps) {
  const keys = formatShortcut(binding)
  return (
    <span className={['inline-flex items-center gap-px', className].filter(Boolean).join(' ')}>
      {keys.map((part, index) => (
        <kbd
          key={`${part}-${index}`}
          className={[
            'inline-flex h-[18px] items-center justify-center bg-[color-mix(in_srgb,var(--c-tooltip-text)_14%,var(--c-tooltip-bg))] text-[11.5px] font-[var(--c-fw-375)] leading-[18px] text-[var(--c-tooltip-text)]',
            part.length > 1 ? 'min-w-[18px] px-1' : 'w-[18px]',
            keycapRadiusClass(index, keys.length),
          ].join(' ')}
        >
          <span className={shortcutSymbolClass(part)}>{part}</span>
        </kbd>
      ))}
    </span>
  )
}

function shortcutSymbolClass(part: string): string {
  return part === '⌘' || part === '⇧' || part === '⌥' ? 'translate-y-[0.5px] text-[14px]' : ''
}

function keycapRadiusClass(index: number, count: number): string {
  if (count === 1) return 'rounded-[4px]'
  if (index === 0) return 'rounded-l-[4px] rounded-r-[0.8px]'
  if (index === count - 1) return 'rounded-l-[0.8px] rounded-r-[4px]'
  return 'rounded-[0.8px]'
}

export function ToolTape({ label, shortcut }: ToolTapeProps) {
  return (
    <span className="flex items-center gap-3">
      <span className="text-[13px] font-[var(--c-fw-375)]">{label}</span>
      {shortcut && <ShortcutHint binding={shortcut} />}
    </span>
  )
}
