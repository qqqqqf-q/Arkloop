import { createPortal } from 'react-dom'
import { Check } from 'lucide-react'

const SETUP_TEXT_COLOR = 'rgb(64, 117, 208)'
const SETUP_HOVER_BG = 'rgb(231, 239, 251)'

export type SlashCommandItem = {
  id: string
  label: string
  description: string
}

export type SlashCommandGroup = {
  label: string
  items: SlashCommandItem[]
}

type Props = {
  groups: SlashCommandGroup[]
  selectedIndex: number
  position: { left: number; bottom: number }
  onSelect: (item: SlashCommandItem) => void
  onMouseEnter: (flatIndex: number) => void
}

export function SlashCommandPopup({
  groups,
  selectedIndex,
  position,
  onSelect,
  onMouseEnter,
}: Props) {
  return createPortal(
    <div
      data-slash-popup
      className="dropdown-menu-up"
      style={{
        position: 'fixed',
        left: position.left,
        bottom: `calc(100vh - ${position.bottom}px + 8px)`,
        zIndex: 80,
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        padding: '4px',
        background: 'var(--c-bg-menu)',
        width: '300px',
        maxHeight: 'min(320px, calc(100vh - 120px))',
        overflowY: 'auto',
        boxShadow: 'var(--c-dropdown-shadow)',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
        {groups.map((group, groupIndex) => (
          <div key={group.label}>
            <div
              style={{
                padding: '6px 12px 3px 10px',
                fontSize: '11px',
                fontWeight: 500,
                color: 'var(--c-text-muted)',
                letterSpacing: '0.01em',
                userSelect: 'none',
              }}
            >
              {group.label}
            </div>
            {group.items.map((item, itemIndex) => {
              const currentIndex = groups
                .slice(0, groupIndex)
                .reduce((count, previousGroup) => count + previousGroup.items.length, itemIndex)
              const selected = currentIndex === selectedIndex
              const isSetup = item.id === 'setup'

              return (
                <button
                  key={item.id}
                  type="button"
                  onMouseEnter={() => onMouseEnter(currentIndex)}
                  onMouseDown={(event) => {
                    event.preventDefault()
                  }}
                  onClick={() => {
                    onSelect(item)
                  }}
                  className="flex w-full items-center gap-2 rounded-lg py-[6px] pl-3 pr-2 text-left text-sm"
                  style={{
                    background: selected ? (isSetup ? SETUP_HOVER_BG : 'var(--c-bg-deep)') : undefined,
                    border: 'none',
                    color: isSetup
                      ? SETUP_TEXT_COLOR
                      : selected
                        ? 'var(--c-text-primary)'
                        : 'var(--c-text-secondary)',
                    fontWeight: 450,
                  }}
                >
                  <span
                    style={{
                      flex: 1,
                      minWidth: 0,
                      display: 'grid',
                      gridTemplateColumns: 'max-content minmax(0, 1fr)',
                      alignItems: 'baseline',
                      gap: '12px',
                    }}
                  >
                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {item.label}
                    </span>
                    <span
                      style={{
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        color: isSetup ? SETUP_TEXT_COLOR : 'var(--c-text-muted)',
                        opacity: isSetup ? 0.78 : undefined,
                        fontSize: '13px',
                        fontWeight: 400,
                      }}
                    >
                      {item.description}
                    </span>
                  </span>
                  {selected && (
                    <Check size={13} strokeWidth={1.75} style={{ color: isSetup ? SETUP_TEXT_COLOR : 'var(--c-text-muted)', flexShrink: 0 }} />
                  )}
                </button>
              )
            })}
          </div>
        ))}
      </div>
    </div>,
    document.body,
  )
}
