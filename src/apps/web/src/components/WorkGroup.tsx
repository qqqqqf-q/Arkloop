import { useState, useCallback } from 'react'
import { motion } from 'framer-motion'
import { ChevronRight } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'

type WorkGroupProps = {
  durationMs: number
  children: React.ReactNode
}

function formatDuration(ms: number): string {
  const totalSec = Math.round(ms / 1000)
  const minutes = Math.floor(totalSec / 60)
  const seconds = totalSec % 60
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`
  }
  return `${seconds}s`
}

export function WorkGroup({ durationMs, children }: WorkGroupProps) {
  const { t } = useLocale()
  const [expanded, setExpanded] = useState(false)
  const [hovered, setHovered] = useState(false)

  const toggle = useCallback(() => setExpanded((v) => !v), [])

  const durationLabel = durationMs > 0 ? formatDuration(durationMs) : null

  return (
    <div className="work-group-root">
      <button
        type="button"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        onClick={toggle}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '3px 0 3px',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: hovered ? 'var(--c-cop-row-hover-fg, var(--c-text-primary))' : 'var(--c-cop-row-fg, var(--c-text-tertiary))',
          fontSize: 'var(--c-cop-row-font-size, 14px)',
          fontWeight: 'var(--c-cop-row-weight, 400)',
          lineHeight: 'var(--c-cop-row-line-height, 20px)',
          transition: 'color 0.15s ease',
          maxWidth: '100%',
          minWidth: 0,
        }}
      >
        <span>
          {t.worked}
          {durationLabel ? ` ${durationLabel}` : ''}
        </span>
        <motion.div
          animate={{ rotate: expanded ? 90 : 0 }}
          transition={{ duration: 0.2, ease: 'easeOut' }}
          style={{ display: 'flex', flexShrink: 0 }}
        >
          <ChevronRight size={13} />
        </motion.div>
      </button>
      <motion.div
        initial={false}
        animate={{ height: expanded ? 'auto' : 0, opacity: expanded ? 1 : 0 }}
        transition={{ duration: 0.24, ease: [0.4, 0, 0.2, 1] }}
        style={{ overflow: expanded ? 'visible' : 'hidden' }}
      >
        <div
          style={{
            borderTop: '1px solid var(--c-border-subtle)',
            marginBottom: '8px',
          }}
        />
        {children}
      </motion.div>
    </div>
  )
}
