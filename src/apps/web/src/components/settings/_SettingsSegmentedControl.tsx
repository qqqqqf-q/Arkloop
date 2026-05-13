import { memo, type ReactNode } from 'react'
import { useLayoutEffect, useRef, useState } from 'react'

type Option<T extends string> = {
  value: T
  label: ReactNode
  title?: string
  ariaLabel?: string
}

function SettingsSegmentedControlImpl<T extends string>({
  value,
  options,
  onChange,
  density = 'default',
}: {
  value: T
  options: Array<Option<T>>
  onChange: (value: T) => void
  density?: 'default' | 'icon'
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [pill, setPill] = useState({ left: 0, width: 0 })
  const [animate, setAnimate] = useState(false)
  const initializedRef = useRef(false)
  const animationFrameRef = useRef<number | null>(null)

  useLayoutEffect(() => {
    const container = containerRef.current
    if (!container) return

    const measure = () => {
      const button = container.querySelector<HTMLButtonElement>(`[data-capsule="${value}"]`)
      if (!button) return
      const next = { left: button.offsetLeft, width: button.offsetWidth }
      if (next.width <= 0) return

      setPill((current) => (
        current.left === next.left && current.width === next.width ? current : next
      ))

      if (initializedRef.current) return
      setAnimate(false)
      if (animationFrameRef.current !== null) cancelAnimationFrame(animationFrameRef.current)
      animationFrameRef.current = requestAnimationFrame(() => {
        initializedRef.current = true
        setAnimate(true)
        animationFrameRef.current = null
      })
    }

    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(container)
    return () => {
      observer.disconnect()
      if (animationFrameRef.current !== null) {
        cancelAnimationFrame(animationFrameRef.current)
        animationFrameRef.current = null
      }
    }
  }, [value])

  return (
    <div
      ref={containerRef}
      className="relative inline-flex w-fit gap-0.5 rounded-[7.5px] bg-[var(--c-bg-deep)] p-[2px]"
    >
      <span
        className="pointer-events-none absolute bottom-[2px] top-[2px] rounded-[6.25px] border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] shadow-[0_1px_2px_rgba(0,0,0,0.04)]"
        style={{
          left: pill.left,
          width: pill.width,
          transition: animate
            ? 'left 180ms cubic-bezier(0.22, 1, 0.36, 1), width 180ms cubic-bezier(0.22, 1, 0.36, 1)'
            : 'none',
        }}
      />
      {options.map((option) => {
        const active = option.value === value
        return (
          <button
            key={option.value}
            type="button"
            data-capsule={option.value}
            title={option.title}
            aria-label={option.ariaLabel}
            onClick={() => onChange(option.value)}
            className={[
              'group relative z-10 overflow-hidden rounded-[6.25px] font-[450] transition-colors duration-[160ms]',
              density === 'icon'
                ? 'grid h-[30px] w-[36px] place-items-center p-0 text-[13px] leading-none'
                : 'px-[10px] py-[5px] text-[12.5px] leading-[19px]',
              active
                ? 'text-[var(--c-text-primary)]'
                : 'text-[var(--c-text-muted)] hover:text-[var(--c-text-primary)]',
            ].join(' ')}
          >
            {!active && (
              <span className="pointer-events-none absolute inset-0 bg-transparent transition-[background-color,box-shadow] duration-[160ms] ease-out group-hover:bg-[color-mix(in_srgb,var(--c-bg-deep)_90%,var(--c-text-primary)_10%)] group-hover:shadow-[inset_0_0_0_0.5px_var(--c-border-subtle)] group-active:bg-[color-mix(in_srgb,var(--c-bg-deep)_84%,var(--c-text-primary)_16%)] group-active:shadow-[inset_0_1px_2px_rgba(0,0,0,0.08)]" />
            )}
            <span className="relative z-10 inline-flex items-center justify-center">{option.label}</span>
          </button>
        )
      })}
    </div>
  )
}

export const SettingsSegmentedControl = memo(SettingsSegmentedControlImpl) as typeof SettingsSegmentedControlImpl
