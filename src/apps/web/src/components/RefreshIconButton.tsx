import { useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { AnimatedCheck } from './AnimatedCheck'

type Phase = 'idle' | 'spinning' | 'done'

type Props = {
  onRefresh: () => void
  disabled?: boolean
  size?: number
  className?: string
  tooltip?: string
}

const SPIN_KEYFRAME = `@keyframes spin-once { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`

let styleInjected = false
function ensureStyle() {
  if (styleInjected) return
  const el = document.createElement('style')
  el.textContent = SPIN_KEYFRAME
  document.head.appendChild(el)
  styleInjected = true
}

export function RefreshIconButton({ onRefresh, disabled = false, size = 16, className, tooltip = 'Retry' }: Props) {
  const [phase, setPhase] = useState<Phase>('idle')
  const [pressed, setPressed] = useState(false)
  const [hovered, setHovered] = useState(false)

  const interactive = !disabled && phase === 'idle'

  const handlePointerDown = () => {
    if (!interactive) return
    setPressed(true)
  }

  const handlePointerUp = () => {
    if (!pressed) return
    setPressed(false)
    if (!interactive) return
    ensureStyle()
    onRefresh()
    setPhase('spinning')
  }

  const handleAnimationEnd = () => {
    setPhase('done')
    setTimeout(() => setPhase('idle'), 1500)
  }

  const handleMouseEnter = () => setHovered(true)
  const handleMouseLeave = () => {
    setHovered(false)
    setPressed(false)
  }

  const counterScale = pressed ? (1 / 0.96) : 1

  const iconSpanStyle: React.CSSProperties = phase === 'spinning'
    ? {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        transform: `scale(${counterScale})`,
        animation: 'spin-once 200ms ease-out forwards',
      }
    : {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        transform: `scale(${counterScale})`,
        transition: 'transform 80ms ease-out',
      }

  const showTooltip = hovered && phase === 'idle' && !disabled

  return (
    <span style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        disabled={disabled}
        className={className}
        style={{
          transform: pressed ? 'scale(0.96)' : 'scale(1)',
          transition: 'transform 80ms ease-out',
          opacity: disabled ? undefined : undefined,
        }}
      >
        <span style={iconSpanStyle} onAnimationEnd={phase === 'spinning' ? handleAnimationEnd : undefined}>
          {phase === 'done'
            ? <AnimatedCheck size={size} />
            : <RefreshCw size={size} />
          }
        </span>
      </button>
      <span
        style={{
          position: 'absolute',
          top: '100%',
          left: '50%',
          transform: showTooltip
            ? 'translateX(-50%) translateY(4px)'
            : 'translateX(-50%) translateY(0px)',
          marginTop: '3px',
          fontSize: '11px',
          fontWeight: 500,
          color: 'rgba(255,255,255,0.9)',
          background: 'rgba(0,0,0,0.75)',
          borderRadius: '5px',
          padding: '2px 7px',
          whiteSpace: 'nowrap',
          opacity: showTooltip ? 1 : 0,
          transition: 'opacity 120ms ease, transform 120ms ease',
          pointerEvents: 'none',
          userSelect: 'none',
          zIndex: 20,
        }}
      >
        {tooltip}
      </span>
    </span>
  )
}
