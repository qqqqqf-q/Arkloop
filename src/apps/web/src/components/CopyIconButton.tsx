import { useState, useRef, useEffect } from 'react'
import { Copy } from 'lucide-react'
import { AnimatedCheck } from './AnimatedCheck'

// idle         → 显示 Copy
// exiting      → 旧图标缩小消失
// entering     → 新图标从小弹出
// active       → 显示 AnimatedCheck，鼠标在按钮上时锁定不可再按
// exiting-back → Check 缩小消失
// entering-back→ Copy 从小弹出
type Phase = 'idle' | 'exiting' | 'entering' | 'active' | 'exiting-back' | 'entering-back'

type Props = {
  onCopy: () => void
  onCopied?: (active: boolean) => void
  size?: number
  className?: string
  style?: React.CSSProperties
  resetDelay?: number
  tooltip?: string
  onMouseEnter?: React.MouseEventHandler<HTMLButtonElement>
  onMouseLeave?: React.MouseEventHandler<HTMLButtonElement>
}

export function CopyIconButton({ onCopy, onCopied, size = 16, className, style, resetDelay = 1500, tooltip = 'Copy', onMouseEnter, onMouseLeave }: Props) {
  const [phase, setPhase] = useState<Phase>('idle')
  const [pressed, setPressed] = useState(false)
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

  const handlePointerDown = () => {
    // 无论 phase，都响应缩小
    setPressed(true)
  }

  const handlePointerUp = () => {
    if (!pressed) return
    setPressed(false)
    // 只有 idle 时才触发复制
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
    setPressed(false)
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

  // 按钮缩小时，内部 span 反向补偿，使图标保持原始大小
  const counterScale = pressed ? (1 / 0.96) : 1
  const iStyle = iconStyle()
  const spanTransform = iStyle.transform
    ? `scale(${counterScale}) ${iStyle.transform}`
    : `scale(${counterScale})`

  const showCheck = phase === 'entering' || phase === 'active' || phase === 'exiting-back'
  // tooltip 只在 idle 且 hover 时显示
  const showTooltip = hovered && phase === 'idle'

  return (
    <span style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        className={className}
        style={{
          ...style,
          transform: pressed ? 'scale(0.96)' : 'scale(1)',
          transition: 'transform 80ms ease-out',
        }}
      >
        <span style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          ...iStyle,
          transform: spanTransform,
          transition: [
            pressed ? undefined : 'transform 80ms ease-out',
            iStyle.transition,
          ].filter(Boolean).join(', ') || undefined,
        }}>
          {showCheck
            ? <AnimatedCheck size={size} />
            : <Copy size={size} />
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
