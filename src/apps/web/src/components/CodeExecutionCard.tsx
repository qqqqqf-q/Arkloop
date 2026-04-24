import { Code2, Terminal } from 'lucide-react'
import type { CodeExecutionRef } from '../storage'
import { codeExecutionAccentColor } from '../codeExecutionStatus'

export type CodeExecution = CodeExecutionRef

export function CodeExecutionCard({ language, code, output, emptyLabel, errorMessage, status, onOpen, isActive }: {
  language: 'python' | 'shell'
  code?: string
  output?: string
  emptyLabel?: string
  errorMessage?: string
  status: CodeExecution['status']
  onOpen?: () => void
  isActive?: boolean
}) {
  const isPython = language === 'python'
  const hasDetail = !!(code || output || emptyLabel || errorMessage)
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
