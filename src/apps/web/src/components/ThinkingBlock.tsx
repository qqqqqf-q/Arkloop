import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, Loader2, Code2, Terminal } from 'lucide-react'
import { MarkdownRenderer } from './MarkdownRenderer'
import { ShellExecutionBlock } from './ShellExecutionBlock'
import type { CodeExecutionRef } from '../storage'
import { codeExecutionAccentColor } from '../codeExecutionStatus'

export type CodeExecution = CodeExecutionRef

export function CodeExecutionCard({ language, code, output, errorMessage, status, onOpen, isActive }: {
  language: 'python' | 'shell'
  code?: string
  output?: string
  errorMessage?: string
  status: CodeExecution['status']
  onOpen?: () => void
  isActive?: boolean
}) {
  const isPython = language === 'python'
  const hasDetail = !!(code || output || errorMessage)
  const clickable = hasDetail && !!onOpen
  const accentColor = codeExecutionAccentColor(status)

  return (
    <div
      className={clickable ? 'code-exec-card-clickable' : undefined}
      data-active={isActive || undefined}
      data-status={status}
      style={{
        borderRadius: '7px',
        border: `0.5px solid ${accentColor}`,
        background: 'var(--c-bg-page)',
        width: 'fit-content',
        maxWidth: '100%',
        boxShadow: isActive ? '0 0 0 1.5px var(--c-text-secondary)' : 'none',
      }}
    >
      <button
        type="button"
        onClick={() => hasDetail && onOpen?.()}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          padding: '6px 10px',
          background: 'none',
          border: 'none',
          cursor: hasDetail && onOpen ? 'pointer' : 'default',
          width: '100%',
        }}
      >
        <div
          style={{
            width: '28px',
            height: '28px',
            borderRadius: '6px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
            color: accentColor,
          }}
        >
          {isPython
            ? <Code2 size={15} color={accentColor} strokeWidth={2} />
            : <Terminal size={15} color={accentColor} strokeWidth={2} />
          }
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0px', textAlign: 'left' }}>
          <span style={{ fontSize: '12px', fontWeight: 500, color: 'var(--c-text-secondary)', lineHeight: '15px' }}>
            {isPython ? 'Python' : 'Shell'}
          </span>
          <span style={{ fontSize: '10px', color: 'var(--c-text-muted)', lineHeight: '13px' }}>
            Code
          </span>
        </div>
      </button>
    </div>
  )
}

type Props = {
  kind: string
  label: string
  mode: 'visible' | 'collapsed' | 'hidden'
  content: string
  isStreaming?: boolean
  codeExecutions?: CodeExecution[]
  onOpenCodeExecution?: (ce: CodeExecution) => void
}

export function ThinkingBlock({ label, mode, content, isStreaming, codeExecutions, onOpenCodeExecution }: Props) {
  const [expanded, setExpanded] = useState(false)

  if (mode === 'hidden') return null

  if (mode === 'visible') {
    return (
      <div style={{ maxWidth: '663px' }}>
        <MarkdownRenderer content={content} disableMath />
        {codeExecutions && codeExecutions.length > 0 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginTop: '10px' }}>
            {codeExecutions.map((ce) =>
              ce.language === 'shell'
                ? <ShellExecutionBlock key={ce.id} code={ce.code} output={ce.output} status={ce.status} errorMessage={ce.errorMessage} />
                : <CodeExecutionCard key={ce.id} language={ce.language} code={ce.code} output={ce.output} errorMessage={ce.errorMessage} status={ce.status} onOpen={() => onOpenCodeExecution?.(ce)} />
            )}
          </div>
        )}
      </div>
    )
  }

  // collapsed mode
  return (
    <div
      style={{
        borderRadius: '8px',
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-sub)',
        overflow: 'hidden',
        maxWidth: '663px',
      }}
    >
      <button
        type="button"
        onClick={() => setExpanded((prev) => !prev)}
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '8px 12px',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--c-text-secondary)',
          fontSize: '13px',
        }}
      >
        {isStreaming ? (
          <Loader2 size={13} className="animate-spin" style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
        ) : (
          <motion.div
            animate={{ rotate: expanded ? 90 : 0 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            style={{ display: 'flex', flexShrink: 0 }}
          >
            <ChevronRight size={13} />
          </motion.div>
        )}
        <span style={{ textAlign: 'left' }}>{label}</span>
      </button>

      <AnimatePresence initial={false}>
        {expanded && (content || (codeExecutions && codeExecutions.length > 0)) && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.4, 0, 0.2, 1] }}
            style={{ overflow: 'hidden' }}
          >
            <div
              style={{
                padding: '0 12px 10px',
                borderTop: '0.5px solid var(--c-border-subtle)',
                paddingTop: '8px',
              }}
            >
              {content && <MarkdownRenderer content={content} disableMath />}
              {codeExecutions && codeExecutions.length > 0 && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginTop: content ? '10px' : '0' }}>
                  {codeExecutions.map((ce) =>
                    ce.language === 'shell'
                      ? <ShellExecutionBlock key={ce.id} code={ce.code} output={ce.output} status={ce.status} errorMessage={ce.errorMessage} />
                      : <CodeExecutionCard key={ce.id} language={ce.language} code={ce.code} output={ce.output} errorMessage={ce.errorMessage} status={ce.status} onOpen={() => onOpenCodeExecution?.(ce)} />
                  )}
                </div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
