import { Children, useState, useCallback, useRef, useContext, createContext, Fragment, isValidElement, cloneElement, useMemo, useEffect } from 'react'
import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import { CopyIconButton } from './CopyIconButton'
import type { Components, Options, UrlTransform } from 'react-markdown'
import { defaultUrlTransform } from 'react-markdown'
import { CitationBadge, WebSourcesContext } from './CitationBadge'
import type { WebSource, ArtifactRef } from '../storage'
import { ArtifactImage } from './ArtifactImage'
import { ArtifactHtmlPreview } from './ArtifactHtmlPreview'
import { ArtifactDownload } from './ArtifactDownload'
import { MindmapBlock } from './MindmapBlock'
import { MermaidBlock } from './MermaidBlock'
import { GeoGebraBlock } from './GeoGebraBlock'
import { WorkspaceResource, type WorkspaceFileRef } from './WorkspaceResource'
import { recordPerfCount, recordPerfValue } from '../perfDebug'

type ArtifactsContextValue = {
  artifacts: ArtifactRef[]
  accessToken: string
  runId?: string
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
}

const ArtifactsContext = createContext<ArtifactsContextValue>({ artifacts: [], accessToken: '' })
const STREAMING_MATH_COMMIT_INTERVAL_MS = 96

function isDocumentArtifact(artifact: ArtifactRef): boolean {
  if (artifact.display === 'panel') return true
  return !artifact.mime_type.startsWith('image/') && artifact.mime_type !== 'text/html'
}

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

function containsLikelyMath(content: string): boolean {
  return /\$\$[\s\S]*?\$\$|\$[^$\n]+\$|\\\([\s\S]*?\\\)|\\\[[\s\S]*?\\\]/.test(content)
}

const COLLAPSED_PIPE_TABLE_SEPARATOR_RE = /\|\|\s*:?-{3,}/
const COLLAPSED_PIPE_TABLE_ROW_BREAK_RE = /\|\|\s*(?=\|?\s*:?-{3,}\s*\||[^\s|])/g

function normalizeTableDelimiterCell(cell: string): string {
  const trimmed = cell.trim()
  if (trimmed === '') return ' --- '
  if (!/^[:\-\s]+$/.test(trimmed) || !trimmed.includes('-')) return cell

  const leftAligned = trimmed.startsWith(':')
  const rightAligned = trimmed.endsWith(':')
  return ` ${leftAligned ? ':' : ''}---${rightAligned ? ':' : ''} `
}

function normalizeTableDelimiterRow(line: string): string {
  const trimmed = line.trim()
  if (!/^\|?[\s:|-]+\|?\s*$/.test(trimmed) || !trimmed.includes('|') || !trimmed.includes('-')) return line

  const startsWithPipe = trimmed.startsWith('|')
  const endsWithPipe = trimmed.endsWith('|')
  const cells = trimmed.replace(/^\|/, '').replace(/\|$/, '').split('|')
  if (cells.length < 2) return line

  return `${startsWithPipe ? '|' : ''}${cells.map(normalizeTableDelimiterCell).join('|')}${endsWithPipe ? '|' : ''}`
}

function repairCollapsedTableBlock(block: string): string {
  const pipeCount = (block.match(/\|/g) ?? []).length
  if (pipeCount < 6) return block

  let repaired = block
  if (repaired.includes('||') && COLLAPSED_PIPE_TABLE_SEPARATOR_RE.test(repaired)) {
    repaired = repaired.replace(COLLAPSED_PIPE_TABLE_ROW_BREAK_RE, '|\n| ')
  }

  const lines = repaired.split('\n')
  let changed = repaired !== block
  const normalizedLines = lines.map((line) => {
    const nextLine = normalizeTableDelimiterRow(line)
    if (nextLine !== line) changed = true
    return nextLine
  })

  return changed ? normalizedLines.join('\n') : block
}

function normalizeCollapsedPipeTables(content: string): string {
  const parts = content.split(/(```[\s\S]*?```)/g)

  return parts.map((part, index) => {
    if (index % 2 === 1) return part

    return part
      .split(/(\n{2,})/g)
      .map((block, blockIndex) => (blockIndex % 2 === 1 ? block : repairCollapsedTableBlock(block)))
      .join('')
  }).join('')
}

