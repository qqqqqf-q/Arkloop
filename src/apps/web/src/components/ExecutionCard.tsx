import { useState, useRef, useEffect, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, ChevronDown, Check, Copy, Loader2 } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import { useTypewriter } from '../hooks/useTypewriter'

type Status = 'running' | 'success' | 'failed' | 'completed'

type Props = {
  variant: 'shell' | 'fileop'
  toolName?: string
  label?: string
  code?: string
  output?: string
  errorMessage?: string
  status: Status
  /** 仅流式时为 true：逐字平滑；历史/静态为 false 立即展示 */
  smooth?: boolean
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

function extractCommandPreview(code: string | undefined): string {
  if (!code) return ''
  const first = code.split('\n')[0].trim()
  return first.length > 72 ? first.slice(0, 72) + '...' : first
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
  const { t } = useLocale()
  if (status === 'running') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
        {t.shellRunning}
      </span>
    )
  }
  if (status === 'failed') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-status-error-text, #ef4444)' }}>
        {t.shellFailed}
      </span>
    )
  }
  if (status === 'completed') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
        {t.shellCompleted}
      </span>
    )
  }
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
      {t.shellSuccess}
    </span>
  )
}

export function ExecutionCard({ variant, toolName, label, code, output, errorMessage, status, smooth = false }: Props) {
  const { t } = useLocale()
  const [expanded, setExpanded] = useState(false)
  const [cmdHovered, setCmdHovered] = useState(false)
  const [outHovered, setOutHovered] = useState(false)
  const [cmdCopied, setCmdCopied] = useState(false)
  const [outCopied, setOutCopied] = useState(false)
  const outputRef = useRef<HTMLDivElement>(null)
  const [scrollEdge, setScrollEdge] = useState<ScrollEdge>('none')

  const preview = variant === 'shell' ? (extractCommandPreview(code) || t.shellRan) : (label || '')
  const displayOutput = output?.trim()
    ? output
    : errorMessage?.trim()
      ? errorMessage
      : undefined
  const previewTw = useTypewriter(preview, !smooth)
  const shellCodeTw = useTypewriter(variant === 'shell' && code ? code.trim() : '', !smooth)
  const outputForTw = displayOutput?.trimEnd() ?? ''
  const outputTw = useTypewriter(outputForTw, !smooth)
  const hasOutput = !!displayOutput
  const hasCommand = variant === 'shell' && !!code
  const expandable = !!(hasCommand || displayOutput || status === 'running')

  const copyText = useCallback((text: string, setter: (v: boolean) => void) => {
    void navigator.clipboard.writeText(text)
    setter(true)
    setTimeout(() => setter(false), 1500)
  }, [])

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
      {/* Trigger */}
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
        <span style={{
          fontSize: '11px',
          fontFamily: MONO,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          lineHeight: '16px',
          color: 'var(--c-text-muted)',
          transition: 'color 150ms ease',
        }}>
          {smooth ? previewTw : preview}
        </span>
        {expandable && (
          expanded
            ? <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
            : <ChevronRight size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
        )}
      </div>

      {/* Expanded body */}
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={expandTransition}
            style={{ overflow: 'hidden' }}
          >
            <div style={{ borderRadius: '8px', background: 'var(--c-attachment-bg)', overflow: 'hidden', marginTop: '4px' }}>
              {/* Label */}
              <div style={{ padding: '6px 10px 2px', fontSize: '10px', color: 'var(--c-text-muted)', fontFamily: MONO, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                {variant === 'shell' ? 'shell' : toolKindLabel(toolName || '')}
              </div>

              {/* Command input (shell only) */}
              {variant === 'shell' && code && (
                <div
                  style={{ position: 'relative', padding: '2px 10px 8px' }}
                  onMouseEnter={() => setCmdHovered(true)}
                  onMouseLeave={() => setCmdHovered(false)}
                >
                  <AnimatePresence>
                    {cmdHovered && (
                      <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        style={{ position: 'absolute', top: '2px', right: '6px', zIndex: 1 }}
                      >
                        <CopyBtn copied={cmdCopied} onClick={() => copyText(code, setCmdCopied)} />
                      </motion.div>
                    )}
                  </AnimatePresence>
                  <pre style={{
                    margin: 0,
                    fontSize: '11px',
                    lineHeight: '1.5',
                    color: 'var(--c-text-primary)',
                    fontFamily: MONO,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                    maxHeight: '120px',
                    overflowY: 'auto',
                    paddingRight: '34px',
                  }}>
                    <span style={{ color: 'var(--c-text-muted)' }}>$ </span>{smooth ? shellCodeTw : (variant === 'shell' && code ? code.trim() : '')}
                  </pre>
                </div>
              )}

              {/* Output */}
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
                        <CopyBtn copied={outCopied} onClick={() => copyText(displayOutput!, setOutCopied)} />
                      </motion.div>
                    )}
                  </AnimatePresence>
                  <div
                    ref={outputRef}
                    style={{
                      maxHeight: '240px',
                      overflowY: 'auto',
                      padding: '4px 10px 8px',
                      paddingRight: '34px',
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
                        {smooth ? outputTw : outputForTw}
                      </pre>
                    ) : (
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '20px' }}>
                        <Loader2 size={12} className="animate-spin" style={{ color: 'var(--c-text-muted)' }} />
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* No output */}
              {!hasOutput && status !== 'running' && (
                <div style={{ padding: '4px 10px 8px', fontSize: '10.5px', color: 'var(--c-text-muted)', fontStyle: 'italic', fontFamily: MONO }}>
                  {t.shellNoOutput}
                </div>
              )}

              {/* Status */}
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
