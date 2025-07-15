import { useState, useRef, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, ChevronDown, Check, Loader2, Copy } from 'lucide-react'

type Status = 'running' | 'success' | 'failed'

type Props = {
  toolName: string
  label: string
  output?: string
  status: Status
  errorMessage?: string
}

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'
const expandTransition = { duration: 0.25, ease: [0.4, 0, 0.2, 1] as const }

type ScrollEdge = 'top' | 'bottom' | 'both' | 'none'

function maskFor(edge: ScrollEdge): string | undefined {
  if (edge === 'top') return 'linear-gradient(to bottom, black 85%, transparent)'
  if (edge === 'bottom') return 'linear-gradient(to top, black 85%, transparent)'
  if (edge === 'both') return 'linear-gradient(to bottom, transparent, black 12%, black 88%, transparent)'
  return undefined
}

function toolKindLabel(toolName: string): string {
  switch (toolName) {
    case 'grep': return 'grep'
    case 'glob': return 'glob'
    case 'read_file': return 'read'
    case 'write_file': return 'write'
    case 'edit':
    case 'edit_file': return 'edit'
    default: return toolName
  }
}

export function FileOpBlock({ toolName, label, output, status, errorMessage }: Props) {
  const [expanded, setExpanded] = useState(false)
  const [outHovered, setOutHovered] = useState(false)
  const [copied, setCopied] = useState(false)
  const outputRef = useRef<HTMLDivElement>(null)
  const [scrollEdge, setScrollEdge] = useState<ScrollEdge>('none')

  const displayOutput = output && output.trim()
    ? output
    : errorMessage && errorMessage.trim()
      ? errorMessage
      : undefined
  const hasOutput = !!displayOutput
  const expandable = hasOutput || status === 'running'

  const copyOutput = useCallback(() => {
    if (!displayOutput) return
    void navigator.clipboard.writeText(displayOutput)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }, [displayOutput])

  useEffect(() => {
    const el = outputRef.current
    if (!el || !expanded) return
    const update = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      if (scrollHeight <= clientHeight + 1) { setScrollEdge('none'); return }
      const atTop = scrollTop <= 1
      const atBottom = scrollTop + clientHeight >= scrollHeight - 1
      if (atTop && atBottom) setScrollEdge('none')
      else if (atTop) setScrollEdge('top')
      else if (atBottom) setScrollEdge('bottom')
      else setScrollEdge('both')
    }
    update()
    el.addEventListener('scroll', update, { passive: true })
    return () => el.removeEventListener('scroll', update)
  }, [expanded, displayOutput])

  const mask = maskFor(scrollEdge)

  return (
    <div style={{ maxWidth: 'min(100%, 720px)' }}>
      <div
        role="button"
        tabIndex={0}
        onClick={() => { if (expandable) setExpanded((p) => !p) }}
        onKeyDown={(e) => { if ((e.key === 'Enter' || e.key === ' ') && expandable) { e.preventDefault(); setExpanded((p) => !p) } }}
        className="shell-exec-trigger"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '5px',
          padding: '4px 0',
          border: 'none',
          cursor: expandable ? 'pointer' : 'default',
          width: 'fit-content',
          maxWidth: '100%',
          background: 'transparent',
          userSelect: 'none',
          WebkitUserSelect: 'none',
        }}
      >
        <span
          className="shell-exec-label"
          style={{
            fontSize: '11px',
            fontFamily: MONO,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            lineHeight: '16px',
            color: 'var(--c-text-muted)',
            transition: 'color 150ms ease',
          }}
        >
          {label}
        </span>
        {expandable && (
          expanded
            ? <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
            : <ChevronRight size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
        )}
      </div>

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={expandTransition}
            style={{ overflow: 'hidden' }}
          >
            <div style={{ borderRadius: '8px', background: 'var(--c-bg-menu)', overflow: 'hidden', marginTop: '4px' }}>
              <div style={{ padding: '6px 10px 2px', fontSize: '10px', color: 'var(--c-text-muted)', fontFamily: MONO, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                {toolKindLabel(toolName)}
              </div>

              <div style={{ padding: '2px 10px 8px' }}>
                <pre style={{
                  margin: 0,
                  fontSize: '11px',
                  lineHeight: '1.5',
                  color: 'var(--c-text-primary)',
                  fontFamily: MONO,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  maxHeight: '80px',
                  overflowY: 'auto',
                }}>
                  {label}
                </pre>
              </div>

              {(hasOutput || status === 'running') && (
                <div
                  style={{ position: 'relative' }}
                  onMouseEnter={() => setOutHovered(true)}
                  onMouseLeave={() => setOutHovered(false)}
                >
                  <AnimatePresence>
                    {outHovered && hasOutput && (
                      <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        style={{ position: 'absolute', top: '4px', right: '6px', zIndex: 1 }}
                      >
                        <CopyBtn copied={copied} onClick={copyOutput} />
                      </motion.div>
                    )}
                  </AnimatePresence>
                  <div
                    ref={outputRef}
                    style={{
                      maxHeight: '240px',
                      overflowY: 'auto',
                      padding: '4px 10px 8px',
                      paddingRight: outHovered ? '34px' : '10px',
                      maskImage: mask,
                      WebkitMaskImage: mask,
                    }}
                  >
                    {hasOutput ? (
                      <pre style={{
                        margin: 0,
                        fontSize: '10.5px',
                        lineHeight: '1.4',
                        color: status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)',
                        fontFamily: MONO,
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-word',
                      }}>
                        {displayOutput!.trimEnd()}
                      </pre>
                    ) : (
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '20px' }}>
                        <Loader2 size={12} className="animate-spin" style={{ color: 'var(--c-text-muted)' }} />
                      </div>
                    )}
                  </div>
                </div>
              )}

              {!hasOutput && status !== 'running' && (
                <div style={{ padding: '4px 10px 8px', fontSize: '10.5px', color: 'var(--c-text-muted)', fontStyle: 'italic', fontFamily: MONO }}>
                  (no output)
                </div>
              )}

              <div style={{ display: 'flex', justifyContent: 'flex-end', padding: '0 10px 6px' }}>
                <StatusBadge status={status} />
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function CopyBtn({ copied, onClick }: { copied: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={(e) => { e.stopPropagation(); onClick() }}
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: '22px',
        height: '22px',
        borderRadius: '4px',
        border: 'none',
        background: 'transparent',
        cursor: 'pointer',
        color: copied ? 'var(--c-text-secondary)' : 'var(--c-text-muted)',
        transition: 'color 150ms ease',
        padding: 0,
      }}
    >
      {copied ? <Check size={12} strokeWidth={2} /> : <Copy size={12} strokeWidth={1.5} />}
    </button>
  )
}

function StatusBadge({ status }: { status: Status }) {
  if (status === 'running') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
        <Loader2 size={10} className="animate-spin" />
      </span>
    )
  }
  if (status === 'failed') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-status-error-text, #ef4444)' }}>
        failed
      </span>
    )
  }
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
      <Check size={10} strokeWidth={2.5} />
    </span>
  )
}
