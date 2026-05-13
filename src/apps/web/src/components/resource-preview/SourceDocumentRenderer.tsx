import { isJsonMime } from './mime'

type Props = {
  content: string
  filename?: string
  mimeType?: string
}

function displayContent(content: string, filename?: string, mimeType?: string): string {
  if (!isJsonMime(mimeType ?? '', filename ?? '')) return content
  try {
    return JSON.stringify(JSON.parse(content), null, 2)
  } catch {
    return content
  }
}

export function SourceDocumentRenderer({ content, filename, mimeType }: Props) {
  return (
    <div data-preview-renderer="source" style={{ width: '100%', minHeight: '100%', background: 'transparent' }}>
      <pre
        style={{
          margin: 0,
          padding: 16,
          minHeight: '100%',
          overflow: 'auto',
          color: 'var(--c-text-primary)',
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
          fontSize: 12,
          lineHeight: 1.6,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        <code>{displayContent(content, filename, mimeType)}</code>
      </pre>
    </div>
  )
}
