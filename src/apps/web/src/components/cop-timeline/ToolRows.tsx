import { createContext, memo, useCallback, useContext, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import hljs from 'highlight.js/lib/common'
import type { FileOpRef } from '../../storage'
import { CopThoughtSummaryRow } from './ThinkingBlock'
import { basename, type ExploreGroupRef } from '../../toolPresentation'
import { useLocale } from '../../contexts/LocaleContext'
import { COP_TIMELINE_CONTENT_PADDING_LEFT_PX, TypewriterText } from './utils'
import { CopTimelineUnifiedRow } from './CopUnifiedRow'
import { localizeTimelineLabel } from './labels'
import { markerForFileOp } from './markers'
import { renderTimelineText } from '../../timelineText'

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'
const EXPLORE_VIEWPORT_BOTTOM_PAD = 12
const TIMELINE_ROW_TITLE_STYLE = {
  color: 'var(--c-cop-row-fg, var(--c-text-secondary))',
  fontSize: 'var(--c-cop-row-font-size)',
  fontWeight: 400,
  lineHeight: 'var(--c-cop-row-line-height)',
} as const

const LocalExpansionScrollContext = createContext<((trigger?: HTMLElement | null) => void) | null>(null)

export function CopTimelineLocalExpansionProvider({ stabilizeScroll, children }: { stabilizeScroll?: (trigger?: HTMLElement | null) => void; children: React.ReactNode }) {
  return (
    <LocalExpansionScrollContext.Provider value={stabilizeScroll ?? null}>
      {children}
    </LocalExpansionScrollContext.Provider>
  )
}

function useLocalExpansionScrollStabilizer() {
  return useContext(LocalExpansionScrollContext)
}

export function summarizeDiff(text: string | undefined): { added: number; removed: number } | null {
  if (!text) return null
  let added = 0
  let removed = 0
  for (const line of text.replace(/\r\n/g, '\n').split('\n')) {
    if (line.startsWith('+++') || line.startsWith('---')) continue
    if (line.startsWith('+')) added += 1
    else if (line.startsWith('-')) removed += 1
  }
  return added > 0 || removed > 0 ? { added, removed } : null
}


const EXT_TO_LANG: Record<string, string> = {
  ts: 'typescript', tsx: 'typescript', mts: 'typescript', cts: 'typescript',
  js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  go: 'go', py: 'python', rb: 'ruby', rs: 'rust', java: 'java', kt: 'kotlin',
  c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', cs: 'csharp', swift: 'swift',
  php: 'php', lua: 'lua', sh: 'bash', bash: 'bash', zsh: 'bash',
  json: 'json', yaml: 'yaml', yml: 'yaml', toml: 'ini', ini: 'ini',
  css: 'css', scss: 'scss', html: 'xml', xml: 'xml', svg: 'xml',
  md: 'markdown', sql: 'sql', dockerfile: 'dockerfile',
}

function languageFromPath(path: string): string | undefined {
  const name = basename(path).toLowerCase()
  if (name === 'dockerfile') return 'dockerfile'
  const ext = name.includes('.') ? name.split('.').pop() : ''
  return ext ? EXT_TO_LANG[ext] : undefined
}

function highlightCode(code: string, lang: string | undefined): string {
  try {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value
    }
    return hljs.highlightAuto(code).value
  } catch {
    return ''
  }
}

function previewLines(text: string | undefined, limit = 30): string[] {
  if (!text?.trim()) return []
  return text.replace(/\r\n/g, '\n').split('\n').slice(0, limit)
}

const DEFAULT_FILEOP_COLOR = 'var(--c-cop-row-fg)'

function ToolTitle({ title, live, status: _status, highlightedSuffix }: { title: string; live?: boolean; status?: FileOpRef['status']; highlightedSuffix?: string }) {
  const head = highlightedSuffix && title.endsWith(highlightedSuffix)
    ? title.slice(0, title.length - highlightedSuffix.length)
    : title
  const tail = highlightedSuffix && title.endsWith(highlightedSuffix) ? highlightedSuffix : ''
  return (
    <span
      style={{
        ...TIMELINE_ROW_TITLE_STYLE,
        color: 'inherit',
        minWidth: 0,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      }}
    >
      {tail ? (
        <>
          <TypewriterText text={head} live={live} className={live ? 'thinking-shimmer-dim' : undefined} />
          <span style={{ color: 'var(--c-text-primary)' }}>{tail}</span>
        </>
      ) : (
        <TypewriterText text={title} live={live} className={live ? 'thinking-shimmer-dim' : undefined} />
      )}
    </span>
  )
}

