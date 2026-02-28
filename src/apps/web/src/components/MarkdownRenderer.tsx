import { useState, useCallback, useRef, useContext, createContext, Fragment, isValidElement, cloneElement } from 'react'
import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import { Copy, Check } from 'lucide-react'
import type { Components } from 'react-markdown'
import { CitationBadge, WebSourcesContext } from './CitationBadge'
import type { WebSource, ArtifactRef } from '../storage'
import { ArtifactImage } from './ArtifactImage'
import { ArtifactHtmlPreview } from './ArtifactHtmlPreview'
import { ArtifactDownload } from './ArtifactDownload'

type ArtifactsContextValue = {
  artifacts: ArtifactRef[]
  accessToken: string
}

const ArtifactsContext = createContext<ArtifactsContextValue>({ artifacts: [], accessToken: '' })

const ARTIFACT_PREFIX = 'artifact:'

function findArtifactByKey(artifacts: ArtifactRef[], key: string): ArtifactRef | undefined {
  return artifacts.find((a) => a.key === key)
}

const EXT_MIME: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif',
  svg: 'image/svg+xml', webp: 'image/webp', html: 'text/html', htm: 'text/html',
  pdf: 'application/pdf', csv: 'text/csv', txt: 'text/plain',
}

function guessMimeType(key: string): string {
  const ext = key.split('.').pop()?.toLowerCase() ?? ''
  return EXT_MIME[ext] ?? 'application/octet-stream'
}

// artifact: 协议感知的 img 渲染器
function ArtifactAwareImg({ src, alt }: { src?: string; alt?: string }) {
  const { artifacts, accessToken } = useContext(ArtifactsContext)

  if (src?.startsWith(ARTIFACT_PREFIX)) {
    const key = src.slice(ARTIFACT_PREFIX.length)
    const artifact = findArtifactByKey(artifacts, key)

    // 从 key 推断的回退 artifact（当 SSE 事件尚未到达或 artifacts 为空时）
    const resolved: ArtifactRef = artifact ?? {
      key,
      filename: key.split('/').pop() ?? key,
      size: 0,
      mime_type: guessMimeType(key),
    }

    if (!accessToken) return null

    if (resolved.mime_type.startsWith('image/')) {
      return <ArtifactImage artifact={resolved} accessToken={accessToken} />
    }
    if (resolved.mime_type === 'text/html') {
      return <ArtifactHtmlPreview artifact={resolved} accessToken={accessToken} />
    }
    return <ArtifactDownload artifact={resolved} accessToken={accessToken} />
  }

  return <img src={src} alt={alt ?? ''} style={{ maxWidth: '100%', borderRadius: '8px' }} />
}

// artifact: 协议感知的 a 渲染器
function ArtifactAwareLink({ href, children }: { href?: string; children?: ReactNode }) {
  const { artifacts, accessToken } = useContext(ArtifactsContext)

  if (href?.startsWith(ARTIFACT_PREFIX)) {
    const key = href.slice(ARTIFACT_PREFIX.length)
    const artifact = findArtifactByKey(artifacts, key)

    const resolved: ArtifactRef = artifact ?? {
      key,
      filename: key.split('/').pop() ?? key,
      size: 0,
      mime_type: guessMimeType(key),
    }

    if (accessToken) {
      return <ArtifactDownload artifact={resolved} accessToken={accessToken} />
    }
    return null
  }

  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      style={{ color: 'var(--c-text-secondary)', textDecoration: 'underline', textDecorationColor: 'var(--c-border-mid)' }}
    >
      {children}
    </a>
  )
}

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

// 匹配 【web:N】 或 [web:N] 形式的引用，支持连续引用分组
const CITATION_GROUP_RE = /(?:【web:(\d+)】|\[web:(\d+)\])+/g
const CITATION_SINGLE_RE = /【web:(\d+)】|\[web:(\d+)\]/g

