import { useState, useEffect, useRef } from 'react'
import type { Tab } from './types'

export function TabBar({ tabs, active, onChange }: {
  tabs: { key: Tab; label: string }[]
  active: Tab
  onChange: (t: Tab) => void
}) {
  const barRef = useRef<HTMLDivElement>(null)
  const [indicator, setIndicator] = useState({ left: 0, width: 0 })

  useEffect(() => {
    const container = barRef.current
    if (!container) return
    const btn = container.querySelector<HTMLButtonElement>(`[data-tab="${active}"]`)
    if (!btn) return
    setIndicator({ left: btn.offsetLeft, width: btn.offsetWidth })
  }, [active])

  return (
    <div ref={barRef} className="relative mb-6 flex gap-1 border-b border-[var(--c-border-console)]">
      {tabs.map(tab => (
        <button
          key={tab.key}
          data-tab={tab.key}
          onClick={() => onChange(tab.key)}
          className={`relative px-4 py-2 text-sm transition-colors ${
            active === tab.key
              ? 'font-medium text-[var(--c-text-primary)]'
              : 'text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]'
          }`}
        >
          {tab.label}
        </button>
      ))}
      <span
        className="absolute bottom-0 h-0.5 bg-[var(--c-text-primary)] transition-all duration-200"
        style={{ left: indicator.left, width: indicator.width }}
      />
    </div>
  )
}
