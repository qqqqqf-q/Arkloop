import type { ReactNode } from 'react'
import { motion } from 'framer-motion'
import {
  COP_TIMELINE_LINE_LEFT_PX,
  COP_TIMELINE_DOT_SIZE,
  COP_TIMELINE_DOT_LEFT_PX,
  COP_TIMELINE_DOT_TOP,
} from './utils'

/** 与 unified 列表项同一套点线（ChatPage 顶层工具条等复用） */
export function CopTimelineUnifiedRow({
  isFirst,
  isLast,
  multiItems,
  dotTop = COP_TIMELINE_DOT_TOP,
  dotColor,
  paddingBottom = 7,
  horizontalMotion = true,
  children,
}: {
  isFirst: boolean
  isLast: boolean
  multiItems: boolean
  dotTop?: number
  dotColor: string
  paddingBottom?: number
  horizontalMotion?: boolean
  children: ReactNode
}) {
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
            top: `${dotTop + COP_TIMELINE_DOT_SIZE}px`,
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
            height: `${dotTop}px`,
            width: '1.5px',
            background: 'var(--c-border-subtle)',
            zIndex: 0,
          }}
        />
      )}
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
      {children}
    </motion.div>
  )
}
