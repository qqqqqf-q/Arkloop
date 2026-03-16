import { useEffect, useState } from 'react'
import {
  ChevronDown,
  Check,
  FileText,
  Folder,
  FolderOpen,
  Globe,
  Monitor,
} from 'lucide-react'

import {
  getProjectWorkspace,
  isApiError,
  listProjectWorkspaceFiles,
  type ProjectWorkspace,
  type ProjectWorkspaceFileEntry,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { WorkspaceResource } from './WorkspaceResource'

export type StepStatus = 'done' | 'active' | 'pending'

export type ProgressStep = {
  id: string
  label: string
  status: StepStatus
}

export type Connector = {
  name: string
  icon: 'globe' | 'monitor'
}

export type ClawRightPanelProps = {
  accessToken?: string
  projectId?: string
  steps?: ProgressStep[]
  connectors?: Connector[]
  onForbidden?: () => void
}

function AnimatedHeight({
  open,
  children,
}: {
  open: boolean
  children: React.ReactNode
}) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateRows: open ? '1fr' : '0fr',
        transition: 'grid-template-rows 220ms cubic-bezier(0.16,1,0.3,1)',
      }}
    >
      <div style={{ overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

function Card({
  title,
  badge,
  defaultOpen = true,
  children,
}: {
  title: string
  badge?: string
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <div
      style={{
        borderRadius: 12,
        border: '0.5px solid var(--c-claw-card-border)',
        background: 'var(--c-bg-page)',
        overflow: 'hidden',
      }}
    >
      <button
        onClick={() => setOpen((value) => !value)}
        style={{
          display: 'flex',
          width: '100%',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '14px 16px',
          background: 'transparent',
          border: 'none',
          cursor: 'pointer',
        }}
      >
        <span
          style={{
            fontSize: '14px',
            fontWeight: 600,
            color: 'var(--c-text-primary)',
            letterSpacing: '-0.2px',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {title}
        </span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
          {badge ? (
            <span style={{ fontSize: '12px', color: 'var(--c-text-muted)', textTransform: 'capitalize' }}>
              {badge}
            </span>
          ) : null}
          <span
            style={{
              color: 'var(--c-text-muted)',
              display: 'flex',
              transition: 'transform 200ms ease',
              transform: open ? 'rotate(0deg)' : 'rotate(-90deg)',
            }}
          >
            <ChevronDown size={16} />
          </span>
        </div>
      </button>
      <AnimatedHeight open={open}>
        <div style={{ padding: '0 16px 16px' }}>{children}</div>
      </AnimatedHeight>
    </div>
  )
}

function ProgressPanel({ steps }: { steps: ProgressStep[] }) {
  const { t } = useLocale()

  if (steps.length === 0) {
    return (
      <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
        {t.claw.progressEmpty}
      </p>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      {steps.map((step, index) => (
        <div key={step.id} style={{ display: 'flex', gap: 10 }}>
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              width: 20,
              flexShrink: 0,
            }}
          >
            {step.status === 'done' ? (
              <div
                style={{
                  width: 20,
                  height: 20,
                  borderRadius: '50%',
                  border: '1.5px solid var(--c-claw-step-done)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <Check size={11} color="var(--c-claw-step-done)" strokeWidth={2.5} />
              </div>
            ) : (
              <div
                style={{
                  width: 20,
                  height: 20,
                  borderRadius: '50%',
                  border: '1.5px solid var(--c-claw-step-pending)',
                }}
              />
            )}
            {index < steps.length - 1 ? (
              <div
                style={{
                  width: 1.5,
                  flex: 1,
                  minHeight: 12,
                  background: 'var(--c-claw-step-line)',
                }}
              />
            ) : null}
          </div>
          <span
            style={{
              fontSize: '13px',
              color: step.status === 'done' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)',
              lineHeight: '20px',
              paddingBottom: index < steps.length - 1 ? 8 : 0,
            }}
          >
            {step.label}
          </span>
        </div>
      ))}
    </div>
  )
}

function fileExtension(name: string): string {
  const ext = name.split('.').pop()?.trim().toLowerCase()
  return ext || 'file'
}

function DocIcon({ ext }: { ext?: string }) {
  const tag = ext?.toUpperCase() ?? ''
  return (
    <div
      style={{
        width: 22,
        height: 26,
        borderRadius: 3,
        border: '0.5px solid var(--c-claw-card-border)',
        background: 'var(--c-claw-file-bg)',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'flex-end',
        padding: '0 0 2px',
        flexShrink: 0,
        position: 'relative',
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: 0,
          right: 0,
          width: 6,
          height: 6,
          background: 'var(--c-bg-sub)',
          borderRadius: '0 0 0 1.5px',
        }}
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 1.5, alignItems: 'center', marginTop: 5 }}>
        <div style={{ width: 10, height: 0.5, background: 'var(--c-claw-step-pending)', borderRadius: 0.5 }} />
        <div style={{ width: 10, height: 0.5, background: 'var(--c-claw-step-pending)', borderRadius: 0.5 }} />
      </div>
      {tag ? (
        <span
          style={{
            fontSize: '6px',
            fontWeight: 700,
            color: 'var(--c-text-muted)',
            letterSpacing: '0.2px',
            marginTop: 1,
            lineHeight: 1,
          }}
        >
          {tag}
        </span>
      ) : null}
    </div>
  )
}

function ConnectorIcon({ icon }: { icon: Connector['icon'] }) {
  const style = { color: 'var(--c-text-muted)' }
  switch (icon) {
    case 'globe':
      return <Globe size={15} style={style} />
    case 'monitor':
      return <Monitor size={15} style={style} />
    default:
      return <Globe size={15} style={style} />
  }
}

function ContextPanel({ connectors }: { connectors: Connector[] }) {
  const { t } = useLocale()

  if (connectors.length === 0) {
    return (
      <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
        {t.claw.contextEmpty}
      </p>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: '12px', color: 'var(--c-text-muted)', marginBottom: 2 }}>{t.claw.toolsCalled}</span>
      {connectors.map((connector) => (
        <div
          key={connector.name}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '6px 2px',
            borderRadius: 6,
          }}
        >
          <div
            style={{
              width: 26,
              height: 26,
              borderRadius: 6,
              border: '0.5px solid var(--c-claw-card-border)',
              background: 'var(--c-claw-file-bg)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexShrink: 0,
            }}
          >
            <ConnectorIcon icon={connector.icon} />
          </div>
          <span style={{ fontSize: '13px', color: 'var(--c-text-secondary)' }}>{connector.name}</span>
        </div>
      ))}
    </div>
  )
}

