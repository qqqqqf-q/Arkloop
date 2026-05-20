import { isJsonMime } from './mime'
import { CodeDocumentViewer } from '../code-document/CodeDocumentViewer'

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
  const display = displayContent(content, filename, mimeType)
  return (
    <div data-preview-renderer="source" style={{ width: '100%', minHeight: '100%', background: 'transparent' }}>
      <CodeDocumentViewer content={display} filename={filename} mimeType={mimeType} fillHeight />
    </div>
  )
}
