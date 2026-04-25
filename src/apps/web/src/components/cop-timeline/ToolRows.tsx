import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { FileOpRef } from '../../storage'
import type { ExploreGroupRef } from '../../toolPresentation'
import { COP_TIMELINE_CONTENT_PADDING_LEFT_PX, TypewriterText } from './utils'
import { CopTimelineUnifiedRow } from './CopUnifiedRow'

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'
const EXPLORE_VIEWPORT_BOTTOM_PAD = 10
const TIMELINE_ROW_TITLE_STYLE = {
  color: 'var(--c-cop-row-fg, var(--c-text-secondary))',
  fontSize: 13,
  fontWeight: 400,
  lineHeight: '18px',
} as const

function basename(path: string): string {
  return path.replace(/\\/g, '/').split('/').filter(Boolean).pop() ?? path
}

function previewLines(text: string | undefined, limit = 18): string[] {
  if (!text?.trim()) return []
  return text.replace(/\r\n/g, '\n').split('\n').slice(0, limit)
}

function statusColor(status: FileOpRef['status']): string {
  if (status === 'running') return 'var(--c-cop-row-fg, var(--c-text-secondary))'
  return 'var(--c-cop-row-fg, var(--c-text-secondary))'
}

function ToolTitle({ title, live, status }: { title: string; live?: boolean; status?: FileOpRef['status'] }) {
  return (
    <span
      style={{
        ...TIMELINE_ROW_TITLE_STYLE,
        color: status ? statusColor(status) : TIMELINE_ROW_TITLE_STYLE.color,
        display: 'block',
        minWidth: 0,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
        paddingBlock: 1,
        marginBlock: -1,
      }}
    >
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
    <div style={{ maxWidth: 'min(100%, 760px)', minWidth: 0 }}>
      <button
        type="button"
        onClick={() => expandable && setExpanded((value) => !value)}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          minWidth: 0,
          border: 'none',
          padding: 0,
          background: 'transparent',
          cursor: expandable ? 'pointer' : 'default',
          ...TIMELINE_ROW_TITLE_STYLE,
        }}
      >
        <ToolTitle title={title} live={live && op.status === 'running'} status={op.status} />
        {op.displaySubject && !title.includes(op.displaySubject) && (
          <span style={{ color: 'var(--c-text-muted)', fontSize: 12, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{op.displaySubject}</span>
        )}
        {expandable && (expanded ? <ChevronDown size={12} style={{ flexShrink: 0 }} /> : <ChevronRight size={12} style={{ flexShrink: 0 }} />)}
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

export function ExploreTimelineRow({ group, live, segmentLive }: { group: ExploreGroupRef; live?: boolean; segmentLive?: boolean }) {
  const reduceMotion = useReducedMotion()
  const [expanded, setExpanded] = useState(false)
  const [metrics, setMetrics] = useState({ fullHeight: 0, previewHeight: 0, previewOffset: 0 })
  const [viewportAnimating, setViewportAnimating] = useState(false)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const itemRefs = useRef(new Map<string, HTMLDivElement>())
  const hasItems = group.items.length > 0
  const hasPreviewItems = group.items.length > 2

  const measureMetrics = useCallback(() => {
    const content = contentRef.current
    if (!content) return
    const firstPreviewItem = group.items.at(-2)
    const firstPreviewNode = firstPreviewItem ? itemRefs.current.get(firstPreviewItem.id) : undefined
    const fullHeight = content.scrollHeight
    const previewOffset = hasPreviewItems && firstPreviewNode ? firstPreviewNode.offsetTop : 0
    const previewHeight = hasPreviewItems ? Math.max(0, fullHeight - previewOffset) : fullHeight
    setMetrics((current) => (
      current.fullHeight === fullHeight && current.previewHeight === previewHeight && current.previewOffset === previewOffset
        ? current
        : { fullHeight, previewHeight, previewOffset }
    ))
  }, [group.items, hasPreviewItems])

  useLayoutEffect(() => {
    measureMetrics()
  }, [measureMetrics])

  useLayoutEffect(() => {
    const content = contentRef.current
    if (!content) return
    const resizeObserver = new ResizeObserver(measureMetrics)
    resizeObserver.observe(content)
    return () => resizeObserver.disconnect()
  }, [measureMetrics])

  const displayMode: 'full' | 'preview' | 'closed' = expanded ? 'full' : segmentLive ? 'preview' : 'closed'
  const viewportHeight = displayMode === 'full'
    ? metrics.fullHeight
    : displayMode === 'preview'
      ? metrics.previewHeight
      : 0
  const viewportTargetHeight = displayMode === 'full' && !viewportAnimating ? 'auto' : viewportHeight
  const contentY = displayMode === 'preview' ? -metrics.previewOffset : 0
  const viewportTransition = reduceMotion
    ? { duration: 0 }
    : { duration: 0.24, ease: [0.4, 0, 0.2, 1] as const }

  const toggleExpanded = () => {
    if (!hasItems) return
    setViewportAnimating(true)
    setExpanded((value) => !value)
  }

  return (
    <div style={{ maxWidth: 'min(100%, 760px)', minWidth: 0 }}>
      <button
        type="button"
        onClick={toggleExpanded}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          minWidth: 0,
          border: 'none',
          padding: 0,
          background: 'transparent',
          cursor: hasItems ? 'pointer' : 'default',
          ...TIMELINE_ROW_TITLE_STYLE,
        }}
      >
        <ToolTitle title={group.label} live={live && group.status === 'running'} status={group.status} />
        {hasItems && (expanded ? <ChevronDown size={12} style={{ flexShrink: 0 }} /> : <ChevronRight size={12} style={{ flexShrink: 0 }} />)}
      </button>

      <motion.div
        initial={false}
        animate={{ height: viewportTargetHeight, opacity: displayMode === 'closed' ? 0 : 1 }}
        transition={viewportTransition}
        onAnimationStart={() => setViewportAnimating(true)}
        onAnimationComplete={() => setViewportAnimating(false)}
        style={{
          overflow: displayMode === 'full' && !viewportAnimating ? 'visible' : 'hidden',
        }}
      >
        <motion.div
          ref={contentRef}
          initial={false}
          animate={{ y: contentY }}
          transition={viewportTransition}
          style={{
            position: 'relative',
            paddingTop: 6,
            paddingLeft: COP_TIMELINE_CONTENT_PADDING_LEFT_PX,
            paddingBottom: EXPLORE_VIEWPORT_BOTTOM_PAD,
          }}
        >
          {group.items.map((op, index) => (
            <div
              key={op.id}
              ref={(node) => {
                if (node) itemRefs.current.set(op.id, node)
                else itemRefs.current.delete(op.id)
              }}
              style={{ position: 'relative' }}
            >
              <CopTimelineUnifiedRow
                isFirst={index === 0}
                isLast={index === group.items.length - 1}
                multiItems={group.items.length >= 2}
                dotColor={statusColor(op.status)}
                paddingBottom={8}
                horizontalMotion={false}
              >
                <FileOpToolRow op={op} live={live} />
              </CopTimelineUnifiedRow>
            </div>
          ))}
        </motion.div>
      </motion.div>
    </div>
  )
}