function fileOpDiffCounts(op: FileOpRef): { added: number; removed: number } | null {
  if (op.status === 'running') return null
  if (typeof op.diffAdded === 'number' || typeof op.diffRemoved === 'number') {
    const added = Math.max(0, op.diffAdded ?? 0)
    const removed = Math.max(0, op.diffRemoved ?? 0)
    return added > 0 || removed > 0 ? { added, removed } : null
  }
  return summarizeDiff(op.output || op.errorMessage)
}

function FileOpDiffSuffix({ added, removed }: { added: number; removed: number }) {
  return (
    <span
      aria-label={`${added} added${removed > 0 ? `, ${removed} removed` : ''}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        flexShrink: 0,
        fontVariantNumeric: 'tabular-nums',
        fontSize: 12,
        lineHeight: '16px',
      }}
    >
      {added > 0 && (
        <motion.span
          key={`added-${added}`}
          className="cop-diff-added"
          initial={{ y: 5, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -5, opacity: 0 }}
          transition={{ duration: 0.16, ease: 'easeOut' }}
        >
          +{added}
        </motion.span>
      )}
      {removed > 0 && (
        <motion.span
          key={`removed-${removed}`}
          className="cop-diff-removed"
          initial={{ y: 5, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -5, opacity: 0 }}
          transition={{ duration: 0.16, ease: 'easeOut' }}
        >
          -{removed}
        </motion.span>
      )}
    </span>
  )
}

export function FileOpToolCard({ op }: { op: FileOpRef }) {
  const { locale } = useLocale()
  const title = op.displayText ? renderTimelineText(op.displayText, locale) : localizeTimelineLabel(op.displayDescription || op.label || op.toolName, locale)
  const filePath = op.filePath || op.displayDetail || ''
  const lines = previewLines(op.output || op.errorMessage)
  const cardTitle = op.pattern || op.displaySubject || (filePath ? basename(filePath) : title)
  const cardSubtitle = filePath && cardTitle !== filePath ? filePath : op.displayDetail || ''
  const diffCounts = fileOpDiffCounts(op)

  return (
    <div style={{ marginTop: 4, borderRadius: 8, background: 'var(--c-attachment-bg)', overflow: 'hidden', border: '0.5px solid var(--c-border-subtle)' }}>
      {(cardTitle || cardSubtitle || diffCounts) && (
        <div style={{ padding: '8px 10px', fontFamily: MONO, fontSize: 12, color: 'var(--c-text-secondary)', background: 'var(--c-bg-menu)', borderBottom: '0.5px solid var(--c-border-subtle)', display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
          <span style={{ fontWeight: 600, color: 'var(--c-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cardTitle}</span>
          {cardSubtitle && <span style={{ color: 'var(--c-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>· {cardSubtitle}</span>}
          {diffCounts && <FileOpDiffSuffix added={diffCounts.added} removed={diffCounts.removed} />}
        </div>
      )}
      {lines.length > 0 ? (
        <pre style={{ margin: 0, padding: '9px 0', maxHeight: 280, overflow: 'auto', fontFamily: MONO, fontSize: 12, lineHeight: '18px', color: 'var(--c-text-secondary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
          {lines
            .filter((line) => {
              const t = line.trim()
              if (!t) return false
              if (t.startsWith('--- ') || t.startsWith('+++ ') || t === '---' || t === '+++') return false
              if (t.startsWith('@@') && t.includes('@@')) return false
              if (t.startsWith('diff --git') || t.startsWith('index ')) return false
              return true
            })
            .map((line, lineIndex) => {
              let bg: string | undefined
              if (line.startsWith('+')) bg = 'rgba(34,197,94,0.12)'
              else if (line.startsWith('-')) bg = 'rgba(239,68,68,0.12)'
              return (
                <div key={lineIndex} style={{ padding: '0 10px', background: bg }}>
                  {`${String(lineIndex + 1).padStart(3, ' ')}  ${line}`}
                </div>
              )
            })}
        </pre>
      ) : (
        <div style={{ padding: '8px 10px', fontSize: 12, color: 'var(--c-text-muted)' }}>
          {op.pattern || op.operation || basename(filePath) || op.toolName}
        </div>
      )}
    </div>
  )
}


type FileOpToolRowProps = { op: FileOpRef; live?: boolean; expandedOffsetLeft?: number }

function areFileOpToolRowPropsEqual(prev: FileOpToolRowProps, next: FileOpToolRowProps): boolean {
  return prev.op === next.op && prev.live === next.live && prev.expandedOffsetLeft === next.expandedOffsetLeft
}

export const FileOpToolRow = memo(function FileOpToolRow({ op, live, expandedOffsetLeft = 0 }: FileOpToolRowProps) {
  const { locale } = useLocale()
  const [expanded, setExpanded] = useState(false)
  const [hovered, setHovered] = useState(false)
  const stabilizeLocalExpansionScroll = useLocalExpansionScrollStabilizer()
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const title = op.displayText ? renderTimelineText(op.displayText, locale) : localizeTimelineLabel(op.displayDescription || op.label || op.toolName, locale)
  const filePath = op.filePath || op.displayDetail || ''
  const lines = useMemo(() => previewLines(op.output || op.errorMessage), [op.output, op.errorMessage])
  const cardTitle = op.pattern || op.displaySubject || (filePath ? basename(filePath) : title)
  const cardSubtitle = filePath && cardTitle !== filePath ? filePath : op.displayDetail || ''
  const expandable = !!(filePath || lines.length > 0 || op.pattern || op.operation)
  const isReadFile = op.toolName === 'read_file' || op.toolName === 'read' || op.toolName.startsWith('read.')
  const fileNameForTitle = isReadFile && filePath ? basename(filePath) : ''
  const codeText = useMemo(() => {
    if (!isReadFile || lines.length === 0 || op.status === 'running') return null
    return lines.map((line, index) => `${String(index + 1).padStart(3, ' ')}  ${line}`).join('\n')
  }, [isReadFile, lines, op.status])
  const language = useMemo(() => languageFromPath(filePath), [filePath])
  const codeBody = useMemo(() => {
    if (!codeText) return null
    const html = highlightCode(codeText, language)
    return html || null
  }, [codeText, language])
  const numberedText = useMemo(() => (
    lines.map((line, index) => `${String(index + 1).padStart(3, ' ')}  ${line}`).join('\n')
  ), [lines])

  return (
    <div style={{ maxWidth: 'min(100%, 760px)', minWidth: 0 }}>
      <button
        type="button"
        ref={triggerRef}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        onClick={() => {
          if (!expandable) return
          stabilizeLocalExpansionScroll?.(triggerRef.current)
          setExpanded((value) => !value)
        }}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          minWidth: 0,
          border: 'none',
          padding: 0,
          background: 'transparent',
          cursor: expandable ? 'pointer' : 'default',
          ...TIMELINE_ROW_TITLE_STYLE,
          color: hovered ? 'var(--c-cop-row-hover-fg)' : 'var(--c-cop-row-fg)',
          transition: 'color 0.15s ease',
        }}
      >
        <ToolTitle title={title} live={live && op.status === 'running'} status={op.status} highlightedSuffix={fileNameForTitle} />
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
            style={{
              overflow: 'hidden',
              marginLeft: expandedOffsetLeft,
              width: expandedOffsetLeft < 0 ? `calc(100% + ${Math.abs(expandedOffsetLeft)}px)` : undefined,
            }}
          >
            <div style={{ marginTop: 4, borderRadius: 8, background: 'var(--c-code-preview-bg)', overflow: 'hidden', border: '0.5px solid var(--c-border-subtle)' }}>
              {(cardTitle || cardSubtitle) && (
                <div style={{ padding: '8px 10px', fontFamily: MONO, fontSize: 12, color: 'var(--c-text-secondary)', background: 'var(--c-bg-menu)', borderBottom: '0.5px solid var(--c-border-subtle)', display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
                  <span style={{ fontWeight: 600, color: 'var(--c-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cardTitle}</span>
                  {cardSubtitle && <span style={{ color: 'var(--c-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>· {cardSubtitle}</span>}
                </div>
              )}
              {lines.length > 0 ? (
                codeBody ? (
                  <pre
                    className="hljs"
                    style={{ margin: 0, padding: '9px 10px', maxHeight: 280, overflow: 'auto', fontFamily: MONO, fontSize: 12, lineHeight: '18px', background: 'var(--c-code-preview-bg)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
                  >
                    <code className={`language-${language ?? 'plaintext'}`} dangerouslySetInnerHTML={{ __html: codeBody }} />
                  </pre>
                ) : (
                  <pre style={{ margin: 0, padding: '9px 10px', maxHeight: 280, overflow: 'auto', fontFamily: MONO, fontSize: 12, lineHeight: '18px', color: op.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                    {numberedText}
                  </pre>
                )
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
}, areFileOpToolRowPropsEqual)

export function ExploreTimelineRow({ group, live, segmentLive, headerVariant = 'tool', attachedThinkingRows }: { group: ExploreGroupRef; live?: boolean; segmentLive?: boolean; headerVariant?: 'tool' | 'segment'; attachedThinkingRows?: Array<{ id: string; markdown: string; live?: boolean; durationSec?: number; startedAtMs?: number }> }) {
  const { locale } = useLocale()
  const reduceMotion = useReducedMotion()
  const [expanded, setExpanded] = useState(false)
  const [hovered, setHovered] = useState(false)
  const stabilizeLocalExpansionScroll = useLocalExpansionScrollStabilizer()
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const [metrics, setMetrics] = useState({ fullHeight: 0, previewHeight: 0, previewOffset: 0 })
  const [viewportAnimating, setViewportAnimating] = useState(false)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const itemRefs = useRef(new Map<string, HTMLDivElement>())
  const hasItems = group.items.length > 0
  const previewCount = segmentLive ? Math.min(2, group.items.length) : 0

  const measureMetrics = useCallback(() => {
    const content = contentRef.current
    if (!content) return
    const firstPreviewItem = previewCount > 0 ? group.items.at(-previewCount) : undefined
    const firstPreviewNode = firstPreviewItem ? itemRefs.current.get(firstPreviewItem.id) : undefined
    const fullHeight = content.scrollHeight
    const previewOffset = previewCount > 0 && firstPreviewNode ? firstPreviewNode.offsetTop : 0
    const previewHeight = previewCount > 0 ? Math.max(0, fullHeight - previewOffset) : 0
    setMetrics((current) => (
      current.fullHeight === fullHeight && current.previewHeight === previewHeight && current.previewOffset === previewOffset
        ? current
        : { fullHeight, previewHeight, previewOffset }
    ))
  }, [group.items, previewCount])

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
    stabilizeLocalExpansionScroll?.(triggerRef.current)
    setViewportAnimating(true)
    setExpanded((value) => !value)
  }

  return (
    <div style={{ maxWidth: 'min(100%, 760px)', minWidth: 0 }}>
      <button
        ref={triggerRef}
        type="button"
        onClick={toggleExpanded}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          minWidth: 0,
          border: 'none',
          padding: headerVariant === 'segment' ? '4px 0 2px' : 0,
          background: 'transparent',
          cursor: hasItems ? 'pointer' : 'default',
          ...TIMELINE_ROW_TITLE_STYLE,
          color: headerVariant === 'segment'
            ? (hovered ? 'var(--c-cop-row-hover-fg, var(--c-cop-row-fg))' : 'var(--c-cop-row-fg)')
            : TIMELINE_ROW_TITLE_STYLE.color,
          transition: 'color 0.15s ease',
        }}
      >
        {headerVariant === 'segment' ? (
          <span
            style={{
              display: 'block',
              minWidth: 0,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              paddingBlock: 1,
              marginBlock: -1,
              color: 'inherit',
              fontSize: 'var(--c-cop-row-font-size)',
              fontWeight: 400,
              lineHeight: 'var(--c-cop-row-line-height)',
            }}
          >
            <TypewriterText text={group.text ? renderTimelineText(group.text, locale) : localizeTimelineLabel(group.label, locale)} live={live && group.status === 'running'} className={live && group.status === 'running' ? 'thinking-shimmer-dim' : undefined} />
          </span>
        ) : (
          <ToolTitle title={group.text ? renderTimelineText(group.text, locale) : localizeTimelineLabel(group.label, locale)} live={live && group.status === 'running'} status={group.status} />
        )}
        {hasItems && (expanded ? <ChevronDown size={headerVariant === 'segment' ? 13 : 12} style={{ flexShrink: 0, color: 'currentColor' }} /> : <ChevronRight size={headerVariant === 'segment' ? 13 : 12} style={{ flexShrink: 0, color: 'currentColor' }} />)}
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
                dotColor={DEFAULT_FILEOP_COLOR}
                paddingBottom={10}
                horizontalMotion={false}
                marker={markerForFileOp(op)}
              >
                <FileOpToolRow op={op} live={live} />
              </CopTimelineUnifiedRow>
            </div>
          ))}
        </motion.div>
      </motion.div>
      <AnimatePresence initial={false}>
        {expanded && attachedThinkingRows && attachedThinkingRows.length > 0 && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            style={{ overflow: 'hidden' }}
          >
            <div style={{ paddingTop: 6, paddingLeft: COP_TIMELINE_CONTENT_PADDING_LEFT_PX }}>
              {attachedThinkingRows.map((row) => (
                <CopThoughtSummaryRow
                  key={row.id}
                  markdown={row.markdown}
                  live={!!row.live}
                  thoughtDurationSeconds={row.durationSec ?? 0}
                  startedAtMs={row.startedAtMs}
                />
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