function useStreamingRenderContent(content: string, throttle: boolean): string {
  const [renderContent, setRenderContent] = useState(content)
  const timerRef = useRef<number | null>(null)

  useEffect(() => {
    if (!throttle) {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
      return
    }

    if (content.length <= renderContent.length) {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
      return
    }

    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => {
      setRenderContent((current) => (current === content ? current : content))
      timerRef.current = null
    }, STREAMING_MATH_COMMIT_INTERVAL_MS)

    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }
  }, [content, renderContent.length, throttle])

  if (!throttle && renderContent !== content) {
    return content
  }
  if (content.length <= renderContent.length && renderContent !== content) {
    return content
  }

  return renderContent
}

const ARTIFACT_PREFIX = 'artifact:'
const WORKSPACE_PREFIX = 'workspace:'

// react-markdown v10 的 defaultUrlTransform 会过滤非标准协议，需要放行 artifact:/workspace:
const artifactUrlTransform: UrlTransform = (url) => {
  if (url.startsWith(ARTIFACT_PREFIX) || url.startsWith(WORKSPACE_PREFIX)) return url
  return defaultUrlTransform(url)
}

function findArtifactByKey(artifacts: ArtifactRef[], key: string): ArtifactRef | undefined {
  return artifacts.find((a) => a.key === key)
}

const EXT_MIME: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif',
  svg: 'image/svg+xml', webp: 'image/webp', html: 'text/html', htm: 'text/html',
  pdf: 'application/pdf', csv: 'text/csv', txt: 'text/plain', md: 'text/markdown',
  json: 'application/json', log: 'text/plain', py: 'text/x-python', ts: 'text/typescript',
  tsx: 'text/typescript', js: 'text/javascript', jsx: 'text/javascript', sh: 'text/x-shellscript',
  yml: 'text/yaml', yaml: 'text/yaml', xml: 'application/xml', sql: 'text/plain', go: 'text/plain',
}

function guessMimeType(key: string): string {
  const ext = key.split('.').pop()?.toLowerCase() ?? ''
  return EXT_MIME[ext] ?? 'application/octet-stream'
}

function buildWorkspaceFileRef(path: string): WorkspaceFileRef {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return {
    path: normalizedPath,
    filename: normalizedPath.split('/').pop() ?? normalizedPath,
    mime_type: guessMimeType(normalizedPath),
  }
}

// artifact: 协议感知的 img 渲染器
function ArtifactAwareImg({ src, alt }: { src?: string; alt?: string }) {
  const { artifacts, accessToken, runId, onOpenDocument } = useContext(ArtifactsContext)
  const [failed, setFailed] = useState(false)

  if (src?.startsWith(ARTIFACT_PREFIX)) {
    const key = src.slice(ARTIFACT_PREFIX.length)
    const artifact = findArtifactByKey(artifacts, key)

    if (!artifact || !accessToken) return null

    if (artifact.mime_type.startsWith('image/')) {
      return <ArtifactImage artifact={artifact} accessToken={accessToken} />
    }
    if (artifact.mime_type === 'text/html') {
      return <ArtifactHtmlPreview artifact={artifact} accessToken={accessToken} />
    }
    if (onOpenDocument && isDocumentArtifact(artifact)) return null
    return <ArtifactDownload artifact={artifact} accessToken={accessToken} />
  }

  if (src?.startsWith(WORKSPACE_PREFIX)) {
    const file = buildWorkspaceFileRef(src.slice(WORKSPACE_PREFIX.length))
    if (!accessToken || !runId) return alt ? <span>{alt}</span> : null
    return <WorkspaceResource file={file} runId={runId} accessToken={accessToken} />
  }

  if (failed || !src) {
    return (
      <span
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          maxWidth: '320px',
          aspectRatio: '16 / 10',
          borderRadius: '8px',
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-deep)',
          color: 'var(--c-text-muted)',
          fontSize: '13px',
          margin: '0.5em 0',
        }}
      >
        {alt || '\u56fe\u7247\u52a0\u8f7d\u5931\u8d25'}
      </span>
    )
  }

  return <img src={src} alt={alt ?? ''} style={{ maxWidth: '100%', borderRadius: '8px' }} onError={() => setFailed(true)} />
}