function normalizePath(path: string): string {
  const trimmed = path.trim()
  return trimmed || '/'
}

function FileTree({
  itemsByPath,
  loadingPaths,
  expandedPaths,
  selectedFilePath,
  onToggleDir,
  onSelectFile,
  rootPath,
}: {
  itemsByPath: Record<string, ProjectWorkspaceFileEntry[]>
  loadingPaths: Record<string, boolean>
  expandedPaths: Record<string, boolean>
  selectedFilePath?: string
  onToggleDir: (entry: ProjectWorkspaceFileEntry) => void
  onSelectFile: (entry: ProjectWorkspaceFileEntry) => void
  rootPath: string
}) {
  const renderLevel = (currentPath: string, depth: number): React.ReactNode => {
    const items = itemsByPath[currentPath] ?? []
    return items.map((entry) => {
      const isDir = entry.type === 'dir'
      const isExpanded = Boolean(expandedPaths[entry.path])
      const isSelected = selectedFilePath === entry.path
      const loading = Boolean(loadingPaths[entry.path])
      const paddingLeft = depth * 14

      return (
        <div key={entry.path}>
          <button
            type="button"
            data-testid={`claw-file-entry-${entry.path}`}
            onClick={() => (isDir ? onToggleDir(entry) : onSelectFile(entry))}
            style={{
              width: '100%',
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '7px 8px',
              paddingLeft: 8 + paddingLeft,
              border: 'none',
              borderRadius: 8,
              background: isSelected ? 'var(--c-claw-file-bg)' : 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              textAlign: 'left',
            }}
          >
            <span style={{ width: 16, display: 'flex', justifyContent: 'center', flexShrink: 0 }}>
              {isDir ? (
                isExpanded ? <FolderOpen size={14} style={{ color: 'var(--c-text-muted)' }} /> : <Folder size={14} style={{ color: 'var(--c-text-muted)' }} />
              ) : (
                <DocIcon ext={fileExtension(entry.name)} />
              )}
            </span>
            <span
              style={{
                flex: 1,
                minWidth: 0,
                fontSize: '13px',
                lineHeight: 1.35,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {entry.name}
            </span>
            {isDir && loading ? (
              <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', flexShrink: 0 }}>…</span>
            ) : null}
          </button>
          {isDir && isExpanded ? <div>{renderLevel(normalizePath(entry.path), depth + 1)}</div> : null}
        </div>
      )
    })
  }

  return <div>{renderLevel(rootPath, 0)}</div>
}

function WorkingFolderPanel({ accessToken, projectId, onForbidden }: { accessToken?: string; projectId?: string; onForbidden?: () => void }) {
  const { t } = useLocale()
  const [workspace, setWorkspace] = useState<ProjectWorkspace | null>(null)
  const [workspaceLoading, setWorkspaceLoading] = useState(false)
  const [workspaceError, setWorkspaceError] = useState(false)
  const [itemsByPath, setItemsByPath] = useState<Record<string, ProjectWorkspaceFileEntry[]>>({})
  const [loadingPaths, setLoadingPaths] = useState<Record<string, boolean>>({})
  const [expandedPaths, setExpandedPaths] = useState<Record<string, boolean>>({})
  const [selectedFile, setSelectedFile] = useState<ProjectWorkspaceFileEntry | null>(null)
  const [filesError, setFilesError] = useState(false)

  useEffect(() => {
    let cancelled = false
    if (!accessToken || !projectId) {
      setWorkspace(null)
      setWorkspaceLoading(false)
      setWorkspaceError(false)
      setItemsByPath({})
      setLoadingPaths({})
      setExpandedPaths({})
      setSelectedFile(null)
      setFilesError(false)
      return
    }

    setWorkspaceLoading(true)
    setWorkspaceError(false)
    setFilesError(false)
    setItemsByPath({})
    setLoadingPaths({})
    setExpandedPaths({})
    setSelectedFile(null)

    Promise.all([
      getProjectWorkspace(accessToken, projectId),
      listProjectWorkspaceFiles(accessToken, projectId, '/'),
    ])
      .then(([workspaceResp, filesResp]) => {
        if (cancelled) return
        setWorkspace(workspaceResp)
        setItemsByPath({ [normalizePath(filesResp.path)]: filesResp.items })
      })
      .catch((err) => {
        if (cancelled) return
        if (isApiError(err) && err.status === 403) {
          onForbidden?.()
          return
        }
        setWorkspace(null)
        setWorkspaceError(true)
      })
      .finally(() => {
        if (cancelled) return
        setWorkspaceLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [accessToken, projectId])

  async function loadDirectory(entry: ProjectWorkspaceFileEntry) {
    if (!accessToken || !projectId || entry.type !== 'dir') return
    const normalizedPath = normalizePath(entry.path)
    if (itemsByPath[normalizedPath]) return

    setLoadingPaths((current) => ({ ...current, [normalizedPath]: true }))
    try {
      const response = await listProjectWorkspaceFiles(accessToken, projectId, normalizedPath)
      setItemsByPath((current) => ({ ...current, [normalizePath(response.path)]: response.items }))
      setFilesError(false)
    } catch (err) {
      if (isApiError(err) && err.status === 403) {
        onForbidden?.()
        return
      }
      setFilesError(true)
    } finally {
      setLoadingPaths((current) => {
        const next = { ...current }
        delete next[normalizedPath]
        return next
      })
    }
  }

  function handleToggleDir(entry: ProjectWorkspaceFileEntry) {
    const normalizedPath = normalizePath(entry.path)
    setExpandedPaths((current) => ({ ...current, [normalizedPath]: !current[normalizedPath] }))
    if (!expandedPaths[normalizedPath]) {
      void loadDirectory(entry)
    }
  }

  const rootPath = '/'
  const rootItems = itemsByPath[rootPath] ?? []
  const title = workspace?.workspace_ref || t.claw.workingFolder
  const badge = workspace?.status

  if (!projectId) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
          {t.claw.workingFolderEmpty}
        </p>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <span
          style={{
            fontSize: '12px',
            color: 'var(--c-text-muted)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={title}
        >
          {title}
        </span>
        {badge ? (
          <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', textTransform: 'capitalize', flexShrink: 0 }}>
            {badge}
          </span>
        ) : null}
      </div>

      {workspaceLoading ? (
        <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
          {t.claw.workingFolderLoading}
        </p>
      ) : null}

      {!workspaceLoading && workspaceError ? (
        <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
          {t.claw.workingFolderError}
        </p>
      ) : null}

      {!workspaceLoading && !workspaceError && rootItems.length === 0 ? (
        <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
          {t.claw.workingFolderEmptyDir}
        </p>
      ) : null}

      {!workspaceLoading && !workspaceError && rootItems.length > 0 ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div
            style={{
              borderRadius: 10,
              border: '0.5px solid var(--c-claw-card-border)',
              background: 'var(--c-bg-sub)',
              padding: 6,
            }}
          >
            <FileTree
              itemsByPath={itemsByPath}
              loadingPaths={loadingPaths}
              expandedPaths={expandedPaths}
              selectedFilePath={selectedFile?.path}
              onToggleDir={handleToggleDir}
              onSelectFile={setSelectedFile}
              rootPath={rootPath}
            />
          </div>

          {filesError ? (
            <p style={{ fontSize: '12px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
              {t.claw.workingFolderError}
            </p>
          ) : null}

          {selectedFile ? (
            <div data-testid="claw-file-preview" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <FileText size={14} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
                <span
                  style={{
                    fontSize: '12px',
                    color: 'var(--c-text-muted)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {selectedFile.path}
                </span>
              </div>
              <WorkspaceResource
                accessToken={accessToken ?? ''}
                projectId={projectId}
                file={{
                  path: selectedFile.path,
                  filename: selectedFile.name,
                  mime_type: selectedFile.mime_type,
                }}
              />
            </div>
          ) : (
            <p style={{ fontSize: '12px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
              {t.claw.workingFolderSelectFile}
            </p>
          )}
        </div>
      ) : null}
    </div>
  )
}

const clawPanelWidth = 300

export function ClawRightPanel({
  accessToken,
  projectId,
  steps = [],
  connectors = [],
  onForbidden,
}: ClawRightPanelProps) {
  const { t } = useLocale()
  const doneCount = steps.filter((step) => step.status === 'done').length

  return (
    <div
      style={{
        width: clawPanelWidth,
        height: '100%',
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        background: 'var(--c-bg-page)',
      }}
    >
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '8px 10px',
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
        }}
      >
        <Card title={t.claw.progress} badge={steps.length > 0 ? `${doneCount} of ${steps.length}` : undefined} defaultOpen>
          <ProgressPanel steps={steps} />
        </Card>

        <Card title={t.claw.workingFolder} defaultOpen>
          <WorkingFolderPanel accessToken={accessToken} projectId={projectId} onForbidden={onForbidden} />
        </Card>

        <Card title={t.claw.context} defaultOpen>
          <ContextPanel connectors={connectors} />
        </Card>
      </div>
    </div>
  )
}
