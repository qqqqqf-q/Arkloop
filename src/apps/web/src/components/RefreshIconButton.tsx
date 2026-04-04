import { useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { AnimatedCheck } from './AnimatedCheck'
import { ActionIconButton } from './ActionIconButton'

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
  const [hovered, setHovered] = useState(false)

  const interactive = !disabled && phase === 'idle'

  const handleClick = () => {
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
  }

  const iconSpanStyle: React.CSSProperties = phase === 'spinning'
    ? {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        animation: 'spin-once 200ms ease-out forwards',
      }
    : {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }

  const showTooltip = hovered && phase === 'idle' && !disabled

  return (
    <ActionIconButton
      onClick={handleClick}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      disabled={disabled}
      className={className}
      tooltip={tooltip}
      showTooltip={showTooltip}
      hoverBackground="var(--c-bg-deep)"
    >
        <span style={iconSpanStyle} onAnimationEnd={phase === 'spinning' ? handleAnimationEnd : undefined}>
          {phase === 'done'
            ? <AnimatedCheck size={size} />
            : <RefreshCw size={size} />
          }
        </span>
    </ActionIconButton>
  )
}
