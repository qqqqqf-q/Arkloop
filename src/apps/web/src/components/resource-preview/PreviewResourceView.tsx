import { memo, type ReactNode } from 'react'
import { FileIcon } from 'lucide-react'
import { ArtifactIframe } from '../ArtifactIframe'
import type { ArtifactRef } from '../../storage'
import type { PreviewResource } from './types'
import { getPreviewRendererKind, type PreviewRendererKind } from './rendererKind'
import { MarkdownDocumentRenderer } from './MarkdownDocumentRenderer'
import { SourceDocumentRenderer } from './SourceDocumentRenderer'

function formatSize(size: number | undefined): string {
  if (size === undefined) return ''
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function Frame({ children, kind }: { children: ReactNode; kind: PreviewRendererKind }) {
  return (
    <div
      data-preview-renderer={kind}
      style={{
        width: '100%',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: 8,
        background: 'var(--c-bg-sub)',
        overflow: 'hidden',
      }}
    >
      {children}
    </div>
  )
}

function TextPreview({ resource, kind }: { resource: PreviewResource; kind: 'json' | 'code' | 'text' }) {
  return (
    <div data-preview-renderer={kind}>
      <SourceDocumentRenderer content={resource.text ?? ''} filename={resource.filename} mimeType={resource.mimeType} />
    </div>
  )
}

function BinaryPreview({ resource }: { resource: PreviewResource }) {
  return (
    <Frame kind="binary">
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: 12, color: 'var(--c-text-secondary)' }}>
        <FileIcon size={16} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
        <div style={{ minWidth: 0 }}>
          <div style={{ color: 'var(--c-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {resource.filename}
          </div>
          <div style={{ fontSize: 12 }}>
            {[resource.mimeType, formatSize(resource.size)].filter(Boolean).join(' · ')}
          </div>
        </div>
      </div>
    </Frame>
  )
}

type Props = {
  resource: PreviewResource
  accessToken?: string
  artifacts?: ArtifactRef[]
  runId?: string
  mode?: 'preview' | 'source'
}

function SourcePreview({ resource }: { resource: PreviewResource }) {
  return <SourceDocumentRenderer content={resource.text ?? ''} filename={resource.filename} mimeType={resource.mimeType} />
}

function PreviewResourceContent({
  resource,
  accessToken,
  artifacts,
  runId,
  mode,
}: {
  resource: PreviewResource
  accessToken: string
  artifacts?: ArtifactRef[]
  runId?: string
  mode: 'preview' | 'source'
}) {
  const kind = getPreviewRendererKind(resource)

  if (mode === 'source' && resource.text !== undefined) {
    return <SourcePreview resource={resource} />
  }

  if (kind === 'markdown') {
    return <MarkdownDocumentRenderer content={resource.text ?? ''} accessToken={accessToken} artifacts={artifacts} runId={runId} />
  }

  if (kind === 'iframe') {
    if (resource.text !== undefined) {
      return (
        <div data-preview-renderer="iframe" style={{ height: '100%', minHeight: 0 }}>
          <ArtifactIframe
            mode="static"
            content={resource.text}
            contentType={resource.mimeType}
            frameTitle={resource.filename}
            autoResize={false}
            style={{ border: 0, borderRadius: 0, minHeight: 0, height: '100%' }}
          />
        </div>
      )
    }
    if (resource.blobUrl) {
      return (
        <iframe
          data-preview-renderer="iframe"
          src={resource.blobUrl}
          title={resource.filename}
          sandbox=""
          style={{ width: '100%', height: '100%', minHeight: 0, border: 0, borderRadius: 0 }}
        />
      )
    }
  }

  if (kind === 'image' && resource.blobUrl) {
    return (
      <div data-preview-renderer="image" style={{ display: 'inline-block', maxWidth: '100%' }}>
        <img
          src={resource.blobUrl}
          alt={resource.filename}
          draggable={false}
          style={{ display: 'block', maxWidth: '100%', borderRadius: 8, border: '0.5px solid var(--c-border-subtle)' }}
        />
      </div>
    )
  }

  if (kind === 'json' || kind === 'code' || kind === 'text') {
    return <TextPreview resource={resource} kind={kind} />
  }

  return <BinaryPreview resource={resource} />
}

export const PreviewResourceView = memo(function PreviewResourceView({ resource, accessToken = '', artifacts, runId, mode = 'preview' }: Props) {
  return <PreviewResourceContent resource={resource} accessToken={accessToken} artifacts={artifacts} runId={runId} mode={mode} />
})
