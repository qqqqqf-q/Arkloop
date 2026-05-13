import type { ReactNode } from 'react'
import { motion } from 'framer-motion'
import {
  COP_TIMELINE_LINE_LEFT_PX,
  COP_TIMELINE_DOT_SIZE,
  COP_TIMELINE_DOT_LEFT_PX,
  COP_TIMELINE_DOT_TOP,
} from './utils'
import type { TimelineMarker } from './markers'

/** 与 unified 列表项同一套点线（ChatPage 顶层工具条等复用） */
export function CopTimelineUnifiedRow({
  isFirst,
  isLast,
  multiItems,
  dotTop = COP_TIMELINE_DOT_TOP,
  dotColor,
  paddingBottom = 10,
  horizontalMotion = true,
  marker = { kind: 'dot' },
  children,
}: {
  isFirst: boolean
  isLast: boolean
  multiItems: boolean
  dotTop?: number
  dotColor: string
  paddingBottom?: number
  horizontalMotion?: boolean
  marker?: TimelineMarker
  children: ReactNode
}) {
  const markerBoxSize = 16
  const markerTop = dotTop - 4
  const lineBelowTop = marker.kind === 'icon'
    ? markerTop + markerBoxSize
    : dotTop + COP_TIMELINE_DOT_SIZE
  const lineAboveHeight = marker.kind === 'icon'
    ? Math.max(0, markerTop)
    : dotTop
  return (
    <motion.div
      initial={{ opacity: 0, x: horizontalMotion ? -8 : 0 }}
      animate={{ opacity: 1, x: 0 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.3, ease: 'easeOut' }}
      style={{ position: 'relative', paddingBottom: isLast ? 0 : paddingBottom }}
    >
      {!isLast && (
        <div
          style={{
            position: 'absolute',
            left: `${COP_TIMELINE_LINE_LEFT_PX}px`,
            top: `${lineBelowTop}px`,
            bottom: 0,
            width: '1.5px',
            background: 'var(--c-border-subtle)',
            zIndex: 0,
          }}
        />
      )}
      {multiItems && !isFirst && (
        <div
          style={{
            position: 'absolute',
            left: `${COP_TIMELINE_LINE_LEFT_PX}px`,
            top: 0,
            height: `${lineAboveHeight}px`,
            width: '1.5px',
            background: 'var(--c-border-subtle)',
            zIndex: 0,
          }}
        />
      )}
      {marker.kind === 'icon' ? (
        <div
          title={marker.label}
          aria-label={marker.label}
          style={{
            position: 'absolute',
            left: `${COP_TIMELINE_LINE_LEFT_PX - markerBoxSize / 2}px`,
            top: `${markerTop}px`,
            width: `${markerBoxSize}px`,
            height: `${markerBoxSize}px`,
            background: 'var(--c-bg-page)',
            color: dotColor,
            zIndex: 1,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <marker.icon width={12} height={12} strokeWidth={2.1} />
        </div>
      ) : (
        <div
          style={{
            position: 'absolute',
            left: `${COP_TIMELINE_DOT_LEFT_PX}px`,
            top: `${dotTop}px`,
            width: `${COP_TIMELINE_DOT_SIZE}px`,
            height: `${COP_TIMELINE_DOT_SIZE}px`,
            borderRadius: '50%',
            background: dotColor,
            border: '2px solid var(--c-bg-page)',
            zIndex: 1,
          }}
        />
      )}
      {children}
    </motion.div>
  )
}
