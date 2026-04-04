import { useState, useRef, useEffect } from 'react'
import { Copy } from 'lucide-react'
import { AnimatedCheck } from './AnimatedCheck'
import { ActionIconButton } from './ActionIconButton'

type Phase = 'idle' | 'exiting' | 'entering' | 'active' | 'exiting-back' | 'entering-back'

type Props = {
  onCopy: () => void
  onCopied?: (active: boolean) => void
  size?: number
  className?: string
  style?: React.CSSProperties
  resetDelay?: number
  tooltip?: string
  hoverBackground?: string
  onMouseEnter?: React.MouseEventHandler<HTMLButtonElement>
  onMouseLeave?: React.MouseEventHandler<HTMLButtonElement>
}

export function CopyIconButton({
  onCopy,
  onCopied,
  size = 16,
  className,
  style,
  resetDelay = 1500,
  tooltip = 'Copy',
  hoverBackground,
  onMouseEnter,
  onMouseLeave,
}: Props) {
  const [phase, setPhase] = useState<Phase>('idle')
  const [hovered, setHovered] = useState(false)
  const hoveredRef = useRef(false)
  const resetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pendingResetRef = useRef(false)

  useEffect(() => () => { if (resetTimerRef.current) clearTimeout(resetTimerRef.current) }, [])

  const triggerReset = () => {
    setPhase('exiting-back')
    setTimeout(() => {
      setPhase('entering-back')
      setTimeout(() => {
        setPhase('idle')
        onCopied?.(false)
      }, 75)
    }, 60)
  }

  const handleClick = () => {
    if (phase !== 'idle') return
    onCopy()
    onCopied?.(true)
    setPhase('exiting')
    setTimeout(() => {
      setPhase('entering')
      setTimeout(() => {
        setPhase('active')
        if (resetTimerRef.current) clearTimeout(resetTimerRef.current)
        resetTimerRef.current = setTimeout(() => {
          if (hoveredRef.current) {
            pendingResetRef.current = true
          } else {
            triggerReset()
          }
        }, resetDelay)
      }, 75)
    }, 60)
  }

  const handleMouseEnter: React.MouseEventHandler<HTMLButtonElement> = (e) => {
    hoveredRef.current = true
    setHovered(true)
    onMouseEnter?.(e)
  }

  const handleMouseLeave: React.MouseEventHandler<HTMLButtonElement> = (e) => {
    hoveredRef.current = false
    setHovered(false)
    onMouseLeave?.(e)
    if (pendingResetRef.current) {
      pendingResetRef.current = false
      triggerReset()
    }
  }

  const iconStyle = (): React.CSSProperties => {
    if (phase === 'exiting' || phase === 'exiting-back') {
      return { transform: 'scale(0.5)', opacity: 0, transition: 'transform 60ms ease-in, opacity 60ms ease-in' }
    }
    if (phase === 'entering' || phase === 'entering-back') {
      return { transform: 'scale(0.5)', opacity: 0 }
    }
    if (phase === 'active' || phase === 'idle') {
      return { transform: 'scale(1)', opacity: 1, transition: 'transform 75ms ease-out, opacity 50ms ease-out' }
    }
    return {}
  }

  const showCheck = phase === 'entering' || phase === 'active' || phase === 'exiting-back'
  const showTooltip = hovered && phase === 'idle'

  return (
    <ActionIconButton
      onClick={handleClick}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      tooltip={tooltip}
      showTooltip={showTooltip}
      hoverBackground={hoverBackground}
      className={className}
      style={style}
    >
      <span style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        ...iconStyle(),
      }}>
        {showCheck
          ? <AnimatedCheck size={size} />
          : <Copy size={size} />
        }
      </span>
    </ActionIconButton>
  )
}
