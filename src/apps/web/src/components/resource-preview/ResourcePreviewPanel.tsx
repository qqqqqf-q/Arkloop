import { memo, useEffect, useState } from 'react'
import { Code, Eye, FileText, X } from 'lucide-react'
import { SettingsSegmentedControl } from '../settings/_SettingsSegmentedControl'
import type { ArtifactRef } from '../../storage'
import { BrowserResourcePanel } from './BrowserResourcePanel'
import { loadPreviewResource } from './loader'
import { PreviewResourceView } from './PreviewResourceView'
import type { PreviewResource, ResourceRef } from './types'
import { extractPlanNameFromMarkdown, isPlanMarkdownPath } from '../../planMetadata'
import { useLocale } from '../../contexts/LocaleContext'

type ViewMode = 'preview' | 'source'

type Props = {
  resource: ResourceRef
  accessToken: string
  artifacts?: ArtifactRef[]
  runId?: string
  onClose?: () => void
  onResourceChange?: (resource: ResourceRef) => void
}

function releaseResource(resource: PreviewResource | null): void {
  if (resource?.blobUrl) URL.revokeObjectURL(resource.blobUrl)
}

function formatSize(size?: number): string {
  if (size === undefined) return ''
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function getResourceFilename(resource: ResourceRef): string {
  const pathName = 'path' in resource ? resource.path.split('/').filter(Boolean).at(-1) : undefined
  return ('filename' in resource ? resource.filename : undefined) ?? ('name' in resource ? resource.name : undefined) ?? ('title' in resource ? resource.title : undefined) ?? pathName ?? 'Preview'
}

export const ResourcePreviewPanel = memo(function ResourcePreviewPanel({ resource, accessToken, artifacts, runId, onClose, onResourceChange }: Props) {
  const { locale } = useLocale()
  const [mode, setMode] = useState<ViewMode>('preview')
  const [state, setState] = useState<{
    resource: ResourceRef | null
    loaded: PreviewResource | null
    error: string | null
  }>({ resource: null, loaded: null, error: null })

  useEffect(() => {
    if (resource.kind === 'browser') return
    const controller = new AbortController()
    let created: PreviewResource | null = null

    loadPreviewResource(resource, { accessToken, signal: controller.signal })
      .then((next) => {
        if (controller.signal.aborted) {
          releaseResource(next)
          return
        }
        created = next
        setMode('preview')
        setState({ resource, loaded: next, error: null })
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          setState({ resource, loaded: null, error: err instanceof Error ? err.message : 'unknown' })
        }
      })

    return () => {
      controller.abort()
      releaseResource(created)
    }
  }, [resource, accessToken])

  if (resource.kind === 'browser') {
    return <BrowserResourcePanel resource={resource} onClose={onClose} onResourceChange={onResourceChange} />
  }

  const current = state.resource === resource ? state : { resource, loaded: null, error: null }
  const loaded = current.loaded
  const canToggleSource = loaded?.text !== undefined
  const rawFilename = loaded?.filename ?? getResourceFilename(resource)
  const filename = loaded?.text && isPlanMarkdownPath(rawFilename)
    ? extractPlanNameFromMarkdown(loaded.text) ?? rawFilename
    : rawFilename
  const meta = loaded ? [loaded.mimeType, formatSize(loaded.size)].filter(Boolean).join(' · ') : ''
  const previewLabel = locale === 'zh' ? '预览' : 'Preview'
  const sourceLabel = locale === 'zh' ? '源码' : 'Source'
  const closeLabel = locale === 'zh' ? '关闭' : 'Close'

  return (
    <div style={{ height: '100%', minWidth: 0, display: 'flex', flexDirection: 'column', background: 'var(--c-bg-page)' }}>
      <div style={{ minHeight: 66, flexShrink: 0, borderBottom: '0.5px solid var(--c-border-subtle)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '10px 18px', minWidth: 0 }}>
        <div style={{ minWidth: 0, display: 'flex', alignItems: 'center', gap: 10 }}>
          <FileText size={17} color="var(--c-text-tertiary)" />
          <div style={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <span style={{ color: 'var(--c-text-primary)', fontSize: 14, fontWeight: 500, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{filename}</span>
            {meta ? <span style={{ color: 'var(--c-text-muted)', fontSize: 12 }}>{meta}</span> : null}
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0 }}>
          {canToggleSource && (
            <SettingsSegmentedControl<ViewMode>
              value={mode}
              onChange={setMode}
              density="icon"
              options={[
                {
                  value: 'preview',
                  title: previewLabel,
                  ariaLabel: previewLabel,
                  label: <Eye size={14} />,
                },
                {
                  value: 'source',
                  title: sourceLabel,
                  ariaLabel: sourceLabel,
                  label: <Code size={14} />,
                },
              ]}
            />
          )}
          {onClose ? (
            <button
              type="button"
              onClick={onClose}
              title={closeLabel}
              aria-label={closeLabel}
              style={{ width: 30, height: 30, display: 'grid', placeItems: 'center', border: 0, borderRadius: 8, background: 'transparent', color: 'var(--c-text-secondary)', cursor: 'pointer' }}
              onMouseEnter={(event) => { event.currentTarget.style.background = 'var(--c-bg-deep)' }}
              onMouseLeave={(event) => { event.currentTarget.style.background = 'transparent' }}
            >
              <X size={16} />
            </button>
          ) : null}
        </div>
      </div>
      <div style={{ flex: 1, minHeight: 0, overflow: mode === 'preview' && loaded?.mimeType === 'text/html' ? 'hidden' : 'auto' }}>
        {current.error ? (
          <div style={{ padding: 18, color: 'var(--c-text-muted)', fontSize: 13 }}>{current.error}</div>
        ) : loaded ? (
          <PreviewResourceView resource={loaded} accessToken={accessToken} artifacts={artifacts} runId={runId} mode={mode} />
        ) : (
          <div style={{ padding: 18, color: 'var(--c-text-muted)', fontSize: 13 }}>{locale === 'zh' ? '加载中' : 'Loading'}</div>
        )}
      </div>
    </div>
  )
})
