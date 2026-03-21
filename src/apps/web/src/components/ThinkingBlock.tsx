import { useState, useRef, useLayoutEffect } from 'react'
import { useTypewriter } from '../hooks/useTypewriter'
import { motion } from 'framer-motion'
import { ChevronRight, Loader2, Code2, Terminal } from 'lucide-react'
import { MarkdownRenderer } from './MarkdownRenderer'
import { ExecutionCard } from './ExecutionCard'
import type { CodeExecutionRef } from '../storage'
import { codeExecutionAccentColor } from '../codeExecutionStatus'
import { useLocale } from '../contexts/LocaleContext'

export type CodeExecution = CodeExecutionRef

const INNER_BODY_MAX_PX = 220

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

function ThinkingBody({
  content,
  isStreaming,
  codeExecutions,
  onOpenCodeExecution,
  displayedThinkingMd,
  fadeToVar,
}: {
  content: string
  isStreaming?: boolean
  codeExecutions?: CodeExecution[]
  onOpenCodeExecution?: (ce: CodeExecution) => void
  displayedThinkingMd: string
  /** 底部渐隐终点，与外层背景一致 */
  fadeToVar?: string
}) {
  const { t } = useLocale()
  const fadeEnd = fadeToVar ?? 'var(--c-bg-sub)'
  const innerWrapRef = useRef<HTMLDivElement>(null)
  const [innerExpanded, setInnerExpanded] = useState(false)
  const [needsMore, setNeedsMore] = useState(false)

  useLayoutEffect(() => {
    if (innerExpanded) {
      setNeedsMore(false)
      return
    }
    const el = innerWrapRef.current
    if (!el || !content.trim()) {
      setNeedsMore(false)
      return
    }
    setNeedsMore(el.scrollHeight > INNER_BODY_MAX_PX + 1)
  }, [content, displayedThinkingMd, innerExpanded, isStreaming])

  const codeBlock = codeExecutions && codeExecutions.length > 0 && (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginTop: content.trim() ? '10px' : '0' }}>
      {codeExecutions.map((ce) =>
        ce.language === 'shell'
          ? <ExecutionCard key={ce.id} variant="shell" code={ce.code} output={ce.output} status={ce.status} errorMessage={ce.errorMessage} smooth={!!isStreaming} />
          : <CodeExecutionCard key={ce.id} language={ce.language} code={ce.code} output={ce.output} errorMessage={ce.errorMessage} status={ce.status} onOpen={() => onOpenCodeExecution?.(ce)} />,
      )}
    </div>
  )

  const hasCode = !!(codeExecutions && codeExecutions.length > 0)
  const hasText = content.trim() !== ''
  if (!hasText && !hasCode && !isStreaming) {
    return null
  }

  return (
    <div>
      {(hasText || isStreaming) && (
        <>
          <div style={{ position: 'relative' }}>
            <div
              ref={innerWrapRef}
              style={{
                maxHeight: innerExpanded ? 'none' : INNER_BODY_MAX_PX,
                overflow: innerExpanded ? 'visible' : 'hidden',
                position: 'relative',
                minHeight: isStreaming && !hasText ? 40 : undefined,
              }}
            >
              {hasText ? (
                <MarkdownRenderer content={displayedThinkingMd} disableMath />
              ) : (
                <span className="thinking-shimmer text-sm text-[var(--c-text-muted)]">
                  {t.assistantStreamThinkingPlaceholder}
                </span>
              )}
            </div>
            {!innerExpanded && needsMore && (
              <div
                style={{
                  pointerEvents: 'none',
                  position: 'absolute',
                  left: 0,
                  right: 0,
                  bottom: 0,
                  height: 48,
                  background: `linear-gradient(to bottom, transparent, ${fadeEnd})`,
                }}
              />
            )}
          </div>
          {hasText && (needsMore || innerExpanded) && (
            <button
              type="button"
              onClick={() => setInnerExpanded((v) => !v)}
              className="mt-1 text-xs font-medium text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]"
            >
              {innerExpanded ? t.thinkingShowLess : t.thinkingShowMore}
            </button>
          )}
        </>
      )}
      {codeBlock}
    </div>
  )
}

export function ThinkingBlock({ label, mode, content, isStreaming, codeExecutions, onOpenCodeExecution }: Props) {
  const { t } = useLocale()
  const [expanded, setExpanded] = useState(false)
  const displayedThinkingMd = useTypewriter(content, !isStreaming)
  const headerLabel = label.trim() || t.assistantStreamThinkingPlaceholder

  if (mode === 'hidden') return null

  if (mode === 'visible') {
    return (
      <div style={{ maxWidth: '663px' }}>
        <ThinkingBody
          content={content}
          isStreaming={isStreaming}
          codeExecutions={codeExecutions}
          onOpenCodeExecution={onOpenCodeExecution}
          displayedThinkingMd={displayedThinkingMd}
          fadeToVar="var(--c-bg-page)"
        />
      </div>
    )
  }

  return (
    <div
      style={{
        borderRadius: '8px',
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-sub)',
        overflow: expanded ? 'visible' : 'hidden',
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
          gap: '8px',
          padding: '8px 12px',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--c-text-secondary)',
          fontSize: '13px',
        }}
      >
        <span
          style={{
            width: '8px',
            height: '8px',
            borderRadius: '50%',
            flexShrink: 0,
            background: 'var(--c-text-muted)',
          }}
        />
        {isStreaming ? (
          <Loader2 size={13} className="animate-spin" style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
        ) : (
          <motion.div
            animate={{ rotate: expanded ? 90 : 0 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            style={{ display: 'flex', flexShrink: 0, marginLeft: '-2px' }}
          >
            <ChevronRight size={13} />
          </motion.div>
        )}
        <span style={{ textAlign: 'left', flex: 1 }}>{headerLabel}</span>
      </button>

      {expanded && (isStreaming || content.trim() !== '' || (codeExecutions && codeExecutions.length > 0)) && (
        <div
          style={{
            padding: '0 12px 10px',
            borderTop: '0.5px solid var(--c-border-subtle)',
            paddingTop: '8px',
          }}
        >
          <ThinkingBody
            content={content}
            isStreaming={isStreaming}
            codeExecutions={codeExecutions}
            onOpenCodeExecution={onOpenCodeExecution}
            displayedThinkingMd={displayedThinkingMd}
          />
        </div>
      )}
    </div>
  )
}
