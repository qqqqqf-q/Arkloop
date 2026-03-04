import { useState } from 'react'
import { ChevronDown, ChevronRight, Loader2, Code2, Terminal } from 'lucide-react'
import { MarkdownRenderer } from './MarkdownRenderer'

export type CodeExecution = {
  id: string
  language: 'python' | 'shell'
  code?: string
  output?: string
  exitCode?: number
}

export function CodeExecutionCard({ language, code, output, onOpen }: {
  language: 'python' | 'shell'
  code?: string
  output?: string
  exitCode?: number
  onOpen?: () => void
}) {
  const isPython = language === 'python'
  const hasDetail = !!(code || output)

  return (
    <div
      style={{
        borderRadius: '7px',
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-page)',
        width: 'fit-content',
        maxWidth: '100%',
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
          }}
        >
          {isPython
            ? <Code2 size={15} color="var(--c-text-secondary)" strokeWidth={2} />
            : <Terminal size={15} color="var(--c-text-secondary)" strokeWidth={2} />
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
            {codeExecutions.map((ce) => (
              <CodeExecutionCard key={ce.id} language={ce.language} code={ce.code} output={ce.output} exitCode={ce.exitCode} onOpen={() => onOpenCodeExecution?.(ce)} />
            ))}
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
        ) : expanded ? (
          <ChevronDown size={13} style={{ flexShrink: 0 }} />
        ) : (
          <ChevronRight size={13} style={{ flexShrink: 0 }} />
        )}
        <span style={{ textAlign: 'left' }}>{label}</span>
      </button>

      {expanded && (content || (codeExecutions && codeExecutions.length > 0)) && (
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
              {codeExecutions.map((ce) => (
                <CodeExecutionCard key={ce.id} language={ce.language} code={ce.code} output={ce.output} exitCode={ce.exitCode} onOpen={() => onOpenCodeExecution?.(ce)} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
