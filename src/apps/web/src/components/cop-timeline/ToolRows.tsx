import { useMemo, useState } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { FileOpRef } from '../../storage'
import type { ExploreGroupRef } from '../../toolPresentation'
import { COP_TIMELINE_CONTENT_PADDING_LEFT_PX, TypewriterText } from './utils'
import { CopTimelineUnifiedRow } from './CopUnifiedRow'

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'

function basename(path: string): string {
  return path.replace(/\\/g, '/').split('/').filter(Boolean).pop() ?? path
}

function previewLines(text: string | undefined, limit = 18): string[] {
  if (!text?.trim()) return []
  return text.replace(/\r\n/g, '\n').split('\n').slice(0, limit)
}

function statusColor(status: FileOpRef['status']): string {
  if (status === 'failed') return 'var(--c-status-error-text, #ef4444)'
  if (status === 'running') return 'var(--c-text-secondary)'
  return 'var(--c-text-tertiary)'
}

function ToolTitle({ title, live, status }: { title: string; live?: boolean; status?: FileOpRef['status'] }) {
  return (
    <span style={{ color: status ? statusColor(status) : 'var(--c-text-tertiary)' }}>
      <TypewriterText text={title} live={live} className={live ? 'thinking-shimmer-dim' : undefined} />
    </span>
  )
}

export function FileOpToolRow({ op, live }: { op: FileOpRef; live?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const title = op.displayDescription || op.label || op.toolName
  const filePath = op.filePath || op.displayDetail || ''
  const lines = previewLines(op.output || op.errorMessage)
  const cardTitle = op.pattern || op.displaySubject || (filePath ? basename(filePath) : title)
  const cardSubtitle = filePath && cardTitle !== filePath ? filePath : op.displayDetail || ''
  const expandable = !!(filePath || lines.length > 0 || op.pattern || op.operation)

  return (
    <div style={{ maxWidth: 'min(100%, 760px)' }}>
      <button
        type="button"
        onClick={() => expandable && setExpanded((value) => !value)}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          border: 'none',
          padding: 0,
          background: 'transparent',
          cursor: expandable ? 'pointer' : 'default',
          fontSize: 13,
          lineHeight: '18px',
          color: 'var(--c-text-tertiary)',
        }}
      >
        <ToolTitle title={title} live={live && op.status === 'running'} status={op.status} />
        {op.displaySubject && !title.includes(op.displaySubject) && (
          <span style={{ color: 'var(--c-text-muted)', fontSize: 12 }}>{op.displaySubject}</span>
        )}
        {expandable && (expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />)}
      </button>

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
            style={{ overflow: 'hidden' }}
          >
            <div style={{ marginTop: 4, borderRadius: 8, background: 'var(--c-attachment-bg)', overflow: 'hidden', border: '0.5px solid var(--c-border-subtle)' }}>
              {(cardTitle || cardSubtitle) && (
                <div style={{ padding: '8px 10px', fontFamily: MONO, fontSize: 12, color: 'var(--c-text-secondary)', background: 'var(--c-bg-menu)', borderBottom: '0.5px solid var(--c-border-subtle)', display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
                  <span style={{ fontWeight: 600, color: 'var(--c-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cardTitle}</span>
                  {cardSubtitle && <span style={{ color: 'var(--c-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>· {cardSubtitle}</span>}
                </div>
              )}
              {lines.length > 0 ? (
                <pre style={{ margin: 0, padding: '9px 10px', maxHeight: 280, overflow: 'auto', fontFamily: MONO, fontSize: 12, lineHeight: '18px', color: op.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                  {lines.map((line, index) => `${String(index + 1).padStart(3, ' ')}  ${line}`).join('\n')}
                </pre>
              ) : (
                <div style={{ padding: '8px 10px', fontSize: 12, color: 'var(--c-text-muted)' }}>
                  {op.pattern || op.operation || basename(filePath) || op.toolName}
                </div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function ExploreActivity({ op, live }: { op: FileOpRef; live?: boolean }) {
  return (
    <div style={{ fontSize: 13, lineHeight: '18px', color: statusColor(op.status), whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
      <TypewriterText text={op.displayDescription || op.label || op.toolName} live={live && op.status === 'running'} className={live && op.status === 'running' ? 'thinking-shimmer-dim' : undefined} />
    </div>
  )
}

export function ExploreTimelineRow({ group, live }: { group: ExploreGroupRef; live?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const visibleItems = useMemo(() => expanded ? group.items : group.items.slice(-2), [expanded, group.items])
  const hasExpandableItems = group.items.length > 0

  const toggleExpanded = () => {
    if (!hasExpandableItems) return
    setExpanded((value) => !value)
  }

  return (
    <div style={{ maxWidth: 'min(100%, 760px)' }}>
      <button
        type="button"
        onClick={toggleExpanded}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          border: 'none',
          padding: 0,
          background: 'transparent',
          cursor: hasExpandableItems ? 'pointer' : 'default',
          fontSize: 13,
          lineHeight: '18px',
          color: 'var(--c-text-tertiary)',
        }}
      >
        <ToolTitle title={group.label} live={live && group.status === 'running'} status={group.status} />
        {hasExpandableItems && (expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />)}
      </button>

      <motion.div
        layout
        transition={{ layout: { duration: 0.22, ease: [0.4, 0, 0.2, 1] } }}
        style={{
          position: 'relative',
          marginTop: 6,
          overflow: 'hidden',
          paddingLeft: COP_TIMELINE_CONTENT_PADDING_LEFT_PX,
        }}
      >
        <AnimatePresence initial={false} mode="popLayout">
          {visibleItems.map((op, index) => (
            <motion.div
              key={op.id}
              layout
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              transition={{ duration: 0.22, ease: 'easeOut' }}
              style={{ position: 'relative' }}
            >
              <CopTimelineUnifiedRow
                isFirst={index === 0}
                isLast={index === visibleItems.length - 1}
                multiItems={visibleItems.length >= 2}
                dotColor={statusColor(op.status)}
                paddingBottom={expanded ? 8 : 4}
                horizontalMotion={false}
              >
                {expanded ? <FileOpToolRow op={op} live={live} /> : <ExploreActivity op={op} live={live} />}
              </CopTimelineUnifiedRow>
            </motion.div>
          ))}
        </AnimatePresence>
      </motion.div>
    </div>
  )
}
