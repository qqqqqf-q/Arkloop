import { useMemo } from 'react'
import { X, Code2, Terminal } from 'lucide-react'
import hljs from 'highlight.js/lib/core'
import python from 'highlight.js/lib/languages/python'
import bash from 'highlight.js/lib/languages/bash'
import type { CodeExecution } from './ThinkingBlock'

hljs.registerLanguage('python', python)
hljs.registerLanguage('bash', bash)

function escapeHtml(raw: string): string {
  return raw.replace(/[&<>"']/g, (ch) => {
    switch (ch) {
      case '&':
        return '&amp;'
      case '<':
        return '&lt;'
      case '>':
        return '&gt;'
      case '"':
        return '&quot;'
      case '\'':
        return '&#39;'
      default:
        return ch
    }
  })
}

type Props = {
  execution: CodeExecution
  onClose: () => void
}

export function CodeExecutionPanel({ execution, onClose }: Props) {
  const isPython = execution.language === 'python'
  const failed = execution.status === 'failed'
  const outputText = execution.output && execution.output.trim()
    ? execution.output
    : execution.errorMessage && execution.errorMessage.trim()
      ? execution.errorMessage
      : undefined

  const highlightedCode = useMemo(() => {
    if (!execution.code) return ''
    try {
      return hljs.highlight(execution.code, {
        language: isPython ? 'python' : 'bash',
        ignoreIllegals: true,
      }).value
    } catch {
      return escapeHtml(execution.code)
    }
  }, [execution.code, isPython])

  return (
    <div
      style={{
        width: '420px',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
      }}
    >
      {/* header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '12px 16px',
          flexShrink: 0,
          background: 'var(--c-bg-page)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          {isPython
            ? <Code2 size={16} color="var(--c-text-tertiary)" strokeWidth={2} />
            : <Terminal size={16} color="var(--c-text-tertiary)" strokeWidth={2} />
          }
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1px' }}>
            <span style={{ fontSize: '13px', fontWeight: 500, color: 'var(--c-text-secondary)', lineHeight: '16px' }}>
              {isPython ? 'Python' : 'Shell'}
            </span>
            <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', lineHeight: '14px' }}>
              Code
            </span>
          </div>
        </div>
        <button
          onClick={onClose}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: '28px',
            height: '28px',
            borderRadius: '8px',
            border: 'none',
            background: 'transparent',
            color: 'var(--c-text-secondary)',
            cursor: 'pointer',
            transition: 'background 150ms',
          }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
        >
          <X size={18} />
        </button>
      </div>

      {/* code: 直接平铺 */}
      <div style={{ flex: 1, overflowY: 'auto', background: 'var(--c-code-panel-bg)' }}>
        {execution.code && (
          <pre
            style={{
              margin: 0,
              padding: '16px 20px',
              fontSize: '13px',
              lineHeight: '1.65',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
            }}
          >
            <code
              className="hljs"
              dangerouslySetInnerHTML={{ __html: highlightedCode }}
            />
          </pre>
        )}

        {outputText && (
          <div style={{ padding: '0 20px 16px', marginTop: execution.code ? '4px' : '0' }}>
            <div style={{ fontSize: '11px', color: 'var(--c-text-muted)', marginBottom: '6px', fontWeight: 500 }}>
              output
            </div>
            <pre
              style={{
                margin: 0,
                padding: '10px 12px',
                borderRadius: '8px',
                background: 'var(--c-code-panel-output-bg)',
                color: failed ? '#ef4444' : 'var(--c-text-secondary)',
                fontSize: '12px',
                lineHeight: '1.5',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                overflow: 'auto',
              }}
            >
              {outputText.trimEnd()}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}
