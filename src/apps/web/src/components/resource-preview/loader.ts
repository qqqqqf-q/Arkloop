import { apiBaseUrl } from '@arkloop/shared/api'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { LoadPreviewResourceOptions, PreviewResource, ResourceRef, WorkspaceFileResourceRef } from './types'
import { filenameFromPath, isIframeMime, isTextMime, normalizeMimeType } from './mime'

function resourceSource(ref: ResourceRef): PreviewResource['source'] {
  return ref.source ?? ref.kind
}

function normalizeWorkspacePath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed) return '/'
  return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

export function buildWorkspaceResourceUrl(ref: WorkspaceFileResourceRef): string {
  const path = normalizeWorkspacePath(ref.path)
  if (ref.projectId) {
    const sp = new URLSearchParams({ path })
    return `${apiBaseUrl()}/v1/projects/${ref.projectId}/workspace/file?${sp.toString()}`
  }
  if (!ref.runId) throw new Error('workspace-file requires runId or projectId')
  const sp = new URLSearchParams({ run_id: ref.runId, path })
  return `${apiBaseUrl()}/v1/workspace-files?${sp.toString()}`
}

function headersWithAuth(accessToken?: string): HeadersInit | undefined {
  if (!accessToken) return undefined
  return { Authorization: `Bearer ${accessToken}` }
}

function base64ToBlob(data: string, mimeType: string): Blob {
  const binary = atob(data)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
  return new Blob([buffer], { type: mimeType })
}

function readBlobText(blob: Blob): Promise<string> {
  if (typeof FileReader === 'undefined') return new Response(blob).text()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error ?? new Error('blob read failed'))
    reader.readAsText(blob)
  })
}

async function blobToPreviewResource(
  ref: ResourceRef,
  responseBlob: Blob,
  mimeType: string,
  filename: string,
  size?: number,
): Promise<PreviewResource> {
  if (isTextMime(mimeType) || isIframeMime(mimeType)) {
    return {
      source: resourceSource(ref),
      ref,
      mimeType,
      filename,
      size: size ?? responseBlob.size,
      text: await readBlobText(responseBlob),
    }
  }

  return {
    source: resourceSource(ref),
    ref,
    mimeType,
    filename,
    size: size ?? responseBlob.size,
    blobUrl: URL.createObjectURL(responseBlob),
  }
}

async function loadLocalFile(ref: Extract<ResourceRef, { kind: 'local-file' }>): Promise<PreviewResource> {
  const desktopApi = getDesktopApi()
  if (!desktopApi?.fs?.readFile) throw new Error('desktop fs is unavailable')

  const result = await desktopApi.fs.readFile(ref.rootPath, ref.path)
  if ('error' in result) throw new Error(result.error)

  const filename = ref.filename ?? ref.name ?? filenameFromPath(ref.path)
  const mimeType = normalizeMimeType(ref.mimeType ?? result.mime_type, filename)
  const blob = base64ToBlob(result.data, mimeType)
  return blobToPreviewResource(ref, blob, mimeType, filename, ref.size)
}

async function loadFetchResource(
  ref: ResourceRef,
  url: string,
  fallbackMimeType: string | undefined,
  filename: string,
  options: LoadPreviewResourceOptions,
): Promise<PreviewResource> {
  const response = await fetch(url, {
    headers: headersWithAuth(options.accessToken),
    signal: options.signal,
  })
  if (!response.ok) throw new Error(`${response.status}`)

  const mimeType = normalizeMimeType(response.headers.get('content-type') ?? fallbackMimeType, filename)
  const contentLength = Number(response.headers.get('content-length') ?? '')
  const size = Number.isFinite(contentLength) && contentLength >= 0 ? contentLength : undefined
  const blob = await response.blob()
  return blobToPreviewResource(ref, blob, mimeType, filename, ('size' in ref ? ref.size : undefined) ?? size)
}

async function loadArtifact(
  ref: Extract<ResourceRef, { kind: 'artifact' }>,
  options: LoadPreviewResourceOptions,
): Promise<PreviewResource> {
  const filename = ref.filename ?? ref.title ?? ref.key
  return loadFetchResource(ref, `${apiBaseUrl()}/v1/artifacts/${ref.key}`, ref.mimeType, filename, options)
}

async function loadWorkspaceFile(
  ref: Extract<ResourceRef, { kind: 'workspace-file' }>,
  options: LoadPreviewResourceOptions,
): Promise<PreviewResource> {
  const filename = ref.filename ?? ref.name ?? filenameFromPath(ref.path)
  return loadFetchResource(ref, buildWorkspaceResourceUrl(ref), ref.mimeType, filename, options)
}

export async function loadPreviewResource(
  ref: ResourceRef,
  options: LoadPreviewResourceOptions = {},
): Promise<PreviewResource> {
  switch (ref.kind) {
    case 'local-file':
      return loadLocalFile(ref)
    case 'artifact':
      return loadArtifact(ref, options)
    case 'workspace-file':
      return loadWorkspaceFile(ref, options)
    case 'browser':
      throw new Error('browser resources are rendered by the browser panel')
  }
}