// artifact: 协议感知的 a 渲染器
function ArtifactAwareLink({ href, children }: { href?: string; children?: ReactNode }) {
  const { artifacts, accessToken, runId, onOpenDocument } = useContext(ArtifactsContext)

  if (href?.startsWith(ARTIFACT_PREFIX)) {
    const key = href.slice(ARTIFACT_PREFIX.length)
    const artifact = findArtifactByKey(artifacts, key)

    if (!artifact || !accessToken) return <>{children}</>

    // LLM 可能用 [text](artifact:key) 而非 ![text](artifact:key)，统一按 mime_type 分派
    if (artifact.mime_type.startsWith('image/')) {
      return <ArtifactImage artifact={artifact} accessToken={accessToken} />
    }
    if (artifact.mime_type === 'text/html') {
      return <ArtifactHtmlPreview artifact={artifact} accessToken={accessToken} />
    }
    // 文档类型：有面板回调时抑制内联渲染（顶部卡片是唯一入口）
    if (onOpenDocument && isDocumentArtifact(artifact)) return null
    return <ArtifactDownload artifact={artifact} accessToken={accessToken} />
  }

  if (href?.startsWith(WORKSPACE_PREFIX)) {
    const file = buildWorkspaceFileRef(href.slice(WORKSPACE_PREFIX.length))
    if (!accessToken || !runId) return <>{children}</>
    return <WorkspaceResource file={file} runId={runId} accessToken={accessToken} />
  }

  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      style={{ color: 'var(--c-text-primary)', fontWeight: 400, fontSize: '0.92em', textDecoration: 'underline', textDecorationColor: 'var(--c-border-subtle)', textUnderlineOffset: '2px' }}
    >
      {children}
    </a>
  )
}

function hasStandaloneBlockPreview(children: ReactNode): boolean {
  const nodes = Children.toArray(children).filter((child) => {
    return typeof child !== 'string' || child.trim() !== ''
  })

  if (nodes.length !== 1) return false

  const child = nodes[0]
  if (!isValidElement<{ href?: string }>(child)) return false

  const href = typeof child.props?.href === 'string' ? child.props.href : ''
  if (href.startsWith(ARTIFACT_PREFIX) || href.startsWith(WORKSPACE_PREFIX)) return true

  return child.type === ArtifactHtmlPreview || child.type === WorkspaceResource
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

function CodeBlockWrapper({ children, compact = false }: { children: React.ReactNode; compact?: boolean }) {
  const [copyHover, setCopyHover] = useState(false)
  const preRef = useRef<HTMLPreElement>(null)
  const languageLabel = normalizeCodeLanguageLabel(extractCodeLanguage(children))
  const frameRadius = 10
  const labelFontSize = compact ? '10px' : '11px'
  const codeFontSize = compact ? '12.5px' : '13.5px'
  const codePadding = compact ? '34px 42px 12px 14px' : '36px 44px 14px 16px'

  const handleCopy = useCallback(() => {
    const text = preRef.current?.textContent ?? ''
    void navigator.clipboard.writeText(text)
  }, [])

  return (
    <div
      className="group/codeblock"
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
          top: '8px',
          left: '12px',
          zIndex: 1,
          display: 'inline-flex',
          alignItems: 'center',
          color: 'var(--c-text-tertiary)',
          fontSize: labelFontSize,
          letterSpacing: '0.18px',
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
          padding: codePadding,
          overflowX: 'auto',
          fontSize: codeFontSize,
          lineHeight: 1.65,
          fontFamily: "'JetBrains Mono', 'Cascadia Code', 'Fira Code', monospace",
          margin: 0,
        }}
      >
        {children}
      </pre>
      <CopyIconButton
        onCopy={handleCopy}
        size={13}
        className="opacity-0 group-hover/codeblock:opacity-100"
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
          color: 'var(--c-text-icon)',
          transition: 'opacity 150ms ease, background 150ms ease',
        }}
        onMouseEnter={() => setCopyHover(true)}
        onMouseLeave={() => setCopyHover(false)}
        resetDelay={2000}
      />
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

