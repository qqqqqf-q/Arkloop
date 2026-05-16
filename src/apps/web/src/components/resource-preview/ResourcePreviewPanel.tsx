import { memo, useCallback, useEffect, useRef, useState } from 'react'
import { ChevronRight, Code, Eye, FileText, MoreHorizontal, X } from 'lucide-react'
import { Button } from '@arkloop/shared'
import { SettingsSegmentedControl } from '../settings/_SettingsSegmentedControl'
import { DropdownAction } from '../DropdownAction'
import type { ArtifactRef } from '../../storage'
import {
  readSelectedModelFromStorage,
  SELECTED_MODEL_CHANGED_EVENT,
  writeSelectedModelToStorage,
} from '../../storage'
import { BrowserResourcePanel } from './BrowserResourcePanel'
import { loadPreviewResource } from './loader'
import { PreviewResourceView } from './PreviewResourceView'
import type { PreviewResource, ResourceRef } from './types'
import { isPreviewModeToggleable } from './rendererKind'
import { extractPlanNameFromMarkdown, isPlanMarkdownPath, parsePlanMarkdown, PLAN_TODOS_UPDATED_EVENT, resolvePlanBuildState } from '../../planMetadata'
import { useLocale } from '../../contexts/LocaleContext'
import { ModelPicker } from '../ModelPicker'

type ViewMode = 'preview' | 'source'

type Props = {
  resource: ResourceRef
  accessToken: string
  artifacts?: ArtifactRef[]
  runId?: string
  workFolder?: string | null
  chrome?: 'default' | 'content-only'
  mode?: ViewMode
  onModeChange?: (mode: ViewMode) => void
  onClose?: () => void
  onResourceChange?: (resource: ResourceRef) => void
  onBuildPlan?: (message: string) => void
  onOpenModelSettings?: () => void
  onPlanTitleChange?: (title: string) => void
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

function basename(path: string | undefined | null): string {
  const normalized = (path ?? '').replace(/\\/g, '/').replace(/\/+$/g, '')
  return normalized.split('/').filter(Boolean).at(-1) ?? ''
}

function workspaceLabel(resource: ResourceRef, workFolder?: string | null): string {
  if (workFolder) return basename(workFolder) || 'Arkloop'
  if (resource.kind === 'local-file') return basename(resource.rootPath) || 'Arkloop'
  return 'Arkloop'
}

function planReferencePath(resource: ResourceRef, loaded: PreviewResource): string {
  const ref = loaded.ref ?? resource
  if (ref.kind === 'local-file') return ref.path.replace(/\\/g, '/')
  if (ref.kind === 'workspace-file') return ref.path.replace(/^\/+/, '')
  if (ref.kind === 'artifact') return ref.filename ?? loaded.filename
  return loaded.filename
}

function buildPlanMessage(locale: 'zh' | 'en', path: string): string {
  if (locale === 'zh') return `开始执行这个计划。\n\n计划文件：${path}`
  return `Start executing this plan.\n\nPlan file: ${path}`
}

function normalizePlanPath(path: string): string {
  return path.replace(/\\/g, '/').replace(/\/+$/g, '')
}

function BreadcrumbPart({ children }: { children: string }) {
  return (
    <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
      {children}
    </span>
  )
}

function PreviewActionsMenu({ closeLabel, onClose }: { closeLabel: string; onClose?: () => void }) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    const handlePointerDown = (event: PointerEvent) => {
      if (menuRef.current?.contains(event.target as Node)) return
      setOpen(false)
    }
    window.addEventListener('pointerdown', handlePointerDown)
    return () => window.removeEventListener('pointerdown', handlePointerDown)
  }, [open])

  if (!onClose) return null

  return (
    <div ref={menuRef} style={{ position: 'relative', flexShrink: 0 }}>
      <button
        type="button"
        title="More"
        aria-label="More"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        style={{ width: 30, height: 30, display: 'grid', placeItems: 'center', border: 0, borderRadius: 8, background: open ? 'var(--c-bg-deep)' : 'transparent', color: 'var(--c-text-secondary)', cursor: 'pointer' }}
      >
        <MoreHorizontal size={16} />
      </button>
      <div
        data-open={open}
        style={{
          position: 'absolute',
          zIndex: 30,
          top: 'calc(100% + 8px)',
          right: 0,
          width: 150,
          padding: 4,
          border: '0.65px solid color-mix(in srgb, var(--c-border) 78%, var(--c-bg-input) 22%)',
          borderRadius: 10,
          background: 'var(--c-bg-menu)',
          boxShadow: 'var(--c-dropdown-shadow)',
          opacity: open ? 1 : 0,
          transform: open ? 'translateY(0) scale(1)' : 'translateY(-4px) scale(0.98)',
          transformOrigin: 'top right',
          pointerEvents: open ? 'auto' : 'none',
          transition: 'opacity 150ms cubic-bezier(0.22, 1, 0.36, 1), transform 150ms cubic-bezier(0.22, 1, 0.36, 1)',
        }}
      >
        <DropdownAction
          icon={<X size={14} />}
          label={closeLabel}
          onClick={() => {
            setOpen(false)
            onClose()
          }}
        />
      </div>
    </div>
  )
}

