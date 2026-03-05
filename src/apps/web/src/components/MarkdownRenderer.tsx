import { useState, useCallback, useRef, useContext, createContext, Fragment, isValidElement, cloneElement } from 'react'
import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import { Copy, Check } from 'lucide-react'
import type { Components, UrlTransform } from 'react-markdown'
import { defaultUrlTransform } from 'react-markdown'
import { CitationBadge, WebSourcesContext } from './CitationBadge'
import type { WebSource, ArtifactRef } from '../storage'
import { ArtifactImage } from './ArtifactImage'
import { ArtifactHtmlPreview } from './ArtifactHtmlPreview'
import { ArtifactDownload } from './ArtifactDownload'
import { MindmapBlock } from './MindmapBlock'

type ArtifactsContextValue = {
  artifacts: ArtifactRef[]
  accessToken: string
}

const ArtifactsContext = createContext<ArtifactsContextValue>({ artifacts: [], accessToken: '' })

// \[...\] → $$...$$ , \(...\) → $...$
// 跳过代码块和行内代码
function normalizeLatexDelimiters(content: string): string {
  const parts = content.split(/(```[\s\S]*?```)/g)

  return parts.map((part, i) => {
    if (i % 2 === 1) return part // fenced code block

    const segments = part.split(/(`[^`]+`)/g)
    return segments.map((seg, j) => {
      if (j % 2 === 1) return seg // inline code
      return seg
        .replace(/\\\[([\s\S]*?)\\\]/g, (_, inner: string) => `\n$$\n${inner.trim()}\n$$\n`)
        .replace(/\\\(([\s\S]*?)\\\)/g, (_, inner: string) => `$${inner}$`)
    }).join('')
  }).join('')
}

const ARTIFACT_PREFIX = 'artifact:'

// react-markdown v10 的 defaultUrlTransform 会过滤非标准协议，需要放行 artifact:
const artifactUrlTransform: UrlTransform = (url) => {
  if (url.startsWith(ARTIFACT_PREFIX)) return url
  return defaultUrlTransform(url)
}

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

    if (!accessToken) return null

    // LLM 可能用 [text](artifact:key) 而非 ![text](artifact:key)，统一按 mime_type 分派
    if (resolved.mime_type.startsWith('image/')) {
      return <ArtifactImage artifact={resolved} accessToken={accessToken} />
    }
    if (resolved.mime_type === 'text/html') {
      return <ArtifactHtmlPreview artifact={resolved} accessToken={accessToken} />
    }
    return <ArtifactDownload artifact={resolved} accessToken={accessToken} />
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

const CODE_LANGUAGE_CLASS_RE = /(?:^|\s)language-([a-z0-9_-]+)(?:\s|$)/i

function extractCodeLanguage(children: ReactNode): string | null {
  if (isValidElement<{ className?: string }>(children)) {
    const className = children.props?.className
    if (typeof className === 'string') {
      const match = CODE_LANGUAGE_CLASS_RE.exec(className)
      if (match?.[1]) return match[1].toLowerCase()
    }
  }
  if (Array.isArray(children)) {
    for (const child of children) {
      const lang = extractCodeLanguage(child)
      if (lang) return lang
    }
  }
  return null
}

function normalizeCodeLanguageLabel(language: string | null): string {
  if (!language) return 'text'
  if (language === 'plaintext' || language === 'plain' || language === 'txt') return 'text'
  return language
}

function extractTextFromChildren(node: ReactNode): string {
  if (typeof node === 'string') return node
  if (typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractTextFromChildren).join('')
  if (isValidElement<{ children?: ReactNode }>(node) && node.props?.children != null) {
    return extractTextFromChildren(node.props.children)
  }
  return ''
}

function CodeBlockWrapper({ children }: { children: React.ReactNode }) {
  const [copied, setCopied] = useState(false)
  const [copyHover, setCopyHover] = useState(false)
  const preRef = useRef<HTMLPreElement>(null)
  const languageLabel = normalizeCodeLanguageLabel(extractCodeLanguage(children))
  const frameRadius = 10

  const handleCopy = useCallback(() => {
    const text = preRef.current?.textContent ?? ''
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [])

  return (
    <div
      style={{
        position: 'relative',
        margin: '1em 0',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: `${frameRadius}px`,
        background: 'var(--c-md-code-block-bg, var(--c-bg-deep))',
        overflow: 'hidden',
      }}
    >
      <span
        style={{
          position: 'absolute',
          top: 0,
          left: 0,
          zIndex: 1,
          display: 'inline-flex',
          alignItems: 'center',
          height: '24px',
          borderTopLeftRadius: `${frameRadius}px`,
          borderTopRightRadius: '0',
          borderBottomLeftRadius: '0',
          borderBottomRightRadius: '8px',
          borderRight: '0.5px solid var(--c-border-subtle)',
          borderBottom: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-md-code-label-bg, var(--c-bg-sub))',
          color: 'var(--c-text-secondary)',
          fontSize: '11px',
          letterSpacing: '0.18px',
          padding: '0 10px',
          textTransform: 'lowercase',
          userSelect: 'none',
        }}
      >
        {languageLabel}
      </span>
      <pre
        ref={preRef}
        style={{
          background: 'transparent',
          border: 'none',
          borderRadius: 0,
          padding: '36px 44px 14px 16px',
          overflowX: 'auto',
          fontSize: '13.5px',
          lineHeight: 1.65,
          fontFamily: "'JetBrains Mono', 'Cascadia Code', 'Fira Code', monospace",
          margin: 0,
        }}
      >
        {children}
      </pre>
      <button
        onClick={handleCopy}
        onMouseEnter={() => setCopyHover(true)}
        onMouseLeave={() => setCopyHover(false)}
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
          background: copyHover ? 'var(--c-bg-deep)' : 'transparent',
          color: copied ? 'var(--c-text-secondary)' : 'var(--c-text-icon)',
          opacity: copyHover || copied ? 1 : 0.6,
          transition: 'opacity 0.15s, background 0.15s',
        }}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
    </div>
  )
}

const CITATION_TOKEN_RE = /【\s*web\s*[:：]\s*(\d+)\s*】|\[\s*web\s*[:：]\s*(\d+)\s*\]|\bweb\s*[:：]\s*(\d+)\b/gi
const CITATION_GROUP_SEPARATOR_RE = /^[\s,，、;；]*$/

type CitationGroup = {
  start: number
  end: number
  indices: number[]
}

function extractCitationGroups(text: string): CitationGroup[] {
  const groups: CitationGroup[] = []
  let pending: CitationGroup | null = null
  let m: RegExpExecArray | null
  CITATION_TOKEN_RE.lastIndex = 0

  while ((m = CITATION_TOKEN_RE.exec(text)) !== null) {
    const idx = parseInt(m[1] ?? m[2] ?? m[3], 10)
    if (Number.isNaN(idx)) continue

    const start = m.index
    const end = m.index + m[0].length

    if (!pending) {
      pending = { start, end, indices: [idx] }
      continue
    }

    const separator = text.slice(pending.end, start)
    if (separator.length === 0 || CITATION_GROUP_SEPARATOR_RE.test(separator)) {
      pending.end = end
      pending.indices.push(idx)
      continue
    }

    groups.push(pending)
    pending = { start, end, indices: [idx] }
  }

  if (pending) groups.push(pending)
  return groups
}

function processText(text: string, keyPrefix: string): ReactNode[] {
  const groups = extractCitationGroups(text)
  if (groups.length === 0) return [text]

  const parts: ReactNode[] = []
  let lastIndex = 0

  groups.forEach((group, index) => {
    if (lastIndex < group.start) parts.push(text.slice(lastIndex, group.start))
    parts.push(<CitationBadge key={`${keyPrefix}-${index}`} indices={group.indices} />)
    lastIndex = group.end
  })

  if (lastIndex < text.length) {
    parts.push(text.slice(lastIndex))
  }

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
    const nodeTag = typeof (children.props as { node?: { tagName?: unknown } }).node?.tagName === 'string'
      ? (children.props as { node?: { tagName?: string } }).node?.tagName
      : undefined
    if (
      (typeof children.type === 'string' && (children.type === 'code' || children.type === 'pre')) ||
      nodeTag === 'code' ||
      nodeTag === 'pre'
    ) {
      return children
    }
    return cloneElement(children, {}, processChildren(children.props.children, `${prefix}-e`))
  }
  return children
}

function WithCitations({ children, prefix }: { children: ReactNode; prefix: string }) {
  return <>{processChildren(children, prefix)}</>
}

const mdComponents: Components = {
  pre: ({ children }) => {
    const lang = extractCodeLanguage(children)
    if (lang === 'mindmap') {
      return <MindmapBlock content={extractTextFromChildren(children)} />
    }
    return <CodeBlockWrapper>{children}</CodeBlockWrapper>
  },

  // 内联/块级区分通过 CSS .md-content :not(pre) > code 处理
  code: ({ className, children }) => (
    <code className={className}>{children}</code>
  ),

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
    <div className="md-table-wrap">
      <table className="md-table">
        {children}
      </table>
    </div>
  ),

  th: ({ children }) => (
    <th>
      {children}
    </th>
  ),

  td: ({ children }) => (
    <td>
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

  const normalizedContent = disableMath ? content : normalizeLatexDelimiters(content)

  return (
    <ArtifactsContext.Provider value={artifactsValue}>
      <WebSourcesContext.Provider value={webSources ?? []}>
        <div className="md-content" style={{ maxWidth: '100%' }}>
          <ReactMarkdown
            remarkPlugins={remarkPlugins}
            rehypePlugins={rehypePlugins}
            components={mdComponents}
            urlTransform={artifactUrlTransform}
          >
            {normalizedContent}
          </ReactMarkdown>
        </div>
      </WebSourcesContext.Provider>
    </ArtifactsContext.Provider>
  )
}
