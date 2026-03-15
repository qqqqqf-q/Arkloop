import { useRef, useEffect, useState, useCallback } from 'react'
import { Maximize2, Minimize2 } from 'lucide-react'
import mermaid from 'mermaid'

const DEBOUNCE_MS = 200
const DEFAULT_HEIGHT = 400
const EXPANDED_HEIGHT = 640

let mermaidInitialized = false
function ensureMermaidInit() {
  if (mermaidInitialized) return
  mermaidInitialized = true
  mermaid.initialize({
    startOnLoad: false,
    theme: 'base',
    themeVariables: {
      primaryColor: 'var(--c-bg-sub)',
      primaryTextColor: 'var(--c-text-primary)',
      primaryBorderColor: 'var(--c-border)',
      lineColor: 'var(--c-text-tertiary)',
      secondaryColor: 'var(--c-bg-deep)',
      tertiaryColor: 'var(--c-bg-page)',
      fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif",
    },
    flowchart: { htmlLabels: true, curve: 'basis' },
    securityLevel: 'loose',
  })
}

let renderCounter = 0

type Props = {
  content: string
}

export function MermaidBlock({ content }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [expanded, setExpanded] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastContentRef = useRef('')

  const renderMermaid = useCallback(async (code: string) => {
    if (!containerRef.current || !code.trim()) return
    ensureMermaidInit()

    try {
      const id = `mermaid-${++renderCounter}`
      const { svg } = await mermaid.render(id, code.trim())
      if (containerRef.current) {
        containerRef.current.innerHTML = svg
        setError(null)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    if (content === lastContentRef.current) return
    lastContentRef.current = content
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => void renderMermaid(content), DEBOUNCE_MS)
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [content, renderMermaid])

  const height = expanded ? EXPANDED_HEIGHT : DEFAULT_HEIGHT

  return (
    <div
      style={{
        position: 'relative',
        margin: '1em 0',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        background: 'var(--c-bg-page)',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 10px',
          height: '28px',
          borderBottom: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-md-code-label-bg, var(--c-bg-sub))',
        }}
      >
        <span
          style={{
            fontSize: '11px',
            letterSpacing: '0.18px',
            color: 'var(--c-text-secondary)',
            userSelect: 'none',
          }}
        >
          mermaid
        </span>
        <button
          onClick={() => setExpanded(prev => !prev)}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: '22px',
            height: '22px',
            borderRadius: '5px',
            border: 'none',
            background: 'transparent',
            color: 'var(--c-text-icon)',
            cursor: 'pointer',
            transition: 'opacity 0.15s',
          }}
          className="opacity-60 hover:opacity-100"
        >
          {expanded ? <Minimize2 size={12} /> : <Maximize2 size={12} />}
        </button>
      </div>
      {error ? (
        <div
          style={{
            padding: '16px',
            color: 'var(--c-status-error)',
            fontSize: '13px',
            fontFamily: "'JetBrains Mono', monospace",
            whiteSpace: 'pre-wrap',
            overflow: 'auto',
            maxHeight: `${height}px`,
          }}
        >
          {error}
        </div>
      ) : (
        <div
          ref={containerRef}
          style={{
            width: '100%',
            height: `${height}px`,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            overflow: 'auto',
            transition: 'height 0.2s ease',
          }}
        />
      )}
    </div>
  )
}
