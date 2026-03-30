import { useLayoutEffect, useRef, useState } from 'react'

export function TabBar<T extends string>({ tabs, active, onChange, className }: {
  tabs: { key: T; label: string }[]
  active: T
  onChange: (t: T) => void
  className?: string
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [pill, setPill] = useState({ left: 0, width: 0 })
  const [animate, setAnimate] = useState(false)

  useLayoutEffect(() => {
    const container = containerRef.current
    if (!container) return
    const btn = container.querySelector<HTMLButtonElement>(`[data-tab="${active}"]`)
    if (!btn) return
    setPill({ left: btn.offsetLeft, width: btn.offsetWidth })
  }, [active])

  // enable transition only after first paint
  useLayoutEffect(() => {
    const id = requestAnimationFrame(() => setAnimate(true))
    return () => cancelAnimationFrame(id)
  }, [])

  return (
    <div
      ref={containerRef}
      className={`relative mb-3 flex gap-0.5 rounded-[10px] p-[2px] ${className ?? ''}`}
      style={{ background: 'var(--c-mode-switch-track)' }}
    >
      {/* sliding pill */}
      <span
        className="pointer-events-none absolute top-[2px] bottom-[2px] rounded-[9px]"
        style={{
          left: pill.left,
          width: pill.width,
          background: 'var(--c-mode-switch-pill)',
          border: '0.5px solid var(--c-mode-switch-border)',
          transition: animate ? 'left 150ms, width 150ms' : 'none',
        }}
      />
      {tabs.map(tab => (
        <button
          key={tab.key}
          data-tab={tab.key}
          onClick={() => onChange(tab.key)}
          className="relative z-10 rounded-[9px] px-3.5 py-[5px] text-[12.5px] leading-[19px] transition-colors duration-200"
          style={{
            color: active === tab.key
              ? 'var(--c-mode-switch-active-text)'
              : 'var(--c-mode-switch-inactive-text)',
            fontWeight: 450,
            minWidth: '58px',
          }}
        >
          {tab.label}
        </button>
      ))}
    </div>
  )
}