function extractIndices(group: string): number[] {
  const indices: number[] = []
  let m: RegExpExecArray | null
  CITATION_SINGLE_RE.lastIndex = 0
  while ((m = CITATION_SINGLE_RE.exec(group)) !== null) {
    const idx = parseInt(m[1] ?? m[2], 10)
    if (!isNaN(idx)) indices.push(idx)
  }
  return indices
}

function processText(text: string, keyPrefix: string): ReactNode[] {
  const parts: ReactNode[] = []
  let lastIndex = 0
  let groupIdx = 0
  CITATION_GROUP_RE.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = CITATION_GROUP_RE.exec(text)) !== null) {
    if (lastIndex < m.index) parts.push(text.slice(lastIndex, m.index))
    parts.push(<CitationBadge key={`${keyPrefix}-${groupIdx++}`} indices={extractIndices(m[0])} />)
    lastIndex = m.index + m[0].length
  }
  if (lastIndex < text.length) parts.push(text.slice(lastIndex))
  return parts
}

function processChildren(children: ReactNode, prefix: string): ReactNode {
  if (typeof children === 'string') {
    const parts = processText(children, prefix)
    if (parts.length === 1 && typeof parts[0] === 'string') return parts[0]
    return <>{parts}</>
  }
  if (Array.isArray(children)) {
    return (
      <>
        {children.map((child, i) => (
          <Fragment key={i}>{processChildren(child, `${prefix}-${i}`)}</Fragment>
        ))}
      </>
    )
  }
  if (isValidElement<{ children?: ReactNode }>(children) && children.props?.children !== undefined) {
    return cloneElement(children, {}, processChildren(children.props.children, `${prefix}-e`))
  }
  return children
}

function WithCitations({ children, prefix }: { children: ReactNode; prefix: string }) {
  const webSources = useContext(WebSourcesContext)
  if (webSources.length === 0) return <>{children}</>
  return <>{processChildren(children, prefix)}</>
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
      <WithCitations prefix="p">{children}</WithCitations>
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

  li: ({ children }) => <li style={{ marginBottom: '0.3em' }}><WithCitations prefix="li">{children}</WithCitations></li>,

  blockquote: ({ children }) => (
    <blockquote style={{ borderLeft: '3px solid var(--c-border-mid)', paddingLeft: '1em', margin: '1em 0', color: 'var(--c-text-secondary)', fontStyle: 'italic' }}>
      <WithCitations prefix="bq">{children}</WithCitations>
    </blockquote>
  ),

  a: ({ href, children }) => <ArtifactAwareLink href={href}>{children}</ArtifactAwareLink>,

  img: ({ src, alt }) => <ArtifactAwareImg src={src} alt={alt} />,

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
      <WithCitations prefix="td">{children}</WithCitations>
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

type Props = {
  content: string
  disableMath?: boolean
  webSources?: WebSource[]
  artifacts?: ArtifactRef[]
  accessToken?: string
}

export function MarkdownRenderer({ content, disableMath, webSources, artifacts, accessToken }: Props) {
  const remarkPlugins = disableMath
    ? [remarkGfm]
    : [remarkGfm, remarkMath]

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const rehypePlugins: any[] = disableMath
    ? [[rehypeHighlight, { ignoreMissing: true }]]
    : [
        [rehypeKatex, { throwOnError: false, output: 'htmlAndMathml' }],
        [rehypeHighlight, { ignoreMissing: true }],
      ]

  const artifactsValue: ArtifactsContextValue = {
    artifacts: artifacts ?? [],
    accessToken: accessToken ?? '',
  }

  return (
    <ArtifactsContext.Provider value={artifactsValue}>
      <WebSourcesContext.Provider value={webSources ?? []}>
        <div style={{ maxWidth: '100%' }}>
          <ReactMarkdown
            remarkPlugins={remarkPlugins}
            rehypePlugins={rehypePlugins}
            components={mdComponents}
          >
            {content}
          </ReactMarkdown>
        </div>
      </WebSourcesContext.Provider>
    </ArtifactsContext.Provider>
  )
}