function buildMarkdownComponents(compact: boolean): Components {
  const paragraphFontSize = compact ? '13.5px' : '16.5px'
  const heading1FontSize = compact ? '20px' : '24px'
  const heading2FontSize = compact ? '17px' : '20px'
  const heading3FontSize = compact ? '15px' : '17px'
  const heading4FontSize = compact ? '15px' : '17px'
  const heading5FontSize = compact ? '13px' : '14px'
  const heading6FontSize = compact ? '13px' : '14px'
  const listFontSize = compact ? '13.5px' : '16.5px'

  return {
    pre: ({ children }) => {
    const lang = extractCodeLanguage(children)
    if (lang === 'mindmap') {
      return <MindmapBlock content={extractTextFromChildren(children)} />
    }
    if (lang === 'mermaid') {
      return <MermaidBlock content={extractTextFromChildren(children)} />
    }
    if (lang === 'ggbscript' || lang === 'ggb' || lang === 'geogebra') {
      return <GeoGebraBlock content={extractTextFromChildren(children)} />
    }
      return <CodeBlockWrapper compact={compact}>{children}</CodeBlockWrapper>
    },

    code: ({ className, children }) => (
      <code className={className}>{children}</code>
    ),

    p: ({ children }) => {
      if (hasStandaloneBlockPreview(children)) {
        return (
          <div style={{ margin: '0 0 0.5em' }}>
            <WithCitations prefix="p">{children}</WithCitations>
          </div>
        )
      }

      return (
        <p style={{ color: 'var(--c-text-body)', fontWeight: 'var(--c-text-body-weight)' as unknown as number, fontSize: paragraphFontSize, lineHeight: 1.6, letterSpacing: '0.01px', margin: '0 0 0.5em' }}>
          <WithCitations prefix="p">{children}</WithCitations>
        </p>
      )
    },

    h1: ({ children }) => (
      <h1 style={{ color: 'var(--c-text-heading)', fontSize: heading1FontSize, fontWeight: 400, lineHeight: 1.35, margin: '1.5em 0 0.5em', letterSpacing: '-0.3px' }}>
        {children}
      </h1>
    ),

    h2: ({ children }) => (
      <h2 style={{ color: 'var(--c-text-heading)', fontSize: heading2FontSize, fontWeight: 400, lineHeight: 1.35, margin: '1.4em 0 0.5em', letterSpacing: '-0.2px' }}>
        {children}
      </h2>
    ),

    h3: ({ children }) => (
      <h3 style={{ color: 'var(--c-text-heading)', fontSize: heading3FontSize, fontWeight: 400, lineHeight: 1.4, margin: '1.2em 0 0.4em' }}>
        {children}
      </h3>
    ),

    h4: ({ children }) => (
      <h4 style={{ color: 'var(--c-text-heading)', fontSize: heading4FontSize, fontWeight: 400, lineHeight: 1.4, margin: '1em 0 0.4em' }}>
        {children}
      </h4>
    ),

    h5: ({ children }) => (
      <h5 style={{ color: 'var(--c-text-heading)', fontSize: heading5FontSize, fontWeight: 400, lineHeight: 1.4, margin: '0.8em 0 0.3em' }}>
        {children}
      </h5>
    ),

    h6: ({ children }) => (
      <h6 style={{ color: 'var(--c-text-heading)', fontSize: heading6FontSize, fontWeight: 450, lineHeight: 1.4, margin: '0.8em 0 0.3em' }}>
        {children}
      </h6>
    ),

    ul: ({ children }) => (
      <ul style={{ color: 'var(--c-text-body)', fontWeight: 'var(--c-text-body-weight)' as unknown as number, fontSize: listFontSize, lineHeight: 1.6, paddingLeft: '2em', margin: '0 0 1em', listStyleType: 'disc' }}>
        {children}
      </ul>
    ),

    ol: ({ children }) => (
      <ol style={{ color: 'var(--c-text-body)', fontWeight: 'var(--c-text-body-weight)' as unknown as number, fontSize: listFontSize, lineHeight: 1.6, paddingLeft: '2em', margin: '0 0 1em', listStyleType: 'decimal' }}>
        {children}
      </ol>
    ),

    li: ({ children }) => <li style={{ marginBottom: '0.3em' }}><WithCitations prefix="li">{children}</WithCitations></li>,

    blockquote: ({ children }) => (
      <blockquote style={{ borderLeft: '3px solid var(--c-blockquote-bar)', paddingLeft: '1em', margin: '1em 0', color: 'var(--c-text-secondary)', fontStyle: 'italic' }}>
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
      <strong style={{ color: 'var(--c-text-heading)', fontWeight: 450 }}>{children}</strong>
    ),

    em: ({ children }) => (
      <em style={{ fontStyle: 'italic', color: 'var(--c-text-secondary)' }}>{children}</em>
    ),

    del: ({ children }) => (
      <del style={{ textDecoration: 'line-through' }}>{children}</del>
    ),
  }
}

