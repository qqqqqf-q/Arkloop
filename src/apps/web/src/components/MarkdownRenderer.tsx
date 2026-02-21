import { useState, useCallback, useRef } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { Copy, Check } from 'lucide-react'
import type { Components } from 'react-markdown'

function CodeBlockWrapper({ children }: { children: React.ReactNode }) {
  const [copied, setCopied] = useState(false)
  const preRef = useRef<HTMLPreElement>(null)

  const handleCopy = useCallback(() => {
    const text = preRef.current?.textContent ?? ''
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [])

  return (
    <div style={{ position: 'relative', margin: '1em 0' }}>
      <pre
        ref={preRef}
        style={{
          background: 'var(--c-bg-deep)',
          borderRadius: '8px',
          padding: '14px 44px 14px 16px',
          overflowX: 'auto',
          fontSize: '13.5px',
          lineHeight: 1.65,
          fontFamily: "'JetBrains Mono', 'Cascadia Code', 'Fira Code', monospace",
          border: '0.5px solid var(--c-border-subtle)',
          margin: 0,
        }}
      >
        {children}
      </pre>
      <button
        onClick={handleCopy}
        style={{
          position: 'absolute',
          top: '8px',
          right: '8px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: '26px',
          height: '26px',
          borderRadius: '6px',
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-sub)',
          color: copied ? 'var(--c-text-secondary)' : 'var(--c-text-icon)',
          cursor: 'pointer',
          opacity: 0.85,
          transition: 'opacity 0.15s',
        }}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
    </div>
  )
}

const mdComponents: Components = {
  pre: ({ children }) => <CodeBlockWrapper>{children}</CodeBlockWrapper>,

  code: ({ className, children }) => {
    // block code (inside pre) - pass className through for hljs
    if (className?.includes('language-')) {
      return <code className={className}>{children}</code>
    }
    return (
      <code
        style={{
          background: 'var(--c-bg-deep)',
          borderRadius: '4px',
          padding: '1px 5px',
          fontSize: '0.875em',
          fontFamily: "'JetBrains Mono', 'Cascadia Code', 'Fira Code', monospace",
          color: 'var(--c-text-secondary)',
          border: '0.5px solid var(--c-border-subtle)',
        }}
      >
        {children}
      </code>
    )
  },

  p: ({ children }) => (
    <p style={{ color: 'var(--c-text-primary)', fontSize: '16px', lineHeight: 1.6, letterSpacing: '0.16px', margin: '0 0 1em' }}>
      {children}
    </p>
  ),

  h1: ({ children }) => (
    <h1 style={{ color: 'var(--c-text-heading)', fontSize: '22px', fontWeight: 600, lineHeight: 1.4, margin: '1.5em 0 0.5em', letterSpacing: '-0.3px' }}>
      {children}
    </h1>
  ),

  h2: ({ children }) => (
    <h2 style={{ color: 'var(--c-text-heading)', fontSize: '20px', fontWeight: 600, lineHeight: 1.4, margin: '1.4em 0 0.5em', letterSpacing: '-0.2px' }}>
      {children}
    </h2>
  ),

  h3: ({ children }) => (
    <h3 style={{ color: 'var(--c-text-heading)', fontSize: '18px', fontWeight: 600, lineHeight: 1.4, margin: '1.2em 0 0.4em' }}>
      {children}
    </h3>
  ),

  h4: ({ children }) => (
    <h4 style={{ color: 'var(--c-text-heading)', fontSize: '16px', fontWeight: 600, lineHeight: 1.4, margin: '1em 0 0.4em' }}>
      {children}
    </h4>
  ),

  ul: ({ children }) => (
    <ul style={{ color: 'var(--c-text-primary)', fontSize: '16px', lineHeight: 1.6, paddingLeft: '1.5em', margin: '0 0 1em', listStyleType: 'disc' }}>
      {children}
    </ul>
  ),

  ol: ({ children }) => (
    <ol style={{ color: 'var(--c-text-primary)', fontSize: '16px', lineHeight: 1.6, paddingLeft: '1.5em', margin: '0 0 1em', listStyleType: 'decimal' }}>
      {children}
    </ol>
  ),

  li: ({ children }) => <li style={{ marginBottom: '0.3em' }}>{children}</li>,

  blockquote: ({ children }) => (
    <blockquote style={{ borderLeft: '3px solid var(--c-border-mid)', paddingLeft: '1em', margin: '1em 0', color: 'var(--c-text-secondary)', fontStyle: 'italic' }}>
      {children}
    </blockquote>
  ),

  a: ({ href, children }) => (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      style={{ color: 'var(--c-text-secondary)', textDecoration: 'underline', textDecorationColor: 'var(--c-border-mid)' }}
    >
      {children}
    </a>
  ),

  table: ({ children }) => (
    <div style={{ overflowX: 'auto', margin: '1em 0' }}>
      <table style={{ borderCollapse: 'collapse', width: '100%', fontSize: '15px' }}>
        {children}
      </table>
    </div>
  ),

  th: ({ children }) => (
    <th style={{ borderBottom: '1px solid var(--c-border)', padding: '8px 12px', textAlign: 'left', color: 'var(--c-text-secondary)', fontWeight: 600, fontSize: '14px', whiteSpace: 'nowrap' }}>
      {children}
    </th>
  ),

  td: ({ children }) => (
    <td style={{ borderBottom: '0.5px solid var(--c-border-subtle)', padding: '8px 12px', color: 'var(--c-text-primary)', verticalAlign: 'top' }}>
      {children}
    </td>
  ),

  hr: () => <hr style={{ border: 'none', borderTop: '0.5px solid var(--c-border-subtle)', margin: '1.5em 0' }} />,

  strong: ({ children }) => (
    <strong style={{ color: 'var(--c-text-primary)', fontWeight: 600 }}>{children}</strong>
  ),

  em: ({ children }) => (
    <em style={{ fontStyle: 'italic', color: 'var(--c-text-secondary)' }}>{children}</em>
  ),

  del: ({ children }) => (
    <del style={{ color: 'var(--c-text-muted)', textDecoration: 'line-through' }}>{children}</del>
  ),
}

type Props = { content: string }

export function MarkdownRenderer({ content }: Props) {
  return (
    <div style={{ maxWidth: '100%' }}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { ignoreMissing: true }]]}
        components={mdComponents}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
