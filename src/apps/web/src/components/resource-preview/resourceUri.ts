import type { ArtifactRef } from '../../storage'
import type { ArtifactResourceRef, BrowserResourceRef, LocalFileResourceRef, ResourceRef, WorkspaceFileResourceRef } from './types'
import { filenameFromPath, guessMimeType, normalizeMimeType } from './mime'
import { normalizeBrowserUrl } from './browserIdentity'

export const ARTIFACT_URI_PREFIX = 'artifact:'
export const WORKSPACE_URI_PREFIX = 'workspace:'
export const FILE_URI_PREFIX = 'file:'
export const BROWSER_URI_PREFIX = 'browser:'

function normalizeWorkspacePath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed) return '/'
  return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

export function artifactToResourceRef(artifact: ArtifactRef): ArtifactResourceRef {
  return {
    kind: 'artifact',
    key: artifact.key,
    filename: artifact.filename,
    mimeType: artifact.mime_type,
    size: artifact.size,
    title: artifact.title,
  }
}

export function workspaceUriToResourceRef(uri: string, options: { runId?: string; projectId?: string } = {}): WorkspaceFileResourceRef | null {
  if (!uri.startsWith(WORKSPACE_URI_PREFIX)) return null
  const path = normalizeWorkspacePath(uri.slice(WORKSPACE_URI_PREFIX.length))
  const filename = filenameFromPath(path)
  return {
    kind: 'workspace-file',
    path,
    filename,
    name: filename,
    mimeType: normalizeMimeType(guessMimeType(path), filename),
    runId: options.runId,
    projectId: options.projectId,
  }
}

export function browserUriToResourceRef(uri: string): BrowserResourceRef | null {
  if (!uri.startsWith(BROWSER_URI_PREFIX)) return null
  const url = normalizeBrowserUrl(uri.slice(BROWSER_URI_PREFIX.length))
  if (!url) return null
  return {
    kind: 'browser',
    url,
    title: new URL(url).hostname,
  }
}

const WINDOWS_ABSOLUTE_PATH_RE = /^[a-zA-Z]:\//

function normalizeSlashPath(path: string): string {
  return path.trim().replace(/\\/g, '/').replace(/\/+/g, '/')
}

function normalizeAbsolutePath(path: string): string {
  const normalized = normalizeSlashPath(path).replace(/^\/([a-zA-Z]:\/)/, '$1')
  if (!WINDOWS_ABSOLUTE_PATH_RE.test(normalized)) return normalized
  return normalized.charAt(0).toUpperCase() + normalized.slice(1)
}

function isAbsolutePath(path: string): boolean {
  return path.startsWith('/') || WINDOWS_ABSOLUTE_PATH_RE.test(path)
}

function stripFileScheme(value: string): string | null {
  if (!value.startsWith(FILE_URI_PREFIX)) return value
  try {
    const parsed = new URL(value)
    return decodeURIComponent(parsed.pathname)
  } catch {
    return value.slice(FILE_URI_PREFIX.length).replace(/^\/\//, '')
  }
}

function relativeLocalPath(rootPath: string, filePath: string): string | null {
  const root = normalizeAbsolutePath(rootPath).replace(/\/+$/g, '')
  const file = normalizeAbsolutePath(filePath)
  if (!root || !file) return null
  if (file === root) return ''
  const windowsPath = WINDOWS_ABSOLUTE_PATH_RE.test(root) && WINDOWS_ABSOLUTE_PATH_RE.test(file)
  const prefix = `${root}/`
  const comparableFile = windowsPath ? file.toLowerCase() : file
  const comparablePrefix = windowsPath ? prefix.toLowerCase() : prefix
  if (!comparableFile.startsWith(comparablePrefix)) return null
  return file.slice(prefix.length)
}

function splitAbsoluteLocalPath(path: string): { rootPath: string; path: string } | null {
  const normalized = normalizeAbsolutePath(path)
  const slashIndex = normalized.lastIndexOf('/')
  if (slashIndex <= 0) return null
  return {
    rootPath: normalized.slice(0, slashIndex),
    path: normalized.slice(slashIndex + 1),
  }
}

export function filePathToResourceRef(
  value: string,
  options: {
    runId?: string
    projectId?: string
    workFolder?: string | null
  } = {},
): ResourceRef | null {
  const stripped = stripFileScheme(value)
  if (!stripped) return null
  const path = normalizeAbsolutePath(stripped)
  if (!isAbsolutePath(path)) return null

  if (path === '/workspace' || path.startsWith('/workspace/')) {
    const workspacePath = normalizeWorkspacePath(path.slice('/workspace'.length))
    const filename = filenameFromPath(workspacePath)
    return {
      kind: 'workspace-file',
      path: workspacePath,
      filename,
      name: filename,
      mimeType: normalizeMimeType(guessMimeType(workspacePath), filename),
      runId: options.runId,
      projectId: options.projectId,
    }
  }

  if (options.workFolder) {
    const relativePath = relativeLocalPath(options.workFolder, path)
    if (relativePath != null && relativePath !== '') {
      const filename = filenameFromPath(relativePath)
      const ref: LocalFileResourceRef = {
        kind: 'local-file',
        rootPath: options.workFolder,
        path: relativePath,
        filename,
        name: filename,
        mimeType: normalizeMimeType(guessMimeType(relativePath), filename),
      }
      return ref
    }
  }

  const localPath = splitAbsoluteLocalPath(path)
  if (!localPath) return null
  const filename = filenameFromPath(localPath.path)
  return {
    kind: 'local-file',
    rootPath: localPath.rootPath,
    path: localPath.path,
    filename,
    name: filename,
    mimeType: normalizeMimeType(guessMimeType(localPath.path), filename),
  }
}

export function resourceUriToResourceRef(
  uri: string,
  options: {
    artifacts?: ArtifactRef[]
    runId?: string
    projectId?: string
    workFolder?: string | null
  } = {},
): ResourceRef | null {
  if (uri.startsWith(ARTIFACT_URI_PREFIX)) {
    const key = uri.slice(ARTIFACT_URI_PREFIX.length)
    const artifact = options.artifacts?.find((item) => item.key === key)
    if (!artifact) return null
    return artifactToResourceRef(artifact)
  }
  return browserUriToResourceRef(uri) ?? workspaceUriToResourceRef(uri, options) ?? filePathToResourceRef(uri, options)
}

export function resourceTitle(resource: ResourceRef): string {
  if (resource.kind === 'artifact') return resource.title ?? resource.filename ?? 'Artifact'
  if (resource.kind === 'browser') return resource.title ?? new URL(resource.url).hostname
  if (resource.kind === 'local-file') return resource.name ?? resource.filename ?? filenameFromPath(resource.path)
  return resource.name ?? resource.filename ?? filenameFromPath(resource.path)
}
