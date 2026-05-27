export type ResourceSource = 'local-file' | 'artifact' | 'workspace-file' | 'browser' | 'mcp-app'

export type LocalFileResourceRef = {
  kind: 'local-file'
  source?: 'local-file'
  rootPath: string
  path: string
  name?: string
  filename?: string
  mimeType?: string
  size?: number
}

export type ArtifactResourceRef = {
  kind: 'artifact'
  source?: 'artifact'
  key: string
  filename?: string
  mimeType?: string
  size?: number
  title?: string
}

export type WorkspaceFileResourceRef = {
  kind: 'workspace-file'
  source?: 'workspace-file'
  path: string
  name?: string
  filename?: string
  mimeType?: string
  size?: number
  runId?: string
  projectId?: string
}

export type BrowserResourceRef = {
  kind: 'browser'
  source?: 'browser'
  url: string
  title?: string
  faviconUrl?: string
}

export type McpAppResourceRef = {
  kind: 'mcp-app'
  source?: 'mcp-app'
  uri: string
  content: string
  filename?: string
  mimeType?: string
  size?: number
  title?: string
  csp?: {
    connectDomains?: string[]
    resourceDomains?: string[]
    frameDomains?: string[]
    baseUriDomains?: string[]
  }
  initialData?: unknown
  serverId?: string
  toolName?: string
  toolInput?: Record<string, unknown>
}

export type ResourceRef = LocalFileResourceRef | ArtifactResourceRef | WorkspaceFileResourceRef | BrowserResourceRef | McpAppResourceRef

export type PreviewResource = {
  source: ResourceSource
  ref: ResourceRef
  mimeType: string
  filename: string
  size?: number
  text?: string
  blobUrl?: string
}

export type LoadPreviewResourceOptions = {
  accessToken?: string
  signal?: AbortSignal
}
