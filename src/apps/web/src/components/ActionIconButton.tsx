import { useState, type ButtonHTMLAttributes, type CSSProperties, type ReactNode } from 'react'

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  children: ReactNode
  tooltip?: string
  showTooltip?: boolean
  hoverBackground?: string
  pressedScale?: number
}

export function ActionIconButton({
  children,
  tooltip,
  showTooltip,
  hoverBackground,
  pressedScale = 0.92,
  className,
  style,
  disabled = false,
  onMouseEnter,
  onMouseLeave,
  onPointerDown,
  onPointerUp,
  onPointerLeave,
  ...props
}: Props) {
  const [hovered, setHovered] = useState(false)
  const [pressed, setPressed] = useState(false)
  const interactive = !disabled
  const tooltipVisible = showTooltip ?? (interactive && hovered && !!tooltip)

  const handleMouseEnter: React.MouseEventHandler<HTMLButtonElement> = (event) => {
    if (interactive) setHovered(true)
    onMouseEnter?.(event)
  }

  const handleMouseLeave: React.MouseEventHandler<HTMLButtonElement> = (event) => {
    setHovered(false)
    onMouseLeave?.(event)
  }

  const handlePointerDown: React.PointerEventHandler<HTMLButtonElement> = (event) => {
    if (interactive) setPressed(true)
    onPointerDown?.(event)
  }

  const handlePointerUp: React.PointerEventHandler<HTMLButtonElement> = (event) => {
    setPressed(false)
    onPointerUp?.(event)
  }

  const handlePointerLeave: React.PointerEventHandler<HTMLButtonElement> = (event) => {
    setPressed(false)
    onPointerLeave?.(event)
  }

  const buttonStyle: CSSProperties = {
    position: 'relative',
    display: 'inline-flex',
    cursor: interactive ? 'pointer' : 'default',
    ...style,
    transition: `background-color 60ms${style?.transition ? `, ${style.transition}` : ''}`,
  }

  const contentStyle: CSSProperties = {
    position: 'relative',
    zIndex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    transform: pressed ? `scale(${pressedScale})` : 'scale(1)',
    transition: 'transform 80ms ease-out',
  }

  const backgroundStyle: CSSProperties = {
    position: 'absolute',
    inset: 0,
    borderRadius: 'inherit',
    background: hovered && hoverBackground ? hoverBackground : style?.background,
    transform: pressed ? `scale(${pressedScale})` : 'scale(1)',
    transition: 'transform 80ms ease-out, background-color 60ms',
    pointerEvents: 'none',
  }

  return (
    <span style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        type="button"
        disabled={disabled}
        className={className}
        style={buttonStyle}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onPointerLeave={handlePointerLeave}
        {...props}
      >
        <span style={backgroundStyle} />
        <span style={contentStyle}>
          {children}
        </span>
      </button>
      {tooltip && (
        <span
          style={{
            position: 'absolute',
            top: '100%',
            left: '50%',
            transform: tooltipVisible
              ? 'translateX(-50%) translateY(0px)'
              : 'translateX(-50%) translateY(-3px)',
            marginTop: '3px',
            fontSize: '11px',
            fontWeight: 500,
            color: 'var(--c-tooltip-text)',
            background: 'var(--c-tooltip-bg)',
            border: '0.5px solid var(--c-tooltip-border)',
            borderRadius: '5px',
            padding: '2px 7px',
            whiteSpace: 'nowrap',
            opacity: tooltipVisible ? 1 : 0,
            transition: 'opacity 120ms ease, transform 120ms ease',
            pointerEvents: 'none',
            userSelect: 'none',
            zIndex: 20,
          }}
        >
          {tooltip}
        </span>
      )}
    </span>
  )
}