type Props = {
  content: string
  disableMath?: boolean
  streaming?: boolean
  webSources?: WebSource[]
  artifacts?: ArtifactRef[]
  accessToken?: string
  runId?: string
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  compact?: boolean
  /** 下一兄弟为 COP 等块时去掉末段底距，避免正文→COP 过大缝隙 */
  trimTrailingMargin?: boolean
}

export function MarkdownRenderer({ content, disableMath, streaming = false, webSources, artifacts, accessToken, runId, onOpenDocument, compact = false, trimTrailingMargin = false }: Props) {
  const sourceCount = webSources?.length ?? 0
  const artifactCount = artifacts?.length ?? 0
  const shouldThrottleStreamingMath = streaming && !disableMath && containsLikelyMath(content)
  const renderContent = useStreamingRenderContent(content, shouldThrottleStreamingMath)
  const effectiveDisableMath = !!disableMath
  const remarkPlugins = useMemo(
    () => (effectiveDisableMath ? [remarkGfm] : [remarkGfm, remarkMath]),
    [effectiveDisableMath],
  )

  const rehypePlugins = useMemo<NonNullable<Options['rehypePlugins']>>(
    () => (
      effectiveDisableMath
        ? (streaming ? [] : [[rehypeHighlight, { ignoreMissing: true }]])
        : streaming
          ? [[rehypeKatex, { throwOnError: false, output: 'htmlAndMathml' }]]
          : [
              [rehypeKatex, { throwOnError: false, output: 'htmlAndMathml' }],
              [rehypeHighlight, { ignoreMissing: true }],
            ]
    ),
    [effectiveDisableMath, streaming],
  )

  const artifactsValue = useMemo<ArtifactsContextValue>(() => ({
    artifacts: artifacts ?? [],
    accessToken: accessToken ?? '',
    runId,
    onOpenDocument,
  }), [accessToken, artifacts, onOpenDocument, runId])

  const normalizedContent = useMemo(() => {
    const structuredContent = normalizeCollapsedPipeTables(renderContent)
    return effectiveDisableMath ? structuredContent : normalizeLatexDelimiters(structuredContent)
  }, [effectiveDisableMath, renderContent])
  const mdComponents = useMemo(() => buildMarkdownComponents(compact), [compact])

  useEffect(() => {
    recordPerfCount('markdown_render', 1, {
      length: content.length,
      renderLength: renderContent.length,
      compact,
      disableMath: !!disableMath,
      streaming,
      throttledMath: shouldThrottleStreamingMath,
      hasWebSources: sourceCount > 0,
      hasArtifacts: artifactCount > 0,
    })
    recordPerfValue('markdown_content_length', content.length, 'chars', {
      compact,
      disableMath: !!disableMath,
      streaming,
    })
  }, [artifactCount, compact, content.length, disableMath, renderContent.length, shouldThrottleStreamingMath, sourceCount, streaming])

  return (
    <ArtifactsContext.Provider value={artifactsValue}>
      <WebSourcesContext.Provider value={webSources ?? []}>
        <div
          className={`md-content${compact ? ' md-content--compact' : ''}${trimTrailingMargin ? ' md-content--trim-trailing' : ''}`}
          style={{ maxWidth: '100%', fontWeight: 350 }}
        >
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