export const ResourcePreviewPanel = memo(function ResourcePreviewPanel({
  resource,
  accessToken,
  artifacts,
  runId,
  workFolder,
  chrome = 'default',
  mode: controlledMode,
  onModeChange,
  onClose,
  onResourceChange,
  onBuildPlan,
  onOpenModelSettings,
  onPlanTitleChange,
}: Props) {
  const { locale } = useLocale()
  const [internalMode, setInternalMode] = useState<ViewMode>('preview')
  const [selectedModel, setSelectedModel] = useState<string | null>(readSelectedModelFromStorage)
  const [buildRequestedPath, setBuildRequestedPath] = useState<string | null>(null)
  const [refreshNonce, setRefreshNonce] = useState(0)
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
        if (next.text && isPlanMarkdownPath(next.filename)) {
          const plan = parsePlanMarkdown(next.text)
          if (plan?.name) onPlanTitleChange?.(plan.name)
        }
        if (controlledMode === undefined) setInternalMode('preview')
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
  }, [resource, accessToken, controlledMode, onPlanTitleChange, refreshNonce])

  useEffect(() => {
    const syncSelectedModel = () => setSelectedModel(readSelectedModelFromStorage())
    window.addEventListener(SELECTED_MODEL_CHANGED_EVENT, syncSelectedModel)
    return () => window.removeEventListener(SELECTED_MODEL_CHANGED_EVENT, syncSelectedModel)
  }, [])

  useEffect(() => {
    const loaded = state.resource === resource ? state.loaded : null
    if (!loaded?.text || !isPlanMarkdownPath(loaded.filename)) return
    const currentPath = normalizePlanPath(planReferencePath(resource, loaded))
    const handlePlanTodosUpdated = (event: Event) => {
      const detail = (event as CustomEvent<{ planPath?: unknown }>).detail
      if (typeof detail?.planPath !== 'string') return
      if (normalizePlanPath(detail.planPath) !== currentPath) return
      setRefreshNonce((value) => value + 1)
    }
    window.addEventListener(PLAN_TODOS_UPDATED_EVENT, handlePlanTodosUpdated)
    return () => window.removeEventListener(PLAN_TODOS_UPDATED_EVENT, handlePlanTodosUpdated)
  }, [resource, state])

  const handleModelChange = useCallback((model: string | null) => {
    setSelectedModel(model)
    writeSelectedModelToStorage(model)
  }, [])

  if (resource.kind === 'browser') {
    return <BrowserResourcePanel resource={resource} onClose={onClose} onResourceChange={onResourceChange} />
  }

  const mode = controlledMode ?? internalMode
  const setMode = onModeChange ?? setInternalMode
  const current = state.resource === resource ? state : { resource, loaded: null, error: null }
  const loaded = current.loaded
  const rawFilename = loaded?.filename ?? getResourceFilename(resource)
  const plan = loaded?.text && isPlanMarkdownPath(rawFilename) ? parsePlanMarkdown(loaded.text) : null
  const isPlan = Boolean(plan)
  const filename = plan?.name ?? (loaded?.text && isPlanMarkdownPath(rawFilename)
    ? extractPlanNameFromMarkdown(loaded.text) ?? rawFilename
    : rawFilename)
  const canToggleSource = loaded?.text !== undefined && !isPlan && isPreviewModeToggleable(loaded)
  const meta = loaded ? [loaded.mimeType, formatSize(loaded.size)].filter(Boolean).join(' · ') : ''
  const previewLabel = locale === 'zh' ? '预览' : 'Preview'
  const sourceLabel = locale === 'zh' ? '源码' : 'Source'
  const closeLabel = locale === 'zh' ? '关闭' : 'Close'
  const buildLabel = 'Build'
  const plansLabel = 'Plans'

  if (isPlan && loaded && plan) {
    const path = planReferencePath(resource, loaded)
    const title = plan.name ?? filename
    const buildState = resolvePlanBuildState(plan, buildRequestedPath === path)
    const handleBuild = () => {
      setBuildRequestedPath(path)
      onBuildPlan?.(buildPlanMessage(locale, path))
    }
    const buildButtonLabel = buildState === 'building' ? 'Building...' : buildLabel

    return (
      <div style={{ height: '100%', minWidth: 0, display: 'flex', flexDirection: 'column', background: 'var(--c-bg-page)' }}>
        {chrome === 'default' ? (
        <div style={{ minHeight: 42, flexShrink: 0, borderBottom: '0.5px solid var(--c-border-subtle)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '0 12px', minWidth: 0 }}>
          <div style={{ minWidth: 0, display: 'flex', alignItems: 'center', gap: 8, color: 'var(--c-text-secondary)', fontSize: 14 }}>
            <BreadcrumbPart>{workspaceLabel(resource, workFolder)}</BreadcrumbPart>
            <ChevronRight size={14} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
            <BreadcrumbPart>{plansLabel}</BreadcrumbPart>
            <ChevronRight size={14} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--c-text-primary)', fontWeight: 405 }}>
              {title}
            </span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
            <ModelPicker
              accessToken={accessToken}
              value={selectedModel}
              onChange={handleModelChange}
              onAddModel={() => onOpenModelSettings?.()}
              thinkingEnabled="off"
              onThinkingChange={() => {}}
              showReasoningOptions={false}
              controlHeight={30}
            />
            {buildState === 'built' ? (
              <span style={{ height: 30, display: 'inline-flex', alignItems: 'center', color: 'var(--c-status-success-text)', fontSize: 14, fontWeight: 'var(--c-fw-450)', padding: '0 4px' }}>
                Built
              </span>
            ) : onBuildPlan ? (
              <Button type="button" size="sm" className="px-3" style={{ height: 30 }} disabled={buildState === 'building'} onClick={handleBuild}>
                {buildButtonLabel}
              </Button>
            ) : null}
            <PreviewActionsMenu closeLabel={closeLabel} onClose={onClose} />
          </div>
        </div>
        ) : null}
        <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
          <PreviewResourceView resource={loaded} accessToken={accessToken} artifacts={artifacts} runId={runId} mode="preview" />
        </div>
      </div>
    )
  }

  return (
    <div style={{ height: '100%', minWidth: 0, display: 'flex', flexDirection: 'column', background: 'var(--c-bg-page)' }}>
      {chrome === 'default' ? (
      <div style={{ minHeight: 42, flexShrink: 0, borderBottom: '0.5px solid var(--c-border-subtle)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '0 12px', minWidth: 0 }}>
        <div style={{ minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
          <FileText size={17} color="var(--c-text-tertiary)" />
          <span style={{ minWidth: 0, color: 'var(--c-text-primary)', fontSize: 14, fontWeight: 480, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{filename}</span>
          {meta ? <span style={{ flexShrink: 0, color: 'var(--c-text-muted)', fontSize: 12 }}>{meta}</span> : null}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
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
          <PreviewActionsMenu closeLabel={closeLabel} onClose={onClose} />
        </div>
      </div>
      ) : null}
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
